package tui

import (
	tea "charm.land/bubbletea/v2"
	"github.com/mynameis-nigel/ssh-bulletin/internal/board"
)

type editSavedMsg struct {
	post    board.Post
	changed bool
	err     error
}

func (m *Model) openEditPost(post board.Post, opening bool) {
	c := &m.compose
	if m.acct == nil || post.AuthorID != m.acct.ID || post.Placeholder() != "" || m.curThread.Removed || c.busy {
		return
	}
	c.reset()
	c.loadRequest++
	c.board, c.thread = m.curBoard, m.curThread
	c.destination = composeDestination{boardID: m.curThread.BoardID, threadID: m.curThread.ID}
	c.edit, c.editOpening = post, opening
	if opening {
		c.title.SetValue(m.curThread.Title)
	}
	c.body.SetValue(post.Body)
	m.showCompose()
}

func (m *Model) receiveEditSaved(msg editSavedMsg) tea.Cmd {
	c := &m.compose
	if c.edit.ID != msg.post.ID {
		return nil
	}
	c.busy = false
	if msg.err != nil {
		c.err = m.writeError(msg.err)
		return nil
	}
	c.reset()
	if m.overlay == ovCompose {
		m.closeOverlay()
	}
	if !msg.changed {
		return nil
	}
	m.toasts.Push(m.now(), "Edit saved", "", m.myStops())
	return m.onEvent(board.Event{Kind: board.EventModeration, ThreadID: msg.post.ThreadID, PostID: msg.post.ID})
}
