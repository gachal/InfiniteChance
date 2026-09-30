package relay_test

// 25 号票(火山方舟 Seedance 渠道 + 视频 token 轨)的端到端测试:fake Ark
// 上游用官方任务面形状(POST/GET/DELETE {base}/contents/generations/
// tasks,content 分节 + 顶层 resolution/ratio/duration,usage.completion_
// tokens)验证 adaptor 翻译、adaptorFor 分发与 token 轨账务时序(预扣估
// 算 → 实报多退少补 / 无实报回退 / 失败退款),以及 /v1/videos 新契约
// 字段(last_image/references/ratio)的校验与透传。共享 relay_test.go 的
// MySQL 环境。

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

// fakeArkVendor 是火山方舟任务面替身:提交回 {"id"},查询回 Ark 形任务体
// (status / content.video_url / error / usage),取消是 DELETE。测试在
// 轮询间隙改剧本;最近一次提交体被留存供翻译断言。
type fakeArkVendor struct {
	server *httptest.Server
	mu     sync.Mutex

	status          string // 查询端点当前回的 Ark status 原文
	videoURL        string // succeeded 时 content.video_url
	errMsg          string // failed 时 error.message
	withUsage       bool   // 查询响应是否带 usage
	completionToken int64  // usage.completion_tokens
	submitStatus    int    // 提交端点回答码(0 = 200)

	submits    int
	cancels    int
	queries    int
	lastBody   []byte
	lastAuth   string
	lastPath   string
	lastMethod string
}

const arkTaskID = "cgt-20260930110000-abcde"

