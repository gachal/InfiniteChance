package character

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"github.com/gachal/InfiniteChance/internal/sqlitedb"
)

// SQLiteStore backs Store with the 角色库三表 in the desktop's single app
// database (51 号票)。时间列说明见 canvas 包 store_sqlite.go:定宽 TEXT,
// 写入显式给 FormatTime。
type SQLiteStore struct {
	DB *sql.DB
}

func NewSQLiteStore(db *sql.DB) *SQLiteStore { return &SQLiteStore{DB: db} }

// SQLiteStore 与 MySQLStore 同构实现 character.Store。
var _ Store = (*SQLiteStore)(nil)

// 三表与 MySQL 版同形(SQLite 方言):表内不带索引子句,二级索引用独立的
// CREATE INDEX IF NOT EXISTS,名字沿用 MySQL 版索引名(48 号票同款纪律,
// asset 包先例)。voice_id(57 号票)是可空列,新库随 CREATE 落地、存量
// 库走 sqliteMigrations 幂等加列。空起步无其他迁移。
const sqliteCharactersSchema = `
CREATE TABLE IF NOT EXISTS characters (
	id          INTEGER NOT NULL PRIMARY KEY AUTOINCREMENT,
	ip_id       INTEGER NOT NULL,
	name        TEXT    NOT NULL,
	alias       TEXT    NOT NULL DEFAULT '',
	description TEXT    NOT NULL DEFAULT '',
	detail      TEXT    NOT NULL DEFAULT '',
	voice_id    INTEGER NULL,
	created_at  TEXT    NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%f000Z','now')),
	updated_at  TEXT    NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%f000Z','now'))
)`

const sqliteVariantsSchema = `
CREATE TABLE IF NOT EXISTS character_variants (
	id           INTEGER NOT NULL PRIMARY KEY AUTOINCREMENT,
	character_id INTEGER NOT NULL,
	name         TEXT    NOT NULL,
	note         TEXT    NOT NULL DEFAULT '',
	sort         INTEGER NOT NULL DEFAULT 0,
	created_at   TEXT    NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%f000Z','now')),
	updated_at   TEXT    NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%f000Z','now'))
)`

const sqliteImagesSchema = `
CREATE TABLE IF NOT EXISTS character_images (
	id           INTEGER NOT NULL PRIMARY KEY AUTOINCREMENT,
	character_id INTEGER NOT NULL,
	variant_id   INTEGER NOT NULL,
	asset_id     INTEGER NOT NULL,
	label        TEXT    NOT NULL DEFAULT '',
	sort         INTEGER NOT NULL DEFAULT 0
)`

var sqliteIndexes = []string{
	`CREATE INDEX IF NOT EXISTS idx_characters_ip ON characters (ip_id, id)`,
	`CREATE INDEX IF NOT EXISTS idx_character_variants_character ON character_variants (character_id, sort, id)`,
	`CREATE INDEX IF NOT EXISTS idx_character_images_character ON character_images (character_id, sort, id)`,
	`CREATE INDEX IF NOT EXISTS idx_character_images_variant ON character_images (variant_id)`,
}

// sqliteMigrations widens tables created by earlier desktop builds in place
// (57 号票的角色绑定音色列):CREATE TABLE IF NOT EXISTS 不会加宽已存在的
// 表,存量 app.db 靠 PRAGMA table_info 检查后 ALTER(canvastask 包先例)。
// Idempotent.
var sqliteMigrations = []struct{ column, ddl string }{
	{"voice_id", "ALTER TABLE characters ADD COLUMN voice_id INTEGER NULL"},
}

