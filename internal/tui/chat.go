package tui

import (
	"errors"
	"slices"
	"strconv"
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/mynameis-nigel/ssh-bulletin/internal/board"
	"github.com/mynameis-nigel/ssh-bulletin/internal/tui/kit"
)

// Live chat. Every board has one. The board screen shows its tail in a
// section under the thread list (tab or ↓ past the last thread focuses it),
// and enter opens the chat screen: messages anchored to the bottom, newest
// last, with the input underneath — like a Discord channel.
//
// Scrolling is measured in lines up from the bottom, so a reader who has
// scrolled back keeps their place while new messages arrive below; at
// offset 0 the view follows the conversation.

const (
	// chatGroupGap is how long one author can pause and still have their
	// next message folded under the same header.
	chatGroupGap = 5 * 60
	// chatKeep caps the messages held in memory while following the bottom.
	chatKeep = 1000
)

// chatState is the live chat of the board in view. The board section and the
// chat screen share it, so moving between them never reloads.
type chatState struct {
	boardID      int64
	request      int // bumped per board: loads for another board are dropped
	msgs         []board.ChatMessage
	loaded       bool // the first page has arrived
	failed       bool // the first page could not be loaded
	stale        bool // a message arrived before the first page did
	more         bool // older messages exist on the server
	loadingOlder bool
	offset       int // lines scrolled up from the bottom; 0 follows new messages
	unread       int // messages that arrived below a reader scrolled up
	sel          int // message selected while browsing; -1 while typing
	link         int // link focused within the selected message, or -1
	input        textinput.Model
	sending      bool
	err          string
}

type chatLoadMode int

const (
	chatReplace chatLoadMode = iota // the newest page, replacing what is held
	chatOlder                       // a page above the oldest message held
	chatNewer                       // everything after the newest message held
)

type chatLoadedMsg struct {
	request int
	mode    chatLoadMode
	msgs    []board.ChatMessage
	more    bool
	err     error
	refs    board.Refs
}

type chatSentMsg struct {
	request int
	err     error
}

type chatWithdrawnMsg struct {
	id     int64
	remove bool
	err    error
}

func newChatInput() textinput.Model {
	in := textinput.New()
	in.Prompt = "› "
	in.Placeholder = "Message the board…"
	in.CharLimit = board.MaxChatLen
	return in
}

// ---- loading ------------------------------------------------------------

// resetChat points the chat at a board. Coming back to the board already
// held keeps its messages and draft and just catches up.
func (m *Model) resetChat(boardID int64) tea.Cmd {
	if m.chat.boardID == boardID && m.chat.request != 0 {
		return m.refreshChat()
	}
	m.chat = chatState{boardID: boardID, request: m.chat.request + 1, sel: -1, link: -1, input: newChatInput()}
	return m.loadChat(chatReplace, 0, board.ChatPage)
}

func (m Model) loadChat(mode chatLoadMode, cursor int64, limit int) tea.Cmd {
	d, request, boardID := m.d, m.chat.request, m.chat.boardID
	return func() tea.Msg {
		out := chatLoadedMsg{request: request, mode: mode}
		if mode == chatNewer {
			out.msgs, out.err = d.Service.ChatSince(d.Context, boardID, cursor)
		} else {
			out.msgs, out.more, out.err = d.Service.ChatHistory(d.Context, boardID, cursor, limit)
		}
		bodies := make([]string, len(out.msgs))
		for i, c := range out.msgs {
			bodies[i] = c.Body
		}
		out.refs, _ = d.Service.ResolveLines(d.Context, bodies...)
		return out
	}
}

// refreshChat fetches whatever arrived after the newest message held. Broker
// delivery is best-effort, so this catch-up is what makes chat reliable: any
// later event pulls in everything a full buffer dropped.
func (m *Model) refreshChat() tea.Cmd {
	if m.chat.request == 0 {
		return nil
	}
	if !m.chat.loaded {
		m.chat.stale = true
		return nil
	}
	return m.loadChat(chatNewer, m.lastChatID(), 0)
}

// reloadChat re-reads the messages held, for changes that rewrite them in
// place (a handle, palette or role).
func (m *Model) reloadChat() tea.Cmd {
	if !m.chat.loaded {
		return nil
	}
	return m.loadChat(chatReplace, 0, max(len(m.chat.msgs), board.ChatPage))
}

