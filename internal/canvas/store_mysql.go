package canvas

import (
	"context"
	"database/sql"
	"errors"
)

// MySQLStore backs Store with the canvases table.
type MySQLStore struct {
	DB *sql.DB
}

func NewMySQLStore(db *sql.DB) *MySQLStore { return &MySQLStore{DB: db} }

const schema = `
CREATE TABLE IF NOT EXISTS canvases (
	id         BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,
	name       VARCHAR(128) NOT NULL,
	graph      JSON         NOT NULL,
	version    BIGINT UNSIGNED NOT NULL DEFAULT 1,
	created_at TIMESTAMP(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
	updated_at TIMESTAMP(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6) ON UPDATE CURRENT_TIMESTAMP(6)
) ENGINE = InnoDB DEFAULT CHARSET = utf8mb4`

// EnsureSchema creates the canvases table when missing. It runs at
// canvas-server startup and is idempotent.
func (s *MySQLStore) EnsureSchema(ctx context.Context) error {
	_, err := s.DB.ExecContext(ctx, schema)
	return err
}

func (s *MySQLStore) List(ctx context.Context) ([]Canvas, error) {
	rows, err := s.DB.QueryContext(ctx, `
		SELECT id, name, version, created_at, updated_at
		FROM canvases
		ORDER BY updated_at DESC, id DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []Canvas
	for rows.Next() {
		var c Canvas
		if err := rows.Scan(&c.ID, &c.Name, &c.Version, &c.CreatedAt, &c.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (s *MySQLStore) Get(ctx context.Context, id int64) (Canvas, error) {
	var c Canvas
	err := s.DB.QueryRowContext(ctx, `
		SELECT id, name, graph, version, created_at, updated_at
		FROM canvases WHERE id = ?`, id,
	).Scan(&c.ID, &c.Name, &c.Graph, &c.Version, &c.CreatedAt, &c.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return Canvas{}, ErrNotFound
	}
	if err != nil {
		return Canvas{}, err
	}
	return c, nil
}

func (s *MySQLStore) Create(ctx context.Context, name string, graph []byte) (Canvas, error) {
	res, err := s.DB.ExecContext(ctx,
		`INSERT INTO canvases (name, graph) VALUES (?, ?)`, name, graph)
	if err != nil {
		return Canvas{}, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return Canvas{}, err
	}
	return s.Get(ctx, id)
}

// Rename changes the display name only. The version stays put on purpose:
// versions guard the graph document, so an unrelated rename must never
// invalidate another tab's pending auto-save.
func (s *MySQLStore) Rename(ctx context.Context, id int64, name string) (Canvas, error) {
	// 先探存在性再更新:MySQL 默认把「值未变的行」算 0 affected,
	// 同名改名若按 affected==0 判缺失会误报不存在。
	if _, err := s.Get(ctx, id); err != nil {
		return Canvas{}, err
	}
	if _, err := s.DB.ExecContext(ctx,
		`UPDATE canvases SET name = ? WHERE id = ?`, name, id); err != nil {
		return Canvas{}, err
	}
	return s.Get(ctx, id)
}

func (s *MySQLStore) Delete(ctx context.Context, id int64) error {
	res, err := s.DB.ExecContext(ctx, `DELETE FROM canvases WHERE id = ?`, id)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// SaveGraph stores the whole graph document behind the optimistic lock: the
// conditional UPDATE wins exactly once per version, so of two tabs saving
// the same expected version only one lands and the other conflicts.
//
// The acked version comes from LAST_INSERT_ID(version + 1) — the one-row
// UPDATE is atomic, so the winner always acks the version it produced. A
// plain read-back (Get) could observe a *later* writer's version and let
// that client silently clobber it on the next save.
func (s *MySQLStore) SaveGraph(ctx context.Context, id int64, graph []byte, expectedVersion int64) (Canvas, error) {
	res, err := s.DB.ExecContext(ctx,
		`UPDATE canvases SET graph = ?, version = LAST_INSERT_ID(version + 1) WHERE id = ? AND version = ?`,
		graph, id, expectedVersion)
	if err != nil {
		return Canvas{}, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return Canvas{}, err
	}
	if n == 0 {
		// 版本不匹配与画布不存在都会落到 0 行:查一次区分,
		// 让客户端能把「冲突」和「画布已被删」分开处理。
		if _, err := s.Get(ctx, id); err != nil {
			return Canvas{}, err // ErrNotFound
		}
		return Canvas{}, ErrVersionConflict
	}
	newVersion, err := res.LastInsertId()
	if err != nil {
		return Canvas{}, err
	}
	// 时间戳仅作展示:Get 读到的是快照时刻的最新值,不影响版本正确性。
	saved, err := s.Get(ctx, id)
	if err != nil {
		return Canvas{}, err
	}
	saved.Version = newVersion
	return saved, nil
}
