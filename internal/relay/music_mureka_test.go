package relay_test

// 79 号票(同曲换词,Mureka Song Edit)网关契约的端到端测试:mureka 渠道
// 勾 music 能力即被 /v1/audio/music 调度;audio_ref 非空 + 歌词必填(Edit
// 无自动写词)是身份必需,两个形状 sentinel 与上传链失败 sentinel 都是
// 请求级 400 invalid_request_error(不换道、不记熔断账、预扣原退、0 用量
// 行,78 号票 errAudioRefUnsupported 同构);正向链 = 下载参考字节 →
// multipart 上传取文件 id → 提交 region-edit(全曲选区:edit_start=0、
// edit_end=嗅探时长)→ 轮询状态词翻译归一。fake vendor 有「下载/上传/
// 提交/查询」四个面。共享 relay_test.go 的 MySQL 环境,基础设施不可达时
// 自动 skip。

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/gachal/InfiniteChance/internal/channel"
)

// murekaMP3Blob builds one clean MPEG-1 Layer III CBR stream:48000Hz/
// 128kbps 的帧恰 384 字节、每帧 1152 样本 = 24ms —— 嗅探期望值按 mp3 规格
// 独立可算,n 帧即 n×24ms(100 帧 = 2400ms)。
func murekaMP3Blob(frames int) []byte {
	const frameSize = 384 // 144 × 128000 / 48000 = 384,无 padding
	b := make([]byte, frameSize*frames)
	for i := 0; i < len(b); i += frameSize {
		b[i], b[i+1], b[i+2], b[i+3] = 0xFF, 0xFB, 0x94, 0x00
	}
	return b
}

// fakeMurekaVendor 是 Mureka Song Edit 厂商替身,四个面:GET
// /ref/master.mp3(参考下载)、POST /v1/files/upload(multipart 上传回
// {"id":…})、POST /v1/song/region-edit(提交回 {"task_id":…})、GET
// /v1/song/query/{id}(轮询回原始状态词与多形状产物地址)。剧本在轮询
// 间隙改动(mu 保护);requests 计数是「拨上游」的总闸 —— sentinel 拒绝
// 必须零命中。
type fakeMurekaVendor struct {
	server *httptest.Server
	mu     sync.Mutex

	refBytes     []byte // /ref/master.mp3 的字节(嗅探/上传链输入)
	refOversize  bool   // 参考下载端点流式吐出超过网关取上限的体积(chunked,无 Content-Length)
	refDeclared  int64  // >0 = 参考下载端点声明超大 Content-Length(体本身很小,走预检路径)
	uploadStatus int    // 上传端点回答码(0 = 200)
	uploadBody   string // 上传应答覆盖
	editStatus   int    // region-edit 回答码(0 = 200)
	editBody     string // 提交应答覆盖
	shape        string // 轮询产物地址形状:nested / flat / files
	status       string // 轮询状态词(Mureka 原文)
	audioURL     string // succeeded 时的产物地址
	errMsg       string // failed 时的失败原因

	requests   int
	uploads    int
	edits      int
	queries    int
	refFetches int

	lastUploadPurpose     string
	lastUploadContentType string
	lastUploadFileLen     int
	editBodies            []string
}

const murekaTaskID = "mureka-task-1"

