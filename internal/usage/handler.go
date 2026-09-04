package usage

import (
	"encoding/json"
	"log"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/gachal/InfiniteChance/internal/apierr"
)

const (
	// defaultListLimit 与 maxListLimit 给分页设界:一页默认 50 条,
	// 最多 500 条,审计页靠 offset 翻页。
	defaultListLimit = 50
	maxListLimit     = 500
)

// Handlers serves the admin usage-audit endpoints. The gateway mounts them
// under /admin behind the JWT session middleware.
type Handlers struct {
	Store Store
}

// RegisterAdminRoutes mounts:
//
//	GET /admin/usage/logs    — request-level trail, filtered + paged
//	GET /admin/usage/summary — by=day|model|channel aggregation
//
// Both accept the same filters (from/to RFC3339, key_id, channel_id,
// model, status, source), so bucket numbers reconcile against the list.
func RegisterAdminRoutes(group *gin.RouterGroup, h *Handlers) {
	group.GET("/usage/logs", h.Logs)
	group.GET("/usage/summary", h.Summary)
}

// parseFilter compiles the shared query params into a Filter; it answers
// whether the response was already written (400 on a bad param).
func parseFilter(c *gin.Context) (Filter, bool) {
	var f Filter
	var ok bool
	if f.From, ok = parseTimeParam(c, "from"); !ok {
		return f, false
	}
	if f.To, ok = parseTimeParam(c, "to"); !ok {
		return f, false
	}
	if f.KeyID, ok = parsePositiveID(c, "key_id"); !ok {
		return f, false
	}
	if f.ChannelID, ok = parsePositiveID(c, "channel_id"); !ok {
		return f, false
	}
	f.Model = c.Query("model")
	if v := c.Query("status"); v != "" {
		if v != StatusSuccess && v != StatusUpstreamError {
			apierr.InvalidRequest(c, "status 必须是 success 或 upstream_error")
			return f, false
		}
		f.Status = v
	}
	if v := c.Query("source"); v != "" {
		if v != SourceCanvas && v != SourceDirect {
			apierr.InvalidRequest(c, "source 必须是 canvas、direct 或留空")
			return f, false
		}
		f.Source = v
	}
	return f, true
}

// parseTimeParam reads an RFC3339 instant; missing = nil (no bound).
// ok=false means the response was already written (400).
func parseTimeParam(c *gin.Context, name string) (*time.Time, bool) {
	v := c.Query(name)
	if v == "" {
		return nil, true
	}
	t, err := time.Parse(time.RFC3339, v)
	if err != nil {
		apierr.InvalidRequest(c, name+" 必须是 RFC3339 时间(如 2026-09-01T00:00:00Z)")
		return nil, false
	}
	return &t, true
}

// parsePositiveID reads a positive-int query param; missing = 0 (no
// filter). ok=false means the response was already written (400).
func parsePositiveID(c *gin.Context, name string) (int64, bool) {
	v := c.Query(name)
	if v == "" {
		return 0, true
	}
	id, err := strconv.ParseInt(v, 10, 64)
	if err != nil || id < 1 {
		apierr.InvalidRequest(c, name+" 必须是正整数")
		return 0, false
	}
	return id, true
}

func (h *Handlers) Logs(c *gin.Context) {
	f, ok := parseFilter(c)
	if !ok {
		return
	}
	limit, offset, ok := parsePage(c)
	if !ok {
		return
	}

	page, err := h.Store.List(c.Request.Context(), f, limit, offset)
	if err != nil {
		h.failInternal(c, err)
		return
	}
	logs := make([]logJSON, 0, len(page.Logs))
	for _, l := range page.Logs {
		logs = append(logs, toLogJSON(l))
	}
	c.JSON(http.StatusOK, logsResponse{Logs: logs, Total: page.Total})
}

func (h *Handlers) Summary(c *gin.Context) {
	var d Dimension
	switch c.Query("by") {
	case string(ByDay):
		d = ByDay
	case string(ByModel):
		d = ByModel
	case string(ByChannel):
		d = ByChannel
	default:
		apierr.InvalidRequest(c, "by 必须是 day、model 或 channel")
		return
	}
	f, ok := parseFilter(c)
	if !ok {
		return
	}

	buckets, err := h.Store.Summary(c.Request.Context(), d, f)
	if err != nil {
		h.failInternal(c, err)
		return
	}
	out := make([]bucketJSON, 0, len(buckets))
	for _, b := range buckets {
		out = append(out, toBucketJSON(b))
	}
	c.JSON(http.StatusOK, summaryResponse{Buckets: out})
}

