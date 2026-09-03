package relay_test

// 08 号票(视频异步任务契约与上游代理)的端到端测试:提交返回 task_id、
// 轮询推进五态、取消不扣费;fake 上游用真实厂商状态名(THROTTLED、
// PENDING、UNKNOWN、succeed……)验证归并;「仅成功计费」—— 失败/取消
// 预扣全额退回,任务行与用量日志可查。共享 relay_test.go 的 MySQL 环境,
// 上游是内存 fake(spec 的主缝:HTTP API + fake 上游 + 真实 MySQL)。

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

	"github.com/gachal/InfiniteChance/internal/apikey"
	"github.com/gachal/InfiniteChance/internal/channel"
	"github.com/gachal/InfiniteChance/internal/pricing"
	"github.com/gachal/InfiniteChance/internal/usage"
	"github.com/gachal/InfiniteChance/internal/videotask"
)

// fakeVideoVendor 是异步视频厂商替身:同一内存服务器按剧本回答提交、
// 轮询与取消。status/url/errMsg 由测试在轮询间隙改动(mu 保护)模拟任务
// 生命周期;原始状态串原样下发,归并由网关做。
type fakeVideoVendor struct {
	server *httptest.Server
	mu     sync.Mutex

	status       string // 查询端点当前回的 task_status 原文
	url          string // succeeded 时随查询回的产物地址
	errMsg       string // 查询端点随失败态回的厂商原因
	submitStatus int    // 提交端点回答码(0 = 200)
	submitBody   string // 非空则原样作为提交响应体
	queryStatus  int    // 查询端点回答码(0 = 200)
	queryBody    string // 非空则原样作为查询响应体(坏形状用)

	submits int
	queries int
	cancels int
	models  []string
	auths   []string
}

