// Package canvas implements the creator-facing canvas surface (09 号票):
// multi-canvas CRUD and whole-graph JSON persistence with a version-number
// optimistic lock. Nodes and edges are never rows — the graph lives in one
// JSON column and travels as a whole; auto-save debounces on the client and
// the version check catches lost updates (two tabs editing one canvas).
package canvas

import (
	"context"
	"errors"
	"time"
)

// ErrNotFound reports a canvas id that has no row.
var ErrNotFound = errors.New("canvas: not found")

// ErrVersionConflict reports a SaveGraph whose expected version no longer
// matches the stored row: another writer saved in between.
var ErrVersionConflict = errors.New("canvas: version conflict")

// Canvas is one persisted canvas. Graph is the whole-graph JSON document
// (vue-flow elements: nodes, edges). The server never interprets its shape,
// so the editor schema can evolve without a migration; the MySQL JSON column
// validates and normalizes it, so round-trips are document-equal, not
// byte-equal. IPID 是归属的 IP 企划(50 号票,NOT NULL——存量由启动自愈
// 迁移收编);Type 是画布类型的字符串标签,后端不校验枚举(注册表驱动,
// ADR 0004 定案 2);IPName 是展示事实(读路径 join ips 得到,写路径不
// 收它——IP 改名后旧快照里的名字自然过期,与 asset.Listed.CanvasName
// 同一哲学)。
type Canvas struct {
	ID        int64
	Name      string
	Graph     []byte
	Version   int64
	IPID      int64
	IPName    string
	Type      string
	CreatedAt time.Time
	UpdatedAt time.Time
}

// canvasJoinIP 是读路径的 ips join 片段:IPName 是展示事实,IP 行没了照
// 列(与素材-画布同一哲学)。ips 表由 canvas 域的 EnsureSchema 建出。
const canvasJoinIP = ` LEFT JOIN ips i ON i.id = c.ip_id`

// Store persists canvases. SaveGraph is the optimistic-lock seam: it stores
// the graph only when the stored version still equals expectedVersion,
// answering ErrVersionConflict otherwise. Create 必带 ipID 与画布类型
// (50 号票:画布必归属 IP);Move 只改归属、不回填历史素材行——历史产物
// 留在原 IP 名下,此后新产物随新 IP(历史行不清洗原则)。
type Store interface {
	// List answers canvases newest first; ipID > 0 narrows to one IP's
	// canvases (IP 详情页按类型分组的取数面)。
	List(ctx context.Context, ipID int64) ([]Canvas, error)
	Get(ctx context.Context, id int64) (Canvas, error)
	Create(ctx context.Context, ipID int64, name string, graph []byte, canvasType string) (Canvas, error)
	Rename(ctx context.Context, id int64, name string) (Canvas, error)
	// SetType retypes one canvas; the type string is stored verbatim (后端
	// 不校验枚举).
	SetType(ctx context.Context, id int64, canvasType string) (Canvas, error)
	// Move re-homes one canvas under another IP.
	Move(ctx context.Context, id, ipID int64) (Canvas, error)
	Delete(ctx context.Context, id int64) error
	SaveGraph(ctx context.Context, id int64, graph []byte, expectedVersion int64) (Canvas, error)
}
