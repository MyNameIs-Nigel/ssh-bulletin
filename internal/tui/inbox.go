package tui

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/mynameis-nigel/ssh-bulletin/internal/board"
	"github.com/mynameis-nigel/ssh-bulletin/internal/markup"
	"github.com/mynameis-nigel/ssh-bulletin/internal/tui/kit"
)

type inboxState struct {
	unread, page        int
	threadID            int64
	title               string
	entries             []board.InboxEntry
	menu                kit.Menu
	more, loading, busy bool
	err                 string
}

type inboxLoadedMsg struct {
	request, unread int
	entries         []board.InboxEntry
	more            bool
	err             error
}

type inboxOpenedMsg struct {
	entry board.InboxEntry
	err   error
}

type inboxMuteMsg struct {
	threadID int64
	on       bool
	err      error
}

func (m Model) loadInbox() tea.Cmd {
	d, viewer, request := m.d, m.viewerID(), m.inboxRequest
	if viewer == 0 {
		return nil
	}
	visible, thread, page := m.screen == scrInbox, m.inbox.threadID, m.inbox.page
	return func() tea.Msg {
		n, err := d.Service.InboxUnread(d.Context, viewer)
		out := inboxLoadedMsg{request: request, unread: n, err: err}
		if err == nil && visible {
			out.entries, out.more, out.err = d.Service.InboxPage(d.Context, viewer, thread, page)
		}
		return out
	}
}

func (m *Model) refreshInbox() tea.Cmd {
	m.inboxRequest++
	return m.loadInbox()
}

func (m *Model) openInbox() tea.Cmd {
	if m.acct == nil {
		return nil
	}
	m.inbox = inboxState{unread: m.inbox.unread}
	return m.showInbox()
}

func (m *Model) showInbox() tea.Cmd {
	m.screen = scrInbox
	m.setAccent(m.th.Brand.Stops)
	m.touchFocus()
	m.inbox.loading = true
	return m.refreshInbox()
}

func (m *Model) receiveInbox(msg inboxLoadedMsg) tea.Cmd {
	if msg.request != m.inboxRequest {
		return nil
	}
	s := &m.inbox
	s.loading = false
	if msg.err != nil {
		s.err = "Couldn't load the inbox. Press r to retry."
		return nil
	}
	s.unread, s.err = msg.unread, ""
	if m.screen != scrInbox {
		return nil
	}
	if len(msg.entries) == 0 && s.page > 0 {
		return m.pageInbox(-1)
	}
	var keep int64
	if i := s.menu.Selected(); i >= 0 && i < len(s.entries) {
		keep = inboxEntryKey(s.entries[i], s.threadID)
	}
	s.entries, s.more = msg.entries, msg.more
	items := make([]kit.Item, len(s.entries))
	for i, e := range s.entries {
		items[i] = m.inboxItem(e)
	}
	s.menu.SetItems(items)
	for i, e := range s.entries {
		if inboxEntryKey(e, s.threadID) == keep {
			s.menu.Select(i)
			break
		}
	}
	s.menu.Snap()
	return nil
}

func inboxEntryKey(e board.InboxEntry, thread int64) int64 {
	if thread == 0 {
		return e.ThreadID
	}
	return e.PostID
}

func (m Model) inboxItem(e board.InboxEntry) kit.Item {
	var kinds []string
	if e.Reasons&board.ThreadReply != 0 {
		kinds = append(kinds, "thread reply")
	}
	if e.Reasons&board.DirectReply != 0 {
		kinds = append(kinds, "direct reply")
	}
	if e.Reasons&board.Mention != 0 {
		kinds = append(kinds, "mention")
	}
	item := kit.Item{Title: m.inboxTitle(e), Desc: fmt.Sprintf("@%s · %s", e.Actor, strings.Join(kinds, " · ")), Stops: m.stopsForBoard(e.BoardID)}
	if m.inbox.threadID != 0 {
		item.Title = "@" + e.Actor
		item.Desc = strings.Join(kinds, " · ")
	}
	if e.Unread > 0 {
		item.Meta = fmt.Sprintf("%d unread", e.Unread)
	}
	return item
}

func (m Model) inboxTitle(e board.InboxEntry) string {
	return m.listingTitle(board.Thread{ID: e.ThreadID, Title: e.Title, AuthorID: e.ThreadAuthorID})
}

func (m *Model) pageInbox(change int) tea.Cmd {
	s := &m.inbox
	s.page += change
	s.loading, s.more, s.entries, s.menu = true, false, nil, kit.NewMenu(nil)
	return m.refreshInbox()
}

func (m *Model) backInbox() tea.Cmd {
	s := &m.inbox
	if s.threadID == 0 {
		m.goHome()
		return m.refreshInbox()
	}
	s.threadID, s.title, s.page = 0, "", 0
	return m.pageInbox(0)
}

