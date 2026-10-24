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
// content_hash 是 SHA-256 十六进制(64 字符,48 号票),空 = 未算(历史行
// 态);两条普通索引服务 48 号票:idx_assets_hash 供上传查重与回填分组,
// idx_assets_object_key 供删除防护的兄弟行计数(均不加唯一索引 —— 多行同
// 哈希指向同一对象正是去重的合法结果)。
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
	content_hash VARCHAR(64) NOT NULL DEFAULT '',
	ip_id       BIGINT UNSIGNED NOT NULL DEFAULT 0,
	created_at  TIMESTAMP(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
	last_used_at TIMESTAMP(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
	KEY idx_assets_canvas (canvas_id, id),
	KEY idx_assets_task (task_id),
	KEY idx_assets_kind (kind, id),
	KEY idx_assets_last_used (last_used_at, id),
	KEY idx_assets_object_key (object_key),
	KEY idx_assets_hash (content_hash),
	KEY idx_assets_ip (ip_id, id)
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
	{"content_hash", "ALTER TABLE assets ADD COLUMN content_hash VARCHAR(64) NOT NULL DEFAULT ''"},
	{"ip_id", "ALTER TABLE assets ADD COLUMN ip_id BIGINT UNSIGNED NOT NULL DEFAULT 0"},
}

// indexMigrations adds the secondary indexes the fresh schema carries; the
// 47 号票的 last-used 排序索引与 48 号票的去重双索引(object_key 兄弟行
// 计数、content_hash 上传查重与回填分组)长在 CREATE TABLE 里,存量表同样
// 要在原地补上.
var indexMigrations = []struct{ index, ddl string }{
	{"idx_assets_last_used", "ALTER TABLE assets ADD INDEX idx_assets_last_used (last_used_at, id)"},
	{"idx_assets_object_key", "ALTER TABLE assets ADD INDEX idx_assets_object_key (object_key)"},
	{"idx_assets_hash", "ALTER TABLE assets ADD INDEX idx_assets_hash (content_hash)"},
	{"idx_assets_ip", "ALTER TABLE assets ADD INDEX idx_assets_ip (ip_id, id)"},
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
	object_key, content_type, size_bytes, content_hash, ip_id, created_at, last_used_at`

func (s *MySQLStore) Create(ctx context.Context, a Asset) (Asset, error) {
	var prompt any
	if a.Prompt != "" {
		prompt = a.Prompt
	}
	res, err := s.DB.ExecContext(ctx,
		`INSERT INTO assets (kind, canvas_id, task_id, model, prompt, url, object_key, content_type, size_bytes, content_hash, ip_id)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		a.Kind, a.CanvasID, a.TaskID, a.Model, prompt, a.URL, a.ObjectKey, a.ContentType, a.SizeBytes, a.ContentHash, a.IPID)
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
		a.object_key, a.content_type, a.size_bytes, a.content_hash, a.ip_id, a.created_at, a.last_used_at, c.name, i.name
		FROM assets a
		LEFT JOIN canvases c ON c.id = a.canvas_id
		LEFT JOIN ips i ON i.id = a.ip_id`+
			where+orderBy+` LIMIT ? OFFSET ?`,
		append(args, limit, offset)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []Listed
	for rows.Next() {
		var l Listed
		var prompt, canvasName, ipName sql.NullString
		if err := rows.Scan(&l.ID, &l.Kind, &l.CanvasID, &l.TaskID, &l.Model, &prompt,
			&l.URL, &l.ObjectKey, &l.ContentType, &l.SizeBytes, &l.ContentHash, &l.IPID, &l.CreatedAt, &l.LastUsedAt, &canvasName, &ipName); err != nil {
			return nil, err
		}
		l.Prompt = prompt.String
		l.CanvasName = canvasName.String
		l.IPName = ipName.String
		out = append(out, l)
	}
	return out, rows.Err()
}

// listWhere 拼过滤:scope 三态(50 号票)——ip = 本 IP + 全局两段
// (ip_id IN (X, 0)),global = 仅 0,其余(含空串)= 不过滤。
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
	switch f.Scope {
	case ScopeIP:
		where += " AND a.ip_id IN (?, 0)"
		args = append(args, f.IPID)
	case ScopeGlobal:
		where += " AND a.ip_id = 0"
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
		&a.URL, &a.ObjectKey, &a.ContentType, &a.SizeBytes, &a.ContentHash, &a.IPID, &a.CreatedAt, &a.LastUsedAt); err != nil {
		return Asset{}, err
	}
	a.Prompt = prompt.String
	return a, nil
}

// FindOldestByHash answers the oldest row carrying the hash (48 号票上传
// 复用的查重点;ORDER BY id 即最老,组内保留最老对象的同一口径)。
func (s *MySQLStore) FindOldestByHash(ctx context.Context, contentHash string) (Asset, error) {
	row := s.DB.QueryRowContext(ctx,
		`SELECT `+assetColumns+` FROM assets WHERE content_hash = ? ORDER BY id ASC LIMIT 1`, contentHash)
	a, err := scanAsset(row)
	if errors.Is(err, sql.ErrNoRows) {
		return Asset{}, ErrNotFound
	}
	return a, err
}

// CountOthersOnObjectKey answers the sibling-row count behind one object key
// (48 号票删除防护;idx_assets_object_key 服务此查询)。
func (s *MySQLStore) CountOthersOnObjectKey(ctx context.Context, objectKey string, excludeID int64) (int, error) {
	var n int
	if err := s.DB.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM assets WHERE object_key = ? AND id <> ?`, objectKey, excludeID).Scan(&n); err != nil {
		return 0, err
	}
	return n, nil
}

