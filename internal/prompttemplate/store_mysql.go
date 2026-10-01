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
	id          BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,
	name        VARCHAR(128)  NOT NULL,
	description VARCHAR(200)  NOT NULL DEFAULT '',
	template    MEDIUMTEXT    NOT NULL,
	target      VARCHAR(16)   NOT NULL DEFAULT 'any',
	enabled     TINYINT(1)    NOT NULL DEFAULT 1,
	created_at  TIMESTAMP(6)  NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
	updated_at  TIMESTAMP(6)  NOT NULL DEFAULT CURRENT_TIMESTAMP(6) ON UPDATE CURRENT_TIMESTAMP(6)
) ENGINE = InnoDB DEFAULT CHARSET = utf8mb4`

// migrations adds the 29 号票技能列 in place:CREATE TABLE IF NOT EXISTS
// never widens an existing table, an already-deployed gateway DB must
// upgrade without a rebuild. Idempotent.
var migrations = []struct{ column, ddl string }{
	{"description", "ALTER TABLE prompt_templates ADD COLUMN description VARCHAR(200) NOT NULL DEFAULT ''"},
	{"target", "ALTER TABLE prompt_templates ADD COLUMN target VARCHAR(16) NOT NULL DEFAULT 'any'"},
}

// EnsureSchema creates the prompt_templates table when missing and widens an
// existing one in place. Idempotent.
func (s *MySQLStore) EnsureSchema(ctx context.Context) error {
	if _, err := s.DB.ExecContext(ctx, schema); err != nil {
		return err
	}
	for _, m := range migrations {
		var count int
		if err := s.DB.QueryRowContext(ctx,
			`SELECT COUNT(*) FROM information_schema.COLUMNS
			 WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'prompt_templates' AND COLUMN_NAME = ?`,
			m.column).Scan(&count); err != nil {
			return err
		}
		if count > 0 {
			continue
		}
		if _, err := s.DB.ExecContext(ctx, m.ddl); err != nil {
			return err
		}
	}
	return nil
}

const templateColumns = `id, name, description, template, target, enabled, created_at, updated_at`

type rowScanner interface {
	Scan(dest ...any) error
}

func scanRow(scan rowScanner) (Template, error) {
	var t Template
	err := scan.Scan(&t.ID, &t.Name, &t.Description, &t.Template, &t.Target, &t.Enabled, &t.CreatedAt, &t.UpdatedAt)
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
		`INSERT INTO prompt_templates (name, description, template, target, enabled) VALUES (?, ?, ?, ?, ?)`,
		t.Name, t.Description, t.Template, t.Target, t.Enabled)
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
		`UPDATE prompt_templates SET name = ?, description = ?, template = ?, target = ?, enabled = ? WHERE id = ?`,
		t.Name, t.Description, t.Template, t.Target, t.Enabled, t.ID); err != nil {
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
