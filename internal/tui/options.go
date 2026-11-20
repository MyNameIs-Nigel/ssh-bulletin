package tui

import (
	"errors"
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/mynameis-nigel/ssh-bulletin/internal/board"
	"github.com/mynameis-nigel/ssh-bulletin/internal/tui/kit"
)

// optionsState keeps actions attached to the thread, post, or chat message
// selected at open.
type optionsState struct {
	thread     board.Thread
	post       board.Post
	chat       board.ChatMessage
	profile    board.Account
	removing   bool // confirming a staff removal of chat rather than a deletion
	opening    bool
	menu       kit.Menu
	actions    []func(*Model) tea.Cmd
	confirming bool
	busy       bool
}

func (m *Model) openProfileOptions() {
	a := m.userProfile.data.Account
	if m.acct == nil || a.ID == 0 || a.ID == m.viewerID() {
		return
	}
	m.opts = optionsState{profile: a, menu: kit.NewMenu(nil)}
	m.opts.menu.Rows = 1
	add := func(label string, run func(*Model) tea.Cmd) {
		m.opts.menu.Items = append(m.opts.menu.Items, kit.Item{Title: label, Stops: m.accent()})
		m.opts.actions = append(m.opts.actions, run)
	}
	add("Report profile", func(m *Model) tea.Cmd {
		m.startContentReport(board.ModerationTarget{Kind: board.ProfileTarget, ID: a.ID}, "@"+a.Handle)
		return nil
	})
	if m.canModerate() && a.Role != board.RoleOwner && (a.Role != board.RoleAdmin || m.isOwner()) {
		for _, opt := range []struct {
			label  string
			action board.ModerationAction
		}{{"Time out account", board.TimedOut}, {"Suspend account", board.Suspended}, {"Lift restriction", board.RestrictionLifted}} {
			add(opt.label, func(m *Model) tea.Cmd {
				m.startRestrictionForm(opt.action, a)
				return nil
			})
		}
	}
	add(m.muteAction(a.ID))
	m.compose.lastShown = ovOptions
	m.openOverlay(ovOptions)
}

type pinResultMsg struct {
	title  string
	pinned bool
	err    error
}

type deleteResultMsg struct {
	threadID int64
	postID   int64
	err      error
}

// optionsTarget is the thread the options menu would act on right now.
func (m Model) optionsTarget() (board.Thread, bool) {
	switch m.screen {
	case scrBoard:
		if i := m.threadMenu.Selected(); i >= 0 && i < len(m.threads) {
			return m.threads[i], true
		}
	case scrThread:
		if m.postCursor < len(m.posts) {
			return m.curThread, true
		}
	}
	return board.Thread{}, false
}

func (m *Model) openOptions() {
	t, ok := m.optionsTarget()
	if !ok {
		return
	}
	m.opts = optionsState{thread: t, menu: kit.NewMenu(nil)}
	m.opts.menu.Rows = 1
	if m.screen == scrThread {
		m.opts.post = m.posts[m.postCursor]
		m.opts.opening = m.postCursor == 0
	}
	stops := kit.PaletteNamed(m.curBoard.Palette).Stops
	var items []kit.Item
	add := func(label, meta string, run func(*Model) tea.Cmd) {
		items = append(items, kit.Item{Title: label, Meta: meta, Stops: stops})
		m.opts.actions = append(m.opts.actions, run)
	}
	if m.canModerate() && !t.OpeningUnavailable {
		if t.Pinned {
			add("Unpin from board", "returns it to activity order", (*Model).togglePin)
		} else {
			add("Pin to top of board", "keeps it above newer threads", (*Model).togglePin)
		}
	}
	p := m.opts.post
	authorID := t.AuthorID
	if p.ID != 0 {
		authorID = p.AuthorID
	}
	add("View author profile", "", func(m *Model) tea.Cmd {
		m.closeOverlay()
		return m.openUserProfile(authorID)
	})
	if p.ID != 0 && p.Placeholder() == "" && !t.Removed && !t.Locked() && m.acct != nil {
		ref := fmt.Sprintf("#%d/%d ", t.ID, m.postCursor+1)
		add("Reply linking this post", strings.TrimSpace(ref), func(m *Model) tea.Cmd {
			m.closeOverlay()
			m.openCompose(composeReply)
			if m.overlay == ovCompose && !strings.Contains(m.compose.body.Value(), strings.TrimSpace(ref)) {
				m.compose.body.InsertString(ref)
			}
			return nil
		})
	}
	if p.ID != 0 && p.Placeholder() == "" {
		kind := postKind(m.opts.opening)
		if m.viewerID() == p.AuthorID {
			add("Delete "+kind, "", func(m *Model) tea.Cmd {
				m.opts.confirming = true
				return nil
			})
		}
		if m.canModerate() {
			add("Remove "+kind, "", func(m *Model) tea.Cmd {
				m.startModerationForm(board.Removed, p.ID, t.Title, m.opts.opening)
				return nil
			})
		}
	}
	if m.acct != nil && authorID != m.viewerID() {
		label, mute := m.muteAction(authorID)
		add(label, "", mute)
	}
	if m.canModerate() && !t.Removed {
		label := "Lock thread"
		if t.Locked() {
			label = "Unlock thread"
		}
		add(label, "", func(m *Model) tea.Cmd {
			m.startThreadLockForm(t)
			return nil
		})
	}
	m.opts.menu.SetItems(items)
	m.compose.lastShown = ovOptions
	m.openOverlay(ovOptions)
}

