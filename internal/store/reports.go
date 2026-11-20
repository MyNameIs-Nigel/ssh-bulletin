package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

func captureEvidence(ctx context.Context, tx *sql.Tx, target ModerationTarget, visibleOnly bool) (moderationEvidence, error) {
	var e moderationEvidence
	var err error
	switch target.Kind {
	case PostTarget:
		err = tx.QueryRowContext(ctx, `SELECT t.title, a.handle, p.body FROM posts p
			JOIN threads t ON t.id = p.thread_id JOIN accounts a ON a.id = p.author_id
			WHERE p.id = ? AND (NOT ? OR (p.deleted = 0 AND p.removed = 0))`, target.ID, visibleOnly).
			Scan(&e.title, &e.author, &e.body)
	case ChatTarget:
		err = tx.QueryRowContext(ctx, `SELECT b.title, a.handle FROM chat_messages c
			JOIN boards b ON b.id = c.board_id JOIN accounts a ON a.id = c.author_id
			WHERE c.id = ? AND (NOT ? OR (c.deleted = 0 AND c.removed = 0))`, target.ID, visibleOnly).
			Scan(&e.title, &e.author)
		if err == nil {
			e.body, err = chatEvidence(ctx, tx, target.ID)
		}
	case ProfileTarget, ContactTarget:
		err = tx.QueryRowContext(ctx, `SELECT '@' || handle, handle, bio FROM accounts WHERE id = ?`, target.ID).
			Scan(&e.title, &e.author, &e.body)
	default:
		return e, ErrNotFound
	}
	if errors.Is(err, sql.ErrNoRows) {
		err = ErrNotFound
	}
	return e, err
}

// Capture the reported line with nearby visible messages from the same board.
func chatEvidence(ctx context.Context, tx *sql.Tx, id int64) (string, error) {
	rows, err := tx.QueryContext(ctx, `WITH target AS (SELECT id, board_id FROM chat_messages WHERE id = ?),
		before AS (SELECT c.id FROM chat_messages c JOIN target t ON t.board_id = c.board_id
			WHERE c.id < t.id AND c.deleted = 0 AND c.removed = 0 ORDER BY c.id DESC LIMIT 2),
		after AS (SELECT c.id FROM chat_messages c JOIN target t ON t.board_id = c.board_id
			WHERE c.id > t.id AND c.deleted = 0 AND c.removed = 0 ORDER BY c.id LIMIT 2)
		SELECT c.id, a.handle, c.body, c.created_at FROM chat_messages c JOIN accounts a ON a.id = c.author_id
		WHERE c.id IN (SELECT id FROM before UNION ALL SELECT id FROM target UNION ALL SELECT id FROM after) ORDER BY c.id`, id)
	if err != nil {
		return "", err
	}
	defer rows.Close()
	var lines []string
	for rows.Next() {
		var messageID, created int64
		var handle, body string
		if err := rows.Scan(&messageID, &handle, &body, &created); err != nil {
			return "", err
		}
		line := fmt.Sprintf("%s UTC @%s: %s", time.Unix(created, 0).UTC().Format("15:04:05"), handle, body)
		if messageID == id {
			line = "[reported message] " + line
		}
		lines = append(lines, line)
	}
	return strings.Join(lines, "\n"), rows.Err()
}

func checkProfileStaff(ctx context.Context, tx *sql.Tx, actorID, targetID int64) error {
	actor, err := accountRole(ctx, tx, actorID)
	if err != nil {
		return err
	}
	if !actor.CanModerate() || actorID == targetID {
		return ErrForbidden
	}
	target, err := accountRole(ctx, tx, targetID)
	if err != nil {
		return err
	}
	if target == RoleOwner || (target == RoleAdmin && actor != RoleOwner) {
		return ErrForbidden
	}
	return nil
}

// ReviewReport applies a decision to the item from an open report.
func (st *Store) ReviewReport(ctx context.Context, actorID, reportID int64, action ModerationAction, reason string, now int64) (ModerationTarget, int64, error) {
	var target ModerationTarget
	tx, err := st.beginAccountWrite(ctx, actorID, now)
	if err != nil {
		return target, 0, err
	}
	defer tx.Rollback()
	role, err := accountRole(ctx, tx, actorID)
	if err != nil {
		return target, 0, err
	}
	if !role.CanModerate() {
		return target, 0, ErrForbidden
	}
	var e moderationEvidence
	err = tx.QueryRowContext(ctx, `SELECT target_kind, target_id, title, author, body FROM moderation_log
		WHERE id = ? AND action = 'reported' AND resolved = 0`, reportID).
		Scan(&target.Kind, &target.ID, &e.title, &e.author, &e.body)
	if errors.Is(err, sql.ErrNoRows) {
		return target, 0, ErrReviewChanged
	}
	if err != nil {
		return target, 0, err
	}
	var boardID int64
	switch {
	case action == Dismissed && target.Kind != PostTarget:
	case action == Removed && target.Kind == ChatTarget:
		boardID, err = removeChat(ctx, tx, actorID, target.ID, now)
	case target.Kind == ProfileTarget && (action == BioCleared || action == HandleChangeRequired):
		err = reviewProfile(ctx, tx, actorID, target.ID, action, e)
	default:
		return target, 0, ErrReviewChanged
	}
	if err != nil {
		return target, 0, err
	}
	if _, err := resolveReports(ctx, tx, target); err != nil {
		return target, 0, err
	}
	if _, err := logModeration(ctx, tx, actorID, target, action, reason, e, now, 0); err != nil {
		return target, 0, err
	}
	return target, boardID, tx.Commit()
}

func reviewProfile(ctx context.Context, tx *sql.Tx, actorID, targetID int64, action ModerationAction, report moderationEvidence) error {
	if err := checkProfileStaff(ctx, tx, actorID, targetID); err != nil {
		return err
	}
	var res sql.Result
	var err error
	if action == BioCleared {
		res, err = tx.ExecContext(ctx, `UPDATE accounts SET bio = '' WHERE id = ? AND bio = ? AND bio != ''`, targetID, report.body)
	} else {
		res, err = tx.ExecContext(ctx, `UPDATE accounts SET handle_change_required = 1 WHERE id = ? AND handle = ? AND handle_change_required = 0`, targetID, report.author)
	}
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrReviewChanged
	}
	return nil
}

// ContactStaff is a private, rate-limited route available during restrictions.
func (st *Store) ContactStaff(ctx context.Context, actorID int64, message string, now int64) error {
	tx, err := st.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var recent bool
	if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM moderation_log
		WHERE actor_id = ? AND action = 'reported' AND target_kind = 'contact' AND created_at > ?)`, actorID, now-300).Scan(&recent); err != nil {
		return err
	}
	if recent {
		return ErrContactLimit
	}
	target := ModerationTarget{Kind: ContactTarget, ID: actorID}
	e, err := captureEvidence(ctx, tx, target, false)
	if err != nil {
		return err
	}
	e.body = message
	if _, err := logModeration(ctx, tx, actorID, target, Reported, "Message to staff", e, now, 0); err != nil {
		return err
	}
	return tx.Commit()
}

var ErrContactLimit = errors.New("you can contact staff once every five minutes")
