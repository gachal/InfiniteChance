package prop

import (
	"context"
	"database/sql"
	"errors"
	"strings"
)

// MySQLStore backs Store with the 道具库两表 on the canvas database (65
// 号票)。时间列靠表默认值(CURRENT_TIMESTAMP(6) + ON UPDATE),读回即
// time.Time;两表自含,不 join 其他包的表。
type MySQLStore struct {
	DB *sql.DB
}

func NewMySQLStore(db *sql.DB) *MySQLStore { return &MySQLStore{DB: db} }

// MySQLStore 与 SQLiteStore 同构实现 prop.Store。
var _ Store = (*MySQLStore)(nil)

// 两表空起步(65 号票,无存量迁移):label 存字符串不校验枚举;detail
// TEXT 承载详细设定纯文本;索引服务列表树的取数路径,普通索引不加唯一
// (同 IP 重名允许)。空起步无 migrations(表自含,新库随 CREATE 落齐)。
const propsSchema = `
CREATE TABLE IF NOT EXISTS props (
	id          BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,
	ip_id       BIGINT UNSIGNED NOT NULL,
	name        VARCHAR(255) NOT NULL,
	alias       VARCHAR(255) NOT NULL DEFAULT '',
	description VARCHAR(1024) NOT NULL DEFAULT '',
	detail      TEXT NOT NULL,
	created_at  TIMESTAMP(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
	updated_at  TIMESTAMP(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6) ON UPDATE CURRENT_TIMESTAMP(6),
	KEY idx_props_ip (ip_id, id)
) ENGINE = InnoDB DEFAULT CHARSET = utf8mb4`

const imagesSchema = `
CREATE TABLE IF NOT EXISTS prop_images (
	id       BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,
	prop_id  BIGINT UNSIGNED NOT NULL,
	asset_id BIGINT UNSIGNED NOT NULL,
	label    VARCHAR(32) NOT NULL DEFAULT '',
	sort     INT NOT NULL DEFAULT 0,
	KEY idx_prop_images_prop (prop_id, sort, id)
) ENGINE = InnoDB DEFAULT CHARSET = utf8mb4`

// EnsureSchema creates the two tables when missing. Idempotent; 空起步无
// 同序依赖(表自含,装配位置随意,服务器侧随 characters 之后)。
func (s *MySQLStore) EnsureSchema(ctx context.Context) error {
	for _, ddl := range []string{propsSchema, imagesSchema} {
		if _, err := s.DB.ExecContext(ctx, ddl); err != nil {
			return err
		}
	}
	return nil
}

const mysqlPropColumns = `id, ip_id, name, alias, description, detail, created_at, updated_at`

func scanMySQLProp(row interface{ Scan(...any) error }) (Prop, error) {
	var p Prop
	err := row.Scan(&p.ID, &p.IPID, &p.Name, &p.Alias, &p.Description, &p.Detail, &p.CreatedAt, &p.UpdatedAt)
	return p, err
}

// touchProp bumps the prop's updated_at: 图动作让道具在卡墙(updated_at
// DESC 排序)按最近活动浮上来,与角色库「最近更新」哲学同源。尽力而为
// (时钟推进失败不拦主流程)。
func (s *MySQLStore) touchProp(ctx context.Context, id int64) {
	_, _ = s.DB.ExecContext(ctx, `UPDATE props SET updated_at = CURRENT_TIMESTAMP(6) WHERE id = ?`, id)
}

// loadTree assembles one prop's images (sort+id 序)。
func (s *MySQLStore) loadTree(ctx context.Context, p *Prop) error {
	rows, err := s.DB.QueryContext(ctx, `SELECT id, prop_id, asset_id, label, sort
		FROM prop_images WHERE prop_id = ? ORDER BY sort ASC, id ASC`, p.ID)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var img Image
		if err := rows.Scan(&img.ID, &img.PropID, &img.AssetID, &img.Label, &img.Sort); err != nil {
			return err
		}
		p.Images = append(p.Images, img)
	}
	return rows.Err()
}

// attachImages 一次把一批道具的图取全(join props 限本 IP,sort+id 序),
// 按 prop_id 挂图。byID 必须指向已收满的 out(指针稳定性,角色库同款
// 纪律 —— 边收边存会在 append 扩容时指向旧底层数组)。
func (s *MySQLStore) attachImages(ctx context.Context, ipID int64, byID map[int64]*Prop) error {
	if len(byID) == 0 {
		return nil
	}
	rows, err := s.DB.QueryContext(ctx, `SELECT i.id, i.prop_id, i.asset_id, i.label, i.sort
		FROM prop_images i JOIN props c ON i.prop_id = c.id
		WHERE c.ip_id = ? ORDER BY i.sort ASC, i.id ASC`, ipID)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var img Image
		if err := rows.Scan(&img.ID, &img.PropID, &img.AssetID, &img.Label, &img.Sort); err != nil {
			return err
		}
		if p, ok := byID[img.PropID]; ok {
			p.Images = append(p.Images, img)
		}
	}
	return rows.Err()
}

