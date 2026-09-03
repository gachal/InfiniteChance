package relay_test

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/gin-gonic/gin"
	mysqldriver "github.com/go-sql-driver/mysql"

	"github.com/gachal/InfiniteChance/internal/apikey"
	"github.com/gachal/InfiniteChance/internal/channel"
	"github.com/gachal/InfiniteChance/internal/pricing"
	"github.com/gachal/InfiniteChance/internal/relay"
	"github.com/gachal/InfiniteChance/internal/usage"
	"github.com/gachal/InfiniteChance/internal/videotask"
)

// openRelayTestDB brings up all four stores on one throwaway database of the
// compose MySQL (host port 3307), mirroring the gateway binary's wiring.
// MYSQL_TEST_DSN overrides the DSN. Tests skip when MySQL is unreachable so
// `go test ./...` stays green without infra.
type relayStores struct {
	channels *channel.MySQLStore
	keys     *apikey.MySQLStore
	prices   *pricing.MySQLStore
	usage    *usage.MySQLStore
	tasks    *videotask.MySQLStore
	db       *sql.DB
}

func openRelayTestDB(t *testing.T) *relayStores {
	t.Helper()

	dsn := os.Getenv("MYSQL_TEST_DSN")
	if dsn == "" {
		dsn = "root:infinitechance@tcp(localhost:3307)/infinitechance_test?parseTime=true"
	}
	cfg, err := mysqldriver.ParseDSN(dsn)
	if err != nil {
		t.Fatalf("parse MYSQL_TEST_DSN: %v", err)
	}
	// 每个测试包独占一个库:go test 会并行跑不同包的二进制,
	// 共库会让彼此的清理 DELETE 互删数据。
	dbName := cfg.DBName + "_relay"
	cfg.DBName = ""

	server, err := sql.Open("mysql", cfg.FormatDSN())
	if err != nil {
		t.Fatalf("open mysql server: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := server.PingContext(ctx); err != nil {
		t.Skipf("mysql unreachable, skipping relay tests: %v", err)
	}
	if _, err := server.ExecContext(ctx, "CREATE DATABASE IF NOT EXISTS `"+dbName+"`"); err != nil {
		t.Fatalf("create test database: %v", err)
	}

	cfg.DBName = dbName
	db, err := sql.Open("mysql", cfg.FormatDSN())
	if err != nil {
		t.Fatalf("open test database: %v", err)
	}
	stores := &relayStores{
		channels: channel.NewMySQLStore(db),
		keys:     apikey.NewMySQLStore(db),
		prices:   pricing.NewMySQLStore(db),
		usage:    usage.NewMySQLStore(db),
		tasks:    videotask.NewMySQLStore(db),
		db:       db,
	}
	for _, ensure := range []func(context.Context) error{
		stores.channels.EnsureSchema, stores.keys.EnsureSchema,
		stores.prices.EnsureSchema, stores.usage.EnsureSchema,
		stores.tasks.EnsureSchema,
	} {
		if err := ensure(ctx); err != nil {
			t.Fatalf("EnsureSchema: %v", err)
		}
	}
	for _, table := range []string{"api_key_quota_log", "api_keys", "channels", "model_prices", "usage_logs", "video_tasks"} {
		if _, err := db.ExecContext(ctx, "DELETE FROM "+table); err != nil {
			t.Fatalf("clean %s: %v", table, err)
		}
	}
	t.Cleanup(func() {
		db.Close()
		server.Close()
	})
	return stores
}

// fakeUpstream is an in-memory OpenAI-compatible vendor: it answers
// /chat/completions (or the images endpoints) with whatever its current
// handler produces and records the auth headers, paths and model names it
// saw. The request body is restored after the model peek so multipart
// handlers can re-read it.
type fakeUpstream struct {
	server *httptest.Server
	mu     sync.Mutex
	calls  int
	auth   []string
	paths  []string
	models []string
	handle http.HandlerFunc
}

func newFakeUpstream(t *testing.T, handle http.HandlerFunc) *fakeUpstream {
	t.Helper()
	f := &fakeUpstream{handle: handle}
	f.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		f.calls++
		f.auth = append(f.auth, r.Header.Get("Authorization"))
		f.paths = append(f.paths, r.URL.Path)
		raw, _ := io.ReadAll(r.Body)
		r.Body = io.NopCloser(bytes.NewReader(raw))
		var body struct {
			Model string `json:"model"`
		}
		_ = json.Unmarshal(raw, &body)
		f.models = append(f.models, body.Model)
		f.mu.Unlock()
		f.handle(w, r)
	}))
	t.Cleanup(f.server.Close)
	return f
}

