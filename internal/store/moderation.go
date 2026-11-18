package store

import (
	"context"
	"database/sql"
	"errors"
)

type ModerationAction string

const (
	Reported  ModerationAction = "reported"
	Dismissed ModerationAction = "dismissed"
	Removed   ModerationAction = "removed"
	Restored  ModerationAction = "restored"
)

type ModerationView int

const (
	QueueView ModerationView = iota
	HistoryView
	RemovedView
)

type ModerationEntry struct {
	PostID    int64
	Title     string
	Author    string
	Body      string
	Opening   bool
	Actor     string
	Action    ModerationAction
	Reason    string
	CreatedAt int64
}

// MaxPinned is how many threads one board can keep pinned at a time.
const MaxPinned = 2

var (
	ErrPinLimit        = errors.New("that board already has 2 pinned threads. Unpin one first")
	ErrAlreadyReported = errors.New("you already reported this post")
	ErrReviewChanged   = errors.New("this post's moderation status changed. Refresh and try again")
)

type moderationTarget struct {
	threadID    int64
	opening     bool
	postRemoved bool
	postDeleted bool
	authorID    int64
}

func findModerationTarget(ctx context.Context, tx *sql.Tx, postID int64) (moderationTarget, error) {
	var p moderationTarget
	err := tx.QueryRowContext(ctx, `SELECT p.thread_id, p.id = (SELECT MIN(id) FROM posts WHERE thread_id = p.thread_id),
		p.removed, p.deleted, p.author_id FROM posts p WHERE p.id = ?`, postID).
		Scan(&p.threadID, &p.opening, &p.postRemoved, &p.postDeleted, &p.authorID)
	if errors.Is(err, sql.ErrNoRows) {
		return p, ErrNotFound
	}
	return p, err
}

// Report records a flag only while the post is publicly visible.
func (st *Store) Report(ctx context.Context, actorID, postID int64, reason string, now int64) error {
	tx, err := st.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	p, err := findModerationTarget(ctx, tx, postID)
	if err != nil {
		return err
	}
	if p.postRemoved || p.postDeleted {
		return ErrNotFound
	}

	var duplicate bool
	var recent int
	err = tx.QueryRowContext(ctx, `SELECT
		EXISTS(SELECT 1 FROM moderation_log WHERE action = 'reported' AND actor_id = ? AND post_id = ?),
		(SELECT COUNT(*) FROM moderation_log WHERE action = 'reported' AND actor_id = ? AND created_at > ?)`,
		actorID, postID, actorID, now-600).Scan(&duplicate, &recent)
	switch {
	case err != nil:
		return err
	case duplicate:
		return ErrAlreadyReported
	case recent >= 5:
		return ErrReportLimit
	}
	if err := logModeration(ctx, tx, actorID, postID, Reported, reason, now); err != nil {
		return err
	}
	return tx.Commit()
}

func logModeration(ctx context.Context, tx *sql.Tx, actorID, postID int64, action ModerationAction, reason string, now int64) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO moderation_log (post_id, actor_id, action, reason, created_at)
		VALUES (?, ?, ?, ?, ?)`, postID, actorID, action, reason, now)
	return err
}

// Moderate checks the actor's current role, applies the decision, and
// resolves any open reports as one transaction.
func (st *Store) Moderate(ctx context.Context, actorID, postID int64, action ModerationAction, reason string, now int64) (int64, error) {
	tx, err := st.db.BeginTx(ctx, nil)
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
	p, err := findModerationTarget(ctx, tx, postID)
	if err != nil {
		return 0, err
	}
	if action != Dismissed {
		if p.postDeleted || (action != Removed && action != Restored) {
			return 0, ErrReviewChanged
		}
		remove := action == Removed
		if p.postRemoved == remove {
			return 0, ErrReviewChanged
		}
		if _, err := tx.ExecContext(ctx, `UPDATE posts SET removed = ? WHERE id = ?`, remove, postID); err != nil {
			return 0, err
		}
		if p.opening {
			if _, err := tx.ExecContext(ctx, `UPDATE threads SET removed = ?, pinned = CASE WHEN ? THEN 0 ELSE pinned END
				WHERE id = ?`, remove, remove, p.threadID); err != nil {
				return 0, err
			}
		}
		if err := updateThreadActivity(ctx, tx, p.threadID); err != nil {
			return 0, err
		}
	}
	resolved, err := resolveReports(ctx, tx, postID)
	if err != nil {
		return 0, err
	}
	if n, _ := resolved.RowsAffected(); action == Dismissed && n == 0 {
		return 0, ErrReviewChanged
	}
	if err := logModeration(ctx, tx, actorID, postID, action, reason, now); err != nil {
		return 0, err
	}
	return p.threadID, tx.Commit()
}

// DeletePost clears an author's content while preserving its place in the thread.
func (st *Store) DeletePost(ctx context.Context, actorID, postID int64) (int64, error) {
	tx, err := st.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	p, err := findModerationTarget(ctx, tx, postID)
	if err != nil {
		return 0, err
	}
	if p.authorID != actorID {
		return 0, ErrForbidden
	}
	if p.postRemoved || p.postDeleted {
		return 0, ErrReviewChanged
	}
	if _, err := tx.ExecContext(ctx, `UPDATE posts SET deleted = 1, body = '', flair = '' WHERE id = ?`, postID); err != nil {
		return 0, err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM kudos WHERE post_id = ?`, postID); err != nil {
		return 0, err
	}
	if p.opening {
		if _, err := tx.ExecContext(ctx, `UPDATE threads SET title = '< deleted >', pinned = 0 WHERE id = ?`, p.threadID); err != nil {
			return 0, err
		}
	}
	if err := updateThreadActivity(ctx, tx, p.threadID); err != nil {
		return 0, err
	}
	if _, err := resolveReports(ctx, tx, postID); err != nil {
		return 0, err
	}
	return p.threadID, tx.Commit()
}

