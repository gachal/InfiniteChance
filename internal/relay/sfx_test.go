package relay_test

// 61 号票(音效生成,VOD AIGC 聚合渠道)的端到端测试:同步 POST
// /v1/audio/sfx 单面直达音效 URL;second 轨预扣 = 单价 × seconds、实结 =
// 预扣定格(usage.seconds 回显);seconds 域 1–30 与 text 1–2000 的 400、
// model_not_priced、无 sfx 能力渠道 404;上游终态失败 502 触发换道(预扣
// 原封带往下一渠道)。/v1/audio/sfx-models 只含 sfx 能力渠道 ∩ second 配价。
// 走真实 VOD adaptor(TC3 签名对本地桩,桩不校签),共享 relay_test.go 的
// MySQL 环境,基础设施不可达时自动 skip。

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/gachal/InfiniteChance/internal/channel"
	"github.com/gachal/InfiniteChance/internal/pricing"
)

// fakeSfxVendor 是音效厂商替身:提交应答固定受理,轮询应答可按剧本切换
// (detail 为空时回答 WAITING)。sfx 与音乐共用 CreateAigcAudioTask,但
// 这里只服务音效用例,抄 music_vod_test.go 的 fakeVODVendor 形状另立一份
// —— 两个替身各自演进,不共享剧本状态。
type fakeSfxVendor struct {
	server *httptest.Server
	mu     sync.Mutex

	detail  string // DescribeTaskDetail 的下一拍应答体(原文)
	submits int
	bodies  []string
}

