package store

import (
	"context"
	"database/sql"
	"errors"
)

var (
	ErrPostLimit     = errors.New("posting limit reached (5 per minute). Try again shortly")
	ErrNewcomerLimit = errors.New("new accounts are posting quickly. Try again shortly")
	ErrReportLimit   = errors.New("report limit reached (5 per 10 minutes). Try again later")
)

// checkPostQuota uses the posting transaction, so sessions cannot race to
// consume the same allowance. Soft-removed posts still consume their slots.
func checkPostQuota(ctx context.Context, tx *sql.Tx, authorID, now int64) error {
	var count int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM posts WHERE author_id = ? AND created_at > ?`,
		authorID, now-60).Scan(&count); err != nil {
		return err
	}
	if count >= 5 {
		return ErrPostLimit
	}
	var created int64
	if err := tx.QueryRowContext(ctx, `SELECT created_at FROM accounts WHERE id = ?`, authorID).Scan(&created); err != nil {
		return err
	}
	const day = 24 * 60 * 60
	if created <= now-day {
		return nil
	}
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM posts p JOIN accounts a ON a.id = p.author_id
		WHERE p.created_at > ? AND a.created_at > ?`, now-60, now-day).Scan(&count); err != nil {
		return err
	}
	if count >= 20 {
		return ErrNewcomerLimit
	}
	return nil
}