func updateThreadActivity(ctx context.Context, tx *sql.Tx, threadID int64) error {
	_, err := tx.ExecContext(ctx, `UPDATE threads SET last_post_at = COALESCE(
		(SELECT MAX(created_at) FROM posts WHERE thread_id = ? AND removed = 0 AND deleted = 0), created_at)
		WHERE id = ?`, threadID, threadID)
	return err
}

func resolveReports(ctx context.Context, tx *sql.Tx, postID int64) (sql.Result, error) {
	return tx.ExecContext(ctx, `UPDATE moderation_log SET resolved = 1
		WHERE post_id = ? AND action = 'reported' AND resolved = 0`, postID)
}

// ModerationPage reads private content only after checking the actor's role
// in the same transaction.
func (st *Store) ModerationPage(ctx context.Context, actorID int64, view ModerationView, offset, limit int) ([]ModerationEntry, error) {
	tx, err := st.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	role, err := accountRole(ctx, tx, actorID)
	if err != nil {
		return nil, err
	}
	if !role.CanModerate() {
		return nil, ErrForbidden
	}
	const selectLog = `SELECT p.id, t.title, a.handle, p.body,
		p.id = (SELECT MIN(id) FROM posts WHERE thread_id = p.thread_id),
		actor.handle, l.action, l.reason, l.created_at
		FROM moderation_log l
		JOIN posts p ON p.id = l.post_id JOIN threads t ON t.id = p.thread_id
		JOIN accounts a ON a.id = p.author_id JOIN accounts actor ON actor.id = l.actor_id`
	var filter string
	switch view {
	case QueueView:
		filter = `WHERE l.action = 'reported' AND l.resolved = 0 ORDER BY l.id`
	case HistoryView:
		filter = `ORDER BY l.id DESC`
	case RemovedView:
		filter = `WHERE p.removed = 1 AND l.action = 'removed' AND l.id =
			(SELECT MAX(id) FROM moderation_log WHERE post_id = p.id AND action = 'removed')
			ORDER BY l.id DESC`
	default:
		return nil, ErrNotFound
	}
	rows, err := tx.QueryContext(ctx, selectLog+" "+filter+` LIMIT ? OFFSET ?`, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var entries []ModerationEntry
	for rows.Next() {
		var entry ModerationEntry
		if err := rows.Scan(&entry.PostID, &entry.Title, &entry.Author, &entry.Body, &entry.Opening,
			&entry.Actor, &entry.Action, &entry.Reason, &entry.CreatedAt); err != nil {
			return nil, err
		}
		entries = append(entries, entry)
	}
	return entries, rows.Err()
}

// SetPinned sets a thread's sticky state. The actor's role is checked in the
// same transaction as the update, and the target state is explicit rather
// than a toggle, so a double press or two admins racing cannot undo each
// other. It returns the thread's board for event routing.
func (st *Store) SetPinned(ctx context.Context, actorID, threadID int64, pinned bool) (int64, error) {
	tx, err := st.db.BeginTx(ctx, nil)
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
	err = tx.QueryRowContext(ctx, `SELECT t.board_id FROM threads t JOIN posts opening
		ON opening.id = (SELECT MIN(p.id) FROM posts p WHERE p.thread_id = t.id)
		WHERE t.id = ? AND t.removed = 0 AND opening.deleted = 0`, threadID).Scan(&boardID)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, ErrNotFound
	}
	if err != nil {
		return 0, err
	}
	if pinned {
		// Other threads only: pinning an already pinned thread stays harmless.
		var others int
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM threads
			WHERE board_id = ? AND pinned = 1 AND removed = 0 AND id != ?`, boardID, threadID).Scan(&others); err != nil {
			return 0, err
		}
		if others >= MaxPinned {
			return 0, ErrPinLimit
		}
	}
	if _, err := tx.ExecContext(ctx, `UPDATE threads SET pinned = ? WHERE id = ?`, pinned, threadID); err != nil {
		return 0, err
	}
	return boardID, tx.Commit()
}