// EnsureSchema creates the three tables and their indexes when missing,
// then widens an existing characters table in place. Idempotent.
func (s *SQLiteStore) EnsureSchema(ctx context.Context) error {
	for _, ddl := range []string{sqliteCharactersSchema, sqliteVariantsSchema, sqliteImagesSchema} {
		if _, err := s.DB.ExecContext(ctx, ddl); err != nil {
			return err
		}
	}
	for _, ddl := range sqliteIndexes {
		if _, err := s.DB.ExecContext(ctx, ddl); err != nil {
			return err
		}
	}
	for _, m := range sqliteMigrations {
		rows, err := s.DB.QueryContext(ctx, `PRAGMA table_info(characters)`)
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
	return nil
}

const sqliteCharacterColumns = `id, ip_id, name, alias, description, detail, voice_id, created_at, updated_at`

// scanSQLiteCharacterRow 按列序扫描一行,TEXT 时间经 ParseTime 还原。
func scanSQLiteCharacterRow(row interface{ Scan(...any) error }) (Character, error) {
	var c Character
	var voiceID sql.NullInt64
	var createdAt, updatedAt string
	if err := row.Scan(&c.ID, &c.IPID, &c.Name, &c.Alias, &c.Description, &c.Detail, &voiceID, &createdAt, &updatedAt); err != nil {
		return Character{}, err
	}
	c.VoiceID = voiceID.Int64
	t, err := sqlitedb.ParseTime(createdAt)
	if err != nil {
		return Character{}, err
	}
	c.CreatedAt = t
	u, err := sqlitedb.ParseTime(updatedAt)
	if err != nil {
		return Character{}, err
	}
	c.UpdatedAt = u
	return c, nil
}

// touchCharacter bumps the character's updated_at(SQLite 没有 ON UPDATE,
// 写入显式带):变体/图动作让角色在卡片墙按最近活动浮上来。
func (s *SQLiteStore) touchCharacter(ctx context.Context, id int64) {
	_, _ = s.DB.ExecContext(ctx,
		`UPDATE characters SET updated_at = ? WHERE id = ?`, sqlitedb.FormatTime(time.Now()), id)
}

// loadTree assembles one character's variants and images (sort+id 序)。
func (s *SQLiteStore) loadTree(ctx context.Context, c *Character) error {
	vrows, err := s.DB.QueryContext(ctx, `SELECT id, character_id, name, note, sort, created_at, updated_at
		FROM character_variants WHERE character_id = ? ORDER BY sort ASC, id ASC`, c.ID)
	if err != nil {
		return err
	}
	defer vrows.Close()
	for vrows.Next() {
		var v Variant
		var createdAt, updatedAt string
		if err := vrows.Scan(&v.ID, &v.CharacterID, &v.Name, &v.Note, &v.Sort, &createdAt, &updatedAt); err != nil {
			return err
		}
		if v.CreatedAt, err = sqlitedb.ParseTime(createdAt); err != nil {
			return err
		}
		if v.UpdatedAt, err = sqlitedb.ParseTime(updatedAt); err != nil {
			return err
		}
		v.Images = []Image{}
		c.Variants = append(c.Variants, v)
	}
	if err := vrows.Err(); err != nil {
		return err
	}

	irows, err := s.DB.QueryContext(ctx, `SELECT id, character_id, variant_id, asset_id, label, sort
		FROM character_images WHERE character_id = ? ORDER BY sort ASC, id ASC`, c.ID)
	if err != nil {
		return err
	}
	defer irows.Close()
	for irows.Next() {
		var img Image
		if err := irows.Scan(&img.ID, &img.CharacterID, &img.VariantID, &img.AssetID, &img.Label, &img.Sort); err != nil {
			return err
		}
		for i := range c.Variants {
			if c.Variants[i].ID == img.VariantID {
				c.Variants[i].Images = append(c.Variants[i].Images, img)
				break
			}
		}
	}
	return irows.Err()
}

func (s *SQLiteStore) CreateCharacter(ctx context.Context, ipID int64, in CharacterInput) (Character, error) {
	now := sqlitedb.FormatTime(time.Now())
	res, err := s.DB.ExecContext(ctx,
		`INSERT INTO characters (ip_id, name, alias, description, detail, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		ipID, in.Name, in.Alias, in.Description, in.Detail, now, now)
	if err != nil {
		return Character{}, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return Character{}, err
	}
	// 变体纪律:建角色自动带「默认」变体(可改名;最后一个不可删)。
	if _, err := s.DB.ExecContext(ctx,
		`INSERT INTO character_variants (character_id, name, note, sort, created_at, updated_at) VALUES (?, '默认', '', 0, ?, ?)`,
		id, now, now); err != nil {
		return Character{}, err
	}
	return s.GetCharacter(ctx, id)
}

func (s *SQLiteStore) ListCharacters(ctx context.Context, ipID int64) ([]Character, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT `+sqliteCharacterColumns+`
		FROM characters WHERE ip_id = ? ORDER BY updated_at DESC, id DESC`, ipID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []Character
	byID := map[int64]*Character{}
	for rows.Next() {
		c, err := scanSQLiteCharacterRow(rows)
		if err != nil {
			return nil, err
		}
		c.Variants = []Variant{}
		c.CoverAssetID = 0
		out = append(out, c)
		byID[c.ID] = &out[len(out)-1]
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if err := s.attachTree(ctx, ipID, byID); err != nil {
		return nil, err
	}
	for i := range out {
		out[i].CoverAssetID = CoverAssetID(out[i])
	}
	return out, nil
}

// attachTree 一次把一批角色的变体与图取全(join characters 限本 IP,
// sort+id 序),按 variant_id 挂图。变体先收进 map(指针稳定)再按角色
// 的 sort+id 序装回 —— 直接持有指向 slice 元素的指针会在后续 append 扩容
// 时失效,图会挂进旧底层数组。
func (s *SQLiteStore) attachTree(ctx context.Context, ipID int64, byID map[int64]*Character) error {
	if len(byID) == 0 {
		return nil
	}
	vrows, err := s.DB.QueryContext(ctx, `SELECT v.id, v.character_id, v.name, v.note, v.sort, v.created_at, v.updated_at
		FROM character_variants v JOIN characters c ON v.character_id = c.id
		WHERE c.ip_id = ? ORDER BY v.sort ASC, v.id ASC`, ipID)
	if err != nil {
		return err
	}
	defer vrows.Close()

	variantByID := map[int64]*Variant{}
	order := map[int64][]int64{}
	for vrows.Next() {
		var v Variant
		var createdAt, updatedAt string
		if err := vrows.Scan(&v.ID, &v.CharacterID, &v.Name, &v.Note, &v.Sort, &createdAt, &updatedAt); err != nil {
			return err
		}
		if v.CreatedAt, err = sqlitedb.ParseTime(createdAt); err != nil {
			return err
		}
		if v.UpdatedAt, err = sqlitedb.ParseTime(updatedAt); err != nil {
			return err
		}
		if _, ok := byID[v.CharacterID]; !ok {
			continue
		}
		v.Images = []Image{}
		variantByID[v.ID] = &v
		order[v.CharacterID] = append(order[v.CharacterID], v.ID)
	}
	if err := vrows.Err(); err != nil {
		return err
	}

	irows, err := s.DB.QueryContext(ctx, `SELECT i.id, i.character_id, i.variant_id, i.asset_id, i.label, i.sort
		FROM character_images i JOIN characters c ON i.character_id = c.id
		WHERE c.ip_id = ? ORDER BY i.sort ASC, i.id ASC`, ipID)
	if err != nil {
		return err
	}
	defer irows.Close()
	for irows.Next() {
		var img Image
		if err := irows.Scan(&img.ID, &img.CharacterID, &img.VariantID, &img.AssetID, &img.Label, &img.Sort); err != nil {
			return err
		}
		if v, ok := variantByID[img.VariantID]; ok {
			v.Images = append(v.Images, img)
		}
	}
	if err := irows.Err(); err != nil {
		return err
	}
	for cid, ids := range order {
		c, ok := byID[cid]
		if !ok {
			continue
		}
		for _, vid := range ids {
			c.Variants = append(c.Variants, *variantByID[vid])
		}
	}
	return nil
}

func (s *SQLiteStore) GetCharacter(ctx context.Context, id int64) (Character, error) {
	c, err := scanSQLiteCharacterRow(s.DB.QueryRowContext(ctx,
		`SELECT `+sqliteCharacterColumns+` FROM characters WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return Character{}, ErrCharacterNotFound
	}
	if err != nil {
		return Character{}, err
	}
	c.Variants = []Variant{}
	if err := s.loadTree(ctx, &c); err != nil {
		return Character{}, err
	}
	c.CoverAssetID = CoverAssetID(c)
	return c, nil
}

// UpdateCharacter applies the non-nil fields; 先探存在,updated_at 显式
// 带上(SQLite 没有 ON UPDATE CURRENT_TIMESTAMP)。
func (s *SQLiteStore) UpdateCharacter(ctx context.Context, id int64, u *CharacterUpdate) (Character, error) {
	if _, err := s.GetCharacter(ctx, id); err != nil {
		return Character{}, err
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
	if u.VoiceID != nil {
		// 0 = 解除绑定:可空列落 NULL,与「0 = 未绑定」的读回口径一致。
		if *u.VoiceID == 0 {
			sets = append(sets, "voice_id = NULL")
		} else {
			sets = append(sets, "voice_id = ?")
			args = append(args, *u.VoiceID)
		}
	}
	args = append(args, id)
	if _, err := s.DB.ExecContext(ctx,
		`UPDATE characters SET `+strings.Join(sets, ", ")+` WHERE id = ?`, args...); err != nil {
		return Character{}, err
	}
	return s.GetCharacter(ctx, id)
}

// DeleteCharacter 级联删图行与变体行(素材行不动 —— 素材在 asset 域,
// 引用不复制);单事务保证不留半删态。
func (s *SQLiteStore) DeleteCharacter(ctx context.Context, id int64) error {
	if _, err := s.GetCharacter(ctx, id); err != nil {
		return err
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	for _, stmt := range []string{
		`DELETE FROM character_images WHERE character_id = ?`,
		`DELETE FROM character_variants WHERE character_id = ?`,
		`DELETE FROM characters WHERE id = ?`,
	} {
		if _, err := tx.ExecContext(ctx, stmt, id); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// loadVariantRow reads one variant row (无图,不组树)。
func (s *SQLiteStore) loadVariantRow(ctx context.Context, id int64) (Variant, error) {
	var v Variant
	var createdAt, updatedAt string
	err := s.DB.QueryRowContext(ctx, `SELECT id, character_id, name, note, sort, created_at, updated_at
		FROM character_variants WHERE id = ?`, id).
		Scan(&v.ID, &v.CharacterID, &v.Name, &v.Note, &v.Sort, &createdAt, &updatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return Variant{}, ErrVariantNotFound
	}
	if err != nil {
		return Variant{}, err
	}
	if v.CreatedAt, err = sqlitedb.ParseTime(createdAt); err != nil {
		return Variant{}, err
	}
	if v.UpdatedAt, err = sqlitedb.ParseTime(updatedAt); err != nil {
		return Variant{}, err
	}
	return v, nil
}

func (s *SQLiteStore) GetVariant(ctx context.Context, id int64) (Variant, error) {
	return s.loadVariantRow(ctx, id)
}

func (s *SQLiteStore) CreateVariant(ctx context.Context, characterID int64, name, note string) (Variant, error) {
	var one int
	if err := s.DB.QueryRowContext(ctx, `SELECT 1 FROM characters WHERE id = ?`, characterID).Scan(&one); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Variant{}, ErrCharacterNotFound
		}
		return Variant{}, err
	}
	now := sqlitedb.FormatTime(time.Now())
	res, err := s.DB.ExecContext(ctx,
		`INSERT INTO character_variants (character_id, name, note, sort, created_at, updated_at) VALUES (?, ?, ?, 0, ?, ?)`,
		characterID, name, note, now, now)
	if err != nil {
		return Variant{}, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return Variant{}, err
	}
	s.touchCharacter(ctx, characterID)
	return s.loadVariantRow(ctx, id)
}

// UpdateVariant applies the non-nil fields; 变体更新顺带 bump 角色的
// updated_at(卡片墙按最近活动排序)。
func (s *SQLiteStore) UpdateVariant(ctx context.Context, id int64, u *VariantUpdate) (Variant, error) {
	v, err := s.loadVariantRow(ctx, id)
	if err != nil {
		return Variant{}, err
	}
	sets := []string{"updated_at = ?"}
	args := []any{sqlitedb.FormatTime(time.Now())}
	if u.Name != nil {
		sets = append(sets, "name = ?")
		args = append(args, *u.Name)
	}
	if u.Note != nil {
		sets = append(sets, "note = ?")
		args = append(args, *u.Note)
	}
	if u.Sort != nil {
		sets = append(sets, "sort = ?")
		args = append(args, *u.Sort)
	}
	args = append(args, id)
	if _, err := s.DB.ExecContext(ctx,
		`UPDATE character_variants SET `+strings.Join(sets, ", ")+` WHERE id = ?`, args...); err != nil {
		return Variant{}, err
	}
	s.touchCharacter(ctx, v.CharacterID)
	return s.loadVariantRow(ctx, id)
}

// DeleteVariant refuses when it is the character's only variant
// (ErrLastVariant —— 零变体角色没有资产载体);非最后变体的图行随级联消失。
func (s *SQLiteStore) DeleteVariant(ctx context.Context, id int64) error {
	v, err := s.loadVariantRow(ctx, id)
	if err != nil {
		return err
	}
	var count int
	if err := s.DB.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM character_variants WHERE character_id = ?`, v.CharacterID).Scan(&count); err != nil {
		return err
	}
	if count <= 1 {
		return ErrLastVariant
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	for _, stmt := range []string{
		`DELETE FROM character_images WHERE variant_id = ?`,
		`DELETE FROM character_variants WHERE id = ?`,
	} {
		if _, err := tx.ExecContext(ctx, stmt, id); err != nil {
			return err
		}
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	s.touchCharacter(ctx, v.CharacterID)
	return nil
}

// loadImageRow reads one image row.
func (s *SQLiteStore) loadImageRow(ctx context.Context, id int64) (Image, error) {
	var img Image
	err := s.DB.QueryRowContext(ctx, `SELECT id, character_id, variant_id, asset_id, label, sort
		FROM character_images WHERE id = ?`, id).
		Scan(&img.ID, &img.CharacterID, &img.VariantID, &img.AssetID, &img.Label, &img.Sort)
	if errors.Is(err, sql.ErrNoRows) {
		return Image{}, ErrImageNotFound
	}
	return img, err
}

func (s *SQLiteStore) GetImage(ctx context.Context, id int64) (Image, error) {
	return s.loadImageRow(ctx, id)
}

func (s *SQLiteStore) CreateImage(ctx context.Context, characterID int64, in ImageInput) (Image, error) {
	var one int
	if err := s.DB.QueryRowContext(ctx, `SELECT 1 FROM characters WHERE id = ?`, characterID).Scan(&one); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Image{}, ErrCharacterNotFound
		}
		return Image{}, err
	}
	// 变体必须属于本角色(不存在的变体答 ErrVariantNotFound,跨角色答
	// ErrVariantMismatch —— 两个 4xx 语义分开)。
	var owner int64
	if err := s.DB.QueryRowContext(ctx,
		`SELECT character_id FROM character_variants WHERE id = ?`, in.VariantID).Scan(&owner); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Image{}, ErrVariantNotFound
		}
		return Image{}, err
	}
	if owner != characterID {
		return Image{}, ErrVariantMismatch
	}
	res, err := s.DB.ExecContext(ctx,
		`INSERT INTO character_images (character_id, variant_id, asset_id, label, sort) VALUES (?, ?, ?, ?, ?)`,
		characterID, in.VariantID, in.AssetID, in.Label, in.Sort)
	if err != nil {
		return Image{}, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return Image{}, err
	}
	s.touchCharacter(ctx, characterID)
	return s.loadImageRow(ctx, id)
}

// UpdateImage applies the non-nil fields; 换变体必须同角色(ErrVariantMismatch)。
func (s *SQLiteStore) UpdateImage(ctx context.Context, id int64, u *ImageUpdate) (Image, error) {
	img, err := s.loadImageRow(ctx, id)
	if err != nil {
		return Image{}, err
	}
	if u.VariantID != nil {
		var owner int64
		if err := s.DB.QueryRowContext(ctx,
			`SELECT character_id FROM character_variants WHERE id = ?`, *u.VariantID).Scan(&owner); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return Image{}, ErrVariantNotFound
			}
			return Image{}, err
		}
		if owner != img.CharacterID {
			return Image{}, ErrVariantMismatch
		}
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
	if u.VariantID != nil {
		sets = append(sets, "variant_id = ?")
		args = append(args, *u.VariantID)
	}
	if len(sets) == 0 {
		return img, nil
	}
	args = append(args, id)
	if _, err := s.DB.ExecContext(ctx,
		`UPDATE character_images SET `+strings.Join(sets, ", ")+` WHERE id = ?`, args...); err != nil {
		return Image{}, err
	}
	s.touchCharacter(ctx, img.CharacterID)
	return s.loadImageRow(ctx, id)
}

// DeleteImage removes only the image row (素材行不动)。
func (s *SQLiteStore) DeleteImage(ctx context.Context, id int64) error {
	img, err := s.loadImageRow(ctx, id)
	if err != nil {
		return err
	}
	if _, err := s.DB.ExecContext(ctx, `DELETE FROM character_images WHERE id = ?`, id); err != nil {
		return err
	}
	s.touchCharacter(ctx, img.CharacterID)
	return nil
}
