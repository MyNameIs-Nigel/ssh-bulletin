// Package store is the SQLite persistence layer and owns every SQL statement
// in the project.
//
// An account is one SSH key fingerprint. Handles are unique ignoring case so
// "Nigel" cannot pose as "nigel". The pool is capped at a single connection,
// so this process never issues concurrent writes; litestream replicates the
// file (see ../../entrypoint.sh).
package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	_ "modernc.org/sqlite" // pure-Go, cgo-free driver (static builds)
)

var (
	// ErrNotFound is returned when a row does not exist.
	ErrNotFound = errors.New("store: not found")
	// ErrHandleTaken is returned when a handle collides case-insensitively.
	ErrHandleTaken = errors.New("store: handle taken")
	// ErrAccountExists is returned when the fingerprint already has an account.
	ErrAccountExists = errors.New("store: account exists")
)

// Store is the open database handle.
type Store struct{ db *sql.DB }

// Account is one poster, keyed by key fingerprint.
type Account struct {
	ID                   int64
	Fingerprint          string
	PublicKey            string // authorized_keys format, as last seen
	Handle               string
	Palette              string
	Bio                  string
	Flair                string
	CreatedAt            int64
	LastSeen             int64
	PostCount            int64
	Role                 Role
	HandleChangeRequired bool
}

// Board is a top-level topic.
type Board struct {
	ID          int64
	Slug        string
	Title       string
	Tagline     string
	Palette     string
	ThreadCount int64
	PostCount   int64
	LastPostAt  int64
}

// Thread is a titled conversation inside a board.
type Thread struct {
	ID                 int64
	BoardID            int64
	Title              string
	AuthorID           int64
	AuthorHandle       string
	AuthorPalette      string
	AuthorRole         Role
	Flair              string // flair on the opening post
	CreatedAt          int64
	LastPostAt         int64
	ReplyCount         int64
	Pinned             bool
	Removed            bool // staff removed the opening post and closed replies
	OpeningUnavailable bool
	LockReason         string
}

func (t Thread) Locked() bool { return t.LockReason != "" }

// Post is one message in a thread.
type Post struct {
	ID            int64
	ThreadID      int64
	AuthorID      int64
	AuthorHandle  string
	AuthorPalette string
	AuthorRole    Role
	AuthorKey     string // author's fingerprint; seeds their sigil, never displayed
	Flair         string
	Body          string
	CreatedAt     int64
	Kudos         int64
	KudosByViewer bool
	Removed       bool
	Deleted       bool
	EditedAt      int64
	Revision      int64
}

// Placeholder describes a post whose original body is unavailable.
func (p Post) Placeholder() string {
	switch {
	case p.Deleted:
		return "< deleted >"
	case p.Removed:
		return "< removed >"
	default:
		return ""
	}
}

// Stats are fleet-wide counters for the masthead.
type Stats struct {
	Accounts int64
	Threads  int64
	Posts    int64
}

// Open opens (creating if needed) the database and applies migrations.
func Open(ctx context.Context, path string) (*Store, error) {
	if dir := filepath.Dir(path); dir != "." && dir != "" {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return nil, fmt.Errorf("store: create db dir: %w", err)
		}
	}
	dsn := "file:" + url.PathEscape(path) +
		"?_pragma=busy_timeout(5000)" +
		"&_pragma=journal_mode(WAL)" +
		"&_pragma=synchronous(NORMAL)" +
		"&_pragma=foreign_keys(1)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("store: open: %w", err)
	}
	db.SetMaxOpenConns(1)
	st := &Store{db: db}
	if err := st.migrate(ctx); err != nil {
		_ = db.Close()
		return nil, err
	}
	_ = os.Chmod(path, 0o600)
	return st, nil
}

// Close closes the database.
func (st *Store) Close() error { return st.db.Close() }

// AccountByFingerprint loads the account for a key, or ErrNotFound.
func (st *Store) AccountByFingerprint(ctx context.Context, fp string) (Account, error) {
	return st.scanAccount(st.db.QueryRowContext(ctx, accountSelect+` WHERE a.fingerprint = ?`, fp))
}

