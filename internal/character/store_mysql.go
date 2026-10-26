package character

import (
	"context"
	"database/sql"
	"errors"
	"strings"
)

// MySQLStore backs Store with the 角色库三表 on the canvas database
// (51 号票)。时间列靠表默认值(CURRENT_TIMESTAMP(6) + ON UPDATE),读回
// 即 time.Time;同库跨包只读 assets 的语义不存在 —— 本包三表自含。
type MySQLStore struct {
	DB *sql.DB
}

func NewMySQLStore(db *sql.DB) *MySQLStore { return &MySQLStore{DB: db} }

// MySQLStore 与 SQLiteStore 同构实现 character.Store。
var _ Store = (*MySQLStore)(nil)

// 三表空起步(51 号票,无存量迁移):label 存字符串不校验枚举;detail
// TEXT 承载详细设定纯文本;两索引服务列表树的取数路径,普通索引不加唯一
// (同 IP 重名允许)。voice_id(57 号票)是可空列,新库随 CREATE 落地、
// 存量库走 migrations 幂等加列。
const charactersSchema = `
CREATE TABLE IF NOT EXISTS characters (
	id          BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,
	ip_id       BIGINT UNSIGNED NOT NULL,
	name        VARCHAR(255) NOT NULL,
	alias       VARCHAR(255) NOT NULL DEFAULT '',
	description VARCHAR(1024) NOT NULL DEFAULT '',
	detail      TEXT NOT NULL,
	voice_id    BIGINT UNSIGNED NULL,
	created_at  TIMESTAMP(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
	updated_at  TIMESTAMP(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6) ON UPDATE CURRENT_TIMESTAMP(6),
	KEY idx_characters_ip (ip_id, id)
) ENGINE = InnoDB DEFAULT CHARSET = utf8mb4`

const variantsSchema = `
CREATE TABLE IF NOT EXISTS character_variants (
	id           BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,
	character_id BIGINT UNSIGNED NOT NULL,
	name         VARCHAR(255) NOT NULL,
	note         VARCHAR(1024) NOT NULL DEFAULT '',
	sort         INT NOT NULL DEFAULT 0,
	created_at   TIMESTAMP(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
	updated_at   TIMESTAMP(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6) ON UPDATE CURRENT_TIMESTAMP(6),
	KEY idx_character_variants_character (character_id, sort, id)
) ENGINE = InnoDB DEFAULT CHARSET = utf8mb4`

const imagesSchema = `
CREATE TABLE IF NOT EXISTS character_images (
	id           BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,
	character_id BIGINT UNSIGNED NOT NULL,
	variant_id   BIGINT UNSIGNED NOT NULL,
	asset_id     BIGINT UNSIGNED NOT NULL,
	label        VARCHAR(32) NOT NULL DEFAULT '',
	sort         INT NOT NULL DEFAULT 0,
	KEY idx_character_images_character (character_id, sort, id),
	KEY idx_character_images_variant (variant_id)
) ENGINE = InnoDB DEFAULT CHARSET = utf8mb4`

// migrations widens tables created by earlier tickets in place (57 号票的
// 角色绑定音色列):CREATE TABLE IF NOT EXISTS never widens an existing
// table, an already-deployed canvas DB must upgrade without a rebuild.
// Idempotent(canvases.ip_id 的 information_schema 先例)。
var migrations = []struct{ column, ddl string }{
	{"voice_id", "ALTER TABLE characters ADD COLUMN voice_id BIGINT UNSIGNED NULL"},
}

