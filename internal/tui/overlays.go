package tui

import (
	"fmt"
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
	case ovOptions:
		return m.updateOptions(msg)
	case ovLink:
		return m.updateLinkModal(msg)
	case ovHelp:
		m.updateHelp(msg)
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
	case ovOptions:
		card = m.viewOptions()
	case ovLink:
		card = m.viewLinkModal(m.link, m.panelPhase())
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
	if m.shownOverlay() == ovHelp {
		y = min(y, max(0, m.h-h-1))
	}
	sc.Overlay(card, max(0, (m.w-w)/2), y, m.th.Overlay)
}

// ---- compose ------------------------------------------------------------

type composeMode int

const (
	composeThread composeMode = iota
	composeReply
)

type composeDestination struct {
	boardID, threadID int64 // zero threadID means a new thread on boardID
}

type composeState struct {
	destination         composeDestination
	board               board.Board
	thread              board.Thread
	replyTo             int64
	revision            int64
	loadRequest         int
	loading, loadFailed bool
	linkedPost          int64
	linkedRef           string
	edit                board.Post
	editOpening         bool
	title               textinput.Model
	body                textarea.Model
	focus               int // 0 title, 1 body
	err                 string
	busy                bool
	busyLabel           string
	discarding          bool
	lastShown           overlayKind
}

func newCompose(th kit.Theme) composeState {
	c := composeState{title: textinput.New(), body: textarea.New()}
	c.title.Prompt = "▍ "
	c.title.Placeholder = "A title worth clicking"
	c.title.CharLimit = board.MaxTitleLen
	c.body.Placeholder = "Write something. **bold**, *italic*, `code`, - lists and > quotes all work. Link @users, #boards and #12 threads."
	c.body.ShowLineNumbers = false
	c.body.CharLimit = board.MaxBodyLen
	c.body.Prompt = "▍ "
	return c
}

func (c *composeState) resize(w, h int) {
	cw := min(w-8, 86)
	c.title.SetWidth(cw - 10) // prompt (2) + padding (4) + border (2) + cursor (1), with a cell to spare
	c.body.SetWidth(cw - 6)
	c.body.SetHeight(max(4, min(h-16, 14)))
}

func (c *composeState) reset() {
	c.title.Reset()
	c.body.Reset()
	c.err, c.busy, c.discarding = "", false, false
	c.replyTo, c.revision, c.linkedPost, c.linkedRef = 0, 0, 0, ""
	c.loading, c.loadFailed, c.edit, c.editOpening = false, false, board.Post{}, false
}

func (m *Model) openCompose(mode composeMode) tea.Cmd {
	if mode == composeReply && m.curThread.Removed {
		m.setFlash("This thread is closed to new replies")
		return nil
	}
	if mode == composeReply && m.curThread.Locked() {
		m.setFlash("This thread is locked to new replies")
		return nil
	}
	if m.acct == nil {
		m.setFlash("create an account first")
		return nil
	}
	c := &m.compose
	if c.busy {
		m.setFlash("Waiting for your post to finish")
		return nil
	}
	b, t := m.curBoard, board.Thread{}
	if m.screen == scrHome || b.ID == 0 {
		if i := m.boardMenu.Selected(); i >= 0 && i < len(m.boards) {
			b = m.boards[i]
		}
	}
	if mode == composeReply {
		t = m.curThread
		t.Title = m.readingTitle()
	}
	if b.ID == 0 || (mode == composeReply && t.ID == 0) {
		m.setFlash("Choose a board or thread to post in")
		return nil
	}
	c.reset()
	c.board, c.thread = b, t
	c.destination = composeDestination{boardID: b.ID, threadID: t.ID}
	m.showCompose()
	c.loadRequest++
	c.loading = true
	d, viewer, key, request := m.d, m.viewerID(), c.destination.draftKey(), c.loadRequest
	return func() tea.Msg {
		draft, err := d.Service.Draft(d.Context, viewer, key)
		return draftLoadedMsg{request: request, draft: draft, err: err}
	}
}

func (c composeState) hasTitle() bool {
	return c.editOpening || c.edit.ID == 0 && c.destination.threadID == 0
}

