package store

import "context"

type SearchResult struct {
	ThreadID, PostID, BoardID, ThreadAuthorID int64
	Title, Board, Excerpt                     string
}

func (st *Store) SearchThreads(ctx context.Context, viewer int64, expression string, offset, limit int) ([]SearchResult, error) {
	rows, err := st.db.QueryContext(ctx, `WITH matched AS MATERIALIZED (
		SELECT rowid, bm25(forum_search, 4, 1) AS score, snippet(forum_search, 1, '', '', '…', 12) AS excerpt
		FROM forum_search WHERE forum_search MATCH ?
	), ranked AS (
		SELECT t.id AS thread_id, p.id AS post_id, t.board_id, t.author_id, t.title, b.title AS board_title, m.excerpt, m.score,
		ROW_NUMBER() OVER (PARTITION BY t.id ORDER BY m.score, p.id) AS rank
		FROM matched m JOIN posts p ON p.id = m.rowid JOIN threads t ON t.id = p.thread_id JOIN boards b ON b.id = t.board_id
		WHERE p.deleted = 0 AND p.removed = 0 AND t.removed = 0
		AND NOT EXISTS (SELECT 1 FROM mutes WHERE viewer_id = ? AND muted_id = p.author_id)
	) SELECT thread_id, post_id, board_id, author_id, title, board_title, excerpt FROM ranked WHERE rank = 1
	ORDER BY score, thread_id DESC LIMIT ? OFFSET ?`, expression, viewer, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var results []SearchResult
	for rows.Next() {
		var r SearchResult
		if err := rows.Scan(&r.ThreadID, &r.PostID, &r.BoardID, &r.ThreadAuthorID, &r.Title, &r.Board, &r.Excerpt); err != nil {
			return nil, err
		}
		results = append(results, r)
	}
	return results, rows.Err()
}
