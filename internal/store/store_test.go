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
	if err != nil || a.Role != RoleOwner {
		t.Fatalf("earliest registered real player: %+v %v", a, err)
	}
	if err := st.ConfigureOwner(ctx, later); !errors.Is(err, ErrOwnerMismatch) {
		t.Fatalf("config replaced established owner: %v", err)
	}
	posts, err := st.Posts(ctx, 1, 0)
	if err != nil || len(posts) != 1 || posts[0].Body != "Old post" || posts[0].AuthorRole != RoleOwner {
		t.Fatalf("legacy content: %+v %v", posts, err)
	}
}
