package board

import (
	"context"
	"errors"
	"time"
	"unicode/utf8"

	"github.com/mynameis-nigel/ssh-bulletin/internal/store"
)

type Draft = store.Draft
type DraftKey = store.DraftKey

var ErrDraftChanged = store.ErrDraftChanged

// NextDraftRevision orders inputs before their asynchronous writes run. A
// loaded revision also works as the lower bound after a server restart.
func (s *Service) NextDraftRevision(after int64) int64 {
	for {
		last := s.draftRevision.Load()
		next := max(time.Now().UnixNano(), after+1, last+1)
		if s.draftRevision.CompareAndSwap(last, next) {
			return next
		}
	}
}

func (s *Service) Draft(ctx context.Context, actorID int64, key DraftKey) (Draft, error) {
	if actorID == 0 {
		return Draft{}, ErrNoAccount
	}
	return s.st.Draft(ctx, actorID, key)
}

func (s *Service) SaveDraft(ctx context.Context, actorID int64, key DraftKey, draft Draft) error {
	if actorID == 0 {
		return ErrNoAccount
	}
	if draft.Revision <= 0 {
		return ErrDraftChanged
	}
	// Drafts retain whitespace and formatting so a partial post reopens as typed.
	if utf8.RuneCountInString(draft.Title) > MaxTitleLen || utf8.RuneCountInString(draft.Body) > MaxBodyLen {
		return ErrTooLong
	}
	if draft.Title == "" && draft.Body == "" {
		draft.ReplyTo = 0
	}
	err := s.st.SaveDraft(ctx, actorID, key, draft, s.now().Unix())
	if errors.Is(err, store.ErrNotFound) {
		return ErrNotFound
	}
	return err
}

func (s *Service) SubmitDraft(ctx context.Context, author Account, key DraftKey, draft Draft) (threadID, postID int64, err error) {
	if draft.Revision <= 0 {
		return 0, 0, ErrDraftChanged
	}
	if key.ThreadID == 0 {
		threadID, err = s.createThread(ctx, author, key.BoardID, draft.Title, draft.Body, draft.Revision)
		return threadID, 0, err
	}
	thread, err := s.Thread(ctx, key.ThreadID)
	if err != nil {
		return 0, 0, err
	}
	if thread.BoardID != key.BoardID {
		return 0, 0, ErrNotFound
	}
	postID, err = s.replyTo(ctx, author, thread, draft.ReplyTo, draft.Body, draft.Revision)
	return thread.ID, postID, err
}
