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
	created_at   TEXT    NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%f000Z','now'))
)`

// sqliteIndexes 是 MySQL 版表内 KEY idx_… 的 SQLite 对应物:MySQL 的
// 二级索引长在 CREATE TABLE 里,SQLite 用独立的 CREATE INDEX IF NOT
// EXISTS,名字沿用 MySQL 版的索引名。
var sqliteIndexes = []string{
	`CREATE INDEX IF NOT EXISTS idx_assets_canvas ON assets (canvas_id, id)`,
	`CREATE INDEX IF NOT EXISTS idx_assets_task ON assets (task_id)`,
	`CREATE INDEX IF NOT EXISTS idx_assets_kind ON assets (kind, id)`,
}

// EnsureSchema creates the assets table and its indexes when missing. The
// desktop database starts from scratch, so the MySQL-side information_schema
// column migration has no counterpart here: 新建表自带全部列(对象存储三
// 列已在表里)。Idempotent.
func (s *SQLiteStore) EnsureSchema(ctx context.Context) error {
	if _, err := s.DB.ExecContext(ctx, sqliteSchema); err != nil {
		return err
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
	res, err := s.DB.ExecContext(ctx,
		`INSERT INTO assets (kind, canvas_id, task_id, model, prompt, url, object_key, content_type, size_bytes, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		a.Kind, a.CanvasID, a.TaskID, a.Model, prompt, a.URL, a.ObjectKey, a.ContentType, a.SizeBytes,
		sqlitedb.FormatTime(time.Now()))
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

// List answers assets newest first under the filter. The canvas join is a
// LEFT JOIN: 素材比画布活得久,画布行没了素材照列,名字空着即可。LIMIT/
// OFFSET 分页与 MySQL 版同形;listWhere 过滤拼接两边共用。
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
	where, args := listWhere(f)
	rows, err := s.DB.QueryContext(ctx,
		`SELECT a.id, a.kind, a.canvas_id, a.task_id, a.model, a.prompt, a.url,
			a.object_key, a.content_type, a.size_bytes, a.created_at, c.name
		 FROM assets a LEFT JOIN canvases c ON c.id = a.canvas_id`+
			where+` ORDER BY a.id DESC LIMIT ? OFFSET ?`,
		append(args, limit, offset)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []Listed
	for rows.Next() {
		var l Listed
		var prompt, createdAt, canvasName sql.NullString
		if err := rows.Scan(&l.ID, &l.Kind, &l.CanvasID, &l.TaskID, &l.Model, &prompt,
			&l.URL, &l.ObjectKey, &l.ContentType, &l.SizeBytes, &createdAt, &canvasName); err != nil {
			return nil, err
		}
		if l.CreatedAt, err = sqlitedb.ParseTime(createdAt.String); err != nil {
			return nil, err
		}
		l.Prompt = prompt.String
		l.CanvasName = canvasName.String
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

// scanSQLiteAsset 是 scanAsset 的 SQLite 版:created_at 是 TEXT,读出转回
// time.Time,保持与 MySQL 版相同的 Go 返回类型。
func scanSQLiteAsset(row rowScanner) (Asset, error) {
	var a Asset
	var prompt, createdAt sql.NullString
	if err := row.Scan(&a.ID, &a.Kind, &a.CanvasID, &a.TaskID, &a.Model, &prompt,
		&a.URL, &a.ObjectKey, &a.ContentType, &a.SizeBytes, &createdAt); err != nil {
		return Asset{}, err
	}
	a.Prompt = prompt.String
	t, err := sqlitedb.ParseTime(createdAt.String)
	if err != nil {
		return Asset{}, err
	}
	a.CreatedAt = t
	return a, nil
}
