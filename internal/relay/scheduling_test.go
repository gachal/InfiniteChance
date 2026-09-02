package relay_test

// 06 号票(多渠道调度)的端到端测试:主渠道故障自动换道且客户端无感、
// 熔断与半开恢复的全生命周期、换道的计费边界、非临时故障不换道、流式的
// 换道窗口、/v1/models 目录。共享 relay_test.go 的 MySQL 环境与种子助手,
// 上游是内存 fake(spec 的主缝:HTTP API + fake 上游 + 真实 MySQL)。

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gachal/InfiniteChance/internal/apikey"
	"github.com/gachal/InfiniteChance/internal/channel"
	"github.com/gachal/InfiniteChance/internal/usage"
)

// unavailableHandler answers the vendor's overload rejection.
func unavailableHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusServiceUnavailable)
		w.Write([]byte(`{"error":{"message":"provider overloaded","type":"server_error","code":null}}`))
	}
}

// setHandler swaps a fake upstream's behavior mid-test (sequential tests
// only — the previous request must be done before the swap).
func setHandler(f *fakeUpstream, h http.HandlerFunc) {
	f.mu.Lock()
	f.handle = h
	f.mu.Unlock()
}

// completionEnvelope mirrors what the client sees on a buffered success.
type completionEnvelope struct {
	Model   string `json:"model"`
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
}

func TestRelayFailoverToBackupClientUnaware(t *testing.T) {
	env := newRelayEnv(t, nil)
	primary := newFakeUpstream(t, unavailableHandler())
	backup := newFakeUpstream(t, okChatHandler("来自备用渠道", 100, 200))
	// 主 10 备 1:候选顺序确定,主渠道先试、失败落到备用。
	env.seedChannelFull(t, "primary", primary.server.URL, "public-m", "upstream-m", 10, 0)
	env.seedChannelFull(t, "backup", backup.server.URL, "public-m", "upstream-m", 1, 0)
	key, full := env.seedKey(t, 1_000_000)
	env.seedPrice(t, "public-m")

	w := env.post(t, full, testBody)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d body %s, want 200 served by the backup", w.Code, w.Body.String())
	}
	var completion completionEnvelope
	if err := json.Unmarshal(w.Body.Bytes(), &completion); err != nil {
		t.Fatalf("response not JSON: %v (%s)", err, w.Body.String())
	}
	if completion.Model != "public-m" || len(completion.Choices) != 1 ||
		completion.Choices[0].Message.Content != "来自备用渠道" {
		t.Errorf("response = %+v, want the backup's answer under the public model name", completion)
	}
	if primary.callCount() != 1 || backup.callCount() != 1 {
		t.Errorf("upstream calls = primary %d backup %d, want exactly one each",
			primary.callCount(), backup.callCount())
	}

	// 账务:预扣一次、结算一次 —— 换道对钱无感,失败的尝试不产生退款
	// 条目(预扣原封带去了备用渠道)。
	entries := env.quotaLog(t, key.ID)
	if len(entries) != 3 || entries[0].Reason != apikey.ReasonSettle ||
		entries[1].Reason != apikey.ReasonEstimate || entries[2].Reason != apikey.ReasonInitial {
		t.Fatalf("ledger = %+v, want exactly initial+estimate+settle", entries)
	}
	// 实际扣费 = (100×1.25 + 200×10)×1.5 / 1e6 USD = 3188 micros。
	if balance := env.balanceOf(t, key.ID); balance != 1_000_000-3188 {
		t.Errorf("balance = %d, want %d (settled once against the backup's usage)",
			balance, 1_000_000-3188)
	}

	// 留痕:一行成功行,渠道记备用;上游错误摘要列记下换道史,审计能
	// 看出这次成功多绕了一步。
	rows := env.usageRows(t)
	if len(rows) != 1 {
		t.Fatalf("usage rows = %d, want 1", len(rows))
	}
	row := rows[0]
	if row.Status != usage.StatusSuccess || row.ChannelName != "backup" || row.ChargeMicros != 3188 {
		t.Errorf("usage row = %+v, want a success row on the backup channel", row)
	}
	if !strings.Contains(row.UpstreamError, "'primary'") ||
		!strings.Contains(row.UpstreamError, "provider overloaded") {
		t.Errorf("upstream_error = %q, want the failover trail naming the primary", row.UpstreamError)
	}
}

