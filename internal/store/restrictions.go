package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

var ErrHandleChangeRequired = errors.New("staff requested a new handle. Edit your profile to choose one before posting or sending messages")

type AccountRestriction struct {
	Kind      ModerationAction
	Reason    string
	ExpiresAt int64
}

func (r *AccountRestriction) Error() string {
	return fmt.Sprintf("Account %s until %s UTC. %s. Use Contact staff in the jump menu to request review.",
		r.Kind, time.Unix(r.ExpiresAt, 0).UTC().Format("Jan 2, 2006 15:04"), r.Reason)
}

type restrictionQuery interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func accountRestriction(ctx context.Context, q restrictionQuery, accountID, now int64) (*AccountRestriction, error) {
	var r AccountRestriction
	err := q.QueryRowContext(ctx, `SELECT l.action, l.reason, r.expires_at
		FROM account_restrictions r JOIN moderation_log l ON l.id = r.log_id
		WHERE r.account_id = ? AND r.expires_at > ?`, accountID, now).Scan(&r.Kind, &r.Reason, &r.ExpiresAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &r, nil
}

func (st *Store) AccountRestriction(ctx context.Context, accountID, now int64) (*AccountRestriction, error) {
	return accountRestriction(ctx, st.db, accountID, now)
}

// Actor checks use the same transaction as each player-initiated write.
func checkAccountWrite(ctx context.Context, tx *sql.Tx, actorID, now int64, changingProfile bool) error {
	r, err := accountRestriction(ctx, tx, actorID, now)
	if err != nil {
		return err
	}
	if r != nil {
		return r
	}
	if changingProfile {
		return nil
	}
	var required bool
	if err := tx.QueryRowContext(ctx, `SELECT handle_change_required FROM accounts WHERE id = ?`, actorID).Scan(&required); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrForbidden
		}
		return err
	}
	if required {
		return ErrHandleChangeRequired
	}
	return nil
}

func (st *Store) beginAccountWrite(ctx context.Context, actorID, now int64) (*sql.Tx, error) {
	tx, err := st.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	if err := checkAccountWrite(ctx, tx, actorID, now, false); err != nil {
		tx.Rollback()
		return nil, err
	}
	return tx, nil
}

func (st *Store) SetRestriction(ctx context.Context, actorID, targetID int64, action ModerationAction, reason string, now, expiresAt int64) error {
	tx, err := st.beginAccountWrite(ctx, actorID, now)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := checkProfileStaff(ctx, tx, actorID, targetID); err != nil {
		return err
	}
	target := ModerationTarget{Kind: ProfileTarget, ID: targetID}
	evidence, err := captureEvidence(ctx, tx, target, false)
	if err != nil {
		return err
	}
	current, err := accountRestriction(ctx, tx, targetID, now)
	if err != nil {
		return err
	}
	if action == RestrictionLifted {
		if current == nil {
			return ErrReviewChanged
		}
		expiresAt = current.ExpiresAt
	} else if (action != TimedOut && action != Suspended) || expiresAt <= now {
		return ErrReviewChanged
	}
	logID, err := logModeration(ctx, tx, actorID, target, action, reason, evidence, now, expiresAt)
	if err != nil {
		return err
	}
	if action == RestrictionLifted {
		_, err = tx.ExecContext(ctx, `DELETE FROM account_restrictions WHERE account_id = ?`, targetID)
	} else {
		_, err = tx.ExecContext(ctx, `INSERT INTO account_restrictions (account_id, log_id, expires_at) VALUES (?, ?, ?)
			ON CONFLICT (account_id) DO UPDATE SET log_id = excluded.log_id, expires_at = excluded.expires_at`, targetID, logID, expiresAt)
	}
	if err != nil {
		return err
	}
	return tx.Commit()
}
