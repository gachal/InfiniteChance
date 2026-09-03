package prompttemplate

import (
	"context"
	"database/sql"
	"errors"
)

// MySQLStore backs Store with the prompt_templates table. Gateway and
// canvas/server each construct their own handle over the same shared table.
type MySQLStore struct {
	DB *sql.DB
}

func NewMySQLStore(db *sql.DB) *MySQLStore { return &MySQLStore{DB: db} }

const schema = `
CREATE TABLE IF NOT EXISTS prompt_templates (
	id         BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,
	name       VARCHAR(128)  NOT NULL,
	template   MEDIUMTEXT    NOT NULL,
	enabled    TINYINT(1)    NOT NULL DEFAULT 1,
	created_at TIMESTAMP(6)  NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
	updated_at TIMESTAMP(6)  NOT NULL DEFAULT CURRENT_TIMESTAMP(6) ON UPDATE CURRENT_TIMESTAMP(6)
) ENGINE = InnoDB DEFAULT CHARSET = utf8mb4`

// EnsureSchema creates the prompt_templates table when missing. Idempotent.
func (s *MySQLStore) EnsureSchema(ctx context.Context) error {
	_, err := s.DB.ExecContext(ctx, schema)
	return err
}

const templateColumns = `id, name, template, enabled, created_at, updated_at`

type rowScanner interface {
	Scan(dest ...any) error
}

func scanRow(scan rowScanner) (Template, error) {
	var t Template
	err := scan.Scan(&t.ID, &t.Name, &t.Template, &t.Enabled, &t.CreatedAt, &t.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return Template{}, ErrNotFound
	}
	if err != nil {
		return Template{}, err
	}
	return t, nil
}

func (s *MySQLStore) byID(ctx context.Context, id int64) (Template, error) {
	return scanRow(s.DB.QueryRowContext(ctx,
		`SELECT `+templateColumns+` FROM prompt_templates WHERE id = ?`, id))
}

func (s *MySQLStore) List(ctx context.Context) ([]Template, error) {
	return s.listWhere(ctx, `1`)
}

func (s *MySQLStore) ListEnabled(ctx context.Context) ([]Template, error) {
	return s.listWhere(ctx, `enabled = 1`)
}

// listWhere lists by the given WHERE predicate, id ascending — the stable
// order for both the admin table and the canvas catalog.
func (s *MySQLStore) listWhere(ctx context.Context, where string) ([]Template, error) {
	rows, err := s.DB.QueryContext(ctx,
		`SELECT `+templateColumns+` FROM prompt_templates WHERE `+where+` ORDER BY id ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var templates []Template
	for rows.Next() {
		t, err := scanRow(rows)
		if err != nil {
			return nil, err
		}
		templates = append(templates, t)
	}
	return templates, rows.Err()
}

func (s *MySQLStore) Get(ctx context.Context, id int64) (Template, error) {
	return s.byID(ctx, id)
}

func (s *MySQLStore) Create(ctx context.Context, t Template) (Template, error) {
	res, err := s.DB.ExecContext(ctx,
		`INSERT INTO prompt_templates (name, template, enabled) VALUES (?, ?, ?)`,
		t.Name, t.Template, t.Enabled)
	if err != nil {
		return Template{}, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return Template{}, err
	}
	return s.byID(ctx, id)
}

func (s *MySQLStore) Update(ctx context.Context, t Template) (Template, error) {
	if _, err := s.DB.ExecContext(ctx,
		`UPDATE prompt_templates SET name = ?, template = ?, enabled = ? WHERE id = ?`,
		t.Name, t.Template, t.Enabled, t.ID); err != nil {
		return Template{}, err
	}
	// MySQL 只计「被更改」的行:affected=0 可能是行不存在,
	// 也可能是新值与旧值完全一致,统一交给 byID 判定。
	return s.byID(ctx, t.ID)
}

func (s *MySQLStore) Delete(ctx context.Context, id int64) error {
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
