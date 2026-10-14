package relay_test

// 45 号票(MiniMax H3 视频渠道)的端到端测试:fake MiniMax 上游用官方
// V2 任务面形状(POST /v2/video_generation 提交回 {"task_id"}、GET
// /v2/query/video_generation/{id} 轮询回 {"task":{…}} 包裹、DELETE
// /v2/video_generation/{id} 取消)验证 adaptor 翻译、adaptorFor 分发与
// second 轨账务时序(预扣按秒×系数 → 结算=预扣零差额 / 失败退款),以及
// 2xx 无 task_id 的防御。共享 relay_test.go 的 MySQL 环境。

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/gachal/InfiniteChance/internal/channel"
	"github.com/gachal/InfiniteChance/internal/usage"
)

// fakeMiniMaxVendor 是 MiniMax V2 任务面替身:提交回 {"task_id"},查询回
// {"task":{…}} 包裹的任务体(status / content.url / error / usage),取消
// 是 DELETE。测试在轮询间隙改剧本;最近一次提交体被留存供翻译断言。
type fakeMiniMaxVendor struct {
	server *httptest.Server
	mu     sync.Mutex

	status          string // 查询端点当前回的 MiniMax status 原文
	videoURL        string // succeeded 时 task.content.url
	errMsg          string // failed 时 task.error.message
	withUsage       bool   // 查询响应是否带 usage
	completionToken int64  // usage.completion_tokens
	submitStatus    int    // 提交端点回答码(0 = 200)
	submitBody      string // 提交应答覆盖(空 = 正常回 task_id)

	submits    int
	cancels    int
	queries    int
	lastBody   []byte
	lastAuth   string
	lastPath   string
	lastMethod string
}

const minimaxTaskID = "424010985738629"

func newFakeMiniMaxVendor(t *testing.T) *fakeMiniMaxVendor {
	t.Helper()
	v := &fakeMiniMaxVendor{}
	v.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		v.mu.Lock()
		defer v.mu.Unlock()
		raw, _ := io.ReadAll(r.Body)
		v.lastAuth = r.Header.Get("Authorization")
		v.lastMethod = r.Method
		v.lastPath = r.URL.Path

		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/v2/video_generation":
			v.submits++
			v.lastBody = raw
			w.Header().Set("Content-Type", "application/json")
			if v.submitStatus != 0 && v.submitStatus != http.StatusOK {
				w.WriteHeader(v.submitStatus)
				w.Write([]byte(`{"type":"error","error":{"type":"bad_request_error","message":"duration out of range (2013)","http_code":"400"}}`))
				return
			}
			body := v.submitBody
			if body == "" {
				body = fmt.Sprintf(`{"task_id":%q}`, minimaxTaskID)
			}
			w.Write([]byte(body))
		case r.Method == http.MethodGet && r.URL.Path == "/v2/query/video_generation/"+minimaxTaskID:
			v.queries++
			w.Header().Set("Content-Type", "application/json")
			task := map[string]any{"id": minimaxTaskID, "status": v.status}
			if v.videoURL != "" {
				task["content"] = map[string]any{"url": v.videoURL}
			}
			if v.errMsg != "" {
				task["error"] = map[string]any{"code": "1026", "message": v.errMsg}
			}
			if v.withUsage {
				task["usage"] = map[string]any{
					"completion_tokens": v.completionToken,
					"total_tokens":      v.completionToken,
					"output_seconds":    5,
				}
			}
			json.NewEncoder(w).Encode(map[string]any{"task": task})
		case r.Method == http.MethodDelete && r.URL.Path == "/v2/video_generation/"+minimaxTaskID:
			v.cancels++
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprintf(w, `{"task_id":%q,"action":"cancelled","status":"cancelled"}`, minimaxTaskID)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(v.server.Close)
	return v
}

func (v *fakeMiniMaxVendor) script(status, videoURL, errMsg string, withUsage bool, completionTokens int64) {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.status, v.videoURL, v.errMsg = status, videoURL, errMsg
	v.withUsage, v.completionToken = withUsage, completionTokens
}

func (v *fakeMiniMaxVendor) counts() (submits, queries, cancels int) {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.submits, v.queries, v.cancels
}

func (v *fakeMiniMaxVendor) lastSubmit() (method, path, auth string, body []byte) {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.lastMethod, v.lastPath, v.lastAuth, v.lastBody
}

