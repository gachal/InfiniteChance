package relay_test

// 07 号票(生图同步转发 + 按次计费)的端到端测试:OpenAI 形状的
// /v1/images/generations(JSON)与 /v1/images/edits(multipart)同步转发,
// 「按次 USD 单价 × 尺寸系数」的预扣/结算/退款,以及生图只落在声明
// images 能力的渠道上。共享 relay_test.go 的 MySQL 环境与种子助手,上游
// 是内存 fake(spec 的主缝:HTTP API + fake 上游 + 真实 MySQL)。

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/gachal/InfiniteChance/internal/apikey"
	"github.com/gachal/InfiniteChance/internal/channel"
	"github.com/gachal/InfiniteChance/internal/pricing"
	"github.com/gachal/InfiniteChance/internal/relay"
	"github.com/gachal/InfiniteChance/internal/usage"
)

const imageBody = `{"model":"img-m","prompt":"一只在月光下奔跑的猫","size":"1024x1024"}`

// okImagesHandler answers one successful images response carrying the given
// data entries ({"url":…} 或 {"b64_json":…} 形状由测试决定)。
func okImagesHandler(entries ...map[string]any) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if entries == nil {
			entries = []map[string]any{}
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"created": 1_700_000_000,
			"data":    entries,
		})
	}
}

// seedImageChannel inserts one enabled channel with explicit capabilities
// (07 号票)and scheduling priority, mapping publicModel→upstreamModel.
func (e *relayEnv) seedImageChannel(t *testing.T, name, baseURL, publicModel, upstreamModel string, priority int, caps []channel.Capability) channel.Channel {
	t.Helper()
	ch, err := e.stores.channels.Create(context.Background(), channel.Channel{
		Name: name, Type: channel.TypeOpenAI, BaseURL: baseURL,
		APIKey: "vendor-secret-key", ModelMap: map[string]string{publicModel: upstreamModel},
		Capabilities: caps, Priority: priority, Enabled: true,
	})
	if err != nil {
		t.Fatalf("seed image channel: %v", err)
	}
	return ch
}

// seedImagePrice writes the call-track test price: $0.04/张 plus optional
// size factors (micro-units;nil = 全部 ×1.0).
func (e *relayEnv) seedImagePrice(t *testing.T, publicModel string, factors map[string]int64) {
	t.Helper()
	_, err := e.stores.prices.Upsert(context.Background(), pricing.Price{
		PublicModel: publicModel, Unit: pricing.UnitCall,
		Call: &pricing.CallPrice{USDPerCallMicros: 40_000, SizeFactorMicros: factors},
	})
	if err != nil {
		t.Fatalf("seed image price: %v", err)
	}
}

func (e *relayEnv) postImages(t *testing.T, fullKey, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/v1/images/generations", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+fullKey)
	w := httptest.NewRecorder()
	e.engine.ServeHTTP(w, req)
	return w
}

