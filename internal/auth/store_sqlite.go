package auth

import (
	"context"
	"database/sql"
	"errors"

	"github.com/gachal/InfiniteChance/internal/sqlitedb"
)

// SQLiteStore backs Store with the admin_accounts table in the desktop's
// single app database. Same table shape as MySQLStore's minus the MySQL
// engine clauses; timestamps are canonical TEXT (see sqlitedb).
type SQLiteStore struct {
	DB *sql.DB
}

func NewSQLiteStore(db *sql.DB) *SQLiteStore { return &SQLiteStore{DB: db} }

const sqliteSchema = `
CREATE TABLE IF NOT EXISTS admin_accounts (
	id            INTEGER      NOT NULL PRIMARY KEY,
	username      TEXT         NOT NULL,
	password_hash TEXT         NOT NULL,
	created_at    TEXT         NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%f000Z','now')),
	updated_at    TEXT         NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%f000Z','now')),
	CONSTRAINT uniq_admin_username UNIQUE (username)
)`

// EnsureSchema creates the admin_accounts table when missing. The desktop
// database starts from scratch (no legacy AUTO_INCREMENT shape), so the
// MySQL-side migration has no counterpart here. Idempotent.
func (s *SQLiteStore) EnsureSchema(ctx context.Context) error {
	_, err := s.DB.ExecContext(ctx, sqliteSchema)
	return err
}

func (s *SQLiteStore) Initialized(ctx context.Context) (bool, error) {
	var count int
	if err := s.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM admin_accounts`).Scan(&count); err != nil {
		return false, err
	}
	return count > 0, nil
}

// CreateFirstAdmin inserts the first account or reports ErrAdminExists. The
// fixed primary key makes concurrent inits lose with a constraint error at
// any isolation level, so exactly one insert can ever succeed.
func (s *SQLiteStore) CreateFirstAdmin(ctx context.Context, username, passwordHash string) error {
	_, err := s.DB.ExecContext(ctx,
		`INSERT INTO admin_accounts (id, username, password_hash) VALUES (1, ?, ?)`,
		username, passwordHash)
	if err != nil {
		if sqlitedb.IsUniqueViolation(err) {
			return ErrAdminExists
		}
		return err
	}
	return nil
}

func (s *SQLiteStore) AccountByUsername(ctx context.Context, username string) (Account, error) {
	var account Account
	err := s.DB.QueryRowContext(ctx,
		`SELECT username, password_hash FROM admin_accounts WHERE username = ?`,
		username,
	).Scan(&account.Username, &account.PasswordHash)
	if errors.Is(err, sql.ErrNoRows) {
		return Account{}, ErrAdminNotFound
	}
	if err != nil {
		return Account{}, err
	}
	return account, nil
}
