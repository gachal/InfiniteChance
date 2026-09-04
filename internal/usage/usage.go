// Package usage persists the request-level usage log (用量日志): one
// immutable row per relayed request with channel, model, billed quantity,
// duration, status, charge and the pricing snapshot in force at request
// time. It is the only basis for reconciliation and audit (ticket 15 builds
// the admin views on top); failed requests are logged too, with an upstream
// error summary.
package usage

import (
	"context"
	"time"
)

// Log statuses. upstream_error covers every failure after the request left
// the gateway: non-2xx upstream answers, transport errors and cancellations
// (the summary column tells them apart).
const (
	StatusSuccess       = "success"
	StatusUpstreamError = "upstream_error"
)

// Log is one immutable usage row. ChannelName is snapshotted as text so the
// trail survives a channel deletion; PriceSnapshot is the JSON rendering of
// the pricing at request time (see pricing.Price.Snapshot), empty when the
// request never got priced. Source carries the caller's X-InfiniteChance-Source
// value verbatim (10 号票):canvas/server marks its gateway calls with the
// canvas origin so canvas spend is auditable apart from direct key traffic.
type Log struct {
	ID               int64
	KeyID            int64
	ChannelID        int64
	ChannelName      string
	PublicModel      string
	UpstreamModel    string
	Unit             string // pricing.Unit 值(token;call 轨 07 号票引入)
	PromptTokens     int64
	CompletionTokens int64
	DurationMS       int64
	Status           string
	ChargeMicros     int64
	PriceSnapshot    []byte // JSON, nil = 未计价
	UpstreamError    string // 空 = 无上游错误
	Source           string // 空 = 直连流量;画布侧为 canvas=… task=… node=…
	CreatedAt        time.Time
}

// Store persists usage rows. Insert is the relay's append path; List and
// Summary are ticket 15's audit reads — the filtered request trail and the
// per-day/model/channel aggregations that must reconcile with it.
type Store interface {
	// Insert stores one row and returns it with ID and CreatedAt.
	Insert(ctx context.Context, l Log) (Log, error)
	// List pages the trail, newest first, under f, with the exact-match
	// total for pagination.
	List(ctx context.Context, f Filter, limit, offset int) (Page, error)
	// Summary groups the trail by d under the same filter semantics as
	// List, so bucket numbers always reconcile against the log list.
	Summary(ctx context.Context, d Dimension, f Filter) ([]Bucket, error)
}

// Dimension selects a Summary grouping.
type Dimension string

const (
	ByDay     Dimension = "day"
	ByModel   Dimension = "model"
	ByChannel Dimension = "channel"
)

// SourceFilter values narrow the trail by origin: canvas marks (the
// canvas/server "canvas=…" prefix), direct key traffic, or everything.
const (
	SourceAll    = ""
	SourceCanvas = "canvas"
	SourceDirect = "direct"
)

// Filter narrows List and Summary to the same slice of the ledger. Zero
// values mean "no constraint"; Model and Status match exactly — audit
// numbers must reconcile, so no fuzzy predicates.
type Filter struct {
	From      *time.Time // created_at >= From
	To        *time.Time // created_at < To
	KeyID     int64      // >0 = 只看这个 key
	ChannelID int64      // >0 = 只看这个渠道
	Model     string     // 按公开模型精确匹配
	Status    string     // StatusSuccess / StatusUpstreamError
	Source    string     // SourceCanvas / SourceDirect / SourceAll
}

// Page is one List result: the rows and the total matching the filter
// (not just this page), so the UI can paginate without guessing.
type Page struct {
	Logs  []Log
	Total int64
}

// Bucket is one Summary row. Exactly the dimension fields of the requested
// Dimension are filled; Requests/Errors/ChargeMicros always are.
type Bucket struct {
	Day          string // ByDay:"2006-01-02"(库会话时区的自然日)
	Model        string // ByModel
	ChannelID    int64  // ByChannel
	ChannelName  string // ByChannel:该渠道行内的一枚名字快照(改名不分桶,非保证最新)
	Requests     int64
	Errors       int64 // Status = upstream_error 的请求数
	ChargeMicros int64
}
