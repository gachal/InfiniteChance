package canvas

import (
	"context"
	"errors"
	"time"
)

// ErrIPNotFound reports an IP id that has no row.
var ErrIPNotFound = errors.New("canvas: ip not found")

// ErrIPNotEmpty reports a DeleteIP against an IP that still owns canvases or
// directly-owned assets: 删除拦截,不级联不归档(ADR 0004 定案——级联删的
// 破坏半径不可接受,画布是核心资产)。
var ErrIPNotEmpty = errors.New("canvas: ip not empty")

// DefaultIPName is the self-heal migration's default container for legacy
// canvases. 收编条件只看「ips 表为空」,改名后不会重复建。
const DefaultIPName = "默认 IP"

// IP is one IP 企划:创作侧第一层容器——一个 ACG 企划/作品宇宙(50 号票,
// ADR 0004)。CoverAssetID 引用素材行(0 = 无封面);素材行被删后封面由
// 前端回落「渐变+首字」占位,后端无对象回收问题(节点-素材引用同哲学)。
// CanvasCount 与 LatestAt 是 List 的展示投影:名下画布数、IP 自身与名下
// 画布最近更新时间的较大者(卡片墙的「最近更新」与排序键同源)。
type IP struct {
	ID           int64
	Name         string
	Description  string
	CoverAssetID int64
	CanvasCount  int64
	LatestAt     time.Time
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

// IPUpdate carries UpdateIP's partial fields: nil = 不动该字段。
// CoverAssetID 显式给 0 = 清除封面。
type IPUpdate struct {
	Name         *string
	Description  *string
	CoverAssetID *int64
}

// IPStore persists IP 企划. DeleteIP is the guarded seam: 名下有画布或
// 有直接归属素材(assets.ip_id 指向该 IP)都答 ErrIPNotEmpty。
type IPStore interface {
	// ListIPs answers all IPs with their canvas counts, ordered by the
	// sort key (max of the IP's own updated_at and its canvases' newest
	// updated_at) newest first.
	ListIPs(ctx context.Context) ([]IP, error)
	// GetIP returns the IP or ErrIPNotFound.
	GetIP(ctx context.Context, id int64) (IP, error)
	// CreateIP stores one IP and returns it with ID and timestamps.
	CreateIP(ctx context.Context, name, description string, coverAssetID int64) (IP, error)
	// UpdateIP applies the non-nil fields of u; ErrIPNotFound when there
	// is no row.
	UpdateIP(ctx context.Context, id int64, u *IPUpdate) (IP, error)
	// DeleteIP removes the row; ErrIPNotEmpty when canvases or directly
	// owned assets still point at it, ErrIPNotFound when there is no row.
	DeleteIP(ctx context.Context, id int64) error
	// IPExists answers whether the id has a row — the canvas-create and
	// upload seams' existence check (ip_not_found 的裁决点)。
	IPExists(ctx context.Context, id int64) (bool, error)
	// EnsureSchema creates the ips table when missing and runs the
	// self-heal adoption: 仅当「存在 ip_id=0 的画布行且 ips 表为空」时
	// 建「默认 IP」收编存量画布;新库不预建,幂等。依赖 canvases.ip_id
	// 列已就位(canvas store 的 EnsureSchema 先行——main.go 装配顺序)。
	EnsureSchema(ctx context.Context) error
}
