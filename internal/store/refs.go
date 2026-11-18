package store

import (
	"context"
	"database/sql"
	"errors"
	"strings"
)

// BoardRef is the identity of a board, for drawing a link to it.
type BoardRef struct {
	ID      int64
	Slug    string
	Title   string
	Palette string
}

// PostRef places a post within its thread.
type PostRef struct {
	ThreadID int64
	Position int // 1-based
	PostID   int64
}

// placeholders is "?, ?, ?" for n parameters.
func placeholders(n int) string {
	return strings.TrimSuffix(strings.Repeat("?, ", n), ", ")
}

// ProfilesByHandle loads accounts by lowercased handle, keyed the same way.
func (st *Store) ProfilesByHandle(ctx context.Context, lowers []string) (map[string]ProfileEntry, error) {
	out := map[string]ProfileEntry{}
	if len(lowers) == 0 {
		return out, nil
	}
	args := make([]any, len(lowers))
	for i, h := range lowers {
		args[i] = h
	}
	rows, err := st.db.QueryContext(ctx, `SELECT a.handle_lower, a.id, a.handle, a.palette, `+roleSQL+`
		FROM accounts a WHERE a.handle_lower IN (`+placeholders(len(lowers))+`)`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var key string
		var e ProfileEntry
		if err := rows.Scan(&key, &e.ID, &e.Handle, &e.Palette, &e.Role); err != nil {
			return nil, err
		}
		out[key] = e
	}
	return out, rows.Err()
}

// BoardRefs lists every board's identity, keyed by slug.
func (st *Store) BoardRefs(ctx context.Context) (map[string]BoardRef, error) {
	rows, err := st.db.QueryContext(ctx, `SELECT id, slug, title, palette FROM boards`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]BoardRef{}
	for rows.Next() {
		var b BoardRef
		if err := rows.Scan(&b.ID, &b.Slug, &b.Title, &b.Palette); err != nil {
			return nil, err
		}
		out[strings.ToLower(b.Slug)] = b
	}
	return out, rows.Err()
}

// ThreadsByID loads the threads that exist among ids.
func (st *Store) ThreadsByID(ctx context.Context, ids []int64) (map[int64]Thread, error) {
	out := map[int64]Thread{}
	if len(ids) == 0 {
		return out, nil
	}
	args := make([]any, len(ids))
	for i, id := range ids {
		args[i] = id
	}
	rows, err := st.db.QueryContext(ctx, threadSelect+` WHERE t.id IN (`+placeholders(len(ids))+`)`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		t, err := scanThread(rows)
		if err != nil {
			return nil, err
		}
		out[t.ID] = t
	}
	return out, rows.Err()
}

// PostAt finds the post at a 1-based position in a thread. Posts are never
// deleted outright (a deletion leaves a placeholder), so positions are stable.
func (st *Store) PostAt(ctx context.Context, threadID int64, position int) (int64, error) {
	var id int64
	err := st.db.QueryRowContext(ctx, `SELECT id FROM posts WHERE thread_id = ? ORDER BY id LIMIT 1 OFFSET ?`,
		threadID, position-1).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, ErrNotFound
	}
	return id, err
}
