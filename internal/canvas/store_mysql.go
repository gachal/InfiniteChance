package canvas

import (
	"context"
	"database/sql"
	"errors"
	"strconv"
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
	ip_id      BIGINT UNSIGNED NOT NULL DEFAULT 0,
	type       VARCHAR(32) NOT NULL DEFAULT 'animation',
	created_at TIMESTAMP(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
	updated_at TIMESTAMP(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6) ON UPDATE CURRENT_TIMESTAMP(6)
) ENGINE = InnoDB DEFAULT CHARSET = utf8mb4`

// ipsSchema 与 canvases 同包领域,canvas store 建表时一并建出:List/Get
// join ips 取展示名,表必须在 canvas schema 就绪时即存在。IP 行的收编
// (默认 IP 自愈迁移)在 IPStore 的 EnsureSchema,装配顺序在其后。
const ipsSchema = `
CREATE TABLE IF NOT EXISTS ips (
	id              BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,
	name            VARCHAR(255) NOT NULL,
	description     VARCHAR(1024) NOT NULL DEFAULT '',
	cover_asset_id  BIGINT UNSIGNED NOT NULL DEFAULT 0,
	created_at      TIMESTAMP(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
	updated_at      TIMESTAMP(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6) ON UPDATE CURRENT_TIMESTAMP(6)
) ENGINE = InnoDB DEFAULT CHARSET = utf8mb4`

// migrations widens canvases tables created by earlier tickets in place
// (50 号票的 ip_id/type):CREATE TABLE IF NOT EXISTS never widens an
// existing table, an already-deployed canvas DB must upgrade without a
// rebuild. Idempotent. 存量行由列默认值收编(ip_id=0 等自愈迁移归入默认
// IP;type='animation' 即「当前的画布就是动画画布」口径)。
var migrations = []struct{ column, ddl string }{
	{"ip_id", "ALTER TABLE canvases ADD COLUMN ip_id BIGINT UNSIGNED NOT NULL DEFAULT 0"},
	{"type", "ALTER TABLE canvases ADD COLUMN type VARCHAR(32) NOT NULL DEFAULT 'animation'"},
}

// EnsureSchema creates the canvases + ips tables when missing and widens an
// existing canvases table in place. It runs at canvas-server startup and is
// idempotent.
func (s *MySQLStore) EnsureSchema(ctx context.Context) error {
	if _, err := s.DB.ExecContext(ctx, schema); err != nil {
		return err
	}
	for _, m := range migrations {
		var count int
		if err := s.DB.QueryRowContext(ctx,
			`SELECT COUNT(*) FROM information_schema.COLUMNS
			 WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'canvases' AND COLUMN_NAME = ?`,
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
	if _, err := s.DB.ExecContext(ctx, ipsSchema); err != nil {
		return err
	}
	return nil
}

func (s *MySQLStore) List(ctx context.Context, ipID int64) ([]Canvas, error) {
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
		var ipName sql.NullString
		if err := rows.Scan(&c.ID, &c.Name, &c.Version, &c.IPID, &ipName, &c.Type, &c.CreatedAt, &c.UpdatedAt); err != nil {
			return nil, err
		}
		c.IPName = ipName.String
		out = append(out, c)
	}
	return out, rows.Err()
}

func (s *MySQLStore) Get(ctx context.Context, id int64) (Canvas, error) {
	var c Canvas
	var ipName sql.NullString
	err := s.DB.QueryRowContext(ctx, `
		SELECT c.id, c.name, c.graph, c.version, c.ip_id, i.name, c.type, c.created_at, c.updated_at
		FROM canvases c`+canvasJoinIP+` WHERE c.id = ?`, id,
	).Scan(&c.ID, &c.Name, &c.Graph, &c.Version, &c.IPID, &ipName, &c.Type, &c.CreatedAt, &c.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return Canvas{}, ErrNotFound
	}
	if err != nil {
		return Canvas{}, err
	}
	c.IPName = ipName.String
	return c, nil
}

func (s *MySQLStore) Create(ctx context.Context, ipID int64, name string, graph []byte, canvasType string) (Canvas, error) {
	res, err := s.DB.ExecContext(ctx,
		`INSERT INTO canvases (name, graph, ip_id, type) VALUES (?, ?, ?, ?)`, name, graph, ipID, canvasType)
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
// 路径:MySQL 默认把「值未变的行」算 0 affected,同名改名/同值更新若按
// affected==0 判缺失会误报不存在,故先探活。版本号不推进:版本只守护图文
// 档,无关的元数据更新不能打断其他标签页的在途自动保存。
func (s *MySQLStore) updateColumn(ctx context.Context, id int64, column, value string) (Canvas, error) {
	if _, err := s.Get(ctx, id); err != nil {
		return Canvas{}, err
	}
	if _, err := s.DB.ExecContext(ctx,
		`UPDATE canvases SET `+column+` = ? WHERE id = ?`, value, id); err != nil {
		return Canvas{}, err
	}
	return s.Get(ctx, id)
}

// Rename changes the display name only. The version stays put on purpose:
// versions guard the graph document, so an unrelated rename must never
// invalidate another tab's pending auto-save.
func (s *MySQLStore) Rename(ctx context.Context, id int64, name string) (Canvas, error) {
	return s.updateColumn(ctx, id, "name", name)
}

// SetType retypes one canvas(50 号票 PATCH type);type 串原样落库。
func (s *MySQLStore) SetType(ctx context.Context, id int64, canvasType string) (Canvas, error) {
	return s.updateColumn(ctx, id, "type", canvasType)
}

// Move re-homes one canvas under another IP. 只改画布归属,不回填该画布
// 名下历史素材行的 ip_id —— 旧产物留在原 IP 名下、过滤仍可见,此后新产物
// 由 worker 传导新 IP(历史行不清洗原则)。
func (s *MySQLStore) Move(ctx context.Context, id, ipID int64) (Canvas, error) {
	return s.updateColumn(ctx, id, "ip_id", strconv.FormatInt(ipID, 10))
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
