package tui

import (
	"math"
	"strconv"
	"strings"

	"charm.land/bubbles/v2/textarea"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/mynameis-nigel/ssh-bulletin/internal/board"
	"github.com/mynameis-nigel/ssh-bulletin/internal/tui/kit"
)

func (m *Model) openOverlay(k overlayKind) {
	m.overlay = k
	m.touchFocus()
}

func (m *Model) closeOverlay() {
	m.overlay = ovNone
	m.touchFocus()
}

// lastOverlay remembers what was open while the close animation plays.
func (m Model) shownOverlay() overlayKind {
	if m.overlay != ovNone {
		return m.overlay
	}
	return m.compose.lastShown
}

func (m *Model) updateOverlay(msg tea.Msg) tea.Cmd {
	switch m.overlay {
	case ovCompose:
		return m.updateCompose(msg)
	case ovPalette:
		return m.updatePalette(msg)
	case ovModeration:
		return m.updateModerationForm(msg)
	case ovHelp:
		if _, ok := msg.(tea.KeyPressMsg); ok {
			m.closeOverlay()
		}
	}
	return nil
}

// drawOverlay places the open overlay on the scene. The card rises a few
// rows as it fades in.
func (m Model) drawOverlay(sc *kit.Scene) {
	var card string
	switch m.shownOverlay() {
	case ovCompose:
		card = m.viewCompose()
	case ovPalette:
		card = m.viewPalette()
	case ovModeration:
		card = m.viewModerationForm()
	case ovHelp:
		card = m.viewHelp()
	default:
		return
	}
	w, h := lipgloss.Width(card), lipgloss.Height(card)
	lift := int(math.Round((1 - smooth(m.overlayT)) * 3))
	y := max(1, (m.h-h)/2+lift)
	if m.shownOverlay() == ovPalette {
		y = max(2, m.h/6+lift)
	}
	sc.Overlay(card, max(0, (m.w-w)/2), y, m.th.Overlay)
}

// ---- compose ------------------------------------------------------------

type composeMode int

const (
	composeThread composeMode = iota
	composeReply
)

type composeState struct {
	mode      composeMode
	title     textinput.Model
	body      textarea.Model
	focus     int // 0 title, 1 body
	err       string
	busy      bool
	lastShown overlayKind
	w, h      int
}

func newCompose(th kit.Theme) composeState {
	c := composeState{title: textinput.New(), body: textarea.New()}
	c.title.Prompt = "▍ "
	c.title.Placeholder = "A title worth clicking"
	c.title.CharLimit = board.MaxTitleLen
	c.body.Placeholder = "Write something. Markdown-ish is fine; it renders as plain text."
	c.body.ShowLineNumbers = false
	c.body.CharLimit = board.MaxBodyLen
	c.body.Prompt = "▍ "
	return c
}

func (c *composeState) resize(w, h int) {
	c.w, c.h = w, h
	cw := min(w-8, 86)
	c.title.SetWidth(cw - 10) // prompt (2) + padding (4) + border (2) + cursor (1), with a cell to spare
	c.body.SetWidth(cw - 6)
	c.body.SetHeight(max(4, min(h-16, 14)))
}

func (c *composeState) reset() {
	c.title.Reset()
	c.body.Reset()
	c.err, c.busy = "", false
}

func (m *Model) openCompose(mode composeMode) {
	if m.acct == nil {
		m.setFlash("create an account first")
		return
	}
	if m.compose.mode != mode {
		m.compose.reset()
	}
	m.compose.mode = mode
	m.compose.lastShown = ovCompose
	m.compose.resize(m.w, m.h)
	stops := m.myStops()
	kit.StyleInput(&m.compose.title, m.th, stops)
	kit.StyleArea(&m.compose.body, m.th, stops)
	m.compose.focus = 1
	if mode == composeThread {
		m.compose.focus = 0
	}
	m.focusCompose()
	m.openOverlay(ovCompose)
}

func (m *Model) focusCompose() {
	m.compose.title.Blur()
	m.compose.body.Blur()
	if m.compose.focus == 0 {
		m.compose.title.Focus()
	} else {
		m.compose.body.Focus()
	}
}

func (m *Model) updateCompose(msg tea.Msg) tea.Cmd {
	c := &m.compose
	if c.busy {
		return nil
	}
	if k, ok := msg.(tea.KeyPressMsg); ok {
		switch k.String() {
		case "esc":
			m.closeOverlay()
			return nil
		case "tab", "shift+tab":
			if c.mode == composeThread {
				c.focus = 1 - c.focus
				m.focusCompose()
			}
			return nil
		case "enter":
			if c.focus == 0 {
				c.focus = 1
				m.focusCompose()
				return nil
			}
		case "ctrl+s", "ctrl+d":
			return m.submitCompose()
		}
	}
	c.err = ""
	var cmd tea.Cmd
	if c.focus == 0 {
		c.title, cmd = c.title.Update(msg)
	} else {
		c.body, cmd = c.body.Update(msg)
	}
	return cmd
}