func (m *Model) updateOptions(msg tea.Msg) tea.Cmd {
	k, ok := msg.(tea.KeyPressMsg)
	if !ok || m.opts.busy {
		return nil
	}
	if m.opts.confirming {
		switch k.String() {
		case "esc", "q", "o", "n":
			m.opts.confirming = false
		case "y":
			if m.opts.chat.ID != 0 {
				return m.withdrawChat()
			}
			return m.deletePost()
		}
		return nil
	}
	switch k.String() {
	case "esc", "q", "o":
		m.closeOverlay()
	case "up", "k":
		m.opts.menu.Move(-1)
		m.opts.menu.Snap()
	case "down", "j":
		m.opts.menu.Move(1)
		m.opts.menu.Snap()
	case "enter":
		if i := m.opts.menu.Selected(); i >= 0 {
			return m.opts.actions[i](m)
		}
	}
	return nil
}

// optionsMenu is the options list with each row a hot spot that runs it.
func (m Model) optionsMenu() kit.Menu {
	menu := m.opts.menu
	m.menuHot(&menu, "option", func(m *Model, i int) tea.Cmd {
		if m.opts.busy || m.opts.confirming || i >= len(m.opts.actions) {
			return nil
		}
		m.opts.menu.Select(i)
		return m.opts.actions[i](m)
	})
	return menu
}

func (m *Model) deletePost() tea.Cmd {
	m.opts.busy = true
	d, actor, post := m.d, m.viewerID(), m.opts.post
	return func() tea.Msg {
		return deleteResultMsg{threadID: post.ThreadID, postID: post.ID, err: d.Service.DeletePost(d.Context, actor, post.ID)}
	}
}

func (m *Model) receiveDelete(msg deleteResultMsg) tea.Cmd {
	m.opts.busy = false
	if m.overlay == ovOptions {
		m.closeOverlay()
	}
	if msg.err != nil {
		m.setFlash(friendly(msg.err))
		return nil
	}
	m.toasts.Push(m.now(), "Post deleted", "", m.myStops())
	return m.onEvent(board.Event{Kind: board.EventModeration, ThreadID: msg.threadID, PostID: msg.postID})
}

// togglePin sends the opposite of the snapshot's state as an explicit target.
func (m *Model) togglePin() tea.Cmd {
	if m.acct == nil {
		return nil
	}
	m.opts.busy = true
	d, actor, t := m.d, m.acct.ID, m.opts.thread
	pin := !t.Pinned
	return func() tea.Msg {
		return pinResultMsg{title: t.Title, pinned: pin, err: d.Service.SetPinned(d.Context, actor, t.ID, pin)}
	}
}

func (m *Model) receivePin(msg pinResultMsg) tea.Cmd {
	m.opts.busy = false
	if m.overlay == ovOptions {
		m.closeOverlay()
	}
	switch {
	case errors.Is(msg.err, board.ErrForbidden):
		m.setFlash(friendly(msg.err))
		return m.refreshRole()
	case msg.err != nil:
		m.setFlash(friendly(msg.err))
		return nil
	}
	head := "Pinned to the top"
	if !msg.pinned {
		head = "Unpinned"
	}
	m.toasts.Push(m.now(), head, msg.title, m.myStops())
	return nil
}

func (m Model) viewOptions() string {
	acc := m.accent()
	w := min(m.w-10, 56)
	menu := m.optionsMenu()
	title := "thread options"
	label, subject := "thread  ", m.listingTitle(m.opts.thread)
	question, verb := "Permanently delete this "+postKind(m.opts.opening)+"?", "delete"
	switch {
	case m.opts.profile.ID != 0:
		title, label, subject = "profile options", "player  ", "@"+m.opts.profile.Handle
	case m.opts.chat.ID != 0:
		title = "message options"
		label, subject = m.opts.chat.AuthorHandle+"  ", m.opts.chat.Body
		question = "Permanently delete this message?"
		if m.opts.removing {
			question, verb = "Remove this message from the chat?", "remove"
		}
	case m.opts.post.ID != 0:
		title = postKind(m.opts.opening) + " options"
	}
	body := "\n" + kit.S(m.th.Muted).Render(label) + kit.S(m.th.Text).Bold(true).Render(clip(subject, max(1, w-6-len([]rune(label))))) + "\n" +
		kit.Rule(w-4, "─", acc, 0) + "\n"
	if m.opts.confirming {
		body += kit.S(m.th.Text).Render(question) + "\n" +
			m.hintBar([]kit.Hint{{"y", verb}, {"esc", "cancel"}}, acc, w-4, "options")
	} else {
		body += menu.Render(w-4, max(1, min(2*len(menu.Items)-1, m.h-10)), m.th, true) + "\n" +
			m.hintBar([]kit.Hint{{"↑↓", "move"}, {"⏎", "choose"}, {"esc", "close"}}, acc, w-4, "options")
	}
	p := kit.Panel{Title: "⚙ " + title, Width: w, Height: lipgloss.Height(body) + 2, Stops: acc, Focused: true,
		Phase: m.panelPhase(), Fill: m.th.Overlay, Theme: m.th}
	return p.Render(body)
}
