package relay_test

// 56 号票(网关 TTS 与音色定制面)的端到端测试:fake 百炼 TTS 上游用
// customization / SpeechSynthesizer 同步面形状(POST /api/v1/services/audio/
// tts/customization 的 create/query/delete_voice 三个 action、POST …/Speech
// Synthesizer 的 input/parameters 三段体),验证四端点路由、钉单渠道不换道、
// 校验链(data: 拒绝、长度上限、mode 互斥必填、渠道存在/启用/audio 能力/
// 模型映射命中)、次轨与字符轨账务时序(enrollment 预扣→实结零差额;speech
// 预扣即实结、失败全额退款)、query/delete 不计费不落用量行。共享
// relay_test.go 的 MySQL 环境。

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
	"github.com/gachal/InfiniteChance/internal/pricing"
	"github.com/gachal/InfiniteChance/internal/usage"
)

const (
	testVoiceID  = "qwen-audio-3.1-tts-plus-vd-zn-1234"
	testTTSModel = "qwen-audio-3.1-tts-plus"
)

// fakeDashScopeTTS 是百炼音色定制/台词合成的同步面替身:customization 按
// body 的 action 分派 create/query/delete_voice;SpeechSynthesizer 回临时
// OSS 地址。请求头(Bearer、X-DashScope-Async 缺席)与最近请求体被留存供
// 翻译断言;script 控制 fail/status/url。
type fakeDashScopeTTS struct {
	server *httptest.Server
	mu     sync.Mutex

	voiceStatus string // query_voice 回的 status
	voiceBody   string // create 应答覆盖(空 = 正常 output)
	speechBody  string // speech 应答覆盖(空 = 正常 audio.url)
	speechURL   string
	status      int // 非 0 = 该状态码统一应答(错误体 code/message)
	errCode     string
	errMsg      string

	calls      int
	lastMethod string
	lastPath   string
	lastAuth   string
	lastAsync  string
	lastBody   []byte
}

func newFakeDashScopeTTS(t *testing.T) *fakeDashScopeTTS {
	t.Helper()
	v := &fakeDashScopeTTS{voiceStatus: "DEPLOYING", speechURL: "https://oss-cn-beijing.aliyuncs.com/tts/out.wav"}
	v.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		v.mu.Lock()
		defer v.mu.Unlock()
		raw, _ := io.ReadAll(r.Body)
		v.calls++
		v.lastMethod, v.lastPath = r.Method, r.URL.Path
		v.lastAuth = r.Header.Get("Authorization")
		v.lastAsync = r.Header.Get("X-DashScope-Async")
		v.lastBody = raw

		w.Header().Set("Content-Type", "application/json")
		if v.status != 0 {
			w.WriteHeader(v.status)
			fmt.Fprintf(w, `{"request_id":"rq-1","code":%q,"message":%q}`, v.errCode, v.errMsg)
			return
		}
		var body struct {
			Action string `json:"action"`
		}
		_ = json.Unmarshal(raw, &body)
		switch {
		case r.URL.Path == "/api/v1/services/audio/tts/customization" && body.Action == "create_voice":
			w.Write([]byte(orDefault(v.voiceBody,
				`{"request_id":"rq-1","output":{"voice_id":"`+testVoiceID+`","target_model":"`+testTTSModel+`","status":"DEPLOYING",`+
					`"preview_audio":{"data":"QUFB","sample_rate":24000,"format":"wav"}}}`)))
		case r.URL.Path == "/api/v1/services/audio/tts/customization" && body.Action == "query_voice":
			fmt.Fprintf(w, `{"request_id":"rq-2","output":{"voice_id":%q,"status":%q}}`, testVoiceID, v.voiceStatus)
		case r.URL.Path == "/api/v1/services/audio/tts/customization" && body.Action == "delete_voice":
			w.Write([]byte(`{"request_id":"rq-3","output":{}}`))
		case r.URL.Path == "/api/v1/services/audio/tts/SpeechSynthesizer":
			w.Write([]byte(orDefault(v.speechBody,
				`{"request_id":"rq-4","output":{"audio":{"url":"`+v.speechURL+`","expires_at":1767000000}}}`)))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(v.server.Close)
	return v
}

func orDefault(s, def string) string {
	if s == "" {
		return def
	}
	return s
}

func (v *fakeDashScopeTTS) callCount() int {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.calls
}

func (v *fakeDashScopeTTS) last() (method, path, auth, async string, body []byte) {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.lastMethod, v.lastPath, v.lastAuth, v.lastAsync, v.lastBody
}

// script puts the vendor into a failure or success state.
func (v *fakeDashScopeTTS) script(status int, errCode, errMsg string) {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.status, v.errCode, v.errMsg = status, errCode, errMsg
}

// seedAudioChannel inserts one enabled dashscope channel with the audio
// capability and both TTS model mappings.
func (e *relayEnv) seedAudioChannel(t *testing.T, name, baseURL string) channel.Channel {
	t.Helper()
	ch, err := e.stores.channels.Create(context.Background(), channel.Channel{
		Name: name, Type: channel.TypeDashScope, BaseURL: baseURL,
		APIKey: "sk-bailian-secret",
		ModelMap: map[string]string{
			"voice-enrollment": "voice-enrollment",
			testTTSModel:       testTTSModel,
		},
		Capabilities: []channel.Capability{channel.CapAudio}, Enabled: true,
	})
	if err != nil {
		t.Fatalf("seed audio channel: %v", err)
	}
	return ch
}

// seedEnrollmentPrice writes the enrollment call-track price: 0.01 元/次.
func (e *relayEnv) seedEnrollmentPrice(t *testing.T) {
	t.Helper()
	if _, err := e.stores.prices.Upsert(context.Background(), pricing.Price{
		PublicModel: "voice-enrollment", Unit: pricing.UnitCall,
		Call: &pricing.CallPrice{CNYPerCallMicros: 10_000},
	}); err != nil {
		t.Fatalf("seed enrollment price: %v", err)
	}
}

// seedSpeechPrice writes the character-track price: 1.4 元/万字符.
func (e *relayEnv) seedSpeechPrice(t *testing.T) {
	t.Helper()
	if _, err := e.stores.prices.Upsert(context.Background(), pricing.Price{
		PublicModel: testTTSModel, Unit: pricing.UnitCharacter,
		Character: &pricing.CharacterPrice{CNYPer10KCharsMicros: 1_400_000},
	}); err != nil {
		t.Fatalf("seed speech price: %v", err)
	}
}

func (e *relayEnv) postPath(t *testing.T, fullKey, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+fullKey)
	w := httptest.NewRecorder()
	e.engine.ServeHTTP(w, req)
	return w
}