func (m *Model) submitCompose() tea.Cmd {
	c := &m.compose
	if c.busy || m.acct == nil {
		return nil
	}
	acct, d := *m.acct, m.d
	title, body := c.title.Value(), c.body.Value()
	if strings.TrimSpace(body) == "" {
		c.err = board.ErrEmptyBody.Error()
		return nil
	}
	c.busy = true
	if c.mode == composeThread {
		if strings.TrimSpace(title) == "" {
			c.busy, c.err = false, board.ErrEmptyTitle.Error()
			return nil
		}
		boardID := m.curBoard.ID
		return func() tea.Msg {
			tid, err := d.Service.CreateThread(d.Context, acct, boardID, title, body)
			return postedMsg{threadID: tid, err: err}
		}
	}
	t := m.curThread
	return func() tea.Msg {
		pid, err := d.Service.Reply(d.Context, acct, t, body)
		return postedMsg{threadID: t.ID, postID: pid, err: err}
	}
}

func (m Model) viewCompose() string {
	c := m.compose
	stops := m.myStops()
	cw := min(m.w-8, 86)
	var b strings.Builder
	b.WriteString("\n")
	if c.mode == composeThread {
		b.WriteString(kit.S(m.th.Muted).Render("new thread in ") + kit.GradientBold(m.curBoard.Title, kit.PaletteNamed(m.curBoard.Palette).Stops) + "\n\n")
		b.WriteString(c.title.View() + "\n")
		b.WriteString(kit.Rule(cw-6, "─", stops, 0) + "\n\n")
	} else {
		b.WriteString(kit.S(m.th.Muted).Render("replying to ") + kit.S(m.th.Text).Bold(true).Render(clip(m.curThread.Title, cw-20)) + "\n\n")
	}
	b.WriteString(c.body.View() + "\n\n")
	count := len([]rune(c.body.Value()))
	counter := kit.S(m.th.Muted).Render(strconv.Itoa(count) + "/" + strconv.Itoa(board.MaxBodyLen))
	if c.err != "" {
		counter = kit.S(m.th.Bad).Render("✗ "+c.err) + "   " + counter
	}
	if c.busy {
		counter = kit.Spinner(m.frame, stops) + kit.S(m.th.Muted).Render(" posting…")
	}
	b.WriteString(counter + "\n")
	b.WriteString(kit.Hints([]kit.Hint{{"ctrl+s", "post"}, {"tab", "switch"}, {"esc", "cancel"}}, m.th, stops, cw-6))
	title := "✎ new thread"
	if c.mode == composeReply {
		title = "✎ reply"
	}
	p := kit.Panel{Title: title, Width: cw, Height: lipgloss.Height(b.String()) + 2, Stops: stops, Focused: true,
		Phase: m.panelPhase(), Fill: m.th.Overlay, Theme: m.th, PadX: 2}
	return p.Render(b.String())
}

// ---- command palette ----------------------------------------------------

type action struct {
	label string
	desc  string
	stops []colorT
	run   func(m *Model) tea.Cmd
}

type paletteState struct {
	input   textinput.Model
	menu    kit.Menu
	actions []action
	shown   []action
}

func newPalette(th kit.Theme) paletteState {
	p := paletteState{input: textinput.New(), menu: kit.NewMenu(nil)}
	p.input.Prompt = "❯ "
	p.input.Placeholder = "jump to a board, or type a command…"
	p.menu.Rows = 1
	return p
}

func (m *Model) openPalette() {
	var acts []action
	for _, b := range m.boards {
		b := b
		acts = append(acts, action{label: b.Title, desc: "board", stops: kit.PaletteNamed(b.Palette).Stops,
			run: func(m *Model) tea.Cmd { return m.enterBoard(b) }})
	}
	brand := m.th.Brand.Stops
	acts = append(acts,
		action{"Home", "go", brand, func(m *Model) tea.Cmd { m.goHome(); return nil }},
		action{"New thread", "compose", brand, func(m *Model) tea.Cmd {
			if m.curBoard.ID == 0 && len(m.boards) > 0 {
				m.curBoard = m.boards[max(0, m.boardMenu.Selected())]
			}
			m.openCompose(composeThread)
			return nil
		}},
		action{"Profile & palette", "you", m.myStops(), func(m *Model) tea.Cmd { m.openProfile(); return nil }},
		action{"Keyboard help", "help", brand, func(m *Model) tea.Cmd { m.openOverlay(ovHelp); return nil }},
		action{"Quit", "leave", brand, func(*Model) tea.Cmd { return tea.Quit }},
	)
	if m.canModerate() {
		acts = append(acts, action{"Moderation", "staff", brand, (*Model).openModeration})
	}
	m.pal.actions = acts
	m.pal.input.Reset()
	m.pal.input.Focus()
	kit.StyleInput(&m.pal.input, m.th, m.accent())
	m.filterPalette()
	m.compose.lastShown = ovPalette
	m.openOverlay(ovPalette)
}