// parsePage reads limit/offset with the same conventions as the asset
// list: missing limit = default, bad values are 400, an oversized limit is
// rejected rather than silently clamped.
func parsePage(c *gin.Context) (limit, offset int, ok bool) {
	limit = defaultListLimit
	if v := c.Query("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 {
			apierr.InvalidRequest(c, "limit 必须是正整数")
			return 0, 0, false
		}
		if n > maxListLimit {
			apierr.InvalidRequest(c, "limit 最多 500")
			return 0, 0, false
		}
		limit = n
	}
	if v := c.Query("offset"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 0 {
			apierr.InvalidRequest(c, "offset 必须是非负整数")
			return 0, 0, false
		}
		offset = n
	}
	return limit, offset, true
}

// requestFacts is the item track's billed quantity facts (尺寸与张数/秒数)。
type requestFacts struct {
	Size string `json:"size"`
	N    int64  `json:"n"`
}

// requestFactsOf extracts the billed facts from the price snapshot for
// call/second rows — the columns' token counts are always 0 there, the
// charge derives from snapshot.request. Token rows and unpriced failures
// answer nil.
func requestFactsOf(l Log) *requestFacts {
	if l.Unit != "call" && l.Unit != "second" {
		return nil
	}
	var snap struct {
		Request requestFacts `json:"request"`
	}
	if err := json.Unmarshal(l.PriceSnapshot, &snap); err != nil {
		return nil
	}
	if snap.Request.Size == "" && snap.Request.N == 0 {
		return nil
	}
	return &snap.Request
}

type logJSON struct {
	ID               int64         `json:"id"`
	KeyID            int64         `json:"key_id"`
	ChannelID        int64         `json:"channel_id"`
	ChannelName      string        `json:"channel_name"`
	PublicModel      string        `json:"public_model"`
	UpstreamModel    string        `json:"upstream_model"`
	Unit             string        `json:"unit"`
	PromptTokens     int64         `json:"prompt_tokens"`
	CompletionTokens int64         `json:"completion_tokens"`
	Request          *requestFacts `json:"request"`
	DurationMS       int64         `json:"duration_ms"`
	Status           string        `json:"status"`
	ChargeUSD        float64       `json:"charge_usd"`
	UpstreamError    string        `json:"upstream_error"`
	Source           string        `json:"source"`
	CreatedAt        time.Time     `json:"created_at"`
}

func toLogJSON(l Log) logJSON {
	return logJSON{
		ID: l.ID, KeyID: l.KeyID, ChannelID: l.ChannelID, ChannelName: l.ChannelName,
		PublicModel: l.PublicModel, UpstreamModel: l.UpstreamModel, Unit: l.Unit,
		PromptTokens: l.PromptTokens, CompletionTokens: l.CompletionTokens,
		Request: requestFactsOf(l), DurationMS: l.DurationMS, Status: l.Status,
		ChargeUSD:     float64(l.ChargeMicros) / 1e6,
		UpstreamError: l.UpstreamError, Source: l.Source, CreatedAt: l.CreatedAt,
	}
}

type logsResponse struct {
	Logs  []logJSON `json:"logs"`
	Total int64     `json:"total"`
}

type bucketJSON struct {
	Day         string  `json:"day,omitempty"`
	Model       string  `json:"model,omitempty"`
	ChannelID   int64   `json:"channel_id,omitempty"`
	ChannelName string  `json:"channel_name,omitempty"`
	Requests    int64   `json:"requests"`
	Errors      int64   `json:"errors"`
	ChargeUSD   float64 `json:"charge_usd"`
}

func toBucketJSON(b Bucket) bucketJSON {
	return bucketJSON{
		Day: b.Day, Model: b.Model, ChannelID: b.ChannelID, ChannelName: b.ChannelName,
		Requests: b.Requests, Errors: b.Errors,
		ChargeUSD: float64(b.ChargeMicros) / 1e6,
	}
}

type summaryResponse struct {
	Buckets []bucketJSON `json:"buckets"`
}

func (h *Handlers) failInternal(c *gin.Context, err error) {
	log.Printf("usage: %s %s: %v", c.Request.Method, c.Request.URL.Path, err)
	apierr.Internal(c, "服务内部错误,请稍后再试")
}
