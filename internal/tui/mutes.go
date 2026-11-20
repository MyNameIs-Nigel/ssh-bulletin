package tui

import (
	"fmt"
	"slices"

	tea "charm.land/bubbletea/v2"

	"github.com/mynameis-nigel/ssh-bulletin/internal/board"
	"github.com/mynameis-nigel/ssh-bulletin/internal/tui/kit"
)

type muteSavedMsg struct {
	on  bool
	err error
}

func (m *Model) setMute(target int64, on bool) tea.Cmd {
	m.opts.busy = true
	d, viewer := m.d, m.viewerID()
	return func() tea.Msg {
		return muteSavedMsg{on: on, err: d.Service.SetMuted(d.Context, viewer, target, on)}
	}
}

func (m Model) muteAction(target int64) (string, func(*Model) tea.Cmd) {
	on := !m.muted[target]
	label := "Mute player"
	if !on {
		label = "Unmute player"
	}
	return label, func(m *Model) tea.Cmd { return m.setMute(target, on) }
}

func (m *Model) receiveMutes(ids map[int64]bool) tea.Cmd {
	m.muted = ids
	m.revealedPosts = nil
	m.linkSel = -1
	m.muteVersion++
	m.threadMenu.SetItems(m.threadItems())
	if m.screen == scrThread {
		m.followCursor()
	}
	var search tea.Cmd
	if m.screen == scrSearch && m.search.query != "" {
		search = m.runSearch(m.search.page)
	}
	c := &m.chat
	if c.request == 0 {
		return search
	}
	keep, count := m.selectedChatID(), len(c.msgs)
	c.msgs = slices.DeleteFunc(slices.Clone(c.msgs), func(msg board.ChatMessage) bool { return ids[msg.AuthorID] })
	c.loadingOlder, c.unread = false, 0
	m.reselectChat(keep)
	return tea.Batch(search, m.loadChat(chatReplace, 0, max(count, board.ChatPage)))
}

func (m Model) collapsedPost(p board.Post) bool {
	return p.Placeholder() == "" && m.muted[p.AuthorID] && !m.revealedPosts[p.ID]
}

func (m *Model) togglePostReveal(id int64) {
	if m.revealedPosts == nil {
		m.revealedPosts = make(map[int64]bool)
	}
	if m.revealedPosts[id] {
		delete(m.revealedPosts, id)
	} else {
		m.revealedPosts[id] = true
	}
	for i, p := range m.posts {
		if p.ID == id {
			m.postCursor, m.linkSel = i, -1
			m.followCursor()
			return
		}
	}
}

func (m Model) revealControl(p board.Post) string {
	label := "Muted player · reveal post (z)"
	if m.revealedPosts[p.ID] {
		label = "Hide muted post (z)"
	}
	return m.hot(fmt.Sprintf("post:%d:reveal", p.ID), func(m *Model) tea.Cmd {
		m.togglePostReveal(p.ID)
		return nil
	}, kit.S(m.th.Muted).Underline(true).Render(label))
}

func (m Model) listingTitle(t board.Thread) string {
	if m.muted[t.AuthorID] && !t.OpeningUnavailable {
		return "Thread by muted player"
	}
	return t.Title
}
