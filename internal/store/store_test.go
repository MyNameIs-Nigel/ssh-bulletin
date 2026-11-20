package store

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"strings"
	"testing"
)

func TestMigrationsAreIdempotentAndForwardOnly(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "s.db")
	st, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.CreateAccount(ctx, "SHA256:x", "", "Xy", "aurora", 1); err != nil {
		t.Fatal(err)
	}
	st.Close()
	st, err = Open(ctx, path) // reopening must not re-run v1 or lose data
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if _, err := st.AccountByFingerprint(ctx, "SHA256:x"); err != nil {
		t.Fatal(err)
	}
	if _, err := st.db.ExecContext(ctx, `PRAGMA user_version = 99`); err != nil {
		t.Fatal(err)
	}
	if err := st.migrate(ctx); err == nil {
		t.Fatal("a newer schema must refuse to open")
	}
}

func TestUpgradeChoosesEarliestExistingPlayer(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "legacy.db")
	// Construct a v1 file before exercising the normal migration path.
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	first := "SHA256:" + strings.Repeat("A", 43)
	later := "SHA256:" + strings.Repeat("B", 42) + "A"
	if _, err := db.ExecContext(ctx, migrations[0]); err != nil {
		db.Close()
		t.Fatal(err)
	}
	_, err = db.ExecContext(ctx, `PRAGMA user_version = 1;
		INSERT INTO accounts (fingerprint, handle, handle_lower, palette, created_at, last_seen) VALUES
		('SHA256:demo-0', 'Demo', 'demo', 'reef', 1, 1),
		(?, 'First', 'first', 'reef', 2, 2),
		(?, 'Later', 'later', 'reef', 2, 2);
		INSERT INTO threads (board_id, title, author_id, created_at, last_post_at) VALUES (1, 'Old thread', 2, 2, 2);
		INSERT INTO posts (thread_id, author_id, body, created_at) VALUES (1, 2, 'Old post', 2)`, first, later)
	db.Close()
	if err != nil {
		t.Fatal(err)
	}
	st, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.ClaimOwner(ctx, later); err != nil {
		st.Close()
		t.Fatal(err)
	}
	st.Close()
	st, err = Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	a, err := st.AccountByFingerprint(ctx, first)
	if err != nil || a.Role != RoleOwner || a.Flair != "" {
		t.Fatalf("earliest registered real player: %+v %v", a, err)
	}
	if err := st.ConfigureOwner(ctx, later); !errors.Is(err, ErrOwnerMismatch) {
		t.Fatalf("config replaced established owner: %v", err)
	}
	posts, err := st.Posts(ctx, 1, 0)
	if err != nil || len(posts) != 1 || posts[0].Body != "Old post" || posts[0].AuthorRole != RoleOwner || posts[0].Flair != "" {
		t.Fatalf("legacy content: %+v %v", posts, err)
	}
}

func TestUpgradePreservesReportsAndDecisions(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "moderation.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for _, migration := range migrations[:6] {
		if _, err := db.ExecContext(ctx, migration); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.ExecContext(ctx, `PRAGMA user_version = 6;
		INSERT INTO accounts (id, fingerprint, handle, handle_lower, palette, created_at, last_seen) VALUES
		(1, 'owner-key', 'Owner', 'owner', 'reef', 1, 1), (2, 'player-key', 'Player', 'player', 'reef', 2, 2);
		INSERT INTO owner (singleton, fingerprint) VALUES (1, 'owner-key');
		INSERT INTO threads (id, board_id, title, author_id, created_at, last_post_at) VALUES (1, 1, 'Thread', 1, 1, 2);
		INSERT INTO posts (id, thread_id, author_id, body, created_at, removed) VALUES (1, 1, 1, 'Opening', 1, 0), (2, 1, 2, 'Reply', 2, 1);
		INSERT INTO moderation_log (id, post_id, actor_id, action, reason, created_at, resolved) VALUES
		(5, 1, 2, 'reported', 'Pending', 3, 0), (6, 2, 1, 'removed', 'Decision', 4, 0);`); err != nil {
		t.Fatal(err)
	}
	db.Close()
	st, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	queue, err := st.ModerationPage(ctx, 1, QueueView, 0, 10)
	if err != nil || len(queue) != 1 || queue[0].ID != 5 || queue[0].Target != (ModerationTarget{PostTarget, 1}) || queue[0].Body != "Opening" {
		t.Fatalf("migrated queue: %+v %v", queue, err)
	}
	removed, err := st.ModerationPage(ctx, 1, RemovedView, 0, 10)
	if err != nil || len(removed) != 1 || removed[0].ID != 6 || removed[0].Target != (ModerationTarget{PostTarget, 2}) || removed[0].Reason != "Decision" {
		t.Fatalf("migrated decisions: %+v %v", removed, err)
	}
}

func TestSearchMigrationIndexesExistingVisiblePosts(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "archive.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for _, migration := range migrations[:10] {
		if _, err := db.ExecContext(ctx, migration); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.ExecContext(ctx, `PRAGMA user_version = 10;
		INSERT INTO accounts (id, fingerprint, handle, handle_lower, palette, created_at, last_seen) VALUES
		(1, 'player-key', 'Player', 'player', 'reef', 1, 1);
		INSERT INTO threads (id, board_id, title, author_id, created_at, last_post_at) VALUES (1, 1, 'Historic thread', 1, 1, 2);
		INSERT INTO posts (id, thread_id, author_id, body, created_at, removed) VALUES
		(1, 1, 1, 'Existing searchable body', 1, 0), (2, 1, 1, 'Withdrawn evidence', 2, 1);`); err != nil {
		t.Fatal(err)
	}
	db.Close()
	st, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	got, err := st.SearchThreads(ctx, 1, `"historic"`, 0, 10)
	if err != nil || len(got) != 1 || got[0].ThreadID != 1 || got[0].PostID != 1 {
		t.Fatalf("migrated search result: %+v %v", got, err)
	}
	got, err = st.SearchThreads(ctx, 1, `"withdrawn"`, 0, 10)
	if err != nil || len(got) != 0 {
		t.Fatalf("removed post indexed: %+v %v", got, err)
	}
}