// AccountByID loads an account by id.
func (st *Store) AccountByID(ctx context.Context, id int64) (Account, error) {
	return st.scanAccount(st.db.QueryRowContext(ctx, accountSelect+` WHERE a.id = ?`, id))
}

const accountSelect = `SELECT a.id, a.fingerprint, a.public_key, a.handle, a.palette, a.bio, a.flair, a.created_at, a.last_seen,
	(SELECT COUNT(*) FROM posts p
		WHERE p.author_id = a.id AND p.removed = 0 AND p.deleted = 0),
	` + roleSQL + `, a.handle_change_required FROM accounts a`

func (st *Store) scanAccount(row interface{ Scan(...any) error }) (Account, error) {
	var a Account
	err := row.Scan(&a.ID, &a.Fingerprint, &a.PublicKey, &a.Handle, &a.Palette, &a.Bio, &a.Flair, &a.CreatedAt, &a.LastSeen, &a.PostCount, &a.Role, &a.HandleChangeRequired)
	if errors.Is(err, sql.ErrNoRows) {
		return Account{}, ErrNotFound
	}
	return a, err
}

// CreateAccount registers a key under a handle.
func (st *Store) CreateAccount(ctx context.Context, fp, publicKey, handle, palette string, now int64) (Account, error) {
	res, err := st.db.ExecContext(ctx,
		`INSERT INTO accounts (fingerprint, public_key, handle, handle_lower, palette, bio, created_at, last_seen)
		 VALUES (?, ?, ?, ?, ?, '', ?, ?)`,
		fp, publicKey, handle, strings.ToLower(handle), palette, now, now)
	if err != nil {
		return Account{}, uniqueErr(err)
	}
	id, _ := res.LastInsertId()
	return st.AccountByID(ctx, id)
}

