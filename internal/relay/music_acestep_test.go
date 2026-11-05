package relay_test

// 80 号票(同曲换词通路二落,ACE-Step 1.5 自托管 cover 任务)网关契约的
// 端到端测试:acestep 渠道勾 music 能力即被 /v1/audio/music 调度;三个
// 形状 sentinel(audio_ref 非空歌词空 / audio_ref 空 / 参数形状非法)与
// 上传链失败 sentinel 都是请求级 400 invalid_request_error(不换道、不记
// 熔断账、预扣原退、0 用量行,79 号票 mureka 同构);正向链 = 下载参考
// 字节 → POST /release_task multipart(task_type=cover、src_audio 文件
// 字段、lyrics、prompt 非空才送、audio_cover_strength 携带才送)→
// POST /query_result 轮询(status 整数 0/1/2;result 是 JSON 字符串须二次
// unmarshal,file 相对路径拼 BaseURL)。429(队列满)/415 → 合成可换道
// 502;data 空 → 暂态 502。prompt 选填豁免谓词 = mureka ∪ acestep。
// fake vendor 有「下载/提交/查询」三个面。共享 relay_test.go 的 MySQL
// 环境,基础设施不可达时自动 skip。

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
)

const acestepTaskID = "acestep-task-1"

// fakeAcestepVendor 是 acestep-api 厂商替身,三个面:GET /ref/master.mp3
// (参考下载)、POST /release_task(multipart 提交回信封 {data:[{task_id}],
// code:200})、POST /query_result(轮询回 {data:[{task_id,status整数,
// result:JSON字符串}],code:200})。剧本在轮询间隙改动(mu 保护);
// requests 计数是「拨上游」的总闸 —— sentinel 拒绝必须零命中。
type fakeAcestepVendor struct {
	server *httptest.Server
	mu     sync.Mutex

	refBytes []byte // /ref/master.mp3 的字节(下载链输入)

	releaseStatus int    // release_task 回答码(0 = 200)
	releaseBody   string // 提交应答覆盖(如信封无 task_id / 400 detail)

	queryStatus int    // query_result 回答码(0 = 200)
	queryBody   string // 查询应答整体覆盖(data 空等形状)
	queryStatusInt int // data[0].status 的整数(0/1/2)
	queryResult any    // result 数组的载荷(编码成 JSON 字符串落进信封)
	queryErr    any    // 信封 error(对象 {"message":…} 或字符串)

	requests   int
	releases   int
	queries    int
	refFetches int

	// multipart 提交事实(task_type/字段名/文件字节量/禁送键)。
	lastCT          string
	lastAuth        string
	lastTaskType    string
	lastLyrics      string
	lastPromptVal   string
	lastPromptOK    bool
	lastStrengthVal string
	lastStrengthOK  bool
	lastFileLen     int
	extraKeys       []string // 携带的非白名单表单键(禁送键断言用)
	queryBodies     []string
}