func TestRelayCircuitBreakerLifecycle(t *testing.T) {
	env := newRelayEnv(t, nil)
	// 先病后愈:前两次 503,之后换成健康应答。
	upstream := newFakeUpstream(t, unavailableHandler())
	env.seedChannelFull(t, "solo", upstream.server.URL, "public-m", "upstream-m", 0, 0)
	key, full := env.seedKey(t, 1_000_000)
	env.seedPrice(t, "public-m")
	env.handlers.Breaker.Threshold = 2
	env.handlers.Breaker.Cooldown = 50 * time.Millisecond

	// 两次真实失败:503 透传,失败留痕两行。
	for i := 0; i < 2; i++ {
		w := env.post(t, full, testBody)
		if w.Code != http.StatusServiceUnavailable {
			t.Fatalf("request %d: status = %d, want upstream 503 passthrough", i+1, w.Code)
		}
	}

	// 第三次:达到阈值渠道已熔断 —— 上游零调用,503 model_unavailable,
	// 预扣原退,不留用量行。
	w := env.post(t, full, testBody)
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("circuit-open status = %d body %s, want 503", w.Code, w.Body.String())
	}
	eb := decodeOpenAIError(t, w.Body.Bytes())
	if eb.Error.Code != "model_unavailable" || eb.Error.Type != "server_error" {
		t.Errorf("error object = %+v, want model_unavailable", eb.Error)
	}
	if got := upstream.callCount(); got != 2 {
		t.Fatalf("upstream calls = %d, want 2 (the circuit-open channel must not be dialed)", got)
	}
	if balance := env.balanceOf(t, key.ID); balance != 1_000_000 {
		t.Errorf("balance = %d, want fully refunded 1_000_000", balance)
	}
	if rows := env.usageRows(t); len(rows) != 2 {
		t.Fatalf("usage rows = %d, want one per real upstream attempt only", len(rows))
	}

	// 上游恢复 + 冷却过期:半开探测自动把渠道放回。
	setHandler(upstream, okChatHandler("痊愈", 10, 10))
	time.Sleep(150 * time.Millisecond)
	w = env.post(t, full, testBody)
	if w.Code != http.StatusOK {
		t.Fatalf("probe status = %d body %s, want 200 through the half-open probe", w.Code, w.Body.String())
	}
	if got := upstream.callCount(); got != 3 {
		t.Fatalf("upstream calls = %d, want the probe to reach the upstream", got)
	}
	// 探测成功即闭环:后续请求照常走该渠道。
	w = env.post(t, full, testBody)
	if w.Code != http.StatusOK {
		t.Fatalf("post-recovery status = %d, want 200 with the circuit closed", w.Code)
	}
	if got := upstream.callCount(); got != 4 {
		t.Errorf("upstream calls = %d, want 4 after the circuit closed", got)
	}
	rows := env.usageRows(t)
	if len(rows) != 4 {
		t.Fatalf("usage rows = %d, want 2 failures + 2 successes", len(rows))
	}
	for _, row := range rows[2:] {
		if row.Status != usage.StatusSuccess {
			t.Errorf("recovery rows = %+v, want successes", rows[2:])
		}
	}
}

