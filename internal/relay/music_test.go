package relay_test

// 60 号票(配乐异步任务契约)的端到端测试:POST /v1/audio/music 受理回
// {id,status}、GET /v1/audio/music/tasks/{id} 轮询五态与 audio.url;
// 按次计费「仅成功计费」—— 失败预扣全额退回,任务行与用量日志可查;
// 计价目录 /v1/audio/music-models 只含 music 能力渠道 ∩ call 配价。走
// 真实 MPS adaptor(TC3 签名对本地桩,桩不校签),上游态词表用 MPS 原文
// (WAIT/RUN/DONE/FAIL)验证归并。共享 relay_test.go 的 MySQL 环境,
// 基础设施不可达时自动 skip。

import (
	"context"
	"encoding/json"
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
)

// fakeMPSVendor 是异步音乐厂商替身:同一内存服务器按剧本回答提交与轮询。
// status/url/errMsg 由测试在轮询间隙改动(mu 保护)模拟任务生命周期;
// 原始状态串(WAIT/RUN/DONE/FAIL)原样下发,归并由网关做。
type fakeMPSVendor struct {
	server *httptest.Server
	mu     sync.Mutex

	status       string
	url          string
	errMsg       string
	submitStatus int
	submitBody   string

	submits int
	queries int
	bodies  []string
}

