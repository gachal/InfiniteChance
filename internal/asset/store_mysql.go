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

// url 列用 MEDIUMTEXT:厂商回 b64 时产物以 data: URI 落库(转存落地后
// 新产物走 object_key,data: URI 只存在于历史行),几 MB 的内联图片要放得下。
const schema = `
CREATE TABLE IF NOT EXISTS assets (
	id          BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,
	kind        VARCHAR(16)  NOT NULL,
	canvas_id   BIGINT UNSIGNED NOT NULL,
	task_id     VARCHAR(64)  NOT NULL DEFAULT '',
	model       VARCHAR(200) NOT NULL DEFAULT '',
	prompt      TEXT         NULL,
	url         MEDIUMTEXT   NOT NULL,
	object_key  VARCHAR(512) NOT NULL DEFAULT '',
	content_type VARCHAR(100) NOT NULL DEFAULT '',
	size_bytes  BIGINT       NOT NULL DEFAULT 0,
	created_at  TIMESTAMP(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
	last_used_at TIMESTAMP(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
	KEY idx_assets_canvas (canvas_id, id),
	KEY idx_assets_task (task_id),
	KEY idx_assets_kind (kind, id),
	KEY idx_assets_last_used (last_used_at, id)
) ENGINE = InnoDB DEFAULT CHARSET = utf8mb4`

// migrations widens tables created by earlier tickets in place (14 号票的
// 对象存储列;47 号票的 last_used_at):CREATE TABLE IF NOT EXISTS never
// widens an existing table, an already-deployed canvas DB must upgrade
// without a rebuild. Idempotent. 存量行由列默认值统一填 ALTER 时刻 —— 排序
// 上全部「从未使用」,彼此按 id DESC 呈创建序,语义与「初始值 = created_at」
// 的意图一致(47 号票)。
var migrations = []struct{ column, ddl string }{
	{"object_key", "ALTER TABLE assets ADD COLUMN object_key VARCHAR(512) NOT NULL DEFAULT ''"},
	{"content_type", "ALTER TABLE assets ADD COLUMN content_type VARCHAR(100) NOT NULL DEFAULT ''"},
	{"size_bytes", "ALTER TABLE assets ADD COLUMN size_bytes BIGINT NOT NULL DEFAULT 0"},
	{"last_used_at", "ALTER TABLE assets ADD COLUMN last_used_at TIMESTAMP(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6)"},
}

// indexMigrations adds the secondary indexes the fresh schema carries; the
// 47 号票的 last-used 排序索引长在 CREATE TABLE 里,存量表同样要在原地补上.
var indexMigrations = []struct{ index, ddl string }{
	{"idx_assets_last_used", "ALTER TABLE assets ADD INDEX idx_assets_last_used (last_used_at, id)"},
}