func newFakeMurekaVendor(t *testing.T) *fakeMurekaVendor {
	t.Helper()
	v := &fakeMurekaVendor{refBytes: murekaMP3Blob(100)}
	v.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		v.mu.Lock()
		defer v.mu.Unlock()
		v.requests++

		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/ref/master.mp3":
			v.refFetches++
			w.Header().Set("Content-Type", "audio/mpeg")
			if n := v.refDeclared; n > 0 {
				// 只声明超大 Content-Length、体本身很小:网关的预检在
				// 读体之前就该拒绝,多余字节无需真传。
				w.Header().Set("Content-Length", strconv.FormatInt(n, 10))
				w.Write(v.refBytes)
				return
			}
			if v.refOversize {
				// 不在内存里造 32MiB 参考体:按 1MiB 块流式写出 33MiB,
				// chunked 无 Content-Length —— 走「读满上限再探一字节」
				// 的判定路径。
				chunk := make([]byte, 1<<20)
				for i := 0; i < 33; i++ {
					w.Write(chunk)
				}
				return
			}
			w.Write(v.refBytes)

		case r.Method == http.MethodPost && r.URL.Path == "/v1/files/upload":
			v.uploads++
			if v.uploadStatus != 0 && v.uploadStatus != http.StatusOK {
				w.WriteHeader(v.uploadStatus)
				w.Write([]byte(`{"error":{"message":"upload quota exceeded"}}`))
				return
			}
			// 表单面照官方语义核对:purpose 必填字段 + file 文件字段。
			v.lastUploadContentType = r.Header.Get("Content-Type")
			v.lastUploadPurpose = r.FormValue("purpose")
			if f, header, err := r.FormFile("file"); err == nil {
				v.lastUploadFileLen = int(header.Size)
				f.Close()
			}
			w.Header().Set("Content-Type", "application/json")
			if v.uploadBody != "" {
				w.Write([]byte(v.uploadBody))
				return
			}
			w.Write([]byte(`{"id":"mureka-file-1"}`))

		case r.Method == http.MethodPost && r.URL.Path == "/v1/song/region-edit":
			v.edits++
			raw, _ := io.ReadAll(r.Body)
			v.editBodies = append(v.editBodies, string(raw))
			if v.editStatus != 0 && v.editStatus != http.StatusOK {
				w.WriteHeader(v.editStatus)
				w.Write([]byte(`{"error":{"message":"bad request"}}`))
				return
			}
			w.Header().Set("Content-Type", "application/json")
			if v.editBody != "" {
				w.Write([]byte(v.editBody))
				return
			}
			w.Write([]byte(`{"task_id":"` + murekaTaskID + `"}`))

		case r.Method == http.MethodGet && r.URL.Path == "/v1/song/query/"+murekaTaskID:
			v.queries++
			w.Header().Set("Content-Type", "application/json")
			answer := map[string]any{"task_id": murekaTaskID, "status": v.status}
			switch v.shape {
			case "nested":
				answer["audio"] = map[string]any{"url": v.audioURL}
			case "flat":
				answer["audio_url"] = v.audioURL
			case "files":
				answer["files"] = []any{map[string]any{"url": v.audioURL, "kind": "audio"}}
			}
			if v.errMsg != "" {
				answer["error"] = map[string]any{"message": v.errMsg}
			}
			json.NewEncoder(w).Encode(answer)

		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(v.server.Close)
	return v
}

// scriptQuery sets the query endpoint's next answer(状态词原文、产物形状
// 与地址、失败原因).
func (v *fakeMurekaVendor) scriptQuery(shape, status, audioURL, errMsg string) {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.shape, v.status, v.audioURL, v.errMsg = shape, status, audioURL, errMsg
}

func (v *fakeMurekaVendor) scriptUpload(status int, body string) {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.uploadStatus, v.uploadBody = status, body
}

func (v *fakeMurekaVendor) scriptEdit(status int, body string) {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.editStatus, v.editBody = status, body
}

func (v *fakeMurekaVendor) setRefBytes(b []byte) {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.refBytes = b
}

func (v *fakeMurekaVendor) setRefOversize() {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.refOversize = true
}

func (v *fakeMurekaVendor) setRefDeclared(n int64) {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.refDeclared = n
}

func (v *fakeMurekaVendor) dialCount() int {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.requests
}

func (v *fakeMurekaVendor) lastEditBody() string {
	v.mu.Lock()
	defer v.mu.Unlock()
	if len(v.editBodies) == 0 {
		return ""
	}
	return v.editBodies[len(v.editBodies)-1]
}

// uploadFacts reports the most recent upload hop(用途字段与上传字节量).
func (v *fakeMurekaVendor) uploadFacts() (purpose string, contentType string, fileLen int) {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.lastUploadPurpose, v.lastUploadContentType, v.lastUploadFileLen
}

