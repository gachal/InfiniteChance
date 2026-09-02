package auth

import (
	"context"
	"database/sql"
	"errors"
	"strings"

	mysqldriver "github.com/go-sql-driver/mysql"
)

// MySQLStore backs Store with the admin_accounts table.
type MySQLStore struct {
	DB *sql.DB
}

func NewMySQLStore(db *sql.DB) *MySQLStore { return &MySQLStore{DB: db} }

// 全库唯一管理员由主键本身保证:id 固定为 1,并发 init 的输家必然撞 1062,
// 该保证不依赖事务隔离级别。唯一键仍保留,用于同用户名并发插入的报错归一。
const schema = `
CREATE TABLE IF NOT EXISTS admin_accounts (
	id            BIGINT UNSIGNED NOT NULL PRIMARY KEY,
	username      VARCHAR(64)  NOT NULL,
	password_hash VARCHAR(255) NOT NULL,
	created_at    TIMESTAMP(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
	updated_at    TIMESTAMP(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6) ON UPDATE CURRENT_TIMESTAMP(6),
	UNIQUE KEY uniq_admin_username (username)
) ENGINE = InnoDB DEFAULT CHARSET = utf8mb4`

// EnsureSchema creates the admin_accounts table when missing and migrates
// tables created by earlier builds (AUTO_INCREMENT id) to the fixed-PK
// shape. It runs at gateway startup and is idempotent.
func (s *MySQLStore) EnsureSchema(ctx context.Context) error {
	if _, err := s.DB.ExecContext(ctx, schema); err != nil {
		return err
	}
	return s.migrateLegacyAutoIncrement(ctx)
}

// migrateLegacyAutoIncrement drops AUTO_INCREMENT from admin_accounts.id and
// normalizes the surviving row (at most one could exist under the old guard)
// to id = 1. No-op for tables created with the current schema.
func (s *MySQLStore) migrateLegacyAutoIncrement(ctx context.Context) error {
	var extra string
	err := s.DB.QueryRowContext(ctx,
		`SELECT EXTRA FROM information_schema.COLUMNS
		 WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'admin_accounts' AND COLUMN_NAME = 'id'`,
	).Scan(&extra)
	if err != nil {
		return err
	}
	if !strings.Contains(strings.ToLower(extra), "auto_increment") {
		return nil
	}
	if _, err := s.DB.ExecContext(ctx,
		`ALTER TABLE admin_accounts MODIFY id BIGINT UNSIGNED NOT NULL`); err != nil {
		return err
	}
	_, err = s.DB.ExecContext(ctx, `UPDATE admin_accounts SET id = 1`)
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
// fixed primary key makes concurrent inits lose with a duplicate-key error
// at any isolation level, so exactly one insert can ever succeed.
func (s *MySQLStore) CreateFirstAdmin(ctx context.Context, username, passwordHash string) error {
	_, err := s.DB.ExecContext(ctx,
		`INSERT INTO admin_accounts (id, username, password_hash) VALUES (1, ?, ?)`,
		username, passwordHash)
	if err != nil {
		var mysqlErr *mysqldriver.MySQLError
		if errors.As(err, &mysqlErr) && mysqlErr.Number == 1062 {
			return ErrAdminExists
		}
		return err
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