func (m *Model) showCompose() {
	c := &m.compose
	c.lastShown = ovCompose
	c.resize(m.w, m.h)
	stops := m.myStops()
	kit.StyleInput(&c.title, m.th, stops)
	kit.StyleArea(&c.body, m.th, stops)
	c.focus = 1
	if c.hasTitle() {
		c.focus = 0
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
	if c.loading || c.loadFailed {
		if k, ok := msg.(tea.KeyPressMsg); ok && k.String() == "esc" {
			m.closeOverlay()
		}
		return nil
	}
	if c.discarding {
		if k, ok := msg.(tea.KeyPressMsg); ok {
			switch k.String() {
			case "y":
				return m.persistCompose(draftDiscard)
			case "esc", "n":
				c.discarding = false
			}
		}
		return nil
	}
	if k, ok := msg.(tea.KeyPressMsg); ok {
		switch k.String() {
		case "esc":
			if c.edit.ID != 0 || c.revision == 0 && c.title.Value() == "" && c.body.Value() == "" {
				m.closeOverlay()
				return nil
			}
			return m.persistCompose(draftClose)
		case "ctrl+x":
			c.discarding = c.edit.ID == 0
			return nil
		case "tab", "shift+tab":
			if c.hasTitle() {
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
	beforeTitle, beforeBody := c.title.Value(), c.body.Value()
	var cmd tea.Cmd
	if c.focus == 0 {
		c.title, cmd = c.title.Update(msg)
	} else {
		c.body, cmd = c.body.Update(msg)
	}
	if c.edit.ID == 0 && (beforeTitle != c.title.Value() || beforeBody != c.body.Value()) {
		return tea.Batch(cmd, m.persistCompose(draftAuto))
	}
	return cmd
}

func (m *Model) submitCompose() tea.Cmd {
	c := &m.compose
	if c.busy || m.acct == nil {
		return nil
	}
	acct, d := *m.acct, m.d
	destination := c.destination
	title, body := c.title.Value(), c.body.Value()
	if strings.TrimSpace(body) == "" {
		c.err = board.ErrEmptyBody.Error()
		return nil
	}
	if c.hasTitle() && strings.TrimSpace(title) == "" {
		c.err = board.ErrEmptyTitle.Error()
		return nil
	}
	c.busy, c.busyLabel = true, "Posting…"
	if c.edit.ID != 0 {
		c.busyLabel = "Saving changes…"
		post := c.edit
		return func() tea.Msg {
			changed, err := d.Service.EditPost(d.Context, acct.ID, post, title, body)
			return editSavedMsg{post: post, changed: changed, err: err}
		}
	}
	c.revision = d.Service.NextDraftRevision(c.revision)
	draft := board.Draft{Title: title, Body: body, ReplyTo: c.replyTo, Revision: c.revision}
	return func() tea.Msg {
		tid, pid, err := d.Service.SubmitDraft(d.Context, acct, destination.draftKey(), draft)
		return postedMsg{destination: destination, threadID: tid, postID: pid, err: err}
	}
}

func (m Model) viewCompose() string {
	c := m.compose
	stops := m.myStops()
	cw := min(m.w-8, 86)
	var b strings.Builder
	b.WriteString("\n")
	publicCue := ""
	if c.edit.ID == 0 && m.acct != nil && m.acct.PostCount == 0 {
		publicCue = kit.S(m.th.Muted).Render("Posts are public to every arcade player.")
	}
	if c.edit.ID != 0 {
		b.WriteString(kit.S(m.th.Muted).Render("Editing your "+postKind(c.editOpening)) + "\n\n")
		if c.hasTitle() {
			b.WriteString(c.title.View() + "\n" + kit.Rule(cw-6, "─", stops, 0) + "\n\n")
		}
	} else if c.destination.threadID == 0 {
		b.WriteString(kit.S(m.th.Muted).Render("new thread in ") + kit.GradientBold(clip(c.board.Title, max(1, cw-20)), kit.PaletteNamed(c.board.Palette).Stops) + "\n" + publicCue + "\n")
		b.WriteString(c.title.View() + "\n")
		b.WriteString(kit.Rule(cw-6, "─", stops, 0) + "\n\n")
	} else {
		destination := c.thread.Title
		if c.replyTo != 0 {
			for i, p := range m.posts {
				if p.ID == c.replyTo {
					destination = fmt.Sprintf("#%d/%d by @%s · %s", c.thread.ID, i+1, p.AuthorHandle, destination)
					break
				}
			}
		}
		b.WriteString(kit.S(m.th.Muted).Render("replying to ") + kit.S(m.th.Text).Bold(true).Render(clip(destination, max(1, cw-20))) + "\n" + publicCue + "\n")
	}
	b.WriteString(c.body.View() + "\n\n")
	count := len([]rune(c.body.Value()))
	counter := kit.S(m.th.Muted).Render(strconv.Itoa(count) + "/" + strconv.Itoa(board.MaxBodyLen))
	if c.err != "" {
		counter = kit.S(m.th.Bad).Render("✗ "+c.err) + "   " + counter
	}
	if c.loading {
		counter = kit.S(m.th.Muted).Render("Loading draft…")
	} else if c.busy {
		counter = kit.Spinner(m.frame, stops) + kit.S(m.th.Muted).Render(" "+c.busyLabel)
	}
	hints := []kit.Hint{{"ctrl+s", "post"}, {"esc", "save draft"}, {"ctrl+x", "discard"}}
	if c.edit.ID != 0 {
		hints = []kit.Hint{{"ctrl+s", "save edit"}, {"esc", "cancel"}}
	}
	if c.hasTitle() {
		hints = append(hints, kit.Hint{"tab", "field"})
	}
	if c.discarding {
		counter = kit.S(m.th.Bad).Render("Discard this draft?")
		hints = []kit.Hint{{"y", "discard"}, {"esc", "keep writing"}}
	}
	b.WriteString(counter + "\n")
	b.WriteString(m.hintBar(hints, stops, cw-6, "compose"))
	title := "✎ new thread"
	if c.destination.threadID != 0 {
		title = "✎ reply"
	}
	if c.edit.ID != 0 {
		title = "✎ edit " + postKind(c.editOpening)
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
	for _, b := range m.boards {
		b := b
		acts = append(acts, action{label: b.Title + " chat", desc: "live chat", stops: kit.PaletteNamed(b.Palette).Stops,
			run: func(m *Model) tea.Cmd { return m.openChatFor(b) }})
	}
	brand := m.th.Brand.Stops
	if m.screen != scrProfile {
		acts = append(acts,
			action{"User directory", "usernames & friends", brand, (*Model).openDirectory},
			action{"View your profile", "you", m.myStops(), func(m *Model) tea.Cmd { return m.openUserProfile(m.viewerID()) }},
		)
	}
	if m.acct != nil {
		acts = append(acts, action{"Inbox", "replies and mentions", brand, (*Model).openInbox},
			action{"Contact staff", "private message", brand, (*Model).openStaffContact})
	}
	acts = append(acts,
		action{"Home", "go", brand, func(m *Model) tea.Cmd { m.goHome(); return nil }},
		action{"Search threads", "titles and posts", brand, (*Model).openSearch},
		action{"New thread", "compose", brand, func(m *Model) tea.Cmd {
			return m.openCompose(composeThread)
		}},
		action{"Edit profile", "palette, bio, flair", m.myStops(), func(m *Model) tea.Cmd { m.openProfile(); return nil }},
		action{"Keyboard help", "help", brand, (*Model).openHelp},
		action{"Quit", "leave", brand, func(*Model) tea.Cmd { return tea.Quit }},
	)
	if m.canModerate() {
		acts = append(acts, action{"Moderation", "staff", brand, (*Model).openModeration})
	}
	if _, ok := m.optionsTarget(); ok {
		label := "Thread options"
		if m.screen == scrThread {
			label = "Post options"
		}
		acts = append(acts, action{label, "", brand, func(m *Model) tea.Cmd { m.openOptions(); return nil }})
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
	m.menuHot(&menu, "palette", func(m *Model, i int) tea.Cmd {
		m.closeOverlay()
		if i < len(m.pal.shown) {
			return m.pal.shown[i].run(m)
		}
		return nil
	})
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
