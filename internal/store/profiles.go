package store

import "context"

// ProfileEntry contains the identity fields shown in directory results.
type ProfileEntry struct {
	ID      int64
	Handle  string
	Palette string
	Role    Role
}

// SearchProfiles matches only usernames. Exact matches come first, followed
// by prefixes and then other substring matches.
func (st *Store) SearchProfiles(ctx context.Context, query string, viewer int64, friendsOnly bool, limit int) ([]ProfileEntry, error) {
	rows, err := st.db.QueryContext(ctx, `
		SELECT a.id, a.handle, a.palette, `+roleSQL+`
		FROM accounts a
		WHERE instr(a.handle_lower, ?1) > 0
		  AND (NOT ?2 OR EXISTS (SELECT 1 FROM friends f WHERE f.viewer_id = ?3 AND f.friend_id = a.id))
		ORDER BY (a.handle_lower = ?1) DESC, (instr(a.handle_lower, ?1) = 1) DESC, a.handle_lower
		LIMIT ?4`, query, friendsOnly, viewer, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var entries []ProfileEntry
	for rows.Next() {
		var entry ProfileEntry
		if err := rows.Scan(&entry.ID, &entry.Handle, &entry.Palette, &entry.Role); err != nil {
			return nil, err
		}
		entries = append(entries, entry)
	}
	return entries, rows.Err()
}

// FriendIDs returns the viewer's private list of saved accounts.
func (st *Store) FriendIDs(ctx context.Context, viewer int64) (map[int64]bool, error) {
	rows, err := st.db.QueryContext(ctx, `SELECT friend_id FROM friends WHERE viewer_id = ?`, viewer)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	ids := make(map[int64]bool)
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids[id] = true
	}
	return ids, rows.Err()
}

// SetFriend adds or removes a private bookmark for another account.
func (st *Store) SetFriend(ctx context.Context, viewer, target int64, on bool, now int64) error {
	tx, err := st.beginAccountWrite(ctx, viewer, now)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if on {
		_, err = tx.ExecContext(ctx, `INSERT INTO friends (viewer_id, friend_id) VALUES (?, ?)
			ON CONFLICT (viewer_id, friend_id) DO NOTHING`, viewer, target)
	} else {
		_, err = tx.ExecContext(ctx, `DELETE FROM friends WHERE viewer_id = ? AND friend_id = ?`, viewer, target)
	}
	if err != nil {
		return err
	}
	return tx.Commit()
}

// ProfilePost points to an account's public activity.
type ProfilePost struct {
	PostID    int64
	ThreadID  int64
	BoardID   int64
	Title     string
	Board     string
	CreatedAt int64
	IsReply   bool
}

// RecentProfilePosts lists the account's latest visible posts and replies.
func (st *Store) RecentProfilePosts(ctx context.Context, id int64) ([]ProfilePost, error) {
	rows, err := st.db.QueryContext(ctx, `
		SELECT p.id, t.id, b.id, CASE WHEN t.removed = 1 THEN '< removed >' ELSE t.title END, b.title, p.created_at,
		  EXISTS (SELECT 1 FROM posts first WHERE first.thread_id = t.id AND first.id < p.id)
		FROM posts p JOIN threads t ON t.id = p.thread_id JOIN boards b ON b.id = t.board_id
		WHERE p.author_id = ? AND p.removed = 0 AND p.deleted = 0
		ORDER BY p.created_at DESC, p.id DESC LIMIT 5`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var posts []ProfilePost
	for rows.Next() {
		var p ProfilePost
		if err := rows.Scan(&p.PostID, &p.ThreadID, &p.BoardID, &p.Title, &p.Board, &p.CreatedAt, &p.IsReply); err != nil {
			return nil, err
		}
		posts = append(posts, p)
	}
	return posts, rows.Err()
}