func TestRelayImagesGenerationsEndToEnd(t *testing.T) {
	env := newRelayEnv(t, nil)
	upstream := newFakeUpstream(t, okImagesHandler(map[string]any{"url": "https://img.example/cat.png"}))
	env.seedImageChannel(t, "img", upstream.server.URL, "img-m", "upstream-img", 0,
		[]channel.Capability{channel.CapChat, channel.CapImages})
	key, full := env.seedKey(t, 1_000_000)
	env.seedImagePrice(t, "img-m", nil)

	w := env.postImages(t, full, imageBody)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d body %s, want 200", w.Code, w.Body.String())
	}

	// 客户端拿到透传的 images 响应:URL 在 data 里原样可达。
	var resp struct {
		Created int64 `json:"created"`
		Data    []struct {
			URL string `json:"url"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("response not JSON: %v (%s)", err, w.Body.String())
	}
	if len(resp.Data) != 1 || resp.Data[0].URL != "https://img.example/cat.png" {
		t.Errorf("data = %+v, want the vendor's URL entry", resp.Data)
	}

	// 上游收到 images/generations 调用:模型名已映射、厂商密钥在头里。
	if upstream.callCount() != 1 || upstream.lastPath() != "/images/generations" {
		t.Fatalf("upstream = %d calls on %q, want one on /images/generations",
			upstream.callCount(), upstream.lastPath())
	}
	if got := upstream.models[0]; got != "upstream-img" {
		t.Errorf("upstream model = %q, want mapped upstream-img", got)
	}
	if got := upstream.auth[0]; got != "Bearer vendor-secret-key" {
		t.Errorf("upstream auth = %q, want vendor key", got)
	}

	// 账务:n 缺省 1、尺寸 ×1.0 → 预扣即实扣 $0.04 = 40000 micros;差额
	// 为零不产生 settle 流水(initial + estimate 两条)。
	const wantCharge = int64(40_000)
	if balance := env.balanceOf(t, key.ID); balance != 1_000_000-wantCharge {
		t.Errorf("balance = %d, want %d (initial minus the per-call charge)", balance, 1_000_000-wantCharge)
	}
	entries := env.quotaLog(t, key.ID)
	if len(entries) != 2 || entries[0].Reason != apikey.ReasonEstimate || entries[1].Reason != apikey.ReasonInitial {
		t.Fatalf("ledger = %+v, want initial + estimate (zero settle delta books nothing)", entries)
	}
	if entries[0].DeltaMicros != -wantCharge {
		t.Errorf("estimate delta = %d, want %d", entries[0].DeltaMicros, -wantCharge)
	}

	// 留痕:unit=call、零 token、快照带按次价格与请求事实(size、n)。
	rows := env.usageRows(t)
	if len(rows) != 1 {
		t.Fatalf("usage rows = %d, want 1", len(rows))
	}
	row := rows[0]
	if row.Unit != "call" || row.PromptTokens != 0 || row.CompletionTokens != 0 ||
		row.Status != usage.StatusSuccess || row.ChargeMicros != wantCharge ||
		row.PublicModel != "img-m" || row.UpstreamModel != "upstream-img" {
		t.Errorf("usage row = %+v, want a call-track success trail", row)
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
	if err := json.Unmarshal(row.PriceSnapshot, &snapshot); err != nil {
		t.Fatalf("snapshot not JSON: %v (%s)", err, row.PriceSnapshot)
	}
	if snapshot.Unit != "call" || snapshot.Call.USDPerCallMicros != 40_000 ||
		snapshot.Request.Size != "1024x1024" || snapshot.Request.N != 1 {
		t.Errorf("snapshot = %s, want call track with request {1024x1024, 1}", row.PriceSnapshot)
	}
}

func TestRelayImagesSizeFactorBillsAndTruesUp(t *testing.T) {
	env := newRelayEnv(t, nil)
	// 大尺寸 ×2.0;n=2 预扣 2×80000,上游却只交付一张(b64 形状)——
	// 结算按实交 1 张补退一半。
	upstream := newFakeUpstream(t, okImagesHandler(map[string]any{"b64_json": "aGVsbG8="}))
	env.seedImageChannel(t, "img", upstream.server.URL, "img-m", "upstream-img", 0,
		[]channel.Capability{channel.CapImages})
	key, full := env.seedKey(t, 1_000_000)
	env.seedImagePrice(t, "img-m", map[string]int64{"1024x1024": 1_000_000, "1792x1024": 2_000_000})

	w := env.postImages(t, full, `{"model":"img-m","prompt":"星空","n":2,"size":"1792x1024"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d body %s, want 200", w.Code, w.Body.String())
	}

	// 预扣 160000,实扣 80000,退回 80000。
	if balance := env.balanceOf(t, key.ID); balance != 1_000_000-80_000 {
		t.Errorf("balance = %d, want %d (charged per delivered image)", balance, 1_000_000-80_000)
	}
	entries := env.quotaLog(t, key.ID)
	if len(entries) != 3 || entries[0].Reason != apikey.ReasonSettle || entries[0].DeltaMicros != 80_000 {
		t.Fatalf("ledger = %+v, want a settle refund of 80000", entries)
	}
	rows := env.usageRows(t)
	if len(rows) != 1 || rows[0].ChargeMicros != 80_000 {
		t.Fatalf("usage rows = %+v, want one row charged 80000", rows)
	}
}

func TestRelayImagesFailureRefundsReserve(t *testing.T) {
	env := newRelayEnv(t, nil)
	upstream := newFakeUpstream(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusServiceUnavailable)
		w.Write([]byte(`{"error":{"message":"image queue overloaded","type":"server_error","code":null}}`))
	})
	env.seedImageChannel(t, "img", upstream.server.URL, "img-m", "upstream-img", 0,
		[]channel.Capability{channel.CapImages})
	key, full := env.seedKey(t, 1_000_000)
	env.seedImagePrice(t, "img-m", nil)

	w := env.postImages(t, full, imageBody)
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d body %s, want upstream 503 passthrough", w.Code, w.Body.String())
	}
	eb := decodeOpenAIError(t, w.Body.Bytes())
	if eb.Error.Code != "upstream_error" || !strings.Contains(eb.Error.Message, "image queue overloaded") {
		t.Errorf("error object = %+v, want upstream_error carrying the vendor message", eb.Error)
	}

	// 预扣全额退回;失败照常留痕,零扣费。
	if balance := env.balanceOf(t, key.ID); balance != 1_000_000 {
		t.Errorf("balance = %d, want fully refunded 1_000_000", balance)
	}
	entries := env.quotaLog(t, key.ID)
	if len(entries) != 3 || entries[0].Reason != apikey.ReasonRefund || entries[0].DeltaMicros != 40_000 {
		t.Fatalf("ledger = %+v, want the 40000 reserve refunded", entries)
	}
	rows := env.usageRows(t)
	if len(rows) != 1 || rows[0].Status != usage.StatusUpstreamError || rows[0].ChargeMicros != 0 ||
		!strings.Contains(rows[0].UpstreamError, "image queue overloaded") {
		t.Errorf("failure trail = %+v, want upstream_error with vendor summary and zero charge", rows)
	}
	if rows[0].Unit != "call" {
		t.Errorf("failure row unit = %q, want call", rows[0].Unit)
	}
}

