package tui

import (
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/mynameis-nigel/ssh-bulletin/internal/board"
	"github.com/mynameis-nigel/ssh-bulletin/internal/markup"
	"github.com/mynameis-nigel/ssh-bulletin/internal/tui/kit"
)

// Hover and links.
//
// Anything interactive is drawn through m.hot, which tags it as a hot spot
// (kit.Hot) and records what a click on it does. The compositor reports
// where every hot spot landed, so the pointer is looked up in the frame the
// player is actually looking at.
//
// A hot spot is "lit" when the pointer rests on it or the keyboard has it
// focused, and both look the same: its gradient flows (kit.Flow). The flow
// runs on the shared frame loop and settles hoverFlow after the last input,
// so a pointer left resting on a link costs nothing once it is still.
//
// Mouse motion arrives for every cell the pointer crosses, so a motion that
// does not change what is lit reuses the last frame rather than drawing a
// new one (see View): crossing the screen costs lookups, not renders.

// hoverFlow is how long a lit hot spot keeps flowing after the last input.
const hoverFlow = 6 * time.Second

// frameState is shared by every copy of a session's Model: View writes it,
// Update reads it. Bubble Tea calls both from the same goroutine.
type frameState struct {
	hot     kit.HotMap
	targets map[string]func(*Model) tea.Cmd

	view  tea.View // the last frame, for motion that changes nothing
	stamp int      // the Model.stamp it was drawn for
	valid bool
	idle  idleSplashFrames

	blocks map[string][]markup.Block // parsed bodies, by source text
	spans  map[string][]markup.Span  // parsed single lines
}

func newFrameState() *frameState {
	return &frameState{
		targets: map[string]func(*Model) tea.Cmd{},
		blocks:  map[string][]markup.Block{},
		spans:   map[string][]markup.Span{},
	}
}

// parseCacheMax bounds the parse caches; past it they start over.
const parseCacheMax = 2048

func (f *frameState) parse(body string) []markup.Block {
	if b, ok := f.blocks[body]; ok {
		return b
	}
	if len(f.blocks) >= parseCacheMax {
		clear(f.blocks)
	}
	b := markup.Parse(body)
	f.blocks[body] = b
	return b
}

func (f *frameState) parseLine(text string) []markup.Span {
	if s, ok := f.spans[text]; ok {
		return s
	}
	if len(f.spans) >= parseCacheMax {
		clear(f.spans)
	}
	s := markup.ParseLine(text)
	f.spans[text] = s
	return s
}

// hot draws s as a hot spot that runs run when clicked. An empty key draws
// s as it is.
func (m Model) hot(key string, run func(*Model) tea.Cmd, s string) string {
	if key == "" {
		return s
	}
	if run != nil {
		m.ui.targets[key] = run
	}
	return kit.Hot(key, s)
}

// lit reports whether the hot spot key is hovered or keyboard-focused.
func (m Model) lit(key string) bool {
	return key != "" && (key == m.hover || key == m.focusKey())
}

// litKey is what is lit right now, preferring the keyboard's focus.
func (m Model) litKey() string {
	if k := m.focusKey(); k != "" {
		return k
	}
	return m.hover
}

// trackLit restarts the flow whenever something new lights up.
func (m *Model) trackLit() {
	if k := m.litKey(); k != m.litWas {
		m.litWas, m.litAt = k, m.now()
	}
}

// phase is how far the lit gradient has flowed. It stops advancing when the
// flow settles, so a later redraw does not jump.
func (m Model) phase() float64 {
	end := m.now()
	if settle := m.lastInput.Add(hoverFlow); settle.Before(end) {
		end = settle
	}
	return kit.FlowPhase(max(0, end.Sub(m.litAt)))
}

// flowing reports whether a lit hot spot should still be animating.
func (m Model) flowing() bool {
	return m.litKey() != "" && m.now().Sub(m.lastInput) < hoverFlow
}

// focusKey is the hot spot the keyboard has focused: a link in the selected
// post or chat message.
func (m Model) focusKey() string {
	switch {
	case m.overlay != ovNone:
		return ""
	case m.screen == scrThread && m.linkSel >= 0 && m.postCursor < len(m.posts):
		return linkKey(postScope(m.posts[m.postCursor].ID), m.linkSel)
	case m.screen == scrChat && m.chat.sel >= 0 && m.chat.link >= 0 && m.chat.sel < len(m.chat.msgs):
		return linkKey(chatScope(m.chat.msgs[m.chat.sel].ID), m.chat.link)
	}
	return ""
}