func newFakeMPSVendor(t *testing.T) *fakeMPSVendor {
	t.Helper()
	v := &fakeMPSVendor{}
	v.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		v.mu.Lock()
		defer v.mu.Unlock()
		raw, _ := io.ReadAll(r.Body)
		switch r.Header.Get("X-TC-Action") {
		case "CreateAigcAudioTask":
			v.submits++
			v.bodies = append(v.bodies, string(raw))
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
			w.Write([]byte(`{"Response":{"TaskId":"mps-task-1"}}`))
		case "DescribeAigcAudioTask":
			v.queries++
			w.Header().Set("Content-Type", "application/json")
			answer := map[string]any{"Status": v.status}
			if v.url != "" {
				answer["AudioInfos"] = []any{map[string]any{"Url": v.url, "Duration": 183}}
			}
			if v.errMsg != "" {
				answer["Message"] = v.errMsg
			}
			json.NewEncoder(w).Encode(map[string]any{"Response": answer})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(v.server.Close)
	return v
}

// script sets the query endpoint's next answer(原始状态、产物、失败原因).
func (v *fakeMPSVendor) script(status, url, errMsg string) {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.status, v.url, v.errMsg = status, url, errMsg
}

func (v *fakeMPSVendor) counts() (submits, queries int) {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.submits, v.queries
}

// lastSubmitBody returns the most recent CreateAigcAudioTask body the stub saw.
func (v *fakeMPSVendor) lastSubmitBody() string {
	v.mu.Lock()
	defer v.mu.Unlock()
	if len(v.bodies) == 0 {
		return ""
	}
	return v.bodies[len(v.bodies)-1]
}

// seedMPSChannel inserts one enabled tencent-mps channel (music 能力) whose
// TC3 endpoint is the stub.
func (e *relayEnv) seedMPSChannel(t *testing.T, baseURL, publicModel, upstreamModel string) channel.Channel {
	t.Helper()
	return e.seedMPSChannelPrio(t, baseURL, publicModel, upstreamModel, 0)
}

func (e *relayEnv) seedMPSChannelPrio(t *testing.T, baseURL, publicModel, upstreamModel string, priority int) channel.Channel {
	t.Helper()
	ch, err := e.stores.channels.Create(context.Background(), channel.Channel{
		Name: "mps-stub", Type: channel.TypeTencentMps, BaseURL: baseURL,
		Config: map[string]string{
			"secret_id": "AKIDtest", "secret_key": "mpstestkey",
			"bucket": "music-1250000000", "region": "ap-guangzhou",
		},
		ModelMap:     map[string]string{publicModel: upstreamModel},
		Capabilities: []channel.Capability{channel.CapMusic},
		Priority:     priority,
		Enabled:      true,
	})
	if err != nil {
		t.Fatalf("seed mps channel: %v", err)
	}
	return ch
}

// seedMusicPrice writes the call-track test price: ¥0.20/次。
func (e *relayEnv) seedMusicPrice(t *testing.T, publicModel string) {
	t.Helper()
	_, err := e.stores.prices.Upsert(context.Background(), pricing.Price{
		PublicModel: publicModel, Unit: pricing.UnitCall,
		Call: &pricing.CallPrice{CNYPerCallMicros: 200_000},
	})
	if err != nil {
		t.Fatalf("seed music price: %v", err)
	}
}

func (e *relayEnv) postMusic(t *testing.T, fullKey, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/v1/audio/music", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+fullKey)
	w := httptest.NewRecorder()
	e.engine.ServeHTTP(w, req)
	return w
}

func (e *relayEnv) getMusicTask(t *testing.T, fullKey, id string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/v1/audio/music/tasks/"+id, nil)
	req.Header.Set("Authorization", "Bearer "+fullKey)
	w := httptest.NewRecorder()
	e.engine.ServeHTTP(w, req)
	return w
}

func decodeMusicTask(t *testing.T, body []byte) map[string]any {
	t.Helper()
	var parsed map[string]any
	if err := json.Unmarshal(body, &parsed); err != nil {
		t.Fatalf("music task body: %v (%s)", err, body)
	}
	return parsed
}

func TestRelayMusicSubmitPollSucceedEndToEnd(t *testing.T) {
	env := newRelayEnv(t, nil)
	vendor := newFakeMPSVendor(t)
	vendor.script("WAIT", "", "")
	env.seedMPSChannel(t, vendor.server.URL, "bgm-m", "MiniMaxMusic 3.0")
	key, full := env.seedKey(t, 2_000_000)
	env.seedMusicPrice(t, "bgm-m")

	// 提交:拿到网关任务句柄,状态 queued。
	w := env.postMusic(t, full, `{"model":"bgm-m","prompt":"轻快的电子爵士"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("submit status = %d body %s, want 200", w.Code, w.Body.String())
	}
	submitted := decodeMusicTask(t, w.Body.Bytes())
	id, _ := submitted["id"].(string)
	if !strings.HasPrefix(id, "mt_") {
		t.Fatalf("id = %v, want an mt_ gateway id", submitted["id"])
	}
	if submitted["status"] != "queued" || submitted["model"] != "bgm-m" {
		t.Errorf("submitted = %v, want queued with echoed model", submitted)
	}

	// 上游收到 MPS 请求:模型串已映射、SceneType/输出桶在体内。
	if body := vendor.lastSubmitBody(); !strings.Contains(body, `"ModelName":"MiniMaxMusic"`) ||
		!strings.Contains(body, `"ModelVersion":"3.0"`) ||
		!strings.Contains(body, `"SceneType":"music"`) ||
		!strings.Contains(body, `"CosBucketName":"music-1250000000"`) ||
		!strings.Contains(body, `is_instrumental`) {
		t.Errorf("mps submit body = %s, want translated CreateAigcAudioTask", body)
	}

	// 账务:按次预扣 ¥0.20,余额 = 2_000_000 − 200_000。
	if balance := env.balanceOf(t, key.ID); balance != 1_800_000 {
		t.Fatalf("balance after submit = %d, want 1_800_000 held", balance)
	}

	// 轮询 1:WAIT 归并 queued。
	w = env.getMusicTask(t, full, id)
	if got := decodeMusicTask(t, w.Body.Bytes()); got["status"] != "queued" {
		t.Errorf("WAIT merged to %v, want queued", got["status"])
	}

	// 轮询 2:RUN → running。
	vendor.script("RUN", "", "")
	w = env.getMusicTask(t, full, id)
	if got := decodeMusicTask(t, w.Body.Bytes()); got["status"] != "running" {
		t.Errorf("RUN merged to %v, want running", got["status"])
	}

	// 轮询 3:DONE + 产物 → succeeded,拿到 audio.url。
	vendor.script("DONE", "https://music.cos.ap-guangzhou.myqcloud.com/a.mp3", "")
	w = env.getMusicTask(t, full, id)
	if w.Code != http.StatusOK {
		t.Fatalf("final poll status = %d body %s, want 200", w.Code, w.Body.String())
	}
	done := decodeMusicTask(t, w.Body.Bytes())
	if done["status"] != "succeeded" {
		t.Fatalf("final = %v, want succeeded", done["status"])
	}
	audio, _ := done["audio"].(map[string]any)
	if audio == nil || audio["url"] != "https://music.cos.ap-guangzhou.myqcloud.com/a.mp3" {
		t.Errorf("audio = %v, want the vendor URL under audio.url", done["audio"])
	}

	// 账务:实扣即预扣(差额为零不落 settle 流水)。
	if balance := env.balanceOf(t, key.ID); balance != 1_800_000 {
		t.Errorf("balance after success = %d, want 1_800_000 (reserve became the charge)", balance)
	}
	if entries := env.quotaLog(t, key.ID); len(entries) != 2 || entries[0].Reason != apikey.ReasonEstimate {
		t.Fatalf("ledger = %+v, want initial + estimate only", entries)
	}

	// 任务行:终态、产物、扣费定格(经 store 直查)。
	row, err := env.stores.musicTasks.Get(context.Background(), id)
	if err != nil || row.Status != "succeeded" || row.AudioURL == "" || row.ChargeMicros != 200_000 {
		t.Errorf("task row = %+v err %v, want succeeded with url and 200_000 charge", row, err)
	}

	// 留痕:unit=call、成功、扣 200_000。
	rows := env.usageRows(t)
	if len(rows) != 1 {
		t.Fatalf("usage rows = %d, want 1", len(rows))
	}
	trail := rows[0]
	if trail.Unit != "call" || trail.Status != usage.StatusSuccess || trail.ChargeMicros != 200_000 ||
		trail.PublicModel != "bgm-m" {
		t.Errorf("trail = %+v, want a call-track success row", trail)
	}

	// 终态后再轮询:直接答账本事实,上游零打扰。
	_, queries := vendor.counts()
	w = env.getMusicTask(t, full, id)
	if got := decodeMusicTask(t, w.Body.Bytes()); got["status"] != "succeeded" {
		t.Errorf("terminal re-poll = %v, want stored succeeded", got["status"])
	}
	if _, queries2 := vendor.counts(); queries2 != queries {
		t.Errorf("terminal re-poll hit the vendor (%d → %d), want none", queries, queries2)
	}
}

func TestRelayMusicFailRefundsAndTrails(t *testing.T) {
	env := newRelayEnv(t, nil)
	vendor := newFakeMPSVendor(t)
	vendor.script("FAIL", "", "tme audio url is empty")
	env.seedMPSChannel(t, vendor.server.URL, "bgm-m", "Mureka instrumental_9.5")
	key, full := env.seedKey(t, 2_000_000)
	env.seedMusicPrice(t, "bgm-m")

	w := env.postMusic(t, full, `{"model":"bgm-m","prompt":"低沉的大提琴"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("submit status = %d", w.Code)
	}
	id, _ := decodeMusicTask(t, w.Body.Bytes())["id"].(string)
	if balance := env.balanceOf(t, key.ID); balance != 1_800_000 {
		t.Fatalf("balance after submit = %d, want 1_800_000 held", balance)
	}

	w = env.getMusicTask(t, full, id)
	if got := decodeMusicTask(t, w.Body.Bytes()); got["status"] != "failed" {
		t.Fatalf("FAIL merged to %v, want failed", got["status"])
	}
	// 失败全额退款 + 原因留任务行 + upstream_error 留痕。
	if balance := env.balanceOf(t, key.ID); balance != 2_000_000 {
		t.Errorf("balance after failure = %d, want refunded to 2_000_000", balance)
	}
	row, err := env.stores.musicTasks.Get(context.Background(), id)
	if err != nil || row.Status != "failed" || !strings.Contains(row.Error, "tme audio url is empty") {
		t.Errorf("task row = %+v err %v, want failed with the vendor reason", row, err)
	}
	rows := env.usageRows(t)
	if len(rows) != 1 || rows[0].Status != usage.StatusUpstreamError || rows[0].ChargeMicros != 0 {
		t.Errorf("trail = %+v, want one upstream_error row with zero charge", rows)
	}
}

func TestRelayMusicSubmitFailoverPinsBackup(t *testing.T) {
	env := newRelayEnv(t, nil)
	dead := newFakeMPSVendor(t)
	dead.submitStatus = http.StatusInternalServerError
	alive := newFakeMPSVendor(t)
	// 主渠道(高优先级,恒先试)500:预扣原封带往下一渠道,受理钉在活
	// 渠道上。
	env.seedMPSChannelPrio(t, dead.server.URL, "bgm-m", "MiniMaxMusic 3.0", 10)
	env.seedMPSChannelPrio(t, alive.server.URL, "bgm-m", "MiniMaxMusic 2.5", 0)
	_, full := env.seedKey(t, 2_000_000)
	env.seedMusicPrice(t, "bgm-m")

	// 主渠道 500:预扣原封带往下一渠道,受理钉在活渠道上。
	w := env.postMusic(t, full, `{"model":"bgm-m","prompt":"夜雨"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("submit status = %d body %s, want 200 via failover", w.Code, w.Body.String())
	}
	if submits, _ := dead.counts(); submits != 1 {
		t.Errorf("dead vendor submits = %d, want 1", submits)
	}
	if submits, _ := alive.counts(); submits != 1 {
		t.Errorf("alive vendor submits = %d, want 1", submits)
	}
	if body := alive.lastSubmitBody(); !strings.Contains(body, `"ModelVersion":"2.5"`) {
		t.Errorf("alive vendor body = %s, want the backup channel's upstream model", body)
	}
}

func TestRelayMusicSubmitGuards(t *testing.T) {
	env := newRelayEnv(t, nil)
	vendor := newFakeMPSVendor(t)
	env.seedMPSChannel(t, vendor.server.URL, "bgm-m", "MiniMaxMusic 3.0")
	key, full := env.seedKey(t, 2_000_000)

	// 未配价:model_not_priced,不拨上游。
	w := env.postMusic(t, full, `{"model":"bgm-m","prompt":"p"}`)
	if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "model_not_priced") {
		t.Errorf("unpriced status = %d body %s, want 400 model_not_priced", w.Code, w.Body.String())
	}
	// 计价轨错位(配了 second 轨):同码拒绝。
	env.stores.prices.Upsert(context.Background(), pricing.Price{
		PublicModel: "bgm-m", Unit: pricing.UnitSecond,
		Call: &pricing.CallPrice{CNYPerCallMicros: 100_000},
	})
	w = env.postMusic(t, full, `{"model":"bgm-m","prompt":"p"}`)
	if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "model_not_priced") {
		t.Errorf("second-track status = %d body %s, want 400 model_not_priced", w.Code, w.Body.String())
	}

	// 超上限风格描述(MPS 2000 字):400。
	long := strings.Repeat("雨", 2001)
	w = env.postMusic(t, full, `{"model":"bgm-m","prompt":"`+long+`"}`)
	if w.Code != http.StatusBadRequest {
		t.Errorf("long prompt status = %d, want 400", w.Code)
	}

	// 换正确配价后能走通(守卫不是永久封死)。
	env.seedMusicPrice(t, "bgm-m")
	w = env.postMusic(t, full, `{"model":"bgm-m","prompt":"p"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("priced submit status = %d body %s, want 200", w.Code, w.Body.String())
	}
	if balance := env.balanceOf(t, key.ID); balance != 1_800_000 {
		t.Errorf("balance = %d, want the per-call hold", balance)
	}
}

func TestRelayMusicModelsCatalogIntersectsCallPricing(t *testing.T) {
	env := newRelayEnv(t, nil)
	vendor := newFakeMPSVendor(t)
	env.seedMPSChannel(t, vendor.server.URL, "bgm-call", "MiniMaxMusic 3.0")
	// music 渠道映射但只有 character 配价的模型:不进目录。
	env.seedMPSChannel(t, vendor.server.URL, "bgm-char", "GL")
	env.stores.prices.Upsert(context.Background(), pricing.Price{
		PublicModel: "bgm-char", Unit: pricing.UnitCharacter,
		Character: &pricing.CharacterPrice{CNYPer10KCharsMicros: 20_000_000},
	})
	env.seedMusicPrice(t, "bgm-call")

	_, full := env.seedKey(t, 0)
	req := httptest.NewRequest(http.MethodGet, "/v1/audio/music-models", nil)
	req.Header.Set("Authorization", "Bearer "+full)
	w := httptest.NewRecorder()
	env.engine.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("catalog status = %d body %s", w.Code, w.Body.String())
	}
	var parsed struct {
		Models []string                        `json:"models"`
		Prices map[string]pricing.PriceSummary `json:"prices"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &parsed); err != nil {
		t.Fatalf("catalog body: %v", err)
	}
	if len(parsed.Models) != 1 || parsed.Models[0] != "bgm-call" {
		t.Errorf("models = %v, want only the call-priced music model", parsed.Models)
	}
	if _, ok := parsed.Prices["bgm-call"]; !ok {
		t.Errorf("prices = %v, want the price summary attached", parsed.Prices)
	}
}