func newFakeArkVendor(t *testing.T) *fakeArkVendor {
	t.Helper()
	v := &fakeArkVendor{}
	v.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		v.mu.Lock()
		defer v.mu.Unlock()
		raw, _ := io.ReadAll(r.Body)
		v.lastAuth = r.Header.Get("Authorization")
		v.lastMethod = r.Method
		v.lastPath = r.URL.Path

		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/contents/generations/tasks":
			v.submits++
			v.lastBody = raw
			w.Header().Set("Content-Type", "application/json")
			if v.submitStatus != 0 && v.submitStatus != http.StatusOK {
				w.WriteHeader(v.submitStatus)
				w.Write([]byte(`{"error":{"code":"InvalidParameter","message":"bad"}}`))
				return
			}
			fmt.Fprintf(w, `{"id":%q}`, arkTaskID)
		case r.Method == http.MethodGet && r.URL.Path == "/contents/generations/tasks/"+arkTaskID:
			v.queries++
			w.Header().Set("Content-Type", "application/json")
			answer := map[string]any{"id": arkTaskID, "status": v.status}
			if v.videoURL != "" {
				answer["content"] = map[string]any{"video_url": v.videoURL}
			}
			if v.errMsg != "" {
				answer["error"] = map[string]any{"code": "InternalError", "message": v.errMsg}
			}
			if v.withUsage {
				answer["usage"] = map[string]any{"completion_tokens": v.completionToken, "total_tokens": v.completionToken}
			}
			json.NewEncoder(w).Encode(answer)
		case r.Method == http.MethodDelete && r.URL.Path == "/contents/generations/tasks/"+arkTaskID:
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

func (v *fakeArkVendor) script(status, videoURL, errMsg string, withUsage bool, completionTokens int64) {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.status, v.videoURL, v.errMsg = status, videoURL, errMsg
	v.withUsage, v.completionToken = withUsage, completionTokens
}

func (v *fakeArkVendor) counts() (submits, queries, cancels int) {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.submits, v.queries, v.cancels
}

func (v *fakeArkVendor) lastSubmit() (method, path, auth string, body []byte) {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.lastMethod, v.lastPath, v.lastAuth, v.lastBody
}

// seedArkChannel inserts one enabled volcengine-ark channel (videos only).
func (e *relayEnv) seedArkChannel(t *testing.T, name, baseURL, publicModel, upstreamModel string) channel.Channel {
	t.Helper()
	ch, err := e.stores.channels.Create(context.Background(), channel.Channel{
		Name: name, Type: channel.TypeVolcengineArk, BaseURL: baseURL,
		APIKey: "ark-secret-key", ModelMap: map[string]string{publicModel: upstreamModel},
		Capabilities: []channel.Capability{channel.CapVideos}, Priority: 0, Enabled: true,
	})
	if err != nil {
		t.Fatalf("seed ark channel: %v", err)
	}
	return ch
}

// seedVideoTokenPrice writes the token-track test price: $2/M output tokens
// at cost, 720p converts at 24000 tokens/s, the default scalar 12000.
func (e *relayEnv) seedVideoTokenPrice(t *testing.T, publicModel string) {
	t.Helper()
	_, err := e.stores.prices.Upsert(context.Background(), pricing.Price{
		PublicModel: publicModel, Unit: pricing.UnitToken,
		Token: &pricing.TokenPrice{
			OutputMicrosPerMTokens: 2_000_000, // $2/M tokens
			RatioMicros:            1_000_000, // ×1.0
			SizeTokensPerSecond:    map[string]float64{"720p": 24_000},
			DefaultTokensPerSecond: 12_000,
		},
	})
	if err != nil {
		t.Fatalf("seed token price: %v", err)
	}
}

const arkVideoBody = `{"model":"sd-m","prompt":"一只猫在月光下奔跑","seconds":5,"size":"720p","ratio":"16:9",` +
	`"image":"https://cdn.example/first.png","last_image":"https://cdn.example/last.png",` +
	`"references":[{"url":"https://cdn.example/ref.png","kind":"image"},` +
	`{"url":"https://cdn.example/clip.mp4","kind":"video"},` +
	`{"url":"https://cdn.example/voice.mp3","kind":"audio"}]}`

// TestArkAdaptorTranslatesContract pins the gateway→Ark submit translation
// end to end: content 分节(text/首帧/尾帧/参考,role 与 type 各就各位)、
// seconds→duration、size→resolution、ratio 直传、model 已映射、Bearer
// 头、任务面路径;查询响应归一成网关任务面(状态、产物、失败原因、
// usage);取消走 DELETE。
func TestArkAdaptorTranslatesContract(t *testing.T) {
	env := newRelayEnv(t, nil)
	vendor := newFakeArkVendor(t)
	env.seedArkChannel(t, "ark-main", vendor.server.URL, "sd-m", "doubao-seedance-2-5-250929")
	key, full := env.seedKey(t, 5_000_000)
	env.seedVideoTokenPrice(t, "sd-m")

	w := env.postVideo(t, full, arkVideoBody)
	if w.Code != http.StatusOK {
		t.Fatalf("submit status = %d body %s, want 200", w.Code, w.Body.String())
	}
	task := decodeVideoTask(t, w.Body.Bytes())
	if task.Status != "queued" || task.TaskID == "" {
		t.Fatalf("submitted = %+v, want queued with a gateway id", task)
	}

	method, path, auth, body := vendor.lastSubmit()
	if method != http.MethodPost || path != "/contents/generations/tasks" {
		t.Fatalf("upstream saw %s %s, want POST /contents/generations/tasks", method, path)
	}
	if auth != "Bearer ark-secret-key" {
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
	if sent.Model != "doubao-seedance-2-5-250929" {
		t.Errorf("model = %q, want the mapped upstream id", sent.Model)
	}
	if sent.Duration == nil || *sent.Duration != 5 {
		t.Errorf("duration = %v, want seconds 5 carried as duration", sent.Duration)
	}
	if sent.Resolution != "720p" || sent.Ratio != "16:9" {
		t.Errorf("resolution/ratio = %q/%q, want 720p/16:9 verbatim", sent.Resolution, sent.Ratio)
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

	// 查询:Ark 状态词汇归并走既有五态机;取消态退款。
	vendor.script("queued", "", "", false, 0)
	if w = env.getVideoTask(t, full, task.TaskID); decodeVideoTask(t, w.Body.Bytes()).Status != "queued" {
		t.Errorf("Ark queued merged wrong")
	}
	vendor.script("cancelled", "", "canceled by vendor", false, 0)
	if w = env.getVideoTask(t, full, task.TaskID); decodeVideoTask(t, w.Body.Bytes()).Status != "canceled" {
		t.Errorf("Ark cancelled should merge canceled (refunded)")
	}
	if balance := env.balanceOf(t, key.ID); balance != 5_000_000 {
		t.Errorf("balance after cancel = %d, want the full refund (pre-deduction returned)", balance)
	}

	// 取消端点:本地取消触发厂商 DELETE(尽力而为)。
	task2 := decodeVideoTask(t, env.postVideo(t, full, arkVideoBody).Body.Bytes())
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

// TestRelayVideoTokenTrackBillsByReportedUsage pins the token track's
// happy path:预扣按估算表折算(720p 24000/s × 5s = 120000 tokens → $0.24),
// 成功终态按厂商实报 completion_tokens 多退少补(240000 tokens → $0.48,
// 补扣一笔 settle),用量行 unit=token、completion_tokens 记视频 token 数,
// 快照带请求事实与实报。
func TestRelayVideoTokenTrackBillsByReportedUsage(t *testing.T) {
	env := newRelayEnv(t, nil)
	vendor := newFakeArkVendor(t)
	env.seedArkChannel(t, "ark-main", vendor.server.URL, "sd-m", "doubao-seedance-2-5-250929")
	key, full := env.seedKey(t, 5_000_000)
	env.seedVideoTokenPrice(t, "sd-m")

	w := env.postVideo(t, full, arkVideoBody)
	if w.Code != http.StatusOK {
		t.Fatalf("submit status = %d body %s", w.Code, w.Body.String())
	}
	task := decodeVideoTask(t, w.Body.Bytes())

	// 预扣:ceil($2/M × 120000 tokens) = $0.24 = 240000 micros。
	if balance := env.balanceOf(t, key.ID); balance != 5_000_000-240_000 {
		t.Fatalf("balance after submit = %d, want 4_760_000 held by the estimate", balance)
	}

	// 成功:实报 240000 tokens → 实结 $0.48,补扣一笔 settle 流水。
	vendor.script("succeeded", "https://ark.example/out.mp4", "", true, 240_000)
	if w = env.getVideoTask(t, full, task.TaskID); w.Code != http.StatusOK {
		t.Fatalf("poll status = %d body %s", w.Code, w.Body.String())
	}
	done := decodeVideoTask(t, w.Body.Bytes())
	if done.Status != "succeeded" || done.VideoURL != "https://ark.example/out.mp4" {
		t.Fatalf("final = %+v, want succeeded with the video URL", done)
	}
	if balance := env.balanceOf(t, key.ID); balance != 5_000_000-480_000 {
		t.Errorf("balance after settle = %d, want 4_520_000 (billed the reported tokens)", balance)
	}
	entries := env.quotaLog(t, key.ID) // newest first
	if len(entries) != 3 || entries[0].Reason != apikey.ReasonSettle || entries[0].DeltaMicros != -240_000 ||
		entries[1].Reason != apikey.ReasonEstimate || entries[2].Reason != "initial" {
		t.Fatalf("ledger = %+v, want initial + estimate + one settle of −240000", entries)
	}

	// 任务行:实结额定格。
	row := env.taskRow(t, task.TaskID)
	if row.Status != videotask.StatusSucceeded || row.ChargeMicros != 480_000 {
		t.Errorf("task row = %+v, want succeeded charged 480000", row)
	}

	// 用量行:token 轨、completion_tokens 记视频 token 数、快照带请求事实
	// 与实报(settle 段)。
	rows := env.usageRows(t)
	if len(rows) != 1 {
		t.Fatalf("usage rows = %d, want 1", len(rows))
	}
	trail := rows[0]
	if trail.Unit != "token" || trail.Status != usage.StatusSuccess || trail.ChargeMicros != 480_000 ||
		trail.CompletionTokens != 240_000 || trail.PromptTokens != 0 {
		t.Errorf("trail = %+v, want a token-track success row with the reported tokens", trail)
	}
	var snap struct {
		Unit    string `json:"unit"`
		Request struct {
			Size      string `json:"size"`
			Seconds   int64  `json:"seconds"`
			EstTokens int64  `json:"est_tokens"`
		} `json:"request"`
		Settle struct {
			CompletionTokens int64  `json:"completion_tokens"`
			Note             string `json:"note"`
		} `json:"settle"`
	}
	if err := json.Unmarshal(trail.PriceSnapshot, &snap); err != nil {
		t.Fatalf("snapshot not JSON: %v (%s)", err, trail.PriceSnapshot)
	}
	if snap.Unit != "token" || snap.Request.Size != "720p" || snap.Request.Seconds != 5 || snap.Request.EstTokens != 120_000 {
		t.Errorf("snapshot request = %+v, want {720p, 5s, 120000 est}", snap.Request)
	}
	if snap.Settle.CompletionTokens != 240_000 || snap.Settle.Note != "" {
		t.Errorf("snapshot settle = %+v, want the reported 240000 without a note", snap.Settle)
	}
}

// TestRelayVideoTokenTrackUsageMissingFallsBackToEstimate pins the
// no-usage fallback:厂商跑完但没报 usage —— 按预扣估算定格实结,不退款
// 也不虚记,用量摘要留痕 usage missing, billed estimate。
func TestRelayVideoTokenTrackUsageMissingFallsBackToEstimate(t *testing.T) {
	env := newRelayEnv(t, nil)
	vendor := newFakeArkVendor(t)
	env.seedArkChannel(t, "ark-main", vendor.server.URL, "sd-m", "doubao-seedance-2-5-250929")
	key, full := env.seedKey(t, 5_000_000)
	env.seedVideoTokenPrice(t, "sd-m")

	task := decodeVideoTask(t, env.postVideo(t, full, arkVideoBody).Body.Bytes())
	vendor.script("succeeded", "https://ark.example/out.mp4", "", false, 0)
	if w := env.getVideoTask(t, full, task.TaskID); w.Code != http.StatusOK {
		t.Fatalf("poll status = %d body %s", w.Code, w.Body.String())
	}

	// 定格预扣:没有 settle 流水。
	if balance := env.balanceOf(t, key.ID); balance != 5_000_000-240_000 {
		t.Errorf("balance = %d, want the estimate kept as the final charge", balance)
	}
	entries := env.quotaLog(t, key.ID)
	if len(entries) != 2 {
		t.Fatalf("ledger = %+v, want initial + estimate only", entries)
	}
	rows := env.usageRows(t)
	if len(rows) != 1 || rows[0].ChargeMicros != 240_000 || rows[0].Unit != "token" ||
		rows[0].CompletionTokens != 0 {
		t.Fatalf("trail = %+v, want the estimate charge with zero tokens", rows)
	}
	if !strings.Contains(rows[0].UpstreamError, "usage missing, billed estimate") {
		t.Errorf("trail note = %q, want the usage-missing breadcrumb", rows[0].UpstreamError)
	}
}

// TestRelayVideoTokenTrackFailureRefunds:失败退款语义对 token 轨不变。
func TestRelayVideoTokenTrackFailureRefunds(t *testing.T) {
	env := newRelayEnv(t, nil)
	vendor := newFakeArkVendor(t)
	env.seedArkChannel(t, "ark-main", vendor.server.URL, "sd-m", "doubao-seedance-2-5-250929")
	key, full := env.seedKey(t, 5_000_000)
	env.seedVideoTokenPrice(t, "sd-m")

	task := decodeVideoTask(t, env.postVideo(t, full, arkVideoBody).Body.Bytes())
	vendor.script("failed", "", "content policy rejected", true, 240_000)
	w := env.getVideoTask(t, full, task.TaskID)
	if w.Code != http.StatusOK {
		t.Fatalf("poll status = %d body %s", w.Code, w.Body.String())
	}
	got := decodeVideoTask(t, w.Body.Bytes())
	if got.Status != "failed" || got.Error == nil || !strings.Contains(got.Error.Message, "content policy rejected") {
		t.Fatalf("final = %+v, want failed with the vendor reason", got)
	}
	if balance := env.balanceOf(t, key.ID); balance != 5_000_000 {
		t.Errorf("balance after failure = %d, want the whole reserve refunded", balance)
	}
	rows := env.usageRows(t)
	if len(rows) != 1 || rows[0].Status != usage.StatusUpstreamError || rows[0].ChargeMicros != 0 ||
		rows[0].CompletionTokens != 0 {
		t.Errorf("trail = %+v, want a zero-charge failure row without tokens", rows)
	}
}

// TestRelayVideoExtendedContractValidation pins the new contract fields'
// rejections(发生在预扣之前):last_image/references 条目 http(s) 契约、
// kind 枚举、条数上限、ratio 长度;一例合法新字段请求原样透传(openai 形
// 渠道不剥离)。
func TestRelayVideoExtendedContractValidation(t *testing.T) {
	env := newRelayEnv(t, nil)
	vendor := newFakeVideoVendor(t)
	env.seedVideoChannel(t, "wan", vendor.server.URL, "vid-m", "upstream-vid", 0,
		[]channel.Capability{channel.CapVideos})
	key, full := env.seedKey(t, 1_000_000)
	env.seedVideoPrice(t, "vid-m", nil)

	longURL := "https://" + strings.Repeat("u", 4200)
	tenRefs := make([]string, 10)
	for i := range tenRefs {
		tenRefs[i] = fmt.Sprintf(`{"url":"https://cdn.example/%d.png","kind":"image"}`, i)
	}
	for _, tc := range []struct {
		name string
		body string
	}{
		{"image data uri", `{"model":"vid-m","prompt":"x","image":"data:image/png;base64,AAAA"}`},
		{"image not a url", `{"model":"vid-m","prompt":"x","image":"ftp://x/y.png"}`},
		{"image too long", fmt.Sprintf(`{"model":"vid-m","prompt":"x","image":%q}`, longURL)},
		{"last_image data uri", `{"model":"vid-m","prompt":"x","last_image":"data:image/png;base64,AAAA"}`},
		{"last_image too long", fmt.Sprintf(`{"model":"vid-m","prompt":"x","last_image":%q}`, longURL)},
		{"ratio too long", fmt.Sprintf(`{"model":"vid-m","prompt":"x","ratio":%q}`, strings.Repeat("w", 65))},
		{"reference kind unknown", `{"model":"vid-m","prompt":"x","references":[{"url":"https://x/1.png","kind":"doc"}]}`},
		{"reference url data uri", `{"model":"vid-m","prompt":"x","references":[{"url":"data:image/png;base64,AAAA","kind":"image"}]}`},
		{"references over nine", `{"model":"vid-m","prompt":"x","references":[` + strings.Join(tenRefs, ",") + `]}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := env.postVideo(t, full, tc.body)
			if w.Code != http.StatusBadRequest {
				t.Fatalf("status = %d body %s, want 400", w.Code, w.Body.String())
			}
			if eb := decodeOpenAIError(t, w.Body.Bytes()); eb.Error.Code != "invalid_request" {
				t.Errorf("error code = %q, want invalid_request", eb.Error.Code)
			}
		})
	}

	// 拒绝都在预扣之前:余额、上游、任务行、用量行全部原封。
	if balance := env.balanceOf(t, key.ID); balance != 1_000_000 {
		t.Errorf("balance = %d, want untouched", balance)
	}
	if submits, _, _ := vendor.counts(); submits != 0 {
		t.Errorf("upstream submits = %d, want 0", submits)
	}

	// 合法新字段:openai 形渠道全量透传(请求体仅重写 model,新字段不剥离)。
	ok := `{"model":"vid-m","prompt":"x","ratio":"21:9","last_image":"https://cdn.example/last.png",` +
		`"references":[{"url":"https://cdn.example/r.mp4","kind":"video"}]}`
	if w := env.postVideo(t, full, ok); w.Code != http.StatusOK {
		t.Fatalf("valid extended body rejected: %d %s", w.Code, w.Body.String())
	}
	forwarded := vendor.submittedBody(t)
	for _, needle := range []string{`"ratio":"21:9"`, `"last_image":"https://cdn.example/last.png"`, `"kind":"video"`, `"upstream-vid"`} {
		if !strings.Contains(forwarded, needle) {
			t.Errorf("forwarded body missing %s: %s", needle, forwarded)
		}
	}
}
