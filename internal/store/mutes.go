package store

import "context"

func (st *Store) MutedIDs(ctx context.Context, viewer int64) (map[int64]bool, error) {
	ids := make(map[int64]bool)
	rows, err := st.db.QueryContext(ctx, `SELECT muted_id FROM mutes WHERE viewer_id = ?`, viewer)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids[id] = true
	}
	return ids, rows.Err()
}

func (st *Store) SetMuted(ctx context.Context, viewer, target int64, on bool, now int64) error {
	tx, err := st.beginAccountWrite(ctx, viewer, now)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var exists bool
	if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM accounts WHERE id = ?)`, target).Scan(&exists); err != nil {
		return err
	}
	if !exists {
		return ErrNotFound
	}
	if on {
		_, err = tx.ExecContext(ctx, `INSERT INTO mutes (viewer_id, muted_id) VALUES (?, ?) ON CONFLICT DO NOTHING`, viewer, target)
	} else {
		_, err = tx.ExecContext(ctx, `DELETE FROM mutes WHERE viewer_id = ? AND muted_id = ?`, viewer, target)
	}
	if err != nil {
		return err
	}
	return tx.Commit()
}