// seedMurekaChannel inserts one enabled mureka channel (music 能力) whose
// HTTP endpoints are the stub.
func (e *relayEnv) seedMurekaChannel(t *testing.T, baseURL, publicModel, upstreamModel string) channel.Channel {
	t.Helper()
	ch, err := e.stores.channels.Create(context.Background(), channel.Channel{
		Name: "mureka-stub", Type: channel.TypeMureka, BaseURL: baseURL,
		APIKey:       "mureka-secret-key",
		ModelMap:     map[string]string{publicModel: upstreamModel},
		Capabilities: []channel.Capability{channel.CapMusic},
		Enabled:      true,
	})
	if err != nil {
		t.Fatalf("seed mureka channel: %v", err)
	}
	return ch
}

// relayricBody is one valid relyric submit body(prompt 风格描述在此形状里
// 是可选项,测试按需拼)。
func murekaRef(v *fakeMurekaVendor) string { return v.server.URL + "/ref/master.mp3" }

// 形状 sentinel 1:audio_ref 非空而歌词空 = 请求级 400(Edit 无自动写词,
// 词是身份必需)—— 不拨上游、预扣原退、0 用量行、不记熔断账(下一拍
// 正常请求照常受理)。
func TestRelayMurekaMusicEditRequiresLyrics(t *testing.T) {
	env := newRelayEnv(t, nil)
	vendor := newFakeMurekaVendor(t)
	env.seedMurekaChannel(t, vendor.server.URL, "mureka-song-edit", "mureka-9.5")
	key, full := env.seedKey(t, 2_000_000)
	env.seedMusicPrice(t, "mureka-song-edit")

	w := env.postMusic(t, full,
		`{"model":"mureka-song-edit","prompt":"city pop","instrumental":false,"audio_ref":"`+murekaRef(vendor)+`"}`)
	if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "invalid_request_error") {
		t.Fatalf("status = %d body %s, want 400 invalid_request_error", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "歌词必填") {
		t.Errorf("body = %s, want the lyrics-required copy", w.Body.String())
	}
	// sentinel 在 adaptor 本地拒绝,厂商零命中(下载/上传/提交都没发生)。
	if dials := vendor.dialCount(); dials != 0 {
		t.Errorf("vendor dials = %d, want 0 (refused at the adaptor seam)", dials)
	}
	// 预扣原退,不留用量行。
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
		`{"model":"mureka-song-edit","prompt":"city pop","instrumental":false,"lyrics":"夜色慢慢降落","audio_ref":"`+murekaRef(vendor)+`"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("follow-up relyric submit status = %d body %s, want 200", w.Code, w.Body.String())
	}
	if vendor.dialCount() == 0 {
		t.Errorf("vendor dials = %d, want >0 (channel not breaker-tripped)", vendor.dialCount())
	}
}

// 双缺形状(prompt 与歌词都空、audio_ref 非空、候选全 mureka)= 请求级
// 400 且报**歌词**文案而非 prompt 文案(79 号票评审修正):词是身份必需、
// prompt 是选填项,错误优先级对齐票面定案 5,与 adaptor sentinel 同源;
// 拒绝发生在拨号之前,预扣不动、0 用量行。歌词过线后同一形状走 prompt
// 豁免照常受理(对照)。
func TestRelayMurekaMusicDoubleMissingReportsLyrics(t *testing.T) {
	env := newRelayEnv(t, nil)
	vendor := newFakeMurekaVendor(t)
	env.seedMurekaChannel(t, vendor.server.URL, "mureka-song-edit", "mureka-9.5")
	key, full := env.seedKey(t, 2_000_000)
	env.seedMusicPrice(t, "mureka-song-edit")

	w := env.postMusic(t, full,
		`{"model":"mureka-song-edit","instrumental":false,"audio_ref":"`+murekaRef(vendor)+`"}`)
	if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "invalid_request_error") {
		t.Fatalf("status = %d body %s, want 400 invalid_request_error", w.Code, w.Body.String())
	}
	if strings.Contains(w.Body.String(), "'prompt'") {
		t.Errorf("body = %s, want the lyrics copy not the prompt copy", w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "歌词必填") {
		t.Errorf("body = %s, want the lyrics-required copy", w.Body.String())
	}
	if dials := vendor.dialCount(); dials != 0 {
		t.Errorf("vendor dials = %d, want 0 (refused before dialing)", dials)
	}
	if balance := env.balanceOf(t, key.ID); balance != 2_000_000 {
		t.Errorf("balance = %d, want the reserve untouched", balance)
	}

	// 对照:补上歌词(prompt 仍空)同一形状即走豁免受理。
	w = env.postMusic(t, full,
		`{"model":"mureka-song-edit","instrumental":false,"lyrics":"夜色慢慢降落","audio_ref":"`+murekaRef(vendor)+`"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("lyrics-only submit status = %d body %s, want 200", w.Code, w.Body.String())
	}
}