// ListWithoutHash pages un-hashed rows with an object key (48 号票回填的
// 批次查询),游标 = afterID:对象读不动的行留在无哈希态也不会卡住推进,
// 下一轮重跑自然重试。
func (s *MySQLStore) ListWithoutHash(ctx context.Context, afterID int64, limit int) ([]Asset, error) {
	rows, err := s.DB.QueryContext(ctx,
		`SELECT `+assetColumns+` FROM assets
		 WHERE content_hash = '' AND object_key <> '' AND id > ? ORDER BY id ASC LIMIT ?`,
		afterID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanAssets(rows)
}

// SetContentHash stamps one row's content hash (48 号票回填的落列)。
func (s *MySQLStore) SetContentHash(ctx context.Context, id int64, contentHash string) error {
	_, err := s.DB.ExecContext(ctx, `UPDATE assets SET content_hash = ? WHERE id = ?`, contentHash, id)
	return err
}

// ListByHash answers every row of one hash group, oldest first (48 号票
// 回填的组内成员;首行即保留行)。
func (s *MySQLStore) ListByHash(ctx context.Context, contentHash string) ([]Asset, error) {
	rows, err := s.DB.QueryContext(ctx,
		`SELECT `+assetColumns+` FROM assets WHERE content_hash = ? ORDER BY id ASC`, contentHash)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanAssets(rows)
}

// DuplicateHashes answers the hashes carried by more than one row (48 号票
// 回填的合并候选;组内保留与改指由调用方逐组推进)。
func (s *MySQLStore) DuplicateHashes(ctx context.Context) ([]string, error) {
	rows, err := s.DB.QueryContext(ctx,
		`SELECT content_hash FROM assets WHERE content_hash <> '' GROUP BY content_hash HAVING COUNT(*) > 1 ORDER BY MIN(id) ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var h string
		if err := rows.Scan(&h); err != nil {
			return nil, err
		}
		out = append(out, h)
	}
	return out, rows.Err()
}

// SetObjectKey repoints one row at another object key (48 号票回填的改指;
// 调用方负责在改指成功后才删冗余对象)。
func (s *MySQLStore) SetObjectKey(ctx context.Context, id int64, objectKey string) error {
	_, err := s.DB.ExecContext(ctx, `UPDATE assets SET object_key = ? WHERE id = ?`, objectKey, id)
	return err
}

// scanAssets drains an assets query into rows (回填的批读共用).
func scanAssets(rows *sql.Rows) ([]Asset, error) {
	var out []Asset
	for rows.Next() {
		a, err := scanAsset(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}
