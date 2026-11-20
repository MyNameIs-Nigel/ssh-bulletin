package store

import (
	"context"
	"database/sql"
	"errors"
)

const Edited ModerationAction = "edited"

var ErrEditChanged = errors.New("this post changed while you were editing. Reopen the editor and try again")
var ErrEditTitleRequired = errors.New("a thread needs a title")

func (st *Store) EditPost(ctx context.Context, authorID, postID, revision int64, title, body string, now int64) (boardID, threadID int64, changed bool, err error) {
	tx, err := st.beginAccountWrite(ctx, authorID, now)
	if err != nil {
		return 0, 0, false, err
	}
	defer tx.Rollback()
	var previous moderationEvidence
	var owner, currentRevision int64
	var opening bool
	err = tx.QueryRowContext(ctx, `SELECT t.board_id, t.id, p.author_id, p.revision,
		p.id = (SELECT MIN(id) FROM posts WHERE thread_id = p.thread_id), t.title, a.handle, p.body
		FROM posts p JOIN threads t ON t.id = p.thread_id JOIN accounts a ON a.id = p.author_id
		WHERE p.id = ? AND p.deleted = 0 AND p.removed = 0 AND t.removed = 0`, postID).
		Scan(&boardID, &threadID, &owner, &currentRevision, &opening, &previous.title, &previous.author, &previous.body)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, 0, false, ErrNotFound
	}
	if err != nil {
		return 0, 0, false, err
	}
	if owner != authorID {
		return 0, 0, false, ErrForbidden
	}
	if currentRevision != revision {
		return 0, 0, false, ErrEditChanged
	}
	if opening && title == "" {
		return 0, 0, false, ErrEditTitleRequired
	}
	if !opening {
		title = previous.title
	}
	if previous.body == body && previous.title == title {
		return boardID, threadID, false, nil
	}
	if _, err := tx.ExecContext(ctx, `UPDATE posts SET body = ?, edited_at = ?, revision = revision + 1 WHERE id = ?`, body, now, postID); err != nil {
		return 0, 0, false, err
	}
	if previous.title != title {
		if _, err := tx.ExecContext(ctx, `UPDATE threads SET title = ? WHERE id = ?`, title, threadID); err != nil {
			return 0, 0, false, err
		}
	}
	if _, err := logModeration(ctx, tx, authorID, ModerationTarget{Kind: PostTarget, ID: postID}, Edited, "Previous version of post", previous, now, 0); err != nil {
		return 0, 0, false, err
	}
	return boardID, threadID, true, tx.Commit()
}
