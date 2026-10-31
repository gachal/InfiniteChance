// Package scene stores the IP-level 场景库 (65 号票):scenes as IP 级资产
// 实体,把角色库(51 号票)扩成「资产库三类实体」的第二类。照 characters
// 族同构但**无造型变体**(票面定案 1):日景/夜景、局部等视图语义用
// images 的 label 表达,不上变体表;label 前端白名单五类(全景/日景/
// 夜景/局部/其他),后端存字符串不校验枚举(character_images 先例)。
//
// 图行是纯素材引用(asset_id):删场景级联删图行、素材行不动(引用不
// 复制);被引素材行被删后图行保留,UI 显示「素材不可用」占位可手动移除
// (角色库同哲学)。全景(establishing view)是场景的主资产,封面投影
// 置前(角色库视图类优先同规则)。
package scene

import (
	"context"
	"errors"
	"time"
)

// PrimaryLabels 是主资产 label 的集合:全景是场景的定场视图,画廊与卡片
// 封面排序置前。这只是展示层投影用的约定集(label 本身不校验枚举);前端
// 白名单五类的完整清单在 canvas/web(65 号票口径)。
var PrimaryLabels = map[string]bool{"全景": true}

var (
	// ErrSceneNotFound reports a scene id that has no row.
	ErrSceneNotFound = errors.New("scene: scene not found")
	// ErrImageNotFound reports an image id that has no row.
	ErrImageNotFound = errors.New("scene: image not found")
)

// Scene is one IP 级场景实体:跨镜复用的一致性源。Images 随 Get/List 一次
// 取全(单管理员量级小,目录一次拿全);CoverAssetID 是列表投影(规则见
// CoverAssetID);0 = 全空(卡片走占位)。场景无造型变体、无音色绑定
// (voice_id 类比显式不做,票面定案 6)。
type Scene struct {
	ID           int64
	IPID         int64
	Name         string
	Alias        string
	Description  string
	Detail       string
	CoverAssetID int64
	Images       []Image
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

// Image is one 场景设定图:纯素材引用(asset_id),label/sort 随行。
// 素材行被删后本行保留,可用性由展示面按素材可达性判定。
type Image struct {
	ID      int64
	SceneID int64
	AssetID int64
	Label   string
	Sort    int
}

// SceneInput carries CreateScene's fields; alias/description/detail 可选
// (空串 = 未填)。
type SceneInput struct {
	Name        string
	Alias       string
	Description string
	Detail      string
}

// SceneUpdate carries UpdateScene's partial fields: nil = 不动。
type SceneUpdate struct {
	Name        *string
	Alias       *string
	Description *string
	Detail      *string
}

// ImageInput carries CreateImage's fields; label/sort 可选(sort 缺省 0,
// 同值按 id 即创建序)。
type ImageInput struct {
	AssetID int64
	Label   string
	Sort    int
}

// ImageUpdate carries UpdateImage's partial fields: nil = 不动。无变体,
// 不存在「换挂」语义。
type ImageUpdate struct {
	Label *string
	Sort  *int
}

// Store persists 场景库. 图挂场景,场景严格限本 IP(创建带 ip_id,列表按
// ip_id 过滤 —— 消费面在服务端另行校验归属,本包不做「全局场景」)。
type Store interface {
	// CreateScene stores one scene under ipID.
	CreateScene(ctx context.Context, ipID int64, in SceneInput) (Scene, error)
	// ListScenes answers the IP's scenes as a full tree(场景 → 图),场景
	// updated_at DESC,图 sort+id。
	ListScenes(ctx context.Context, ipID int64) ([]Scene, error)
	// GetScene returns one scene with its images or ErrSceneNotFound.
	GetScene(ctx context.Context, id int64) (Scene, error)
	// UpdateScene applies the non-nil fields of u; ErrSceneNotFound when
	// there is no row.
	UpdateScene(ctx context.Context, id int64, u *SceneUpdate) (Scene, error)
	// DeleteScene removes the scene row and cascades its image rows; asset
	// rows are untouched (引用不复制).
	DeleteScene(ctx context.Context, id int64) error
	// CreateImage anchors one asset under the scene; ErrSceneNotFound when
	// there is no scene row.
	CreateImage(ctx context.Context, sceneID int64, in ImageInput) (Image, error)
	// UpdateImage applies the non-nil fields of u; ErrImageNotFound.
	UpdateImage(ctx context.Context, id int64, u *ImageUpdate) (Image, error)
	// DeleteImage removes only the image row (素材行不动).
	DeleteImage(ctx context.Context, id int64) error
	// GetImage returns one image row or ErrImageNotFound.
	GetImage(ctx context.Context, id int64) (Image, error)
	// EnsureSchema creates the two tables when missing (MySQL 与 SQLite
	// 双方言,65 号票空起步无存量迁移,幂等).
	EnsureSchema(ctx context.Context) error
}

// CoverAssetID computes the card-wall cover projection for one assembled
// scene(角色库 51 号票 API 节同款规则降一维):排序最前的「全景」图优先,
// 无全景图则取第一张,全空 = 0(卡片走占位)。主资产集合 = PrimaryLabels。
func CoverAssetID(s Scene) int64 {
	var first int64
	for _, img := range s.Images {
		if PrimaryLabels[img.Label] {
			return img.AssetID
		}
		if first == 0 {
			first = img.AssetID
		}
	}
	return first
}
