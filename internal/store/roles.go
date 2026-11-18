package store

import (
	"context"
	"database/sql"
	"errors"
	"strings"
)

// Role is assigned to a player's key identity.
type Role string

const (
	RoleOwner Role = "SU"
	RoleAdmin Role = "A"
)

func (r Role) CanModerate() bool { return r == RoleOwner || r == RoleAdmin }

var (
	ErrForbidden     = errors.New("you don't have permission for that")
	ErrKeyMismatch   = errors.New("that fingerprint does not match the handle")
	ErrOwnerMismatch = errors.New("configured owner differs from the stored owner")
	ErrOwnerRole     = errors.New("the super admin already has all permissions")
)

const roleSQL = ` CASE
	WHEN a.fingerprint = (SELECT fingerprint FROM owner WHERE singleton = 1) THEN 'SU'
	WHEN EXISTS (SELECT 1 FROM admins WHERE account_id = a.id) THEN 'A'
	ELSE '' END `

// ConfigureOwner reserves a specific key before accepting connections.
func (st *Store) ConfigureOwner(ctx context.Context, fingerprint string) error {
	_, err := st.db.ExecContext(ctx, `INSERT INTO owner (singleton, fingerprint) VALUES (1, ?)
		ON CONFLICT(singleton) DO NOTHING`, fingerprint)
	if err != nil {
		return err
	}
	var owner string
	if err := st.db.QueryRowContext(ctx, `SELECT fingerprint FROM owner WHERE singleton = 1`).Scan(&owner); err != nil {
		return err
	}
	if owner != fingerprint {
		return ErrOwnerMismatch
	}
	return nil
}

// ClaimOwner reserves ownership on the first accepted session. A database
// upgraded from before roles uses the oldest real account as its best record
// of who arrived first. Demo account fingerprints have a different length.
func (st *Store) ClaimOwner(ctx context.Context, fingerprint string) error {
	_, err := st.db.ExecContext(ctx, `INSERT INTO owner (singleton, fingerprint)
		SELECT 1, COALESCE((SELECT fingerprint FROM accounts
			WHERE length(fingerprint) = 50 AND substr(fingerprint, 1, 7) = 'SHA256:'
			ORDER BY created_at, id LIMIT 1), ?)
		WHERE NOT EXISTS (SELECT 1 FROM owner WHERE singleton = 1)`, fingerprint)
	return err
}

func accountRole(ctx context.Context, tx *sql.Tx, id int64) (Role, error) {
	var role Role
	err := tx.QueryRowContext(ctx, `SELECT `+roleSQL+` FROM accounts a WHERE a.id = ?`, id).Scan(&role)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrForbidden
	}
	return role, err
}

// SetAdmin checks the caller and the handle/key pair in the same transaction
// as the role update, so a concurrent rename cannot change its target.
func (st *Store) SetAdmin(ctx context.Context, actorID int64, handle, fingerprint string, grant bool) (Account, error) {
	tx, err := st.db.BeginTx(ctx, nil)
	if err != nil {
		return Account{}, err
	}
	defer tx.Rollback()
	role, err := accountRole(ctx, tx, actorID)
	if err != nil {
		return Account{}, err
	}
	if role != RoleOwner {
		return Account{}, ErrForbidden
	}
	target, err := st.scanAccount(tx.QueryRowContext(ctx, accountSelect+` WHERE a.handle_lower = ?`, strings.ToLower(handle)))
	if err != nil {
		return Account{}, err
	}
	if target.Fingerprint != fingerprint {
		return Account{}, ErrKeyMismatch
	}
	if target.Role == RoleOwner {
		return Account{}, ErrOwnerRole
	}
	if grant {
		_, err = tx.ExecContext(ctx, `INSERT INTO admins (account_id) VALUES (?) ON CONFLICT DO NOTHING`, target.ID)
		target.Role = RoleAdmin
	} else {
		_, err = tx.ExecContext(ctx, `DELETE FROM admins WHERE account_id = ?`, target.ID)
		target.Role = ""
	}
	if err != nil {
		return Account{}, err
	}
	return target, tx.Commit()
}

// Admins lists the current admins for the owner.
func (st *Store) Admins(ctx context.Context, actorID int64, offset, limit int) ([]Account, error) {
	tx, err := st.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	role, err := accountRole(ctx, tx, actorID)
	if err != nil {
		return nil, err
	}
	if role != RoleOwner {
		return nil, ErrForbidden
	}
	rows, err := tx.QueryContext(ctx, accountSelect+`
		JOIN admins ON admins.account_id = a.id ORDER BY a.handle_lower, a.id LIMIT ? OFFSET ?`, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var admins []Account
	for rows.Next() {
		a, err := st.scanAccount(rows)
		if err != nil {
			return nil, err
		}
		admins = append(admins, a)
	}
	return admins, rows.Err()
}
