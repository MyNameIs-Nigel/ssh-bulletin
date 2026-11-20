package board

import (
	"context"
	"errors"

	"github.com/mynameis-nigel/ssh-bulletin/internal/store"
)

var ErrSelfMute = errors.New("you can't mute yourself")

func (s *Service) MutedIDs(ctx context.Context, viewer int64) (map[int64]bool, error) {
	return s.st.MutedIDs(ctx, viewer)
}

func (s *Service) SetMuted(ctx context.Context, viewer, target int64, on bool) error {
	if viewer == 0 {
		return ErrNoAccount
	}
	if viewer == target {
		return ErrSelfMute
	}
	err := s.st.SetMuted(ctx, viewer, target, on, s.now().Unix())
	if errors.Is(err, store.ErrNotFound) {
		return ErrNotFound
	}
	if err == nil {
		s.broker.Publish(Event{Kind: EventMute, AccountID: viewer})
	}
	return err
}