func (s *MySQLStore) CreateProp(ctx context.Context, ipID int64, in PropInput) (Prop, error) {
	res, err := s.DB.ExecContext(ctx,
		`INSERT INTO props (ip_id, name, alias, description, detail) VALUES (?, ?, ?, ?, ?)`,
		ipID, in.Name, in.Alias, in.Description, in.Detail)
	if err != nil {
		return Prop{}, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return Prop{}, err
	}
	return s.GetProp(ctx, id)
}

func (s *MySQLStore) ListProps(ctx context.Context, ipID int64) ([]Prop, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT `+mysqlPropColumns+`
		FROM props WHERE ip_id = ? ORDER BY updated_at DESC, id DESC`, ipID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []Prop
	for rows.Next() {
		p, err := scanMySQLProp(rows)
		if err != nil {
			return nil, err
		}
		p.Images = []Image{}
		p.CoverAssetID = 0
		out = append(out, p)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	// byID 必须在 out 收满后再建:边收边存 &out[len(out)-1] 会在后续
	// append 扩容时指向旧底层数组,attachImages 经旧指针把图挂进被丢弃的
	// 数组,先收入道具的整棵树丢失(多道具列表必现,角色库同款回归)。
	byID := make(map[int64]*Prop, len(out))
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

func (s *MySQLStore) GetProp(ctx context.Context, id int64) (Prop, error) {
	p, err := scanMySQLProp(s.DB.QueryRowContext(ctx,
		`SELECT `+mysqlPropColumns+` FROM props WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return Prop{}, ErrPropNotFound
	}
	if err != nil {
		return Prop{}, err
	}
	p.Images = []Image{}
	if err := s.loadTree(ctx, &p); err != nil {
		return Prop{}, err
	}
	p.CoverAssetID = CoverAssetID(p)
	return p, nil
}

// UpdateProp applies the non-nil fields; 先探存在(MySQL 同值更新算
// 0 affected,缺失语义靠探活路径对齐)。updated_at 由 ON UPDATE 自动推进。
func (s *MySQLStore) UpdateProp(ctx context.Context, id int64, u *PropUpdate) (Prop, error) {
	if _, err := s.GetProp(ctx, id); err != nil {
		return Prop{}, err
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
	if len(sets) == 0 {
		return s.GetProp(ctx, id)
	}
	args = append(args, id)
	if _, err := s.DB.ExecContext(ctx,
		`UPDATE props SET `+strings.Join(sets, ", ")+` WHERE id = ?`, args...); err != nil {
		return Prop{}, err
	}
	return s.GetProp(ctx, id)
}

// DeleteProp 级联删图行(素材行不动 —— 素材在 asset 域,引用不复制);
// 单事务保证不留半删态。
func (s *MySQLStore) DeleteProp(ctx context.Context, id int64) error {
	if _, err := s.GetProp(ctx, id); err != nil {
		return err
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	for _, stmt := range []string{
		`DELETE FROM prop_images WHERE prop_id = ?`,
		`DELETE FROM props WHERE id = ?`,
	} {
		if _, err := tx.ExecContext(ctx, stmt, id); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// loadImageRow reads one image row.
func (s *MySQLStore) loadImageRow(ctx context.Context, id int64) (Image, error) {
	var img Image
	err := s.DB.QueryRowContext(ctx, `SELECT id, prop_id, asset_id, label, sort
		FROM prop_images WHERE id = ?`, id).
		Scan(&img.ID, &img.PropID, &img.AssetID, &img.Label, &img.Sort)
	if errors.Is(err, sql.ErrNoRows) {
		return Image{}, ErrImageNotFound
	}
	return img, err
}

func (s *MySQLStore) GetImage(ctx context.Context, id int64) (Image, error) {
	return s.loadImageRow(ctx, id)
}

func (s *MySQLStore) CreateImage(ctx context.Context, propID int64, in ImageInput) (Image, error) {
	var one int
	if err := s.DB.QueryRowContext(ctx, `SELECT 1 FROM props WHERE id = ?`, propID).Scan(&one); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Image{}, ErrPropNotFound
		}
		return Image{}, err
	}
	res, err := s.DB.ExecContext(ctx,
		`INSERT INTO prop_images (prop_id, asset_id, label, sort) VALUES (?, ?, ?, ?)`,
		propID, in.AssetID, in.Label, in.Sort)
	if err != nil {
		return Image{}, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return Image{}, err
	}
	s.touchProp(ctx, propID)
	return s.loadImageRow(ctx, id)
}

// UpdateImage applies the non-nil fields; 图更新顺带 bump 道具的 updated_at
// (卡片墙按最近活动排序)。
func (s *MySQLStore) UpdateImage(ctx context.Context, id int64, u *ImageUpdate) (Image, error) {
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
		`UPDATE prop_images SET `+strings.Join(sets, ", ")+` WHERE id = ?`, args...); err != nil {
		return Image{}, err
	}
	s.touchProp(ctx, img.PropID)
	return s.loadImageRow(ctx, id)
}

// DeleteImage removes only the image row (素材行不动)。
func (s *MySQLStore) DeleteImage(ctx context.Context, id int64) error {
	img, err := s.loadImageRow(ctx, id)
	if err != nil {
		return err
	}
	if _, err := s.DB.ExecContext(ctx, `DELETE FROM prop_images WHERE id = ?`, id); err != nil {
		return err
	}
	s.touchProp(ctx, img.PropID)
	return nil
}