// EnsureSchema creates the assets table when missing and widens an existing
// one in place. It runs at canvas-server startup and is idempotent. The
// indexes serve the per-canvas / per-task / per-kind filters of the asset
// library and admin page.
func (s *MySQLStore) EnsureSchema(ctx context.Context) error {
	if _, err := s.DB.ExecContext(ctx, schema); err != nil {
		return err
	}
	for _, m := range migrations {
		var count int
		if err := s.DB.QueryRowContext(ctx,
			`SELECT COUNT(*) FROM information_schema.COLUMNS
			 WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'assets' AND COLUMN_NAME = ?`,
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
	for _, m := range indexMigrations {
		var count int
		if err := s.DB.QueryRowContext(ctx,
			`SELECT COUNT(*) FROM information_schema.STATISTICS
			 WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'assets' AND INDEX_NAME = ?`,
			m.index).Scan(&count); err != nil {
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

const assetColumns = `id, kind, canvas_id, task_id, model, prompt, url,
	object_key, content_type, size_bytes, created_at, last_used_at`

func (s *MySQLStore) Create(ctx context.Context, a Asset) (Asset, error) {
	var prompt any
	if a.Prompt != "" {
		prompt = a.Prompt
	}
	res, err := s.DB.ExecContext(ctx,
		`INSERT INTO assets (kind, canvas_id, task_id, model, prompt, url, object_key, content_type, size_bytes)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		a.Kind, a.CanvasID, a.TaskID, a.Model, prompt, a.URL, a.ObjectKey, a.ContentType, a.SizeBytes)
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
	row := s.DB.QueryRowContext(ctx,
		`SELECT `+assetColumns+` FROM assets WHERE id = ?`, id)
	a, err := scanAsset(row)
	if errors.Is(err, sql.ErrNoRows) {
		return Asset{}, ErrNotFound
	}
	return a, err
}

// listLimit 默认与上限:库管理面分页拉取,一页足够铺满管理端网格。
const (
	defaultListLimit = 50
	maxListLimit     = 200
)

// List answers assets newest first under the filter; sort = SortLastUsed
// turns the order into last_used_at DESC(47 号票挑选场景的最近使用序),
// id DESC 在两个口径里都作平局裁决。
func (s *MySQLStore) List(ctx context.Context, f Filter) ([]Listed, error) {
	limit := f.Limit
	if limit <= 0 {
		limit = defaultListLimit
	}
	if limit > maxListLimit {
		limit = maxListLimit
	}
	offset := f.Offset
	if offset < 0 {
		offset = 0
	}
	orderBy := " ORDER BY a.id DESC"
	if f.Sort == SortLastUsed {
		orderBy = " ORDER BY a.last_used_at DESC, a.id DESC"
	}
	where, args := listWhere(f)
	rows, err := s.DB.QueryContext(ctx,
		`SELECT a.id, a.kind, a.canvas_id, a.task_id, a.model, a.prompt, a.url,
			a.object_key, a.content_type, a.size_bytes, a.created_at, a.last_used_at, c.name
		 FROM assets a LEFT JOIN canvases c ON c.id = a.canvas_id`+
			where+orderBy+` LIMIT ? OFFSET ?`,
		append(args, limit, offset)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []Listed
	for rows.Next() {
		var l Listed
		var prompt, canvasName sql.NullString
		if err := rows.Scan(&l.ID, &l.Kind, &l.CanvasID, &l.TaskID, &l.Model, &prompt,
			&l.URL, &l.ObjectKey, &l.ContentType, &l.SizeBytes, &l.CreatedAt, &l.LastUsedAt, &canvasName); err != nil {
			return nil, err
		}
		l.Prompt = prompt.String
		l.CanvasName = canvasName.String
		out = append(out, l)
	}
	return out, rows.Err()
}

func listWhere(f Filter) (string, []any) {
	where := " WHERE 1=1"
	var args []any
	if f.Kind != "" {
		where += " AND a.kind = ?"
		args = append(args, f.Kind)
	}
	if f.CanvasID > 0 {
		where += " AND a.canvas_id = ?"
		args = append(args, f.CanvasID)
	}
	return where, args
}

func (s *MySQLStore) Delete(ctx context.Context, id int64) error {
	res, err := s.DB.ExecContext(ctx, `DELETE FROM assets WHERE id = ?`, id)
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

// TouchLastUsed stamps one asset's last_used_at to the statement time
// (47 号票「解引用即算」的 MySQL 写入点)。NOW(6) 与行内其余列的语句级
// 时间戳同源,一次任务多参考逐条调用即逐条刷新。
func (s *MySQLStore) TouchLastUsed(ctx context.Context, id int64) error {
	res, err := s.DB.ExecContext(ctx, `UPDATE assets SET last_used_at = NOW(6) WHERE id = ?`, id)
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

type rowScanner interface {
	Scan(dest ...any) error
}

func scanAsset(row rowScanner) (Asset, error) {
	var a Asset
	var prompt sql.NullString
	if err := row.Scan(&a.ID, &a.Kind, &a.CanvasID, &a.TaskID, &a.Model, &prompt,
		&a.URL, &a.ObjectKey, &a.ContentType, &a.SizeBytes, &a.CreatedAt, &a.LastUsedAt); err != nil {
		return Asset{}, err
	}
	a.Prompt = prompt.String
	return a, nil
}
