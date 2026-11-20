package board

import (
	"context"
	"errors"
	"unicode/utf8"

	"github.com/mynameis-nigel/ssh-bulletin/internal/store"
)

var ErrEditChanged = store.ErrEditChanged

func (s *Service) EditPost(ctx context.Context, actorID int64, post Post, title, body string) (bool, error) {
	if actorID == 0 {
		return false, ErrNoAccount
	}
	title, body = CleanLine(title), CleanBody(body)
	if body == "" {
		return false, ErrEmptyBody
	}
	if utf8.RuneCountInString(title) > MaxTitleLen || !bodyFits(body) {
		return false, ErrTooLong
	}
	boardID, threadID, changed, err := s.st.EditPost(ctx, actorID, post.ID, post.Revision, title, body, s.now().Unix())
	if errors.Is(err, store.ErrNotFound) {
		return false, ErrNotFound
	}
	if errors.Is(err, store.ErrEditTitleRequired) {
		return false, ErrEmptyTitle
	}
	if err == nil && changed {
		s.broker.Publish(Event{Kind: EventModeration, BoardID: boardID, ThreadID: threadID, PostID: post.ID})
	}
	return changed, err
}