// EnsureSchema creates the three tables when missing and widens an existing
// characters table in place. Idempotent; 空起步无同序依赖(表自含,装配
// 位置随意,服务器侧随 ips 之后)。
func (s *MySQLStore) EnsureSchema(ctx context.Context) error {
	for _, ddl := range []string{charactersSchema, variantsSchema, imagesSchema} {
		if _, err := s.DB.ExecContext(ctx, ddl); err != nil {
			return err
		}
	}
	for _, m := range migrations {
		var count int
		if err := s.DB.QueryRowContext(ctx,
			`SELECT COUNT(*) FROM information_schema.COLUMNS
			 WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'characters' AND COLUMN_NAME = ?`,
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
	return nil
}

const mysqlCharacterColumns = `id, ip_id, name, alias, description, detail, voice_id, created_at, updated_at`

func scanMySQLCharacter(row interface{ Scan(...any) error }) (Character, error) {
	var c Character
	var voiceID sql.NullInt64
	err := row.Scan(&c.ID, &c.IPID, &c.Name, &c.Alias, &c.Description, &c.Detail, &voiceID, &c.CreatedAt, &c.UpdatedAt)
	if err != nil {
		return Character{}, err
	}
	c.VoiceID = voiceID.Int64
	return c, nil
}

// touchCharacter bumps the character's updated_at: 变体/图动作让角色在卡
// 片墙(updated_at DESC 排序)按最近活动浮上来,与 IP 层「最近更新」哲学
// 同源。尽力而为(时钟推进失败不拦主流程)。
func (s *MySQLStore) touchCharacter(ctx context.Context, id int64) {
	_, _ = s.DB.ExecContext(ctx, `UPDATE characters SET updated_at = CURRENT_TIMESTAMP(6) WHERE id = ?`, id)
}

// loadTree assembles one character's variants and images (sort+id 序)。
func (s *MySQLStore) loadTree(ctx context.Context, c *Character) error {
	vrows, err := s.DB.QueryContext(ctx, `SELECT id, character_id, name, note, sort, created_at, updated_at
		FROM character_variants WHERE character_id = ? ORDER BY sort ASC, id ASC`, c.ID)
	if err != nil {
		return err
	}
	defer vrows.Close()
	for vrows.Next() {
		var v Variant
		if err := vrows.Scan(&v.ID, &v.CharacterID, &v.Name, &v.Note, &v.Sort, &v.CreatedAt, &v.UpdatedAt); err != nil {
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

func (s *MySQLStore) CreateCharacter(ctx context.Context, ipID int64, in CharacterInput) (Character, error) {
	res, err := s.DB.ExecContext(ctx,
		`INSERT INTO characters (ip_id, name, alias, description, detail) VALUES (?, ?, ?, ?, ?)`,
		ipID, in.Name, in.Alias, in.Description, in.Detail)
	if err != nil {
		return Character{}, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return Character{}, err
	}
	// 变体纪律:建角色自动带「默认」变体(可改名;最后一个不可删)。
	if _, err := s.DB.ExecContext(ctx,
		`INSERT INTO character_variants (character_id, name, note, sort) VALUES (?, '默认', '', 0)`, id); err != nil {
		return Character{}, err
	}
	return s.GetCharacter(ctx, id)
}

func (s *MySQLStore) ListCharacters(ctx context.Context, ipID int64) ([]Character, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT `+mysqlCharacterColumns+`
		FROM characters WHERE ip_id = ? ORDER BY updated_at DESC, id DESC`, ipID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []Character
	byID := map[int64]*Character{}
	for rows.Next() {
		c, err := scanMySQLCharacter(rows)
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
func (s *MySQLStore) attachTree(ctx context.Context, ipID int64, byID map[int64]*Character) error {
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
		if err := vrows.Scan(&v.ID, &v.CharacterID, &v.Name, &v.Note, &v.Sort, &v.CreatedAt, &v.UpdatedAt); err != nil {
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

func (s *MySQLStore) GetCharacter(ctx context.Context, id int64) (Character, error) {
	c, err := scanMySQLCharacter(s.DB.QueryRowContext(ctx,
		`SELECT `+mysqlCharacterColumns+` FROM characters WHERE id = ?`, id))
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

// UpdateCharacter applies the non-nil fields; 先探存在(MySQL 同值更新算
// 0 affected,缺失语义靠探活路径对齐)。updated_at 由 ON UPDATE 自动推进。
func (s *MySQLStore) UpdateCharacter(ctx context.Context, id int64, u *CharacterUpdate) (Character, error) {
	if _, err := s.GetCharacter(ctx, id); err != nil {
		return Character{}, err
	}
	sets := []string{}
	args := []any{}
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
	if len(sets) == 0 {
		return s.GetCharacter(ctx, id)
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
func (s *MySQLStore) DeleteCharacter(ctx context.Context, id int64) error {
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
func (s *MySQLStore) loadVariantRow(ctx context.Context, id int64) (Variant, error) {
	var v Variant
	err := s.DB.QueryRowContext(ctx, `SELECT id, character_id, name, note, sort, created_at, updated_at
		FROM character_variants WHERE id = ?`, id).
		Scan(&v.ID, &v.CharacterID, &v.Name, &v.Note, &v.Sort, &v.CreatedAt, &v.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return Variant{}, ErrVariantNotFound
	}
	return v, err
}

func (s *MySQLStore) GetVariant(ctx context.Context, id int64) (Variant, error) {
	return s.loadVariantRow(ctx, id)
}

func (s *MySQLStore) CreateVariant(ctx context.Context, characterID int64, name, note string) (Variant, error) {
	var one int
	if err := s.DB.QueryRowContext(ctx, `SELECT 1 FROM characters WHERE id = ?`, characterID).Scan(&one); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Variant{}, ErrCharacterNotFound
		}
		return Variant{}, err
	}
	res, err := s.DB.ExecContext(ctx,
		`INSERT INTO character_variants (character_id, name, note, sort) VALUES (?, ?, ?, 0)`,
		characterID, name, note)
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
func (s *MySQLStore) UpdateVariant(ctx context.Context, id int64, u *VariantUpdate) (Variant, error) {
	v, err := s.loadVariantRow(ctx, id)
	if err != nil {
		return Variant{}, err
	}
	sets := []string{}
	args := []any{}
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
	if len(sets) == 0 {
		return v, nil
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
func (s *MySQLStore) DeleteVariant(ctx context.Context, id int64) error {
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
func (s *MySQLStore) loadImageRow(ctx context.Context, id int64) (Image, error) {
	var img Image
	err := s.DB.QueryRowContext(ctx, `SELECT id, character_id, variant_id, asset_id, label, sort
		FROM character_images WHERE id = ?`, id).
		Scan(&img.ID, &img.CharacterID, &img.VariantID, &img.AssetID, &img.Label, &img.Sort)
	if errors.Is(err, sql.ErrNoRows) {
		return Image{}, ErrImageNotFound
	}
	return img, err
}

func (s *MySQLStore) GetImage(ctx context.Context, id int64) (Image, error) {
	return s.loadImageRow(ctx, id)
}

func (s *MySQLStore) CreateImage(ctx context.Context, characterID int64, in ImageInput) (Image, error) {
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
func (s *MySQLStore) UpdateImage(ctx context.Context, id int64, u *ImageUpdate) (Image, error) {
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
func (s *MySQLStore) DeleteImage(ctx context.Context, id int64) error {
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
