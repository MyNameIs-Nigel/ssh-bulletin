package board

import (
	"context"
	"errors"
	"strings"
	"unicode/utf8"

	"github.com/mynameis-nigel/ssh-bulletin/internal/identity"
	"github.com/mynameis-nigel/ssh-bulletin/internal/store"
)

type (
	Role             = store.Role
	ModerationAction = store.ModerationAction
	ModerationView   = store.ModerationView
	ModerationEntry  = store.ModerationEntry
)

const (
	RoleOwner = store.RoleOwner
	RoleAdmin = store.RoleAdmin

	Reported  = store.Reported
	Dismissed = store.Dismissed
	Removed   = store.Removed
	Restored  = store.Restored

	QueueView   = store.QueueView
	HistoryView = store.HistoryView
	RemovedView = store.RemovedView

	MaxReasonLen       = 160
	ModerationPageSize = 20
)

var (
	ErrForbidden          = store.ErrForbidden
	ErrInvalidFingerprint = errors.New("enter the full SHA256 fingerprint from the player's profile")
	ErrKeyMismatch        = store.ErrKeyMismatch
	ErrOwnerRole          = store.ErrOwnerRole
	ErrReasonRequired     = errors.New("enter a reason")
	ErrAlreadyReported    = store.ErrAlreadyReported
	ErrReviewChanged      = store.ErrReviewChanged
	ErrPostLimit          = store.ErrPostLimit
	ErrNewcomerLimit      = store.ErrNewcomerLimit
	ErrReportLimit        = store.ErrReportLimit
)

// ConfigureOwner applies the operator's optional initial key reservation.
func (s *Service) ConfigureOwner(ctx context.Context, fp string) error {
	if !identity.ValidFingerprint(fp) {
		return ErrInvalidFingerprint
	}
	return s.st.ConfigureOwner(ctx, fp)
}

// Connect claims ownership if needed before resolving the session's account.
func (s *Service) Connect(ctx context.Context, fp string) (Account, bool, error) {
	if err := s.st.ClaimOwner(ctx, fp); err != nil {
		return Account{}, false, err
	}
	return s.Lookup(ctx, fp)
}

func (s *Service) SetAdmin(ctx context.Context, actorID int64, handle, fp string, grant bool) (Account, error) {
	handle, fp = strings.TrimSpace(handle), strings.TrimSpace(fp)
	if !ValidHandle(handle) {
		return Account{}, ErrHandleInvalid
	}
	if !identity.ValidFingerprint(fp) {
		return Account{}, ErrInvalidFingerprint
	}
	target, err := s.st.SetAdmin(ctx, actorID, handle, fp, grant)
	if errors.Is(err, store.ErrNotFound) {
		return Account{}, ErrNotFound
	}
	if err == nil {
		s.broker.Publish(Event{Kind: EventRole, AccountID: target.ID})
	}
	return target, err
}

func (s *Service) Admins(ctx context.Context, actorID int64, page int) ([]Account, error) {
	return s.st.Admins(ctx, actorID, page*ModerationPageSize, ModerationPageSize+1)
}

func (s *Service) ModerationPage(ctx context.Context, actorID int64, view ModerationView, page int) ([]ModerationEntry, error) {
	return s.st.ModerationPage(ctx, actorID, view, page*ModerationPageSize, ModerationPageSize+1)
}

func (s *Service) Report(ctx context.Context, actorID, postID int64, reason string) error {
	if actorID == 0 {
		return ErrNoAccount
	}
	reason, err := moderationReason(reason)
	if err != nil {
		return err
	}
	err = s.st.Report(ctx, actorID, postID, reason, s.now().Unix())
	if errors.Is(err, store.ErrNotFound) {
		return ErrNotFound
	}
	if err == nil {
		s.broker.Publish(Event{Kind: EventReview})
	}
	return err
}

func (s *Service) Moderate(ctx context.Context, actorID, postID int64, action ModerationAction, reason string) error {
	reason, err := moderationReason(reason)
	if err != nil {
		return err
	}
	threadID, err := s.st.Moderate(ctx, actorID, postID, action, reason, s.now().Unix())
	if errors.Is(err, store.ErrNotFound) {
		return ErrNotFound
	}
	if err == nil {
		kind := EventReview
		if action != Dismissed {
			kind = EventModeration
		}
		s.broker.Publish(Event{Kind: kind, ThreadID: threadID})
	}
	return err
}

func moderationReason(reason string) (string, error) {
	reason = CleanLine(reason)
	switch {
	case reason == "":
		return "", ErrReasonRequired
	case utf8.RuneCountInString(reason) > MaxReasonLen:
		return "", ErrTooLong
	default:
		return reason, nil
	}
}

// SetPinned pins a thread to the top of its board, or releases it. Only
// admins and the owner may; the store enforces that.
func (s *Service) SetPinned(ctx context.Context, actorID, threadID int64, pinned bool) error {
	boardID, err := s.st.SetPinned(ctx, actorID, threadID, pinned)
	if errors.Is(err, store.ErrNotFound) {
		return ErrNotFound
	}
	if err == nil {
		s.broker.Publish(Event{Kind: EventModeration, BoardID: boardID, ThreadID: threadID})
	}
	return err
}
