package canvas

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/gachal/InfiniteChance/internal/sqlitedb"
)

// SQLiteStore backs Store with the canvases table in the desktop's single
// app database. Same table shape as MySQLStore's minus the MySQL engine
// clauses; timestamps are canonical TEXT (see sqlitedb).
type SQLiteStore struct {
	DB *sql.DB
}

func NewSQLiteStore(db *sql.DB) *SQLiteStore { return &SQLiteStore{DB: db} }

// SQLiteStore 与 MySQLStore 同构实现 canvas.Store。
var _ Store = (*SQLiteStore)(nil)

// 时间列说明:TEXT 定宽 RFC3339 纳秒 UTC(sqlitedb.TimeLayout),字典序=
// 时间序。表默认值(strftime 的 %f 只有毫秒精度)只作兜底,store 的全部
// 写入都显式给 sqlitedb.FormatTime 生成的定宽值,保证读回 ParseTime 无损。
const sqliteSchema = `
CREATE TABLE IF NOT EXISTS canvases (
	id         INTEGER      NOT NULL PRIMARY KEY AUTOINCREMENT,
	name       TEXT         NOT NULL,
	graph      TEXT         NOT NULL,
	version    INTEGER      NOT NULL DEFAULT 1,
	created_at TEXT         NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%f000Z','now')),
	updated_at TEXT         NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%f000Z','now'))
)`

// EnsureSchema creates the canvases table when missing. The desktop database
// starts from scratch (no legacy AUTO_INCREMENT shape), so the MySQL-side
// migration has no counterpart here. Idempotent.
func (s *SQLiteStore) EnsureSchema(ctx context.Context) error {
	_, err := s.DB.ExecContext(ctx, sqliteSchema)
	return err
}

func (s *SQLiteStore) List(ctx context.Context) ([]Canvas, error) {
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
		var createdAt, updatedAt string
		if err := rows.Scan(&c.ID, &c.Name, &c.Version, &createdAt, &updatedAt); err != nil {
			return nil, err
		}
		if c.CreatedAt, err = sqlitedb.ParseTime(createdAt); err != nil {
			return nil, err
		}
		if c.UpdatedAt, err = sqlitedb.ParseTime(updatedAt); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (s *SQLiteStore) Get(ctx context.Context, id int64) (Canvas, error) {
	var c Canvas
	var createdAt, updatedAt string
	err := s.DB.QueryRowContext(ctx, `
		SELECT id, name, graph, version, created_at, updated_at
		FROM canvases WHERE id = ?`, id,
	).Scan(&c.ID, &c.Name, &c.Graph, &c.Version, &createdAt, &updatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return Canvas{}, ErrNotFound
	}
	if err != nil {
		return Canvas{}, err
	}
	if c.CreatedAt, err = sqlitedb.ParseTime(createdAt); err != nil {
		return Canvas{}, err
	}
	if c.UpdatedAt, err = sqlitedb.ParseTime(updatedAt); err != nil {
		return Canvas{}, err
	}
	return c, nil
}

func (s *SQLiteStore) Create(ctx context.Context, name string, graph []byte) (Canvas, error) {
	now := sqlitedb.FormatTime(time.Now())
	res, err := s.DB.ExecContext(ctx,
		`INSERT INTO canvases (name, graph, created_at, updated_at) VALUES (?, ?, ?, ?)`,
		name, graph, now, now)
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
func (s *SQLiteStore) Rename(ctx context.Context, id int64, name string) (Canvas, error) {
	// 先探存在性再更新,与 MySQL 版同一结构:同名改名在 MySQL 会按「值未
	// 变的行」算 0 affected;SQLite 的 changes() 按命中行计,不会误报,
	// 但保留同一探活路径让两个 store 的缺失语义完全一致。updated_at 由
	// 写入显式带上(SQLite 没有 ON UPDATE CURRENT_TIMESTAMP)。
	if _, err := s.Get(ctx, id); err != nil {
		return Canvas{}, err
	}
	if _, err := s.DB.ExecContext(ctx,
		`UPDATE canvases SET name = ?, updated_at = ? WHERE id = ?`,
		name, sqlitedb.FormatTime(time.Now()), id); err != nil {
		return Canvas{}, err
	}
	return s.Get(ctx, id)
}

func (s *SQLiteStore) Delete(ctx context.Context, id int64) error {
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
// MySQL 版的 ack 版本来自 LAST_INSERT_ID(version + 1);SQLite 没有这个
// 每连接技巧,但条件 UPDATE 已保证赢家的旧版本恰为 expectedVersion,新
// 版本必然是 expectedVersion + 1 —— 在 Go 侧计算,与 LAST_INSERT_ID 同值
// 同语义。其后的 Get 只取展示用时间戳,读到的可能是更晚写者的行,ack 的
// 版本不受影响,客户端不会因此悄悄覆盖别人。
func (s *SQLiteStore) SaveGraph(ctx context.Context, id int64, graph []byte, expectedVersion int64) (Canvas, error) {
	res, err := s.DB.ExecContext(ctx,
		`UPDATE canvases SET graph = ?, version = version + 1, updated_at = ?
		 WHERE id = ? AND version = ?`,
		graph, sqlitedb.FormatTime(time.Now()), id, expectedVersion)
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
	newVersion := expectedVersion + 1
	// 时间戳仅作展示:Get 读到的是快照时刻的最新值,不影响版本正确性。
	saved, err := s.Get(ctx, id)
	if err != nil {
		return Canvas{}, err
	}
	saved.Version = newVersion
	return saved, nil
}