func TestRelayImagesNeverRoutedToChatOnlyChannels(t *testing.T) {
	env := newRelayEnv(t, nil)
	upstream := newFakeUpstream(t, okImagesHandler(map[string]any{"url": "https://img.example/x.png"}))
	// chat-only 渠道优先级更高:若能力过滤失效,生图会先打它(成功行会
	// 记在它名下,断言即破)。
	env.seedImageChannel(t, "chatonly", upstream.server.URL, "img-m", "up-img", 10,
		[]channel.Capability{channel.CapChat})
	env.seedImageChannel(t, "imgcap", upstream.server.URL, "img-m", "up-img", 1,
		[]channel.Capability{channel.CapChat, channel.CapImages})
	// 仅 chat-only 渠道挂的模型:生图必须 404。
	env.seedImageChannel(t, "chatonly2", upstream.server.URL, "chatonly-m", "up-m", 0, nil)
	// 仅 images 渠道挂的模型:聊天必须 404。
	env.seedImageChannel(t, "imgonly", upstream.server.URL, "imgonly-m", "up-m", 0,
		[]channel.Capability{channel.CapImages})
	key, full := env.seedKey(t, 1_000_000)
	env.seedImagePrice(t, "img-m", nil)

	// 生图打到 imgcap,chatonly 零参与。
	w := env.postImages(t, full, imageBody)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d body %s, want 200 via the image-capable channel", w.Code, w.Body.String())
	}
	rows := env.usageRows(t)
	if len(rows) != 1 || rows[0].ChannelName != "imgcap" {
		t.Fatalf("usage rows = %+v, want the request served by imgcap", rows)
	}

	// 只挂聊天渠道的模型生图、只挂生图渠道的模型聊天,两个方向都被 404
	// 拒绝(发生在计价之前,无需配价)。
	w = env.postImages(t, full, `{"model":"chatonly-m","prompt":"x"}`)
	if w.Code != http.StatusNotFound {
		t.Fatalf("chat-only model images status = %d body %s, want 404", w.Code, w.Body.String())
	}
	eb := decodeOpenAIError(t, w.Body.Bytes())
	if eb.Error.Code != "model_not_found" || !strings.Contains(eb.Error.Message, "image-capable") {
		t.Errorf("error object = %+v, want model_not_found naming the capability gap", eb.Error)
	}
	w = env.post(t, full, `{"model":"imgonly-m","messages":[{"role":"user","content":"hi"}]}`)
	if w.Code != http.StatusNotFound {
		t.Fatalf("images-only model chat status = %d, want 404", w.Code)
	}

	// 全程账目干净:除成功那笔 40000 外无额外扣减。
	if balance := env.balanceOf(t, key.ID); balance != 1_000_000-40_000 {
		t.Errorf("balance = %d, want only the one image charge", balance)
	}
}