func newFakeAcestepVendor(t *testing.T) *fakeAcestepVendor {
	t.Helper()
	v := &fakeAcestepVendor{refBytes: murekaMP3Blob(100)}
	v.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		v.mu.Lock()
		defer v.mu.Unlock()
		v.requests++
		v.lastAuth = r.Header.Get("Authorization")

		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/ref/master.mp3":
			v.refFetches++
			w.Header().Set("Content-Type", "audio/mpeg")
			w.Write(v.refBytes)

		case r.Method == http.MethodPost && r.URL.Path == "/release_task":
			v.releases++
			if v.releaseStatus != 0 && v.releaseStatus != http.StatusOK {
				w.WriteHeader(v.releaseStatus)
				w.Write([]byte(`{"detail":"queue is full"}`))
				return
			}
			v.lastCT = r.Header.Get("Content-Type")
			if err := r.ParseMultipartForm(64 << 20); err != nil {
				t.Errorf("release_task multipart parse: %v", err)
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			v.lastTaskType = r.FormValue("task_type")
			v.lastLyrics = r.FormValue("lyrics")
			v.lastPromptVal = r.FormValue("prompt")
			_, v.lastPromptOK = r.MultipartForm.Value["prompt"]
			v.lastStrengthVal = r.FormValue("audio_cover_strength")
			_, v.lastStrengthOK = r.MultipartForm.Value["audio_cover_strength"]
			if parts := r.MultipartForm.File["src_audio"]; len(parts) == 1 {
				fh, oerr := parts[0].Open()
				if oerr == nil {
					fh.Close()
				}
				v.lastFileLen = int(parts[0].Size)
			}
			for k := range r.MultipartForm.Value {
				switch k {
				case "task_type", "lyrics", "prompt", "audio_cover_strength":
				default:
					v.extraKeys = append(v.extraKeys, k)
				}
			}
			w.Header().Set("Content-Type", "application/json")
			if v.releaseBody != "" {
				w.Write([]byte(v.releaseBody))
				return
			}
			w.Write([]byte(`{"data":[{"task_id":"` + acestepTaskID + `","status":0}],"code":200}`))

		case r.Method == http.MethodPost && r.URL.Path == "/query_result":
			v.queries++
			raw, _ := io.ReadAll(r.Body)
			v.queryBodies = append(v.queryBodies, string(raw))
			if v.queryStatus != 0 && v.queryStatus != http.StatusOK {
				w.WriteHeader(v.queryStatus)
				w.Write([]byte(`{"detail":"queue is full"}`))
				return
			}
			w.Header().Set("Content-Type", "application/json")
			if v.queryBody != "" {
				w.Write([]byte(v.queryBody))
				return
			}
			answer := map[string]any{
				"data": []any{map[string]any{
					"task_id": acestepTaskID,
					"status":  v.queryStatusInt,
				}},
				"code": 200,
			}
			if v.queryResult != nil {
				payload, _ := json.Marshal(v.queryResult)
				answer["data"].([]any)[0].(map[string]any)["result"] = string(payload)
			}
			if v.queryErr != nil {
				answer["error"] = v.queryErr
			}
			json.NewEncoder(w).Encode(answer)

		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(v.server.Close)
	return v
}

func (v *fakeAcestepVendor) scriptQuery(statusInt int, result any, queryErr any) {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.queryStatus = 0
	v.queryBody = ""
	v.queryStatusInt = statusInt
	v.queryResult = result
	v.queryErr = queryErr
}

func (v *fakeAcestepVendor) scriptQueryBody(status int, body string) {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.queryStatus = status
	v.queryBody = body
}

func (v *fakeAcestepVendor) scriptRelease(status int, body string) {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.releaseStatus = status
	v.releaseBody = body
}

func (v *fakeAcestepVendor) setRefBytes(b []byte) {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.refBytes = b
}

func (v *fakeAcestepVendor) dialCount() int {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.requests
}

func (v *fakeAcestepVendor) releaseCount() int {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.releases
}

func (v *fakeAcestepVendor) lastQueryBody() string {
	v.mu.Lock()
	defer v.mu.Unlock()
	if len(v.queryBodies) == 0 {
		return ""
	}
	return v.queryBodies[len(v.queryBodies)-1]
}

// acestepSubmitFacts reports the most recent release_task hop(multipart 面:
// Content-Type、Bearer、表单字段值与在场性、文件字节量、白名单外键)。
func (v *fakeAcestepVendor) acestepSubmitFacts() (ct, auth, taskType, lyrics string,
	promptVal string, promptOK bool, strengthVal string, strengthOK bool, fileLen int, extraKeys []string) {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.lastCT, v.lastAuth, v.lastTaskType, v.lastLyrics,
		v.lastPromptVal, v.lastPromptOK, v.lastStrengthVal, v.lastStrengthOK, v.lastFileLen, v.extraKeys
}

// seedAcestepChannel inserts one enabled acestep channel (music 能力,带
// Bearer key)whose HTTP endpoints are the stub。
func (e *relayEnv) seedAcestepChannel(t *testing.T, baseURL, publicModel, upstreamModel string) channel.Channel {
	t.Helper()
	ch, err := e.stores.channels.Create(context.Background(), channel.Channel{
		Name: "acestep-stub", Type: channel.TypeAcestep, BaseURL: baseURL,
		APIKey:       "acestep-secret-key",
		ModelMap:     map[string]string{publicModel: upstreamModel},
		Capabilities: []channel.Capability{channel.CapMusic},
		Enabled:      true,
	})
	if err != nil {
		t.Fatalf("seed acestep channel: %v", err)
	}
	return ch
}

// seedAcestepChannelPrio seeds with explicit priority(换道测试用:高优先级
// 渠道先被调度)and an explicit key(空串 = 匿名渠道,合法形态)。
func (e *relayEnv) seedAcestepChannelPrio(t *testing.T, name, baseURL, publicModel, upstreamModel string, priority int, apiKey string) channel.Channel {
	t.Helper()
	ch, err := e.stores.channels.Create(context.Background(), channel.Channel{
		Name: name, Type: channel.TypeAcestep, BaseURL: baseURL,
		APIKey:       apiKey,
		ModelMap:     map[string]string{publicModel: upstreamModel},
		Capabilities: []channel.Capability{channel.CapMusic},
		Priority:     priority,
		Enabled:      true,
	})
	if err != nil {
		t.Fatalf("seed acestep channel %s: %v", name, err)
	}
	return ch
}

func acestepRef(v *fakeAcestepVendor) string { return v.server.URL + "/ref/master.mp3" }

// 形状 sentinel 1:audio_ref 非空而歌词空 = 请求级 400(cover 任务词是
// 身份必需)—— 不拨上游、预扣原退、0 用量行、不记熔断账。
func TestRelayAcestepMusicRequiresLyrics(t *testing.T) {
	env := newRelayEnv(t, nil)
	vendor := newFakeAcestepVendor(t)
	env.seedAcestepChannel(t, vendor.server.URL, "acestep-relyric", "acestep-v1.5")
	key, full := env.seedKey(t, 2_000_000)
	env.seedMusicPrice(t, "acestep-relyric")

	w := env.postMusic(t, full,
		`{"model":"acestep-relyric","prompt":"city pop","instrumental":false,"audio_ref":"`+acestepRef(vendor)+`"}`)
	if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "invalid_request_error") {
		t.Fatalf("status = %d body %s, want 400 invalid_request_error", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "歌词必填") {
		t.Errorf("body = %s, want the lyrics-required copy", w.Body.String())
	}
	if dials := vendor.dialCount(); dials != 0 {
		t.Errorf("vendor dials = %d, want 0 (refused at the adaptor seam)", dials)
	}
	if balance := env.balanceOf(t, key.ID); balance != 2_000_000 {
		t.Errorf("balance = %d, want the reserve refunded whole", balance)
	}
	var rows int
	if err := env.stores.db.QueryRowContext(context.Background(),
		`SELECT COUNT(*) FROM usage_logs`).Scan(&rows); err != nil {
		t.Fatalf("count usage rows: %v", err)
	}
	if rows != 0 {
		t.Errorf("usage rows = %d, want 0 (refusal before any upstream dial)", rows)
	}

	// 不记熔断账:同一渠道随后的正常 relyric 请求照常受理。
	w = env.postMusic(t, full,
		`{"model":"acestep-relyric","prompt":"city pop","instrumental":false,"lyrics":"夜色慢慢降落","audio_ref":"`+acestepRef(vendor)+`"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("follow-up relyric submit status = %d body %s, want 200", w.Code, w.Body.String())
	}
	if vendor.dialCount() == 0 {
		t.Errorf("vendor dials = %d, want >0 (channel not breaker-tripped)", vendor.dialCount())
	}
}

// 形状 sentinel 2:audio_ref 为空的普通音乐请求 = 请求级 400,文案指路
// 普通音乐公开名 —— 同款断言集。
func TestRelayAcestepMusicRejectsPlainGeneration(t *testing.T) {
	env := newRelayEnv(t, nil)
	vendor := newFakeAcestepVendor(t)
	env.seedAcestepChannel(t, vendor.server.URL, "acestep-relyric", "acestep-v1.5")
	key, full := env.seedKey(t, 2_000_000)
	env.seedMusicPrice(t, "acestep-relyric")

	w := env.postMusic(t, full, `{"model":"acestep-relyric","prompt":"纯音乐","instrumental":true}`)
	if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "invalid_request_error") {
		t.Fatalf("status = %d body %s, want 400 invalid_request_error", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "普通音乐") {
		t.Errorf("body = %s, want the plain-music guidance copy", w.Body.String())
	}
	if dials := vendor.dialCount(); dials != 0 {
		t.Errorf("vendor dials = %d, want 0 (refused at the adaptor seam)", dials)
	}
	if balance := env.balanceOf(t, key.ID); balance != 2_000_000 {
		t.Errorf("balance = %d, want the reserve refunded whole", balance)
	}

	// 不记熔断账:正常 relyric 请求照常受理。
	w = env.postMusic(t, full,
		`{"model":"acestep-relyric","prompt":"city pop","instrumental":false,"lyrics":"夜色慢慢降落","audio_ref":"`+acestepRef(vendor)+`"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("follow-up relyric submit status = %d body %s, want 200", w.Code, w.Body.String())
	}
	if vendor.dialCount() == 0 {
		t.Errorf("vendor dials = %d, want >0 (channel not breaker-tripped)", vendor.dialCount())
	}
}

// 正向受理全链:下载参考字节(1 次)→ POST /release_task multipart
// (task_type=cover、src_audio 文件字段、lyrics、prompt 透传、
// audio_cover_strength %g 文本;model/vocal_language/instrumental 等禁送
// 键不出现)→ 信封取 task_id → 轮询 status=0 翻 running、status=1 +
// result 双层 JSON 的 file 相对路径拼 BaseURL 交付 → 按次预扣恒等结清。
func TestRelayAcestepMusicCoverSubmitEndToEnd(t *testing.T) {
	env := newRelayEnv(t, nil)
	vendor := newFakeAcestepVendor(t)
	env.seedAcestepChannel(t, vendor.server.URL, "acestep-relyric", "acestep-v1.5")
	key, full := env.seedKey(t, 2_000_000)
	env.seedMusicPrice(t, "acestep-relyric")
	vendor.scriptQuery(0, nil, nil)

	w := env.postMusic(t, full,
		`{"model":"acestep-relyric","prompt":"city pop","instrumental":false,"lyrics":"夜色慢慢降落","audio_ref":"`+acestepRef(vendor)+`","cover_strength":0.55}`)
	if w.Code != http.StatusOK {
		t.Fatalf("submit status = %d body %s, want 200", w.Code, w.Body.String())
	}
	id, _ := decodeMusicTask(t, w.Body.Bytes())["id"].(string)
	if !strings.HasPrefix(id, "mt_") {
		t.Fatalf("id = %v, want an mt_ gateway id", id)
	}

	// multipart 面:task_type=cover、字段名、参考字节整段、强度文本。
	ct, auth, taskType, lyrics, promptVal, _, strengthVal, _, fileLen, extraKeys := vendor.acestepSubmitFacts()
	if !strings.Contains(ct, "multipart/form-data") {
		t.Errorf("content-type = %q, want multipart/form-data", ct)
	}
	if auth != "Bearer acestep-secret-key" {
		t.Errorf("auth = %q, want the Bearer key", auth)
	}
	if taskType != "cover" {
		t.Errorf("task_type = %q, want cover", taskType)
	}
	if lyrics != "夜色慢慢降落" {
		t.Errorf("lyrics = %q, want the full new lyrics verbatim", lyrics)
	}
	if promptVal != "city pop" {
		t.Errorf("prompt = %q, want the style prompt passthrough", promptVal)
	}
	if strengthVal != "0.55" {
		t.Errorf("audio_cover_strength = %q, want 0.55 as %g text", strengthVal, 0.55)
	}
	if fileLen != len(murekaMP3Blob(100)) {
		t.Errorf("src_audio file bytes = %d, want the downloaded reference whole (%d)", fileLen, len(murekaMP3Blob(100)))
	}
	for _, leaked := range extraKeys {
		if strings.Contains(leaked, "model") || strings.Contains(leaked, "vocal_language") ||
			strings.Contains(leaked, "instrumental") || strings.Contains(leaked, "duration") {
			t.Errorf("release body carries forbidden key %q (票面定案 4:不透传)", leaked)
		}
	}

	// 账务:按次预扣照旧(同曲换词不改变计价面)。
	if balance := env.balanceOf(t, key.ID); balance != 1_800_000 {
		t.Fatalf("balance after submit = %d, want 1_800_000 held", balance)
	}

	// 轮询 1:status=0(queued/running 档)→ running;顺带核对轮询体形状
	// {"task_id_list":["<id>"]}。
	w = env.getMusicTask(t, full, id)
	if body := vendor.lastQueryBody(); !strings.Contains(body, `"task_id_list":["`+acestepTaskID+`"]`) {
		t.Errorf("query body = %s, want the task_id_list array form", body)
	}
	if got := decodeMusicTask(t, w.Body.Bytes()); got["status"] != "running" {
		t.Errorf("status 0 merged to %v, want running", got["status"])
	}

	// 轮询 2:status=1 + result 双层 JSON → succeeded,file 相对路径拼
	// BaseURL 成完整产物地址。
	vendor.scriptQuery(1, []map[string]any{{"file": "/v1/audio?path=out.mp3", "status": "done"}}, nil)
	w = env.getMusicTask(t, full, id)
	if w.Code != http.StatusOK {
		t.Fatalf("final poll status = %d body %s", w.Code, w.Body.String())
	}
	done := decodeMusicTask(t, w.Body.Bytes())
	if done["status"] != "succeeded" {
		t.Fatalf("final = %v, want succeeded", done["status"])
	}
	audio, _ := done["audio"].(map[string]any)
	if audio == nil || audio["url"] != vendor.server.URL+"/v1/audio?path=out.mp3" {
		t.Errorf("audio = %v, want the relative file joined onto the BaseURL", done["audio"])
	}
	if balance := env.balanceOf(t, key.ID); balance != 1_800_000 {
		t.Errorf("balance after success = %d, want 1_800_000 (reserve became the charge)", balance)
	}
}

// 状态映射与失败原因:status=2 → failed,信封 error 的失败文案进任务行
// error 并全额退款留痕。
func TestRelayAcestepMusicStatusFailed(t *testing.T) {
	env := newRelayEnv(t, nil)
	vendor := newFakeAcestepVendor(t)
	env.seedAcestepChannel(t, vendor.server.URL, "acestep-relyric", "acestep-v1.5")
	key, full := env.seedKey(t, 2_000_000)
	env.seedMusicPrice(t, "acestep-relyric")

	w := env.postMusic(t, full,
		`{"model":"acestep-relyric","prompt":"city pop","instrumental":false,"lyrics":"夜色慢慢降落","audio_ref":"`+acestepRef(vendor)+`"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("submit status = %d body %s, want 200", w.Code, w.Body.String())
	}
	id, _ := decodeMusicTask(t, w.Body.Bytes())["id"].(string)

	vendor.scriptQuery(2, nil, map[string]any{"message": "vocal separation failed"})
	w = env.getMusicTask(t, full, id)
	if w.Code != http.StatusOK {
		t.Fatalf("poll status = %d body %s", w.Code, w.Body.String())
	}
	got := decodeMusicTask(t, w.Body.Bytes())
	if got["status"] != "failed" {
		t.Fatalf("status 2 merged to %v, want failed", got["status"])
	}
	if !strings.Contains(w.Body.String(), "vocal separation failed") {
		t.Errorf("body = %s, want the vendor failure copy", w.Body.String())
	}
	if balance := env.balanceOf(t, key.ID); balance != 2_000_000 {
		t.Errorf("balance = %d, want the reserve refunded on failure", balance)
	}
}

// 提交 429(队列满)= 上游临时失败:合成可换道 502,预扣原封带往下一
// 渠道 —— 高优先级渠道 429、低优先级渠道受理,公开名同挂两渠道。
func TestRelayAcestepMusicQueueFullFailover(t *testing.T) {
	env := newRelayEnv(t, nil)
	busy := newFakeAcestepVendor(t)
	healthy := newFakeAcestepVendor(t)
	env.seedAcestepChannelPrio(t, "acestep-busy", busy.server.URL, "acestep-relyric", "acestep-v1.5", 10, "k1")
	env.seedAcestepChannelPrio(t, "acestep-healthy", healthy.server.URL, "acestep-relyric", "acestep-v1.5", 0, "")
	key, full := env.seedKey(t, 2_000_000)
	env.seedMusicPrice(t, "acestep-relyric")
	busy.scriptRelease(http.StatusTooManyRequests, "")

	w := env.postMusic(t, full,
		`{"model":"acestep-relyric","prompt":"city pop","instrumental":false,"lyrics":"夜色慢慢降落","audio_ref":"`+acestepRef(busy)+`"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("failover submit status = %d body %s, want 200 pinned on backup", w.Code, w.Body.String())
	}
	if busy.releaseCount() != 1 || healthy.releaseCount() != 1 {
		t.Fatalf("releases = (busy %d, healthy %d), want one attempt on each", busy.releaseCount(), healthy.releaseCount())
	}
	// 匿名渠道(无 key)不发 Authorization 头。
	if _, auth, _, _, _, _, _, _, _, _ := healthy.acestepSubmitFacts(); auth != "" {
		t.Errorf("keyless channel auth = %q, want no Authorization header", auth)
	}
	if balance := env.balanceOf(t, key.ID); balance != 1_800_000 {
		t.Errorf("balance = %d, want 1_800_000 held on the backup channel", balance)
	}
}

// 查询面暂态故障:data 空(任务不在)与 429 都不推进任务 —— 502 形如实
// 应答,任务行原地不动、账不动,随后正常查询照常推进。
func TestRelayAcestepMusicQueryTransient(t *testing.T) {
	env := newRelayEnv(t, nil)
	vendor := newFakeAcestepVendor(t)
	env.seedAcestepChannel(t, vendor.server.URL, "acestep-relyric", "acestep-v1.5")
	key, full := env.seedKey(t, 2_000_000)
	env.seedMusicPrice(t, "acestep-relyric")

	w := env.postMusic(t, full,
		`{"model":"acestep-relyric","prompt":"city pop","instrumental":false,"lyrics":"夜色慢慢降落","audio_ref":"`+acestepRef(vendor)+`"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("submit status = %d body %s, want 200", w.Code, w.Body.String())
	}
	id, _ := decodeMusicTask(t, w.Body.Bytes())["id"].(string)

	t.Run("data 空=暂态 502", func(t *testing.T) {
		vendor.scriptQueryBody(http.StatusOK, `{"data":[],"code":200}`)
		w := env.getMusicTask(t, full, id)
		if w.Code != http.StatusBadGateway || !strings.Contains(w.Body.String(), "upstream_error") {
			t.Fatalf("empty-data poll = %d %s, want 502 upstream_error", w.Code, w.Body.String())
		}
		if got := decodeMusicTaskFromPoll(t, w); got != "" && got != "queued" && got != "running" {
			t.Errorf("poll body status = %q, want no terminal advance", got)
		}
	})
	t.Run("查询 429=如实报错不动账", func(t *testing.T) {
		vendor.scriptQueryBody(http.StatusTooManyRequests, `{"detail":"queue is full"}`)
		w := env.getMusicTask(t, full, id)
		if w.Code != http.StatusTooManyRequests {
			t.Fatalf("429 poll = %d %s, want the upstream status verbatim", w.Code, w.Body.String())
		}
	})
	if balance := env.balanceOf(t, key.ID); balance != 1_800_000 {
		t.Errorf("balance = %d, want the reserve untouched by transient polls", balance)
	}
	// 恢复后照常推进:status=1 + file → succeeded。
	vendor.scriptQuery(1, []map[string]any{{"file": "/v1/audio?path=recovered.mp3"}}, nil)
	w = env.getMusicTask(t, full, id)
	got := decodeMusicTask(t, w.Body.Bytes())
	if got["status"] != "succeeded" {
		t.Fatalf("post-transient poll = %v, want succeeded", got["status"])
	}
}

// decodeMusicTaskFromPoll reads the status word off a poll response body
// (transient 502 应答是 OpenAI error object,不是任务对象)。
func decodeMusicTaskFromPoll(t *testing.T, w *httptest.ResponseRecorder) string {
	t.Helper()
	var body struct {
		Status string `json:"status"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		return ""
	}
	return body.Status
}

// prompt 选填豁免谓词扩展(80 号票定案 6):候选类型集合从「全 mureka」
// 扩为 mureka ∪ acestep —— 全 acestep 候选同样放行 promptless relyric
// (提交体不带 prompt 键);全 mureka 回归既有行为;混入非豁免类型(vod)
// 豁免不生效照旧 400。relyric 形状缺词的网关前置校验对两通路同一文案。
func TestRelayAcestepMusicPromptExemptPredicate(t *testing.T) {
	t.Run("全 acestep=promptless 受理", func(t *testing.T) {
		env := newRelayEnv(t, nil)
		vendor := newFakeAcestepVendor(t)
		env.seedAcestepChannel(t, vendor.server.URL, "acestep-relyric", "acestep-v1.5")
		_, full := env.seedKey(t, 2_000_000)
		env.seedMusicPrice(t, "acestep-relyric")

		w := env.postMusic(t, full,
			`{"model":"acestep-relyric","instrumental":false,"lyrics":"第二首的词","audio_ref":"`+acestepRef(vendor)+`"}`)
		if w.Code != http.StatusOK {
			t.Fatalf("promptless relyric submit status = %d body %s, want 200", w.Code, w.Body.String())
		}
		if _, _, _, _, promptVal, promptOK, _, _, _, _ := vendor.acestepSubmitFacts(); promptOK || promptVal != "" {
			t.Errorf("promptless submit carries prompt key (value %q, present %v)", promptVal, promptOK)
		}
	})
	t.Run("全 acestep 缺词=歌词文案而非 prompt 文案", func(t *testing.T) {
		env := newRelayEnv(t, nil)
		vendor := newFakeAcestepVendor(t)
		env.seedAcestepChannel(t, vendor.server.URL, "acestep-relyric", "acestep-v1.5")
		_, full := env.seedKey(t, 2_000_000)
		env.seedMusicPrice(t, "acestep-relyric")

		w := env.postMusic(t, full,
			`{"model":"acestep-relyric","instrumental":false,"audio_ref":"`+acestepRef(vendor)+`"}`)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("status = %d body %s, want 400", w.Code, w.Body.String())
		}
		if strings.Contains(w.Body.String(), "'prompt'") || !strings.Contains(w.Body.String(), "歌词必填") {
			t.Errorf("body = %s, want the lyrics copy not the prompt copy", w.Body.String())
		}
		if dials := vendor.dialCount(); dials != 0 {
			t.Errorf("vendor dials = %d, want 0 (refused before dialing)", dials)
		}
	})
	t.Run("混合 vod 候选=豁免不生效", func(t *testing.T) {
		env := newRelayEnv(t, nil)
		vendor := newFakeAcestepVendor(t)
		env.seedAcestepChannel(t, vendor.server.URL, "acestep-relyric", "acestep-v1.5")
		mps := newFakeMPSVendor(t)
		env.seedMPSChannelPrio(t, mps.server.URL, "acestep-relyric", "MiniMaxMusic 3.0", 0)
		_, full := env.seedKey(t, 2_000_000)
		env.seedMusicPrice(t, "acestep-relyric")
		env.seedMusicPrice(t, "MiniMaxMusic 3.0")

		w := env.postMusic(t, full,
			`{"model":"acestep-relyric","instrumental":false,"lyrics":"x","audio_ref":"`+acestepRef(vendor)+`"}`)
		if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "'prompt' parameter") {
			t.Fatalf("mixed-candidate promptless status = %d body %s, want 400 missing-prompt", w.Code, w.Body.String())
		}
	})
}

// cover_strength 网关校验(80 号票定案 5):携带时 0–1 之外 400
// invalid_request_error(拨号前,预扣不动);不携带 = 不透传;边界 0/1
// 合法;合法值以 %g 文本落 multipart。
func TestRelayAcestepMusicCoverStrengthGuard(t *testing.T) {
	cases := []struct {
		name string
		raw  string
	}{
		{"负数", `{"model":"acestep-relyric","prompt":"p","instrumental":false,"lyrics":"x","audio_ref":"REF","cover_strength":-0.1}`},
		{"大于 1", `{"model":"acestep-relyric","prompt":"p","instrumental":false,"lyrics":"x","audio_ref":"REF","cover_strength":1.1}`},
		{"浮点上溢(+Inf)", `{"model":"acestep-relyric","prompt":"p","instrumental":false,"lyrics":"x","audio_ref":"REF","cover_strength":1e999}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			env := newRelayEnv(t, nil)
			vendor := newFakeAcestepVendor(t)
			env.seedAcestepChannel(t, vendor.server.URL, "acestep-relyric", "acestep-v1.5")
			key, full := env.seedKey(t, 2_000_000)
			env.seedMusicPrice(t, "acestep-relyric")

			body := strings.Replace(tc.raw, "REF", acestepRef(vendor), 1)
			w := env.postMusic(t, full, body)
			if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "invalid_request_error") {
				t.Fatalf("status = %d body %s, want 400 invalid_request_error", w.Code, w.Body.String())
			}
			if dials := vendor.dialCount(); dials != 0 {
				t.Errorf("vendor dials = %d, want 0 (refused before dialing)", dials)
			}
			if balance := env.balanceOf(t, key.ID); balance != 2_000_000 {
				t.Errorf("balance = %d, want the reserve untouched", balance)
			}
		})
	}

	t.Run("不携带=multipart 无 audio_cover_strength 键", func(t *testing.T) {
		env := newRelayEnv(t, nil)
		vendor := newFakeAcestepVendor(t)
		env.seedAcestepChannel(t, vendor.server.URL, "acestep-relyric", "acestep-v1.5")
		_, full := env.seedKey(t, 2_000_000)
		env.seedMusicPrice(t, "acestep-relyric")

		w := env.postMusic(t, full,
			`{"model":"acestep-relyric","prompt":"p","instrumental":false,"lyrics":"夜色","audio_ref":"`+acestepRef(vendor)+`"}`)
		if w.Code != http.StatusOK {
			t.Fatalf("submit status = %d body %s, want 200", w.Code, w.Body.String())
		}
		if _, _, _, _, _, _, strengthVal, strengthOK, _, _ := vendor.acestepSubmitFacts(); strengthOK || strengthVal != "" {
			t.Errorf("strengthless submit carries audio_cover_strength (value %q, present %v)", strengthVal, strengthOK)
		}
	})

	t.Run("边界 0 与 1 合法且按 %g 文本落 multipart", func(t *testing.T) {
		for _, tc := range []struct {
			json string
			text string
		}{{`"cover_strength":0`, "0"}, {`"cover_strength":1`, "1"}} {
			env := newRelayEnv(t, nil)
			vendor := newFakeAcestepVendor(t)
			env.seedAcestepChannel(t, vendor.server.URL, "acestep-relyric", "acestep-v1.5")
			_, full := env.seedKey(t, 2_000_000)
			env.seedMusicPrice(t, "acestep-relyric")

			body := `{"model":"acestep-relyric","prompt":"p","instrumental":false,"lyrics":"夜色","audio_ref":"` +
				acestepRef(vendor) + `",` + tc.json + `}`
			w := env.postMusic(t, full, body)
			if w.Code != http.StatusOK {
				t.Fatalf("%s submit status = %d body %s, want 200", tc.json, w.Code, w.Body.String())
			}
			if _, _, _, _, _, _, strengthVal, strengthOK, _, _ := vendor.acestepSubmitFacts(); !strengthOK || strengthVal != tc.text {
				t.Errorf("%s → strength (%q, present %v), want (%q, true)", tc.json, strengthVal, strengthOK, tc.text)
			}
		}
	})
}
