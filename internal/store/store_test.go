package store

import (
	"context"
	"path/filepath"
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