// seedMiniMaxChannel inserts one enabled minimax channel (videos only).
func (e *relayEnv) seedMiniMaxChannel(t *testing.T, name, baseURL, publicModel, upstreamModel string) channel.Channel {
	t.Helper()
	ch, err := e.stores.channels.Create(context.Background(), channel.Channel{
		Name: name, Type: channel.TypeMiniMax, BaseURL: baseURL,
		APIKey: "minimax-secret-key", ModelMap: map[string]string{publicModel: upstreamModel},
		Capabilities: []channel.Capability{channel.CapVideos}, Priority: 0, Enabled: true,
	})
	if err != nil {
		t.Fatalf("seed minimax channel: %v", err)
	}
	return ch
}

const minimaxVideoBody = `{"model":"mm-h3","prompt":"海面日落延时,浪声环绕","seconds":5,"size":"768P","ratio":"16:9",` +
	`"image":"https://cdn.example/first.png","last_image":"https://cdn.example/last.png",` +
	`"references":[{"url":"https://cdn.example/ref.png","kind":"image"},` +
	`{"url":"https://cdn.example/clip.mp4","kind":"video"},` +
	`{"url":"https://cdn.example/voice.mp3","kind":"audio"}]}`

// TestMiniMaxAdaptorTranslatesContract pins the gateway→MiniMax submit
// translation end to end: content 分节(text/首帧/尾帧/参考,role 与 type 各就
// 各位,与 Ark 同构)、seconds→duration、size→resolution(官方档 768P 原串
// 透传)、ratio 直传、model 已映射、Bearer 头、/v2 任务面路径;查询响应剥
// {"task":{…}} 包裹归一成网关任务面(状态、产物 content.url、失败原因);
// 取消走 DELETE /v2/video_generation/{id}。
func TestMiniMaxAdaptorTranslatesContract(t *testing.T) {
	env := newRelayEnv(t, nil)
	vendor := newFakeMiniMaxVendor(t)
	env.seedMiniMaxChannel(t, "minimax-main", vendor.server.URL, "mm-h3", "MiniMax-H3")
	key, full := env.seedKey(t, 5_000_000)
	env.seedVideoPrice(t, "mm-h3", nil) // $0.10/秒 ×1.0 → 5s 预扣 500000

	w := env.postVideo(t, full, minimaxVideoBody)
	if w.Code != http.StatusOK {
		t.Fatalf("submit status = %d body %s, want 200", w.Code, w.Body.String())
	}
	task := decodeVideoTask(t, w.Body.Bytes())
	if task.Status != "queued" || task.TaskID == "" {
		t.Fatalf("submitted = %+v, want queued with a gateway id", task)
	}

	method, path, auth, body := vendor.lastSubmit()
	if method != http.MethodPost || path != "/v2/video_generation" {
		t.Fatalf("upstream saw %s %s, want POST /v2/video_generation", method, path)
	}
	if auth != "Bearer minimax-secret-key" {
		t.Errorf("auth = %q, want the channel's Bearer key", auth)
	}
	var sent struct {
		Model      string           `json:"model"`
		Content    []map[string]any `json:"content"`
		Duration   *int64           `json:"duration"`
		Resolution string           `json:"resolution"`
		Ratio      string           `json:"ratio"`
	}
	if err := json.Unmarshal(body, &sent); err != nil {
		t.Fatalf("submit body not JSON: %v (%s)", err, body)
	}
	if sent.Model != "MiniMax-H3" {
		t.Errorf("model = %q, want the mapped upstream id", sent.Model)
	}
	if sent.Duration == nil || *sent.Duration != 5 {
		t.Errorf("duration = %v, want seconds 5 carried as duration", sent.Duration)
	}
	if sent.Resolution != "768P" || sent.Ratio != "16:9" {
		t.Errorf("resolution/ratio = %q/%q, want 768P/16:9 verbatim", sent.Resolution, sent.Ratio)
	}
	if len(sent.Content) != 6 {
		t.Fatalf("content = %d 分节, want 6 (text+首帧+尾帧+3 参考)", len(sent.Content))
	}
	wantParts := []struct {
		typ, role string
	}{
		{"text", ""},
		{"image_url", "first_frame"},
		{"image_url", "last_frame"},
		{"image_url", "reference_image"},
		{"video_url", "reference_video"},
		{"audio_url", "reference_audio"},
	}
	for i, want := range wantParts {
		part := sent.Content[i]
		if part["type"] != want.typ {
			t.Errorf("content[%d].type = %v, want %s", i, part["type"], want.typ)
			continue
		}
		if want.role != "" && part["role"] != want.role {
			t.Errorf("content[%d].role = %v, want %s", i, part["role"], want.role)
		}
		if want.typ != "text" {
			holder, _ := part[want.typ].(map[string]any)
			if holder == nil || !strings.HasPrefix(holder["url"].(string), "https://") {
				t.Errorf("content[%d].%s.url = %v, want the reference URL", i, want.typ, part[want.typ])
			}
		}
	}

	// 查询:{"task":{…}} 包裹剥开,MiniMax 状态词汇归并走既有五态机;
	// cancelled(双 L)归并 canceled 并退款。
	vendor.script("queued", "", "", false, 0)
	if w = env.getVideoTask(t, full, task.TaskID); decodeVideoTask(t, w.Body.Bytes()).Status != "queued" {
		t.Errorf("MiniMax queued merged wrong")
	}
	vendor.script("cancelled", "", "canceled by user", false, 0)
	if w = env.getVideoTask(t, full, task.TaskID); decodeVideoTask(t, w.Body.Bytes()).Status != "canceled" {
		t.Errorf("MiniMax cancelled should merge canceled (refunded)")
	}
	if balance := env.balanceOf(t, key.ID); balance != 5_000_000 {
		t.Errorf("balance after cancel = %d, want the full refund (pre-deduction returned)", balance)
	}

	// 取消端点:本地取消触发厂商 DELETE(尽力而为)。
	task2 := decodeVideoTask(t, env.postVideo(t, full, minimaxVideoBody).Body.Bytes())
	if w = env.cancelVideoTask(t, full, task2.TaskID); w.Code != http.StatusOK {
		t.Fatalf("cancel status = %d", w.Code)
	}
	if _, _, cancels := vendor.counts(); cancels != 1 {
		t.Errorf("vendor DELETEs = %d, want 1 for the canceled task", cancels)
	}
	if m, _, _, _ := vendor.lastSubmit(); m != http.MethodDelete {
		t.Errorf("last vendor method = %s, want DELETE on the cancel path", m)
	}
}

