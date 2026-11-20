package store

import (
	"context"
	"database/sql"
	"errors"
)

type DraftKey struct {
	BoardID, ThreadID int64 // zero ThreadID is a new thread in BoardID
}

type Draft struct {
	Title, Body       string
	ReplyTo, Revision int64
}

var ErrDraftChanged = errors.New("a newer version of this draft is saved. Reopen the composer to load it")

func (st *Store) Draft(ctx context.Context, actorID int64, key DraftKey) (Draft, error) {
	var draft Draft
	err := st.db.QueryRowContext(ctx, `SELECT title, body, reply_to, revision FROM drafts WHERE account_id = ? AND board_id = ? AND thread_id = ?`,
		actorID, key.BoardID, key.ThreadID).Scan(&draft.Title, &draft.Body, &draft.ReplyTo, &draft.Revision)
	if errors.Is(err, sql.ErrNoRows) {
		err = nil
	}
	return draft, err
}

func (st *Store) SaveDraft(ctx context.Context, actorID int64, key DraftKey, draft Draft, now int64) error {
	tx, err := st.beginAccountWrite(ctx, actorID, now)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var valid bool
	if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM boards WHERE id = ?)
		AND (? = 0 OR EXISTS(SELECT 1 FROM threads WHERE id = ? AND board_id = ?))`, key.BoardID, key.ThreadID, key.ThreadID, key.BoardID).Scan(&valid); err != nil {
		return err
	}
	if !valid {
		return ErrNotFound
	}
	if err := writeDraft(ctx, tx, actorID, key, draft); err != nil {
		return err
	}
	return tx.Commit()
}

// A cleared draft keeps its revision so an earlier autosave cannot restore it.
func writeDraft(ctx context.Context, tx *sql.Tx, actorID int64, key DraftKey, draft Draft) error {
	res, err := tx.ExecContext(ctx, `INSERT INTO drafts (account_id, board_id, thread_id, title, body, reply_to, revision) VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (account_id, board_id, thread_id) DO UPDATE SET
			title = excluded.title, body = excluded.body, reply_to = excluded.reply_to, revision = excluded.revision
		WHERE excluded.revision > drafts.revision`, actorID, key.BoardID, key.ThreadID, draft.Title, draft.Body, draft.ReplyTo, draft.Revision)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrDraftChanged
	}
	return nil
}

func clearPostedDraft(ctx context.Context, tx *sql.Tx, actorID int64, key DraftKey, revision int64) error {
	if revision == 0 {
		return nil
	}
	err := writeDraft(ctx, tx, actorID, key, Draft{Revision: revision})
	if errors.Is(err, ErrDraftChanged) {
		return nil // another session has a newer draft
	}
	return err
}
