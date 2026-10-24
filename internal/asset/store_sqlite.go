package asset

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/gachal/InfiniteChance/internal/sqlitedb"
)

// SQLiteStore backs Store with the assets table in the desktop's single app
// database. Same table shape as MySQLStore's minus the MySQL engine clauses;
// timestamps are canonical TEXT (see sqlitedb).
type SQLiteStore struct {
	DB *sql.DB
}

func NewSQLiteStore(db *sql.DB) *SQLiteStore { return &SQLiteStore{DB: db} }

// SQLiteStore 与 MySQLStore 同构实现 asset.Store。
var _ Store = (*SQLiteStore)(nil)

// url 列在 MySQL 上是 MEDIUMTEXT:厂商回 b64 时产物以 data: URI 落库,
// 几 MB 的内联图片要放得下;SQLite 的 TEXT 没有长度上限,语义直接对上。
// 时间列说明见 canvas/store_sqlite.go:定宽 TEXT,写入显式给 FormatTime。
// content_hash 是 SHA-256 十六进制(48 号票),空 = 未算(历史行态)。
const sqliteSchema = `
CREATE TABLE IF NOT EXISTS assets (
	id           INTEGER NOT NULL PRIMARY KEY AUTOINCREMENT,
	kind         TEXT    NOT NULL,
	canvas_id    INTEGER NOT NULL,
	task_id      TEXT    NOT NULL DEFAULT '',
	model        TEXT    NOT NULL DEFAULT '',
	prompt       TEXT    NULL,
	url          TEXT    NOT NULL,
	object_key   TEXT    NOT NULL DEFAULT '',
	content_type TEXT    NOT NULL DEFAULT '',
	size_bytes   INTEGER NOT NULL DEFAULT 0,
	content_hash TEXT    NOT NULL DEFAULT '',
	ip_id        INTEGER NOT NULL DEFAULT 0,
	created_at   TEXT    NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%f000Z','now')),
	last_used_at TEXT    NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%f000Z','now'))
)`

// sqliteIndexes 是 MySQL 版表内 KEY idx_… 的 SQLite 对应物:MySQL 的
// 二级索引长在 CREATE TABLE 里,SQLite 用独立的 CREATE INDEX IF NOT
// EXISTS,名字沿用 MySQL 版的索引名。idx_assets_object_key / idx_assets_hash
// 服务 48 号票的删除防护计数与上传查重/回填分组(均非唯一)。
var sqliteIndexes = []string{
	`CREATE INDEX IF NOT EXISTS idx_assets_canvas ON assets (canvas_id, id)`,
	`CREATE INDEX IF NOT EXISTS idx_assets_task ON assets (task_id)`,
	`CREATE INDEX IF NOT EXISTS idx_assets_kind ON assets (kind, id)`,
	`CREATE INDEX IF NOT EXISTS idx_assets_last_used ON assets (last_used_at, id)`,
	`CREATE INDEX IF NOT EXISTS idx_assets_object_key ON assets (object_key)`,
	`CREATE INDEX IF NOT EXISTS idx_assets_hash ON assets (content_hash)`,
	`CREATE INDEX IF NOT EXISTS idx_assets_ip ON assets (ip_id, id)`,
}

// sqliteMigrations widens tables created by earlier desktop builds in place
// (canvastask 同款决策:CREATE TABLE IF NOT EXISTS 不会加宽已存在的表,
// 存量 app.db 靠 PRAGMA table_info 检查后 ALTER)。Idempotent.
var sqliteMigrations = []struct{ column, ddl string }{
	{"content_hash", "ALTER TABLE assets ADD COLUMN content_hash TEXT NOT NULL DEFAULT ''"},
	{"ip_id", "ALTER TABLE assets ADD COLUMN ip_id INTEGER NOT NULL DEFAULT 0"},
}