func (m Model) lastChatID() int64 {
	if n := len(m.chat.msgs); n > 0 {
		return m.chat.msgs[n-1].ID
	}
	return 0
}

func (m *Model) receiveChat(msg chatLoadedMsg) tea.Cmd {
	c := &m.chat
	if msg.request != c.request {
		return nil
	}
	m.mergeRefs(msg.refs)
	if msg.err != nil {
		switch msg.mode {
		case chatOlder:
			c.loadingOlder = false
		case chatReplace:
			if !c.loaded {
				c.loaded, c.failed = true, true
			}
		}
		m.setFlash("couldn't load chat")
		return nil
	}
	var cmds []tea.Cmd
	switch msg.mode {
	case chatReplace:
		keep := m.selectedChatID()
		c.msgs, c.more, c.loaded, c.failed = msg.msgs, msg.more, true, false
		m.reselectChat(keep)
		if c.stale {
			c.stale = false
			cmds = append(cmds, m.refreshChat())
		}
	case chatOlder:
		c.loadingOlder = false
		c.more = msg.more
		var older []board.ChatMessage
		first := int64(1<<63 - 1)
		if len(c.msgs) > 0 {
			first = c.msgs[0].ID
		}
		for _, x := range msg.msgs {
			if x.ID < first {
				older = append(older, x)
			}
		}
		c.msgs = append(older, c.msgs...)
		if c.sel >= 0 {
			c.sel += len(older)
		}
	case chatNewer:
		last := m.lastChatID()
		var fresh []board.ChatMessage
		mine := false
		for _, x := range msg.msgs {
			if x.ID > last {
				fresh = append(fresh, x)
				mine = mine || x.AuthorID == m.viewerID()
			}
		}
		if len(fresh) == 0 {
			break
		}
		from := len(c.msgs)
		c.msgs = append(c.msgs, fresh...)
		switch {
		case mine:
			// Sending a message brings you back to the conversation.
			c.offset, c.unread = 0, 0
		case c.offset > 0:
			// Keep the reader's place: the new lines land below the view.
			w, _ := m.chatGeom()
			for i := from; i < len(c.msgs); i++ {
				c.offset += len(m.chatBlock(i, w, false, ""))
			}
			c.unread += len(fresh)
		}
		if len(c.msgs) > chatKeep && c.offset == 0 && c.sel < 0 {
			c.msgs = slices.Clone(c.msgs[len(c.msgs)-chatKeep:])
			c.more = true
		}
		if len(msg.msgs) >= board.MaxChatFetch {
			cmds = append(cmds, m.refreshChat()) // a long gap: keep catching up
		}
	}
	cmds = append(cmds, m.clampChat())
	return tea.Batch(cmds...)
}

func (m Model) selectedChatID() int64 {
	if s := m.chat.sel; s >= 0 && s < len(m.chat.msgs) {
		return m.chat.msgs[s].ID
	}
	return 0
}

// reselectChat puts the selection back on message id after the list
// changed under it, or on its nearest surviving neighbour.
func (m *Model) reselectChat(id int64) {
	c := &m.chat
	if c.sel < 0 {
		return
	}
	if len(c.msgs) == 0 {
		m.stopBrowsing()
		return
	}
	i, _ := slices.BinarySearchFunc(c.msgs, id, func(x board.ChatMessage, id int64) int {
		switch {
		case x.ID < id:
			return -1
		case x.ID > id:
			return 1
		}
		return 0
	})
	c.sel = min(i, len(c.msgs)-1)
	if c.msgs[c.sel].ID != id {
		c.link = -1
	}
}

// dropChat forgets a deleted or removed message.
func (m *Model) dropChat(id int64) {
	c := &m.chat
	i := slices.IndexFunc(c.msgs, func(x board.ChatMessage) bool { return x.ID == id })
	if i < 0 {
		return
	}
	keep := m.selectedChatID()
	if keep == id && i+1 < len(c.msgs) {
		keep = c.msgs[i+1].ID
	}
	c.msgs = slices.Delete(c.msgs, i, i+1)
	m.reselectChat(keep)
}

// ---- navigation ---------------------------------------------------------