// TestMiniMaxSecondTrackSettlesAtReserve pins the second track's happy path
// (45 号票定案 H3 走 second 轨):预扣 $0.10/秒 × 5s = 500000,成功终态结算
// = 预扣零差额(不因厂商报的 usage.completion_tokens 改账),无 settle 流
// 水,用量行 unit=second。
func TestMiniMaxSecondTrackSettlesAtReserve(t *testing.T) {
	env := newRelayEnv(t, nil)
	vendor := newFakeMiniMaxVendor(t)
	env.seedMiniMaxChannel(t, "minimax-main", vendor.server.URL, "mm-h3", "MiniMax-H3")
	key, full := env.seedKey(t, 5_000_000)
	env.seedVideoPrice(t, "mm-h3", nil) // $0.10/秒,系数全 ×1.0

	task := decodeVideoTask(t, env.postVideo(t, full, minimaxVideoBody).Body.Bytes())
	if balance := env.balanceOf(t, key.ID); balance != 5_000_000-500_000 {
		t.Fatalf("balance after submit = %d, want 4_500_000 held by the per-second estimate", balance)
	}

	// 成功:厂商报了 usage 也不动账 —— second 轨结算即预扣。
	vendor.script("succeeded", "https://cdn.hailuoai.com/prod/out.mp4", "", true, 260_390)
	w := env.getVideoTask(t, full, task.TaskID)
	if w.Code != http.StatusOK {
		t.Fatalf("poll status = %d body %s", w.Code, w.Body.String())
	}
	done := decodeVideoTask(t, w.Body.Bytes())
	if done.Status != "succeeded" || done.VideoURL != "https://cdn.hailuoai.com/prod/out.mp4" {
		t.Fatalf("final = %+v, want succeeded with the video URL", done)
	}
	if balance := env.balanceOf(t, key.ID); balance != 5_000_000-500_000 {
		t.Errorf("balance after settle = %d, want unchanged (second track settles at the reserve)", balance)
	}
	entries := env.quotaLog(t, key.ID) // newest first
	if len(entries) != 2 || entries[0].Reason != "estimate" || entries[1].Reason != "initial" {
		t.Fatalf("ledger = %+v, want initial + estimate only (no settle)", entries)
	}
	if row := env.taskRow(t, task.TaskID); row.ChargeMicros != 500_000 {
		t.Errorf("task row charge = %d, want the reserve kept as the final charge", row.ChargeMicros)
	}
	rows := env.usageRows(t)
	if len(rows) != 1 {
		t.Fatalf("usage rows = %d, want 1", len(rows))
	}
	if trail := rows[0]; trail.Unit != "second" || trail.Status != usage.StatusSuccess ||
		trail.ChargeMicros != 500_000 || trail.CompletionTokens != 0 {
		t.Errorf("trail = %+v, want a second-track success row at the reserve", trail)
	}
}

