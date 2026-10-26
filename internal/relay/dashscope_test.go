package relay_test

// 46 号票(阿里云百炼 DashScope 渠道)的端到端测试:fake 百炼上游用官方
// video-synthesis 异步面形状(POST /api/v1/services/aigc/video-generation/
// video-synthesis 提交回 {"output":{"task_id",…}} 且必验 X-DashScope-Async
// 头、GET /api/v1/tasks/{id} 轮询回 {"output":{…}})验证 adaptor 翻译、
// adaptorFor 分发与 second 轨账务时序(预扣按秒×系数 → 结算=预扣零差额 /
// 失败退款),UNKNOWN 状态归 failed 退款,取消为合成 2xx(厂商侧无 DELETE),
// 以及 2xx 无 task_id 的防御。共享 relay_test.go 的 MySQL 环境。

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
)

// fakeDashScopeVendor 是百炼 video-synthesis 任务面替身:提交回
// {"output":{"task_id",…}},查询回 {"output":{…}}(task_status / video_url /
// code+message)。测试在轮询间隙改剧本;最近一次提交体与请求头被留存供
// 翻译断言。DashScope 无公开取消 API,替身对 DELETE 等其他方法一律 404 ——
// 取消方法的合成 2xx 不该产生任何上游请求。
type fakeDashScopeVendor struct {
	server *httptest.Server
	mu     sync.Mutex

	status     string // 查询端点当前回的 output.task_status 原文
	videoURL   string // succeeded 时 output.video_url
	errMsg     string // failed 时 output.message
	errCode    string // failed 时 output.code
	submitBody string // 提交应答覆盖(空 = 正常回 output.task_id)

	submits     int
	queries     int
	lastBody    []byte
	lastAuth    string
	lastPath    string
	lastMethod  string
	lastAsync   string
	lastMethods map[string]int // 全部请求按方法计数(取消应无 DELETE)
}

const dashscopeTaskID = "ca7eaceb-2b00-4f29-9fac-6f1e2d3c4b5a"