// openChat opens the current board's chat screen, scrolled to the bottom.
func (m *Model) openChat() tea.Cmd {
	m.screen = scrChat
	m.chatFocus = true
	c := &m.chat
	c.offset, c.unread, c.sel, c.err = 0, 0, -1, ""
	kit.StyleInput(&c.input, m.th, m.myStops())
	c.input.Focus()
	m.setAccent(kit.PaletteNamed(m.curBoard.Palette).Stops)
	m.touchFocus()
	return tea.Batch(m.refreshChat(), m.clampChat())
}

// openChatFor jumps straight into a board's chat (from the jump palette).
func (m *Model) openChatFor(b board.Board) tea.Cmd {
	var enter tea.Cmd
	if m.curBoard.ID != b.ID || m.chat.boardID != b.ID {
		enter = m.enterBoard(b)
	}
	return tea.Batch(enter, m.openChat())
}

func (m *Model) leaveChat() tea.Cmd {
	m.stopBrowsing()
	m.chat.input.Blur()
	m.chat.offset, m.chat.unread = 0, 0
	m.screen = scrBoard
	m.touchFocus()
	return m.loadThreads(m.curBoard.ID)
}

// ---- input --------------------------------------------------------------

func (m *Model) updateChat(msg tea.Msg) tea.Cmd {
	c := &m.chat
	k, isKey := msg.(tea.KeyPressMsg)
	if isKey && c.sel >= 0 {
		return m.updateChatBrowse(k)
	}
	if isKey {
		_, mh := m.chatGeom()
		switch k.String() {
		case "esc":
			if c.offset > 0 {
				c.offset, c.unread = 0, 0
				return nil
			}
			return m.leaveChat()
		case "enter":
			return m.sendChat()
		case "tab", "shift+tab":
			m.startBrowsing()
			return m.clampChat()
		case "up":
			return m.scrollChat(1)
		case "down":
			return m.scrollChat(-1)
		case "pgup", "ctrl+u":
			return m.scrollChat(max(1, mh/2))
		case "pgdown", "ctrl+d":
			return m.scrollChat(-max(1, mh/2))
		case "ctrl+k":
			m.openPalette()
			return nil
		}
	}
	if c.sending {
		return nil
	}
	c.err = ""
	var cmd tea.Cmd
	c.input, cmd = c.input.Update(msg)
	return cmd
}

// updateChatBrowse handles keys while a message is selected. Tab walks the
// message's links and then returns to typing, like focus moving through a
// page: input → message → its links → input.
func (m *Model) updateChatBrowse(k tea.KeyPressMsg) tea.Cmd {
	c := &m.chat
	links := m.lineLinks(c.msgs[c.sel].Body)
	switch k.String() {
	case "tab":
		if c.link+1 < len(links) {
			c.link++
			return nil
		}
		m.stopBrowsing()
		return nil
	case "shift+tab":
		if c.link >= 0 {
			c.link--
			return nil
		}
		m.stopBrowsing()
		return nil
	case "esc":
		if c.link >= 0 {
			c.link = -1
			return nil
		}
		m.stopBrowsing()
		return nil
	case "i":
		m.stopBrowsing()
		return nil
	case "up", "k":
		c.sel, c.link = max(0, c.sel-1), -1
	case "down", "j":
		c.sel, c.link = min(len(c.msgs)-1, c.sel+1), -1
	case "home", "g":
		c.sel, c.link = 0, -1
	case "end", "G":
		c.sel, c.link = len(c.msgs)-1, -1
	case "enter":
		if c.link >= 0 && c.link < len(links) {
			return m.follow(links[c.link])
		}
		m.openChatOptions()
		return nil
	case "o":
		m.openChatOptions()
		return nil
	case "v":
		return m.openUserProfile(c.msgs[c.sel].AuthorID)
	case "ctrl+k", "/":
		m.openPalette()
		return nil
	}
	m.followChatSel()
	return m.clampChat()
}

func (m *Model) scrollChat(lines int) tea.Cmd {
	m.chat.offset = max(0, m.chat.offset+lines)
	return m.clampChat()
}

// startBrowsing selects the newest message on screen, so ↑ walks back from
// where the reader is looking.
func (m *Model) startBrowsing() {
	c := &m.chat
	if len(c.msgs) == 0 {
		return
	}
	w, mh := m.chatGeom()
	lines, first, starts, _ := m.chatTail(w, c.offset+mh, -1)
	bottom := len(lines) - c.offset
	c.sel, c.link = len(c.msgs)-1, -1
	for i := len(starts) - 1; i >= 0; i-- {
		if starts[i] < bottom {
			c.sel = first + i
			break
		}
	}
	c.input.Blur()
}

