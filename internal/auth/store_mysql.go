package auth

import (
	"context"
	"database/sql"
	"errors"

	mysqldriver "github.com/go-sql-driver/mysql"
)

// MySQLStore backs Store with the admin_accounts table.
type MySQLStore struct {
	DB *sql.DB
}

func NewMySQLStore(db *sql.DB) *MySQLStore { return &MySQLStore{DB: db} }

const schema = `
CREATE TABLE IF NOT EXISTS admin_accounts (
	id            BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
	username      VARCHAR(64)  NOT NULL,
	password_hash VARCHAR(255) NOT NULL,
	created_at    TIMESTAMP(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
	updated_at    TIMESTAMP(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6) ON UPDATE CURRENT_TIMESTAMP(6),
	UNIQUE KEY uniq_admin_username (username)
) ENGINE = InnoDB DEFAULT CHARSET = utf8mb4`

// EnsureSchema creates the admin_accounts table when missing. It runs at
// gateway startup; the statement is idempotent.
func (s *MySQLStore) EnsureSchema(ctx context.Context) error {
	_, err := s.DB.ExecContext(ctx, schema)
	return err
}

func (s *MySQLStore) Initialized(ctx context.Context) (bool, error) {
	var count int
	if err := s.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM admin_accounts`).Scan(&count); err != nil {
		return false, err
	}
	return count > 0, nil
}

// CreateFirstAdmin inserts the first account or reports ErrAdminExists. The
// existence check and the insert are one statement, so concurrent inits
// cannot both win.
func (s *MySQLStore) CreateFirstAdmin(ctx context.Context, username, passwordHash string) error {
	res, err := s.DB.ExecContext(ctx,
		`INSERT INTO admin_accounts (username, password_hash)
		 SELECT ?, ? FROM DUAL
		 WHERE NOT EXISTS (SELECT 1 FROM admin_accounts)`,
		username, passwordHash)
	if err != nil {
		// Same-name concurrent inits hit the unique key instead.
		var mysqlErr *mysqldriver.MySQLError
		if errors.As(err, &mysqlErr) && mysqlErr.Number == 1062 {
			return ErrAdminExists
		}
		return err
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return ErrAdminExists
	}
	return nil
}

func (s *MySQLStore) AccountByUsername(ctx context.Context, username string) (Account, error) {
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
