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
// request never got priced.
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
	CreatedAt        time.Time
}

// Store persists usage rows. Ticket 04 only needs append; ticket 15 adds
// the filtered list and the per-day/model/channel aggregations.
type Store interface {
	// Insert stores one row and returns it with ID and CreatedAt.
	Insert(ctx context.Context, l Log) (Log, error)
}