func (m *Model) stopBrowsing() {
	m.chat.sel, m.chat.link = -1, -1
	if m.screen == scrChat {
		m.chat.input.Focus()
	}
}

func (m *Model) sendChat() tea.Cmd {
	c := &m.chat
	if c.sending {
		return nil
	}
	if m.acct == nil {
		m.setFlash("create an account first")
		return nil
	}
	body := c.input.Value()
	if strings.TrimSpace(body) == "" {
		return nil
	}
	c.sending, c.err = true, ""
	acct, d, boardID, request := *m.acct, m.d, c.boardID, c.request
	return func() tea.Msg {
		_, err := d.Service.SendChat(d.Context, acct, boardID, body)
		return chatSentMsg{request: request, err: err}
	}
}

func (m *Model) receiveChatSent(msg chatSentMsg) tea.Cmd {
	c := &m.chat
	if msg.request != c.request {
		return nil
	}
	c.sending = false
	if msg.err != nil {
		c.err = friendly(msg.err) // keep the draft so it can be sent again
		return nil
	}
	c.input.Reset()
	c.offset, c.unread = 0, 0
	return m.refreshChat()
}

// ---- options -------------------------------------------------------------

func (m *Model) openChatOptions() {
	c := m.chat
	if c.sel < 0 || c.sel >= len(c.msgs) {
		return
	}
	msg := c.msgs[c.sel]
	m.opts = optionsState{chat: msg, menu: kit.NewMenu(nil)}
	m.opts.menu.Rows = 1
	stops := kit.PaletteNamed(m.curBoard.Palette).Stops
	add := func(label string, run func(*Model) tea.Cmd) {
		m.opts.menu.Items = append(m.opts.menu.Items, kit.Item{Title: label, Stops: stops})
		m.opts.actions = append(m.opts.actions, run)
	}
	add("View author profile", func(m *Model) tea.Cmd {
		m.closeOverlay()
		return m.openUserProfile(msg.AuthorID)
	})
	if m.viewerID() == msg.AuthorID {
		add("Delete message", func(m *Model) tea.Cmd {
			m.opts.confirming = true
			return nil
		})
	} else if m.canModerate() {
		add("Remove message", func(m *Model) tea.Cmd {
			m.opts.confirming, m.opts.removing = true, true
			return nil
		})
	}
	m.compose.lastShown = ovOptions
	m.openOverlay(ovOptions)
}

func (m *Model) withdrawChat() tea.Cmd {
	m.opts.busy = true
	d, actor, id, remove := m.d, m.viewerID(), m.opts.chat.ID, m.opts.removing
	return func() tea.Msg {
		var err error
		if remove {
			err = d.Service.RemoveChat(d.Context, actor, id)
		} else {
			err = d.Service.DeleteChat(d.Context, actor, id)
		}
		return chatWithdrawnMsg{id: id, remove: remove, err: err}
	}
}

func (m *Model) receiveChatWithdrawn(msg chatWithdrawnMsg) tea.Cmd {
	m.opts.busy = false
	if m.overlay == ovOptions {
		m.closeOverlay()
	}
	switch {
	case errors.Is(msg.err, board.ErrForbidden):
		m.setFlash(friendly(msg.err))
		return m.refreshRole()
	case errors.Is(msg.err, board.ErrNotFound):
		m.dropChat(msg.id) // already gone
		return m.clampChat()
	case msg.err != nil:
		m.setFlash(friendly(msg.err))
		return nil
	}
	head := "Message deleted"
	if msg.remove {
		head = "Message removed"
	}
	m.toasts.Push(m.now(), head, "", m.myStops())
	m.dropChat(msg.id)
	return m.clampChat()
}

// ---- geometry -------------------------------------------------------------

// chatRect is the chat column: the same centered reading width as a thread.
func (m Model) chatRect() rect { return m.threadRect() }

func (m Model) chatPanel() kit.Panel {
	r := m.chatRect()
	return kit.Panel{Width: r.w, Height: r.h, PadX: threadPadX}
}

