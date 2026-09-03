package asset

import (
	"context"
	"database/sql"
	"errors"
)

// MySQLStore backs Store with the assets table.
type MySQLStore struct {
	DB *sql.DB
}

func NewMySQLStore(db *sql.DB) *MySQLStore { return &MySQLStore{DB: db} }

// url 列用 MEDIUMTEXT:厂商回 b64 时产物以 data: URI 落库(对象存储转存
// 为后续决策点),几 MB 的内联图片要放得下。
const schema = `
CREATE TABLE IF NOT EXISTS assets (
	id         BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,
	kind       VARCHAR(16)  NOT NULL,
	canvas_id  BIGINT UNSIGNED NOT NULL,
	task_id    VARCHAR(64)  NOT NULL DEFAULT '',
	model      VARCHAR(200) NOT NULL DEFAULT '',
	prompt     TEXT         NULL,
	url        MEDIUMTEXT   NOT NULL,
	created_at TIMESTAMP(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
	KEY idx_assets_canvas (canvas_id, id),
	KEY idx_assets_task (task_id),
	KEY idx_assets_kind (kind, id)
) ENGINE = InnoDB DEFAULT CHARSET = utf8mb4`

// EnsureSchema creates the assets table when missing. It runs at
// canvas-server startup and is idempotent. The indexes serve ticket 14's
// per-canvas / per-task / per-kind filters.
func (s *MySQLStore) EnsureSchema(ctx context.Context) error {
	_, err := s.DB.ExecContext(ctx, schema)
	return err
}

func (s *MySQLStore) Create(ctx context.Context, a Asset) (Asset, error) {
	var prompt any
	if a.Prompt != "" {
		prompt = a.Prompt
	}
	res, err := s.DB.ExecContext(ctx,
		`INSERT INTO assets (kind, canvas_id, task_id, model, prompt, url)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		a.Kind, a.CanvasID, a.TaskID, a.Model, prompt, a.URL)
	if err != nil {
		return Asset{}, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return Asset{}, err
	}
	return s.Get(ctx, id)
}

func (s *MySQLStore) Get(ctx context.Context, id int64) (Asset, error) {
	var a Asset
	var prompt sql.NullString
	err := s.DB.QueryRowContext(ctx,
		`SELECT id, kind, canvas_id, task_id, model, prompt, url, created_at
		 FROM assets WHERE id = ?`, id,
	).Scan(&a.ID, &a.Kind, &a.CanvasID, &a.TaskID, &a.Model, &prompt, &a.URL, &a.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return Asset{}, ErrNotFound
	}
	if err != nil {
		return Asset{}, err
	}
	a.Prompt = prompt.String
	return a, nil
}
