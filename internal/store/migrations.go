package store

import (
	"context"
	"fmt"
)

// migrations are applied in order on startup. The schema version lives in
// SQLite's user_version pragma; each step runs in a transaction with the
// version bump. Steps are forward-only and never destructive.
var migrations = []string{
	// v1: accounts by key, boards, threads, posts, kudos.
	`CREATE TABLE accounts (
		id           INTEGER PRIMARY KEY,
		fingerprint  TEXT NOT NULL UNIQUE,
		public_key   TEXT NOT NULL DEFAULT '',
		handle       TEXT NOT NULL,
		handle_lower TEXT NOT NULL UNIQUE,
		palette      TEXT NOT NULL,
		bio          TEXT NOT NULL DEFAULT '',
		created_at   INTEGER NOT NULL,
		last_seen    INTEGER NOT NULL
	);
	CREATE TABLE boards (
		id      INTEGER PRIMARY KEY,
		slug    TEXT NOT NULL UNIQUE,
		title   TEXT NOT NULL,
		tagline TEXT NOT NULL,
		palette TEXT NOT NULL,
		sort    INTEGER NOT NULL
	);
	CREATE TABLE threads (
		id           INTEGER PRIMARY KEY,
		board_id     INTEGER NOT NULL REFERENCES boards(id),
		title        TEXT NOT NULL,
		author_id    INTEGER NOT NULL REFERENCES accounts(id),
		created_at   INTEGER NOT NULL,
		last_post_at INTEGER NOT NULL,
		pinned       INTEGER NOT NULL DEFAULT 0
	);
	CREATE INDEX threads_board_activity ON threads (board_id, pinned DESC, last_post_at DESC);
	CREATE TABLE posts (
		id         INTEGER PRIMARY KEY,
		thread_id  INTEGER NOT NULL REFERENCES threads(id),
		author_id  INTEGER NOT NULL REFERENCES accounts(id),
		body       TEXT NOT NULL,
		created_at INTEGER NOT NULL
	);
	CREATE INDEX posts_thread_order ON posts (thread_id, created_at, id);
	CREATE INDEX posts_author ON posts (author_id);
	CREATE TABLE kudos (
		post_id    INTEGER NOT NULL REFERENCES posts(id),
		account_id INTEGER NOT NULL REFERENCES accounts(id),
		PRIMARY KEY (post_id, account_id)
	) WITHOUT ROWID;
	INSERT INTO boards (slug, title, tagline, palette, sort) VALUES
		('lobby',     'The Lobby',      'Say hi. Introduce your key.',            'aurora', 10),
		('arcade',    'Arcade Talk',    'Farms, moons, gambits — strategy & brags', 'sunset', 20),
		('workshop',  'The Workshop',   'Terminals, dotfiles, things you built',   'reef',   30),
		('ideas',     'Idea Board',     'What should the arcade build next?',      'orchid', 40),
		('offtopic',  'Off the Grid',   'Everything else.',                        'ember',  50);`,
	// v2: roles, reports, audit log, and reversible removal.
	`CREATE TABLE owner (
		singleton INTEGER PRIMARY KEY CHECK (singleton = 1),
		fingerprint TEXT NOT NULL
	);
	CREATE TABLE admins (
		account_id INTEGER PRIMARY KEY REFERENCES accounts(id)
	);
	ALTER TABLE threads ADD COLUMN removed INTEGER NOT NULL DEFAULT 0;
	ALTER TABLE posts ADD COLUMN removed INTEGER NOT NULL DEFAULT 0;
	CREATE INDEX posts_author_time ON posts (author_id, created_at);
	CREATE INDEX posts_time ON posts (created_at);
	CREATE TABLE moderation_log (
		id INTEGER PRIMARY KEY,
		post_id INTEGER NOT NULL REFERENCES posts(id),
		actor_id INTEGER NOT NULL REFERENCES accounts(id),
		action TEXT NOT NULL CHECK (action IN ('reported', 'dismissed', 'removed', 'restored')),
		reason TEXT NOT NULL,
		created_at INTEGER NOT NULL,
		resolved INTEGER NOT NULL DEFAULT 0
	);
	CREATE UNIQUE INDEX moderation_one_report ON moderation_log (post_id, actor_id) WHERE action = 'reported';
	CREATE INDEX moderation_report_rate ON moderation_log (actor_id, created_at) WHERE action = 'reported';
	CREATE INDEX moderation_pending ON moderation_log (post_id, id) WHERE action = 'reported' AND resolved = 0;
	CREATE INDEX moderation_removals ON moderation_log (post_id, id) WHERE action = 'removed';`,
}

func (st *Store) migrate(ctx context.Context) error {
	var version int
	if err := st.db.QueryRowContext(ctx, `PRAGMA user_version`).Scan(&version); err != nil {
		return fmt.Errorf("store: read schema version: %w", err)
	}
	if version > len(migrations) {
		return fmt.Errorf("store: database schema v%d is newer than this binary (v%d)", version, len(migrations))
	}
	for i := version; i < len(migrations); i++ {
		tx, err := st.db.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, migrations[i]); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("store: migration v%d: %w", i+1, err)
		}
		if _, err := tx.ExecContext(ctx, fmt.Sprintf(`PRAGMA user_version = %d`, i+1)); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("store: bump schema v%d: %w", i+1, err)
		}
		if err := tx.Commit(); err != nil {
			return err
		}
	}
	return nil
}