// TestMiniMaxFailureRefunds:失败退款语义对 second 轨不变,厂商原因透传。
func TestMiniMaxFailureRefunds(t *testing.T) {
	env := newRelayEnv(t, nil)
	vendor := newFakeMiniMaxVendor(t)
	env.seedMiniMaxChannel(t, "minimax-main", vendor.server.URL, "mm-h3", "MiniMax-H3")
	key, full := env.seedKey(t, 5_000_000)
	env.seedVideoPrice(t, "mm-h3", nil)

	task := decodeVideoTask(t, env.postVideo(t, full, minimaxVideoBody).Body.Bytes())
	vendor.script("failed", "", "sensitive content rejected", false, 0)
	w := env.getVideoTask(t, full, task.TaskID)
	if w.Code != http.StatusOK {
		t.Fatalf("poll status = %d", w.Code)
	}
	got := decodeVideoTask(t, w.Body.Bytes())
	if got.Status != "failed" || got.Error == nil || !strings.Contains(got.Error.Message, "sensitive content rejected") {
		t.Fatalf("final = %+v, want failed with the vendor reason", got)
	}
	if balance := env.balanceOf(t, key.ID); balance != 5_000_000 {
		t.Errorf("balance after failure = %d, want the whole reserve refunded", balance)
	}
}

// TestMiniMaxAutoDurationCarriesDefault:「自动时长」(不传 seconds)提交
// 到 MiniMax 时 duration 必填 —— adaptor 补网关缺省秒数 5,与计费预扣同值
// ($0.10/秒 × 5s),账实一致(review 修复:网关 seconds 缺省不改写请求体,
// Ark 的 duration 可选不受影响,MiniMax 必填不补会 400)。
func TestMiniMaxAutoDurationCarriesDefault(t *testing.T) {
	env := newRelayEnv(t, nil)
	vendor := newFakeMiniMaxVendor(t)
	env.seedMiniMaxChannel(t, "minimax-main", vendor.server.URL, "mm-h3", "MiniMax-H3")
	key, full := env.seedKey(t, 5_000_000)
	env.seedVideoPrice(t, "mm-h3", nil)

	auto := `{"model":"mm-h3","prompt":"海面日落延时","size":"768P"}`
	task := decodeVideoTask(t, env.postVideo(t, full, auto).Body.Bytes())
	if task.Status != "queued" {
		t.Fatalf("submitted = %+v, want queued", task)
	}
	if balance := env.balanceOf(t, key.ID); balance != 5_000_000-500_000 {
		t.Errorf("balance after submit = %d, want the 5s default held by the estimate", balance)
	}
	_, _, _, body := vendor.lastSubmit()
	var sent struct {
		Duration *int64 `json:"duration"`
	}
	if err := json.Unmarshal(body, &sent); err != nil {
		t.Fatalf("submit body not JSON: %v (%s)", err, body)
	}
	if sent.Duration == nil || *sent.Duration != 5 {
		t.Errorf("duration = %v, want the gateway default 5 carried for auto-duration submits", sent.Duration)
	}
}

// TestMiniMaxSubmitWithoutTaskIDRejected:2xx 应答不带 task_id 是上游坏形状
// —— adaptor 归一 502 形,单渠道耗尽后提交失败,不建任务行、余额原封。
func TestMiniMaxSubmitWithoutTaskIDRejected(t *testing.T) {
	env := newRelayEnv(t, nil)
	vendor := newFakeMiniMaxVendor(t)
	vendor.mu.Lock()
	vendor.submitBody = `{}`
	vendor.mu.Unlock()
	env.seedMiniMaxChannel(t, "minimax-main", vendor.server.URL, "mm-h3", "MiniMax-H3")
	key, full := env.seedKey(t, 5_000_000)
	env.seedVideoPrice(t, "mm-h3", nil)

	w := env.postVideo(t, full, minimaxVideoBody)
	if w.Code == http.StatusOK {
		t.Fatalf("status = 200 body %s, want a rejected submit", w.Body.String())
	}
	if balance := env.balanceOf(t, key.ID); balance != 5_000_000 {
		t.Errorf("balance = %d, want untouched", balance)
	}
	if n := env.videoTaskCount(t); n != 0 {
		t.Errorf("task rows = %d, want 0", n)
	}
}