// UpdateProfile changes an account's handle, palette, bio and post flair.
func (st *Store) UpdateProfile(ctx context.Context, id int64, handle, palette, bio, flair string, now int64) error {
	tx, err := st.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := checkAccountWrite(ctx, tx, id, now, true); err != nil {
		return err
	}
	lower := strings.ToLower(handle)
	res, err := tx.ExecContext(ctx,
		`UPDATE accounts SET handle = ?, handle_lower = ?, palette = ?, bio = ?, flair = ?, handle_change_required = 0
		WHERE id = ? AND (handle_change_required = 0 OR handle_lower != ?)`,
		handle, lower, palette, bio, flair, id, lower)
	if err != nil {
		return uniqueErr(err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrHandleChangeRequired
	}
	return tx.Commit()
}

// Touch records activity and the latest known public key.
func (st *Store) Touch(ctx context.Context, id int64, publicKey string, now int64) error {
	_, err := st.db.ExecContext(ctx,
		`UPDATE accounts SET last_seen = ?, public_key = CASE WHEN ? = '' THEN public_key ELSE ? END WHERE id = ?`,
		now, publicKey, publicKey, id)
	return err
}

func uniqueErr(err error) error {
	if err == nil {
		return nil
	}
	msg := err.Error()
	switch {
	case strings.Contains(msg, "accounts.handle_lower"):
		return ErrHandleTaken
	case strings.Contains(msg, "accounts.fingerprint"):
		return ErrAccountExists
	}
	return err
}

// Boards lists every board with activity counters, in display order.
func (st *Store) Boards(ctx context.Context) ([]Board, error) {
	rows, err := st.db.QueryContext(ctx, `
		SELECT b.id, b.slug, b.title, b.tagline, b.palette,
		  (SELECT COUNT(*) FROM threads t WHERE t.board_id = b.id),
		  (SELECT COUNT(*) FROM posts p JOIN threads t ON t.id = p.thread_id WHERE t.board_id = b.id AND p.removed = 0 AND p.deleted = 0),
		  COALESCE((SELECT MAX(t.last_post_at) FROM threads t WHERE t.board_id = b.id), 0)
		FROM boards b ORDER BY b.sort, b.id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Board
	for rows.Next() {
		var b Board
		if err := rows.Scan(&b.ID, &b.Slug, &b.Title, &b.Tagline, &b.Palette, &b.ThreadCount, &b.PostCount, &b.LastPostAt); err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

const threadSelect = `SELECT t.id, t.board_id,
	CASE WHEN t.removed = 1 THEN '< removed >' ELSE t.title END,
	t.author_id, a.handle, a.palette, t.created_at, t.last_post_at,
	(SELECT COUNT(*) FROM posts p WHERE p.thread_id = t.id AND p.id > opening.id AND p.removed = 0 AND p.deleted = 0),
	t.pinned, CASE WHEN t.removed = 0 THEN opening.flair ELSE '' END, ` + roleSQL + `, t.removed, (t.removed = 1 OR opening.deleted = 1), t.lock_reason
	FROM threads t JOIN accounts a ON a.id = t.author_id
	JOIN posts opening ON opening.id = (SELECT MIN(p.id) FROM posts p WHERE p.thread_id = t.id)`

func scanThread(sc interface{ Scan(...any) error }) (Thread, error) {
	var t Thread
	err := sc.Scan(&t.ID, &t.BoardID, &t.Title, &t.AuthorID, &t.AuthorHandle, &t.AuthorPalette,
		&t.CreatedAt, &t.LastPostAt, &t.ReplyCount, &t.Pinned, &t.Flair, &t.AuthorRole, &t.Removed, &t.OpeningUnavailable, &t.LockReason)
	return t, err
}

// Threads lists a board's threads, pinned first then most recently active.
func (st *Store) Threads(ctx context.Context, boardID int64) ([]Thread, error) {
	rows, err := st.db.QueryContext(ctx, threadSelect+` WHERE t.board_id = ? ORDER BY t.pinned DESC, (t.pinned AND t.welcome) DESC, t.last_post_at DESC, t.id DESC`, boardID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Thread
	for rows.Next() {
		t, err := scanThread(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// Thread loads one thread.
func (st *Store) Thread(ctx context.Context, id int64) (Thread, error) {
	t, err := scanThread(st.db.QueryRowContext(ctx, threadSelect+` WHERE t.id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return Thread{}, ErrNotFound
	}
	return t, err
}

// Posts lists a thread's posts in order; viewer marks the viewer's kudos.
func (st *Store) Posts(ctx context.Context, threadID, viewer int64) ([]Post, error) {
	rows, err := st.db.QueryContext(ctx, `
		SELECT p.id, p.thread_id, p.author_id, a.handle, a.palette, a.fingerprint, CASE WHEN p.removed = 0 THEN p.flair ELSE '' END,
		  CASE WHEN p.removed = 0 THEN p.body ELSE '' END, p.created_at, p.removed, p.deleted,
		  (SELECT COUNT(*) FROM kudos k WHERE k.post_id = p.id),
		  EXISTS(SELECT 1 FROM kudos k WHERE k.post_id = p.id AND k.account_id = ?), `+roleSQL+`, p.edited_at, p.revision
		FROM posts p JOIN accounts a ON a.id = p.author_id
		WHERE p.thread_id = ? ORDER BY p.id`, viewer, threadID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Post
	for rows.Next() {
		var p Post
		if err := rows.Scan(&p.ID, &p.ThreadID, &p.AuthorID, &p.AuthorHandle, &p.AuthorPalette, &p.AuthorKey, &p.Flair, &p.Body,
			&p.CreatedAt, &p.Removed, &p.Deleted, &p.Kudos, &p.KudosByViewer, &p.AuthorRole, &p.EditedAt, &p.Revision); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// CreateThread inserts a thread and its opening post in one transaction.
func (st *Store) CreateThread(ctx context.Context, boardID, authorID int64, title, body string, now int64, mentions []string, draftRevision int64) (int64, int64, error) {
	tx, err := st.beginAccountWrite(ctx, authorID, now)
	if err != nil {
		return 0, 0, err
	}
	defer func() { _ = tx.Rollback() }()
	if err := checkPostQuota(ctx, tx, authorID, now); err != nil {
		return 0, 0, err
	}
	res, err := tx.ExecContext(ctx,
		`INSERT INTO threads (board_id, title, author_id, created_at, last_post_at, pinned) VALUES (?, ?, ?, ?, ?, 0)`,
		boardID, title, authorID, now, now)
	if err != nil {
		return 0, 0, err
	}
	tid, _ := res.LastInsertId()
	res, err = tx.ExecContext(ctx, insertPost, tid, authorID, body, now, authorID)
	if err != nil {
		return 0, 0, err
	}
	pid, _ := res.LastInsertId()
	if err := addInboxActivity(ctx, tx, tid, pid, authorID, 0, false, mentions); err != nil {
		return 0, 0, err
	}
	if err := clearPostedDraft(ctx, tx, authorID, DraftKey{BoardID: boardID}, draftRevision); err != nil {
		return 0, 0, err
	}
	return tid, pid, tx.Commit()
}

// Read the current flair in the posting transaction so concurrent sessions use
// the saved preference even if their local account data is older.
const insertPost = `INSERT INTO posts (thread_id, author_id, body, created_at, flair)
	VALUES (?, ?, ?, ?, (SELECT flair FROM accounts WHERE id = ?))`

// Reply appends a post, bumps the thread activity, and returns the ID and current thread title.
func (st *Store) Reply(ctx context.Context, threadID, authorID, replyTo int64, body string, now int64, mentions []string, draftRevision int64) (int64, string, error) {
	tx, err := st.beginAccountWrite(ctx, authorID, now)
	if err != nil {
		return 0, "", err
	}
	defer func() { _ = tx.Rollback() }()
	var title string
	var boardID int64
	err = tx.QueryRowContext(ctx, `UPDATE threads SET last_post_at = max(last_post_at, ?)
		WHERE id = ? AND removed = 0 AND lock_reason = '' RETURNING title, board_id`, now, threadID).Scan(&title, &boardID)
	if errors.Is(err, sql.ErrNoRows) {
		var locked bool
		if lookupErr := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM threads WHERE id = ? AND removed = 0 AND lock_reason != '')`, threadID).Scan(&locked); lookupErr != nil {
			return 0, "", lookupErr
		}
		if locked {
			return 0, "", ErrThreadLocked
		}
		return 0, "", ErrNotFound
	}
	if err != nil {
		return 0, "", err
	}
	if err := checkPostQuota(ctx, tx, authorID, now); err != nil {
		return 0, "", err
	}
	if replyTo != 0 {
		var exists bool
		if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM posts
			WHERE id = ? AND thread_id = ?)`, replyTo, threadID).Scan(&exists); err != nil {
			return 0, "", err
		}
		if !exists {
			return 0, "", ErrReplyTarget
		}
	}
	res, err := tx.ExecContext(ctx, insertPost, threadID, authorID, body, now, authorID)
	if err != nil {
		return 0, "", err
	}
	pid, _ := res.LastInsertId()
	if err := addInboxActivity(ctx, tx, threadID, pid, authorID, replyTo, true, mentions); err != nil {
		return 0, "", err
	}
	if err := clearPostedDraft(ctx, tx, authorID, DraftKey{BoardID: boardID, ThreadID: threadID}, draftRevision); err != nil {
		return 0, "", err
	}
	return pid, title, tx.Commit()
}

// ToggleKudos flips the viewer's kudos on a post and returns the new state.
func (st *Store) ToggleKudos(ctx context.Context, postID, accountID, now int64) (bool, error) {
	tx, err := st.beginAccountWrite(ctx, accountID, now)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	var exists bool
	if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM posts p
		WHERE p.id = ? AND p.removed = 0 AND p.deleted = 0)`, postID).Scan(&exists); err != nil {
		return false, err
	}
	if !exists {
		return false, ErrNotFound
	}
	res, err := tx.ExecContext(ctx, `DELETE FROM kudos WHERE post_id = ? AND account_id = ?`, postID, accountID)
	if err != nil {
		return false, err
	}
	if n, _ := res.RowsAffected(); n > 0 {
		return false, tx.Commit()
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO kudos (post_id, account_id) VALUES (?, ?)`, postID, accountID); err != nil {
		return false, err
	}
	return true, tx.Commit()
}

// Stats returns totals for the masthead.
func (st *Store) Stats(ctx context.Context) (Stats, error) {
	var s Stats
	err := st.db.QueryRowContext(ctx,
		`SELECT (SELECT COUNT(*) FROM accounts), (SELECT COUNT(*) FROM threads),
		 (SELECT COUNT(*) FROM posts WHERE removed = 0 AND deleted = 0)`).
		Scan(&s.Accounts, &s.Threads, &s.Posts)
	return s, err
}