func newFakeSfxVendor(t *testing.T) *fakeSfxVendor {
	t.Helper()
	v := &fakeSfxVendor{}
	v.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		v.mu.Lock()
		defer v.mu.Unlock()
		raw, _ := io.ReadAll(r.Body)
		switch r.Header.Get("X-TC-Action") {
		case "CreateAigcAudioTask":
			v.submits++
			v.bodies = append(v.bodies, string(raw))
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"Response":{"TaskId":"vod-sfx-1"}}`))
		case "DescribeTaskDetail":
			w.Header().Set("Content-Type", "application/json")
			if v.detail == "" {
				w.Write([]byte(`{"Response":{"AigcAudioTask":{"Status":"WAITING"}}}`))
				return
			}
			w.Write([]byte(v.detail))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(v.server.Close)
	return v
}

func (v *fakeSfxVendor) scriptDetail(detail string) {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.detail = detail
}

func (v *fakeSfxVendor) submitCount() int {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.submits
}

func (v *fakeSfxVendor) lastSubmitBody() string {
	v.mu.Lock()
	defer v.mu.Unlock()
	if len(v.bodies) == 0 {
		return ""
	}
	return v.bodies[len(v.bodies)-1]
}

// seedVODSfxChannel inserts one enabled tencent-vod channel with the sfx
// capability whose TC3 endpoint is the stub —— 61 号票定案:能力过滤只看
// CapSfx,VOD 渠道勾上即可被音效调度(与 music 能力互不串道)。
func (e *relayEnv) seedVODSfxChannel(t *testing.T, baseURL, publicModel, upstreamModel string, priority int) channel.Channel {
	t.Helper()
	ch, err := e.stores.channels.Create(context.Background(), channel.Channel{
		Name: "vod-sfx-stub", Type: channel.TypeTencentVod, BaseURL: baseURL,
		Config: map[string]string{
			"secret_id":  "AKIDtest",
			"secret_key": "vodtestkey",
			"sub_app_id": "1500000000",
		},
		ModelMap:     map[string]string{publicModel: upstreamModel},
		Capabilities: []channel.Capability{channel.CapSfx},
		Priority:     priority,
		Enabled:      true,
	})
	if err != nil {
		t.Fatalf("seed vod sfx channel: %v", err)
	}
	return ch
}

// seedSfxPrice writes the second-track test price: ¥0.10/秒。
func (e *relayEnv) seedSfxPrice(t *testing.T, publicModel string) {
	t.Helper()
	_, err := e.stores.prices.Upsert(context.Background(), pricing.Price{
		PublicModel: publicModel, Unit: pricing.UnitSecond,
		Call: &pricing.CallPrice{CNYPerCallMicros: 100_000},
	})
	if err != nil {
		t.Fatalf("seed sfx price: %v", err)
	}
}

func (e *relayEnv) postSfx(t *testing.T, fullKey, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/v1/audio/sfx", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+fullKey)
	w := httptest.NewRecorder()
	e.engine.ServeHTTP(w, req)
	return w
}

// 同步全链:提交 + adaptor 内等待 SUCCESS → 一次响应带回音效 URL;
// second 轨预扣 = 单价 × seconds(¥0.10 × 5s = 500_000 微元),实结 =
// 预扣恒等(成功后余额不动);usage.seconds 回显请求秒数。
func TestRelayVODSfxEndToEnd(t *testing.T) {
	env := newRelayEnv(t, nil)
	vendor := newFakeSfxVendor(t)
	vendor.scriptDetail(`{"Response":{"AigcAudioTask":{"Status":"SUCCESS","Output":{"FileInfos":[{"FileUrl":"https://vod-temp.example/boom.mp3"}]}}}}`)
	env.seedVODSfxChannel(t, vendor.server.URL, "vod-kling-sfx", "Kling sfx", 0)
	key, full := env.seedKey(t, 2_000_000)
	env.seedSfxPrice(t, "vod-kling-sfx")

	w := env.postSfx(t, full, `{"model":"vod-kling-sfx","text":"雷声轰鸣,由远及近","seconds":5}`)
	if w.Code != http.StatusOK {
		t.Fatalf("sfx status = %d body %s, want 200", w.Code, w.Body.String())
	}
	var delivered struct {
		Audio *struct {
			URL string `json:"url"`
		} `json:"audio"`
		Usage *struct {
			Seconds int64 `json:"seconds"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &delivered); err != nil {
		t.Fatalf("sfx body: %v (%s)", err, w.Body.String())
	}
	if delivered.Audio == nil || delivered.Audio.URL != "https://vod-temp.example/boom.mp3" {
		t.Fatalf("audio = %v, want the VOD Output URL", delivered.Audio)
	}
	if delivered.Usage == nil || delivered.Usage.Seconds != 5 {
		t.Fatalf("usage = %v, want seconds=5 echoed", delivered.Usage)
	}

	// 上游收到翻译后的 CreateAigcAudioTask:SceneType=sfx、Duration=5、
	// OutputConfig(Temporary + mp3)、SubAppId,无 AdditionalParameters。
	body := vendor.lastSubmitBody()
	for _, want := range []string{
		`"ModelName":"Kling"`, `"ModelVersion":"sfx"`, `"SceneType":"sfx"`,
		`"Duration":5`, `"StorageMode":"Temporary"`, `"OutputAudioFormat":"mp3"`,
		`"SubAppId":1500000000`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("submit body missing %s:\n%s", want, body)
		}
	}
	if strings.Contains(body, "AdditionalParameters") {
		t.Errorf("submit body = %s, want no AdditionalParameters (Kling 无需)", body)
	}

	// 账务:预扣 ¥0.10 × 5s = 500_000;实结 = 预扣(second 轨定格),
	// 成功后余额与预扣后一致。
	if balance := env.balanceOf(t, key.ID); balance != 1_500_000 {
		t.Fatalf("balance after success = %d, want 1_500_000 (reserve became the charge)", balance)
	}
}