func (f *fakeUpstream) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

// lastPath returns the most recent request path the vendor saw.
func (f *fakeUpstream) lastPath() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.paths) == 0 {
		return ""
	}
	return f.paths[len(f.paths)-1]
}

// okChatHandler answers one fixed successful completion.
func okChatHandler(content string, promptTokens, completionTokens int64) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"id":      "chatcmpl-fake",
			"object":  "chat.completion",
			"created": 1_700_000_000,
			"model":   "whatever-upstream-says",
			"choices": []any{map[string]any{
				"index":         0,
				"message":       map[string]any{"role": "assistant", "content": content},
				"finish_reason": "stop",
			}},
			"usage": map[string]any{
				"prompt_tokens":     promptTokens,
				"completion_tokens": completionTokens,
				"total_tokens":      promptTokens + completionTokens,
			},
		})
	}
}

// relayEnv is one fully wired relay surface for a test.
type relayEnv struct {
	stores   *relayStores
	handlers *relay.Handlers
	engine   *gin.Engine
}

func newRelayEnv(t *testing.T, adaptor relay.Adaptor) *relayEnv {
	t.Helper()
	stores := openRelayTestDB(t)
	gin.SetMode(gin.TestMode)
	r := gin.New()
	handlers := &relay.Handlers{
		Channels: stores.channels,
		Keys:     stores.keys,
		Prices:   stores.prices,
		Usage:    stores.usage,
		Tasks:    stores.tasks,
		Adaptor:  adaptor,
	}
	v1 := r.Group("/v1", apikey.RequireKey(stores.keys))
	relay.RegisterRoutes(v1, handlers)
	return &relayEnv{stores: stores, handlers: handlers, engine: r}
}

// seedChannel inserts one enabled channel mapping publicModel→upstreamModel
// pointed at baseURL.
func (e *relayEnv) seedChannel(t *testing.T, baseURL, publicModel, upstreamModel string) channel.Channel {
	t.Helper()
	return e.seedChannelFull(t, "fake-vendor", baseURL, publicModel, upstreamModel, 0, 0)
}

// seedChannelFull inserts one enabled channel with explicit identity and
// scheduling knobs (06 号票):name, priority, weight.
func (e *relayEnv) seedChannelFull(t *testing.T, name, baseURL, publicModel, upstreamModel string, priority, weight int) channel.Channel {
	t.Helper()
	ch, err := e.stores.channels.Create(context.Background(), channel.Channel{
		Name: name, Type: channel.TypeOpenAI, BaseURL: baseURL,
		APIKey: "vendor-secret-key", ModelMap: map[string]string{publicModel: upstreamModel},
		Priority: priority, Weight: weight, Enabled: true,
	})
	if err != nil {
		t.Fatalf("seed channel: %v", err)
	}
	return ch
}

// seedKey issues a real key with quotaMicros and returns the full value.
func (e *relayEnv) seedKey(t *testing.T, quotaMicros int64) (apikey.Key, string) {
	t.Helper()
	full, err := apikey.Generate()
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	k, err := e.stores.keys.Create(context.Background(), apikey.Key{
		Name: "tester", Prefix: apikey.PrefixOf(full), KeyHash: apikey.Hash(full),
		QuotaMicros: quotaMicros,
	})
	if err != nil {
		t.Fatalf("seed key: %v", err)
	}
	return k, full
}

