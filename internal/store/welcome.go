package store

import (
	"context"
	"database/sql"
	"errors"
	"strconv"
	"strings"
)

// SeedWelcome creates a real, replyable Lobby thread under a system identity.
// The marker stays with the original thread even if staff unpin it or remove it.
func (st *Store) SeedWelcome(ctx context.Context, title, body string, now int64) error {
	tx, err := st.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var exists bool
	if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM threads WHERE welcome = 1)`).Scan(&exists); err != nil || exists {
		return err
	}
	const fingerprint = "system:bulletin"
	var author int64
	for suffix := 0; ; suffix++ {
		handle := "Bulletin"
		if suffix > 0 {
			handle += "-" + strconv.Itoa(suffix)
		}
		err := tx.QueryRowContext(ctx, `INSERT INTO accounts (fingerprint, handle, handle_lower, palette, created_at, last_seen)
			VALUES (?, ?, ?, 'aurora', ?, ?) ON CONFLICT (handle_lower) DO NOTHING RETURNING id`,
			fingerprint, handle, strings.ToLower(handle), now, now).Scan(&author)
		if errors.Is(err, sql.ErrNoRows) {
			continue
		}
		if err != nil {
			return err
		}
		break
	}
	var threadID int64
	if err := tx.QueryRowContext(ctx, `INSERT INTO threads (board_id, title, author_id, created_at, last_post_at, pinned, welcome)
		SELECT id, ?, ?, ?, ?, 1, 1 FROM boards WHERE slug = 'lobby' RETURNING id`, title, author, now, now).Scan(&threadID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, insertPost, threadID, author, body, now, author); err != nil {
		return err
	}
	return tx.Commit()
}