// 校验面:seconds 出 1–30 域、text 空/超 2000 → 400 invalid_request_error
// (拨号之前拒绝);未配价 → 400 model_not_priced;渠道只勾 music 不勾
// sfx → 404 model_not_found(能力互不串道)。
func TestRelayVODSfxValidations(t *testing.T) {
	env := newRelayEnv(t, nil)
	vendor := newFakeSfxVendor(t)
	env.seedVODSfxChannel(t, vendor.server.URL, "vod-kling-sfx", "Kling sfx", 0)
	_, full := env.seedKey(t, 2_000_000)
	env.seedSfxPrice(t, "vod-kling-sfx")

	cases := []struct {
		name string
		code int
		body string
	}{
		{"seconds=0", http.StatusBadRequest, `{"model":"vod-kling-sfx","text":"boom","seconds":0}`},
		{"seconds=31", http.StatusBadRequest, `{"model":"vod-kling-sfx","text":"boom","seconds":31}`},
		{"缺 seconds", http.StatusBadRequest, `{"model":"vod-kling-sfx","text":"boom"}`},
		{"空 text", http.StatusBadRequest, `{"model":"vod-kling-sfx","text":"   ","seconds":5}`},
		{"超长 text", http.StatusBadRequest, `{"model":"vod-kling-sfx","text":"` + strings.Repeat("雷", 2001) + `","seconds":5}`},
	}
	for _, tc := range cases {
		w := env.postSfx(t, full, tc.body)
		if w.Code != tc.code || !strings.Contains(w.Body.String(), "invalid_request_error") {
			t.Errorf("%s: status = %d body %s, want %d invalid_request_error", tc.name, w.Code, w.Body.String(), tc.code)
		}
	}
	if submits := vendor.submitCount(); submits != 0 {
		t.Errorf("vendor submits = %d, want 0 (refused before dialing)", submits)
	}

	// 未配价:400 model_not_priced。
	env.seedVODSfxChannel(t, vendor.server.URL, "sfx-unpriced", "Kling sfx", 0)
	if w := env.postSfx(t, full, `{"model":"sfx-unpriced","text":"boom","seconds":5}`); w.Code != http.StatusBadRequest ||
		!strings.Contains(w.Body.String(), "model_not_priced") {
		t.Fatalf("unpriced = %d %s, want 400 model_not_priced", w.Code, w.Body.String())
	}

	// 只勾 music 能力的渠道不服务音效:404 model_not_found。
	musicOnly, err := env.stores.channels.Create(context.Background(), channel.Channel{
		Name: "vod-music-only", Type: channel.TypeTencentVod, BaseURL: vendor.server.URL,
		Config:       map[string]string{"secret_id": "AKIDtest", "secret_key": "vodtestkey"},
		ModelMap:     map[string]string{"sfx-music-ch": "Kling sfx"},
		Capabilities: []channel.Capability{channel.CapMusic},
		Enabled:      true,
	})
	if err != nil {
		t.Fatalf("seed music-only channel: %v", err)
	}
	_ = musicOnly
	env.seedSfxPrice(t, "sfx-music-ch")
	w := env.postSfx(t, full, `{"model":"sfx-music-ch","text":"boom","seconds":5}`)
	if w.Code != http.StatusNotFound || !strings.Contains(w.Body.String(), "model_not_found") {
		t.Fatalf("music-only channel = %d %s, want 404 model_not_found", w.Code, w.Body.String())
	}
}