// chatGeom is the message text width (leaving the scrollbar a column and a
// gap) and the number of message rows above the input.
func (m Model) chatGeom() (w, rows int) {
	iw, ih := m.chatPanel().Inner()
	return max(1, iw-2), max(1, ih-2)
}

// chatSectionHeight is the board screen's chat section, under the threads.
func chatSectionHeight(bodyH int) int { return max(5, min(9, bodyH/3)) }

func (m Model) chatSectionRect() rect {
	list, _ := m.boardLayout()
	return rect{list.x, list.y + list.h, list.w, chatSectionHeight(m.body().h)}
}

// clampChat keeps the scroll offset inside the content, and asks for the
// page above once the reader reaches the top of what is held.
func (m *Model) clampChat() tea.Cmd {
	c := &m.chat
	if c.offset == 0 {
		c.unread = 0
	}
	if m.screen != scrChat || !c.loaded {
		return nil
	}
	w, mh := m.chatGeom()
	lines, _, _, complete := m.chatTail(w, c.offset+mh, -1)
	if !complete {
		return nil
	}
	c.offset = min(c.offset, max(0, len(lines)-mh))
	if c.offset == 0 {
		c.unread = 0
	}
	if c.more && !c.loadingOlder && c.offset+mh >= len(lines) && len(c.msgs) > 0 {
		c.loadingOlder = true
		return m.loadChat(chatOlder, c.msgs[0].ID, board.ChatPage)
	}
	return nil
}

// followChatSel scrolls so the selected message is in view.
func (m *Model) followChatSel() {
	c := &m.chat
	if c.sel < 0 {
		return
	}
	w, mh := m.chatGeom()
	lines, first, starts, _ := m.chatTail(w, c.offset+mh, c.sel)
	total := len(lines)
	start := starts[c.sel-first]
	end := total
	if c.sel-first+1 < len(starts) {
		end = starts[c.sel-first+1]
	}
	if end > total-c.offset {
		c.offset = total - end
	}
	if start < total-c.offset-mh {
		c.offset = total - mh - start
	}
	c.offset = max(0, c.offset)
}

// ---- rendering ------------------------------------------------------------

// chatStartsGroup reports whether message i needs its own author header:
// consecutive messages from one author close together share one.
func (m Model) chatStartsGroup(i int) bool {
	if i == 0 {
		return true
	}
	prev, cur := m.chat.msgs[i-1], m.chat.msgs[i]
	return prev.AuthorID != cur.AuthorID || cur.CreatedAt-prev.CreatedAt > chatGroupGap
}

// chatBlock renders message i at width w: a spacer and author header when it
// starts a group, then its wrapped text. The selected message wears a rail.
func (m Model) chatBlock(i, w int, selected bool, scope string) []string {
	c := m.chat.msgs[i]
	astops := kit.PaletteNamed(c.AuthorPalette).Stops
	tw := max(1, w-2)
	var out []string
	rail := func(row, n int) string {
		if !selected {
			return "  "
		}
		return kit.S(kit.At(astops, float64(row)/float64(max(1, n-1)))).Render("▌") + " "
	}
	var rows []string
	if m.chatStartsGroup(i) {
		if i > 0 {
			out = append(out, "")
		}
		head := m.authorName(scope, c.AuthorID, c.AuthorHandle, astops) + m.friendFlair(c.AuthorID) + staffFlair(c.AuthorRole, kit.At(astops, 0.7)) +
			kit.S(m.th.Muted).Render(" · "+kit.Ago(c.CreatedAt, m.now()))
		rows = append(rows, kit.Fit(head, tw))
	}
	rows = append(rows, m.richLine(c.Body, tw, richOpts{ink: kit.S(m.th.Text), accent: m.chatStops(), scope: scope})...)
	for r, row := range rows {
		out = append(out, rail(r, len(rows))+row)
	}
	return out
}

// chatTail renders messages from the newest back until it has at least need
// lines and has reached message until (-1 for no such bound). starts[k] is
// the line where message first+k begins. When every held message fit,
// complete is true and a banner heads the lines.
func (m Model) chatTail(w, need, until int) (lines []string, first int, starts []int, complete bool) {
	return m.chatTailIn(w, need, until, "chat")
}