func (m *Model) filterPalette() {
	q := strings.ToLower(m.pal.input.Value())
	m.pal.shown = m.pal.shown[:0]
	var items []kit.Item
	for _, a := range m.pal.actions {
		if q != "" && !fuzzy(strings.ToLower(a.label), q) {
			continue
		}
		m.pal.shown = append(m.pal.shown, a)
		items = append(items, kit.Item{Title: a.label, Meta: a.desc, Stops: a.stops})
	}
	m.pal.menu.SetItems(items)
	m.pal.menu.Select(0)
	m.pal.menu.Snap()
}

// fuzzy reports whether every rune of q appears in s, in order.
func fuzzy(s, q string) bool {
	i := 0
	rs := []rune(q)
	for _, r := range s {
		if i < len(rs) && r == rs[i] {
			i++
		}
	}
	return i == len(rs)
}

func (m *Model) updatePalette(msg tea.Msg) tea.Cmd {
	if k, ok := msg.(tea.KeyPressMsg); ok {
		switch k.String() {
		case "esc", "ctrl+k":
			m.closeOverlay()
			return nil
		case "up", "ctrl+p":
			m.pal.menu.Move(-1)
			return nil
		case "down", "ctrl+n", "tab":
			m.pal.menu.Move(1)
			return nil
		case "enter":
			i := m.pal.menu.Selected()
			m.closeOverlay()
			if i >= 0 && i < len(m.pal.shown) {
				return m.pal.shown[i].run(m)
			}
			return nil
		}
	}
	var cmd tea.Cmd
	m.pal.input, cmd = m.pal.input.Update(msg)
	m.filterPalette()
	return cmd
}

func (m Model) viewPalette() string {
	w := min(m.w-10, 64)
	acc := m.accent()
	m.pal.input.SetWidth(w - 8)
	menu := m.pal.menu
	rows := min(m.h/2, max(1, len(m.pal.shown))*2-1)
	list := menu.Render(w-4, rows, m.th, true)
	if len(m.pal.shown) == 0 {
		list = kit.S(m.th.Muted).Render("no matches")
	}
	body := "\n" + m.pal.input.View() + "\n" + kit.Rule(w-4, "─", acc, 0) + "\n" + list
	p := kit.Panel{Title: "⌘ jump", Width: w, Height: lipgloss.Height(body) + 2, Stops: acc, Focused: true,
		Phase: m.panelPhase(), Fill: m.th.Overlay, Theme: m.th}
	return p.Render(body)
}

// ---- help ---------------------------------------------------------------

func (m Model) viewHelp() string {
	acc := m.accent()
	groups := []struct {
		title string
		keys  []kit.Hint
	}{
		{"anywhere", []kit.Hint{{"/ ctrl+k", "jump palette"}, {"p", "your profile"}, {"?", "this help"}, {"esc", "back"}, {"ctrl+c", "leave"}}},
		{"lists", []kit.Hint{{"↑↓ j k", "move"}, {"⏎ → l", "open"}, {"g G", "top / bottom"}, {"n", "new thread"}}},
		{"threads", []kit.Hint{{"↑↓", "post by post"}, {"r ⏎", "reply"}, {"f +", "kudos"}, {"!", "report"}, {"pgup pgdn", "scroll"}}},
		{"writing", []kit.Hint{{"ctrl+s", "post"}, {"tab", "title ↔ body"}, {"esc", "cancel"}}},
	}
	if m.canModerate() {
		groups[0].keys = append(groups[0].keys, kit.Hint{"m", "moderation"})
		groups[2].keys = append(groups[2].keys, kit.Hint{"x", "remove post"})
	}
	col := func(gi int) string {
		g := groups[gi]
		var b strings.Builder
		b.WriteString(kit.Gradient(strings.ToUpper(g.title), lipgloss.NewStyle().Bold(true), acc, 0) + "\n")
		for i, k := range g.keys {
			key := kit.S(kit.At(acc, float64(i)/4)).Bold(true).Render(kit.Fit(k[0], 11))
			b.WriteString(key + kit.S(m.th.Subtle).Render(k[1]) + "\n")
		}
		return b.String()
	}
	body := "\n" + kit.SideBySide(4, col(0), col(1)) + "\n" + kit.SideBySide(4, col(2), col(3)) +
		"\n" + kit.S(m.th.Faint).Render("your key is your account · posts are public to every arcade player")
	p := kit.Panel{Title: "? help", Width: lipgloss.Width(body) + 6, Height: lipgloss.Height(body) + 3, Stops: acc, Focused: true,
		Phase: m.panelPhase(), Fill: m.th.Overlay, Theme: m.th, PadX: 2}
	return p.Render(body)
}
