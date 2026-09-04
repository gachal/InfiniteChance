package usage_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/gachal/InfiniteChance/internal/usage"
)

// fakeStore is the in-memory Store double: the handler tests run at the
// HTTP seam, capturing the filter the handler compiled from the query.
type fakeStore struct {
	gotFilter usage.Filter
	gotLimit  int
	gotOffset int
	gotBy     usage.Dimension
	page      usage.Page
	buckets   []usage.Bucket
	broken    bool
}

func (f *fakeStore) Insert(_ context.Context, l usage.Log) (usage.Log, error) {
	if f.broken {
		return usage.Log{}, errFakeBroken
	}
	l.ID = 1
	l.CreatedAt = time.Date(2026, 9, 4, 0, 0, 0, 0, time.UTC)
	return l, nil
}

func (f *fakeStore) List(_ context.Context, filter usage.Filter, limit, offset int) (usage.Page, error) {
	f.gotFilter, f.gotLimit, f.gotOffset = filter, limit, offset
	if f.broken {
		return usage.Page{}, errFakeBroken
	}
	return f.page, nil
}

func (f *fakeStore) Summary(_ context.Context, d usage.Dimension, filter usage.Filter) ([]usage.Bucket, error) {
	f.gotBy, f.gotFilter = d, filter
	if f.broken {
		return nil, errFakeBroken
	}
	return f.buckets, nil
}

var errFakeBroken = errors.New("store broken")

func newUsageServer(store *fakeStore) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	group := r.Group("/admin")
	usage.RegisterAdminRoutes(group, &usage.Handlers{Store: store})
	return r
}

