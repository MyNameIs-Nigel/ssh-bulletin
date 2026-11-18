package store

import (
	"context"
	"database/sql"
	"errors"
	"slices"
)

// ErrChatLimit is returned when a key sends chat messages too quickly.
var ErrChatLimit = errors.New("you're sending messages too fast. Wait a few seconds")

// Chat quotas. They are looser than the post quota because chat lines are
// short and conversational, but still bound how fast one key can scroll
// everyone else's view.
const (
	chatBurst       = 5 // messages per chatBurstWindow seconds, per key
	chatBurstWindow = 10
	chatPerMinute   = 20 // messages per rolling minute, per key
	chatNewcomers   = 60 // messages per rolling minute shared by accounts < 24h old
)

// ChatMessage is one line in a board's live chat.
type ChatMessage struct {
	ID            int64
	BoardID       int64
	AuthorID      int64
	AuthorHandle  string
	AuthorPalette string
	AuthorRole    Role
	Body          string
	CreatedAt     int64
}

const chatSelect = `SELECT c.id, c.board_id, c.author_id, a.handle, a.palette, ` + roleSQL + `, c.body, c.created_at
	FROM chat_messages c JOIN accounts a ON a.id = c.author_id`

// visibleChat filters out deleted and removed messages: chat has no
// placeholders, a withdrawn line simply disappears.
const visibleChat = ` AND c.deleted = 0 AND c.removed = 0`

func scanChat(rows *sql.Rows) ([]ChatMessage, error) {
	defer rows.Close()
	var out []ChatMessage
	for rows.Next() {
		var c ChatMessage
		if err := rows.Scan(&c.ID, &c.BoardID, &c.AuthorID, &c.AuthorHandle, &c.AuthorPalette, &c.AuthorRole, &c.Body, &c.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// ChatBefore returns up to limit visible messages older than beforeID (0 for
// the newest), oldest first.
func (st *Store) ChatBefore(ctx context.Context, boardID, beforeID int64, limit int) ([]ChatMessage, error) {
	q := chatSelect + ` WHERE c.board_id = ?` + visibleChat
	args := []any{boardID}
	if beforeID > 0 {
		q += ` AND c.id < ?`
		args = append(args, beforeID)
	}
	rows, err := st.db.QueryContext(ctx, q+` ORDER BY c.id DESC LIMIT ?`, append(args, limit)...)
	if err != nil {
		return nil, err
	}
	out, err := scanChat(rows)
	slices.Reverse(out)
	return out, err
}

// ChatAfter returns up to limit visible messages newer than afterID, oldest
// first.
func (st *Store) ChatAfter(ctx context.Context, boardID, afterID int64, limit int) ([]ChatMessage, error) {
	rows, err := st.db.QueryContext(ctx, chatSelect+` WHERE c.board_id = ? AND c.id > ?`+visibleChat+
		` ORDER BY c.id LIMIT ?`, boardID, afterID, limit)
	if err != nil {
		return nil, err
	}
	return scanChat(rows)
}

// PostChat appends a message to a board's chat after checking the quotas in
// the same transaction.
func (st *Store) PostChat(ctx context.Context, boardID, authorID int64, body string, now int64) (ChatMessage, error) {
	tx, err := st.db.BeginTx(ctx, nil)
	if err != nil {
		return ChatMessage{}, err
	}
	defer func() { _ = tx.Rollback() }()
	var exists bool
	if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM boards WHERE id = ?)`, boardID).Scan(&exists); err != nil {
		return ChatMessage{}, err
	}
	if !exists {
		return ChatMessage{}, ErrNotFound
	}
	if err := checkChatQuota(ctx, tx, authorID, now); err != nil {
		return ChatMessage{}, err
	}
	res, err := tx.ExecContext(ctx, `INSERT INTO chat_messages (board_id, author_id, body, created_at) VALUES (?, ?, ?, ?)`,
		boardID, authorID, body, now)
	if err != nil {
		return ChatMessage{}, err
	}
	id, _ := res.LastInsertId()
	rows, err := tx.QueryContext(ctx, chatSelect+` WHERE c.id = ?`, id)
	if err != nil {
		return ChatMessage{}, err
	}
	msgs, err := scanChat(rows)
	if err != nil {
		return ChatMessage{}, err
	}
	if len(msgs) != 1 {
		return ChatMessage{}, ErrNotFound
	}
	return msgs[0], tx.Commit()
}

func checkChatQuota(ctx context.Context, tx *sql.Tx, authorID, now int64) error {
	var burst, minute int
	if err := tx.QueryRowContext(ctx, `SELECT
		COUNT(*) FILTER (WHERE created_at > ?), COUNT(*)
		FROM chat_messages WHERE author_id = ? AND created_at > ?`,
		now-chatBurstWindow, authorID, now-60).Scan(&burst, &minute); err != nil {
		return err
	}
	if burst >= chatBurst || minute >= chatPerMinute {
		return ErrChatLimit
	}
	var created int64
	if err := tx.QueryRowContext(ctx, `SELECT created_at FROM accounts WHERE id = ?`, authorID).Scan(&created); err != nil {
		return err
	}
	const day = 24 * 60 * 60
	if created <= now-day {
		return nil
	}
	var shared int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM chat_messages c JOIN accounts a ON a.id = c.author_id
		WHERE c.created_at > ? AND a.created_at > ?`, now-60, now-day).Scan(&shared); err != nil {
		return err
	}
	if shared >= chatNewcomers {
		return ErrNewcomerLimit
	}
	return nil
}

// DeleteChat withdraws the actor's own message and blanks its body.
func (st *Store) DeleteChat(ctx context.Context, actorID, id int64) (int64, error) {
	tx, err := st.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	boardID, authorID, gone, err := findChat(ctx, tx, id)
	if err != nil {
		return 0, err
	}
	if authorID != actorID {
		return 0, ErrForbidden
	}
	if gone {
		return 0, ErrNotFound
	}
	if _, err := tx.ExecContext(ctx, `UPDATE chat_messages SET deleted = 1, body = '' WHERE id = ?`, id); err != nil {
		return 0, err
	}
	return boardID, tx.Commit()
}

// RemoveChat hides a message on staff authority. The body is kept, with who
// removed it and when, so the decision can be audited.
func (st *Store) RemoveChat(ctx context.Context, actorID, id, now int64) (int64, error) {
	tx, err := st.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	role, err := accountRole(ctx, tx, actorID)
	if err != nil {
		return 0, err
	}
	if !role.CanModerate() {
		return 0, ErrForbidden
	}
	boardID, _, gone, err := findChat(ctx, tx, id)
	if err != nil {
		return 0, err
	}
	if gone {
		return 0, ErrNotFound
	}
	if _, err := tx.ExecContext(ctx, `UPDATE chat_messages SET removed = 1, removed_by = ?, removed_at = ? WHERE id = ?`,
		actorID, now, id); err != nil {
		return 0, err
	}
	return boardID, tx.Commit()
}

func findChat(ctx context.Context, tx *sql.Tx, id int64) (boardID, authorID int64, gone bool, err error) {
	err = tx.QueryRowContext(ctx, `SELECT board_id, author_id, deleted = 1 OR removed = 1 FROM chat_messages WHERE id = ?`, id).
		Scan(&boardID, &authorID, &gone)
	if errors.Is(err, sql.ErrNoRows) {
		err = ErrNotFound
	}
	return
}
