package store

import (
	"context"
	"database/sql"
	"errors"
)

func (st *Store) SetThreadLock(ctx context.Context, actorID, threadID int64, locked bool, reason string, now int64) (int64, error) {
	tx, err := st.beginAccountWrite(ctx, actorID, now)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	role, err := accountRole(ctx, tx, actorID)
	if err != nil {
		return 0, err
	}
	if !role.CanModerate() {
		return 0, ErrForbidden
	}
	var boardID int64
	var e moderationEvidence
	err = tx.QueryRowContext(ctx, `SELECT t.board_id, t.title, a.handle, t.lock_reason FROM threads t
		JOIN accounts a ON a.id = t.author_id WHERE t.id = ? AND t.removed = 0`, threadID).
		Scan(&boardID, &e.title, &e.author, &e.body)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, ErrNotFound
	}
	if err != nil {
		return 0, err
	}
	if (e.body != "") == locked {
		return boardID, ErrReviewChanged
	}
	action, next := ThreadUnlocked, ""
	if locked {
		action, next = ThreadLocked, reason
	}
	if _, err := tx.ExecContext(ctx, `UPDATE threads SET lock_reason = ? WHERE id = ?`, next, threadID); err != nil {
		return 0, err
	}
	if _, err := logModeration(ctx, tx, actorID, ModerationTarget{Kind: ThreadTarget, ID: threadID}, action, reason, e, now, 0); err != nil {
		return 0, err
	}
	return boardID, tx.Commit()
}
