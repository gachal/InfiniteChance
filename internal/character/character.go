// Package character stores the IP-level 角色库 (51 号票):characters as
// first-class IP entities (ADR 0004 定案 3 —— 角色是 IP 级实体,不是画布
// 节点), organized by 造型变体 (outfit variants) with 设定图 (settei
// images) as pure asset references. 用户领域口径:角色的主要资产是三视图/
// 多视图设定画,每换一次装即产生成套新视图 —— 变体是资产的一级组织维度。
//
// 变体纪律:创建角色自动带「默认」变体(可改名);最后一个变体不可删
// (ErrLastVariant —— 零变体角色没有资产载体)。label 前端白名单六类
// (三视图/多视图/表情集/色板/姿态/其他),后端存字符串不校验枚举;
// 视图类(三视图/多视图)是主资产,展示面排序置前(封面投影同规则)。
// 删除语义:删角色级联删变体与图行、素材行不动(引用不复制);被引素材
// 行被删后图行保留,UI 显示「素材不可用」占位可手动移除(与画布节点同
// 哲学,不做跨包级联清理)。
package character

import (
	"context"
	"errors"
	"time"
)

// ViewLabels 是视图类 label 的集合:主资产,画廊与卡片封面排序置前。
// 这只是展示层投影用的约定集(label 本身不校验枚举,画布类型注册表同
// 哲学);前端白名单六类的完整清单在 canvas/web(51 号票口径)。
var ViewLabels = map[string]bool{"三视图": true, "多视图": true}

var (
	// ErrCharacterNotFound reports a character id that has no row.
	ErrCharacterNotFound = errors.New("character: character not found")
	// ErrVariantNotFound reports a variant id that has no row.
	ErrVariantNotFound = errors.New("character: variant not found")
	// ErrImageNotFound reports an image id that has no row.
	ErrImageNotFound = errors.New("character: image not found")
	// ErrLastVariant reports DeleteVariant against the only remaining
	// variant: 最后一个变体不可删(409 variant_last_one)。
	ErrLastVariant = errors.New("character: last variant cannot be deleted")
	// ErrVariantMismatch reports an image whose target variant belongs to
	// another character: 变体必须属于同一角色(400)。
	ErrVariantMismatch = errors.New("character: variant belongs to another character")
)