func (e *relayEnv) deletePath(t *testing.T, fullKey, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodDelete, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+fullKey)
	w := httptest.NewRecorder()
	e.engine.ServeHTTP(w, req)
	return w
}

const designBody = `{"model":"voice-enrollment","target_model":"` + testTTSModel + `","mode":"design","channel_id":%d,` +
	`"voice_prompt":"低沉磁性青年男声","preview_text":"这一段台词用来试听音色的效果如何。","prefix":"zn56"}`

// TestRelayVoiceDesignCreateEndToEnd pins the create face: 校验通过 → 钉
// 渠道 → 次轨预扣 0.01 元 → 上游 create_voice(action/mode/voice_prompt/
// prefix 齐全,无 X-DashScope-Async 头)→ 实结零差额落成功行。
func TestRelayVoiceDesignCreateEndToEnd(t *testing.T) {
	env := newRelayEnv(t, nil)
	vendor := newFakeDashScopeTTS(t)
	ch := env.seedAudioChannel(t, "bailian-audio", vendor.server.URL)
	key, full := env.seedKey(t, 5_000_000)
	env.seedEnrollmentPrice(t)

	w := env.postPath(t, full, "/v1/audio/voices", fmt.Sprintf(designBody, ch.ID))
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d body %s, want 200", w.Code, w.Body.String())
	}
	var got struct {
		VoiceID      string `json:"voice_id"`
		TargetModel  string `json:"target_model"`
		Status       string `json:"status"`
		PreviewAudio *struct {
			Data       string `json:"data"`
			SampleRate int64  `json:"sample_rate"`
			Format     string `json:"format"`
		} `json:"preview_audio"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("response not JSON: %v (%s)", err, w.Body.String())
	}
	if got.VoiceID != testVoiceID || got.TargetModel != testTTSModel || got.Status != "DEPLOYING" {
		t.Errorf("create response = %+v, want the normalized voice object", got)
	}
	if got.PreviewAudio == nil || got.PreviewAudio.SampleRate != 24000 {
		t.Errorf("preview_audio = %+v, want the base64 preview carried", got.PreviewAudio)
	}

	method, path, auth, async, body := vendor.last()
	if method != http.MethodPost || path != "/api/v1/services/audio/tts/customization" {
		t.Fatalf("upstream saw %s %s, want POST the customization path", method, path)
	}
	if async != "" {
		t.Errorf("X-DashScope-Async = %q, want absent on the sync audio face", async)
	}
	if auth != "Bearer sk-bailian-secret" {
		t.Errorf("auth = %q, want the channel's Bearer key", auth)
	}
	var sent struct {
		Model       string `json:"model"`
		Action      string `json:"action"`
		Mode        string `json:"mode"`
		TargetModel string `json:"target_model"`
		VoicePrompt string `json:"voice_prompt"`
		PreviewText string `json:"preview_text"`
		Prefix      string `json:"prefix"`
		ChannelID   int64  `json:"channel_id"`
	}
	if err := json.Unmarshal(body, &sent); err != nil {
		t.Fatalf("upstream body not JSON: %v (%s)", err, body)
	}
	if sent.Model != "voice-enrollment" || sent.Action != "create_voice" || sent.Mode != "design" ||
		sent.TargetModel != testTTSModel || sent.VoicePrompt != "低沉磁性青年男声" || sent.Prefix != "zn56" {
		t.Errorf("upstream body = %s, want the create_voice translation", body)
	}
	if sent.ChannelID != 0 {
		t.Errorf("channel_id = %d, want stripped from the upstream body", sent.ChannelID)
	}

	// 账务:预扣 0.01 元即实结(零差额无 settle 流水),落 call 轨成功行。
	if balance := env.balanceOf(t, key.ID); balance != 5_000_000-10_000 {
		t.Errorf("balance = %d, want the 0.01 yuan enrollment charge held", balance)
	}
	entries := env.quotaLog(t, key.ID)
	if len(entries) != 2 || entries[0].Reason != "estimate" || entries[1].Reason != "initial" {
		t.Fatalf("ledger = %+v, want initial + estimate only", entries)
	}
	rows := env.usageRows(t)
	if len(rows) != 1 {
		t.Fatalf("usage rows = %d, want 1", len(rows))
	}
	if row := rows[0]; row.Unit != "call" || row.Status != usage.StatusSuccess || row.ChargeMicros != 10_000 ||
		row.ChannelID != ch.ID {
		t.Errorf("usage row = %+v, want a call-track success row on the pinned channel", row)
	}
}

// TestRelayVoiceCreateAutoPrefix:「prefix 缺省自动生成」—— 不传 prefix
// 时上游体带一个 ≤10 位字母数字的缺省值。
func TestRelayVoiceCreateAutoPrefix(t *testing.T) {
	env := newRelayEnv(t, nil)
	vendor := newFakeDashScopeTTS(t)
	ch := env.seedAudioChannel(t, "bailian-audio", vendor.server.URL)
	_, full := env.seedKey(t, 5_000_000)
	env.seedEnrollmentPrice(t)

	body := fmt.Sprintf(`{"model":"voice-enrollment","target_model":"%s","mode":"clone","channel_id":%d,`+
		`"audio":"https://example.com/sample.wav","preview_text":"这一段台词用来试听音色的效果如何。"}`, testTTSModel, ch.ID)
	w := env.postPath(t, full, "/v1/audio/voices", body)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d body %s, want 200", w.Code, w.Body.String())
	}
	_, _, _, _, up := vendor.last()
	var sent struct {
		Prefix string `json:"prefix"`
	}
	if err := json.Unmarshal(up, &sent); err != nil {
		t.Fatalf("upstream body not JSON: %v", err)
	}
	if n := len([]rune(sent.Prefix)); n < 1 || n > 10 || strings.TrimSpace(sent.Prefix) == "" {
		t.Errorf("auto prefix = %q, want 1..10 alphanumerics", sent.Prefix)
	}
	for _, r := range sent.Prefix {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9') {
			t.Errorf("auto prefix %q carries a non-alphanumeric rune", sent.Prefix)
		}
	}
}

// TestRelayVoiceValidation walks the 校验链:每个拒绝都在拨上游之前发生,
// 各给明确 OpenAI error code。
func TestRelayVoiceValidation(t *testing.T) {
	env := newRelayEnv(t, nil)
	vendor := newFakeDashScopeTTS(t)
	ch := env.seedAudioChannel(t, "bailian-audio", vendor.server.URL)
	disabled, err := env.stores.channels.Create(context.Background(), channel.Channel{
		Name: "off", Type: channel.TypeDashScope, BaseURL: vendor.server.URL,
		APIKey: "k", ModelMap: map[string]string{"voice-enrollment": "voice-enrollment"},
		Capabilities: []channel.Capability{channel.CapAudio}, Enabled: false,
	})
	if err != nil {
		t.Fatalf("seed disabled channel: %v", err)
	}
	chatOnly, err := env.stores.channels.Create(context.Background(), channel.Channel{
		Name: "chat-only", Type: channel.TypeDashScope, BaseURL: vendor.server.URL,
		APIKey: "k", ModelMap: map[string]string{"voice-enrollment": "voice-enrollment"},
		Capabilities: nil, Enabled: true, // nil = 历史行,仅 chat
	})
	if err != nil {
		t.Fatalf("seed chat-only channel: %v", err)
	}
	_, full := env.seedKey(t, 5_000_000)
	env.seedEnrollmentPrice(t)

	for _, tc := range []struct {
		name       string
		body       string
		wantStatus int
		wantCode   string
	}{
		{"missing model", `{"mode":"design","channel_id":1,"voice_prompt":"x","preview_text":"这一段台词用来试听音色的效果如何。"}`, 400, "missing_model"},
		{"missing channel_id", `{"model":"voice-enrollment","mode":"design","voice_prompt":"低沉磁性男声","preview_text":"这一段台词用来试听音色的效果如何。"}`, 400, "invalid_request"},
		{"unknown channel", `{"model":"voice-enrollment","target_model":"` + testTTSModel + `","mode":"design","channel_id":9999,"voice_prompt":"低沉磁性男声","preview_text":"这一段台词用来试听音色的效果如何。"}`, 404, "channel_not_found"},
		{"disabled channel", fmt.Sprintf(`{"model":"voice-enrollment","target_model":"%s","mode":"design","channel_id":%d,"voice_prompt":"低沉磁性男声","preview_text":"这一段台词用来试听音色的效果如何。"}`, testTTSModel, disabled.ID), 503, "model_unavailable"},
		{"channel without audio capability", fmt.Sprintf(`{"model":"voice-enrollment","target_model":"%s","mode":"design","channel_id":%d,"voice_prompt":"低沉磁性男声","preview_text":"这一段台词用来试听音色的效果如何。"}`, testTTSModel, chatOnly.ID), 400, "invalid_request"},
		{"model not mapped on channel", fmt.Sprintf(`{"model":"other-tts","target_model":"%s","mode":"design","channel_id":%d,"voice_prompt":"低沉磁性男声","preview_text":"这一段台词用来试听音色的效果如何。"}`, testTTSModel, ch.ID), 404, "model_not_found"},
		{"bad mode", fmt.Sprintf(`{"model":"voice-enrollment","mode":"speak","channel_id":%d,"voice_prompt":"低沉磁性男声","preview_text":"这一段台词用来试听音色的效果如何。"}`, ch.ID), 400, "invalid_request"},
		{"design without voice_prompt", fmt.Sprintf(`{"model":"voice-enrollment","mode":"design","channel_id":%d,"preview_text":"这一段台词用来试听音色的效果如何。"}`, ch.ID), 400, "invalid_request"},
		{"design with a clone audio", fmt.Sprintf(`{"model":"voice-enrollment","mode":"design","channel_id":%d,"voice_prompt":"低沉磁性男声","audio":"https://example.com/s.wav","preview_text":"这一段台词用来试听音色的效果如何。"}`, ch.ID), 400, "invalid_request"},
		{"clone without audio", fmt.Sprintf(`{"model":"voice-enrollment","mode":"clone","channel_id":%d,"preview_text":"这一段台词用来试听音色的效果如何。"}`, ch.ID), 400, "invalid_request"},
		{"clone with inline data uri", fmt.Sprintf(`{"model":"voice-enrollment","target_model":"%s","mode":"clone","channel_id":%d,"audio":"data:audio/wav;base64,AAAA","preview_text":"这一段台词用来试听音色的效果如何。"}`, testTTSModel, ch.ID), 400, "audio_inline_unsupported"},
		{"clone with non-http url", fmt.Sprintf(`{"model":"voice-enrollment","mode":"clone","channel_id":%d,"audio":"ftp://example.com/s.wav","preview_text":"这一段台词用来试听音色的效果如何。"}`, ch.ID), 400, "invalid_request"},
		{"preview_text too short", fmt.Sprintf(`{"model":"voice-enrollment","mode":"design","channel_id":%d,"voice_prompt":"低沉磁性男声","preview_text":"太短了"}`, ch.ID), 400, "invalid_request"},
		{"preview_text too long", fmt.Sprintf(`{"model":"voice-enrollment","mode":"design","channel_id":%d,"voice_prompt":"低沉磁性男声","preview_text":"%s"}`, ch.ID, strings.Repeat("长", 201)), 400, "invalid_request"},
		{"voice_prompt over 500", fmt.Sprintf(`{"model":"voice-enrollment","mode":"design","channel_id":%d,"voice_prompt":"%s","preview_text":"这一段台词用来试听音色的效果如何。"}`, ch.ID, strings.Repeat("描", 501)), 400, "invalid_request"},
		{"prefix too long", fmt.Sprintf(`{"model":"voice-enrollment","mode":"design","channel_id":%d,"voice_prompt":"低沉磁性男声","preview_text":"这一段台词用来试听音色的效果如何。","prefix":"%s"}`, ch.ID, strings.Repeat("a", 11)), 400, "invalid_request"},
		{"prefix non-alphanumeric", fmt.Sprintf(`{"model":"voice-enrollment","mode":"design","channel_id":%d,"voice_prompt":"低沉磁性男声","preview_text":"这一段台词用来试听音色的效果如何。","prefix":"zn-56"}`, ch.ID), 400, "invalid_request"},
		{"unpriced enrollment", `{"model":"voice-enrollment","target_model":"` + testTTSModel + `","mode":"design","channel_id":9999,"voice_prompt":"低沉磁性男声","preview_text":"这一段台词用来试听音色的效果如何。"}`, 404, "channel_not_found"},
		{"not json", `{invalid`, 400, "invalid_request"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := env.postPath(t, full, "/v1/audio/voices", tc.body)
			if w.Code != tc.wantStatus {
				t.Fatalf("status = %d body %s, want %d", w.Code, w.Body.String(), tc.wantStatus)
			}
			eb := decodeOpenAIError(t, w.Body.Bytes())
			if eb.Error.Code != tc.wantCode || eb.Error.Param != nil {
				t.Errorf("error object = %+v, want code %s", eb.Error, tc.wantCode)
			}
		})
	}

	// 未配价的 enrollment(渠道映射了但没有价格行)同样 model_not_priced。
	unpriced, err := env.stores.channels.Create(context.Background(), channel.Channel{
		Name: "unpriced", Type: channel.TypeDashScope, BaseURL: vendor.server.URL,
		APIKey: "k", ModelMap: map[string]string{"no-price-voice": "voice-enrollment"},
		Capabilities: []channel.Capability{channel.CapAudio}, Enabled: true,
	})
	if err != nil {
		t.Fatalf("seed unpriced channel: %v", err)
	}
	w := env.postPath(t, full, "/v1/audio/voices", fmt.Sprintf(`{"model":"no-price-voice","target_model":"%s","mode":"design","channel_id":%d,"voice_prompt":"低沉磁性男声","preview_text":"这一段台词用来试听音色的效果如何。"}`, testTTSModel, unpriced.ID))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("unpriced status = %d body %s, want 400", w.Code, w.Body.String())
	}
	if eb := decodeOpenAIError(t, w.Body.Bytes()); eb.Error.Code != "model_not_priced" {
		t.Errorf("unpriced code = %q, want model_not_priced", eb.Error.Code)
	}

	// 轨道不匹配:enrollment 配成字符轨 → model_not_priced。
	if _, err := env.stores.prices.Upsert(context.Background(), pricing.Price{
		PublicModel: "char-priced-voice", Unit: pricing.UnitCharacter,
		Character: &pricing.CharacterPrice{CNYPer10KCharsMicros: 1_400_000},
	}); err != nil {
		t.Fatalf("seed character price: %v", err)
	}
	charCh, err := env.stores.channels.Create(context.Background(), channel.Channel{
		Name: "char-priced", Type: channel.TypeDashScope, BaseURL: vendor.server.URL,
		APIKey: "k", ModelMap: map[string]string{"char-priced-voice": "voice-enrollment"},
		Capabilities: []channel.Capability{channel.CapAudio}, Enabled: true,
	})
	if err != nil {
		t.Fatalf("seed char-priced channel: %v", err)
	}
	w = env.postPath(t, full, "/v1/audio/voices", fmt.Sprintf(`{"model":"char-priced-voice","target_model":"%s","mode":"design","channel_id":%d,"voice_prompt":"低沉磁性男声","preview_text":"这一段台词用来试听音色的效果如何。"}`, testTTSModel, charCh.ID))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("wrong-track status = %d body %s, want 400", w.Code, w.Body.String())
	}
	if eb := decodeOpenAIError(t, w.Body.Bytes()); eb.Error.Code != "model_not_priced" {
		t.Errorf("wrong-track code = %q, want model_not_priced", eb.Error.Code)
	}

	// 全部拒绝:上游零调用,余额与用量零痕迹。
	if vendor.callCount() != 0 {
		t.Errorf("upstream called %d times, want 0", vendor.callCount())
	}
}

// TestRelayVoiceUpstreamFailureRefundsAndBreaks:钉单渠道不换道 —— 上游
// 临时失败记熔断账并直接失败(预扣原退 + upstream_error 留痕透传);连续
// 失败达阈值后电路打开,后续请求 503 且不再拨上游。
func TestRelayVoiceUpstreamFailureRefundsAndBreaks(t *testing.T) {
	env := newRelayEnv(t, nil)
	vendor := newFakeDashScopeTTS(t)
	vendor.script(http.StatusInternalServerError, "InternalError", "upstream exploded")
	ch := env.seedAudioChannel(t, "bailian-audio", vendor.server.URL)
	key, full := env.seedKey(t, 5_000_000)
	env.seedEnrollmentPrice(t)

	for i := 0; i < 3; i++ {
		w := env.postPath(t, full, "/v1/audio/voices", fmt.Sprintf(designBody, ch.ID))
		if w.Code != http.StatusInternalServerError {
			t.Fatalf("attempt %d status = %d body %s, want upstream 500 passthrough", i, w.Code, w.Body.String())
		}
		eb := decodeOpenAIError(t, w.Body.Bytes())
		if eb.Error.Code != "upstream_error" || !strings.Contains(eb.Error.Message, "upstream exploded") {
			t.Errorf("attempt %d error = %+v, want upstream_error carrying the vendor message", i, eb.Error)
		}
	}
	if balance := env.balanceOf(t, key.ID); balance != 5_000_000 {
		t.Errorf("balance = %d, want fully refunded after 3 failures", balance)
	}
	rows := env.usageRows(t)
	if len(rows) != 3 {
		t.Fatalf("usage rows = %d, want one per failed attempt", len(rows))
	}
	for _, row := range rows {
		if row.Status != usage.StatusUpstreamError || row.ChargeMicros != 0 ||
			!strings.Contains(row.UpstreamError, "upstream exploded") {
			t.Errorf("failure trail = %+v, want upstream_error with vendor summary and zero charge", row)
		}
	}

	// 阈值(3)已到:第 4 笔 503 拒之门外,不拨上游、不动账、不落行。
	w := env.postPath(t, full, "/v1/audio/voices", fmt.Sprintf(designBody, ch.ID))
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("circuit-open status = %d body %s, want 503", w.Code, w.Body.String())
	}
	if eb := decodeOpenAIError(t, w.Body.Bytes()); eb.Error.Code != "model_unavailable" {
		t.Errorf("circuit-open code = %q, want model_unavailable", eb.Error.Code)
	}
	if vendor.callCount() != 3 {
		t.Errorf("upstream calls = %d, want exactly 3 (the 4th never dials)", vendor.callCount())
	}
	if rows2 := env.usageRows(t); len(rows2) != 3 {
		t.Errorf("usage rows = %d, want still 3", len(rows2))
	}
	if entries := env.quotaLog(t, key.ID); len(entries) != 7 { // initial + 3×(estimate + refund)
		t.Errorf("ledger = %d entries, want initial + 3×(estimate, refund)", len(entries))
	}
}

// TestRelayVoiceUpstream4xxPassesThroughWithoutBreaker:上游 4xx 是请求侧
// 问题 —— 透传状态与摘要、退款留痕,不记熔断账(下一笔照常拨上游)。
func TestRelayVoiceUpstream4xxPassesThroughWithoutBreaker(t *testing.T) {
	env := newRelayEnv(t, nil)
	vendor := newFakeDashScopeTTS(t)
	vendor.script(http.StatusBadRequest, "InvalidParameter", "sensitive description rejected")
	ch := env.seedAudioChannel(t, "bailian-audio", vendor.server.URL)
	key, full := env.seedKey(t, 5_000_000)
	env.seedEnrollmentPrice(t)

	w := env.postPath(t, full, "/v1/audio/voices", fmt.Sprintf(designBody, ch.ID))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d body %s, want upstream 400 passthrough", w.Code, w.Body.String())
	}
	eb := decodeOpenAIError(t, w.Body.Bytes())
	if eb.Error.Code != "upstream_error" || !strings.Contains(eb.Error.Message, "sensitive description rejected") {
		t.Errorf("error = %+v, want upstream_error with the vendor message", eb.Error)
	}
	if balance := env.balanceOf(t, key.ID); balance != 5_000_000 {
		t.Errorf("balance = %d, want fully refunded", balance)
	}

	// 不记熔断账:厂商恢复后下一笔照常拨通。
	vendor.script(0, "", "")
	w = env.postPath(t, full, "/v1/audio/voices", fmt.Sprintf(designBody, ch.ID))
	if w.Code != http.StatusOK {
		t.Fatalf("status after recovery = %d body %s, want 200 (4xx never opened the circuit)", w.Code, w.Body.String())
	}
	if balance := env.balanceOf(t, key.ID); balance != 5_000_000-10_000 {
		t.Errorf("balance = %d, want one settled enrollment charge", balance)
	}
}

// TestRelaySpeechEndToEnd pins the speech face: 字符数服务端自算
// (text+instruction,汉字与全角计 2)、预扣即实结、usage 行量列存字符数、
// 快照带 request.chars 与单价。
func TestRelaySpeechEndToEnd(t *testing.T) {
	env := newRelayEnv(t, nil)
	vendor := newFakeDashScopeTTS(t)
	ch := env.seedAudioChannel(t, "bailian-audio", vendor.server.URL)
	key, full := env.seedKey(t, 5_000_000)
	env.seedSpeechPrice(t)

	// text「你好,世界」= 4 汉字×2 + 全角逗×2 = 10;instruction「带笑意」
	// = 3×2 = 6;合计 16 字符 → ⌈16 × 1.4 元 / 10000⌉ = 2240 micros。
	body := fmt.Sprintf(`{"model":%q,"voice":%q,"text":"你好\uFF0C世界","instruction":"带笑意","channel_id":%d}`,
		testTTSModel, testVoiceID, ch.ID)
	w := env.postPath(t, full, "/v1/audio/speech", body)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d body %s, want 200", w.Code, w.Body.String())
	}
	var got struct {
		Audio struct {
			URL string `json:"url"`
		} `json:"audio"`
		Usage struct {
			Chars int64 `json:"chars"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("response not JSON: %v (%s)", err, w.Body.String())
	}
	if got.Audio.URL == "" || got.Usage.Chars != 16 {
		t.Fatalf("response = %s, want {audio:{url}, usage:{chars:16}}", w.Body.String())
	}

	method, path, auth, async, up := vendor.last()
	if method != http.MethodPost || path != "/api/v1/services/audio/tts/SpeechSynthesizer" {
		t.Fatalf("upstream saw %s %s, want POST the SpeechSynthesizer path", method, path)
	}
	if async != "" {
		t.Errorf("X-DashScope-Async = %q, want absent (同步面)", async)
	}
	if auth != "Bearer sk-bailian-secret" {
		t.Errorf("auth = %q, want the channel's Bearer key", auth)
	}
	var sent struct {
		Model string `json:"model"`
		Input struct {
			Text        string `json:"text"`
			Voice       string `json:"voice"`
			Instruction string `json:"instruction"`
		} `json:"input"`
		Parameters struct {
			LanguageHints []string `json:"language_hints"`
		} `json:"parameters"`
		ChannelID int64 `json:"channel_id"`
	}
	if err := json.Unmarshal(up, &sent); err != nil {
		t.Fatalf("upstream body not JSON: %v (%s)", err, up)
	}
	if sent.Model != testTTSModel || sent.Input.Text != "你好\uFF0C世界" || sent.Input.Voice != testVoiceID ||
		sent.Input.Instruction != "带笑意" {
		t.Errorf("upstream body = %s, want the input-三段 translation", up)
	}
	if len(sent.Parameters.LanguageHints) != 1 || sent.Parameters.LanguageHints[0] != "zh" {
		t.Errorf("language_hints = %v, want [zh]", sent.Parameters.LanguageHints)
	}
	if sent.ChannelID != 0 {
		t.Errorf("channel_id = %d, want stripped from the upstream body", sent.ChannelID)
	}

	// 账务:预扣即实结(2240 micros,零差额无 settle 流水)。
	if balance := env.balanceOf(t, key.ID); balance != 5_000_000-2240 {
		t.Errorf("balance = %d, want 2240 micros charged (16 chars at 1.4/万)", balance)
	}
	entries := env.quotaLog(t, key.ID)
	if len(entries) != 2 || entries[0].Reason != "estimate" || entries[1].Reason != "initial" {
		t.Fatalf("ledger = %+v, want initial + estimate only", entries)
	}
	rows := env.usageRows(t)
	if len(rows) != 1 {
		t.Fatalf("usage rows = %d, want 1", len(rows))
	}
	row := rows[0]
	if row.Unit != "character" || row.CompletionTokens != 16 || row.ChargeMicros != 2240 ||
		row.Status != usage.StatusSuccess || row.ChannelID != ch.ID {
		t.Errorf("usage row = %+v, want a character-track success row with 16 billed chars", row)
	}
	var snapshot struct {
		Unit      string `json:"unit"`
		Character *struct {
			CNYPer10KCharsMicros int64 `json:"cny_per_10k_chars_micros"`
		} `json:"character"`
		Request struct {
			Chars int64 `json:"chars"`
		} `json:"request"`
	}
	if err := json.Unmarshal(row.PriceSnapshot, &snapshot); err != nil {
		t.Fatalf("snapshot not JSON: %v (%s)", err, row.PriceSnapshot)
	}
	if snapshot.Unit != "character" || snapshot.Request.Chars != 16 ||
		snapshot.Character == nil || snapshot.Character.CNYPer10KCharsMicros != 1_400_000 {
		t.Errorf("snapshot = %s, want the character snapshot with request.chars 16", row.PriceSnapshot)
	}
}

