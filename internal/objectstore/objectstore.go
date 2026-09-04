// Package objectstore is the S3-compatible storage seam behind generated
// media (14 号票): keys name immutable objects, values are opaque bytes.
// The MVP driver is the local volume the spec pins down — 「本地卷存储 +
// S3 兼容接口抽象,为将来切 MinIO/云 OSS 预留」— so switching drivers later
// is a matter of implementing this interface, not of touching call sites.
//
// Keys are slash-separated paths and double as the archival layout:
// canvases/{canvasID}/{taskID}/{kind}.{ext} keeps 图片/视频按画布/任务归档.
package objectstore

import (
	"context"
	"io"
)

// Store is one object namespace. Objects are written once and never
// updated; drivers may treat Put-after-put as a replace.
type Store interface {
	// Put stores the object at key. size is the exact byte count of r
	// (S3-shaped APIs require it up front).
	Put(ctx context.Context, key string, r io.Reader, size int64, contentType string) error
	// Open returns a reader over the stored bytes; drivers answer an
	// error (fs.ErrNotFound-compatible) for unknown keys.
	Open(ctx context.Context, key string) (io.ReadCloser, error)
	// Delete removes the object; deleting an absent key is a no-op so
	// callers can retry row deletion without object-state preambles.
	Delete(ctx context.Context, key string) error
}
