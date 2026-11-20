package tui

import (
	"context"
	"slices"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/mynameis-nigel/ssh-bulletin/internal/board"
)

type draftAction int

const (
	draftAuto draftAction = iota
	draftClose
	draftDiscard
)

type draftLoadedMsg struct {
	request int
	draft   board.Draft
	err     error
}

type draftSavedMsg struct {
	key      composeDestination
	revision int64
	action   draftAction
	err      error
}

func (k composeDestination) draftKey() board.DraftKey {
	return board.DraftKey{BoardID: k.boardID, ThreadID: k.threadID}
}

func (m *Model) receiveDraft(msg draftLoadedMsg) tea.Cmd {
	c := &m.compose
	if m.overlay != ovCompose || c.loadRequest != msg.request || !c.loading {
		return nil
	}
	c.loading = false
	if msg.err != nil {
		c.loadFailed, c.err = true, "Couldn't load the draft. Close and reopen the composer to retry."
		return nil
	}
	c.title.SetValue(msg.draft.Title)
	c.body.SetValue(msg.draft.Body)
	c.replyTo, c.revision = msg.draft.ReplyTo, msg.draft.Revision
	if c.linkedPost == 0 {
		return nil
	}
	c.replyTo = c.linkedPost
	if !slices.Contains(strings.Fields(c.body.Value()), strings.TrimSpace(c.linkedRef)) {
		c.body.InsertString(c.linkedRef)
	}
	return m.persistCompose(draftAuto)
}

func (m *Model) persistCompose(action draftAction) tea.Cmd {
	c := &m.compose
	if m.acct == nil || c.destination.boardID == 0 || c.edit.ID != 0 {
		return nil
	}
	d, viewer, key := m.d, m.viewerID(), c.destination
	c.revision = d.Service.NextDraftRevision(c.revision)
	draft := board.Draft{Revision: c.revision}
	if action != draftDiscard {
		draft.Title, draft.Body, draft.ReplyTo = c.title.Value(), c.body.Value(), c.replyTo
	}
	if action != draftAuto {
		c.busy, c.busyLabel = true, "Saving draft…"
		if action == draftDiscard {
			c.busyLabel = "Discarding draft…"
		}
	}
	return func() tea.Msg {
		// Once a keypress reaches the composer, its write can finish even if
		// the SSH session disconnects before the command returns its message.
		ctx, cancel := context.WithTimeout(context.WithoutCancel(d.Context), 5*time.Second)
		defer cancel()
		return draftSavedMsg{key: key, revision: draft.Revision, action: action,
			err: d.Service.SaveDraft(ctx, viewer, key.draftKey(), draft)}
	}
}

func (m *Model) receiveDraftSaved(msg draftSavedMsg) {
	c := &m.compose
	if msg.key != c.destination || msg.revision != c.revision {
		return
	}
	if msg.action != draftAuto {
		c.busy = false
	}
	if msg.err != nil {
		c.discarding = false
		c.err = m.writeError(msg.err)
		return
	}
	switch msg.action {
	case draftClose:
		m.closeOverlay()
		m.setFlash("Draft saved")
	case draftDiscard:
		c.reset()
		m.closeOverlay()
		m.setFlash("Draft discarded")
	}
}