// TestRelaySpeechValidation:长度上限与校验在拨上游与计费之前发生。
func TestRelaySpeechValidation(t *testing.T) {
	env := newRelayEnv(t, nil)
	vendor := newFakeDashScopeTTS(t)
	ch := env.seedAudioChannel(t, "bailian-audio", vendor.server.URL)
	_, full := env.seedKey(t, 5_000_000)
	env.seedSpeechPrice(t)

	for _, tc := range []struct {
		name       string
		body       string
		wantStatus int
		wantCode   string
	}{
		{"missing model", `{"voice":"v","text":"你好","channel_id":1}`, 400, "missing_model"},
		{"missing voice", fmt.Sprintf(`{"model":%q,"text":"你好","channel_id":%d}`, testTTSModel, ch.ID), 400, "invalid_request"},
		{"missing channel_id", fmt.Sprintf(`{"model":%q,"voice":"v","text":"你好"}`, testTTSModel), 400, "invalid_request"},
		{"empty text", fmt.Sprintf(`{"model":%q,"voice":"v","text":"","channel_id":%d}`, testTTSModel, ch.ID), 400, "invalid_request"},
		{"text over 600", fmt.Sprintf(`{"model":%q,"voice":"v","text":"%s","channel_id":%d}`, testTTSModel, strings.Repeat("长", 601), ch.ID), 400, "invalid_request"},
		{"unknown channel", fmt.Sprintf(`{"model":%q,"voice":"v","text":"你好","channel_id":9999}`, testTTSModel), 404, "channel_not_found"},
		{"model not mapped", fmt.Sprintf(`{"model":"no-such-tts","voice":"v","text":"你好","channel_id":%d}`, ch.ID), 404, "model_not_found"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := env.postPath(t, full, "/v1/audio/speech", tc.body)
			if w.Code != tc.wantStatus {
				t.Fatalf("status = %d body %s, want %d", w.Code, w.Body.String(), tc.wantStatus)
			}
			eb := decodeOpenAIError(t, w.Body.Bytes())
			if eb.Error.Code != tc.wantCode {
				t.Errorf("code = %q, want %s", eb.Error.Code, tc.wantCode)
			}
		})
	}

	// 恰好 600 字可以过(边界):不真拨也没关系,这里厂商开着。
	w := env.postPath(t, full, "/v1/audio/speech",
		fmt.Sprintf(`{"model":%q,"voice":"v","text":"%s","channel_id":%d}`, testTTSModel, strings.Repeat("长", 600), ch.ID))
	if w.Code != http.StatusOK {
		t.Fatalf("boundary-600 status = %d body %s, want 200", w.Code, w.Body.String())
	}
	if vendor.callCount() != 1 {
		t.Errorf("upstream calls = %d, want 1", vendor.callCount())
	}
}