// seedPrice writes the standard test price: in $1.25/M, out $10/M, ×1.5.
func (e *relayEnv) seedPrice(t *testing.T, publicModel string) {
	t.Helper()
	_, err := e.stores.prices.Upsert(context.Background(), pricing.Price{
		PublicModel: publicModel, Unit: pricing.UnitToken,
		Token: &pricing.TokenPrice{
			InputMicrosPerMTokens: 1_250_000, OutputMicrosPerMTokens: 10_000_000, RatioMicros: 1_500_000,
		},
	})
	if err != nil {
		t.Fatalf("seed price: %v", err)
	}
}

func (e *relayEnv) post(t *testing.T, fullKey, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+fullKey)
	w := httptest.NewRecorder()
	e.engine.ServeHTTP(w, req)
	return w
}

// openaiErrBody mirrors the OpenAI error object every relay rejection uses.
type openaiErrBody struct {
	Error struct {
		Message string  `json:"message"`
		Type    string  `json:"type"`
		Param   *string `json:"param"`
		Code    string  `json:"code"`
	} `json:"error"`
}

func decodeOpenAIError(t *testing.T, body []byte) openaiErrBody {
	t.Helper()
	var eb openaiErrBody
	if err := json.Unmarshal(body, &eb); err != nil {
		t.Fatalf("error body not JSON: %v (%s)", err, body)
	}
	return eb
}

func (e *relayEnv) balanceOf(t *testing.T, keyID int64) int64 {
	t.Helper()
	var balance int64
	if err := e.stores.db.QueryRowContext(context.Background(),
		`SELECT quota_micros FROM api_keys WHERE id = ?`, keyID).Scan(&balance); err != nil {
		t.Fatalf("read balance: %v", err)
	}
	return balance
}

func (e *relayEnv) quotaLog(t *testing.T, keyID int64) []apikey.QuotaEntry {
	t.Helper()
	entries, err := e.stores.keys.QuotaLog(context.Background(), keyID, 100)
	if err != nil {
		t.Fatalf("QuotaLog: %v", err)
	}
	return entries
}

func (e *relayEnv) usageRows(t *testing.T) []usage.Log {
	t.Helper()
	rows, err := e.stores.db.QueryContext(context.Background(),
		`SELECT key_id, channel_id, channel_name, public_model, upstream_model, unit,
		        prompt_tokens, completion_tokens, duration_ms, status, charge_micros,
		        price_snapshot, upstream_error, source
		 FROM usage_logs ORDER BY id ASC`)
	if err != nil {
		t.Fatalf("query usage_logs: %v", err)
	}
	defer rows.Close()
	var out []usage.Log
	for rows.Next() {
		var l usage.Log
		var snapshot []byte
		var upstreamErr, source *string
		if err := rows.Scan(&l.KeyID, &l.ChannelID, &l.ChannelName, &l.PublicModel, &l.UpstreamModel,
			&l.Unit, &l.PromptTokens, &l.CompletionTokens, &l.DurationMS, &l.Status,
			&l.ChargeMicros, &snapshot, &upstreamErr, &source); err != nil {
			t.Fatalf("scan usage row: %v", err)
		}
		l.PriceSnapshot = snapshot
		if upstreamErr != nil {
			l.UpstreamError = *upstreamErr
		}
		if source != nil {
			l.Source = *source
		}
		out = append(out, l)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("rows: %v", err)
	}
	return out
}

const testBody = `{"model":"public-m","messages":[{"role":"user","content":"你好,世界"}],"max_tokens":1000}`