func TestRelayNonRetryableUpstreamErrorNotRetried(t *testing.T) {
	env := newRelayEnv(t, nil)
	// 主渠道把请求本身拒了(400,客户端侧问题):换道帮不上忙,必须
	// 原样透传;备用渠道保持零调用;且不记主渠道的熔断账。
	primary := newFakeUpstream(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"error":{"message":"context length exceeded","type":"invalid_request_error"}}`))
	})
	backup := newFakeUpstream(t, okChatHandler("should not be reached", 1, 1))
	env.seedChannelFull(t, "primary", primary.server.URL, "public-m", "upstream-m", 10, 0)
	env.seedChannelFull(t, "backup", backup.server.URL, "public-m", "upstream-m", 1, 0)
	_, full := env.seedKey(t, 1_000_000)
	env.seedPrice(t, "public-m")

	for i := 0; i < 2; i++ { // 两轮:若误记失败,第二次就该换道了
		w := env.post(t, full, testBody)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("status = %d body %s, want upstream 400 passthrough", w.Code, w.Body.String())
		}
		eb := decodeOpenAIError(t, w.Body.Bytes())
		if !strings.Contains(eb.Error.Message, "context length exceeded") {
			t.Errorf("message = %q, want the vendor's own message", eb.Error.Message)
		}
	}
	if backup.callCount() != 0 {
		t.Errorf("backup called %d times, want 0 (client-caused 4xx must not fail over)", backup.callCount())
	}
	if rows := env.usageRows(t); len(rows) != 2 || rows[0].ChannelName != "primary" {
		t.Errorf("usage rows = %+v, want both attempts trailing on the primary", rows)
	}
}

func TestRelayStreamFailoverBeforeStreamOpens(t *testing.T) {
	env := newStreamEnv(t)
	// 主渠道在流开始前就拒绝,备用给出完整流:客户端拿到的流与单渠道
	// 成功无差别。
	primary := newFakeUpstream(t, unavailableHandler())
	backup := newFakeUpstream(t, sseHandler(fullStreamBody))
	env.seedChannelFull(t, "primary", primary.server.URL, "public-m", "upstream-m", 10, 0)
	env.seedChannelFull(t, "backup", backup.server.URL, "public-m", "upstream-m", 1, 0)
	key, full := env.seedKey(t, 1_000_000)
	env.seedPrice(t, "public-m")

	resp, err := env.postStream(t, full, streamBody)
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want the backup's 200 stream", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/event-stream") {
		t.Errorf("content-type = %q, want text/event-stream", ct)
	}
	var body strings.Builder
	sc := frameScanner(resp)
	for sc.Scan() {
		body.WriteString(sc.Text() + "\n")
	}
	if !strings.Contains(body.String(), "世界") || !strings.Contains(body.String(), "[DONE]") {
		t.Errorf("stream body = %q, want the backup's full stream", body.String())
	}
	if primary.callCount() != 1 || backup.callCount() != 1 {
		t.Errorf("upstream calls = primary %d backup %d, want one each", primary.callCount(), backup.callCount())
	}
	// 账务与留痕:用量照常结算,成功行记备用渠道并带换道史。
	if balance := env.balanceOf(t, key.ID); balance != 1_000_000-wantStreamCharge {
		t.Errorf("balance = %d, want %d", balance, 1_000_000-wantStreamCharge)
	}
	rows := env.waitForUsageRow(t)
	if rows[0].Status != usage.StatusSuccess || rows[0].ChannelName != "backup" ||
		!strings.Contains(rows[0].UpstreamError, "'primary'") {
		t.Errorf("usage row = %+v, want backup success with the failover trail", rows[0])
	}
}

func TestRelayStreamNoFailoverOnceStreamOpened(t *testing.T) {
	env := newStreamEnv(t)
	// 主渠道开了流又中途掐断(05 号票语义:退款 + 留痕);响应头已发,
	// 换道已不可能 —— 备用渠道必须保持零调用。
	primary := newFakeUpstream(t, rstAfterFirstFrame())
	backup := newFakeUpstream(t, sseHandler(fullStreamBody))
	env.seedChannelFull(t, "primary", primary.server.URL, "public-m", "upstream-m", 10, 0)
	env.seedChannelFull(t, "backup", backup.server.URL, "public-m", "upstream-m", 1, 0)
	_, full := env.seedKey(t, 1_000_000)
	env.seedPrice(t, "public-m")

	resp, err := env.postStream(t, full, streamBody)
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	sc := frameScanner(resp)
	if !sc.Scan() {
		t.Fatalf("no first chunk: %v", sc.Err())
	}
	for sc.Scan() { // 排干剩余
	}
	resp.Body.Close()

	if backup.callCount() != 0 {
		t.Errorf("backup called %d times, want 0 (no failover once the stream opened)", backup.callCount())
	}
	rows := env.waitForUsageRow(t)
	if rows[0].ChannelName != "primary" || rows[0].Status != usage.StatusUpstreamError {
		t.Errorf("usage row = %+v, want the primary's failure trail", rows[0])
	}
}

// rstAfterFirstFrame hijacks the connection, sends one SSE frame, then RSTs
// instead of finishing the body — an upstream that opens streams but never
// completes them (the gateway must see a read error, not a clean EOF).
func rstAfterFirstFrame() http.HandlerFunc {
	payload := "data: {\"id\":\"c1\",\"model\":\"upstream-m\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"半截\"}}]}\n\n"
	return func(w http.ResponseWriter, r *http.Request) {
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
	}
}

func TestRelayStreamMidStreamFailuresTripBreaker(t *testing.T) {
	env := newStreamEnv(t)
	// 上游只会开流就断:每次流中断都记一次熔断失败 —— 达到阈值后渠道
	// 不再被选中(流式的失败同样积累,打开流本身不算成功)。
	upstream := newFakeUpstream(t, rstAfterFirstFrame())
	env.seedChannelFull(t, "solo", upstream.server.URL, "public-m", "upstream-m", 0, 0)
	key, full := env.seedKey(t, 1_000_000)
	env.seedPrice(t, "public-m")
	// 阈值用默认 3;冷却保持默认 30s,测试窗口内不会半开。

	for i := 0; i < 3; i++ {
		resp, err := env.postStream(t, full, streamBody)
		if err != nil {
			t.Fatalf("request %d: POST: %v", i+1, err)
		}
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("request %d: status = %d, want the committed 200 stream head", i+1, resp.StatusCode)
		}
		sc := frameScanner(resp)
		for sc.Scan() { // 排干(连接被掐,很快见底)
		}
		resp.Body.Close()
		if got := upstream.callCount(); got != i+1 {
			t.Fatalf("request %d: upstream calls = %d, want %d", i+1, got, i+1)
		}
	}

	// 第四次:渠道已熔断 —— 上游零调用,503 model_unavailable,预扣原退,
	// 不新增用量行。
	w := env.post(t, full, streamBody)
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("circuit-open status = %d body %s, want 503", w.Code, w.Body.String())
	}
	eb := decodeOpenAIError(t, w.Body.Bytes())
	if eb.Error.Code != "model_unavailable" {
		t.Errorf("code = %q, want model_unavailable", eb.Error.Code)
	}
	if got := upstream.callCount(); got != 3 {
		t.Errorf("upstream calls = %d, want 3 (circuit-open channel must not be dialed)", got)
	}
	if balance := env.balanceOf(t, key.ID); balance != 1_000_000 {
		t.Errorf("balance = %d, want fully refunded 1_000_000", balance)
	}
	if rows := env.usageRows(t); len(rows) != 3 {
		t.Errorf("usage rows = %d, want one per interrupted stream only", len(rows))
	}
}

func TestRelayExhaustedAfterRealAttemptReportsTheFailure(t *testing.T) {
	env := newRelayEnv(t, nil)
	// 混合场景:首选渠道真实拨过且临时失败,后备候选全在熔断中 ——
	// 客户端必须听到真实失败原因(upstream_error),而不是
	// model_unavailable;失败也要照常留痕。
	env.handlers.Breaker.Threshold = 1

	// 后备渠道 b 只挂 warmup-m:先打一发把它熔断(阈值 1)。
	warm := newFakeUpstream(t, unavailableHandler())
	env.seedChannelFull(t, "b", warm.server.URL, "warmup-m", "up-warm", 5, 0)
	primary := newFakeUpstream(t, unavailableHandler())
	env.seedChannelFull(t, "a", primary.server.URL, "public-m", "up-a", 10, 0)
	key, full := env.seedKey(t, 1_000_000)
	env.seedPrice(t, "public-m")
	env.seedPrice(t, "warmup-m")

	w := env.post(t, full, `{"model":"warmup-m","messages":[{"role":"user","content":"hi"}]}`)
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("warmup status = %d, want upstream 503 passthrough", w.Code)
	}
	if got := warm.callCount(); got != 1 {
		t.Fatalf("warmup upstream calls = %d, want the channel opened by one real failure", got)
	}

	w = env.post(t, full, testBody)
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d body %s, want 503", w.Code, w.Body.String())
	}
	eb := decodeOpenAIError(t, w.Body.Bytes())
	if eb.Error.Code != "upstream_error" || !strings.Contains(eb.Error.Message, "provider overloaded") {
		t.Errorf("error object = %+v, want upstream_error carrying the real vendor failure", eb.Error)
	}
	if got := primary.callCount(); got != 1 {
		t.Errorf("primary calls = %d, want exactly the one real attempt", got)
	}
	if balance := env.balanceOf(t, key.ID); balance != 1_000_000 {
		t.Errorf("balance = %d, want fully refunded", balance)
	}
	rows := env.usageRows(t)
	if len(rows) != 2 {
		t.Fatalf("usage rows = %d, want the warmup row plus the public-m failure trail", len(rows))
	}
	if rows[1].ChannelName != "a" || rows[1].Status != usage.StatusUpstreamError {
		t.Errorf("public-m row = %+v, want the primary's failure trail", rows[1])
	}
}

func TestRelayModelsListsEnabledPublicModelsDeduped(t *testing.T) {
	env := newRelayEnv(t, nil)
	upstream := newFakeUpstream(t, okChatHandler("x", 1, 1))
	// 两渠道共挂 shared-m;unique-a/b 各归一渠;disabled-m 挂在停用渠道,
	// 不得出现。
	env.seedChannelFull(t, "c1", upstream.server.URL, "unique-a", "up-a", 0, 0)
	c2 := env.seedChannelFull(t, "c2", upstream.server.URL, "unique-b", "up-b", 0, 0)
	if _, err := env.stores.channels.Update(context.Background(), channel.Channel{
		ID: c2.ID, Name: "c2", Type: channel.TypeOpenAI, BaseURL: upstream.server.URL,
		ModelMap: map[string]string{"unique-b": "up-b", "shared-m": "up-s"}, Enabled: true,
	}); err != nil {
		t.Fatalf("add shared model to c2: %v", err)
	}
	env.seedChannelFull(t, "c3", upstream.server.URL, "shared-m", "up-s2", 0, 0)
	if _, err := env.stores.channels.Create(context.Background(), channel.Channel{
		Name: "off", Type: channel.TypeOpenAI, BaseURL: upstream.server.URL,
		APIKey: "k", ModelMap: map[string]string{"disabled-m": "up"}, Enabled: false,
	}); err != nil {
		t.Fatalf("seed disabled channel: %v", err)
	}
	_, full := env.seedKey(t, 1_000_000)

	req := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	req.Header.Set("Authorization", "Bearer "+full)
	w := httptest.NewRecorder()
	env.engine.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d body %s, want 200", w.Code, w.Body.String())
	}
	var list struct {
		Object string `json:"object"`
		Data   []struct {
			ID      string `json:"id"`
			Object  string `json:"object"`
			Created int64  `json:"created"`
			OwnedBy string `json:"owned_by"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &list); err != nil {
		t.Fatalf("body not JSON: %v (%s)", err, w.Body.String())
	}
	if list.Object != "list" {
		t.Errorf("object = %q, want list", list.Object)
	}
	var ids []string
	for _, m := range list.Data {
		ids = append(ids, m.ID)
		if m.Object != "model" || m.OwnedBy != "infinitechance" || m.Created <= 0 {
			t.Errorf("model entry = %+v, want the OpenAI model shape", m)
		}
	}
	// 去重 + 启用渠道全集 + 名称排序:shared-m 只出现一次。
	want := []string{"shared-m", "unique-a", "unique-b"}
	if strings.Join(ids, ",") != strings.Join(want, ",") {
		t.Errorf("models = %v, want %v (deduped, enabled only, sorted)", ids, want)
	}
}