// TestRelaySpeechFailureRefunds:上游 5xx 与「2xx 但没有产物地址」都全额
// 退款并留痕(预扣即实结的失败半边)。
func TestRelaySpeechFailureRefunds(t *testing.T) {
	env := newRelayEnv(t, nil)
	vendor := newFakeDashScopeTTS(t)
	vendor.script(http.StatusServiceUnavailable, "Throttling", "too many requests")
	ch := env.seedAudioChannel(t, "bailian-audio", vendor.server.URL)
	key, full := env.seedKey(t, 5_000_000)
	env.seedSpeechPrice(t)

	speak := func() string {
		return fmt.Sprintf(`{"model":%q,"voice":"v","text":"你好","channel_id":%d}`, testTTSModel, ch.ID)
	}
	w := env.postPath(t, full, "/v1/audio/speech", speak())
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d body %s, want upstream 503 passthrough", w.Code, w.Body.String())
	}
	eb := decodeOpenAIError(t, w.Body.Bytes())
	if eb.Error.Code != "upstream_error" || !strings.Contains(eb.Error.Message, "too many requests") {
		t.Errorf("error = %+v, want upstream_error with the vendor message", eb.Error)
	}
	if balance := env.balanceOf(t, key.ID); balance != 5_000_000 {
		t.Errorf("balance = %d, want fully refunded", balance)
	}
	if rows := env.usageRows(t); len(rows) != 1 || rows[0].Status != usage.StatusUpstreamError {
		t.Fatalf("usage rows = %+v, want one upstream_error row", rows)
	}

	// 2xx 却没有 audio.url:上游坏形状,同路失败退款。
	vendor.script(0, "", "")
	vendor.mu.Lock()
	vendor.speechBody = `{"request_id":"rq-4","output":{}}`
	vendor.mu.Unlock()
	w = env.postPath(t, full, "/v1/audio/speech", speak())
	if w.Code != http.StatusBadGateway {
		t.Fatalf("no-audio status = %d body %s, want 502", w.Code, w.Body.String())
	}
	if balance := env.balanceOf(t, key.ID); balance != 5_000_000 {
		t.Errorf("balance = %d, want fully refunded after the undeliverable 2xx", balance)
	}
	if rows := env.usageRows(t); len(rows) != 2 {
		t.Fatalf("usage rows = %d, want 2", len(rows))
	}
}