func newFakeDashScopeVendor(t *testing.T) *fakeDashScopeVendor {
	t.Helper()
	v := &fakeDashScopeVendor{lastMethods: map[string]int{}}
	v.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		v.mu.Lock()
		defer v.mu.Unlock()
		raw, _ := io.ReadAll(r.Body)
		v.lastAuth = r.Header.Get("Authorization")
		v.lastMethod = r.Method
		v.lastPath = r.URL.Path
		v.lastAsync = r.Header.Get("X-DashScope-Async")
		v.lastMethods[r.Method]++

		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/services/aigc/video-generation/video-synthesis":
			v.submits++
			v.lastBody = raw
			w.Header().Set("Content-Type", "application/json")
			body := v.submitBody
			if body == "" {
				body = fmt.Sprintf(`{"request_id":"req-1","output":{"task_id":%q,"task_status":"PENDING"}}`, dashscopeTaskID)
			}
			w.Write([]byte(body))
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/tasks/"+dashscopeTaskID:
			v.queries++
			w.Header().Set("Content-Type", "application/json")
			output := map[string]any{"task_id": dashscopeTaskID, "task_status": v.status}
			if v.videoURL != "" {
				output["video_url"] = v.videoURL
			}
			if v.errMsg != "" || v.errCode != "" {
				output["code"] = v.errCode
				output["message"] = v.errMsg
			}
			json.NewEncoder(w).Encode(map[string]any{
				"request_id": "req-2",
				"output":     output,
				"usage": map[string]any{
					"SR": "768", "duration": 5, "image_count": 0, "input_seconds": 0,
					"output_seconds": 5, "size": "1344*768", "total_seconds": 5, "video_count": 1,
				},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(v.server.Close)
	return v
}

func (v *fakeDashScopeVendor) script(status, videoURL, errCode, errMsg string) {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.status, v.videoURL, v.errCode, v.errMsg = status, videoURL, errCode, errMsg
}

func (v *fakeDashScopeVendor) counts() (submits, queries int) {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.submits, v.queries
}

func (v *fakeDashScopeVendor) lastSubmit() (method, path, auth, async string, body []byte) {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.lastMethod, v.lastPath, v.lastAuth, v.lastAsync, v.lastBody
}

func (v *fakeDashScopeVendor) methodCounts() map[string]int {
	v.mu.Lock()
	defer v.mu.Unlock()
	out := make(map[string]int, len(v.lastMethods))
	for m, n := range v.lastMethods {
		out[m] = n
	}
	return out
}

// seedDashScopeChannel inserts one enabled dashscope channel (videos only).
func (e *relayEnv) seedDashScopeChannel(t *testing.T, name, baseURL, publicModel, upstreamModel string) channel.Channel {
	t.Helper()
	ch, err := e.stores.channels.Create(context.Background(), channel.Channel{
		Name: name, Type: channel.TypeDashScope, BaseURL: baseURL,
		APIKey: "sk-bailian-secret", ModelMap: map[string]string{publicModel: upstreamModel},
		Capabilities: []channel.Capability{channel.CapVideos}, Priority: 0, Enabled: true,
	})
	if err != nil {
		t.Fatalf("seed dashscope channel: %v", err)
	}
	return ch
}

// TestDashScopeAdaptorTranslatesContract pins the gateway→DashScope submit
// translation end to end:三段分离(model / input / parameters)、扁平 media
// 五类(first_frame/last_frame/image_url/feature/driving_audio)、seconds→
// parameters.duration、size→resolution(官方档 768P 原串透传)、ratio 直传、
// X-DashScope-Async 头、Bearer、/api/v1 路径;查询响应剥 output 归一成网关
// 任务面(PENDING 大写归并 queued、SUCCEEDED+video_url、FAILED 的 code/
// message 兜底);UNKNOWN 走未知态归 failed 并退款;取消不产生厂商请求。
func TestDashScopeAdaptorTranslatesContract(t *testing.T) {
	env := newRelayEnv(t, nil)
	vendor := newFakeDashScopeVendor(t)
	env.seedDashScopeChannel(t, "bailian-main", vendor.server.URL, "mm-h3", "MiniMax/MiniMax-H3")
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

	method, path, auth, async, body := vendor.lastSubmit()
	if method != http.MethodPost || path != "/api/v1/services/aigc/video-generation/video-synthesis" {
		t.Fatalf("upstream saw %s %s, want POST the video-synthesis path", method, path)
	}
	if async != "enable" {
		t.Errorf("X-DashScope-Async = %q, want enable (缺失官方拒同步调用)", async)
	}
	if auth != "Bearer sk-bailian-secret" {
		t.Errorf("auth = %q, want the channel's Bearer key", auth)
	}
	var sent struct {
		Model string `json:"model"`
		Input struct {
			Prompt string           `json:"prompt"`
			Media  []map[string]any `json:"media"`
		} `json:"input"`
		Parameters struct {
			Duration   *int64 `json:"duration"`
			Resolution string `json:"resolution"`
			Ratio      string `json:"ratio"`
		} `json:"parameters"`
	}
	if err := json.Unmarshal(body, &sent); err != nil {
		t.Fatalf("submit body not JSON: %v (%s)", err, body)
	}
	if sent.Model != "MiniMax/MiniMax-H3" {
		t.Errorf("model = %q, want the mapped upstream id", sent.Model)
	}
	if sent.Input.Prompt == "" {
		t.Errorf("input.prompt empty, want the gateway prompt carried")
	}
	if sent.Parameters.Duration == nil || *sent.Parameters.Duration != 5 {
		t.Errorf("parameters.duration = %v, want seconds 5 carried", sent.Parameters.Duration)
	}
	if sent.Parameters.Resolution != "768P" || sent.Parameters.Ratio != "16:9" {
		t.Errorf("resolution/ratio = %q/%q, want 768P/16:9 verbatim", sent.Parameters.Resolution, sent.Parameters.Ratio)
	}
	if len(sent.Input.Media) != 5 {
		t.Fatalf("input.media = %d 条, want 5 (首帧+尾帧+3 参考)", len(sent.Input.Media))
	}
	wantParts := []struct {
		typ, urlSuffix string
	}{
		{"first_frame", "first.png"},
		{"last_frame", "last.png"},
		{"image_url", "ref.png"},
		{"feature", "clip.mp4"},
		{"driving_audio", "voice.mp3"},
	}
	for i, want := range wantParts {
		part := sent.Input.Media[i]
		if part["type"] != want.typ {
			t.Errorf("media[%d].type = %v, want %s", i, part["type"], want.typ)
		}
		if u, _ := part["url"].(string); !strings.HasSuffix(u, want.urlSuffix) {
			t.Errorf("media[%d].url = %v, want …%s", i, part["url"], want.urlSuffix)
		}
	}

	// 查询:PENDING 大写归并 queued;UNKNOWN(任务不存在/过期)未知态归
	// failed 并退款。终态后轮询不再打扰上游,每个终态各用新任务演。
	vendor.script("PENDING", "", "", "")
	if w = env.getVideoTask(t, full, task.TaskID); decodeVideoTask(t, w.Body.Bytes()).Status != "queued" {
		t.Errorf("DashScope PENDING merged wrong")
	}
	vendor.script("UNKNOWN", "", "", "")
	got := decodeVideoTask(t, env.getVideoTask(t, full, task.TaskID).Body.Bytes())
	if got.Status != "failed" || got.Error == nil {
		t.Fatalf("UNKNOWN = %+v, want failed (未知态归并) with an error", got)
	}
	if balance := env.balanceOf(t, key.ID); balance != 5_000_000 {
		t.Errorf("balance after unknown = %d, want the whole reserve refunded", balance)
	}

	// FAILED:message 兜底 code,退款语义不变。
	task2 := decodeVideoTask(t, env.postVideo(t, full, minimaxVideoBody).Body.Bytes())
	vendor.script("FAILED", "", "InvalidParameter", "The parameter is invalid")
	got = decodeVideoTask(t, env.getVideoTask(t, full, task2.TaskID).Body.Bytes())
	if got.Status != "failed" || got.Error == nil || !strings.Contains(got.Error.Message, "The parameter is invalid") {
		t.Fatalf("FAILED = %+v, want failed with the vendor reason", got)
	}
	if balance := env.balanceOf(t, key.ID); balance != 5_000_000 {
		t.Errorf("balance after failure = %d, want the whole reserve refunded", balance)
	}

	// SUCCEEDED:产物在 output.video_url。
	task3 := decodeVideoTask(t, env.postVideo(t, full, minimaxVideoBody).Body.Bytes())
	vendor.script("SUCCEEDED", "https://oss.aliyuncs.com/out.mp4", "", "")
	got = decodeVideoTask(t, env.getVideoTask(t, full, task3.TaskID).Body.Bytes())
	if got.Status != "succeeded" || got.VideoURL != "https://oss.aliyuncs.com/out.mp4" {
		t.Fatalf("SUCCEEDED = %+v, want succeeded with output.video_url", got)
	}

	// 取消:本地取消走合成 2xx,厂商侧不产生任何 DELETE。
	task4 := decodeVideoTask(t, env.postVideo(t, full, minimaxVideoBody).Body.Bytes())
	if w = env.cancelVideoTask(t, full, task4.TaskID); w.Code != http.StatusOK {
		t.Fatalf("cancel status = %d", w.Code)
	}
	if deletes := vendor.methodCounts()["DELETE"]; deletes != 0 {
		t.Errorf("vendor DELETEs = %d, want 0 (DashScope 无公开取消 API)", deletes)
	}
	if balance := env.balanceOf(t, key.ID); balance != 5_000_000-500_000 {
		t.Errorf("balance after cancel = %d, want task3's settled charge only", balance)
	}
}

// TestDashScopeSecondTrackSettlesAtReserve pins the second track's happy path
// (与 minimax 同轨):预扣 $0.10/秒 × 5s = 500000,成功终态结算 = 预扣零差额
// (DashScope usage 无 completion_tokens,照零记),用量行 unit=second。
func TestDashScopeSecondTrackSettlesAtReserve(t *testing.T) {
	env := newRelayEnv(t, nil)
	vendor := newFakeDashScopeVendor(t)
	env.seedDashScopeChannel(t, "bailian-main", vendor.server.URL, "mm-h3", "MiniMax/MiniMax-H3")
	key, full := env.seedKey(t, 5_000_000)
	env.seedVideoPrice(t, "mm-h3", nil) // $0.10/秒,系数全 ×1.0

	task := decodeVideoTask(t, env.postVideo(t, full, minimaxVideoBody).Body.Bytes())
	if balance := env.balanceOf(t, key.ID); balance != 5_000_000-500_000 {
		t.Fatalf("balance after submit = %d, want 4_500_000 held by the per-second estimate", balance)
	}

	vendor.script("SUCCEEDED", "https://oss-cn-beijing.aliyuncs.com/out.mp4", "", "")
	w := env.getVideoTask(t, full, task.TaskID)
	if w.Code != http.StatusOK {
		t.Fatalf("poll status = %d body %s", w.Code, w.Body.String())
	}
	done := decodeVideoTask(t, w.Body.Bytes())
	if done.Status != "succeeded" || done.VideoURL != "https://oss-cn-beijing.aliyuncs.com/out.mp4" {
		t.Fatalf("final = %+v, want succeeded with the video URL", done)
	}
	if balance := env.balanceOf(t, key.ID); balance != 5_000_000-500_000 {
		t.Errorf("balance after settle = %d, want unchanged (second track settles at the reserve)", balance)
	}
	entries := env.quotaLog(t, key.ID)
	if len(entries) != 2 || entries[0].Reason != "estimate" || entries[1].Reason != "initial" {
		t.Fatalf("ledger = %+v, want initial + estimate only (no settle)", entries)
	}
	rows := env.usageRows(t)
	if len(rows) != 1 {
		t.Fatalf("usage rows = %d, want 1", len(rows))
	}
	if trail := rows[0]; trail.Unit != "second" || trail.CompletionTokens != 0 {
		t.Errorf("trail = %+v, want a second-track row without completion tokens", trail)
	}
}

// TestDashScopeFailureRefunds:厂商失败原因(output.message)透传并全款退。
func TestDashScopeFailureRefunds(t *testing.T) {
	env := newRelayEnv(t, nil)
	vendor := newFakeDashScopeVendor(t)
	env.seedDashScopeChannel(t, "bailian-main", vendor.server.URL, "mm-h3", "MiniMax/MiniMax-H3")
	key, full := env.seedKey(t, 5_000_000)
	env.seedVideoPrice(t, "mm-h3", nil)

	task := decodeVideoTask(t, env.postVideo(t, full, minimaxVideoBody).Body.Bytes())
	vendor.script("FAILED", "", "InvalidParameter", "sensitive content rejected")
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

// TestDashScopeAutoDurationCarriesDefault:「自动时长」(不传 seconds)提交
// 到百炼时 parameters.duration 必填 —— adaptor 补网关缺省秒数 5,与计费预扣
// 同值($0.10/秒 × 5s),账实一致(MiniMax 同款必填补齐)。
func TestDashScopeAutoDurationCarriesDefault(t *testing.T) {
	env := newRelayEnv(t, nil)
	vendor := newFakeDashScopeVendor(t)
	env.seedDashScopeChannel(t, "bailian-main", vendor.server.URL, "mm-h3", "MiniMax/MiniMax-H3")
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
	_, _, _, _, body := vendor.lastSubmit()
	var sent struct {
		Parameters struct {
			Duration *int64 `json:"duration"`
		} `json:"parameters"`
	}
	if err := json.Unmarshal(body, &sent); err != nil {
		t.Fatalf("submit body not JSON: %v (%s)", err, body)
	}
	if sent.Parameters.Duration == nil || *sent.Parameters.Duration != 5 {
		t.Errorf("duration = %v, want the gateway default 5 carried for auto-duration submits", sent.Parameters.Duration)
	}
}

// TestDashScopeSubmitWithoutTaskIDRejected:2xx 应答不带 output.task_id 是
// 上游坏形状 —— adaptor 归一 502 形,单渠道耗尽后提交失败,不建任务行、
// 余额原封。
func TestDashScopeSubmitWithoutTaskIDRejected(t *testing.T) {
	env := newRelayEnv(t, nil)
	vendor := newFakeDashScopeVendor(t)
	vendor.mu.Lock()
	vendor.submitBody = `{"request_id":"req-1"}`
	vendor.mu.Unlock()
	env.seedDashScopeChannel(t, "bailian-main", vendor.server.URL, "mm-h3", "MiniMax/MiniMax-H3")
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
