package board

import (
	"context"
	"errors"

	"github.com/mynameis-nigel/ssh-bulletin/internal/markup"
	"github.com/mynameis-nigel/ssh-bulletin/internal/store"
)

type InboxEntry = store.InboxEntry

const (
	ThreadReply   = store.ThreadReply
	DirectReply   = store.DirectReply
	Mention       = store.Mention
	InboxPageSize = 20
)

var ErrReplyTarget = store.ErrReplyTarget

func mentionHandles(title, body string) []string {
	refs := markup.Refs(markup.Parse(body))
	if title != "" {
		refs = append(refs, markup.SpanRefs(markup.ParseLine(title))...)
	}
	seen := make(map[string]bool)
	var handles []string
	for _, ref := range refs {
		if ref.Kind != markup.User || seen[ref.Name] {
			continue
		}
		seen[ref.Name] = true
		handles = append(handles, ref.Name)
		if len(handles) == MaxRefsPerLoad {
			break
		}
	}
	return handles
}

func (s *Service) InboxUnread(ctx context.Context, viewer int64) (int, error) {
	return s.st.InboxUnread(ctx, viewer)
}

func (s *Service) InboxPage(ctx context.Context, viewer, threadID int64, page int) ([]InboxEntry, bool, error) {
	entries, err := s.st.InboxPage(ctx, viewer, threadID, max(0, page)*InboxPageSize, InboxPageSize+1)
	more := len(entries) > InboxPageSize
	return entries[:min(len(entries), InboxPageSize)], more, err
}

func (s *Service) ReadInboxPost(ctx context.Context, viewer, postID int64) error {
	if viewer == 0 {
		return ErrNoAccount
	}
	err := s.st.ReadInboxPost(ctx, viewer, postID, s.now().Unix())
	if err == nil {
		s.broker.Publish(Event{Kind: EventInbox, AccountID: viewer})
	}
	return err
}

func (s *Service) ThreadMuted(ctx context.Context, viewer, threadID int64) (bool, error) {
	return s.st.ThreadMuted(ctx, viewer, threadID)
}

func (s *Service) SetThreadMuted(ctx context.Context, viewer, threadID int64, on bool) error {
	if viewer == 0 {
		return ErrNoAccount
	}
	err := s.st.SetThreadMuted(ctx, viewer, threadID, on, s.now().Unix())
	if errors.Is(err, store.ErrNotFound) {
		return ErrNotFound
	}
	if err == nil {
		s.broker.Publish(Event{Kind: EventInbox, AccountID: viewer, ThreadID: threadID})
	}
	return err
}