// TestRelayVoiceQueryAndDeleteNoBilling:管理动作 —— 不计费、不落用量行;
// 上游看到 query_voice / delete_voice 的 action 体。
func TestRelayVoiceQueryAndDeleteNoBilling(t *testing.T) {
	env := newRelayEnv(t, nil)
	vendor := newFakeDashScopeTTS(t)
	vendor.voiceStatus = "OK"
	ch := env.seedAudioChannel(t, "bailian-audio", vendor.server.URL)
	key, full := env.seedKey(t, 5_000_000)
	env.seedEnrollmentPrice(t) // 配价也不该被计

	w := env.postPath(t, full, "/v1/audio/voices/"+testVoiceID+"/query", fmt.Sprintf(`{"channel_id":%d}`, ch.ID))
	if w.Code != http.StatusOK {
		t.Fatalf("query status = %d body %s, want 200", w.Code, w.Body.String())
	}
	var got struct {
		VoiceID string `json:"voice_id"`
		Status  string `json:"status"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("query response not JSON: %v (%s)", err, w.Body.String())
	}
	if got.VoiceID != testVoiceID || got.Status != "OK" {
		t.Errorf("query response = %+v, want the normalized voice object", got)
	}

	w = env.deletePath(t, full, "/v1/audio/voices/"+testVoiceID, fmt.Sprintf(`{"channel_id":%d}`, ch.ID))
	if w.Code != http.StatusNoContent {
		t.Fatalf("delete status = %d body %s, want 204", w.Code, w.Body.String())
	}
	_, _, _, _, body := vendor.last()
	var sent struct {
		Action  string `json:"action"`
		VoiceID string `json:"voice_id"`
	}
	if err := json.Unmarshal(body, &sent); err != nil || sent.Action != "delete_voice" || sent.VoiceID != testVoiceID {
		t.Fatalf("delete body = %s (%v), want delete_voice carrying the voice id", body, err)
	}

	// 再查一次:最近一次 action 是 query_voice。
	w = env.postPath(t, full, "/v1/audio/voices/"+testVoiceID+"/query", fmt.Sprintf(`{"channel_id":%d}`, ch.ID))
	if w.Code != http.StatusOK {
		t.Fatalf("second query status = %d", w.Code)
	}
	_, _, _, _, body = vendor.last()
	var sent2 struct {
		Action string `json:"action"`
	}
	if err := json.Unmarshal(body, &sent2); err != nil || sent2.Action != "query_voice" {
		t.Errorf("last action = %q (%v), want query_voice", sent2.Action, err)
	}

	if rows := env.usageRows(t); len(rows) != 0 {
		t.Errorf("usage rows = %d, want 0 (管理动作不落行)", len(rows))
	}
	if entries := env.quotaLog(t, key.ID); len(entries) != 1 {
		t.Errorf("ledger = %d entries, want initial only", len(entries))
	}
}
