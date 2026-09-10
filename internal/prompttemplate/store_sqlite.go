package prompttemplate

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/gachal/InfiniteChance/internal/sqlitedb"
)

// SQLiteStore backs Store with the prompt_templates table in the desktop's
// single app database. Same table shape and CRUD semantics as MySQLStore's,
// minus the MySQL dialect; timestamps are canonical fixed-width RFC3339 UTC
// TEXT (see sqlitedb).
type SQLiteStore struct {
	DB *sql.DB
}

func NewSQLiteStore(db *sql.DB) *SQLiteStore { return &SQLiteStore{DB: db} }

const sqliteSchema = `
CREATE TABLE IF NOT EXISTS prompt_templates (
	id         INTEGER PRIMARY KEY AUTOINCREMENT,
	name       TEXT      NOT NULL,
	template   TEXT      NOT NULL,
	enabled    INTEGER   NOT NULL DEFAULT 1,
	created_at TEXT      NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%f000Z','now')),
	updated_at TEXT      NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%f000Z','now'))
)`

// EnsureSchema creates the prompt_templates table when missing. 桌面库从零
// 建起,MySQL 侧没有要迁移的老形态。幂等。
func (s *SQLiteStore) EnsureSchema(ctx context.Context) error {
	_, err := s.DB.ExecContext(ctx, sqliteSchema)
	return err
}

func scanSQLiteRow(scan rowScanner) (Template, error) {
	var t Template
	var createdAt, updatedAt string
	err := scan.Scan(&t.ID, &t.Name, &t.Template, &t.Enabled, &createdAt, &updatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return Template{}, ErrNotFound
	}
	if err != nil {
		return Template{}, err
	}
	if t.CreatedAt, err = sqlitedb.ParseTime(createdAt); err != nil {
		return Template{}, err
	}
	if t.UpdatedAt, err = sqlitedb.ParseTime(updatedAt); err != nil {
		return Template{}, err
	}
	return t, nil
}

func (s *SQLiteStore) byID(ctx context.Context, id int64) (Template, error) {
	return scanSQLiteRow(s.DB.QueryRowContext(ctx,
		`SELECT `+templateColumns+` FROM prompt_templates WHERE id = ?`, id))
}

func (s *SQLiteStore) List(ctx context.Context) ([]Template, error) {
	return s.listWhere(ctx, `1`)
}

func (s *SQLiteStore) ListEnabled(ctx context.Context) ([]Template, error) {
	return s.listWhere(ctx, `enabled = 1`)
}

// listWhere lists by the given WHERE predicate, id ascending — the stable
// order for both the admin table and the canvas catalog.
func (s *SQLiteStore) listWhere(ctx context.Context, where string) ([]Template, error) {
	rows, err := s.DB.QueryContext(ctx,
		`SELECT `+templateColumns+` FROM prompt_templates WHERE `+where+` ORDER BY id ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var templates []Template
	for rows.Next() {
		t, err := scanSQLiteRow(rows)
		if err != nil {
			return nil, err
		}
		templates = append(templates, t)
	}
	return templates, rows.Err()
}

func (s *SQLiteStore) Get(ctx context.Context, id int64) (Template, error) {
	return s.byID(ctx, id)
}

func (s *SQLiteStore) Create(ctx context.Context, t Template) (Template, error) {
	// MySQL 版靠 DEFAULT CURRENT_TIMESTAMP(6) 打点;SQLite 的 strftime
	// 默认值只有 6 位小数秒,ParseTime 读不回,时间戳由 Go 侧以定宽
	// FormatTime 显式写入(创建时两个时间戳同值)。
	stamped := sqlitedb.FormatTime(time.Now())
	res, err := s.DB.ExecContext(ctx,
		`INSERT INTO prompt_templates (name, template, enabled, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?)`,
		t.Name, t.Template, t.Enabled, stamped, stamped)
	if err != nil {
		return Template{}, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return Template{}, err
	}
	return s.byID(ctx, id)
}

func (s *SQLiteStore) Update(ctx context.Context, t Template) (Template, error) {
	if _, err := s.DB.ExecContext(ctx,
		`UPDATE prompt_templates SET name = ?, template = ?, enabled = ? WHERE id = ?`,
		t.Name, t.Template, t.Enabled, t.ID); err != nil {
		return Template{}, err
	}
	// SQLite 计「WHERE 命中」的行,affected=0 即行不存在;但与 MySQL 版
	// 一样不做分支,统一交给 byID 判定。
	return s.byID(ctx, t.ID)
}

func (s *SQLiteStore) Delete(ctx context.Context, id int64) error {
	res, err := s.DB.ExecContext(ctx, `DELETE FROM prompt_templates WHERE id = ?`, id)
	if err != nil {
		return err
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return ErrNotFound
	}
	return nil
}
