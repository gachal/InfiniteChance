package relay_test

// 62 号票(歌曲生成,VOD AIGC 聚合渠道)的端到端测试:tencent-vod 渠道勾
// music 能力即被 /v1/audio/music 调度;instrumental/lyrics 随原始 body 透传
// 给 adaptor 翻译成 CreateAigcAudioTask;轮询走 DescribeTaskDetail 的
// AigcAudioTask 子对象,状态词(WAITING/SUCCESS……)由 adaptor 翻成 MPS 词
// 表后归并。互斥校验:instrumental=true 携带 lyrics 在拨号之前 400。走真实
// VOD adaptor(TC3 签名对本地桩,桩不校签),共享 relay_test.go 的 MySQL
// 环境,基础设施不可达时自动 skip。

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/gachal/InfiniteChance/internal/channel"
)

// fakeVODVendor 是 VOD AIGC 音乐厂商替身:同一内存服务器按剧本回答提交与
// 轮询;DescribeTaskDetail 的应答体由测试在轮询间隙改动(mu 保护)模拟任务
// 生命周期,状态词用 VOD 原文(WAITING/RUNNING/SUCCESS……)验证 adaptor 的
// 词表翻译。
type fakeVODVendor struct {
	server *httptest.Server
	mu     sync.Mutex

	detail string // DescribeTaskDetail 的下一拍应答体(原文)

	submits int
	queries int
	bodies  []string
}

