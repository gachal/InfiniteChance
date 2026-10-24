package canvas

import (
	"context"
	"database/sql"
	"errors"
	"strconv"
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
// ip_id/type 是 50 号票加列:存量行由列默认值收编(ip_id=0 等自愈迁移归
// 入默认 IP;type='animation' 即「当前的画布就是动画画布」口径)。ips 表
// 同属本包领域,这里一并建出——List/Get join ips 取展示名,表必须在
// canvas schema 就绪时即存在(asset 侧的 List join 同理受益)。
const sqliteSchema = `
CREATE TABLE IF NOT EXISTS canvases (
	id         INTEGER      NOT NULL PRIMARY KEY AUTOINCREMENT,
	name       TEXT         NOT NULL,
	graph      TEXT         NOT NULL,
	version    INTEGER      NOT NULL DEFAULT 1,
	ip_id      INTEGER      NOT NULL DEFAULT 0,
	type       TEXT         NOT NULL DEFAULT 'animation',
	created_at TEXT         NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%f000Z','now')),
	updated_at TEXT         NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%f000Z','now'))
)`

const sqliteIPsSchema = `
CREATE TABLE IF NOT EXISTS ips (
	id              INTEGER NOT NULL PRIMARY KEY AUTOINCREMENT,
	name            TEXT    NOT NULL,
	description     TEXT    NOT NULL DEFAULT '',
	cover_asset_id  INTEGER NOT NULL DEFAULT 0,
	created_at      TEXT    NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%f000Z','now')),
	updated_at      TEXT    NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%f000Z','now'))
)`

// sqliteMigrations widens tables created by earlier desktop builds in place
// (48 号票同款决策:CREATE TABLE IF NOT EXISTS 不会加宽已存在的表,存量
// app.db 靠 PRAGMA table_info 检查后 ALTER)。Idempotent.
var sqliteMigrations = []struct{ column, ddl string }{
	{"ip_id", "ALTER TABLE canvases ADD COLUMN ip_id INTEGER NOT NULL DEFAULT 0"},
	{"type", "ALTER TABLE canvases ADD COLUMN type TEXT NOT NULL DEFAULT 'animation'"},
}

// EnsureSchema creates the canvases + ips tables when missing and widens an
// existing canvases table in place. The desktop database starts from scratch
// (no legacy AUTO_INCREMENT shape), so the MySQL-side migration has no
// counterpart here beyond the column widening. Idempotent. IP 行的收编
// (默认 IP 自愈迁移)在 IPStore 的 EnsureSchema,装配顺序在其后。
func (s *SQLiteStore) EnsureSchema(ctx context.Context) error {
	if _, err := s.DB.ExecContext(ctx, sqliteSchema); err != nil {
		return err
	}
	for _, m := range sqliteMigrations {
		rows, err := s.DB.QueryContext(ctx, `PRAGMA table_info(canvases)`)
		if err != nil {
			return err
		}
		found := false
		for rows.Next() {
			var cid int
			var name, ctype string
			var notNull, pk int
			var dfltValue any
			if err := rows.Scan(&cid, &name, &ctype, &notNull, &dfltValue, &pk); err != nil {
				rows.Close()
				return err
			}
			if name == m.column {
				found = true
			}
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return err
		}
		rows.Close()
		if found {
			continue
		}
		if _, err := s.DB.ExecContext(ctx, m.ddl); err != nil {
			return err
		}
	}
	if _, err := s.DB.ExecContext(ctx, sqliteIPsSchema); err != nil {
		return err
	}
	return nil
}

func (s *SQLiteStore) List(ctx context.Context, ipID int64) ([]Canvas, error) {
	where := ""
	args := []any{}
	if ipID > 0 {
		where = " WHERE c.ip_id = ?"
		args = append(args, ipID)
	}
	rows, err := s.DB.QueryContext(ctx, `
		SELECT c.id, c.name, c.version, c.ip_id, i.name, c.type, c.created_at, c.updated_at
		FROM canvases c`+canvasJoinIP+where+`
		ORDER BY c.updated_at DESC, c.id DESC`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []Canvas
	for rows.Next() {
		var c Canvas
		var createdAt, updatedAt string
		var ipName sql.NullString
		if err := rows.Scan(&c.ID, &c.Name, &c.Version, &c.IPID, &ipName, &c.Type, &createdAt, &updatedAt); err != nil {
			return nil, err
		}
		if c.CreatedAt, err = sqlitedb.ParseTime(createdAt); err != nil {
			return nil, err
		}
		if c.UpdatedAt, err = sqlitedb.ParseTime(updatedAt); err != nil {
			return nil, err
		}
		c.IPName = ipName.String
		out = append(out, c)
	}
	return out, rows.Err()
}

func (s *SQLiteStore) Get(ctx context.Context, id int64) (Canvas, error) {
	var c Canvas
	var createdAt, updatedAt string
	var ipName sql.NullString
	err := s.DB.QueryRowContext(ctx, `
		SELECT c.id, c.name, c.graph, c.version, c.ip_id, i.name, c.type, c.created_at, c.updated_at
		FROM canvases c`+canvasJoinIP+` WHERE c.id = ?`, id,
	).Scan(&c.ID, &c.Name, &c.Graph, &c.Version, &c.IPID, &ipName, &c.Type, &createdAt, &updatedAt)
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
	c.IPName = ipName.String
	return c, nil
}

func (s *SQLiteStore) Create(ctx context.Context, ipID int64, name string, graph []byte, canvasType string) (Canvas, error) {
	now := sqlitedb.FormatTime(time.Now())
	res, err := s.DB.ExecContext(ctx,
		`INSERT INTO canvases (name, graph, ip_id, type, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?)`,
		name, graph, ipID, canvasType, now, now)
	if err != nil {
		return Canvas{}, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return Canvas{}, err
	}
	return s.Get(ctx, id)
}

// updateColumn 是 Rename/SetType/Move 共用的「探存在 → 单列更新 → 回读」
// 路径:与 MySQL 版同构(同名/同值更新在 MySQL 按 0 affected 计,探活路径
// 让两个 store 的缺失语义完全一致);updated_at 由写入显式带上(SQLite 没有
// ON UPDATE CURRENT_TIMESTAMP)。版本号不推进:版本只守护图文档,无关的
// 元数据更新不能打断其他标签页的在途自动保存。
func (s *SQLiteStore) updateColumn(ctx context.Context, id int64, column, value string) (Canvas, error) {
	if _, err := s.Get(ctx, id); err != nil {
		return Canvas{}, err
	}
	if _, err := s.DB.ExecContext(ctx,
		`UPDATE canvases SET `+column+` = ?, updated_at = ? WHERE id = ?`,
		value, sqlitedb.FormatTime(time.Now()), id); err != nil {
		return Canvas{}, err
	}
	return s.Get(ctx, id)
}

// Rename changes the display name only. The version stays put on purpose:
// versions guard the graph document, so an unrelated rename must never
// invalidate another tab's pending auto-save.
func (s *SQLiteStore) Rename(ctx context.Context, id int64, name string) (Canvas, error) {
	return s.updateColumn(ctx, id, "name", name)
}

// SetType retypes one canvas(50 号票 PATCH type);type 串原样落库。
func (s *SQLiteStore) SetType(ctx context.Context, id int64, canvasType string) (Canvas, error) {
	return s.updateColumn(ctx, id, "type", canvasType)
}

// Move re-homes one canvas under another IP. 只改画布归属,不回填该画布
// 名下历史素材行的 ip_id —— 旧产物留在原 IP 名下、过滤仍可见,此后新产物
// 由 worker 传导新 IP(历史行不清洗原则)。
func (s *SQLiteStore) Move(ctx context.Context, id, ipID int64) (Canvas, error) {
	return s.updateColumn(ctx, id, "ip_id", strconv.FormatInt(ipID, 10))
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
