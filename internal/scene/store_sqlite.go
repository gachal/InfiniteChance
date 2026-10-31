package scene

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"github.com/gachal/InfiniteChance/internal/sqlitedb"
)

// SQLiteStore backs Store with the 场景库两表 in the desktop's single app
// database (65 号票)。时间列说明见 canvas 包 store_sqlite.go:定宽 TEXT,
// 写入显式给 FormatTime。
type SQLiteStore struct {
	DB *sql.DB
}

func NewSQLiteStore(db *sql.DB) *SQLiteStore { return &SQLiteStore{DB: db} }

// SQLiteStore 与 MySQLStore 同构实现 scene.Store。
var _ Store = (*SQLiteStore)(nil)

// 两表与 MySQL 版同形(SQLite 方言):表内不带索引子句,二级索引用独立的
// CREATE INDEX IF NOT EXISTS,名字沿用 MySQL 版索引名(48 号票同款纪律,
// asset 包先例)。空起步无迁移(65 号票新表,无存量桌面库需要加列)。
const sqliteScenesSchema = `
CREATE TABLE IF NOT EXISTS scenes (
	id          INTEGER NOT NULL PRIMARY KEY AUTOINCREMENT,
	ip_id       INTEGER NOT NULL,
	name        TEXT    NOT NULL,
	alias       TEXT    NOT NULL DEFAULT '',
	description TEXT    NOT NULL DEFAULT '',
	detail      TEXT    NOT NULL DEFAULT '',
	created_at  TEXT    NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%f000Z','now')),
	updated_at  TEXT    NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%f000Z','now'))
)`

const sqliteImagesSchema = `
CREATE TABLE IF NOT EXISTS scene_images (
	id       INTEGER NOT NULL PRIMARY KEY AUTOINCREMENT,
	scene_id INTEGER NOT NULL,
	asset_id INTEGER NOT NULL,
	label    TEXT    NOT NULL DEFAULT '',
	sort     INTEGER NOT NULL DEFAULT 0
)`

var sqliteIndexes = []string{
	`CREATE INDEX IF NOT EXISTS idx_scenes_ip ON scenes (ip_id, id)`,
	`CREATE INDEX IF NOT EXISTS idx_scene_images_scene ON scene_images (scene_id, sort, id)`,
}