func newFakeVODVendor(t *testing.T) *fakeVODVendor {
	t.Helper()
	v := &fakeVODVendor{}
	v.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		v.mu.Lock()
		defer v.mu.Unlock()
		raw, _ := io.ReadAll(r.Body)
		switch r.Header.Get("X-TC-Action") {
		case "CreateAigcAudioTask":
			v.submits++
			v.bodies = append(v.bodies, string(raw))
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"Response":{"TaskId":"vod-task-1"}}`))
		case "DescribeTaskDetail":
			v.queries++
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

func (v *fakeVODVendor) scriptDetail(detail string) {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.detail = detail
}

func (v *fakeVODVendor) lastSubmitBody() string {
	v.mu.Lock()
	defer v.mu.Unlock()
	if len(v.bodies) == 0 {
		return ""
	}
	return v.bodies[len(v.bodies)-1]
}

// seedVODMusicChannel inserts one enabled tencent-vod channel with the music
// capability whose TC3 endpoint is the stub —— 62 号票定案:能力过滤只看
// CapMusic,VOD 渠道勾上即可被音乐调度。
func (e *relayEnv) seedVODMusicChannel(t *testing.T, baseURL, publicModel, upstreamModel string) channel.Channel {
	t.Helper()
	ch, err := e.stores.channels.Create(context.Background(), channel.Channel{
		Name: "vod-music-stub", Type: channel.TypeTencentVod, BaseURL: baseURL,
		Config: map[string]string{
			"secret_id":  "AKIDtest",
			"secret_key": "vodtestkey",
			"sub_app_id": "1500000000",
		},
		ModelMap:     map[string]string{publicModel: upstreamModel},
		Capabilities: []channel.Capability{channel.CapMusic},
		Enabled:      true,
	})
	if err != nil {
		t.Fatalf("seed vod music channel: %v", err)
	}
	return ch
}

// 人声歌曲全链:instrumental=false + lyrics 提交受理 → VOD 收到翻译后的
// CreateAigcAudioTask(AdditionalParameters 按模型族带 is_instrumental:false
// 与 lyrics)→ 轮询 WAITING 归并 queued、SUCCESS 交付 Output URL 归并
// succeeded → 按次预扣恒等结清。
func TestRelayVODMusicSongEndToEnd(t *testing.T) {
	env := newRelayEnv(t, nil)
	vendor := newFakeVODVendor(t)
	env.seedVODMusicChannel(t, vendor.server.URL, "vod-minimax-music-3.0", "MiniMaxMusic 3.0")
	key, full := env.seedKey(t, 2_000_000)
	env.seedMusicPrice(t, "vod-minimax-music-3.0")

	w := env.postMusic(t, full,
		`{"model":"vod-minimax-music-3.0","prompt":"city night run","instrumental":false,"lyrics":"夜色慢慢降落"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("submit status = %d body %s, want 200", w.Code, w.Body.String())
	}
	id, _ := decodeMusicTask(t, w.Body.Bytes())["id"].(string)
	if !strings.HasPrefix(id, "mt_") {
		t.Fatalf("id = %v, want an mt_ gateway id", id)
	}

	// 上游收到翻译后的请求:模型串拆分、SceneType、OutputConfig、歌词经
	// AdditionalParameters 透传(is_instrumental:false 与 lyrics 同串)。
	body := vendor.lastSubmitBody()
	for _, want := range []string{
		`"ModelName":"MiniMaxMusic"`, `"ModelVersion":"3.0"`, `"SceneType":"music"`,
		`"StorageMode":"Temporary"`, `"OutputAudioFormat":"mp3"`, `"SubAppId":1500000000`,
		`"AdditionalParameters":"{\"is_instrumental\":false,\"lyrics\":\"夜色慢慢降落\"}"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("submit body missing %s:\n%s", want, body)
		}
	}

	// 账务:按次预扣照旧(歌曲不改变计价面)。
	if balance := env.balanceOf(t, key.ID); balance != 1_800_000 {
		t.Fatalf("balance after submit = %d, want 1_800_000 held", balance)
	}

	// 轮询 1:VOD 的 WAITING 翻成 WAIT → queued(直透会被词表误杀成
	// failed,62 号票定案 3 的翻译矩阵存在的原因)。
	w = env.getMusicTask(t, full, id)
	if got := decodeMusicTask(t, w.Body.Bytes()); got["status"] != "queued" {
		t.Errorf("WAITING merged to %v, want queued", got["status"])
	}

	// 轮询 2:SUCCESS + Output FileUrl → succeeded,拿到 audio.url。
	vendor.scriptDetail(`{"Response":{"AigcAudioTask":{"Status":"SUCCESS","Output":{"FileInfos":[{"FileUrl":"https://vod-temp.example/song.mp3"}]}}}}`)
	w = env.getMusicTask(t, full, id)
	if w.Code != http.StatusOK {
		t.Fatalf("final poll status = %d body %s", w.Code, w.Body.String())
	}
	done := decodeMusicTask(t, w.Body.Bytes())
	if done["status"] != "succeeded" {
		t.Fatalf("final = %v, want succeeded", done["status"])
	}
	audio, _ := done["audio"].(map[string]any)
	if audio == nil || audio["url"] != "https://vod-temp.example/song.mp3" {
		t.Errorf("audio = %v, want the VOD Output URL under audio.url", done["audio"])
	}
	if balance := env.balanceOf(t, key.ID); balance != 1_800_000 {
		t.Errorf("balance after success = %d, want 1_800_000 (reserve became the charge)", balance)
	}
}

// 互斥校验:instrumental=true(显式)携带 lyrics 在拨号之前 400
// invalid_request_error —— 显式拒绝优于静默忽略(歌词会被上游丢弃)。
func TestRelayVODMusicInstrumentalRejectsLyrics(t *testing.T) {
	env := newRelayEnv(t, nil)
	vendor := newFakeVODVendor(t)
	env.seedVODMusicChannel(t, vendor.server.URL, "vod-minimax-music-3.0", "MiniMaxMusic 3.0")
	_, full := env.seedKey(t, 2_000_000)
	env.seedMusicPrice(t, "vod-minimax-music-3.0")

	w := env.postMusic(t, full,
		`{"model":"vod-minimax-music-3.0","prompt":"x","instrumental":true,"lyrics":"多余的歌"}`)
	if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "invalid_request_error") {
		t.Fatalf("status = %d body %s, want 400 invalid_request_error", w.Code, w.Body.String())
	}
	if submits := len(vendor.bodies); submits != 0 {
		t.Errorf("vendor submits = %d, want 0 (refused before dialing)", submits)
	}

	// lyrics 超 3500 runes 同码拒绝。
	long := strings.Repeat("雨", 3501)
	w = env.postMusic(t, full, `{"model":"vod-minimax-music-3.0","prompt":"x","instrumental":false,"lyrics":"`+long+`"}`)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("long lyrics status = %d, want 400", w.Code)
	}

	// instrumental 缺省(60 号票既有调用形状)照常受理。
	w = env.postMusic(t, full, `{"model":"vod-minimax-music-3.0","prompt":"纯音乐"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("default submit status = %d body %s, want 200", w.Code, w.Body.String())
	}
}

// 互斥校验的缺省口径(评审修复):instrumental 不传语义即 true(60 号票
// 纯音乐缺省),携带 lyrics 同样在拨号之前 400 —— 与 canvas 侧
// createMusicTask(先物化后校验)及前端 validateMusicForm 三处口径一致;
// 不补这道闸,vod adaptor 会向 MiniMaxMusic 发 is_instrumental:true +
// lyrics 的矛盾体。对照面:instrumental:false + lyrics(人声歌曲)照常
// 受理。
func TestRelayVODMusicOmittedInstrumentalRejectsLyrics(t *testing.T) {
	env := newRelayEnv(t, nil)
	vendor := newFakeVODVendor(t)
	env.seedVODMusicChannel(t, vendor.server.URL, "vod-minimax-music-3.0", "MiniMaxMusic 3.0")
	_, full := env.seedKey(t, 2_000_000)
	env.seedMusicPrice(t, "vod-minimax-music-3.0")

	// instrumental 缺省(语义 true)+ 歌词:400,且不拨号。
	w := env.postMusic(t, full,
		`{"model":"vod-minimax-music-3.0","prompt":"x","lyrics":"多余的歌"}`)
	if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "invalid_request_error") {
		t.Fatalf("status = %d body %s, want 400 invalid_request_error", w.Code, w.Body.String())
	}
	if submits := len(vendor.bodies); submits != 0 {
		t.Errorf("vendor submits = %d, want 0 (refused before dialing)", submits)
	}

	// 对照面:instrumental:false + 歌词照常受理(人声歌曲)。
	w = env.postMusic(t, full,
		`{"model":"vod-minimax-music-3.0","prompt":"x","instrumental":false,"lyrics":"夜色慢慢降落"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("vocal submit status = %d body %s, want 200", w.Code, w.Body.String())
	}
}

// Mureka 只透传 lyrics(不带 is_instrumental,其纯音乐参数表达未证)。
func TestRelayVODMusicMurekaPassesLyricsOnly(t *testing.T) {
	env := newRelayEnv(t, nil)
	vendor := newFakeVODVendor(t)
	env.seedVODMusicChannel(t, vendor.server.URL, "vod-mureka-song-9", "Mureka song_9")
	_, full := env.seedKey(t, 2_000_000)
	env.seedMusicPrice(t, "vod-mureka-song-9")

	w := env.postMusic(t, full,
		`{"model":"vod-mureka-song-9","prompt":"pop ballad","instrumental":false,"lyrics":"la la la"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("submit status = %d body %s, want 200", w.Code, w.Body.String())
	}
	body := vendor.lastSubmitBody()
	if !strings.Contains(body, `"AdditionalParameters":"{\"lyrics\":\"la la la\"}"`) {
		t.Errorf("submit body = %s, want lyrics-only AdditionalParameters", body)
	}
	if strings.Contains(body, "is_instrumental") {
		t.Errorf("submit body = %s, want no is_instrumental for Mureka", body)
	}
}