// EnsureSchema creates the assets table and its indexes when missing, then
// widens an existing table in place. 列迁移先于索引:存量表没有
// content_hash 列时,带列的 CREATE INDEX 会先炸。Idempotent.
func (s *SQLiteStore) EnsureSchema(ctx context.Context) error {
	if _, err := s.DB.ExecContext(ctx, sqliteSchema); err != nil {
		return err
	}
	for _, m := range sqliteMigrations {
		rows, err := s.DB.QueryContext(ctx, `PRAGMA table_info(assets)`)
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
	for _, ddl := range sqliteIndexes {
		if _, err := s.DB.ExecContext(ctx, ddl); err != nil {
			return err
		}
	}
	return nil
}

func (s *SQLiteStore) Create(ctx context.Context, a Asset) (Asset, error) {
	var prompt any
	if a.Prompt != "" {
		prompt = a.Prompt
	}
	// last_used_at 显式与 created_at 同值写入:初始「从未使用」= 创建时刻
	// (47 号票),同一条 NOW 免依赖 SQLite 默认表达式的语句内恒等。
	now := sqlitedb.FormatTime(time.Now())
	res, err := s.DB.ExecContext(ctx,
		`INSERT INTO assets (kind, canvas_id, task_id, model, prompt, url, object_key, content_type, size_bytes, content_hash, ip_id, created_at, last_used_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		a.Kind, a.CanvasID, a.TaskID, a.Model, prompt, a.URL, a.ObjectKey, a.ContentType, a.SizeBytes, a.ContentHash, a.IPID,
		now, now)
	if err != nil {
		return Asset{}, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return Asset{}, err
	}
	return s.Get(ctx, id)
}

func (s *SQLiteStore) Get(ctx context.Context, id int64) (Asset, error) {
	row := s.DB.QueryRowContext(ctx,
		`SELECT `+assetColumns+` FROM assets WHERE id = ?`, id)
	a, err := scanSQLiteAsset(row)
	if errors.Is(err, sql.ErrNoRows) {
		return Asset{}, ErrNotFound
	}
	return a, err
}

// List answers assets newest first under the filter; sort = SortLastUsed
// turns the order into last_used_at DESC(47 号票挑选场景的最近使用序),
// id DESC 在两个口径里都作平局裁决。The canvas join is a LEFT JOIN: 素材比
// 画布活得久,画布行没了素材照列,名字空着即可。LIMIT/OFFSET 分页与
// MySQL 版同形;listWhere 过滤拼接两边共用。
func (s *SQLiteStore) List(ctx context.Context, f Filter) ([]Listed, error) {
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
		var prompt, contentHash, createdAt, lastUsedAt, canvasName, ipName sql.NullString
		if err := rows.Scan(&l.ID, &l.Kind, &l.CanvasID, &l.TaskID, &l.Model, &prompt,
			&l.URL, &l.ObjectKey, &l.ContentType, &l.SizeBytes, &contentHash, &l.IPID, &createdAt, &lastUsedAt, &canvasName, &ipName); err != nil {
			return nil, err
		}
		if l.CreatedAt, err = sqlitedb.ParseTime(createdAt.String); err != nil {
			return nil, err
		}
		if l.LastUsedAt, err = sqlitedb.ParseTime(lastUsedAt.String); err != nil {
			return nil, err
		}
		l.Prompt = prompt.String
		l.ContentHash = contentHash.String
		l.CanvasName = canvasName.String
		l.IPName = ipName.String
		out = append(out, l)
	}
	return out, rows.Err()
}

func (s *SQLiteStore) Delete(ctx context.Context, id int64) error {
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

// TouchLastUsed stamps one asset's last_used_at to now(47 号票「解引用即算」
// 的 SQLite 写入点),与 MySQLStore 的 NOW(6) 同语义。
func (s *SQLiteStore) TouchLastUsed(ctx context.Context, id int64) error {
	res, err := s.DB.ExecContext(ctx,
		`UPDATE assets SET last_used_at = ? WHERE id = ?`, sqlitedb.FormatTime(time.Now()), id)
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

// scanSQLiteAsset 是 scanAsset 的 SQLite 版:created_at 是 TEXT,读出转回
// time.Time,保持与 MySQL 版相同的 Go 返回类型。
func scanSQLiteAsset(row rowScanner) (Asset, error) {
	var a Asset
	var prompt, contentHash, createdAt, lastUsedAt sql.NullString
	if err := row.Scan(&a.ID, &a.Kind, &a.CanvasID, &a.TaskID, &a.Model, &prompt,
		&a.URL, &a.ObjectKey, &a.ContentType, &a.SizeBytes, &contentHash, &a.IPID, &createdAt, &lastUsedAt); err != nil {
		return Asset{}, err
	}
	a.Prompt = prompt.String
	a.ContentHash = contentHash.String
	t, err := sqlitedb.ParseTime(createdAt.String)
	if err != nil {
		return Asset{}, err
	}
	a.CreatedAt = t
	u, err := sqlitedb.ParseTime(lastUsedAt.String)
	if err != nil {
		return Asset{}, err
	}
	a.LastUsedAt = u
	return a, nil
}

// FindOldestByHash answers the oldest row carrying the hash (48 号票上传
// 复用的查重点,与 MySQL 版同语义)。
func (s *SQLiteStore) FindOldestByHash(ctx context.Context, contentHash string) (Asset, error) {
	row := s.DB.QueryRowContext(ctx,
		`SELECT `+assetColumns+` FROM assets WHERE content_hash = ? ORDER BY id ASC LIMIT 1`, contentHash)
	a, err := scanSQLiteAsset(row)
	if errors.Is(err, sql.ErrNoRows) {
		return Asset{}, ErrNotFound
	}
	return a, err
}

// CountOthersOnObjectKey answers the sibling-row count behind one object key
// (48 号票删除防护)。
func (s *SQLiteStore) CountOthersOnObjectKey(ctx context.Context, objectKey string, excludeID int64) (int, error) {
	var n int
	if err := s.DB.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM assets WHERE object_key = ? AND id <> ?`, objectKey, excludeID).Scan(&n); err != nil {
		return 0, err
	}
	return n, nil
}

// ListWithoutHash pages un-hashed rows with an object key (48 号票回填的
// 批次查询,游标 = afterID,与 MySQL 版同语义)。
func (s *SQLiteStore) ListWithoutHash(ctx context.Context, afterID int64, limit int) ([]Asset, error) {
	rows, err := s.DB.QueryContext(ctx,
		`SELECT `+assetColumns+` FROM assets
		 WHERE content_hash = '' AND object_key <> '' AND id > ? ORDER BY id ASC LIMIT ?`,
		afterID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanSQLiteAssets(rows)
}

// SetContentHash stamps one row's content hash (48 号票回填的落列)。
func (s *SQLiteStore) SetContentHash(ctx context.Context, id int64, contentHash string) error {
	_, err := s.DB.ExecContext(ctx, `UPDATE assets SET content_hash = ? WHERE id = ?`, contentHash, id)
	return err
}

// ListByHash answers every row of one hash group, oldest first (48 号票
// 回填的组内成员;首行即保留行)。
func (s *SQLiteStore) ListByHash(ctx context.Context, contentHash string) ([]Asset, error) {
	rows, err := s.DB.QueryContext(ctx,
		`SELECT `+assetColumns+` FROM assets WHERE content_hash = ? ORDER BY id ASC`, contentHash)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanSQLiteAssets(rows)
}

// DuplicateHashes answers the hashes carried by more than one row (48 号票
// 回填的合并候选)。
func (s *SQLiteStore) DuplicateHashes(ctx context.Context) ([]string, error) {
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

// SetObjectKey repoints one row at another object key (48 号票回填的改指)。
func (s *SQLiteStore) SetObjectKey(ctx context.Context, id int64, objectKey string) error {
	_, err := s.DB.ExecContext(ctx, `UPDATE assets SET object_key = ? WHERE id = ?`, objectKey, id)
	return err
}

// scanSQLiteAssets drains an assets query into rows (回填的批读共用).
func scanSQLiteAssets(rows *sql.Rows) ([]Asset, error) {
	var out []Asset
	for rows.Next() {
		a, err := scanSQLiteAsset(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}
