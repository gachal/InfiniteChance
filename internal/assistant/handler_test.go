package assistant_test

// 画布助手端点全链测试(91 号票):handler 装配真 SQLite 会话 store + 真
// 工具注册表,网关用 httptest 假中转(逐轮回放 OpenAI 流式 SSE),回执由
// 测试模拟前端经回执端点投递 —— 「工具草案 → 前端应用 → 回执 → 续循环」
// 的集成闭环与流前校验纪律在此钉测。

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/gin-gonic/gin"

	"github.com/gachal/InfiniteChance/internal/assistant"
	"github.com/gachal/InfiniteChance/internal/canvas"
	"github.com/gachal/InfiniteChance/internal/pricing"
	"github.com/gachal/InfiniteChance/internal/promptgen"
	"github.com/gachal/InfiniteChance/internal/sqlitedb"
)

// ---- 假网关中转(照 promptgen agentstream_test 同族)----

type recordedRequest struct {
	Method string
	Path   string
	Source string
	Body   map[string]any
}

type fakeRelay struct {
	mu       sync.Mutex
	rounds   []string // 流式轮脚本(完整 SSE 文本,含 [DONE])
	chatText string   // 非流式 chat 的回答(标题生成)
	requests []recordedRequest
}

func (f *fakeRelay) handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		f.mu.Lock()
		f.requests = append(f.requests, recordedRequest{
			Method: r.Method, Path: r.URL.Path,
			Source: r.Header.Get("X-InfiniteChance-Source"), Body: body,
		})
		stream, _ := body["stream"].(bool)
		script := ""
		if n := len(f.requests) - 1; !stream {
			// 非流式(标题生成)回 JSON。
		} else if n < len(f.rounds) {
			script = f.rounds[n]
		}
		chatText := f.chatText
		f.mu.Unlock()

		w.Header().Set("Content-Type", "application/json")
		if !stream {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"` + chatText + `"}}]}`))
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(script))
	})
}

func (f *fakeRelay) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.requests)
}

func (f *fakeRelay) request(n int) recordedRequest {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.requests[n]
}

// relayChunk 把一个流式块编码成一帧 SSE data 事件。
func relayChunk(t *testing.T, v any) string {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal relay chunk: %v", err)
	}
	return "data: " + string(raw) + "\n\n"
}

// relayToolRound 拼一轮工具调用(answer 劈两片,rune 边界防中文 U+FFFD)。
func relayToolRound(t *testing.T, id, name string, args map[string]any) string {
	t.Helper()
	raw, err := json.Marshal(args)
	if err != nil {
		t.Fatalf("marshal tool args: %v", err)
	}
	argStr := string(raw)
	half := len(argStr) / 2
	for half > 0 && !utf8.RuneStart(argStr[half]) {
		half--
	}
	var b strings.Builder
	b.WriteString(relayChunk(t, map[string]any{
		"choices": []any{map[string]any{"delta": map[string]any{"tool_calls": []any{map[string]any{
			"index": 0, "id": id, "type": "function",
			"function": map[string]any{"name": name, "arguments": argStr[:half]},
		}}}}}},
	))
	b.WriteString(relayChunk(t, map[string]any{
		"choices": []any{map[string]any{"delta": map[string]any{"tool_calls": []any{map[string]any{
			"index":    0,
			"function": map[string]any{"arguments": argStr[half:]},
		}}}}}},
	))
	b.WriteString(relayChunk(t, map[string]any{
		"choices": []any{map[string]any{"delta": map[string]any{}, "finish_reason": "tool_calls"}},
	}))
	b.WriteString("data: [DONE]\n\n")
	return b.String()
}

func relayTextRound(t *testing.T, text string) string {
	return relayChunk(t, map[string]any{
		"choices": []any{map[string]any{"delta": map[string]any{"content": text}}},
	}) + "data: [DONE]\n\n"
}

// ---- 装配 ----

type fakeCanvases struct{}

func (fakeCanvases) Get(_ context.Context, id int64) (canvas.Canvas, error) {
	if id == 7 {
		return canvas.Canvas{ID: 7, Name: "测试画布", Version: 1, IPID: 3}, nil
	}
	return canvas.Canvas{}, canvas.ErrNotFound
}

type fakePrices struct{}

func (fakePrices) List(context.Context) ([]pricing.Price, error) { return nil, nil }

func (fakePrices) ByModel(_ context.Context, model string) (pricing.Price, error) {
	if model == "chat-m" {
		return pricing.Price{PublicModel: model, Unit: pricing.UnitToken, Token: &pricing.TokenPrice{}}, nil
	}
	if model == "img-m" {
		return pricing.Price{PublicModel: model, Unit: pricing.UnitCall, Call: &pricing.CallPrice{}}, nil
	}
	return pricing.Price{}, pricing.ErrNotFound
}

type testEnv struct {
	server   *httptest.Server
	relay    *fakeRelay
	hub      *assistant.Hub
	sessions assistant.Store
}

// newTestEnv 装配真 SQLite 会话库 + 真工具注册表 + 假中转,路由照
// canvas/server 的挂法(/canvases 组)。
func newTestEnv(t *testing.T, mutate func(*envParams)) testEnv {
	t.Helper()
	gin.SetMode(gin.TestMode)

	db, err := sqlitedb.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	ctx := context.Background()
	if err := assistant.NewSQLiteStore(db).EnsureSchema(ctx); err != nil {
		t.Fatal(err)
	}
	sessions := assistant.NewSQLiteStore(db)

	relay := &fakeRelay{}
	relayServer := httptest.NewServer(relay.handler())
	t.Cleanup(relayServer.Close)

	hub := assistant.NewHub()
	registry := assistant.NewRegistry(assistant.Deps{
		Hub:      hub,
		Canvases: fakeCanvases{},
	})

	params := &envParams{
		gateway: promptgen.NewClient(relayServer.URL, "sk-relay"),
	}
	if mutate != nil {
		mutate(params)
	}

	h := &assistant.Handlers{
		Sessions: sessions,
		Canvases: fakeCanvases{},
		Models:   fakePrices{},
		Gateway:  params.gateway,
		Tools:    registry,
		Hub:      hub,
	}
	engine := gin.New()
	assistant.RegisterRoutes(engine.Group("/canvases"), h)
	server := httptest.NewServer(engine)
	t.Cleanup(server.Close)

	return testEnv{server: server, relay: relay, hub: hub, sessions: sessions}
}

type envParams struct {
	gateway promptgen.Gateway
}

func (env testEnv) do(t *testing.T, method, path string, body any) (*http.Response, string) {
	t.Helper()
	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal body: %v", err)
		}
		reader = strings.NewReader(string(raw))
	}
	req, err := http.NewRequest(method, env.server.URL+path, reader)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	res, err := env.server.Client().Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	t.Cleanup(func() { _ = res.Body.Close() })
	raw, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatalf("read response: %v", err)
	}
	return res, string(raw)
}

// createSession 经 API 建一个会话,带回 id。
func (env testEnv) createSession(t *testing.T, mode string) int64 {
	t.Helper()
	res, raw := env.do(t, http.MethodPost, "/canvases/7/assistant/sessions", map[string]any{"mode": mode})
	if res.StatusCode != http.StatusCreated {
		t.Fatalf("create session status = %d, body = %s", res.StatusCode, raw)
	}
	var body struct {
		Session struct {
			ID   int64  `json:"id"`
			Mode string `json:"mode"`
		} `json:"session"`
	}
	if err := json.Unmarshal([]byte(raw), &body); err != nil {
		t.Fatalf("decode: %v (%s)", err, raw)
	}
	return body.Session.ID
}

// sseFrames 切开响应原文(data: 前缀逐帧,[DONE] 原样保留)。
func sseFrames(raw string) []string {
	var frames []string
	for _, block := range strings.Split(raw, "\n\n") {
		payload := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(block), "data: "))
		if payload == "" || !strings.HasPrefix(strings.TrimSpace(block), "data: ") {
			continue
		}
		frames = append(frames, payload)
	}
	return frames
}

func frameMap(t *testing.T, payload string) map[string]any {
	t.Helper()
	if payload == "[DONE]" {
		return nil
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(payload), &m); err != nil {
		t.Fatalf("frame %q not JSON: %v", payload, err)
	}
	return m
}

// ---- 用例 ----

// 集成主干:工具草案 → 测试模拟前端应用(回执带新节点 id)→ 续循环 →
// done + [DONE];持久纪律与会话标题(标题生成回落钉在同例)一并断言。
func TestAssistantStreamRunsReceiptLoopEndToEnd(t *testing.T) {
	env := newTestEnv(t, nil)
	env.relay.rounds = []string{
		relayToolRound(t, "call_1", "create_nodes", map[string]any{
			"nodes": []any{map[string]any{"type": "image", "prompt": "主角立绘"}},
		}),
		relayTextRound(t, "已新建图片节点。"),
	}
	env.relay.chatText = "加主角立绘"

	sid := env.createSession(t, "")

	// 流式请求:逐帧读到 tool_call → 模拟前端应用并发回执 → 读到收尾。
	res, err := env.server.Client().Post(env.server.URL+"/canvases/7/assistant/stream",
		"application/json", strings.NewReader(fmt.Sprintf(`{"session_id":%d,"message":"帮我加一个主角立绘节点","model":"chat-m"}`, sid)))
	if err != nil {
		t.Fatalf("stream: %v", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK || res.Header.Get("Content-Type") != "text/event-stream" {
		t.Fatalf("status = %d content-type = %q", res.StatusCode, res.Header.Get("Content-Type"))
	}

	reader := bufio.NewReader(res.Body)
	var raw strings.Builder
	sawToolCall, sawResult, sawDone := false, false, false
	receiptSent := false
	for {
		line, rErr := reader.ReadString('\n')
		raw.WriteString(line)
		payload := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), "data: "))
		if strings.HasPrefix(line, "data: ") && payload != "" && payload != "[DONE]" {
			m := frameMap(t, payload)
			switch m["type"] {
			case "tool_call":
				sawToolCall = true
				if m["name"] != "create_nodes" {
					t.Errorf("tool_call name = %v", m["name"])
				}
			case "tool_result":
				sawResult = true
				if p, _ := m["payload"].(map[string]any); p == nil || !strings.Contains(marshalStr(p), "image-9") {
					t.Errorf("tool_result payload = %v, want the receipt result", m["payload"])
				}
			case "done":
				sawDone = true
				if m["text"] != "已新建图片节点。" {
					t.Errorf("done.text = %v", m["text"])
				}
				if m["summary"] != "本轮工具调用:create_nodes(已应用)" {
					t.Errorf("done.summary = %v", m["summary"])
				}
			}
			// tool_call 一到就投回执(模拟前端应用完成)。
			if sawToolCall && !receiptSent {
				receiptSent = true
				go func() {
					res, _ := env.server.Client().Post(env.server.URL+fmt.Sprintf("/canvases/7/assistant/sessions/%d/receipts", sid),
						"application/json",
						strings.NewReader(`{"call_id":"call_1","status":"applied","result":{"nodes":[{"index":0,"id":"image-9"}]}}`))
					if res != nil {
						_ = res.Body.Close()
					}
				}()
			}
		}
		if payload == "[DONE]" {
			break
		}
		if rErr != nil {
			t.Fatalf("read stream before [DONE]: %v (frames so far: %s)", rErr, raw.String())
		}
	}
	if !sawToolCall || !sawResult || !sawDone || !receiptSent {
		t.Fatalf("frames incomplete: toolCall=%v result=%v done=%v receipt=%v (%s)", sawToolCall, sawResult, sawDone, receiptSent, raw.String())
	}
	if !strings.HasSuffix(raw.String(), "data: [DONE]\n") {
		t.Fatalf("stream tail = %q, want [DONE] marker", raw.String()[max(0, len(raw.String())-40):])
	}

	// 上行:两轮流式 + 一次标题生成;来源标记 canvas=7 gen=assistant(无
	// node 段,票面计费标记)。标题在 [DONE] 之后由 goroutine 异步生成
	// (评审修复:不再阻塞流收尾),轮询等它到齐。
	waitForTitleCall(t, env)
	for i := 0; i < 2; i++ {
		if src := env.relay.request(i).Source; src != "canvas=7 gen=assistant" {
			t.Errorf("round %d source = %q", i, src)
		}
	}
	titleReq := env.relay.request(2)
	if titleReq.Body["stream"] == true {
		t.Error("title call should be non-streaming")
	}

	// 持久纪律:GET 会话 = user 全文 + assistant 全文 + 摘要;标题 = LLM
	// 生成(假中转回了「加主角立绘」),异步落库 —— 标题轮询等到位;
	// messages 在 [DONE] 前已同步落库,首拍即应齐。
	var body struct {
		Session struct {
			Title    string `json:"title"`
			Messages []struct {
				Role    string `json:"role"`
				Content string `json:"content"`
				Summary string `json:"summary"`
			} `json:"messages"`
		} `json:"session"`
	}
	deadline := time.Now().Add(3 * time.Second)
	for {
		res2, raw2 := env.do(t, http.MethodGet, fmt.Sprintf("/canvases/7/assistant/sessions/%d", sid), nil)
		if res2.StatusCode != http.StatusOK {
			t.Fatalf("get session status = %d (%s)", res2.StatusCode, raw2)
		}
		if err := json.Unmarshal([]byte(raw2), &body); err != nil {
			t.Fatalf("decode: %v (%s)", err, raw2)
		}
		if body.Session.Title == "加主角立绘" {
			break
		}
		if !time.Now().Before(deadline) {
			t.Fatalf("title = %q, want the LLM-generated one within 3s", body.Session.Title)
		}
		time.Sleep(20 * time.Millisecond)
	}
	if len(body.Session.Messages) != 2 {
		t.Fatalf("persisted messages = %d, want user+assistant only", len(body.Session.Messages))
	}
	if body.Session.Messages[0].Role != "user" || body.Session.Messages[0].Content != "帮我加一个主角立绘节点" {
		t.Errorf("user turn = %+v", body.Session.Messages[0])
	}
	last := body.Session.Messages[1]
	if last.Role != "assistant" || last.Content != "已新建图片节点。" || last.Summary != "本轮工具调用:create_nodes(已应用)" {
		t.Errorf("assistant turn = %+v, want final answer + tool summary", last)
	}
}

func marshalStr(v any) string {
	raw, _ := json.Marshal(v)
	return string(raw)
}

// waitForTitleCall polls until the async title-generation call lands on the
// fake relay(标题自评审修复起在 [DONE] 后由 goroutine 生成,照 relay 包
// waitForUsageRow 先例轮询),超时按计数失败。
func waitForTitleCall(t *testing.T, env testEnv) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if env.relay.count() >= 3 {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("relay calls = %d, want 2 loop rounds + 1 title call within 3s", env.relay.count())
}

// 流前校验纪律:全部 JSON 错误、不开流。
func TestAssistantStreamPreStreamValidationStaysJSON(t *testing.T) {
	env := newTestEnv(t, func(p *envParams) { p.gateway = nil }) // 未配服务 key
	sid := env.createSession(t, "")

	cases := []struct {
		name   string
		gate   func() (*http.Response, string)
		status int
		code   string
	}{
		{"gateway unconfigured", func() (*http.Response, string) {
			return env.do(t, http.MethodPost, "/canvases/7/assistant/stream",
				map[string]any{"session_id": sid, "message": "hi", "model": "chat-m"})
		}, http.StatusServiceUnavailable, "gateway_unconfigured"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res, raw := tc.gate()
			if res.StatusCode != tc.status {
				t.Fatalf("status = %d, want %d (%s)", res.StatusCode, tc.status, raw)
			}
			var body struct {
				Error struct {
					Code string `json:"code"`
				} `json:"error"`
			}
			if err := json.Unmarshal([]byte(raw), &body); err != nil {
				t.Fatalf("not JSON error: %v (%s)", err, raw)
			}
			if body.Error.Code != tc.code {
				t.Errorf("code = %q, want %q", body.Error.Code, tc.code)
			}
		})
	}

	// 有网关形态:空 message / 未配价模型 / 会话不存在 / 会话跨画布。
	env2 := newTestEnv(t, nil)
	sid2 := env2.createSession(t, "")
	cases2 := []struct {
		name   string
		body   map[string]any
		status int
		code   string
	}{
		{"empty message", map[string]any{"session_id": sid2, "message": "  ", "model": "chat-m"}, http.StatusBadRequest, "invalid_request"},
		{"unpriced model", map[string]any{"session_id": sid2, "message": "hi", "model": "no-such"}, http.StatusBadRequest, "model_not_priced"},
		{"non chat track", map[string]any{"session_id": sid2, "message": "hi", "model": "img-m"}, http.StatusBadRequest, "model_not_priced"},
		{"missing session", map[string]any{"session_id": 999, "message": "hi", "model": "chat-m"}, http.StatusNotFound, "not_found"},
	}
	for _, tc := range cases2 {
		t.Run(tc.name, func(t *testing.T) {
			res, raw := env2.do(t, http.MethodPost, "/canvases/7/assistant/stream", tc.body)
			if res.StatusCode != tc.status {
				t.Fatalf("status = %d, want %d (%s)", res.StatusCode, tc.status, raw)
			}
			var body struct {
				Error struct {
					Code string `json:"code"`
				} `json:"error"`
			}
			if err := json.Unmarshal([]byte(raw), &body); err != nil {
				t.Fatalf("not JSON error: %v (%s)", err, raw)
			}
			if body.Error.Code != tc.code {
				t.Errorf("code = %q, want %q", body.Error.Code, tc.code)
			}
		})
	}

	// 并发流:手工占住 claim,第二条流 409 session_busy。
	env2.hub.TryClaim(sid2)
	res, raw := env2.do(t, http.MethodPost, "/canvases/7/assistant/stream",
		map[string]any{"session_id": sid2, "message": "hi", "model": "chat-m"})
	if res.StatusCode != http.StatusConflict {
		t.Fatalf("busy status = %d, want 409 (%s)", res.StatusCode, raw)
	}
	if !strings.Contains(raw, "session_busy") {
		t.Errorf("busy body = %s", raw)
	}
	env2.hub.Release(sid2)
}

// 回执超时:error 帧如实报告(receipt_timeout)、无 [DONE]、整轮不持久化
// (可重试重放)。
func TestAssistantStreamReceiptTimeoutAbortsAndLeavesHistoryClean(t *testing.T) {
	env := newTestEnv(t, nil)
	env.hub.TimeoutForTest(30 * time.Millisecond)
	env.relay.rounds = []string{
		relayToolRound(t, "call_t", "update_nodes", map[string]any{
			"nodes": []any{map[string]any{"id": "image-1", "prompt": "改"}},
		}),
	}
	sid := env.createSession(t, "")

	res, err := env.server.Client().Post(env.server.URL+"/canvases/7/assistant/stream",
		"application/json", strings.NewReader(fmt.Sprintf(`{"session_id":%d,"message":"改一下","model":"chat-m"}`, sid)))
	if err != nil {
		t.Fatalf("stream: %v", err)
	}
	defer res.Body.Close()
	all, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatalf("read stream: %v", err)
	}
	frames := sseFrames(string(all))
	if len(frames) == 0 || frames[len(frames)-1] == "[DONE]" {
		t.Fatalf("frames tail = %v, want error frame without [DONE]", frames)
	}
	errFrame := frameMap(t, frames[len(frames)-1])
	errObj, _ := errFrame["error"].(map[string]any)
	if errFrame["type"] != "error" || errObj == nil || errObj["code"] != "receipt_timeout" {
		t.Fatalf("error frame = %v, want receipt_timeout", errFrame)
	}

	// 整轮失败不持久化:会话还是空的。
	_, raw := env.do(t, http.MethodGet, fmt.Sprintf("/canvases/7/assistant/sessions/%d", sid), nil)
	if !strings.Contains(raw, `"messages":[]`) {
		t.Errorf("session after abort = %s, want empty history", raw)
	}
}

// 会话 CRUD 与回执端点的状态纪律。
func TestAssistantSessionLifecycleAndReceiptGuards(t *testing.T) {
	env := newTestEnv(t, nil)

	// 建两个:缺省 ask + 显式 full;目录新在前。
	s1 := env.createSession(t, "")
	res, raw := env.do(t, http.MethodPost, "/canvases/7/assistant/sessions", map[string]any{"mode": "full"})
	if res.StatusCode != http.StatusCreated {
		t.Fatalf("create full status = %d (%s)", res.StatusCode, raw)
	}
	var created struct {
		Session struct {
			ID   int64  `json:"id"`
			Mode string `json:"mode"`
		} `json:"session"`
	}
	_ = json.Unmarshal([]byte(raw), &created)
	if created.Session.Mode != "full" {
		t.Fatalf("mode = %q, want full", created.Session.Mode)
	}
	s2 := created.Session.ID

	res, raw = env.do(t, http.MethodGet, "/canvases/7/assistant/sessions", nil)
	var listed struct {
		Sessions []struct {
			ID   int64  `json:"id"`
			Mode string `json:"mode"`
		} `json:"sessions"`
	}
	_ = json.Unmarshal([]byte(raw), &listed)
	if len(listed.Sessions) != 2 || listed.Sessions[0].ID != s2 || listed.Sessions[1].ID != s1 {
		t.Fatalf("sessions = %v, want [s2 s1]", listed.Sessions)
	}

	// 切档:PATCH mode。
	res, raw = env.do(t, http.MethodPatch, fmt.Sprintf("/canvases/7/assistant/sessions/%d", s1), map[string]any{"mode": "full"})
	if res.StatusCode != http.StatusOK || !strings.Contains(raw, `"mode":"full"`) {
		t.Fatalf("patch mode status = %d (%s)", res.StatusCode, raw)
	}
	// 非法档拒绝。
	res, _ = env.do(t, http.MethodPatch, fmt.Sprintf("/canvases/7/assistant/sessions/%d", s1), map[string]any{"mode": "yolo"})
	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("bad mode status = %d, want 400", res.StatusCode)
	}

	// 回执守卫:无挂起 409 no_pending_call;坏 status 400。
	res, raw = env.do(t, http.MethodPost, fmt.Sprintf("/canvases/7/assistant/sessions/%d/receipts", s1),
		map[string]any{"call_id": "call_1", "status": "applied"})
	if res.StatusCode != http.StatusConflict || !strings.Contains(raw, "no_pending_call") {
		t.Fatalf("idle receipt status = %d (%s)", res.StatusCode, raw)
	}
	res, _ = env.do(t, http.MethodPost, fmt.Sprintf("/canvases/7/assistant/sessions/%d/receipts", s1),
		map[string]any{"call_id": "call_1", "status": "maybe"})
	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("bad status receipt = %d, want 400", res.StatusCode)
	}

	// 会话绑死画布:另一画布(不存在)访问按 404。
	res, raw = env.do(t, http.MethodGet, "/canvases/8/assistant/sessions", nil)
	if res.StatusCode != http.StatusNotFound {
		t.Fatalf("canvas 8 status = %d, want 404 (%s)", res.StatusCode, raw)
	}

	// 单删。
	res, _ = env.do(t, http.MethodDelete, fmt.Sprintf("/canvases/7/assistant/sessions/%d", s2), nil)
	if res.StatusCode != http.StatusNoContent {
		t.Fatalf("delete status = %d", res.StatusCode)
	}
	res, raw = env.do(t, http.MethodGet, "/canvases/7/assistant/sessions", nil)
	if strings.Contains(raw, fmt.Sprintf(`"id":%d`, s2)) {
		t.Errorf("deleted session still listed: %s", raw)
	}
}