// chatTailIn is chatTail with its hot spots in their own space, so the board
// screen's preview of the chat and the chat screen never share keys.
func (m Model) chatTailIn(w, need, until int, space string) (lines []string, first int, starts []int, complete bool) {
	var blocks [][]string
	n := 0
	i := len(m.chat.msgs) - 1
	for ; i >= 0 && (n < need || i >= until); i-- {
		scope := space + ":" + strconv.FormatInt(m.chat.msgs[i].ID, 10)
		b := m.chatBlock(i, w, i == m.chat.sel && space == "chat", scope)
		blocks = append(blocks, b)
		n += len(b)
	}
	first, complete = i+1, i < 0
	if complete {
		lines = m.chatBanner(w)
	}
	for k := len(blocks) - 1; k >= 0; k-- {
		starts = append(starts, len(lines))
		lines = append(lines, blocks[k]...)
	}
	return lines, first, starts, complete
}

// chatBanner heads the chat: the start of the conversation, or a marker that
// older messages are on their way.
func (m Model) chatBanner(w int) []string {
	stops := kit.PaletteNamed(m.curBoard.Palette).Stops
	switch {
	case m.chat.more:
		return []string{kit.Spinner(m.frame, stops) + kit.S(m.th.Muted).Render(" loading older messages…"), ""}
	case len(m.chat.msgs) == 0:
		return []string{
			kit.Fit(kit.GradientBold("Welcome to "+m.curBoard.Title+" chat", stops), w),
			kit.S(m.th.Muted).Render(kit.Fit("No one has said anything yet. Say hi!", w)),
		}
	}
	out := []string{kit.Fit(kit.GradientBold("Welcome to "+m.curBoard.Title+" chat", stops), w)}
	for _, l := range strings.Split(kit.Wrap("This is the start of the conversation. Be kind: every arcade player can read it.", w), "\n") {
		out = append(out, kit.S(m.th.Faint).Render(l))
	}
	return append(out, kit.FadeRule(w, "─", kit.At(stops, 0.5), m.th.BgBottom))
}

// chatWindow is the bottom-anchored slice of the chat that fits rows lines,
// offset lines up from the newest. It also reports the scrollbar geometry
// when the whole history held is known.
func (m Model) chatWindow(w, rows, offset int, space string) (view string, top, total int, known bool) {
	lines, _, _, complete := m.chatTailIn(w, offset+rows, -1, space)
	end := max(0, len(lines)-offset)
	start := max(0, end-rows)
	vis := lines[start:end]
	for len(vis) < rows {
		vis = append([]string{""}, vis...)
	}
	return strings.Join(vis, "\n"), start, len(lines), complete
}

func (m Model) viewChat() string {
	r := m.chatRect()
	acc := m.accent()
	stops := kit.PaletteNamed(m.curBoard.Palette).Stops
	c := m.chat
	note := ""
	if c.unread > 0 {
		note = kit.Plural(int64(c.unread), "new message", "new messages") + " ↓"
	}
	p := kit.Panel{Title: "# live chat", Note: note, Width: r.w, Height: r.h, Stops: acc, Focused: true,
		Phase: m.panelPhase(), Theme: m.th, PadX: threadPadX}
	iw, _ := p.Inner()
	w, rows := m.chatGeom()
	var msgs string
	switch {
	case !c.loaded:
		msgs = kit.Block("\n"+kit.Spinner(m.frame, stops)+kit.S(m.th.Muted).Render(" joining the chat…"), iw, rows)
	case c.failed:
		msgs = kit.Block("\n"+kit.S(m.th.Bad).Render("✗ couldn't load this chat. Press esc and try again."), iw, rows)
	default:
		view, top, total, known := m.chatWindow(w, rows, c.offset, "chat")
		msgs = kit.Block(view, iw, rows)
		if known && total > rows {
			msgs = withScrollbar(msgs, top, rows, total, m.th, stops)
		}
	}

	in := c.input
	in.SetWidth(max(1, iw-4))
	line := in.View()
	switch {
	case c.sending:
		line = kit.Spinner(m.frame, m.myStops()) + kit.S(m.th.Muted).Render(" sending…")
	case c.sel >= 0:
		line = kit.S(m.th.Faint).Render("› browsing messages · tab to type")
	}
	rule := kit.FadeRule(iw, "─", kit.At(stops, 0.5), m.th.BgBottom)
	if c.err != "" {
		rule = kit.S(m.th.Bad).Render(kit.Fit("✗ "+c.err, iw))
	}
	return pad(r.x, p.Render(msgs+"\n"+rule+"\n"+kit.Fit(line, iw)))
}