func TestRelayImagesFailoverToImageCapableBackup(t *testing.T) {
	env := newRelayEnv(t, nil)
	primary := newFakeUpstream(t, unavailableHandler())
	backup := newFakeUpstream(t, okImagesHandler(map[string]any{"url": "https://img.example/backup.png"}))
	env.seedImageChannel(t, "primary", primary.server.URL, "img-m", "up-img", 10,
		[]channel.Capability{channel.CapImages})
	env.seedImageChannel(t, "backup", backup.server.URL, "img-m", "up-img", 1,
		[]channel.Capability{channel.CapImages})
	key, full := env.seedKey(t, 1_000_000)
	env.seedImagePrice(t, "img-m", nil)

	w := env.postImages(t, full, imageBody)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d body %s, want 200 served by the backup", w.Code, w.Body.String())
	}
	if primary.callCount() != 1 || backup.callCount() != 1 {
		t.Errorf("upstream calls = primary %d backup %d, want one each", primary.callCount(), backup.callCount())
	}

	// 计费边界:预扣一次,只有备用渠道的结算入账;换道史进成功行的摘要列。
	if balance := env.balanceOf(t, key.ID); balance != 1_000_000-40_000 {
		t.Errorf("balance = %d, want exactly one per-call charge", balance)
	}
	rows := env.usageRows(t)
	if len(rows) != 1 || rows[0].ChannelName != "backup" || rows[0].ChargeMicros != 40_000 {
		t.Fatalf("usage rows = %+v, want one success row on the backup", rows)
	}
	if !strings.Contains(rows[0].UpstreamError, "'primary'") {
		t.Errorf("upstream_error = %q, want the failover trail naming the primary", rows[0].UpstreamError)
	}
}

func TestRelayImagesEditsMultipartRebuildAndBilling(t *testing.T) {
	env := newRelayEnv(t, nil)
	var mu sync.Mutex
	var sawPath, sawContentType, sawModel, sawPrompt string
	var sawPNG []byte
	upstream := newFakeUpstream(t, func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseMultipartForm(32 << 20); err != nil {
			http.Error(w, "not multipart", http.StatusBadRequest)
			return
		}
		mu.Lock()
		sawPath = r.URL.Path
		sawContentType = r.Header.Get("Content-Type")
		sawModel = r.FormValue("model")
		sawPrompt = r.FormValue("prompt")
		if f, _, err := r.FormFile("image"); err == nil {
			sawPNG, _ = io.ReadAll(f)
			f.Close()
		}
		mu.Unlock()
		okImagesHandler(map[string]any{"b64_json": "aW1nLWRhdGE="})(w, r)
	})
	env.seedImageChannel(t, "img", upstream.server.URL, "img-m", "upstream-img", 0,
		[]channel.Capability{channel.CapImages})
	key, full := env.seedKey(t, 1_000_000)
	env.seedImagePrice(t, "img-m", nil)

	// 客户端按 OpenAI SDK 的 multipart 形状上传:图片文件 + prompt/model/size。
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	png := []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n', 1, 2, 3, 4}
	part, _ := mw.CreateFormFile("image", "cat.png")
	part.Write(png)
	mw.WriteField("prompt", "把背景换成星空")
	mw.WriteField("model", "img-m")
	mw.WriteField("size", "512x512")
	mw.Close()

	req := httptest.NewRequest(http.MethodPost, "/v1/images/edits", &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.Header.Set("Authorization", "Bearer "+full)
	w := httptest.NewRecorder()
	env.engine.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d body %s, want 200", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "aW1nLWRhdGE=") {
		t.Errorf("response = %s, want the vendor's b64 entry passthrough", w.Body.String())
	}

	// 上游收到的是重建后的 multipart:model 换成上游名,文件字节与文本
	// 字段原样保留。
	mu.Lock()
	defer mu.Unlock()
	if sawPath != "/images/edits" {
		t.Errorf("upstream path = %q, want /images/edits", sawPath)
	}
	if !strings.HasPrefix(sawContentType, "multipart/form-data") {
		t.Errorf("upstream content-type = %q, want multipart", sawContentType)
	}
	if sawModel != "upstream-img" {
		t.Errorf("upstream model field = %q, want rewritten upstream-img", sawModel)
	}
	if sawPrompt != "把背景换成星空" {
		t.Errorf("upstream prompt field = %q, want the client's prompt intact", sawPrompt)
	}
	if !bytes.Equal(sawPNG, png) {
		t.Errorf("uploaded file bytes = %v, want the original PNG bytes %v", sawPNG, png)
	}

	// 计费:n 缺省 1、size 无系数按 ×1.0 → 实扣 40000。
	if balance := env.balanceOf(t, key.ID); balance != 1_000_000-40_000 {
		t.Errorf("balance = %d, want %d (default n=1 at ×1.0)", balance, 1_000_000-40_000)
	}
	rows := env.usageRows(t)
	if len(rows) != 1 || rows[0].Unit != "call" || rows[0].ChargeMicros != 40_000 {
		t.Fatalf("usage rows = %+v, want one call-track success row", rows)
	}
}