func getUsage(r http.Handler, path string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, path, nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

type usageErrorBody struct {
	Error struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

func TestUsageLogsRejectsBadQueryParams(t *testing.T) {
	store := &fakeStore{}
	r := newUsageServer(store)

	bad := []string{
		"/admin/usage/logs?from=yesterday",
		"/admin/usage/logs?to=2026-13-40",
		"/admin/usage/logs?key_id=abc",
		"/admin/usage/logs?key_id=-1",
		"/admin/usage/logs?channel_id=0x1",
		"/admin/usage/logs?status=ok",
		"/admin/usage/logs?source=web",
		"/admin/usage/logs?limit=0",
		"/admin/usage/logs?limit=501",
		"/admin/usage/logs?offset=-1",
	}
	for _, path := range bad {
		w := getUsage(r, path)
		if w.Code != http.StatusBadRequest {
			t.Errorf("%s = %d, want 400", path, w.Code)
			continue
		}
		var body usageErrorBody
		if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil || body.Error.Code == "" {
			t.Errorf("%s: body is not the standard error shape: %s", path, w.Body.String())
		}
	}
}

func TestUsageLogsCompilesFilterFromQuery(t *testing.T) {
	store := &fakeStore{}
	r := newUsageServer(store)

	w := getUsage(r, "/admin/usage/logs"+
		"?from=2026-09-01T00:00:00Z&to=2026-09-04T00:00:00%2B08:00"+
		"&key_id=7&channel_id=3&model=deepseek-chat&status=upstream_error&source=canvas"+
		"&limit=25&offset=50")
	if w.Code != http.StatusOK {
		t.Fatalf("logs = %d, want 200: %s", w.Code, w.Body.String())
	}

	f := store.gotFilter
	if f.From == nil || !f.From.Equal(time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)) {
		t.Errorf("from = %v, want 2026-09-01T00:00:00Z", f.From)
	}
	if f.To == nil || f.To.UTC().Hour() != 16 || f.To.UTC().Day() != 3 {
		t.Errorf("to = %v, want +08:00 偏移被归一", f.To)
	}
	if f.KeyID != 7 || f.ChannelID != 3 || f.Model != "deepseek-chat" {
		t.Errorf("id/model filter = %d/%d/%q", f.KeyID, f.ChannelID, f.Model)
	}
	if f.Status != usage.StatusUpstreamError || f.Source != usage.SourceCanvas {
		t.Errorf("status/source = %q/%q", f.Status, f.Source)
	}
	if store.gotLimit != 25 || store.gotOffset != 50 {
		t.Errorf("limit/offset = %d/%d, want 25/50", store.gotLimit, store.gotOffset)
	}
}

func TestUsageLogsDefaultsLimitAndOmitsEmptyFilters(t *testing.T) {
	store := &fakeStore{}
	r := newUsageServer(store)

	if w := getUsage(r, "/admin/usage/logs"); w.Code != http.StatusOK {
		t.Fatalf("bare logs = %d, want 200", w.Code)
	}
	f := store.gotFilter
	if f.From != nil || f.To != nil || f.KeyID != 0 || f.ChannelID != 0 ||
		f.Model != "" || f.Status != "" || f.Source != "" {
		t.Errorf("bare filter = %+v, want 零值(不过滤)", f)
	}
	if store.gotLimit != 50 || store.gotOffset != 0 {
		t.Errorf("default limit/offset = %d/%d, want 50/0", store.gotLimit, store.gotOffset)
	}
}

func TestUsageLogsResponseShape(t *testing.T) {
	store := &fakeStore{
		page: usage.Page{
			Total: 2,
			Logs: []usage.Log{
				{
					ID: 12, KeyID: 7, ChannelID: 3, ChannelName: "deepseek-main",
					PublicModel: "deepseek-chat", UpstreamModel: "ds-up",
					Unit: "token", PromptTokens: 120, CompletionTokens: 340,
					DurationMS: 1234, Status: usage.StatusSuccess, ChargeMicros: 21250,
					CreatedAt: time.Date(2026, 9, 4, 8, 0, 0, 0, time.UTC),
				},
				{
					ID: 11, KeyID: 7, ChannelID: 4, ChannelName: "flux-main",
					PublicModel: "flux-pro", UpstreamModel: "flux-up",
					Unit: "call", DurationMS: 4000, Status: usage.StatusSuccess, ChargeMicros: 500,
					PriceSnapshot: []byte(`{"unit":"call","call":{"usd_per_call_micros":250000},"request":{"size":"1792x1024","n":2}}`),
					UpstreamError: "channel-a: 429; channel-b: conn refused",
					Source:        "canvas=1 task=ct_1 node=n1",
					CreatedAt:     time.Date(2026, 9, 4, 7, 0, 0, 0, time.UTC),
				},
			},
		},
	}
	r := newUsageServer(store)

	w := getUsage(r, "/admin/usage/logs")
	if w.Code != http.StatusOK {
		t.Fatalf("logs = %d, want 200", w.Code)
	}
	var body struct {
		Logs []struct {
			ID               int64  `json:"id"`
			KeyID            int64  `json:"key_id"`
			ChannelID        int64  `json:"channel_id"`
			ChannelName      string `json:"channel_name"`
			PublicModel      string `json:"public_model"`
			UpstreamModel    string `json:"upstream_model"`
			Unit             string `json:"unit"`
			PromptTokens     int64  `json:"prompt_tokens"`
			CompletionTokens int64  `json:"completion_tokens"`
			Request          *struct {
				Size string `json:"size"`
				N    int64  `json:"n"`
			} `json:"request"`
			DurationMS    int64     `json:"duration_ms"`
			Status        string    `json:"status"`
			ChargeUSD     float64   `json:"charge_usd"`
			UpstreamError string    `json:"upstream_error"`
			Source        string    `json:"source"`
			CreatedAt     time.Time `json:"created_at"`
		} `json:"logs"`
		Total int64 `json:"total"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode logs body: %v", err)
	}
	if body.Total != 2 || len(body.Logs) != 2 {
		t.Fatalf("body = %d logs, total %d, want 2/2", len(body.Logs), body.Total)
	}

	tokenRow, callRow := body.Logs[0], body.Logs[1]
	if tokenRow.ChargeUSD != 0.02125 {
		t.Errorf("token row charge_usd = %v, want 0.02125(微美元 → 美元)", tokenRow.ChargeUSD)
	}
	if tokenRow.Request != nil {
		t.Errorf("token row request = %+v, want null(数量在 token 列)", tokenRow.Request)
	}
	if callRow.Request == nil || callRow.Request.Size != "1792x1024" || callRow.Request.N != 2 {
		t.Errorf("call row request = %+v, want 快照里的 {1792x1024, 2}", callRow.Request)
	}
	if callRow.UpstreamError == "" || callRow.Source == "" {
		t.Errorf("call row error/source = %q/%q, want 换道摘要与画布标记", callRow.UpstreamError, callRow.Source)
	}
}

func TestUsageLogsStoreFailureAnswers500(t *testing.T) {
	store := &fakeStore{broken: true}
	r := newUsageServer(store)

	w := getUsage(r, "/admin/usage/logs")
	if w.Code != http.StatusInternalServerError {
		t.Errorf("broken store = %d, want 500", w.Code)
	}
}

func TestUsageSummaryValidatesDimension(t *testing.T) {
	store := &fakeStore{}
	r := newUsageServer(store)

	if w := getUsage(r, "/admin/usage/summary"); w.Code != http.StatusBadRequest {
		t.Errorf("missing by = %d, want 400", w.Code)
	}
	if w := getUsage(r, "/admin/usage/summary?by=week"); w.Code != http.StatusBadRequest {
		t.Errorf("by=week = %d, want 400", w.Code)
	}

	store.buckets = []usage.Bucket{{Day: "2026-09-04", Requests: 3, Errors: 1, ChargeMicros: 750}}
	w := getUsage(r, "/admin/usage/summary?by=day&key_id=7&source=direct")
	if w.Code != http.StatusOK {
		t.Fatalf("by=day = %d, want 200: %s", w.Code, w.Body.String())
	}
	if store.gotBy != usage.ByDay || store.gotFilter.KeyID != 7 || store.gotFilter.Source != usage.SourceDirect {
		t.Errorf("store got by=%v filter=%+v", store.gotBy, store.gotFilter)
	}
	if !strings.Contains(w.Body.String(), `"day":"2026-09-04"`) ||
		!strings.Contains(w.Body.String(), `"charge_usd":0.00075`) {
		t.Errorf("bucket body = %s, want day 与美元扣费", w.Body.String())
	}
}

func TestUsageSummaryAllDimensionsMapToStore(t *testing.T) {
	cases := map[string]usage.Dimension{
		"day":     usage.ByDay,
		"model":   usage.ByModel,
		"channel": usage.ByChannel,
	}
	for param, want := range cases {
		store := &fakeStore{}
		r := newUsageServer(store)
		if w := getUsage(r, "/admin/usage/summary?by="+param); w.Code != http.StatusOK {
			t.Errorf("by=%s = %d, want 200", param, w.Code)
		}
		if store.gotBy != want {
			t.Errorf("by=%s → store dimension %v, want %v", param, store.gotBy, want)
		}
	}
}