func TestRelayChatCompletionsEndToEnd(t *testing.T) {
	env := newRelayEnv(t, nil) // default OpenAI adaptor
	upstream := newFakeUpstream(t, okChatHandler("来自厂商的回答", 120, 340))
	env.seedChannel(t, upstream.server.URL, "public-m", "upstream-m")
	key, full := env.seedKey(t, 1_000_000)
	env.seedPrice(t, "public-m")

	w := env.post(t, full, testBody)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d body %s, want 200", w.Code, w.Body.String())
	}

	// 客户端拿到归一化响应:model 回写公开名,内容来自厂商。
	var completion struct {
		Model   string `json:"model"`
		Choices []struct {
			Message struct {
				Role    string `json:"role"`
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
		Usage struct {
			PromptTokens     int64 `json:"prompt_tokens"`
			CompletionTokens int64 `json:"completion_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &completion); err != nil {
		t.Fatalf("response not JSON: %v (%s)", err, w.Body.String())
	}
	if completion.Model != "public-m" {
		t.Errorf("response model = %q, want rewritten public name", completion.Model)
	}
	if len(completion.Choices) != 1 || completion.Choices[0].Message.Content != "来自厂商的回答" {
		t.Errorf("choices = %+v, want the vendor answer", completion.Choices)
	}
	if completion.Usage.PromptTokens != 120 || completion.Usage.CompletionTokens != 340 {
		t.Errorf("usage passthrough = %+v, want 120/340", completion.Usage)
	}

	// 上游收到映射后的模型名与厂商密钥。
	if upstream.callCount() != 1 {
		t.Fatalf("upstream calls = %d, want 1", upstream.callCount())
	}
	if got := upstream.models[0]; got != "upstream-m" {
		t.Errorf("upstream model = %q, want mapped upstream-m", got)
	}
	if got := upstream.auth[0]; got != "Bearer vendor-secret-key" {
		t.Errorf("upstream auth = %q, want vendor key", got)
	}

	// 账务:实际扣费 = (120×1.25 + 340×10)×1.5 / 1e6 USD = 5325 micros。
	const wantActual = int64(5325)
	if balance := env.balanceOf(t, key.ID); balance != 1_000_000-wantActual {
		t.Errorf("balance = %d, want %d (initial minus actual charge)", balance, 1_000_000-wantActual)
	}
	entries := env.quotaLog(t, key.ID)
	if len(entries) != 3 {
		t.Fatalf("ledger = %d entries, want initial+estimate+settle", len(entries))
	}
	if entries[2].Reason != apikey.ReasonInitial {
		t.Errorf("entry 2 = %s, want initial", entries[2].Reason)
	}
	if entries[1].Reason != apikey.ReasonEstimate || entries[1].DeltaMicros >= 0 {
		t.Errorf("entry 1 = {%d %s}, want negative estimate", entries[1].DeltaMicros, entries[1].Reason)
	}
	// 结算差额 = 预扣 − 实际;预扣来自 estimate 条目。
	wantSettle := -entries[1].DeltaMicros - wantActual
	if entries[0].Reason != apikey.ReasonSettle || entries[0].DeltaMicros != wantSettle {
		t.Errorf("entry 0 = {%d %s}, want settle delta %d", entries[0].DeltaMicros, entries[0].Reason, wantSettle)
	}

	// 用量日志:渠道、模型、token、耗时、状态、扣费、倍率快照齐全。
	rows := env.usageRows(t)
	if len(rows) != 1 {
		t.Fatalf("usage rows = %d, want 1", len(rows))
	}
	row := rows[0]
	if row.KeyID != key.ID || row.ChannelName != "fake-vendor" ||
		row.PublicModel != "public-m" || row.UpstreamModel != "upstream-m" ||
		row.Unit != "token" || row.PromptTokens != 120 || row.CompletionTokens != 340 ||
		row.DurationMS < 0 || row.Status != usage.StatusSuccess || row.ChargeMicros != wantActual {
		t.Errorf("usage row = %+v, want complete success trail", row)
	}
	var snapshot struct {
		Unit  string `json:"unit"`
		Token struct {
			RatioMicros int64 `json:"ratio_micros"`
		} `json:"token"`
	}
	if err := json.Unmarshal(row.PriceSnapshot, &snapshot); err != nil {
		t.Fatalf("snapshot not JSON: %v (%s)", err, row.PriceSnapshot)
	}
	if snapshot.Unit != "token" || snapshot.Token.RatioMicros != 1_500_000 {
		t.Errorf("snapshot = %s, want token track at ratio 1.5", row.PriceSnapshot)
	}
}

func TestRelayInsufficientQuotaRejected(t *testing.T) {
	env := newRelayEnv(t, nil)
	upstream := newFakeUpstream(t, okChatHandler("should not be reached", 10, 10))
	env.seedChannel(t, upstream.server.URL, "public-m", "upstream-m")
	key, full := env.seedKey(t, 5) // 5 micros:连预扣都付不起
	env.seedPrice(t, "public-m")

	w := env.post(t, full, testBody)
	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d body %s, want 429", w.Code, w.Body.String())
	}
	eb := decodeOpenAIError(t, w.Body.Bytes())
	if eb.Error.Code != "insufficient_quota" || eb.Error.Type != "insufficient_quota" || eb.Error.Param != nil {
		t.Errorf("error object = %+v, want insufficient_quota with null param", eb.Error)
	}
	if eb.Error.Message == "" {
		t.Errorf("error message empty")
	}

	// 余额与流水原封不动,上游零调用,无用量日志。
	if balance := env.balanceOf(t, key.ID); balance != 5 {
		t.Errorf("balance = %d, want untouched 5", balance)
	}
	if entries := env.quotaLog(t, key.ID); len(entries) != 1 {
		t.Errorf("ledger = %d entries, want initial only", len(entries))
	}
	if upstream.callCount() != 0 {
		t.Errorf("upstream called %d times, want 0", upstream.callCount())
	}
	if rows := env.usageRows(t); len(rows) != 0 {
		t.Errorf("usage rows = %d, want none for a pre-billing rejection", len(rows))
	}
}

func TestRelayUpstreamFailureRefundsReserve(t *testing.T) {
	env := newRelayEnv(t, nil)
	upstream := newFakeUpstream(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusServiceUnavailable)
		w.Write([]byte(`{"error":{"message":"provider overloaded","type":"server_error","code":null}}`))
	})
	env.seedChannel(t, upstream.server.URL, "public-m", "upstream-m")
	key, full := env.seedKey(t, 1_000_000)
	env.seedPrice(t, "public-m")

	w := env.post(t, full, testBody)
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d body %s, want upstream 503 passthrough", w.Code, w.Body.String())
	}
	eb := decodeOpenAIError(t, w.Body.Bytes())
	if eb.Error.Code != "upstream_error" || !strings.Contains(eb.Error.Message, "provider overloaded") {
		t.Errorf("error object = %+v, want upstream_error carrying the vendor message", eb.Error)
	}

	// 预扣全额退回:余额回到初始,流水含 estimate + refund。
	if balance := env.balanceOf(t, key.ID); balance != 1_000_000 {
		t.Errorf("balance = %d, want fully refunded 1_000_000", balance)
	}
	entries := env.quotaLog(t, key.ID)
	if len(entries) != 3 || entries[0].Reason != apikey.ReasonRefund ||
		entries[1].Reason != apikey.ReasonEstimate {
		t.Fatalf("ledger = %+v, want refund reversing the estimate", entries)
	}
	if entries[0].DeltaMicros != -entries[1].DeltaMicros {
		t.Errorf("refund %d does not reverse estimate %d", entries[0].DeltaMicros, entries[1].DeltaMicros)
	}

	// 失败留痕:状态、上游错误摘要、零扣费。
	rows := env.usageRows(t)
	if len(rows) != 1 {
		t.Fatalf("usage rows = %d, want 1", len(rows))
	}
	row := rows[0]
	if row.Status != usage.StatusUpstreamError || row.ChargeMicros != 0 ||
		!strings.Contains(row.UpstreamError, "provider overloaded") {
		t.Errorf("failure trail = %+v, want upstream_error with vendor summary and zero charge", row)
	}
}

func TestRelayUnreachableUpstreamRefundsAndAnswers502(t *testing.T) {
	env := newRelayEnv(t, nil)
	dead := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	dead.Close() // 端口已关:必然连接失败
	env.seedChannel(t, dead.URL, "public-m", "upstream-m")
	key, full := env.seedKey(t, 1_000_000)
	env.seedPrice(t, "public-m")

	w := env.post(t, full, testBody)
	if w.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502", w.Code)
	}
	eb := decodeOpenAIError(t, w.Body.Bytes())
	if eb.Error.Code != "upstream_error" {
		t.Errorf("code = %q, want upstream_error", eb.Error.Code)
	}
	if balance := env.balanceOf(t, key.ID); balance != 1_000_000 {
		t.Errorf("balance = %d, want fully refunded", balance)
	}
	rows := env.usageRows(t)
	if len(rows) != 1 || rows[0].Status != usage.StatusUpstreamError || rows[0].UpstreamError == "" {
		t.Fatalf("failure trail = %+v, want one upstream_error row with summary", rows)
	}
}

func TestRelaySettlesUpWhenActualExceedsEstimate(t *testing.T) {
	env := newRelayEnv(t, nil)
	// max_tokens=1 压低预扣;上游却报了很大的真实用量 → 结算必须补扣。
	upstream := newFakeUpstream(t, okChatHandler("long answer", 2_000, 50_000))
	env.seedChannel(t, upstream.server.URL, "public-m", "upstream-m")
	key, full := env.seedKey(t, 10_000_000)
	env.seedPrice(t, "public-m")

	w := env.post(t, full, `{"model":"public-m","messages":[{"role":"user","content":"hi"}],"max_tokens":1}`)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d body %s, want 200", w.Code, w.Body.String())
	}
	// 实际 = (2000×1.25 + 50000×10)×1.5 / 1e6 USD = 0.75375 USD → 753_750 micros。
	const wantActual = int64(753_750)
	if balance := env.balanceOf(t, key.ID); balance != 10_000_000-wantActual {
		t.Errorf("balance = %d, want %d (settle topped up the shortfall)", balance, 10_000_000-wantActual)
	}
	rows := env.usageRows(t)
	if len(rows) != 1 || rows[0].ChargeMicros != wantActual {
		t.Errorf("usage rows = %+v, want charge %d", rows, wantActual)
	}
}

func TestRelayConcurrentRequestsNeverOverDeduct(t *testing.T) {
	env := newRelayEnv(t, nil)
	// 上游慢 100ms 且只报 20 completion tokens:预扣(每笔 15057
	// micros)在上游返回前都压在余额上,实际结算每笔只扣 300。
	upstream := newFakeUpstream(t, func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(100 * time.Millisecond)
		okChatHandler("tiny", 0, 20)(w, r)
	})
	env.seedChannel(t, upstream.server.URL, "public-m", "upstream-m")
	key, full := env.seedKey(t, 50_000) // 只够约 3 笔预扣
	env.seedPrice(t, "public-m")

	// 8 笔并发:无论怎样交错,余额不得为负,成功笔实扣总和必须与
	// 余额扣减一致(预扣拒绝的请求不留日志也不扣钱)。
	const concurrency = 8
	statuses := make([]int, concurrency)
	var wg sync.WaitGroup
	for i := range concurrency {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			statuses[i] = env.post(t, full, testBody).Code
		}(i)
	}
	wg.Wait()

	var ok, rejected int
	for _, s := range statuses {
		switch s {
		case http.StatusOK:
			ok++
		case http.StatusTooManyRequests:
			rejected++
		default:
			t.Fatalf("unexpected status %d", s)
		}
	}
	if ok == 0 {
		t.Fatalf("ok=0 rejected=%d, want at least one settlement", rejected)
	}
	balance := env.balanceOf(t, key.ID)
	if balance < 0 {
		t.Errorf("balance = %d, went negative", balance)
	}
	// 对账:初始 − 余额 = 成功笔实扣总和(每笔 20 tokens × $10/M × 1.5)。
	rows := env.usageRows(t)
	var charged int64
	for _, row := range rows {
		if row.Status == usage.StatusSuccess {
			charged += row.ChargeMicros
		}
	}
	if len(rows) != ok {
		t.Errorf("usage rows = %d, want one per settled request (%d)", len(rows), ok)
	}
	if 50_000-balance != charged {
		t.Errorf("reconciliation broken: drained %d, usage rows sum to %d", 50_000-balance, charged)
	}
	if want := int64(ok) * 300; charged != want {
		t.Errorf("charged = %d, want %d (300 micros × %d successes)", charged, want, ok)
	}
}

func TestRelayRequestValidation(t *testing.T) {
	env := newRelayEnv(t, nil)
	upstream := newFakeUpstream(t, okChatHandler("x", 1, 1))
	env.seedChannel(t, upstream.server.URL, "public-m", "upstream-m")
	_, full := env.seedKey(t, 1_000_000)
	env.seedPrice(t, "public-m")

	for _, tc := range []struct {
		name       string
		body       string
		wantStatus int
		wantCode   string
	}{
		{"missing model", `{"messages":[{"role":"user","content":"hi"}]}`, 400, "missing_model"},
		{"missing messages", `{"model":"public-m"}`, 400, "missing_messages"},
		{"not json", `{invalid`, 400, "invalid_request"},
		{"unknown model", `{"model":"no-such-m","messages":[{"role":"user","content":"hi"}]}`, 404, "model_not_found"},
		{"unpriced model", `{"model":"no-price-m","messages":[{"role":"user","content":"hi"}]}`, 400, "model_not_priced"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if tc.name == "unpriced model" {
				env.seedChannel(t, upstream.server.URL, "no-price-m", "upstream-m")
			}
			w := env.post(t, full, tc.body)
			if w.Code != tc.wantStatus {
				t.Fatalf("status = %d body %s, want %d", w.Code, w.Body.String(), tc.wantStatus)
			}
			eb := decodeOpenAIError(t, w.Body.Bytes())
			if eb.Error.Code != tc.wantCode || eb.Error.Type != "invalid_request_error" || eb.Error.Param != nil {
				t.Errorf("error object = %+v, want code %s", eb.Error, tc.wantCode)
			}
		})
	}

	// 禁用渠道的模型同样 404:选择器只考虑 enabled 渠道。
	if _, err := env.stores.channels.Create(context.Background(), channel.Channel{
		Name: "off", Type: channel.TypeOpenAI, BaseURL: upstream.server.URL,
		APIKey: "k", ModelMap: map[string]string{"disabled-m": "up"}, Enabled: false,
	}); err != nil {
		t.Fatalf("seed disabled channel: %v", err)
	}
	w := env.post(t, full, `{"model":"disabled-m","messages":[{"role":"user","content":"hi"}]}`)
	if w.Code != http.StatusNotFound {
		t.Fatalf("disabled-channel status = %d, want 404", w.Code)
	}
	// 以上全部拒绝,上游零调用。
	if upstream.callCount() != 0 {
		t.Errorf("upstream called %d times, want 0", upstream.callCount())
	}
}

func TestRelayPriorityPicksBestChannel(t *testing.T) {
	env := newRelayEnv(t, nil)
	upstream := newFakeUpstream(t, okChatHandler("picked", 10, 10))
	if _, err := env.stores.channels.Create(context.Background(), channel.Channel{
		Name: "backup", Type: channel.TypeOpenAI, BaseURL: upstream.server.URL,
		APIKey: "k1", ModelMap: map[string]string{"public-m": "up"}, Priority: 1, Enabled: true,
	}); err != nil {
		t.Fatalf("seed backup channel: %v", err)
	}
	primary, err := env.stores.channels.Create(context.Background(), channel.Channel{
		Name: "primary", Type: channel.TypeOpenAI, BaseURL: upstream.server.URL,
		APIKey: "k2", ModelMap: map[string]string{"public-m": "up"}, Priority: 10, Enabled: true,
	})
	if err != nil {
		t.Fatalf("seed primary channel: %v", err)
	}
	_, full := env.seedKey(t, 1_000_000)
	env.seedPrice(t, "public-m")

	w := env.post(t, full, testBody)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}
	rows := env.usageRows(t)
	if len(rows) != 1 || rows[0].ChannelName != "primary" || rows[0].ChannelID != primary.ID {
		t.Errorf("usage rows = %+v, want routed to the higher-priority channel", rows)
	}
}

func TestRelayAdaptorNormalization(t *testing.T) {
	// Adaptor 单元缝:归一化改写 model、抽取 usage、拒收非对象。
	a := relay.NewOpenAIAdaptor()
	body, u, err := a.Normalize("public-m", []byte(`{"model":"upstream-m","usage":{"prompt_tokens":7,"completion_tokens":8},"choices":[]}`))
	if err != nil {
		t.Fatalf("Normalize: %v", err)
	}
	var out map[string]any
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatalf("normalized not JSON: %v", err)
	}
	if out["model"] != "public-m" {
		t.Errorf("model = %v, want public-m", out["model"])
	}
	if u.PromptTokens != 7 || u.CompletionTokens != 8 {
		t.Errorf("usage = %+v, want 7/8", u)
	}
	if _, _, err := a.Normalize("m", []byte(`not-json`)); err == nil {
		t.Errorf("Normalize non-object = nil error, want failure")
	}
	if s := a.ErrorSummary([]byte(`{"error":{"message":"boom"}}`)); s != "boom" {
		t.Errorf("ErrorSummary = %q, want boom", s)
	}
	if s := a.ErrorSummary([]byte(`raw text`)); !strings.Contains(s, "raw text") {
		t.Errorf("ErrorSummary fallback = %q, want raw text", s)
	}
}

func TestRelayClientDisconnectMidFlightStillRefundsAndLogs(t *testing.T) {
	env := newRelayEnv(t, nil)
	// 上游慢 300ms:取消发生在转发进行中,预扣已落、结算未到。
	upstream := newFakeUpstream(t, func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(300 * time.Millisecond)
		okChatHandler("never delivered", 10, 10)(w, r)
	})
	env.seedChannel(t, upstream.server.URL, "public-m", "upstream-m")
	key, full := env.seedKey(t, 1_000_000)
	env.seedPrice(t, "public-m")

	ctx, cancel := context.WithCancel(context.Background())
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(testBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+full)
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()

	done := make(chan struct{})
	go func() {
		defer close(done)
		env.engine.ServeHTTP(w, req)
	}()
	time.Sleep(100 * time.Millisecond) // 预扣已完成、上游调用在途
	cancel()
	<-done

	if w.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502 after client disconnect", w.Code)
	}
	// 断开不能吞钱:预扣必须全额退回。
	if balance := env.balanceOf(t, key.ID); balance != 1_000_000 {
		t.Errorf("balance = %d, want fully refunded 1_000_000", balance)
	}
	rows := env.usageRows(t)
	if len(rows) != 1 || rows[0].Status != usage.StatusUpstreamError || rows[0].UpstreamError == "" {
		t.Fatalf("failure trail = %+v, want one upstream_error row with summary", rows)
	}
	entries := env.quotaLog(t, key.ID)
	if len(entries) != 3 || entries[0].Reason != apikey.ReasonRefund {
		t.Fatalf("ledger = %+v, want refund after the estimate", entries)
	}
}

func TestRelayErrorSummaryTruncatesOnRuneBoundary(t *testing.T) {
	a := relay.NewOpenAIAdaptor()
	// 600 个汉字按字节截断必然劈开 UTF-8;必须按 rune 截且结果合法。
	long := strings.Repeat("中", 600)
	s := a.ErrorSummary([]byte(long))
	if !utf8.ValidString(s) {
		t.Errorf("ErrorSummary produced invalid UTF-8")
	}
	if n := utf8.RuneCountInString(s); n > 513 { // 512 + 省略号
		t.Errorf("summary = %d runes, want ≤ 513", n)
	}
}