// EnsureSchema creates the two tables and their indexes when missing.
// Idempotent(先建表后建索引;本票无加列迁移,若后续加列须排在索引之前
// —— 角色库 57 号票先例)。
func (s *SQLiteStore) EnsureSchema(ctx context.Context) error {
	for _, ddl := range []string{sqliteScenesSchema, sqliteImagesSchema} {
		if _, err := s.DB.ExecContext(ctx, ddl); err != nil {
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

const sqliteSceneColumns = `id, ip_id, name, alias, description, detail, created_at, updated_at`

// scanSQLiteSceneRow 按列序扫描一行,TEXT 时间经 ParseTime 还原。
func scanSQLiteSceneRow(row interface{ Scan(...any) error }) (Scene, error) {
	var s Scene
	var createdAt, updatedAt string
	if err := row.Scan(&s.ID, &s.IPID, &s.Name, &s.Alias, &s.Description, &s.Detail, &createdAt, &updatedAt); err != nil {
		return Scene{}, err
	}
	t, err := sqlitedb.ParseTime(createdAt)
	if err != nil {
		return Scene{}, err
	}
	s.CreatedAt = t
	u, err := sqlitedb.ParseTime(updatedAt)
	if err != nil {
		return Scene{}, err
	}
	s.UpdatedAt = u
	return s, nil
}

// touchScene bumps the scene's updated_at(SQLite 没有 ON UPDATE,写入显式
// 带):图动作让场景在卡片墙按最近活动浮上来。
func (s *SQLiteStore) touchScene(ctx context.Context, id int64) {
	_, _ = s.DB.ExecContext(ctx,
		`UPDATE scenes SET updated_at = ? WHERE id = ?`, sqlitedb.FormatTime(time.Now()), id)
}

// loadTree assembles one scene's images (sort+id 序)。
func (s *SQLiteStore) loadTree(ctx context.Context, sc *Scene) error {
	rows, err := s.DB.QueryContext(ctx, `SELECT id, scene_id, asset_id, label, sort
		FROM scene_images WHERE scene_id = ? ORDER BY sort ASC, id ASC`, sc.ID)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var img Image
		if err := rows.Scan(&img.ID, &img.SceneID, &img.AssetID, &img.Label, &img.Sort); err != nil {
			return err
		}
		sc.Images = append(sc.Images, img)
	}
	return rows.Err()
}

// attachImages 一次把一批场景的图取全(join scenes 限本 IP,sort+id 序),
// 按 scene_id 挂图。byID 必须指向已收满的 out(指针稳定性,角色库同款
// 纪律 —— 边收边存会在 append 扩容时指向旧底层数组)。
func (s *SQLiteStore) attachImages(ctx context.Context, ipID int64, byID map[int64]*Scene) error {
	if len(byID) == 0 {
		return nil
	}
	rows, err := s.DB.QueryContext(ctx, `SELECT i.id, i.scene_id, i.asset_id, i.label, i.sort
		FROM scene_images i JOIN scenes c ON i.scene_id = c.id
		WHERE c.ip_id = ? ORDER BY i.sort ASC, i.id ASC`, ipID)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var img Image
		if err := rows.Scan(&img.ID, &img.SceneID, &img.AssetID, &img.Label, &img.Sort); err != nil {
			return err
		}
		if sc, ok := byID[img.SceneID]; ok {
			sc.Images = append(sc.Images, img)
		}
	}
	return rows.Err()
}

func (s *SQLiteStore) CreateScene(ctx context.Context, ipID int64, in SceneInput) (Scene, error) {
	now := sqlitedb.FormatTime(time.Now())
	res, err := s.DB.ExecContext(ctx,
		`INSERT INTO scenes (ip_id, name, alias, description, detail, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		ipID, in.Name, in.Alias, in.Description, in.Detail, now, now)
	if err != nil {
		return Scene{}, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return Scene{}, err
	}
	return s.GetScene(ctx, id)
}

func (s *SQLiteStore) ListScenes(ctx context.Context, ipID int64) ([]Scene, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT `+sqliteSceneColumns+`
		FROM scenes WHERE ip_id = ? ORDER BY updated_at DESC, id DESC`, ipID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []Scene
	for rows.Next() {
		sc, err := scanSQLiteSceneRow(rows)
		if err != nil {
			return nil, err
		}
		sc.Images = []Image{}
		sc.CoverAssetID = 0
		out = append(out, sc)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	// byID 必须在 out 收满后再建:边收边存 &out[len(out)-1] 会在后续
	// append 扩容时指向旧底层数组,attachImages 经旧指针把图挂进被丢弃的
	// 数组,先收入场景的整棵树丢失(多场景列表必现,角色库同款回归)。
	byID := make(map[int64]*Scene, len(out))
	for i := range out {
		byID[out[i].ID] = &out[i]
	}
	if err := s.attachImages(ctx, ipID, byID); err != nil {
		return nil, err
	}
	for i := range out {
		out[i].CoverAssetID = CoverAssetID(out[i])
	}
	return out, nil
}

func (s *SQLiteStore) GetScene(ctx context.Context, id int64) (Scene, error) {
	sc, err := scanSQLiteSceneRow(s.DB.QueryRowContext(ctx,
		`SELECT `+sqliteSceneColumns+` FROM scenes WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return Scene{}, ErrSceneNotFound
	}
	if err != nil {
		return Scene{}, err
	}
	sc.Images = []Image{}
	if err := s.loadTree(ctx, &sc); err != nil {
		return Scene{}, err
	}
	sc.CoverAssetID = CoverAssetID(sc)
	return sc, nil
}

// UpdateScene applies the non-nil fields; 先探存在,updated_at 显式带上
// (SQLite 没有 ON UPDATE CURRENT_TIMESTAMP)。
func (s *SQLiteStore) UpdateScene(ctx context.Context, id int64, u *SceneUpdate) (Scene, error) {
	if _, err := s.GetScene(ctx, id); err != nil {
		return Scene{}, err
	}
	sets := []string{"updated_at = ?"}
	args := []any{sqlitedb.FormatTime(time.Now())}
	if u.Name != nil {
		sets = append(sets, "name = ?")
		args = append(args, *u.Name)
	}
	if u.Alias != nil {
		sets = append(sets, "alias = ?")
		args = append(args, *u.Alias)
	}
	if u.Description != nil {
		sets = append(sets, "description = ?")
		args = append(args, *u.Description)
	}
	if u.Detail != nil {
		sets = append(sets, "detail = ?")
		args = append(args, *u.Detail)
	}
	args = append(args, id)
	if _, err := s.DB.ExecContext(ctx,
		`UPDATE scenes SET `+strings.Join(sets, ", ")+` WHERE id = ?`, args...); err != nil {
		return Scene{}, err
	}
	return s.GetScene(ctx, id)
}

// DeleteScene 级联删图行(素材行不动 —— 素材在 asset 域,引用不复制);
// 单事务保证不留半删态。
func (s *SQLiteStore) DeleteScene(ctx context.Context, id int64) error {
	if _, err := s.GetScene(ctx, id); err != nil {
		return err
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	for _, stmt := range []string{
		`DELETE FROM scene_images WHERE scene_id = ?`,
		`DELETE FROM scenes WHERE id = ?`,
	} {
		if _, err := tx.ExecContext(ctx, stmt, id); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// loadImageRow reads one image row.
func (s *SQLiteStore) loadImageRow(ctx context.Context, id int64) (Image, error) {
	var img Image
	err := s.DB.QueryRowContext(ctx, `SELECT id, scene_id, asset_id, label, sort
		FROM scene_images WHERE id = ?`, id).
		Scan(&img.ID, &img.SceneID, &img.AssetID, &img.Label, &img.Sort)
	if errors.Is(err, sql.ErrNoRows) {
		return Image{}, ErrImageNotFound
	}
	return img, err
}

func (s *SQLiteStore) GetImage(ctx context.Context, id int64) (Image, error) {
	return s.loadImageRow(ctx, id)
}

func (s *SQLiteStore) CreateImage(ctx context.Context, sceneID int64, in ImageInput) (Image, error) {
	var one int
	if err := s.DB.QueryRowContext(ctx, `SELECT 1 FROM scenes WHERE id = ?`, sceneID).Scan(&one); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Image{}, ErrSceneNotFound
		}
		return Image{}, err
	}
	res, err := s.DB.ExecContext(ctx,
		`INSERT INTO scene_images (scene_id, asset_id, label, sort) VALUES (?, ?, ?, ?)`,
		sceneID, in.AssetID, in.Label, in.Sort)
	if err != nil {
		return Image{}, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return Image{}, err
	}
	s.touchScene(ctx, sceneID)
	return s.loadImageRow(ctx, id)
}

// UpdateImage applies the non-nil fields; 图更新顺带 bump 场景的 updated_at
// (卡片墙按最近活动排序)。
func (s *SQLiteStore) UpdateImage(ctx context.Context, id int64, u *ImageUpdate) (Image, error) {
	img, err := s.loadImageRow(ctx, id)
	if err != nil {
		return Image{}, err
	}
	sets := []string{}
	args := []any{}
	if u.Label != nil {
		sets = append(sets, "label = ?")
		args = append(args, *u.Label)
	}
	if u.Sort != nil {
		sets = append(sets, "sort = ?")
		args = append(args, *u.Sort)
	}
	if len(sets) == 0 {
		return img, nil
	}
	args = append(args, id)
	if _, err := s.DB.ExecContext(ctx,
		`UPDATE scene_images SET `+strings.Join(sets, ", ")+` WHERE id = ?`, args...); err != nil {
		return Image{}, err
	}
	s.touchScene(ctx, img.SceneID)
	return s.loadImageRow(ctx, id)
}

// DeleteImage removes only the image row (素材行不动)。
func (s *SQLiteStore) DeleteImage(ctx context.Context, id int64) error {
	img, err := s.loadImageRow(ctx, id)
	if err != nil {
		return err
	}
	if _, err := s.DB.ExecContext(ctx, `DELETE FROM scene_images WHERE id = ?`, id); err != nil {
		return err
	}
	s.touchScene(ctx, img.SceneID)
	return nil
}
