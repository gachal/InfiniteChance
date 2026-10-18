// Package asset is the persistent home of generated artifacts (10 号票):
// every successful canvas generation lands here as an asset row, independent
// of any canvas — nodes hold results by reference (task/asset id), so the
// library can outlive canvases and be reused across them (14 号票的库管理
// 与跨画布复用在本包继续). 14 号票起产物落自有对象存储:转存成功的行为
// 携带 object_key/content_type/size_bytes,预存量的 url(厂商地址或 data:
// URI)保留作出处,预览与下载由 Content 路由就地兼容两代行。
package asset

import (
	"context"
	"errors"
	"strings"
	"time"
)

// Artifact kinds. 10 号票落了图片;12 号票的图生视频产物追加 video;
// 24 号票的视频参考音频追加 audio(上传素材进入,生成产物不产出音频)。
const (
	KindImage = "image"
	KindVideo = "video"
	KindAudio = "audio"
)

// ErrNotFound reports an asset id that has no row.
var ErrNotFound = errors.New("asset: not found")

// PublicAddress answers the object-storage public address of one asset:
// publicBase joined with its object key, when the row holds archived bytes
// and a public base is configured. 18 号票的「LLM 可达地址」解析顺序由此
// 起步:自有地址永久,优先于约 24h 过期的厂商原址(url 列回落);调用方
// 在两者皆无时按各自的错误形状拒绝。publicBase 来自 19 号票 settings 的
// public_base_url,未配置(空)时本函数恒答未命中,行为与升级前一致。
func PublicAddress(a Asset, publicBase string) (string, bool) {
	if a.ObjectKey == "" || publicBase == "" {
		return "", false
	}
	return strings.TrimRight(publicBase, "/") + "/" + strings.TrimLeft(a.ObjectKey, "/"), true
}

// Asset is one generated artifact. CanvasID and TaskID record its provenance
// (the originating canvas and the canvas task that produced it); Model and
// Prompt keep the generation facts for previews and future re-runs. URL is
// the artifact's original address — the vendor http(s) URL, or a data: URI
// when the vendor answered base64 — kept for provenance and vendor-facing
// reuse (图生视频的参考图解析). 转存后的自有字节走 ObjectKey. LastUsedAt
// 是「最近一次被服务端解引用」的时点(47 号票):初始 = 创建,任务参考与
// Agent 媒体解析成功时刷新,驱动挑选场景的最近使用排序。ContentHash 是字
// 节的 SHA-256 十六进制指纹(48 号票):判同去重用,不是下载完整性校验;
// 空 = 未算(历史行态),纯内部指纹,不进任何对外响应。
type Asset struct {
	ID          int64
	Kind        string
	CanvasID    int64
	TaskID      string
	Model       string
	Prompt      string
	URL         string
	ObjectKey   string
	ContentType string
	SizeBytes   int64
	ContentHash string
	CreatedAt   time.Time
	LastUsedAt  time.Time
}

// SortLastUsed asks List to order by last_used_at DESC (recently used
// first), id DESC breaking ties. The zero value keeps the historical
// newest-first (id DESC) order — 管理端审计视角不变.
const SortLastUsed = "last_used"

// Filter narrows List. Zero fields mean "no constraint"; Limit <= 0 means
// the handler default.
type Filter struct {
	Kind     string
	CanvasID int64
	Sort     string
	Limit    int
	Offset   int
}

// Listed is one List row: the asset plus the originating canvas's name.
// CanvasName is a display fact, not asset identity — the canvas row may be
// gone (素材比画布活得久), in which case it is empty.
type Listed struct {
	Asset
	CanvasName string
}

// Store persists assets. 14 号票在 10 号票的写入/取回上扩过滤列表与删除;
// 47 号票加 TouchLastUsed(解引用即算「用过」,供最近使用排序);48 号票
// 加 FindOldestByHash(上传字节复用的查重)与 CountOthersOnObjectKey
// (删除防护:共享对象的兄弟行计数)。
type Store interface {
	// Create stores one asset and returns it with ID and CreatedAt.
	Create(ctx context.Context, a Asset) (Asset, error)
	// Get returns the asset or ErrNotFound.
	Get(ctx context.Context, id int64) (Asset, error)
	// List answers assets newest first under the filter, with the
	// originating canvas name joined in.
	List(ctx context.Context, f Filter) ([]Listed, error)
	// Delete removes the row (the caller owns the object's lifecycle);
	// ErrNotFound when there is no row.
	Delete(ctx context.Context, id int64) error
	// TouchLastUsed stamps one asset's last_used_at to now (47 号票的
	// 「解引用即算」写入点);ErrNotFound when there is no row.
	TouchLastUsed(ctx context.Context, id int64) error
	// FindOldestByHash answers the oldest row carrying the content hash
	// (ORDER BY id, LIMIT 1) or ErrNotFound (48 号票上传复用的查重点)。
	FindOldestByHash(ctx context.Context, contentHash string) (Asset, error)
	// CountOthersOnObjectKey answers how many rows besides excludeID point
	// at the object key (48 号票删除防护:计数 > 0 = 字节仍有别的行共享,
	// 只删行不删对象)。
	CountOthersOnObjectKey(ctx context.Context, objectKey string, excludeID int64) (int, error)
}
