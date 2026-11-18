// Package board holds the bulletin's rules: validation, text hygiene, and
// the live-event broker. It is the only thing the TUI talks to.
package board

import (
	"context"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/mynameis-nigel/ssh-bulletin/internal/store"
)

// Re-exported row types so the TUI does not import store directly.
type (
	Account = store.Account
	Board   = store.Board
	Thread  = store.Thread
	Post    = store.Post
	Stats   = store.Stats
)

// Service is shared by every session.
type Service struct {
	st     *store.Store
	broker *Broker
	now    func() time.Time
}

// NewService wires the store and broker.
func NewService(st *store.Store, broker *Broker, now func() time.Time) *Service {
	if now == nil {
		now = time.Now
	}
	return &Service{st: st, broker: broker, now: now}
}

// Broker exposes the live-event broker for subscriptions.
func (s *Service) Broker() *Broker { return s.broker }

// Lookup returns the account for a fingerprint; ok is false if none exists.
func (s *Service) Lookup(ctx context.Context, fp string) (Account, bool, error) {
	a, err := s.st.AccountByFingerprint(ctx, fp)
	if errors.Is(err, store.ErrNotFound) {
		return Account{}, false, nil
	}
	return a, err == nil, err
}

// Register creates the account for a key.
func (s *Service) Register(ctx context.Context, fp, publicKey, handle, palette string) (Account, error) {
	handle = strings.TrimSpace(handle)
	if !ValidHandle(handle) {
		return Account{}, ErrHandleInvalid
	}
	a, err := s.st.CreateAccount(ctx, fp, publicKey, handle, palette, s.now().Unix())
	if errors.Is(err, store.ErrAccountExists) || errors.Is(err, store.ErrHandleTaken) {
		// Another session for this key may have finished onboarding first.
		if existing, lookupErr := s.st.AccountByFingerprint(ctx, fp); lookupErr == nil {
			return existing, nil
		}
	}
	if errors.Is(err, store.ErrHandleTaken) {
		return Account{}, ErrHandleTaken
	}
	return a, err
}

// Touch records that an account connected.
func (s *Service) Touch(ctx context.Context, id int64, publicKey string) error {
	return s.st.Touch(ctx, id, publicKey, s.now().Unix())
}

// UpdateProfile changes handle, palette and bio.
func (s *Service) UpdateProfile(ctx context.Context, a Account, handle, palette, bio string) (Account, error) {
	handle = strings.TrimSpace(handle)
	if !ValidHandle(handle) {
		return a, ErrHandleInvalid
	}
	bio = CleanLine(bio)
	if utf8.RuneCountInString(bio) > MaxBioLen {
		return a, ErrTooLong
	}
	if err := s.st.UpdateProfile(ctx, a.ID, handle, palette, bio); err != nil {
		if errors.Is(err, store.ErrHandleTaken) {
			return a, ErrHandleTaken
		}
		return a, err
	}
	s.broker.Publish(Event{Kind: EventProfile, Actor: handle})
	return s.st.AccountByID(ctx, a.ID)
}

// Account reloads an account by id.
func (s *Service) Account(ctx context.Context, id int64) (Account, error) {
	return s.st.AccountByID(ctx, id)
}

// Boards lists boards.
func (s *Service) Boards(ctx context.Context) ([]Board, error) { return s.st.Boards(ctx) }

// Threads lists a board's threads.
func (s *Service) Threads(ctx context.Context, boardID int64) ([]Thread, error) {
	return s.st.Threads(ctx, boardID)
}

// Thread loads a thread.
func (s *Service) Thread(ctx context.Context, id int64) (Thread, error) {
	t, err := s.st.Thread(ctx, id)
	if errors.Is(err, store.ErrNotFound) {
		return t, ErrNotFound
	}
	return t, err
}

// Posts lists a thread's posts from the viewer's perspective.
func (s *Service) Posts(ctx context.Context, threadID, viewer int64) ([]Post, error) {
	return s.st.Posts(ctx, threadID, viewer)
}

// Stats returns masthead totals.
func (s *Service) Stats(ctx context.Context) (Stats, error) { return s.st.Stats(ctx) }

// CreateThread validates and starts a thread.
func (s *Service) CreateThread(ctx context.Context, author Account, boardID int64, title, body string) (int64, error) {
	if author.ID == 0 {
		return 0, ErrNoAccount
	}
	title = CleanLine(title)
	body = CleanBody(body)
	switch {
	case title == "":
		return 0, ErrEmptyTitle
	case body == "":
		return 0, ErrEmptyBody
	case utf8.RuneCountInString(title) > MaxTitleLen, !bodyFits(body):
		return 0, ErrTooLong
	}
	tid, pid, err := s.st.CreateThread(ctx, boardID, author.ID, title, body, s.now().Unix())
	if err != nil {
		return 0, err
	}
	s.broker.Publish(Event{Kind: EventThread, BoardID: boardID, ThreadID: tid, PostID: pid, Actor: author.Handle, Title: title})
	return tid, nil
}

// Reply validates and appends a post.
func (s *Service) Reply(ctx context.Context, author Account, t Thread, body string) (int64, error) {
	if author.ID == 0 {
		return 0, ErrNoAccount
	}
	body = CleanBody(body)
	switch {
	case body == "":
		return 0, ErrEmptyBody
	case !bodyFits(body):
		return 0, ErrTooLong
	}
	pid, err := s.st.Reply(ctx, t.ID, author.ID, body, s.now().Unix())
	if errors.Is(err, store.ErrNotFound) {
		return 0, ErrNotFound
	}
	if err != nil {
		return 0, err
	}
	s.broker.Publish(Event{Kind: EventReply, BoardID: t.BoardID, ThreadID: t.ID, PostID: pid, Actor: author.Handle, Title: t.Title})
	return pid, nil
}

// ToggleKudos flips the viewer's kudos on a post.
func (s *Service) ToggleKudos(ctx context.Context, viewer Account, p Post) (bool, error) {
	if viewer.ID == 0 {
		return false, ErrNoAccount
	}
	on, err := s.st.ToggleKudos(ctx, p.ID, viewer.ID)
	if errors.Is(err, store.ErrNotFound) {
		return false, ErrNotFound
	}
	if err == nil {
		s.broker.Publish(Event{Kind: EventKudos, ThreadID: p.ThreadID, PostID: p.ID, Actor: viewer.Handle})
	}
	return on, err
}

func bodyFits(body string) bool {
	return utf8.RuneCountInString(body) <= MaxBodyLen && strings.Count(body, "\n") < MaxBodyLines
}
