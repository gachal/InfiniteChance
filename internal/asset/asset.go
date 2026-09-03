// Package asset is the persistent home of generated artifacts (10 号票):
// every successful canvas generation lands here as an asset row, independent
// of any canvas — nodes hold results by reference (task/asset id), so the
// library can outlive canvases and be reused across them (14 号票在库管理与
// 跨画布复用上继续). For now the row carries the artifact's address; the
// object-storage transfer of temporary upstream URLs is a later decision
// (CONTEXT.md 待补).
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
// the artifact address the node previews render — an upstream http(s) URL,
// or a data: URI when the vendor answered base64.
type Asset struct {
	ID        int64
	Kind      string
	CanvasID  int64
	TaskID    string
	Model     string
	Prompt    string
	URL       string
	CreatedAt time.Time
}

// Store persists assets. 10 号票只需要写入与按 id 取回;14 号票在此接口上
// 扩过滤列表、删除与存储抽象。
type Store interface {
	// Create stores one asset and returns it with ID and CreatedAt.
	Create(ctx context.Context, a Asset) (Asset, error)
	// Get returns the asset or ErrNotFound.
	Get(ctx context.Context, id int64) (Asset, error)
}