func TestRelayImagesValidation(t *testing.T) {
	env := newRelayEnv(t, nil)
	upstream := newFakeUpstream(t, okImagesHandler(map[string]any{"url": "https://img.example/x.png"}))
	env.seedImageChannel(t, "img", upstream.server.URL, "img-m", "upstream-img", 0,
		[]channel.Capability{channel.CapImages})
	env.seedImageChannel(t, "img2", upstream.server.URL, "tokenpriced-m", "up", 0,
		[]channel.Capability{channel.CapImages})
	env.seedImageChannel(t, "img3", upstream.server.URL, "unpriced-m", "up", 0,
		[]channel.Capability{channel.CapImages})
	key, full := env.seedKey(t, 1_000_000)
	env.seedImagePrice(t, "img-m", nil)
	env.seedPrice(t, "tokenpriced-m") // token 轨价格:生图必须拒绝

	for _, tc := range []struct {
		name       string
		body       string
		wantStatus int
		wantCode   string
	}{
		{"missing model", `{"prompt":"x"}`, 400, "missing_model"},
		{"n below 1", `{"model":"img-m","n":0}`, 400, "invalid_request"},
		{"n over 100", `{"model":"img-m","n":101}`, 400, "invalid_request"},
		{"n not an integer", `{"model":"img-m","n":1.5}`, 400, "invalid_request"},
		{"not json", `{invalid`, 400, "invalid_request"},
		{"token-track model", `{"model":"tokenpriced-m","prompt":"x"}`, 400, "model_not_priced"},
		{"unpriced model", `{"model":"unpriced-m","prompt":"x"}`, 400, "model_not_priced"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := env.postImages(t, full, tc.body)
			if w.Code != tc.wantStatus {
				t.Fatalf("status = %d body %s, want %d", w.Code, w.Body.String(), tc.wantStatus)
			}
			eb := decodeOpenAIError(t, w.Body.Bytes())
			if eb.Error.Code != tc.wantCode || eb.Error.Type != "invalid_request_error" {
				t.Errorf("error object = %+v, want code %s", eb.Error, tc.wantCode)
			}
		})
	}

	// 全部拒绝发生在预扣之前:余额原封,上游零调用,无用量行。
	if balance := env.balanceOf(t, key.ID); balance != 1_000_000 {
		t.Errorf("balance = %d, want untouched", balance)
	}
	if upstream.callCount() != 0 {
		t.Errorf("upstream called %d times, want 0", upstream.callCount())
	}
	if rows := env.usageRows(t); len(rows) != 0 {
		t.Errorf("usage rows = %d, want none for pre-billing rejections", len(rows))
	}
}

func TestRelayAdaptorNormalizeImages(t *testing.T) {
	// Adaptor 单元缝:数图、响应里的 model 存在才回写,坏形状报错。
	a := relay.NewOpenAIAdaptor()

	body, images, err := a.NormalizeImages("img-m", []byte(`{"created":1,"model":"upstream-img","data":[{"url":"u1"},{"url":"u2"}]}`))
	if err != nil || images != 2 {
		t.Fatalf("NormalizeImages = %d images (err %v), want 2", images, err)
	}
	var out map[string]any
	if err := json.Unmarshal(body, &out); err != nil || out["model"] != "img-m" {
		t.Errorf("normalized model = %v (err %v), want rewritten img-m", out["model"], err)
	}

	// 经典 images 响应没有 model 字段:不得注入,原样透传。
	body, images, err = a.NormalizeImages("img-m", []byte(`{"created":1,"data":[{"b64_json":"x"}]}`))
	if err != nil || images != 1 {
		t.Fatalf("NormalizeImages = %d images (err %v), want 1", images, err)
	}
	if strings.Contains(string(body), `"model"`) {
		t.Errorf("response without a model field got one injected: %s", body)
	}

	// 空数据可数出 0(换道决策由处理器做);坏形状报错。
	if _, images, err := a.NormalizeImages("m", []byte(`{"created":1,"data":[]}`)); err != nil || images != 0 {
		t.Errorf("empty data = %d images (err %v), want 0 and nil error", images, err)
	}
	if _, _, err := a.NormalizeImages("m", []byte(`not-json`)); err == nil {
		t.Errorf("NormalizeImages non-object = nil error, want failure")
	}
	if _, _, err := a.NormalizeImages("m", []byte(`{"data":{"bad":"shape"}}`)); err == nil {
		t.Errorf("NormalizeImages non-array data = nil error, want failure")
	}
}