// onMotion moves the hover. It reports whether anything visible changed.
func (m *Model) onMotion(msg tea.MouseMotionMsg) bool {
	key := ""
	if m.w >= minW && m.h >= minH {
		key = m.ui.hot.At(msg.X, msg.Y)
	}
	if key == m.hover {
		return false
	}
	m.hover = key
	return true
}

// clickHot runs the hot spot under a click, if there is one.
func (m *Model) clickHot(msg tea.MouseClickMsg) (tea.Cmd, bool) {
	if msg.Button != tea.MouseLeft || m.w < minW || m.h < minH {
		return nil, false
	}
	key := m.ui.hot.At(msg.X, msg.Y)
	run := m.ui.targets[key]
	if run == nil {
		return nil, false
	}
	screen, overlay := m.screen, m.overlay
	cmd := run(m)
	if m.screen != screen || m.overlay != overlay {
		// The pointer now rests on whatever the new view puts there; it
		// lights up again on the next motion.
		m.hover = ""
	}
	return cmd, true
}

// ---- following links ------------------------------------------------------

// follow goes where a resolved link points. External links only ever open
// the warning first.
func (m *Model) follow(t board.Target) tea.Cmd {
	if t.Ref.Internal() {
		m.linkSel, m.chat.link = -1, -1
	}
	switch t.Ref.Kind {
	case markup.URL:
		m.openLink(t.Ref.URL)
		return nil
	case markup.User:
		if m.overlay != ovNone {
			m.closeOverlay()
		}
		return m.openUserProfile(t.AccountID)
	case markup.Board:
		return m.enterBoard(m.boardFor(t))
	case markup.Thread, markup.Post:
		m.curBoard = m.boardFor(t)
		m.chatFocus = false
		m.setAccent(kit.PaletteNamed(m.curBoard.Palette).Stops)
		return m.enterThread(board.Thread{ID: t.ThreadID, BoardID: t.BoardID}, t.PostID)
	}
	return nil
}

// boardFor is the board a target lives on, as the home list knows it.
func (m Model) boardFor(t board.Target) board.Board {
	for _, b := range m.boards {
		if b.ID == t.BoardID {
			return b
		}
	}
	return board.Board{ID: t.BoardID, Slug: t.Slug, Title: t.Title, Palette: t.Palette}
}

// ---- the external link warning ---------------------------------------------

type linkModalState struct {
	url    string
	host   string
	odd    bool // the host is not plain ASCII: it may imitate another
	copied bool
}

// openLink warns before an external link can be used. The address is shown
// whole and its host apart from it, since a label or a user@host prefix can
// make a link look like it goes somewhere it does not.
func (m *Model) openLink(raw string) {
	st := linkModalState{url: raw}
	if u, err := url.Parse(raw); err == nil {
		st.host = strings.ToLower(u.Hostname())
	}
	for _, r := range st.host {
		if r > unicode.MaxASCII {
			st.odd = true
		}
	}
	m.link = st
	m.compose.lastShown = ovLink
	m.openOverlay(ovLink)
}

func (m *Model) updateLinkModal(msg tea.Msg) tea.Cmd {
	k, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return nil
	}
	switch k.String() {
	case "esc", "q", "n", "backspace":
		m.closeOverlay()
	case "c", "y", "enter":
		m.link.copied = true
		return tea.SetClipboard(m.link.url)
	}
	return nil
}

