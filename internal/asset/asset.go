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
	"time"
)

// Artifact kinds. 10 号票落了图片;12 号票的图生视频产物追加 video。
const (
	KindImage = "image"
	KindVideo = "video"
)

// ErrNotFound reports an asset id that has no row.
var ErrNotFound = errors.New("asset: not found")

// Asset is one generated artifact. CanvasID and TaskID record its provenance
// (the originating canvas and the canvas task that produced it); Model and
// Prompt keep the generation facts for previews and future re-runs. URL is
// the artifact's original address — the vendor http(s) URL, or a data: URI
// when the vendor answered base64 — kept for provenance and vendor-facing
// reuse (图生视频的参考图解析). 转存后的自有字节走 ObjectKey.
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
	CreatedAt   time.Time
}

// Filter narrows List. Zero fields mean "no constraint"; Limit <= 0 means
// the handler default.
type Filter struct {
	Kind     string
	CanvasID int64
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

// Store persists assets. 14 号票在 10 号票的写入/取回上扩过滤列表与删除。
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
}