// 形状 sentinel 2:audio_ref 为空的普通音乐请求 = 请求级 400,文案指路
// 普通音乐公开名 —— 同款断言集。
func TestRelayMurekaMusicRejectsPlainGeneration(t *testing.T) {
	env := newRelayEnv(t, nil)
	vendor := newFakeMurekaVendor(t)
	env.seedMurekaChannel(t, vendor.server.URL, "mureka-song-edit", "mureka-9.5")
	key, full := env.seedKey(t, 2_000_000)
	env.seedMusicPrice(t, "mureka-song-edit")

	w := env.postMusic(t, full, `{"model":"mureka-song-edit","prompt":"纯音乐","instrumental":true}`)
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
	var rows int
	if err := env.stores.db.QueryRowContext(context.Background(),
		`SELECT COUNT(*) FROM usage_logs`).Scan(&rows); err != nil {
		t.Fatalf("count usage rows: %v", err)
	}
	if rows != 0 {
		t.Errorf("usage rows = %d, want 0", rows)
	}

	// 不记熔断账:正常 relyric 请求照常受理。
	w = env.postMusic(t, full,
		`{"model":"mureka-song-edit","prompt":"city pop","instrumental":false,"lyrics":"夜色慢慢降落","audio_ref":"`+murekaRef(vendor)+`"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("follow-up relyric submit status = %d body %s, want 200", w.Code, w.Body.String())
	}
	if vendor.dialCount() == 0 {
		t.Errorf("vendor dials = %d, want >0 (channel not breaker-tripped)", vendor.dialCount())
	}
}

// 正向受理全链:下载参考字节 → multipart 上传(purpose + 整段字节)取
// 文件 id → 提交 region-edit(文件 id + 歌词 + 全曲选区 edit_start=0/
// edit_end=嗅探时长 + prompt 透传;audio_ref/model 不泄漏)→ 轮询 GENERATING
// 翻 running、FINISHED 翻 succeeded 交付 audio.url → 按次预扣恒等结清。
// 对照面:prompt 省略的提交体不带 prompt 键。
func TestRelayMurekaMusicEditEndToEnd(t *testing.T) {
	env := newRelayEnv(t, nil)
	vendor := newFakeMurekaVendor(t)
	env.seedMurekaChannel(t, vendor.server.URL, "mureka-song-edit", "mureka-9.5")
	key, full := env.seedKey(t, 2_000_000)
	env.seedMusicPrice(t, "mureka-song-edit")
	vendor.scriptQuery("nested", "GENERATING", "", "")

	w := env.postMusic(t, full,
		`{"model":"mureka-song-edit","prompt":"city pop","instrumental":false,"lyrics":"夜色慢慢降落","audio_ref":"`+murekaRef(vendor)+`"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("submit status = %d body %s, want 200", w.Code, w.Body.String())
	}
	id, _ := decodeMusicTask(t, w.Body.Bytes())["id"].(string)
	if !strings.HasPrefix(id, "mt_") {
		t.Fatalf("id = %v, want an mt_ gateway id", id)
	}

	// 上传链:参考被下载(1 次)并整段上传,purpose=audio、multipart。
	if purpose, contentType, fileLen := vendor.uploadFacts(); purpose != "audio" ||
		!strings.Contains(contentType, "multipart/form-data") || fileLen != len(murekaMP3Blob(100)) {
		t.Errorf("upload facts = (%q, %q, %d), want (audio, multipart, %d)", purpose, contentType, fileLen, len(murekaMP3Blob(100)))
	}
	// 提交体:Mureka Edit 形状 —— 文件 id + 歌词 + 全曲选区(48000Hz/
	// 128kbps 100 帧 = 2400ms)+ prompt 透传;网关内部字段不泄漏。
	body := vendor.lastEditBody()
	for _, want := range []string{
		`"audio_id":"mureka-file-1"`,
		`"lyrics":"夜色慢慢降落"`,
		`"edit_start":0`,
		`"edit_end":2400`,
		`"prompt":"city pop"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("edit body missing %s:\n%s", want, body)
		}
	}
	for _, leaked := range []string{`"audio_ref"`, `"model"`, `"instrumental"`} {
		if strings.Contains(body, leaked) {
			t.Errorf("edit body leaked %s:\n%s", leaked, body)
		}
	}

	// 账务:按次预扣照旧(同曲换词不改变计价面)。
	if balance := env.balanceOf(t, key.ID); balance != 1_800_000 {
		t.Fatalf("balance after submit = %d, want 1_800_000 held", balance)
	}

	// 轮询 1:Mureka 的 GENERATING 翻成 running(直透会被词表 default 误杀
	// 成 failed,翻译层存在的原因同 vodMusicStatus)。
	w = env.getMusicTask(t, full, id)
	if got := decodeMusicTask(t, w.Body.Bytes()); got["status"] != "running" {
		t.Errorf("GENERATING merged to %v, want running", got["status"])
	}

	// 轮询 2:FINISHED + audio.url → succeeded,拿到 audio.url。
	vendor.scriptQuery("nested", "FINISHED", "https://mureka-temp.example/song.mp3", "")
	w = env.getMusicTask(t, full, id)
	if w.Code != http.StatusOK {
		t.Fatalf("final poll status = %d body %s", w.Code, w.Body.String())
	}
	done := decodeMusicTask(t, w.Body.Bytes())
	if done["status"] != "succeeded" {
		t.Fatalf("final = %v, want succeeded", done["status"])
	}
	audio, _ := done["audio"].(map[string]any)
	if audio == nil || audio["url"] != "https://mureka-temp.example/song.mp3" {
		t.Errorf("audio = %v, want the Mureka URL under audio.url", done["audio"])
	}
	if balance := env.balanceOf(t, key.ID); balance != 1_800_000 {
		t.Errorf("balance after success = %d, want 1_800_000 (reserve became the charge)", balance)
	}
}

// 轮询归一的翻译面:Mureka 原始状态词(未钉定枚举的常见同义词)翻进网关
// 词表 —— CREATED→queued、GENERATING→running、FINISHED→succeeded(交付
// audio.url)、REJECTED→failed(失败原因进 error.message)。未知词归并
// failed 与词表 default 一致。
func TestRelayMurekaMusicStatusWords(t *testing.T) {
	env := newRelayEnv(t, nil)
	vendor := newFakeMurekaVendor(t)
	env.seedMurekaChannel(t, vendor.server.URL, "mureka-song-edit", "mureka-9.5")
	_, full := env.seedKey(t, 20_000_000)
	env.seedMusicPrice(t, "mureka-song-edit")

	cases := []struct {
		name   string
		word   string
		url    string
		err    string
		want   string
		wantIn string // 终态 failed 时 error.message 应含的片段
	}{
		{"CREATED 归并 queued", "CREATED", "", "", "queued", ""},
		{"GENERATING 归并 running", "GENERATING", "", "", "running", ""},
		{"FINISHED 归并 succeeded", "FINISHED", "https://mureka-temp.example/a.mp3", "", "succeeded", ""},
		{"REJECTED 归并 failed", "REJECTED", "", "内容审核未通过", "failed", "内容审核未通过"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			vendor.scriptQuery("nested", "CREATED", "", "")
			w := env.postMusic(t, full,
				`{"model":"mureka-song-edit","prompt":"city pop","instrumental":false,"lyrics":"夜色慢慢降落","audio_ref":"`+murekaRef(vendor)+`"}`)
			if w.Code != http.StatusOK {
				t.Fatalf("submit status = %d body %s, want 200", w.Code, w.Body.String())
			}
			id, _ := decodeMusicTask(t, w.Body.Bytes())["id"].(string)
			vendor.scriptQuery("nested", tc.word, tc.url, tc.err)
			w = env.getMusicTask(t, full, id)
			if w.Code != http.StatusOK {
				t.Fatalf("poll status = %d body %s", w.Code, w.Body.String())
			}
			got := decodeMusicTask(t, w.Body.Bytes())
			if got["status"] != tc.want {
				t.Errorf("%s merged to %v, want %s (body %s)", tc.word, got["status"], tc.want, w.Body.String())
			}
			if tc.wantIn != "" && !strings.Contains(w.Body.String(), tc.wantIn) {
				t.Errorf("body = %s, want the vendor failure copy %q", w.Body.String(), tc.wantIn)
			}
		})
	}
}

// 产物地址解析从宽:官方产物字段未钉定,候选字段逐个尝试 —— 嵌套
// audio.url、扁平 audio_url、files[0].url 三种形状都归一到 audio.url。
func TestRelayMurekaMusicAudioURLCandidates(t *testing.T) {
	env := newRelayEnv(t, nil)
	vendor := newFakeMurekaVendor(t)
	env.seedMurekaChannel(t, vendor.server.URL, "mureka-song-edit", "mureka-9.5")
	_, full := env.seedKey(t, 20_000_000)
	env.seedMusicPrice(t, "mureka-song-edit")

	for _, shape := range []string{"nested", "flat", "files"} {
		t.Run(shape, func(t *testing.T) {
			vendor.scriptQuery("nested", "CREATED", "", "")
			w := env.postMusic(t, full,
				`{"model":"mureka-song-edit","prompt":"city pop","instrumental":false,"lyrics":"夜色慢慢降落","audio_ref":"`+murekaRef(vendor)+`"}`)
			if w.Code != http.StatusOK {
				t.Fatalf("submit status = %d body %s, want 200", w.Code, w.Body.String())
			}
			id, _ := decodeMusicTask(t, w.Body.Bytes())["id"].(string)
			vendor.scriptQuery(shape, "FINISHED", "https://mureka-temp.example/"+shape+".mp3", "")
			w = env.getMusicTask(t, full, id)
			got := decodeMusicTask(t, w.Body.Bytes())
			audio, _ := got["audio"].(map[string]any)
			if got["status"] != "succeeded" || audio == nil || audio["url"] != "https://mureka-temp.example/"+shape+".mp3" {
				t.Errorf("shape %s → %v audio %v, want succeeded with the URL", shape, got["status"], audio)
			}
		})
	}
}

// 上传链任何失败(下载/上传/嗅探)都按请求级失败处理(参考是身份所在,
// 不静默降级,79 号票定案 6):400 invalid_request_error + 上传失败文案,
// 不再发 Edit 提交,预扣原退、0 用量行。2xx 却拿不到任务 id 是上游失败:
// 合成 502 形(可换道不建行),预扣退回、留一条 upstream_error 用量行。
func TestRelayMurekaMusicUploadChainFails(t *testing.T) {
	t.Run("上传 5xx=请求级 400", func(t *testing.T) {
		env := newRelayEnv(t, nil)
		vendor := newFakeMurekaVendor(t)
		env.seedMurekaChannel(t, vendor.server.URL, "mureka-song-edit", "mureka-9.5")
		key, full := env.seedKey(t, 2_000_000)
		env.seedMusicPrice(t, "mureka-song-edit")
		vendor.scriptUpload(http.StatusInternalServerError, "")

		w := env.postMusic(t, full,
			`{"model":"mureka-song-edit","prompt":"city pop","instrumental":false,"lyrics":"夜色慢慢降落","audio_ref":"`+murekaRef(vendor)+`"}`)
		if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "invalid_request_error") {
			t.Fatalf("status = %d body %s, want 400 invalid_request_error", w.Code, w.Body.String())
		}
		if !strings.Contains(w.Body.String(), "上传失败") {
			t.Errorf("body = %s, want the upload-failed copy", w.Body.String())
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
			t.Errorf("usage rows = %d, want 0 (request-level refusal)", rows)
		}
		if body := vendor.lastEditBody(); body != "" {
			t.Errorf("region-edit dialed despite upload failure: %s", body)
		}
	})

	t.Run("参考超限=请求级 400(读满上限再探一字节)", func(t *testing.T) {
		env := newRelayEnv(t, nil)
		vendor := newFakeMurekaVendor(t)
		env.seedMurekaChannel(t, vendor.server.URL, "mureka-song-edit", "mureka-9.5")
		key, full := env.seedKey(t, 2_000_000)
		env.seedMusicPrice(t, "mureka-song-edit")
		vendor.setRefOversize()

		w := env.postMusic(t, full,
			`{"model":"mureka-song-edit","prompt":"city pop","instrumental":false,"lyrics":"夜色慢慢降落","audio_ref":"`+murekaRef(vendor)+`"}`)
		if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "上传失败") {
			t.Fatalf("status = %d body %s, want 400 with the upload-chain copy", w.Code, w.Body.String())
		}
		if !strings.Contains(w.Body.String(), "too large") {
			t.Errorf("body = %s, want the oversize reason", w.Body.String())
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
			t.Errorf("usage rows = %d, want 0 (request-level refusal)", rows)
		}
		if body := vendor.lastEditBody(); body != "" {
			t.Errorf("region-edit dialed despite oversize reference: %s", body)
		}
	})

	t.Run("参考 Content-Length 超限=请求级 400(预检)", func(t *testing.T) {
		env := newRelayEnv(t, nil)
		vendor := newFakeMurekaVendor(t)
		env.seedMurekaChannel(t, vendor.server.URL, "mureka-song-edit", "mureka-9.5")
		_, full := env.seedKey(t, 2_000_000)
		env.seedMusicPrice(t, "mureka-song-edit")
		vendor.setRefDeclared(40 << 20)

		w := env.postMusic(t, full,
			`{"model":"mureka-song-edit","prompt":"city pop","instrumental":false,"lyrics":"夜色慢慢降落","audio_ref":"`+murekaRef(vendor)+`"}`)
		if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "上传失败") {
			t.Fatalf("status = %d body %s, want 400 with the upload-chain copy", w.Code, w.Body.String())
		}
		if !strings.Contains(w.Body.String(), "too large") {
			t.Errorf("body = %s, want the oversize reason", w.Body.String())
		}
		if body := vendor.lastEditBody(); body != "" {
			t.Errorf("region-edit dialed despite oversize reference: %s", body)
		}
	})

	t.Run("参考音频解不动=请求级 400", func(t *testing.T) {
		env := newRelayEnv(t, nil)
		vendor := newFakeMurekaVendor(t)
		env.seedMurekaChannel(t, vendor.server.URL, "mureka-song-edit", "mureka-9.5")
		_, full := env.seedKey(t, 2_000_000)
		env.seedMusicPrice(t, "mureka-song-edit")
		vendor.setRefBytes([]byte("definitely not an audio stream"))

		w := env.postMusic(t, full,
			`{"model":"mureka-song-edit","prompt":"city pop","instrumental":false,"lyrics":"夜色慢慢降落","audio_ref":"`+murekaRef(vendor)+`"}`)
		if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "上传失败") {
			t.Fatalf("status = %d body %s, want 400 with the upload-chain copy", w.Code, w.Body.String())
		}
		if body := vendor.lastEditBody(); body != "" {
			t.Errorf("region-edit dialed despite sniff failure: %s", body)
		}
	})

	t.Run("提交 2xx 无任务 id=502 可换道", func(t *testing.T) {
		env := newRelayEnv(t, nil)
		vendor := newFakeMurekaVendor(t)
		env.seedMurekaChannel(t, vendor.server.URL, "mureka-song-edit", "mureka-9.5")
		key, full := env.seedKey(t, 2_000_000)
		env.seedMusicPrice(t, "mureka-song-edit")
		vendor.scriptEdit(http.StatusOK, `{"ok":true}`)

		w := env.postMusic(t, full,
			`{"model":"mureka-song-edit","prompt":"city pop","instrumental":false,"lyrics":"夜色慢慢降落","audio_ref":"`+murekaRef(vendor)+`"}`)
		if w.Code != http.StatusBadGateway || !strings.Contains(w.Body.String(), "upstream_error") {
			t.Fatalf("status = %d body %s, want 502 upstream_error", w.Code, w.Body.String())
		}
		if balance := env.balanceOf(t, key.ID); balance != 2_000_000 {
			t.Errorf("balance = %d, want the reserve refunded", balance)
		}
		var rows int
		if err := env.stores.db.QueryRowContext(context.Background(),
			`SELECT COUNT(*) FROM usage_logs`).Scan(&rows); err != nil {
			t.Fatalf("count usage rows: %v", err)
		}
		if rows != 1 {
			t.Errorf("usage rows = %d, want 1 (upstream failure trail)", rows)
		}
	})
}

// prompt 必填豁免(79 号票定案 5「风格 prompt 选填」的网关落地):同曲
// 换词形状(audio_ref 非空 + 歌词非空)打到全 mureka 候选时放行空 prompt
// —— 词已是身份描述;豁免只放行空串,2000 上限恒适用。分型按候选渠道:
// promptless 打到 vod/mps 的语义与错误形状字节不变(78 号票 Out of
// Scope),混入非 mureka 候选或打到纯 cover/普通名照旧 400。
func TestRelayMurekaMusicPromptExempt(t *testing.T) {
	env := newRelayEnv(t, nil)
	vendor := newFakeMurekaVendor(t)
	env.seedMurekaChannel(t, vendor.server.URL, "mureka-song-edit", "mureka-9.5")
	key, full := env.seedKey(t, 2_000_000)
	env.seedMusicPrice(t, "mureka-song-edit")

	// promptless + relyric 形状 = 受理;提交体带歌词、不带 prompt 键。
	w := env.postMusic(t, full,
		`{"model":"mureka-song-edit","instrumental":false,"lyrics":"第二首的词","audio_ref":"`+murekaRef(vendor)+`"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("promptless relyric submit status = %d body %s, want 200", w.Code, w.Body.String())
	}
	if body := vendor.lastEditBody(); !strings.Contains(body, `"lyrics":"第二首的词"`) || strings.Contains(body, `"prompt"`) {
		t.Errorf("promptless edit body = %s, want lyrics carried and no prompt key", body)
	}
	if balance := env.balanceOf(t, key.ID); balance != 1_800_000 {
		t.Errorf("balance = %d, want the call track reserve held", balance)
	}

	// 2000 上限恒适用:超长 prompt 在 exempt 形状里照样 400(拨号前)。
	long := strings.Repeat("雾", 2001)
	w = env.postMusic(t, full,
		`{"model":"mureka-song-edit","prompt":"`+long+`","instrumental":false,"lyrics":"x","audio_ref":"`+murekaRef(vendor)+`"}`)
	if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "invalid_request_error") {
		t.Fatalf("overlong prompt status = %d body %s, want 400", w.Code, w.Body.String())
	}

	// 对照面 1:promptless 打到 MPS 族 cover 名照旧 400(vod/mps 语义不变)。
	mps := newFakeMPSVendor(t)
	env.seedMPSChannelPrio(t, mps.server.URL, "vod-minimax-music-3.0-cover", "MiniMaxMusic 3.0", 0)
	env.seedMusicPrice(t, "vod-minimax-music-3.0-cover")
	w = env.postMusic(t, full,
		`{"model":"vod-minimax-music-3.0-cover","instrumental":false,"lyrics":"x"}`)
	if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "'prompt' parameter") {
		t.Fatalf("promptless cover status = %d body %s, want 400 missing-prompt", w.Code, w.Body.String())
	}
	if submits, _ := mps.counts(); submits != 0 {
		t.Errorf("mps submits = %d, want 0 (refused before dialing)", submits)
	}

	// 对照面 2:候选混入非 mureka 渠道(同公开名双渠道)时豁免不生效 ——
	// promptless 可能落到 vod/mps,语义必须照旧 400。
	env.seedMPSChannelPrio(t, mps.server.URL, "mureka-song-edit", "MiniMaxMusic 3.0", 0)
	w = env.postMusic(t, full,
		`{"model":"mureka-song-edit","instrumental":false,"lyrics":"x","audio_ref":"`+murekaRef(vendor)+`"}`)
	if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "'prompt' parameter") {
		t.Fatalf("mixed-candidate promptless status = %d body %s, want 400 missing-prompt", w.Code, w.Body.String())
	}
}
