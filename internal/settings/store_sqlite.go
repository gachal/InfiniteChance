package settings

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/gachal/InfiniteChance/internal/sqlitedb"
)

// SQLiteStore backs Store with the settings table in the desktop's single
// app database. Same row shape and upsert semantics as MySQLStore's, minus
// the MySQL dialect; timestamps are canonical fixed-width RFC3339 UTC TEXT
// (see sqlitedb). 桌面装配无 OSS 诉求,行缺省即 local,零配置不受影响。
type SQLiteStore struct {
	DB *sql.DB
}

func NewSQLiteStore(db *sql.DB) *SQLiteStore { return &SQLiteStore{DB: db} }

const sqliteSchema = `
CREATE TABLE IF NOT EXISTS settings (
	name       TEXT NOT NULL PRIMARY KEY,
	value      TEXT NOT NULL,
	updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%f000Z','now'))
)`

// EnsureSchema creates the settings table when missing. 桌面库从零建起,
// MySQL 侧没有要迁移的老形态。幂等。
func (s *SQLiteStore) EnsureSchema(ctx context.Context) error {
	_, err := s.DB.ExecContext(ctx, sqliteSchema)
	return err
}

func (s *SQLiteStore) Get(ctx context.Context, name string) (Setting, error) {
	if err := validateName(name); err != nil {
		return Setting{}, err
	}
	var row Setting
	var value []byte
	var updatedAt string
	row.Name = name
	err := s.DB.QueryRowContext(ctx,
		`SELECT value, updated_at FROM settings WHERE name = ?`, name).
		Scan(&value, &updatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return Setting{}, ErrNotFound
	}
	if err != nil {
		return Setting{}, err
	}
	if row.UpdatedAt, err = sqlitedb.ParseTime(updatedAt); err != nil {
		return Setting{}, err
	}
	row.Value = json.RawMessage(value)
	return row, nil
}

func (s *SQLiteStore) Put(ctx context.Context, name string, value []byte) (Setting, error) {
	if err := validateName(name); err != nil {
		return Setting{}, err
	}
	// MySQL 版靠 ON UPDATE CURRENT_TIMESTAMP(6) 打点;SQLite 的 strftime
	// 默认值只有 6 位小数秒,ParseTime 读不回,时间戳由 Go 侧以定宽
	// FormatTime 显式写入(prompttemplate 同款)。
	stamped := sqlitedb.FormatTime(time.Now())
	if _, err := s.DB.ExecContext(ctx,
		`INSERT INTO settings (name, value, updated_at) VALUES (?, ?, ?)
		 ON CONFLICT(name) DO UPDATE SET value = excluded.value, updated_at = excluded.updated_at`,
		name, string(value), stamped); err != nil {
		return Setting{}, err
	}
	return s.Get(ctx, name)
}