// viewChatSection is the board screen's chat teaser: the newest messages,
// one line each, under the thread list.
func (m Model) viewChatSection(r rect) string {
	acc := m.accent()
	stops := kit.PaletteNamed(m.curBoard.Palette).Stops
	p := kit.Panel{Title: "# live chat", Width: r.w, Height: r.h, Stops: acc, Focused: m.chatFocus,
		Phase: m.panelPhase(), Theme: m.th}
	iw, ih := p.Inner()
	rows := max(0, ih-1)
	var lines []string
	c := m.chat
	switch {
	case !c.loaded:
		lines = []string{kit.Spinner(m.frame, stops) + kit.S(m.th.Muted).Render(" joining…")}
	case c.failed:
		lines = []string{kit.S(m.th.Muted).Render("couldn't load chat")}
	case len(c.msgs) == 0:
		lines = []string{kit.S(m.th.Muted).Render("No one's talking yet.")}
	default:
		for i := max(0, len(c.msgs)-rows); i < len(c.msgs); i++ {
			x := c.msgs[i]
			name := kit.GradientBold(x.AuthorHandle, kit.PaletteNamed(x.AuthorPalette).Stops)
			lines = append(lines, kit.Fit(name+"  "+m.plainLine(x.Body, kit.S(m.th.Subtle)), iw))
		}
	}
	for len(lines) < rows {
		lines = append([]string{""}, lines...)
	}
	hint := kit.S(m.th.Faint).Render("tab to join the chat")
	if lit := m.lit(chatSectionKey); m.chatFocus || lit {
		hint = kit.Flow("⏎ join the chat", lipgloss.NewStyle().Bold(true), stops, lit, m.phase(), m.th)
	}
	// The whole section is one hot spot: its teaser lines are not links.
	return m.hot(chatSectionKey, func(m *Model) tea.Cmd {
		if m.chatFocus {
			return m.openChat()
		}
		m.focusChatSection(true)
		return nil
	}, p.Render(strings.Join(append(lines, hint), "\n")))
}

// chatPreview fills the board screen's right pane while the chat section is
// focused: a taller, wrapped look at the conversation.
func (m Model) chatPreview(r rect, stops []colorT) string {
	p := kit.Panel{Title: "live chat", Width: r.w, Height: r.h, Stops: stops, Theme: m.th}
	iw, ih := p.Inner()
	rows := max(1, ih-2)
	var body string
	switch {
	case !m.chat.loaded:
		body = kit.Block("\n"+kit.Spinner(m.frame, stops), iw, rows)
	default:
		view, _, _, _ := m.chatWindow(iw, rows, 0, "chatpv")
		body = kit.Block(view, iw, rows)
	}
	body += "\n" + kit.FadeRule(iw, "─", kit.At(stops, 0.5), m.th.BgBottom) + "\n" +
		kit.S(m.th.Faint).Render("⏎ join the chat")
	return p.Render(body)
}

// chatStops is the board's own gradient, for chat accents.
func (m Model) chatStops() []colorT { return kit.PaletteNamed(m.curBoard.Palette).Stops }

// chatSectionKey is the board screen's chat section as a hot spot.
const chatSectionKey = "chat-section"

// chatHints are the footer hints on the chat screen.
func (m Model) chatHints() []kit.Hint {
	if c := m.chat; c.sel >= 0 && c.sel < len(c.msgs) {
		switch {
		case c.link >= 0:
			return []kit.Hint{{"⏎", "open link"}, {"tab", "next"}, {"esc", "done"}}
		case len(m.lineLinks(c.msgs[c.sel].Body)) > 0:
			return []kit.Hint{{"↑↓", "select"}, {"tab", "links"}, {"o", "options"}, {"v", "profile"}, {"esc", "done"}}
		}
		return []kit.Hint{{"↑↓", "select"}, {"o", "options"}, {"v", "profile"}, {"tab", "type"}, {"esc", "done"}}
	}
	hints := []kit.Hint{{"⏎", "send"}, {"↑↓", "scroll"}, {"tab", "select"}}
	if m.chat.offset > 0 {
		return append(hints, kit.Hint{"esc", "jump to latest"})
	}
	return append(hints, kit.Hint{"esc", "back"}, kit.Hint{"ctrl+k", "jump"})
}
