package store

import (
	"context"
	"database/sql"
	"errors"
)

type InboxReason uint8

const (
	ThreadReply InboxReason = 1 << iota
	DirectReply
	Mention
)

var ErrReplyTarget = errors.New("the post you are replying to is no longer available")

type InboxEntry struct {
	ThreadID, BoardID, PostID, ThreadAuthorID int64
	Title, Actor                              string
	Reasons                                   InboxReason
	Unread                                    int
}

func addInboxActivity(ctx context.Context, tx *sql.Tx, threadID, postID, actorID, replyTo int64, reply bool, mentions []string) error {
	if !reply && len(mentions) == 0 {
		return nil
	}
	sources := `SELECT author_id, ? FROM threads WHERE id = ? AND ?
		UNION ALL SELECT author_id, ? FROM posts WHERE id = ? AND removed = 0 AND deleted = 0`
	args := []any{ThreadReply, threadID, reply, DirectReply, replyTo}
	if len(mentions) > 0 {
		sources += ` UNION ALL SELECT id, ? FROM accounts WHERE handle_lower IN (` + placeholders(len(mentions)) + `)`
		args = append(args, Mention)
		for _, handle := range mentions {
			args = append(args, handle)
		}
	}
	_, err := tx.ExecContext(ctx, `WITH recipients(viewer, reason) AS (`+sources+`)
		INSERT INTO inbox (viewer_id, post_id, reasons)
		SELECT viewer, ?, SUM(DISTINCT reason) FROM recipients WHERE viewer != ?
		AND NOT EXISTS (SELECT 1 FROM mutes WHERE viewer_id = viewer AND muted_id = ?)
		AND NOT EXISTS (SELECT 1 FROM thread_mutes WHERE viewer_id = viewer AND thread_id = ?)
		GROUP BY viewer`, append(args, postID, actorID, actorID, threadID)...)
	return err
}

const inboxVisible = `FROM inbox i JOIN posts p ON p.id = i.post_id
	JOIN threads t ON t.id = p.thread_id JOIN accounts a ON a.id = p.author_id
	WHERE i.viewer_id = ? AND p.removed = 0 AND p.deleted = 0 AND t.removed = 0
	AND NOT EXISTS (SELECT 1 FROM mutes WHERE viewer_id = i.viewer_id AND muted_id = p.author_id)
	AND NOT EXISTS (SELECT 1 FROM thread_mutes WHERE viewer_id = i.viewer_id AND thread_id = t.id)`

func (st *Store) InboxUnread(ctx context.Context, viewer int64) (int, error) {
	var n int
	err := st.db.QueryRowContext(ctx, `SELECT COUNT(*) `+inboxVisible+` AND i.is_read = 0`, viewer).Scan(&n)
	return n, err
}

func (st *Store) InboxPage(ctx context.Context, viewer, threadID int64, offset, limit int) ([]InboxEntry, error) {
	const columns = `t.id, t.board_id, p.id, t.author_id, t.title, a.handle, i.reasons`
	q, args := `SELECT `+columns+`, NOT i.is_read `+inboxVisible+` AND t.id = ? ORDER BY p.id DESC LIMIT ? OFFSET ?`, []any{viewer, threadID, limit, offset}
	if threadID == 0 {
		q = `WITH grouped(thread_id, board_id, post_id, thread_author, title, actor, reasons, unread, row_number) AS (SELECT ` + columns + `,
			SUM(NOT i.is_read) OVER (PARTITION BY t.id),
			ROW_NUMBER() OVER (PARTITION BY t.id ORDER BY p.id DESC) ` + inboxVisible + `)
			SELECT thread_id, board_id, post_id, thread_author, title, actor, reasons, unread
			FROM grouped WHERE row_number = 1 ORDER BY post_id DESC LIMIT ? OFFSET ?`
		args = []any{viewer, limit, offset}
	}
	rows, err := st.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var entries []InboxEntry
	for rows.Next() {
		var e InboxEntry
		if err := rows.Scan(&e.ThreadID, &e.BoardID, &e.PostID, &e.ThreadAuthorID, &e.Title, &e.Actor, &e.Reasons, &e.Unread); err != nil {
			return nil, err
		}
		entries = append(entries, e)
	}
	return entries, rows.Err()
}

func (st *Store) ReadInboxPost(ctx context.Context, viewer, postID, now int64) error {
	tx, err := st.beginAccountWrite(ctx, viewer, now)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `UPDATE inbox SET is_read = 1 WHERE viewer_id = ? AND post_id = ?`, viewer, postID); err != nil {
		return err
	}
	return tx.Commit()
}

func (st *Store) ThreadMuted(ctx context.Context, viewer, threadID int64) (bool, error) {
	var muted bool
	err := st.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM thread_mutes WHERE viewer_id = ? AND thread_id = ?)`, viewer, threadID).Scan(&muted)
	return muted, err
}

func (st *Store) SetThreadMuted(ctx context.Context, viewer, threadID int64, on bool, now int64) error {
	tx, err := st.beginAccountWrite(ctx, viewer, now)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if on {
		var exists bool
		if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM threads WHERE id = ?)`, threadID).Scan(&exists); err != nil {
			return err
		}
		if !exists {
			return ErrNotFound
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO thread_mutes (viewer_id, thread_id) VALUES (?, ?) ON CONFLICT DO NOTHING`, viewer, threadID)
	} else {
		_, err = tx.ExecContext(ctx, `DELETE FROM thread_mutes WHERE viewer_id = ? AND thread_id = ?`, viewer, threadID)
	}
	if err != nil {
		return err
	}
	return tx.Commit()
}