func newFakeVideoVendor(t *testing.T) *fakeVideoVendor {
	t.Helper()
	v := &fakeVideoVendor{}
	v.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		v.mu.Lock()
		defer v.mu.Unlock()
		raw, _ := io.ReadAll(r.Body)
		var body struct {
			Model string `json:"model"`
		}
		_ = json.Unmarshal(raw, &body)
		v.auths = append(v.auths, r.Header.Get("Authorization"))

		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/videos/generations":
			v.submits++
			v.models = append(v.models, body.Model)
			w.Header().Set("Content-Type", "application/json")
			if v.submitStatus != 0 && v.submitStatus != http.StatusOK {
				w.WriteHeader(v.submitStatus)
				w.Write([]byte(v.submitBody))
				return
			}
			if v.submitBody != "" {
				w.Write([]byte(v.submitBody))
				return
			}
			w.Write([]byte(`{"task_id":"vendor-task-1"}`))
		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/videos/tasks/"):
			v.queries++
			w.Header().Set("Content-Type", "application/json")
			if v.queryStatus != 0 && v.queryStatus != http.StatusOK {
				w.WriteHeader(v.queryStatus)
				w.Write([]byte(v.queryBody))
				return
			}
			if v.queryBody != "" {
				w.Write([]byte(v.queryBody))
				return
			}
			answer := map[string]any{
				"task_id":     "vendor-task-1",
				"task_status": v.status,
			}
			if v.url != "" {
				answer["video_url"] = v.url
			}
			if v.errMsg != "" {
				answer["error"] = map[string]any{"message": v.errMsg}
			}
			json.NewEncoder(w).Encode(answer)
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/cancel"):
			v.cancels++
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{}`))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(v.server.Close)
	return v
}

// script sets the query endpoint's next answer (原始状态、产物、失败原因).
func (v *fakeVideoVendor) script(status, url, errMsg string) {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.status, v.url, v.errMsg = status, url, errMsg
}

func (v *fakeVideoVendor) counts() (submits, queries, cancels int) {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.submits, v.queries, v.cancels
}

func (v *fakeVideoVendor) lastModelAndAuth() (string, string) {
	v.mu.Lock()
	defer v.mu.Unlock()
	if len(v.models) == 0 {
		return "", ""
	}
	return v.models[len(v.models)-1], v.auths[len(v.auths)-1]
}

// seedVideoChannel inserts one enabled channel with explicit capabilities
// and priority, mapping publicModel→upstreamModel.
func (e *relayEnv) seedVideoChannel(t *testing.T, name, baseURL, publicModel, upstreamModel string, priority int, caps []channel.Capability) channel.Channel {
	t.Helper()
	ch, err := e.stores.channels.Create(context.Background(), channel.Channel{
		Name: name, Type: channel.TypeOpenAI, BaseURL: baseURL,
		APIKey: "vendor-secret-key", ModelMap: map[string]string{publicModel: upstreamModel},
		Capabilities: caps, Priority: priority, Enabled: true,
	})
	if err != nil {
		t.Fatalf("seed video channel: %v", err)
	}
	return ch
}

// seedVideoPrice writes the second-track test price: $0.10/秒 plus optional
// resolution factors (micro-units;nil = 全部 ×1.0).
func (e *relayEnv) seedVideoPrice(t *testing.T, publicModel string, factors map[string]int64) {
	t.Helper()
	_, err := e.stores.prices.Upsert(context.Background(), pricing.Price{
		PublicModel: publicModel, Unit: pricing.UnitSecond,
		Call: &pricing.CallPrice{USDPerCallMicros: 100_000, SizeFactorMicros: factors},
	})
	if err != nil {
		t.Fatalf("seed video price: %v", err)
	}
}

const videoBody = `{"model":"vid-m","prompt":"一只猫在月光下奔跑","seconds":5,"size":"720p"}`

func (e *relayEnv) postVideo(t *testing.T, fullKey, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/v1/videos/generations", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+fullKey)
	w := httptest.NewRecorder()
	e.engine.ServeHTTP(w, req)
	return w
}

func (e *relayEnv) getVideoTask(t *testing.T, fullKey, id string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/v1/videos/tasks/"+id, nil)
	req.Header.Set("Authorization", "Bearer "+fullKey)
	w := httptest.NewRecorder()
	e.engine.ServeHTTP(w, req)
	return w
}

func (e *relayEnv) cancelVideoTask(t *testing.T, fullKey, id string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/v1/videos/tasks/"+id+"/cancel", nil)
	req.Header.Set("Authorization", "Bearer "+fullKey)
	w := httptest.NewRecorder()
	e.engine.ServeHTTP(w, req)
	return w
}

// videoTaskResp mirrors the external task object.
type videoTaskResp struct {
	TaskID    string `json:"task_id"`
	Status    string `json:"status"`
	Model     string `json:"model"`
	CreatedAt int64  `json:"created_at"`
	Seconds   int64  `json:"seconds"`
	Size      string `json:"size"`
	VideoURL  string `json:"video_url"`
	Error     *struct {
		Message string `json:"message"`
	} `json:"error"`
}

func decodeVideoTask(t *testing.T, body []byte) videoTaskResp {
	t.Helper()
	var resp videoTaskResp
	if err := json.Unmarshal(body, &resp); err != nil {
		t.Fatalf("task response not JSON: %v (%s)", err, body)
	}
	return resp
}

// taskRow reads the stored row through the store(现账本事实).
func (e *relayEnv) taskRow(t *testing.T, id string) videotask.Task {
	t.Helper()
	task, err := e.stores.tasks.Get(context.Background(), id)
	if err != nil {
		t.Fatalf("read task row %s: %v", id, err)
	}
	return task
}

func (e *relayEnv) videoTaskCount(t *testing.T) int64 {
	t.Helper()
	var n int64
	if err := e.stores.db.QueryRow("SELECT COUNT(*) FROM video_tasks").Scan(&n); err != nil {
		t.Fatalf("count video_tasks: %v", err)
	}
	return n
}

func TestRelayVideoSubmitPollSucceedEndToEnd(t *testing.T) {
	env := newRelayEnv(t, nil)
	vendor := newFakeVideoVendor(t)
	vendor.script("THROTTLED", "", "")
	env.seedVideoChannel(t, "wan", vendor.server.URL, "vid-m", "upstream-vid", 0,
		[]channel.Capability{channel.CapVideos})
	key, full := env.seedKey(t, 2_000_000)
	// $0.10/秒,720p 系数 ×2.0 → 每秒 200000 micros,5 秒预扣 1_000_000。
	env.seedVideoPrice(t, "vid-m", map[string]int64{"720p": 2_000_000})

	// 提交:拿到网关 task_id,状态 queued,请求事实回显。
	w := env.postVideo(t, full, videoBody)
	if w.Code != http.StatusOK {
		t.Fatalf("submit status = %d body %s, want 200", w.Code, w.Body.String())
	}
	submitted := decodeVideoTask(t, w.Body.Bytes())
	if !strings.HasPrefix(submitted.TaskID, "vt_") {
		t.Errorf("task_id = %q, want a vt_ gateway id", submitted.TaskID)
	}
	if submitted.Status != "queued" || submitted.Model != "vid-m" ||
		submitted.Seconds != 5 || submitted.Size != "720p" || submitted.CreatedAt == 0 {
		t.Errorf("submitted = %+v, want queued with echoed request facts", submitted)
	}

	// 上游收到提交:模型已映射、厂商密钥在头里。
	if model, auth := vendor.lastModelAndAuth(); model != "upstream-vid" || auth != "Bearer vendor-secret-key" {
		t.Errorf("upstream model/auth = %q/%q, want mapped name + vendor key", model, auth)
	}

	// 账务:预扣已落,余额 = 2_000_000 − 1_000_000。
	if balance := env.balanceOf(t, key.ID); balance != 1_000_000 {
		t.Fatalf("balance after submit = %d, want 1_000_000 held", balance)
	}

	// 轮询 1:厂商 THROTTLED(节流态)归并 queued。
	w = env.getVideoTask(t, full, submitted.TaskID)
	if w.Code != http.StatusOK {
		t.Fatalf("poll status = %d body %s, want 200", w.Code, w.Body.String())
	}
	if got := decodeVideoTask(t, w.Body.Bytes()); got.Status != "queued" {
		t.Errorf("THROTTLED merged to %q, want queued", got.Status)
	}
	row := env.taskRow(t, submitted.TaskID)
	if row.Status != videotask.StatusQueued || row.UpstreamStatus != "THROTTLED" {
		t.Errorf("task row = %s/%s, want queued/THROTTLED", row.Status, row.UpstreamStatus)
	}

	// 轮询 2:RUNNING → running。
	vendor.script("RUNNING", "", "")
	w = env.getVideoTask(t, full, submitted.TaskID)
	if got := decodeVideoTask(t, w.Body.Bytes()); got.Status != "running" {
		t.Errorf("RUNNING merged to %q, want running", got.Status)
	}

	// 轮询 3:SUCCEEDED + 产物 → succeeded,拿到 video_url。
	vendor.script("SUCCEEDED", "https://cdn.example/cat.mp4", "")
	w = env.getVideoTask(t, full, submitted.TaskID)
	if w.Code != http.StatusOK {
		t.Fatalf("final poll status = %d body %s, want 200", w.Code, w.Body.String())
	}
	done := decodeVideoTask(t, w.Body.Bytes())
	if done.Status != "succeeded" || done.VideoURL != "https://cdn.example/cat.mp4" {
		t.Errorf("final = %+v, want succeeded with the vendor URL", done)
	}

	// 账务:实扣即预扣(差额为零不落 settle 流水),流水只有 initial+estimate。
	if balance := env.balanceOf(t, key.ID); balance != 1_000_000 {
		t.Errorf("balance after success = %d, want 1_000_000 (reserve became the charge)", balance)
	}
	entries := env.quotaLog(t, key.ID)
	if len(entries) != 2 || entries[0].Reason != apikey.ReasonEstimate {
		t.Fatalf("ledger = %+v, want initial + estimate only", entries)
	}

	// 任务行:终态、产物、扣费定格。
	row = env.taskRow(t, submitted.TaskID)
	if row.Status != videotask.StatusSucceeded || row.VideoURL != "https://cdn.example/cat.mp4" ||
		row.ChargeMicros != 1_000_000 || row.UpstreamStatus != "SUCCEEDED" {
		t.Errorf("task row = %+v, want succeeded with url and charge", row)
	}

	// 留痕:unit=second、成功、扣 1_000_000、快照带 second 价格与请求事实。
	rows := env.usageRows(t)
	if len(rows) != 1 {
		t.Fatalf("usage rows = %d, want 1", len(rows))
	}
	trail := rows[0]
	if trail.Unit != "second" || trail.Status != usage.StatusSuccess || trail.ChargeMicros != 1_000_000 ||
		trail.PromptTokens != 0 || trail.CompletionTokens != 0 ||
		trail.PublicModel != "vid-m" || trail.UpstreamModel != "upstream-vid" {
		t.Errorf("trail = %+v, want a second-track success row", trail)
	}
	var snapshot struct {
		Unit string `json:"unit"`
		Call struct {
			USDPerCallMicros int64 `json:"usd_per_call_micros"`
		} `json:"call"`
		Request struct {
			Size string `json:"size"`
			N    int64  `json:"n"`
		} `json:"request"`
	}
	if err := json.Unmarshal(trail.PriceSnapshot, &snapshot); err != nil {
		t.Fatalf("snapshot not JSON: %v (%s)", err, trail.PriceSnapshot)
	}
	if snapshot.Unit != "second" || snapshot.Call.USDPerCallMicros != 100_000 ||
		snapshot.Request.Size != "720p" || snapshot.Request.N != 5 {
		t.Errorf("snapshot = %s, want second track with request {720p, 5}", trail.PriceSnapshot)
	}

	// 终态后再轮询:直接答账本事实,上游零打扰。
	_, queries, _ := vendor.counts()
	w = env.getVideoTask(t, full, submitted.TaskID)
	if got := decodeVideoTask(t, w.Body.Bytes()); got.Status != "succeeded" {
		t.Errorf("terminal re-poll = %+v, want stored succeeded", got)
	}
	if _, queries2, _ := vendor.counts(); queries2 != queries {
		t.Errorf("terminal re-poll hit the vendor (%d → %d), want none", queries, queries2)
	}
}

func TestRelayVideoCancelRefundsWhileRunning(t *testing.T) {
	env := newRelayEnv(t, nil)
	vendor := newFakeVideoVendor(t)
	vendor.script("RUNNING", "", "")
	env.seedVideoChannel(t, "wan", vendor.server.URL, "vid-m", "upstream-vid", 0,
		[]channel.Capability{channel.CapVideos})
	key, full := env.seedKey(t, 1_000_000)
	env.seedVideoPrice(t, "vid-m", nil)

	w := env.postVideo(t, full, videoBody)
	if w.Code != http.StatusOK {
		t.Fatalf("submit status = %d", w.Code)
	}
	id := decodeVideoTask(t, w.Body.Bytes()).TaskID
	if w = env.getVideoTask(t, full, id); decodeVideoTask(t, w.Body.Bytes()).Status != "running" {
		t.Fatalf("task should be running before cancel")
	}

	// 取消:状态 canceled,不带产物,上游收到取消调用。
	if w = env.cancelVideoTask(t, full, id); w.Code != http.StatusOK {
		t.Fatalf("cancel status = %d body %s, want 200", w.Code, w.Body.String())
	}
	canceled := decodeVideoTask(t, w.Body.Bytes())
	if canceled.Status != "canceled" || canceled.VideoURL != "" {
		t.Errorf("canceled = %+v, want canceled without a URL", canceled)
	}
	if _, _, cancels := vendor.counts(); cancels != 1 {
		t.Errorf("vendor cancels = %d, want 1", cancels)
	}

	// 取消不扣费:全额退回,流水 initial+estimate+refund。
	if balance := env.balanceOf(t, key.ID); balance != 1_000_000 {
		t.Errorf("balance = %d, want fully refunded 1_000_000", balance)
	}
	entries := env.quotaLog(t, key.ID)
	if len(entries) != 3 || entries[0].Reason != apikey.ReasonRefund || entries[0].DeltaMicros != 500_000 {
		t.Fatalf("ledger = %+v, want the 500000 reserve refunded", entries)
	}

	// 留痕:upstream_error 摘要列区分取消,零扣费;任务行可查。
	rows := env.usageRows(t)
	if len(rows) != 1 || rows[0].Status != usage.StatusUpstreamError || rows[0].ChargeMicros != 0 ||
		!strings.Contains(rows[0].UpstreamError, "canceled by client") {
		t.Errorf("trail = %+v, want an upstream_error row naming the cancel", rows)
	}
	if row := env.taskRow(t, id); row.Status != videotask.StatusCanceled || row.Error == "" {
		t.Errorf("task row = %+v, want canceled with a reason", row)
	}

	// 重复取消与轮询都是幂等的账本事实,不再打扰上游。
	_, queries, cancels := vendor.counts()
	if w = env.cancelVideoTask(t, full, id); decodeVideoTask(t, w.Body.Bytes()).Status != "canceled" {
		t.Errorf("re-cancel = %s, want canceled", w.Body.String())
	}
	if w = env.getVideoTask(t, full, id); decodeVideoTask(t, w.Body.Bytes()).Status != "canceled" {
		t.Errorf("poll after cancel = %s, want canceled", w.Body.String())
	}
	if _, queries2, cancels2 := vendor.counts(); queries2 != queries || cancels2 != cancels {
		t.Errorf("terminal no-ops hit the vendor (queries %d→%d, cancels %d→%d)",
			queries, queries2, cancels, cancels2)
	}
	if balance := env.balanceOf(t, key.ID); balance != 1_000_000 {
		t.Errorf("balance after re-cancel = %d, want unchanged", balance)
	}
	if rows := env.usageRows(t); len(rows) != 1 {
		t.Errorf("usage rows = %d, want still 1 (re-cancel books nothing)", len(rows))
	}
}

func TestRelayVideoCancelAfterSucceedKeepsCharge(t *testing.T) {
	env := newRelayEnv(t, nil)
	vendor := newFakeVideoVendor(t)
	vendor.script("SUCCEEDED", "https://cdn.example/done.mp4", "")
	env.seedVideoChannel(t, "wan", vendor.server.URL, "vid-m", "upstream-vid", 0,
		[]channel.Capability{channel.CapVideos})
	key, full := env.seedKey(t, 1_000_000)
	env.seedVideoPrice(t, "vid-m", nil)

	w := env.postVideo(t, full, videoBody)
	id := decodeVideoTask(t, w.Body.Bytes()).TaskID
	if w = env.getVideoTask(t, full, id); decodeVideoTask(t, w.Body.Bytes()).Status != "succeeded" {
		t.Fatalf("task should succeed before cancel attempt")
	}

	// 已成功的任务取消是历史事实上的 no-op:保持 succeeded、保持已计费。
	w = env.cancelVideoTask(t, full, id)
	got := decodeVideoTask(t, w.Body.Bytes())
	if got.Status != "succeeded" || got.VideoURL == "" {
		t.Errorf("cancel after success = %+v, want the standing succeeded task", got)
	}
	if balance := env.balanceOf(t, key.ID); balance != 500_000 {
		t.Errorf("balance = %d, want the success charge kept (500000)", balance)
	}
	if _, _, cancels := vendor.counts(); cancels != 0 {
		t.Errorf("vendor cancels = %d, want 0 for a terminal task", cancels)
	}
}

func TestRelayVideoVendorStatesMergeOnPoll(t *testing.T) {
	// 每种真实厂商状态一条独立剧情:提交后立刻轮询一次,断言对外五态与
	// 终态账务(成功扣费、失败/取消退款)。
	for _, tc := range []struct {
		name       string
		raw        string
		url        string
		wantStatus string
	}{
		{"kling submitted stays queued", "submitted", "", "queued"},
		{"runway throttled merges queued", "THROTTLED", "", "queued"},
		{"dashscope pending merges queued", "PENDING", "", "queued"},
		{"vidu processing merges running", "processing", "", "running"},
		{"kling succeed merges succeeded", "succeed", "https://cdn.example/k.mp4", "succeeded"},
		{"minimax success merges succeeded", "Success", "https://cdn.example/m.mp4", "succeeded"},
		{"explicit failed", "FAILED", "", "failed"},
		{"dashscope unknown merges failed", "UNKNOWN", "", "failed"},
		{"unrecognized state merges failed", "brand-new-vendor-state", "", "failed"},
		{"runway cancelled merges canceled", "CANCELLED", "", "canceled"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			env := newRelayEnv(t, nil)
			vendor := newFakeVideoVendor(t)
			vendor.script(tc.raw, tc.url, "vendor says no")
			env.seedVideoChannel(t, "wan", vendor.server.URL, "vid-m", "upstream-vid", 0,
				[]channel.Capability{channel.CapVideos})
			key, full := env.seedKey(t, 1_000_000)
			env.seedVideoPrice(t, "vid-m", nil) // $0.10/秒 × 5s = 500000

			w := env.postVideo(t, full, videoBody)
			if w.Code != http.StatusOK {
				t.Fatalf("submit status = %d body %s", w.Code, w.Body.String())
			}
			id := decodeVideoTask(t, w.Body.Bytes()).TaskID

			w = env.getVideoTask(t, full, id)
			if w.Code != http.StatusOK {
				t.Fatalf("poll status = %d body %s, want 200", w.Code, w.Body.String())
			}
			got := decodeVideoTask(t, w.Body.Bytes())
			if got.Status != tc.wantStatus {
				t.Fatalf("raw %s merged to %q, want %q", tc.raw, got.Status, tc.wantStatus)
			}

			wantBalance := int64(500_000) // 预扣持有中,或实扣即预扣
			switch tc.wantStatus {
			case "succeeded":
				if got.VideoURL != tc.url {
					t.Errorf("video_url = %q, want %q", got.VideoURL, tc.url)
				}
			case "failed":
				if got.Error == nil || got.Error.Message == "" {
					t.Errorf("failed task = %+v, want an error reason", got)
				}
				wantBalance = 1_000_000 // 全额退回
			case "canceled":
				wantBalance = 1_000_000 // 全额退回
			}
			if balance := env.balanceOf(t, key.ID); balance != wantBalance {
				t.Errorf("balance = %d, want %d", balance, wantBalance)
			}

			// 任务行与用量日志可查(验收第三条):终态各留一条用量行,
			// 活动任务零留痕;任务行带原始状态与原因。
			rows := env.usageRows(t)
			row := env.taskRow(t, id)
			switch tc.wantStatus {
			case "queued", "running":
				if len(rows) != 0 {
					t.Errorf("usage rows = %d, want 0 while the task is active", len(rows))
				}
				if row.Status != videotask.Status(tc.wantStatus) || row.UpstreamStatus != tc.raw {
					t.Errorf("task row = %s/%s, want %s with raw %q", row.Status, row.UpstreamStatus, tc.wantStatus, tc.raw)
				}
			case "succeeded":
				if len(rows) != 1 || rows[0].Status != usage.StatusSuccess || rows[0].ChargeMicros != 500_000 {
					t.Errorf("trail = %+v, want one success row charged 500000", rows)
				}
				if row.Status != videotask.StatusSucceeded || row.VideoURL != tc.url || row.ChargeMicros != 500_000 {
					t.Errorf("task row = %+v, want succeeded with url and charge", row)
				}
			case "failed":
				if len(rows) != 1 || rows[0].Status != usage.StatusUpstreamError || rows[0].ChargeMicros != 0 ||
					rows[0].UpstreamError == "" {
					t.Errorf("trail = %+v, want one zero-charge failure row with a summary", rows)
				}
				if row.Status != videotask.StatusFailed || row.Error == "" {
					t.Errorf("task row = %+v, want failed with a reason", row)
				}
			case "canceled":
				if len(rows) != 1 || rows[0].Status != usage.StatusUpstreamError || rows[0].ChargeMicros != 0 {
					t.Errorf("trail = %+v, want one zero-charge canceled row", rows)
				}
				if row.Status != videotask.StatusCanceled || row.Error == "" {
					t.Errorf("task row = %+v, want canceled with a reason", row)
				}
			}
		})
	}
}

func TestRelayVideoConcurrentFinalizeBillsOnce(t *testing.T) {
	env := newRelayEnv(t, nil)
	vendor := newFakeVideoVendor(t)
	vendor.script("SUCCEEDED", "https://cdn.example/race.mp4", "")
	env.seedVideoChannel(t, "wan", vendor.server.URL, "vid-m", "upstream-vid", 0,
		[]channel.Capability{channel.CapVideos})
	key, full := env.seedKey(t, 1_000_000)
	env.seedVideoPrice(t, "vid-m", nil) // 5s × $0.10 = 500000

	w := env.postVideo(t, full, videoBody)
	id := decodeVideoTask(t, w.Body.Bytes()).TaskID

	// 8 笔并发收尾(轮询与取消混打):终态守卫决定唯一赢家,账只能动一次
	// ——要么成功实扣 500000,要么取消全额退回,绝不双落。
	const concurrency = 8
	var wg sync.WaitGroup
	for i := range concurrency {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			if i%2 == 0 {
				env.getVideoTask(t, full, id)
			} else {
				env.cancelVideoTask(t, full, id)
			}
		}(i)
	}
	wg.Wait()

	row := env.taskRow(t, id)
	if !videotask.Terminal(row.Status) {
		t.Fatalf("task = %s, want a terminal state after the race", row.Status)
	}
	rows := env.usageRows(t)
	if len(rows) != 1 {
		t.Fatalf("usage rows = %d, want exactly 1 regardless of who wins", len(rows))
	}
	wantBalance := int64(1_000_000)
	if row.Status == videotask.StatusSucceeded {
		if rows[0].Status != usage.StatusSuccess || rows[0].ChargeMicros != 500_000 {
			t.Errorf("trail = %+v, want a success row charged 500000", rows[0])
		}
		wantBalance = 500_000
	} else if rows[0].ChargeMicros != 0 || rows[0].Status != usage.StatusUpstreamError {
		t.Errorf("trail = %+v, want a zero-charge upstream_error row", rows[0])
	}
	if balance := env.balanceOf(t, key.ID); balance != wantBalance {
		t.Errorf("balance = %d, want %d (books closed exactly once)", balance, wantBalance)
	}
}

func TestRelayVideoSubmitFailureRefundsAndTrails(t *testing.T) {
	env := newRelayEnv(t, nil)
	vendor := newFakeVideoVendor(t)
	vendor.submitStatus = http.StatusServiceUnavailable
	vendor.submitBody = `{"error":{"message":"video queue overloaded","type":"server_error","code":null}}`
	env.seedVideoChannel(t, "wan", vendor.server.URL, "vid-m", "upstream-vid", 0,
		[]channel.Capability{channel.CapVideos})
	key, full := env.seedKey(t, 1_000_000)
	env.seedVideoPrice(t, "vid-m", nil)

	w := env.postVideo(t, full, videoBody)
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d body %s, want upstream 503 passthrough", w.Code, w.Body.String())
	}
	eb := decodeOpenAIError(t, w.Body.Bytes())
	if eb.Error.Code != "upstream_error" || !strings.Contains(eb.Error.Message, "video queue overloaded") {
		t.Errorf("error object = %+v, want upstream_error carrying the vendor message", eb.Error)
	}

	// 预扣全额退回;失败照常留痕;任务行根本不存在(没提交成功过)。
	if balance := env.balanceOf(t, key.ID); balance != 1_000_000 {
		t.Errorf("balance = %d, want fully refunded", balance)
	}
	entries := env.quotaLog(t, key.ID)
	if len(entries) != 3 || entries[0].Reason != apikey.ReasonRefund {
		t.Fatalf("ledger = %+v, want refund reversing the estimate", entries)
	}
	rows := env.usageRows(t)
	if len(rows) != 1 || rows[0].Status != usage.StatusUpstreamError || rows[0].ChargeMicros != 0 ||
		!strings.Contains(rows[0].UpstreamError, "video queue overloaded") {
		t.Errorf("failure trail = %+v, want upstream_error with vendor summary", rows)
	}
	if rows[0].Unit != "second" {
		t.Errorf("failure row unit = %q, want second", rows[0].Unit)
	}
	if n := env.videoTaskCount(t); n != 0 {
		t.Errorf("video task rows = %d, want 0 (the submit never succeeded)", n)
	}
}

func TestRelayVideoSubmitFailoverPinsBackupAndKeepsHistory(t *testing.T) {
	env := newRelayEnv(t, nil)
	primary := newFakeVideoVendor(t)
	primary.submitStatus = http.StatusInternalServerError
	backup := newFakeVideoVendor(t)
	env.seedVideoChannel(t, "primary", primary.server.URL, "vid-m", "upstream-vid", 10,
		[]channel.Capability{channel.CapVideos})
	env.seedVideoChannel(t, "backup", backup.server.URL, "vid-m", "upstream-vid", 1,
		[]channel.Capability{channel.CapVideos})
	key, full := env.seedKey(t, 1_000_000)
	env.seedVideoPrice(t, "vid-m", nil)

	w := env.postVideo(t, full, videoBody)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d body %s, want 200 via the backup", w.Code, w.Body.String())
	}
	submits, _, _ := backup.counts()
	if primary.submits != 1 || submits != 1 {
		t.Errorf("submits = primary %d backup %d, want one each", primary.submits, submits)
	}
	id := decodeVideoTask(t, w.Body.Bytes()).TaskID

	// 渠道钉在受理提交的那一家;预扣只发生一次。
	if row := env.taskRow(t, id); row.ChannelName != "backup" {
		t.Errorf("task pinned to %q, want backup", row.ChannelName)
	}
	if balance := env.balanceOf(t, key.ID); balance != 500_000 {
		t.Errorf("balance = %d, want exactly one 500000 hold", balance)
	}

	// 备用渠道上成功:换道史进成功行的摘要列(带病完成)。
	backup.script("succeed", "https://cdn.example/b.mp4", "")
	if w = env.getVideoTask(t, full, id); decodeVideoTask(t, w.Body.Bytes()).Status != "succeeded" {
		t.Fatalf("task should succeed on the backup")
	}
	rows := env.usageRows(t)
	if len(rows) != 1 || rows[0].ChannelName != "backup" || rows[0].ChargeMicros != 500_000 {
		t.Fatalf("trail = %+v, want one success row on the backup", rows)
	}
	if !strings.Contains(rows[0].UpstreamError, "'primary'") {
		t.Errorf("upstream_error = %q, want the submit failover trail naming the primary", rows[0].UpstreamError)
	}
}

func TestRelayVideoQueryFailureLeavesTaskActive(t *testing.T) {
	env := newRelayEnv(t, nil)
	vendor := newFakeVideoVendor(t)
	vendor.script("RUNNING", "", "")
	env.seedVideoChannel(t, "wan", vendor.server.URL, "vid-m", "upstream-vid", 0,
		[]channel.Capability{channel.CapVideos})
	key, full := env.seedKey(t, 1_000_000)
	env.seedVideoPrice(t, "vid-m", nil)

	w := env.postVideo(t, full, videoBody)
	id := decodeVideoTask(t, w.Body.Bytes()).TaskID

	// 一次轮询失败(上游 500):客户端透传上游状态,任务原地不动、账不动。
	vendor.queryStatus = http.StatusInternalServerError
	vendor.queryBody = `{"error":{"message":"task store busy"}}`
	if w = env.getVideoTask(t, full, id); w.Code != http.StatusInternalServerError {
		t.Fatalf("poll status = %d body %s, want upstream passthrough", w.Code, w.Body.String())
	}
	eb := decodeOpenAIError(t, w.Body.Bytes())
	if eb.Error.Code != "upstream_error" {
		t.Errorf("error code = %q, want upstream_error", eb.Error.Code)
	}
	if row := env.taskRow(t, id); row.Status != videotask.StatusQueued {
		t.Errorf("task row = %s, want untouched queued", row.Status)
	}
	if balance := env.balanceOf(t, key.ID); balance != 500_000 {
		t.Errorf("balance = %d, want the hold kept", balance)
	}
	if rows := env.usageRows(t); len(rows) != 0 {
		t.Errorf("usage rows = %d, want none for a failed poll", len(rows))
	}

	// 恢复后照常推进到成功。
	vendor.queryStatus = 0
	vendor.queryBody = ""
	vendor.script("SUCCEEDED", "https://cdn.example/late.mp4", "")
	if w = env.getVideoTask(t, full, id); decodeVideoTask(t, w.Body.Bytes()).Status != "succeeded" {
		t.Fatalf("task should succeed after the transient query failure")
	}
	if balance := env.balanceOf(t, key.ID); balance != 500_000 {
		t.Errorf("balance = %d, want charged 500000", balance)
	}
}

func TestRelayVideoTaskOwnership(t *testing.T) {
	env := newRelayEnv(t, nil)
	vendor := newFakeVideoVendor(t)
	vendor.script("RUNNING", "", "")
	env.seedVideoChannel(t, "wan", vendor.server.URL, "vid-m", "upstream-vid", 0,
		[]channel.Capability{channel.CapVideos})
	_, fullA := env.seedKey(t, 1_000_000)
	_, fullB := env.seedKey(t, 1_000_000)
	env.seedVideoPrice(t, "vid-m", nil)

	w := env.postVideo(t, fullA, videoBody)
	id := decodeVideoTask(t, w.Body.Bytes()).TaskID

	// 别人的 key 查询/取消与未知 id 一律 404 task_not_found(不泄露存在性)。
	for _, tc := range []struct {
		name string
		do   func(string) *httptest.ResponseRecorder
	}{
		{"poll", func(k string) *httptest.ResponseRecorder { return env.getVideoTask(t, k, id) }},
		{"cancel", func(k string) *httptest.ResponseRecorder { return env.cancelVideoTask(t, k, id) }},
	} {
		if w := tc.do(fullB); w.Code != http.StatusNotFound {
			t.Fatalf("%s with a foreign key = %d, want 404", tc.name, w.Code)
		} else if eb := decodeOpenAIError(t, w.Body.Bytes()); eb.Error.Code != "task_not_found" {
			t.Errorf("%s error code = %q, want task_not_found", tc.name, eb.Error.Code)
		}
	}
	if w := env.getVideoTask(t, fullA, "vt_does_not_exist"); w.Code != http.StatusNotFound {
		t.Errorf("unknown id = %d, want 404", w.Code)
	}
	// 属主照常可用。
	if w := env.getVideoTask(t, fullA, id); w.Code != http.StatusOK {
		t.Errorf("owner poll = %d, want 200", w.Code)
	}
}

func TestRelayVideoNeverRoutedToChatOrImageOnlyChannels(t *testing.T) {
	env := newRelayEnv(t, nil)
	upstream := newFakeVideoVendor(t)
	upstream.script("SUCCEEDED", "https://cdn.example/x.mp4", "")
	// chat+images 渠道优先级更高:若能力过滤失效,视频会先打它。
	env.seedVideoChannel(t, "chatimg", upstream.server.URL, "vid-m", "up-vid", 10,
		[]channel.Capability{channel.CapChat, channel.CapImages})
	env.seedVideoChannel(t, "vidcap", upstream.server.URL, "vid-m", "up-vid", 1,
		[]channel.Capability{channel.CapVideos})
	// 仅 chat 渠道挂的模型:视频必须 404;仅 videos 渠道挂的模型:聊天必须 404。
	env.seedVideoChannel(t, "chatonly2", upstream.server.URL, "chatonly-m", "up-m", 0, nil)
	env.seedVideoChannel(t, "vidonly", upstream.server.URL, "vidonly-m", "up-m", 0,
		[]channel.Capability{channel.CapVideos})
	key, full := env.seedKey(t, 1_000_000)
	env.seedVideoPrice(t, "vid-m", nil)

	w := env.postVideo(t, full, videoBody)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d body %s, want 200 via the video-capable channel", w.Code, w.Body.String())
	}
	id := decodeVideoTask(t, w.Body.Bytes()).TaskID
	if row := env.taskRow(t, id); row.ChannelName != "vidcap" {
		t.Fatalf("task pinned to %q, want vidcap", row.ChannelName)
	}

	w = env.postVideo(t, full, `{"model":"chatonly-m","prompt":"x"}`)
	if w.Code != http.StatusNotFound {
		t.Fatalf("chat-only model video status = %d body %s, want 404", w.Code, w.Body.String())
	}
	if eb := decodeOpenAIError(t, w.Body.Bytes()); eb.Error.Code != "model_not_found" ||
		!strings.Contains(eb.Error.Message, "video-capable") {
		t.Errorf("error object = %+v, want model_not_found naming the capability gap", eb.Error)
	}
	w = env.post(t, full, `{"model":"vidonly-m","messages":[{"role":"user","content":"hi"}]}`)
	if w.Code != http.StatusNotFound {
		t.Fatalf("videos-only model chat status = %d, want 404", w.Code)
	}

	// 全程账目干净:除成功那笔预扣转实扣外无额外扣减。
	if balance := env.balanceOf(t, key.ID); balance != 500_000 {
		t.Errorf("balance = %d, want only the one video charge", balance)
	}
}

func TestRelayVideoValidation(t *testing.T) {
	env := newRelayEnv(t, nil)
	upstream := newFakeVideoVendor(t)
	env.seedVideoChannel(t, "vidcap", upstream.server.URL, "vid-m", "upstream-vid", 0,
		[]channel.Capability{channel.CapVideos})
	env.seedVideoChannel(t, "vid2", upstream.server.URL, "callpriced-m", "up", 0,
		[]channel.Capability{channel.CapVideos})
	env.seedVideoChannel(t, "vid3", upstream.server.URL, "unpriced-m", "up", 0,
		[]channel.Capability{channel.CapVideos})
	key, full := env.seedKey(t, 1_000_000)
	env.seedVideoPrice(t, "vid-m", nil)
	// 按次(张)轨价格的视频模型:必须拒绝。
	if _, err := env.stores.prices.Upsert(context.Background(), pricing.Price{
		PublicModel: "callpriced-m", Unit: pricing.UnitCall,
		Call: &pricing.CallPrice{USDPerCallMicros: 40_000},
	}); err != nil {
		t.Fatalf("seed call price: %v", err)
	}

	longSize := strings.Repeat("w", pricing.MaxSizeRunes+1)
	for _, tc := range []struct {
		name       string
		body       string
		wantStatus int
		wantCode   string
	}{
		{"missing model", `{"prompt":"x"}`, 400, "missing_model"},
		{"missing prompt", `{"model":"vid-m"}`, 400, "invalid_request"},
		{"blank prompt", `{"model":"vid-m","prompt":"  "}`, 400, "invalid_request"},
		{"seconds below 1", `{"model":"vid-m","prompt":"x","seconds":0}`, 400, "invalid_request"},
		{"seconds over 100", fmt.Sprintf(`{"model":"vid-m","prompt":"x","seconds":%d}`, pricing.MaxCallItems+1), 400, "invalid_request"},
		{"seconds not an integer", `{"model":"vid-m","prompt":"x","seconds":2.5}`, 400, "invalid_request"},
		{"size too long", fmt.Sprintf(`{"model":"vid-m","prompt":"x","size":%q}`, longSize), 400, "invalid_request"},
		{"not json", `{invalid`, 400, "invalid_request"},
		{"call-track model", `{"model":"callpriced-m","prompt":"x"}`, 400, "model_not_priced"},
		{"unpriced model", `{"model":"unpriced-m","prompt":"x"}`, 400, "model_not_priced"},
		{"unknown model", `{"model":"no-such-m","prompt":"x"}`, 404, "model_not_found"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := env.postVideo(t, full, tc.body)
			if w.Code != tc.wantStatus {
				t.Fatalf("status = %d body %s, want %d", w.Code, w.Body.String(), tc.wantStatus)
			}
			eb := decodeOpenAIError(t, w.Body.Bytes())
			if eb.Error.Code != tc.wantCode || eb.Error.Type != "invalid_request_error" {
				t.Errorf("error object = %+v, want code %s", eb.Error, tc.wantCode)
			}
		})
	}

	// 全部拒绝发生在预扣之前:余额原封,上游零调用,无任务行无用量行。
	if balance := env.balanceOf(t, key.ID); balance != 1_000_000 {
		t.Errorf("balance = %d, want untouched", balance)
	}
	if submits, _, _ := upstream.counts(); submits != 0 {
		t.Errorf("upstream submits = %d, want 0", submits)
	}
	if n := env.videoTaskCount(t); n != 0 {
		t.Errorf("video task rows = %d, want 0", n)
	}
	if rows := env.usageRows(t); len(rows) != 0 {
		t.Errorf("usage rows = %d, want none for pre-billing rejections", len(rows))
	}
}

func TestRelayVideoFreeModelSkipsBillingButStillTracks(t *testing.T) {
	env := newRelayEnv(t, nil)
	vendor := newFakeVideoVendor(t)
	vendor.script("SUCCEEDED", "https://cdn.example/free.mp4", "")
	env.seedVideoChannel(t, "wan", vendor.server.URL, "vid-m", "upstream-vid", 0,
		[]channel.Capability{channel.CapVideos})
	key, full := env.seedKey(t, 1_000_000)
	// 免费模型:单价 0 → 预扣为 0,跳过账务但任务照常全流程。
	if _, err := env.stores.prices.Upsert(context.Background(), pricing.Price{
		PublicModel: "vid-m", Unit: pricing.UnitSecond,
		Call: &pricing.CallPrice{USDPerCallMicros: 0},
	}); err != nil {
		t.Fatalf("seed free price: %v", err)
	}

	w := env.postVideo(t, full, videoBody)
	if w.Code != http.StatusOK {
		t.Fatalf("submit status = %d", w.Code)
	}
	id := decodeVideoTask(t, w.Body.Bytes()).TaskID
	if w = env.getVideoTask(t, full, id); decodeVideoTask(t, w.Body.Bytes()).Status != "succeeded" {
		t.Fatalf("free task should succeed")
	}

	if balance := env.balanceOf(t, key.ID); balance != 1_000_000 {
		t.Errorf("balance = %d, want untouched 1_000_000", balance)
	}
	if entries := env.quotaLog(t, key.ID); len(entries) != 1 {
		t.Errorf("ledger = %d entries, want initial only", len(entries))
	}
	rows := env.usageRows(t)
	if len(rows) != 1 || rows[0].Status != usage.StatusSuccess || rows[0].ChargeMicros != 0 {
		t.Errorf("trail = %+v, want a zero-charge success row", rows)
	}
}