// 换道:首选渠道终态 FAIL(502,临时失败语义)→ 预扣原封带往次选渠道,
// 次选成功即结清 —— 客户端只看到最终成功,账只结一次。
func TestRelayVODSfxFailoverToSecondChannel(t *testing.T) {
	env := newRelayEnv(t, nil)
	broken := newFakeSfxVendor(t)
	broken.scriptDetail(`{"Response":{"AigcAudioTask":{"Status":"FAIL","ErrCode":"InternalError","Message":"boom"}}}`)
	healthy := newFakeSfxVendor(t)
	healthy.scriptDetail(`{"Response":{"AigcAudioTask":{"Status":"SUCCESS","Output":{"FileInfos":[{"FileUrl":"https://vod-temp.example/failover.mp3"}]}}}}`)
	env.seedVODSfxChannel(t, broken.server.URL, "vod-kling-sfx", "Kling sfx", 10) // 高优先级 = 首选
	env.seedVODSfxChannel(t, healthy.server.URL, "vod-kling-sfx", "Kling sfx", 0) // 次选
	key, full := env.seedKey(t, 2_000_000)
	env.seedSfxPrice(t, "vod-kling-sfx")

	w := env.postSfx(t, full, `{"model":"vod-kling-sfx","text":"玻璃碎裂","seconds":3}`)
	if w.Code != http.StatusOK {
		t.Fatalf("failover status = %d body %s, want 200 via the second channel", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "https://vod-temp.example/failover.mp3") {
		t.Fatalf("body = %s, want the healthy channel's URL", w.Body.String())
	}
	if n := broken.submitCount(); n != 1 {
		t.Errorf("broken channel submits = %d, want 1", n)
	}
	if n := healthy.submitCount(); n != 1 {
		t.Errorf("healthy channel submits = %d, want 1", n)
	}
	// 一次结清:余额 = 2_000_000 − (¥0.10 × 3s)。
	if balance := env.balanceOf(t, key.ID); balance != 1_700_000 {
		t.Fatalf("balance after failover = %d, want 1_700_000", balance)
	}
}

// 目录:/v1/audio/sfx-models 只含 sfx 能力渠道 ∩ second 配价的模型;
// 音乐模型(call 轨)与 TTS 模型(character 轨)不混入。
func TestRelaySfxModelsCatalog(t *testing.T) {
	env := newRelayEnv(t, nil)
	vendor := newFakeSfxVendor(t)
	env.seedVODSfxChannel(t, vendor.server.URL, "vod-kling-sfx", "Kling sfx", 0)
	env.seedSfxPrice(t, "vod-kling-sfx")
	// second 配价但渠道只勾 music:不进 sfx 目录。
	_, err := env.stores.channels.Create(context.Background(), channel.Channel{
		Name: "vod-music-only", Type: channel.TypeTencentVod, BaseURL: vendor.server.URL,
		Config:       map[string]string{"secret_id": "AKIDtest", "secret_key": "vodtestkey"},
		ModelMap:     map[string]string{"sfx-shadowed": "Kling sfx"},
		Capabilities: []channel.Capability{channel.CapMusic},
		Enabled:      true,
	})
	if err != nil {
		t.Fatalf("seed music-only channel: %v", err)
	}
	env.seedSfxPrice(t, "sfx-shadowed")
	// sfx 能力但配 call 轨:不进目录。
	env.seedVODSfxChannel(t, vendor.server.URL, "sfx-call-priced", "Kling sfx", 0)
	env.seedMusicPrice(t, "sfx-call-priced")

	req := httptest.NewRequest(http.MethodGet, "/v1/audio/sfx-models", nil)
	req.Header.Set("Authorization", "Bearer "+func() string {
		_, full := env.seedKey(t, 1_000_000)
		return full
	}())
	w := httptest.NewRecorder()
	env.engine.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("sfx-models status = %d body %s", w.Code, w.Body.String())
	}
	var catalog struct {
		Models []string `json:"models"`
		Prices map[string]struct {
			Unit       string  `json:"unit"`
			CNYPerCall float64 `json:"cny_per_call"`
		} `json:"prices"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &catalog); err != nil {
		t.Fatalf("catalog body: %v (%s)", err, w.Body.String())
	}
	if len(catalog.Models) != 1 || catalog.Models[0] != "vod-kling-sfx" {
		t.Fatalf("models = %v, want [vod-kling-sfx] only", catalog.Models)
	}
	if p := catalog.Prices["vod-kling-sfx"]; p.Unit != "second" || p.CNYPerCall != 0.1 {
		t.Fatalf("price = %+v, want second track at ¥0.10/秒", p)
	}
}
