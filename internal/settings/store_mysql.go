package settings

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
)

// MySQLStore backs Store with the settings table. The gateway's admin
// surface and canvas/server's read side each construct their own handle
// over the same shared table (prompt_templates 先例).
type MySQLStore struct {
	DB *sql.DB
}

func NewMySQLStore(db *sql.DB) *MySQLStore { return &MySQLStore{DB: db} }

const schema = `
CREATE TABLE IF NOT EXISTS settings (
	name       VARCHAR(64)  NOT NULL PRIMARY KEY,
	value      JSON         NOT NULL,
	updated_at TIMESTAMP(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6) ON UPDATE CURRENT_TIMESTAMP(6)
) ENGINE = InnoDB DEFAULT CHARSET = utf8mb4`

// EnsureSchema creates the settings table when missing. Idempotent.
func (s *MySQLStore) EnsureSchema(ctx context.Context) error {
	_, err := s.DB.ExecContext(ctx, schema)
	return err
}

func (s *MySQLStore) Get(ctx context.Context, name string) (Setting, error) {
	if err := validateName(name); err != nil {
		return Setting{}, err
	}
	var row Setting
	var value []byte
	row.Name = name
	err := s.DB.QueryRowContext(ctx,
		`SELECT value, updated_at FROM settings WHERE name = ?`, name).
		Scan(&value, &row.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return Setting{}, ErrNotFound
	}
	if err != nil {
		return Setting{}, err
	}
	row.Value = json.RawMessage(value)
	return row, nil
}

func (s *MySQLStore) Put(ctx context.Context, name string, value []byte) (Setting, error) {
	if err := validateName(name); err != nil {
		return Setting{}, err
	}
	// JSON 列拒绝非 JSON 字节:坏值在写入时暴露,而不是读出后解码失败。
	if _, err := s.DB.ExecContext(ctx,
		`INSERT INTO settings (name, value) VALUES (?, ?)
		 ON DUPLICATE KEY UPDATE value = VALUES(value)`,
		name, string(value)); err != nil {
		return Setting{}, err
	}
	return s.Get(ctx, name)
}
