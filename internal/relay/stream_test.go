package relay_test

// SSE 流式透传(05 号票)的测试:adaptor 缝的 SSE 解析/用量抽取单元测试,
// 加 handler 层的端到端流、断开清理与结算集成测试。共享 relay_test.go 的
// MySQL 环境与种子助手。

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gachal/InfiniteChance/internal/apikey"
	"github.com/gachal/InfiniteChance/internal/channel"
	"github.com/gachal/InfiniteChance/internal/relay"
	"github.com/gachal/InfiniteChance/internal/usage"
)

// streamChunkEvent mirrors the shape of one OpenAI streaming chunk the
// client-facing frames are asserted against.
type streamChunkEvent struct {
	Model   string `json:"model"`
	Choices []struct {
		Delta struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"delta"`
		FinishReason *string `json:"finish_reason"`
	} `json:"choices"`
	Usage *relay.Usage `json:"usage"`
}

// collectStream drains an open UpstreamStream into its client-facing frames,
// keeping the last usage the adaptor reported.
func collectStream(t *testing.T, s *relay.UpstreamStream) (frames []string, u *relay.Usage) {
	t.Helper()
	for {
		frame, got, err := s.Next()
		if got != nil {
			u = got
		}
		if err != nil {
			if !errors.Is(err, io.EOF) {
				t.Fatalf("Next: %v", err)
			}
			break
		}
		if frame != nil {
			frames = append(frames, string(frame))
		}
	}
	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	return frames, u
}

// openStream points a default adaptor at baseURL with the given client
// payload (upstream model name already set) and opens the stream.
func openStream(t *testing.T, baseURL, publicModel, payload string) *relay.UpstreamStream {
	t.Helper()
	stream, err := relay.NewOpenAIAdaptor().ChatCompletionsStream(context.Background(),
		channel.Channel{Type: channel.TypeOpenAI, BaseURL: baseURL, APIKey: "vendor-secret"},
		publicModel, []byte(payload))
	if err != nil {
		t.Fatalf("ChatCompletionsStream: %v", err)
	}
	return stream
}

// sseHandler answers a fixed raw SSE body.
func sseHandler(body string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Write([]byte(body))
	}
}

// usageOnlyChunk is the OpenAI usage-only trailing chunk (empty choices).
const usageOnlyChunk = `{"id":"c1","model":"upstream-m","choices":[],"usage":{"prompt_tokens":11,"completion_tokens":7}}`

// fullStreamBody is a well-formed upstream SSE body: two content chunks, a
// finish chunk, the usage-only chunk and [DONE], plus a keep-alive comment.
const fullStreamBody = "data: {\"id\":\"c1\",\"model\":\"upstream-m\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"content\":\"\"},\"finish_reason\":null}]}\n\n" +
	": keep-alive\n\n" +
	"data: {\"id\":\"c1\",\"model\":\"upstream-m\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"你好\"},\"finish_reason\":null}]}\n\n" +
	"data: {\"id\":\"c1\",\"model\":\"upstream-m\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"世界\"},\"finish_reason\":null}]}\n\n" +
	"data: {\"id\":\"c1\",\"model\":\"upstream-m\",\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}],\"usage\":null}\n\n" +
	"data: " + usageOnlyChunk + "\n\n" +
	"data: [DONE]\n\n"

func TestAdaptorStreamFramesPassThrough(t *testing.T) {
	upstream := httptest.NewServer(sseHandler(fullStreamBody))
	t.Cleanup(upstream.Close)

	frames, got := collectStream(t, openStream(t, upstream.URL, "public-m",
		`{"model":"upstream-m","messages":[],"stream":true}`))

	// 客户端不点 include_usage:用量专用块被吞掉,只剩内容块、注释与 [DONE]。
	if len(frames) != 6 {
		t.Fatalf("frames = %d (%q), want 6 with the billing-only usage chunk swallowed", len(frames), frames)
	}
	if frames[len(frames)-1] != "data: [DONE]\n\n" {
		t.Errorf("last frame = %q, want the upstream [DONE] marker verbatim", frames[len(frames)-1])
	}
	if frames[1] != ": keep-alive\n\n" {
		t.Errorf("frame 1 = %q, want the upstream comment forwarded verbatim", frames[1])
	}

	// 每个数据帧都是合法 OpenAI chunk:model 已回写公开名,内容逐块透传。
	for _, want := range []struct {
		idx     int
		content string
	}{{0, ""}, {2, "你好"}, {3, "世界"}} {
		if !strings.HasPrefix(frames[want.idx], "data: {") {
			t.Fatalf("frame %d = %q, want a data chunk", want.idx, frames[want.idx])
		}
		var ev streamChunkEvent
		if err := json.Unmarshal([]byte(strings.TrimPrefix(frames[want.idx], "data: ")), &ev); err != nil {
			t.Fatalf("frame %d not JSON: %v (%s)", want.idx, err, frames[want.idx])
		}
		if ev.Model != "public-m" {
			t.Errorf("frame %d model = %q, want rewritten public name", want.idx, ev.Model)
		}
		if len(ev.Choices) != 1 || ev.Choices[0].Delta.Content != want.content {
			t.Errorf("frame %d = %+v, want delta content %q", want.idx, ev, want.content)
		}
	}
	if !strings.Contains(frames[4], `"finish_reason":"stop"`) {
		t.Errorf("frame 4 = %q, want the finish chunk passed through", frames[4])
	}

	// 供应商上报的实际用量被抽出(即使专用块没发给客户端)。
	if got == nil || got.PromptTokens != 11 || got.CompletionTokens != 7 {
		t.Errorf("usage = %+v, want 11/7 extracted from the swallowed chunk", got)
	}
}

func TestAdaptorStreamForwardsUsageChunkWhenClientAsked(t *testing.T) {
	upstream := httptest.NewServer(sseHandler(fullStreamBody))
	t.Cleanup(upstream.Close)

	frames, got := collectStream(t, openStream(t, upstream.URL, "public-m",
		`{"model":"upstream-m","stream":true,"stream_options":{"include_usage":true}}`))

	// 客户端自己点了 include_usage:用量专用块必须原样到达。
	if len(frames) != 7 {
		t.Fatalf("frames = %d, want 7 with the usage chunk forwarded", len(frames))
	}
	var ev streamChunkEvent
	if err := json.Unmarshal([]byte(strings.TrimPrefix(frames[len(frames)-2], "data: ")), &ev); err != nil {
		t.Fatalf("usage frame not JSON: %v (%s)", err, frames[len(frames)-2])
	}
	if len(ev.Choices) != 0 || ev.Usage == nil || ev.Usage.PromptTokens != 11 || ev.Usage.CompletionTokens != 7 {
		t.Errorf("usage frame = %+v, want empty choices carrying usage 11/7", ev)
	}
	if got == nil || got.PromptTokens != 11 {
		t.Errorf("usage = %+v, want 11/7", got)
	}
}

func TestAdaptorStreamInjectsUsageRequest(t *testing.T) {
	// 上游必须被注入 stream_options.include_usage —— 结算要真实用量。
	// 客户端已给的 stream_options 其他字段不得被抹掉(全量透传)。
	for _, tc := range []struct {
		name       string
		payload    string
		wantCustom bool // 客户端带的自定义字段要活着到达上游
		wantTemp   bool // 客户端给的 temperature 要活着到达上游
	}{
		{"no stream_options", `{"model":"upstream-m","stream":true,"temperature":0.7}`, false, true},
		{"stream_options without include_usage", `{"model":"upstream-m","stream":true,"stream_options":{}}`, false, false},
		{"client asked with extra field", `{"model":"upstream-m","stream":true,"stream_options":{"include_usage":true,"custom_field":9}}`, true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var gotBody []byte
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				gotBody, _ = io.ReadAll(r.Body)
				w.Write([]byte("data: [DONE]\n\n"))
			}))
			t.Cleanup(upstream.Close)

			frames, _ := collectStream(t, openStream(t, upstream.URL, "public-m", tc.payload))
			if len(frames) != 1 || frames[0] != "data: [DONE]\n\n" {
				t.Fatalf("frames = %q, want just [DONE]", frames)
			}

			var sent struct {
				Model         string  `json:"model"`
				Temperature   float64 `json:"temperature"`
				StreamOptions *struct {
					IncludeUsage bool `json:"include_usage"`
					CustomField  *int `json:"custom_field"`
				} `json:"stream_options"`
			}
			if err := json.Unmarshal(gotBody, &sent); err != nil {
				t.Fatalf("upstream body not JSON: %v (%s)", err, gotBody)
			}
			if sent.StreamOptions == nil || !sent.StreamOptions.IncludeUsage {
				t.Errorf("upstream stream_options = %s, want include_usage injected", gotBody)
			}
			if tc.wantCustom && (sent.StreamOptions.CustomField == nil || *sent.StreamOptions.CustomField != 9) {
				t.Errorf("upstream stream_options = %s, want the client's custom field preserved", gotBody)
			}
			if tc.wantTemp && sent.Temperature != 0.7 {
				t.Errorf("upstream body = %s, want temperature untouched", gotBody)
			}
		})
	}
}

func TestAdaptorStreamNon2xxBufferedError(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusTooManyRequests)
		w.Write([]byte(`{"error":{"message":"rate limited","code":"rate_limit_exceeded"}}`))
	}))
	t.Cleanup(upstream.Close)

	stream, err := relay.NewOpenAIAdaptor().ChatCompletionsStream(context.Background(),
		channel.Channel{Type: channel.TypeOpenAI, BaseURL: upstream.URL, APIKey: "k"},
		"public-m", []byte(`{"model":"upstream-m","stream":true}`))
	if err != nil {
		t.Fatalf("ChatCompletionsStream: %v", err)
	}
	if stream.OK {
		t.Fatalf("stream reported OK for a 429 upstream answer")
	}
	if stream.Status != http.StatusTooManyRequests || !bytes.Contains(stream.Body, []byte("rate limited")) {
		t.Errorf("stream = {status %d body %s}, want buffered 429 error body", stream.Status, stream.Body)
	}
	if err := stream.Close(); err != nil {
		t.Errorf("Close on non-2xx stream: %v", err)
	}
}

func TestAdaptorStreamCanceledContextSurfaces(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"half\"}}]}\n\n"))
		w.(http.Flusher).Flush()
		<-r.Context().Done() // 挂住:模拟上游还在慢慢生成
	}))
	t.Cleanup(upstream.Close)

	ctx, cancel := context.WithCancel(context.Background())
	stream, err := relay.NewOpenAIAdaptor().ChatCompletionsStream(ctx,
		channel.Channel{Type: channel.TypeOpenAI, BaseURL: upstream.URL, APIKey: "k"},
		"public-m", []byte(`{"model":"upstream-m","stream":true}`))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { stream.Close() })

	if _, _, err := stream.Next(); err != nil {
		t.Fatalf("first Next: %v", err)
	}
	cancel()
	for {
		_, _, err := stream.Next()
		if err == nil {
			continue
		}
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("Next after cancel = %v, want an error wrapping context.Canceled", err)
		}
		break
	}
}

func TestAdaptorStreamFlushesUnterminatedTail(t *testing.T) {
	// 上游忘了结尾空行、还发了非 JSON 负载:尾事件照发、坏负载原样透传。
	body := "data: {\"id\":\"c1\",\"model\":\"upstream-m\",\"choices\":[{\"delta\":{\"content\":\"tail\"}}]}\n\n" +
		"data: {broken"
	upstream := httptest.NewServer(sseHandler(body))
	t.Cleanup(upstream.Close)

	frames, got := collectStream(t, openStream(t, upstream.URL, "public-m",
		`{"model":"upstream-m","stream":true}`))
	if len(frames) != 2 {
		t.Fatalf("frames = %q, want the tail event and the broken payload", frames)
	}
	if !strings.HasPrefix(frames[0], "data: {") || !strings.Contains(frames[0], "tail") {
		t.Errorf("frame 0 = %q, want the unterminated tail event flushed at EOF", frames[0])
	}
	if frames[1] != "data: {broken\n\n" {
		t.Errorf("frame 1 = %q, want the unparseable payload forwarded verbatim", frames[1])
	}
	if got != nil {
		t.Errorf("usage = %+v, want none from a stream without usage", got)
	}
}

func TestAdaptorStreamMultiLineDataEventJoined(t *testing.T) {
	// SSE 允许一个事件拆多个 data 行:语义上是单个负载(以 \n 连接)。
	// 负载故意不可解析,验证的是原样拼接而非 model 改写。
	body := "data: not\n" +
		"data: json\n\n" +
		"data: [DONE]\n\n"
	upstream := httptest.NewServer(sseHandler(body))
	t.Cleanup(upstream.Close)

	frames, _ := collectStream(t, openStream(t, upstream.URL, "public-m",
		`{"model":"upstream-m","stream":true}`))
	if len(frames) != 2 {
		t.Fatalf("frames = %q, want the joined event and [DONE]", frames)
	}
	if frames[0] != "data: not\njson\n\n" {
		t.Errorf("frame 0 = %q, want multi-line data joined with \\n", frames[0])
	}
}

// ---- handler 层 ----

// streamEnv is a relay env published on a real HTTP server so a test client
// can read the streamed body incrementally (httptest.ResponseRecorder
// buffers, which would hide streaming bugs).
type streamEnv struct {
	*relayEnv
	server *httptest.Server
}

func newStreamEnv(t *testing.T) *streamEnv {
	t.Helper()
	env := newRelayEnv(t, nil)
	srv := httptest.NewServer(env.engine)
	t.Cleanup(srv.Close)
	return &streamEnv{relayEnv: env, server: srv}
}

// postStream issues a streaming POST and returns the live response.
func (e *streamEnv) postStream(t *testing.T, fullKey, body string) (*http.Response, error) {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, e.server.URL+"/v1/chat/completions", strings.NewReader(body))
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+fullKey)
	return e.server.Client().Do(req)
}

// readFrames reads SSE frames (data/comment lines) until the response body
// ends, returning them with their trailing newline, one string per frame.
func readFrames(t *testing.T, resp *http.Response) []string {
	t.Helper()
	sc := bufio.NewScanner(resp.Body)
	sc.Buffer(make([]byte, 0, 64*1024), 1<<20)
	return readScannerFrames(t, sc, "")
}

// streamBody is the streaming variant of testBody.
const streamBody = `{"model":"public-m","messages":[{"role":"user","content":"你好,世界"}],"max_tokens":1000,"stream":true}`

// slowChatUpstream streams a first content chunk, then — after a pause that
// lets the test observe true incremental delivery — the finish chunk, the
// usage-only chunk and [DONE]. firstFlushed closes once the first chunk is
// on the wire; beforeSecond closes only when the handler is about to write
// the rest.
func slowChatUpstream(t *testing.T) (upstream *httptest.Server, firstFlushed, beforeSecond chan struct{}) {
	t.Helper()
	firstFlushed = make(chan struct{})
	beforeSecond = make(chan struct{})
	upstream = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flush := w.(http.Flusher)
		w.Write([]byte("data: {\"id\":\"c1\",\"model\":\"upstream-m\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"content\":\"\"},\"finish_reason\":null}]}\n\n"))
		flush.Flush()
		close(firstFlushed)
		time.Sleep(300 * time.Millisecond)
		close(beforeSecond)
		w.Write([]byte("data: {\"id\":\"c1\",\"model\":\"upstream-m\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"回答\"},\"finish_reason\":\"stop\"}],\"usage\":null}\n\n" +
			"data: " + usageOnlyChunk + "\n\n" +
			"data: [DONE]\n\n"))
		flush.Flush()
	}))
	t.Cleanup(upstream.Close)
	return upstream, firstFlushed, beforeSecond
}

// wantStreamCharge is the expected actual charge for usage 11 prompt / 7
// completion under the standard test price: ceil((11×1.25 + 7×10)×1.5).
const wantStreamCharge = int64(126)

// readScannerFrames scans an in-progress response body into frames, seeding
// the first frame with the line the caller already consumed ("" = none).
func readScannerFrames(t *testing.T, sc *bufio.Scanner, firstLine string) []string {
	t.Helper()
	var frames []string
	buf := firstLine
	if buf != "" {
		buf += "\n"
	}
	flush := func() {
		if buf != "" {
			frames = append(frames, buf)
			buf = ""
		}
	}
	for sc.Scan() {
		line := sc.Text()
		if line == "" {
			flush()
			continue
		}
		buf += line + "\n"
	}
	if err := sc.Err(); err != nil {
		t.Logf("read frames: %v", err)
	}
	flush()
	return frames
}

func TestRelayStreamEndToEndSettlesActualUsage(t *testing.T) {
	env := newStreamEnv(t)
	upstream, firstFlushed, beforeSecond := slowChatUpstream(t)
	env.seedChannel(t, upstream.URL, "public-m", "upstream-m")
	key, full := env.seedKey(t, 1_000_000)
	env.seedPrice(t, "public-m")

	resp, err := env.postStream(t, full, streamBody)
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/event-stream") {
		t.Errorf("content-type = %q, want text/event-stream", ct)
	}

	// 第一块必须在上游仍在生成时到达 —— 攒齐再发就不是流式了。
	sc := bufio.NewScanner(resp.Body)
	sc.Buffer(make([]byte, 0, 64*1024), 1<<20)
	if !sc.Scan() {
		t.Fatalf("no first line: %v", sc.Err())
	}
	first := sc.Text()
	if !strings.HasPrefix(first, "data: {") {
		t.Fatalf("first line = %q, want a data chunk", first)
	}
	chanOrFatal(t, firstFlushed, "upstream never flushed its first chunk")
	select {
	case <-beforeSecond:
		t.Error("upstream finished before the client saw the first chunk: response was buffered, not streamed")
	default:
	}

	// 客户端没点 include_usage:剩余 = finish 块 + [DONE],用量专用块不外发。
	// frames[0] 是已消费的首块。
	frames := readScannerFrames(t, sc, first)
	if len(frames) != 3 {
		t.Fatalf("frames = %q, want chunk1 + finish + [DONE], usage chunk swallowed", frames)
	}
	if frames[len(frames)-1] != "data: [DONE]\n" {
		t.Errorf("last frame = %q, want [DONE]", frames[len(frames)-1])
	}
	for _, f := range frames {
		if f == "data: [DONE]\n" {
			continue
		}
		var ev streamChunkEvent
		payload := strings.TrimSuffix(strings.TrimPrefix(f, "data: "), "\n")
		if err := json.Unmarshal([]byte(payload), &ev); err != nil {
			t.Fatalf("frame %q not JSON: %v", f, err)
		}
		if ev.Model != "public-m" {
			t.Errorf("frame model = %q, want public name", ev.Model)
		}
		if ev.Usage != nil && len(ev.Choices) == 0 {
			t.Errorf("frame %q is the billing-only usage chunk, want it swallowed", f)
		}
	}

	// 结算按实际用量:账本 estimate→settle,余额恰为初始减实扣。
	if balance := env.balanceOf(t, key.ID); balance != 1_000_000-wantStreamCharge {
		t.Errorf("balance = %d, want %d", balance, 1_000_000-wantStreamCharge)
	}
	entries := env.quotaLog(t, key.ID)
	if len(entries) != 3 || entries[0].Reason != apikey.ReasonSettle || entries[1].Reason != apikey.ReasonEstimate {
		t.Fatalf("ledger = %+v, want estimate then settle", entries)
	}
	wantSettle := -entries[1].DeltaMicros - wantStreamCharge
	if entries[0].DeltaMicros != wantSettle {
		t.Errorf("settle delta = %d, want %d", entries[0].DeltaMicros, wantSettle)
	}

	rows := env.usageRows(t)
	if len(rows) != 1 {
		t.Fatalf("usage rows = %d, want 1", len(rows))
	}
	row := rows[0]
	if row.Status != usage.StatusSuccess || row.PromptTokens != 11 || row.CompletionTokens != 7 ||
		row.ChargeMicros != wantStreamCharge || row.UpstreamError != "" {
		t.Errorf("usage row = %+v, want success trail at actual usage 11/7 charge %d", row, wantStreamCharge)
	}
}

// chanOrFatal waits for ch with a hard bound so a misbehaving handler fails
// the test instead of hanging the whole suite.
func chanOrFatal(t *testing.T, ch <-chan struct{}, what string) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(5 * time.Second):
		t.Fatal(what)
	}
}

func TestRelayStreamClientDisconnectCleansUpstreamAndRefunds(t *testing.T) {
	env := newStreamEnv(t)
	// 上游发一块后挂住等取消:断开后网关必须收掉上游连接,不能悬挂。
	upstreamCanceled := make(chan struct{})
	upstreamOpened := make(chan struct{})
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Write([]byte("data: {\"id\":\"c1\",\"model\":\"upstream-m\",\"choices\":[{\"delta\":{\"content\":\"first\"}}]}\n\n"))
		w.(http.Flusher).Flush()
		close(upstreamOpened)
		<-r.Context().Done()
		close(upstreamCanceled)
	}))
	t.Cleanup(upstream.Close)
	env.seedChannel(t, upstream.URL, "public-m", "upstream-m")
	key, full := env.seedKey(t, 1_000_000)
	env.seedPrice(t, "public-m")

	resp, err := env.postStream(t, full, streamBody)
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	sc := bufio.NewScanner(resp.Body)
	if !sc.Scan() {
		t.Fatalf("no first chunk: %v", sc.Err())
	}
	chanOrFatal(t, upstreamOpened, "upstream never received the request")
	resp.Body.Close() // 客户端中途断开

	// 上游连接必须被清理:上游 handler 看到自己的 context 被取消。
	select {
	case <-upstreamCanceled:
	case <-time.After(3 * time.Second):
		t.Fatal("upstream handler never saw cancellation: the gateway left the upstream connection hanging")
	}

	// 账务在脱离请求的 context 上收尾:用量日志最终出现,预扣全额退回。
	deadline := time.Now().Add(3 * time.Second)
	var rows []usage.Log
	for time.Now().Before(deadline) {
		if rows = env.usageRows(t); len(rows) == 1 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if len(rows) != 1 {
		t.Fatalf("usage rows = %d, want the failure trail after the disconnect", len(rows))
	}
	row := rows[0]
	if row.Status != usage.StatusUpstreamError || row.ChargeMicros != 0 || !strings.Contains(row.UpstreamError, "client disconnected") {
		t.Errorf("failure trail = %+v, want upstream_error at zero charge with a client-disconnect summary", row)
	}
	if balance := env.balanceOf(t, key.ID); balance != 1_000_000 {
		t.Errorf("balance = %d, want fully refunded", balance)
	}
	entries := env.quotaLog(t, key.ID)
	if len(entries) != 3 || entries[0].Reason != apikey.ReasonRefund || entries[1].Reason != apikey.ReasonEstimate {
		t.Errorf("ledger = %+v, want refund reversing the estimate", entries)
	}
}

func TestRelayStreamUpstreamNon2xxPassesThrough(t *testing.T) {
	env := newStreamEnv(t)
	upstream := newFakeUpstream(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusServiceUnavailable)
		w.Write([]byte(`{"error":{"message":"provider overloaded","type":"server_error"}}`))
	})
	env.seedChannel(t, upstream.server.URL, "public-m", "upstream-m")
	key, full := env.seedKey(t, 1_000_000)
	env.seedPrice(t, "public-m")

	resp, err := env.postStream(t, full, streamBody)
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want the upstream 503 passed through", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	eb := decodeOpenAIError(t, body)
	if eb.Error.Code != "upstream_error" || !strings.Contains(eb.Error.Message, "provider overloaded") {
		t.Errorf("error object = %+v, want upstream_error with the vendor message", eb.Error)
	}
	if balance := env.balanceOf(t, key.ID); balance != 1_000_000 {
		t.Errorf("balance = %d, want fully refunded", balance)
	}
	rows := env.usageRows(t)
	if len(rows) != 1 || rows[0].Status != usage.StatusUpstreamError {
		t.Errorf("failure trail = %+v, want one upstream_error row", rows)
	}
}

func TestRelayStreamWithoutUsageReportSettlesAtZero(t *testing.T) {
	env := newStreamEnv(t)
	// 上游完整播完却不守 include_usage 约定:无法按实际计费 —— 少记不虚记,
	// 全额退款、成功流零扣费留痕。
	upstream := httptest.NewServer(sseHandler(
		"data: {\"id\":\"c1\",\"model\":\"upstream-m\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"答案\"},\"finish_reason\":\"stop\"}]}\n\n" +
			"data: [DONE]\n\n"))
	t.Cleanup(upstream.Close)
	env.seedChannel(t, upstream.URL, "public-m", "upstream-m")
	key, full := env.seedKey(t, 1_000_000)
	env.seedPrice(t, "public-m")

	resp, err := env.postStream(t, full, streamBody)
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer resp.Body.Close()
	frames := readFrames(t, resp)
	if len(frames) != 2 || frames[len(frames)-1] != "data: [DONE]\n" {
		t.Fatalf("frames = %q, want the content chunk and [DONE]", frames)
	}

	if balance := env.balanceOf(t, key.ID); balance != 1_000_000 {
		t.Errorf("balance = %d, want refunded (no reportable usage)", balance)
	}
	rows := env.usageRows(t)
	if len(rows) != 1 || rows[0].Status != usage.StatusSuccess || rows[0].ChargeMicros != 0 ||
		rows[0].PromptTokens != 0 || rows[0].CompletionTokens != 0 {
		t.Errorf("usage rows = %+v, want one success row at zero charge", rows)
	}
}

func TestRelayStreamDisconnectAfterUsageStillSettles(t *testing.T) {
	env := newStreamEnv(t)
	// 用量块已过、[DONE] 未达时客户端断开:服务已交付,账按已报用量照常
	// 结算(成功),不能因断开就全额退款。
	upstreamCanceled := make(chan struct{})
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Write([]byte("data: {\"id\":\"c1\",\"model\":\"upstream-m\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"答案\"},\"finish_reason\":\"stop\"}],\"usage\":null}\n\n" +
			"data: " + usageOnlyChunk + "\n\n"))
		w.(http.Flusher).Flush()
		<-r.Context().Done()
		close(upstreamCanceled)
	}))
	t.Cleanup(upstream.Close)
	env.seedChannel(t, upstream.URL, "public-m", "upstream-m")
	key, full := env.seedKey(t, 1_000_000)
	env.seedPrice(t, "public-m")

	resp, err := env.postStream(t, full, streamBody)
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	sc := bufio.NewScanner(resp.Body)
	if !sc.Scan() {
		t.Fatalf("no first chunk: %v", sc.Err())
	}
	// 用量块已随首块冲到网关的读缓冲,稍候片刻让它被吞下再断开。
	time.Sleep(200 * time.Millisecond)
	resp.Body.Close()

	select {
	case <-upstreamCanceled:
	case <-time.After(3 * time.Second):
		t.Fatal("upstream connection not cleaned up after disconnect")
	}

	deadline := time.Now().Add(3 * time.Second)
	var rows []usage.Log
	for time.Now().Before(deadline) {
		if rows = env.usageRows(t); len(rows) == 1 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if len(rows) != 1 {
		t.Fatalf("usage rows = %d, want the settled success trail", len(rows))
	}
	row := rows[0]
	if row.Status != usage.StatusSuccess || row.PromptTokens != 11 || row.CompletionTokens != 7 ||
		row.ChargeMicros != wantStreamCharge {
		t.Errorf("usage row = %+v, want success at actual usage 11/7 charge %d", row, wantStreamCharge)
	}
	if balance := env.balanceOf(t, key.ID); balance != 1_000_000-wantStreamCharge {
		t.Errorf("balance = %d, want settled %d", balance, 1_000_000-wantStreamCharge)
	}
	entries := env.quotaLog(t, key.ID)
	if len(entries) != 3 || entries[0].Reason != apikey.ReasonSettle {
		t.Errorf("ledger = %+v, want estimate then settle", entries)
	}
}

func TestRelayStreamUpstreamResetMidStreamRefundsAndLogs(t *testing.T) {
	env := newStreamEnv(t)
	// 上游发一块后直接 RST 掐断(非正常 EOF、非取消):退款 +
	// upstream_error 留痕,摘要写明 upstream stream failed。
	payload := "data: {\"id\":\"c1\",\"model\":\"upstream-m\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"半截\"}}]}\n\n"
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hj, ok := w.(http.Hijacker)
		if !ok {
			http.Error(w, "no hijack support", http.StatusInternalServerError)
			return
		}
		conn, buf, err := hj.Hijack()
		if err != nil {
			return
		}
		defer conn.Close()
		buf.WriteString("HTTP/1.1 200 OK\r\nContent-Type: text/event-stream\r\nTransfer-Encoding: chunked\r\n\r\n")
		fmt.Fprintf(buf, "%x\r\n%s\r\n", len(payload), payload)
		buf.Flush()
		if tc, ok := conn.(*net.TCPConn); ok {
			tc.SetLinger(0) // RST 而非体面 FIN:网关必须看到读错误
		}
	}))
	t.Cleanup(upstream.Close)
	env.seedChannel(t, upstream.URL, "public-m", "upstream-m")
	key, full := env.seedKey(t, 1_000_000)
	env.seedPrice(t, "public-m")

	resp, err := env.postStream(t, full, streamBody)
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	sc := bufio.NewScanner(resp.Body)
	if !sc.Scan() {
		t.Fatalf("no first chunk: %v", sc.Err())
	}
	for sc.Scan() { // 排干剩余(连接被掐,很快见底)
	}
	resp.Body.Close()

	deadline := time.Now().Add(3 * time.Second)
	var rows []usage.Log
	for time.Now().Before(deadline) {
		if rows = env.usageRows(t); len(rows) == 1 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if len(rows) != 1 {
		t.Fatalf("usage rows = %d, want the failure trail", len(rows))
	}
	row := rows[0]
	if row.Status != usage.StatusUpstreamError || row.ChargeMicros != 0 ||
		!strings.Contains(row.UpstreamError, "upstream stream failed") {
		t.Errorf("failure trail = %+v, want upstream_error at zero charge naming the upstream failure", row)
	}
	if balance := env.balanceOf(t, key.ID); balance != 1_000_000 {
		t.Errorf("balance = %d, want fully refunded", balance)
	}
}