// Character is one IP 级角色实体:角色圣经的载体与设定图锚的一致性源。
// Variants 随 Get/List 一次取全(单管理员量级小,画布侧目录一次拿整棵
// 树);CoverAssetID 是列表投影(规则见 CoverAssetID);0 = 全空(卡片走
// 占位)。
type Character struct {
	ID           int64
	IPID         int64
	Name         string
	Alias        string
	Description  string
	Detail       string
	CoverAssetID int64
	Variants     []Variant
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

// Variant is one 造型变体:换装 = 新建变体后成套上传新视图。Note 是服装
// 描述(自由文本);Sort 支持展示排序(同值按 id,即创建序)。
type Variant struct {
	ID          int64
	CharacterID int64
	Name        string
	Note        string
	Sort        int
	Images      []Image
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// Image is one 设定图:纯素材引用(asset_id),label/sort 随行。素材行被
// 删后本行保留,可用性由展示面按素材可达性判定。
type Image struct {
	ID          int64
	CharacterID int64
	VariantID   int64
	AssetID     int64
	Label       string
	Sort        int
}

// CharacterInput carries CreateCharacter's fields; alias/description/
// detail 可选(空串 = 未填)。
type CharacterInput struct {
	Name        string
	Alias       string
	Description string
	Detail      string
}

// CharacterUpdate carries UpdateCharacter's partial fields: nil = 不动。
type CharacterUpdate struct {
	Name        *string
	Alias       *string
	Description *string
	Detail      *string
}

// VariantUpdate carries UpdateVariant's partial fields: nil = 不动。
type VariantUpdate struct {
	Name *string
	Note *string
	Sort *int
}

// ImageInput carries CreateImage's fields; label/sort 可选(sort 缺省 0,
// 同值按 id 即创建序)。
type ImageInput struct {
	VariantID int64
	AssetID   int64
	Label     string
	Sort      int
}

// ImageUpdate carries UpdateImage's partial fields: nil = 不动;
// VariantID 显式给出即「换变体」,必须同角色(ErrVariantMismatch)。
type ImageUpdate struct {
	Label     *string
	Sort      *int
	VariantID *int64
}

// Store persists 角色库. 变体与图挂角色,角色严格限本 IP(创建带 ip_id,
// 列表按 ip_id 过滤 —— 消费面 52 号票在服务端另行校验 character.ip_id ==
// canvas.ip_id,本包不做「全局角色」)。
type Store interface {
	// CreateCharacter stores one character under ipID and seeds its
	// mandatory first variant 「默认」.
	CreateCharacter(ctx context.Context, ipID int64, in CharacterInput) (Character, error)
	// ListCharacters answers the IP's characters as a full tree
	// (角色 → 变体 → 图),角色 updated_at DESC,变体与图 sort+id。
	ListCharacters(ctx context.Context, ipID int64) ([]Character, error)
	// GetCharacter returns one character with its tree or
	// ErrCharacterNotFound.
	GetCharacter(ctx context.Context, id int64) (Character, error)
	// UpdateCharacter applies the non-nil fields of u; ErrCharacterNotFound
	// when there is no row.
	UpdateCharacter(ctx context.Context, id int64, u *CharacterUpdate) (Character, error)
	// DeleteCharacter removes the character row and cascades its variants
	// and image rows; asset rows are untouched (引用不复制).
	DeleteCharacter(ctx context.Context, id int64) error
	// CreateVariant adds one outfit variant under the character;
	// ErrCharacterNotFound when there is no character row.
	CreateVariant(ctx context.Context, characterID int64, name, note string) (Variant, error)
	// UpdateVariant applies the non-nil fields of u; ErrVariantNotFound.
	UpdateVariant(ctx context.Context, id int64, u *VariantUpdate) (Variant, error)
	// DeleteVariant removes the variant and cascades its image rows;
	// ErrLastVariant when it is the character's only one.
	DeleteVariant(ctx context.Context, id int64) error
	// CreateImage anchors one asset under a variant of the character;
	// ErrCharacterNotFound / ErrVariantMismatch(变体必须同角色).
	CreateImage(ctx context.Context, characterID int64, in ImageInput) (Image, error)
	// UpdateImage applies the non-nil fields of u; 换变体同角色校验同上;
	// ErrImageNotFound.
	UpdateImage(ctx context.Context, id int64, u *ImageUpdate) (Image, error)
	// DeleteImage removes only the image row (素材行不动).
	DeleteImage(ctx context.Context, id int64) error
	// GetImage returns one image row or ErrImageNotFound.
	GetImage(ctx context.Context, id int64) (Image, error)
	// GetVariant returns one variant row or ErrVariantNotFound.
	GetVariant(ctx context.Context, id int64) (Variant, error)
	// EnsureSchema creates the three tables when missing (MySQL 与 SQLite
	// 双方言,51 号票空起步无存量迁移,幂等).
	EnsureSchema(ctx context.Context) error
}

// CoverAssetID computes the card-wall cover projection for one assembled
// character(51 号票 API 节规则):逐变体(按 sort+id)扫描,首个有图的
// 变体内取排序最前的视图类图,无视图类图则取该变体第一张 —— 视图类优先
// 是变体内的事,变体序先于视图类;首个变体为空时向后续变体兜底,全部
// 变体皆空 = 0(卡片走占位)。视图类集合 = ViewLabels。
func CoverAssetID(c Character) int64 {
	for _, v := range c.Variants {
		var first int64
		for _, img := range v.Images {
			if ViewLabels[img.Label] {
				return img.AssetID
			}
			if first == 0 {
				first = img.AssetID
			}
		}
		if first != 0 {
			return first
		}
	}
	return 0
}