func (m *Model) openInboxItem(i int) tea.Cmd {
	s := &m.inbox
	if s.busy || i < 0 || i >= len(s.entries) {
		return nil
	}
	e := s.entries[i]
	if s.threadID == 0 {
		s.threadID, s.title, s.page = e.ThreadID, m.inboxTitle(e), 0
		return m.pageInbox(0)
	}
	s.menu.Select(i)
	s.busy = true
	d, viewer := m.d, m.viewerID()
	return func() tea.Msg {
		return inboxOpenedMsg{entry: e, err: d.Service.ReadInboxPost(d.Context, viewer, e.PostID)}
	}
}

func (m *Model) receiveInboxOpened(msg inboxOpenedMsg) tea.Cmd {
	m.inbox.busy = false
	if m.screen != scrInbox {
		return nil
	}
	if msg.err != nil {
		m.setFlash(m.writeError(msg.err))
	}
	e := msg.entry
	jump := m.follow(board.Target{Ref: markup.Ref{Kind: markup.Post}, BoardID: e.BoardID, ThreadID: e.ThreadID, PostID: e.PostID})
	m.threadFromInbox = true
	return tea.Batch(jump, m.refreshInbox())
}

func (m *Model) setInboxThreadMuted(threadID int64, on bool) tea.Cmd {
	m.inbox.busy = true
	if m.overlay == ovOptions {
		m.opts.busy = true
	}
	d, viewer := m.d, m.viewerID()
	return func() tea.Msg {
		return inboxMuteMsg{threadID: threadID, on: on, err: d.Service.SetThreadMuted(d.Context, viewer, threadID, on)}
	}
}

func (m *Model) receiveInboxMute(msg inboxMuteMsg) tea.Cmd {
	m.inbox.busy, m.opts.busy = false, false
	if msg.err != nil {
		m.setFlash(m.writeError(msg.err))
		return nil
	}
	if m.overlay == ovOptions {
		m.closeOverlay()
	}
	if m.screen == scrInbox && m.inbox.threadID == msg.threadID {
		m.inbox.threadID, m.inbox.title, m.inbox.page = 0, "", 0
	}
	var reload tea.Cmd
	if m.screen == scrThread && m.curThread.ID == msg.threadID {
		m.threadMuted = msg.on
		m.contentVersion++
		reload = m.loadPosts(msg.threadID, 0)
	}
	status := "Thread notifications resumed"
	if msg.on {
		status = "Thread notifications muted"
	}
	m.toasts.Push(m.now(), status, "", m.myStops())
	return tea.Batch(reload, m.refreshInbox())
}

func (m *Model) updateInbox(msg tea.Msg) tea.Cmd {
	k, ok := msg.(tea.KeyPressMsg)
	if !ok || m.inbox.busy {
		return nil
	}
	s := &m.inbox
	switch k.String() {
	case "up", "k":
		s.menu.Move(-1)
	case "down", "j":
		s.menu.Move(1)
	case "pgup":
		if s.page > 0 && !s.loading {
			return m.pageInbox(-1)
		}
	case "pgdown":
		if s.more && !s.loading {
			return m.pageInbox(1)
		}
	case "enter":
		return m.openInboxItem(s.menu.Selected())
	case "esc", "backspace", "left", "q":
		return m.backInbox()
	case "m":
		if i := s.menu.Selected(); i >= 0 && i < len(s.entries) {
			return m.setInboxThreadMuted(s.entries[i].ThreadID, true)
		}
	case "r":
		return m.refreshInbox()
	default:
		if cmd, handled := m.globalKey(k); handled {
			return cmd
		}
	}
	s.menu.Snap()
	return nil
}

func (m Model) viewInbox() string {
	s := m.inbox
	r := m.threadRect()
	p := kit.Panel{Title: "Inbox", Note: fmt.Sprintf("%d unread · page %d", s.unread, s.page+1), Width: r.w, Height: r.h,
		Stops: m.accent(), Focused: true, Phase: m.panelPhase(), Theme: m.th, PadX: 2}
	w, h := p.Inner()
	var body string
	if s.threadID != 0 {
		body = kit.S(m.th.Text).Bold(true).Render(kit.Fit(s.title, w)) + "\n\n"
		h -= 2
	}
	switch {
	case s.err != "":
		body += s.err
	case len(s.entries) == 0 && s.loading:
		body += "Loading…"
	case len(s.entries) == 0:
		body += "No replies or mentions here."
	default:
		menu := s.menu
		m.menuHot(&menu, "inbox", func(m *Model, i int) tea.Cmd { return m.openInboxItem(i) })
		body += menu.Render(w, h, m.th, true)
	}
	return pad(r.x, p.Render(body))
}