func (m Model) viewLinkModal() string {
	w := min(m.w-8, 72)
	iw := w - 6
	warn := []colorT{m.th.Warn, m.th.Bad}
	l := m.link
	var b strings.Builder
	b.WriteString("\n" + kit.GradientBold("This link leaves the bulletin.", warn) + "\n")
	b.WriteString(kit.S(m.th.Subtle).Render(kit.Wrap("Someone posted it; nobody here has checked where it goes. Don't trust it with your keys, passwords, or anything you would not post publicly.", iw)) + "\n\n")
	host := l.host
	if host == "" {
		host = "(no host)"
	}
	const label = 10
	b.WriteString(kit.S(m.th.Muted).Render(kit.Fit("goes to", label)) + kit.S(m.th.Text).Bold(true).Render(clip(host, iw-label)) + "\n")
	if l.odd {
		b.WriteString(kit.Indent(kit.S(m.th.Bad).Render(kit.Wrap("⚠ uses letters from other alphabets, which can imitate a familiar site", iw-label)), label) + "\n")
	}
	addr := strings.Split(kit.Wrap(l.url, iw-label), "\n")
	for i, line := range addr {
		lead := strings.Repeat(" ", label)
		if i == 0 {
			lead = kit.S(m.th.Muted).Render(kit.Fit("full link", label))
		}
		b.WriteString(lead + kit.Link(l.url, kit.S(linkStops[0]).Underline(true).Render(line)) + "\n")
	}
	b.WriteString("\n")
	if l.copied {
		b.WriteString(kit.S(m.th.Good).Render("✓ copied — paste it into your browser if you still want it") + "\n")
	} else {
		b.WriteString(kit.S(m.th.Faint).Render(kit.Wrap("Copy it to paste into a browser, or ctrl/cmd-click the address if your terminal opens links.", iw)) + "\n")
	}
	b.WriteString(m.hintBar([]kit.Hint{{"c", "copy link"}, {"esc", "go back"}}, warn, iw, "modal"))
	p := kit.Panel{Title: "⚠ external link", Width: w, Height: lipgloss.Height(b.String()) + 2, Stops: warn, Focused: true,
		Phase: m.panelPhase(), Fill: m.th.Overlay, Theme: m.th, PadX: 2}
	return p.Render(b.String())
}

// ---- clickable hints -------------------------------------------------------

// hintBar draws key hints whose keys can be clicked: a click presses the key.
// Hints whose key has no single press (↑↓, ←→) are drawn but not hot.
func (m Model) hintBar(hints []kit.Hint, accent []colorT, width int, scope string) string {
	var parts []string
	used := 0
	for i, h := range hints {
		part := kit.S(kit.At(accent, float64(i)/float64(max(1, len(hints)-1)))).Bold(true).Render(h[0]) + " " + kit.S(m.th.Muted).Render(h[1])
		w := lipgloss.Width(part) + 3
		if used+w > width {
			break
		}
		used += w
		if press, ok := hintPress(h[0]); ok {
			key := scope + ":hint:" + h[0]
			if m.lit(key) {
				part = kit.Flow(h[0], lipgloss.NewStyle().Bold(true), accent, true, m.phase(), m.th) +
					kit.S(m.th.Text).Background(kit.LitTint(m.th, accent)).Render(" "+h[1])
			}
			part = m.hot(key, func(m *Model) tea.Cmd { return m.routeInput(press) }, part)
		}
		parts = append(parts, part)
	}
	return strings.Join(parts, kit.S(m.th.Faint).Render(" · "))
}

// hintPress is the key press a hint's key label stands for.
func hintPress(label string) (tea.KeyPressMsg, bool) {
	switch label {
	case "⏎":
		return tea.KeyPressMsg{Code: tea.KeyEnter}, true
	case "esc":
		return tea.KeyPressMsg{Code: tea.KeyEscape}, true
	case "tab":
		return tea.KeyPressMsg{Code: tea.KeyTab}, true
	case "ctrl+s":
		return tea.KeyPressMsg{Code: 's', Mod: tea.ModCtrl}, true
	case "ctrl+k":
		return tea.KeyPressMsg{Code: 'k', Mod: tea.ModCtrl}, true
	}
	if r := []rune(label); len(r) == 1 && r[0] < unicode.MaxASCII && unicode.IsPrint(r[0]) {
		return tea.KeyPressMsg{Code: r[0], Text: label}, true
	}
	return tea.KeyPressMsg{}, false
}

// menuHot makes a menu's items hot spots in scope: the pointer lights an
// item without moving the selection, and a click runs open on it.
func (m Model) menuHot(menu *kit.Menu, scope string, open func(*Model, int) tea.Cmd) {
	menu.HotKey = func(i int) string { return scope + ":" + strconv.Itoa(i) }
	menu.Lit, menu.Phase = m.hover, m.phase()
	for i := range menu.Items {
		m.ui.targets[menu.HotKey(i)] = func(m *Model) tea.Cmd { return open(m, i) }
	}
}

// pickOrOpen is the usual click on a list row: the first click selects it,
// a click on the selected row opens it.
func pickOrOpen(menu *kit.Menu, i int, pick, open func() tea.Cmd) tea.Cmd {
	if menu.Cursor == i {
		return open()
	}
	menu.Select(i)
	return pick()
}
