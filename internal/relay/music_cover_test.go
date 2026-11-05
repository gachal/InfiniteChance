package relay_test

// 78 号票(歌曲翻唱)网关契约的端到端测试,真 key 打样反转后的形态:
// audio_ref http(s) 原样透传、素材内容寻址路径经素材表解出真实地址并
// 改写请求体(adaptor 拿到的恒是上游可拉取地址)、data: 内联与其余形状
// 在调度与预扣之前 400;翻唱通路在 MPS 族渠道(AudioInfos 单字段
// AudioUrl —— 实测拒 Type 收 AudioUrl),翻唱参考打到 vod 族渠道 = 请求
// 级 400 invalid_request_error(不换道、不记熔断账、预扣原退、不留用量
// 行;VOD 域收下 Type+Url 结构却不执行翻唱,静默丢参考不可接受)。
// 缺省不传 = 现状行为逐字节不变。共享 relay_test.go 的 MySQL 环境,
// 基础设施不可达时自动 skip。

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"testing"

	"github.com/gachal/InfiniteChance/internal/asset"
)

// seedAudioAsset inserts one audio asset row for the content-path tests.
func (e *relayEnv) seedAudioAsset(t *testing.T, objectKey, url string) asset.Asset {
	t.Helper()
	a, err := e.stores.assets.Create(context.Background(), asset.Asset{
		Kind: asset.KindAudio, ObjectKey: objectKey, URL: url,
	})
	if err != nil {
		t.Fatalf("seed audio asset: %v", err)
	}
	return a
}

// wireCoverResolution attaches the asset store and a fixed public base to
// the test handlers (78 号票 audio_ref 解引用的装配面,生产在 wiring).
func (e *relayEnv) wireCoverResolution(t *testing.T) {
	t.Helper()
	e.handlers.Assets = e.stores.assets
	e.handlers.PublicBaseURL = func(ctx context.Context) string { return "https://cdn.example" }
}

// cover 提交端到端:audio_ref → MPS 收到 AudioInfos
// [{AudioUrl:…}] + 歌曲事实;对照面不带 audio_ref 的提交体不含
// AudioInfos 键且 is_instrumental 缺省 true(逐字节不变)。
func TestRelayMPSMusicCoverCarriesAudioUrl(t *testing.T) {
	env := newRelayEnv(t, nil)
	vendor := newFakeMPSVendor(t)
	env.seedMPSChannelPrio(t, vendor.server.URL, "vod-minimax-music-3.0-cover", "MiniMaxMusic 3.0", 10)
	_, full := env.seedKey(t, 2_000_000)
	env.seedMusicPrice(t, "vod-minimax-music-3.0-cover")

	w := env.postMusic(t, full,
		`{"model":"vod-minimax-music-3.0-cover","prompt":"city pop cover","instrumental":false,"lyrics":"夜色慢慢降落","audio_ref":"https://assets.example/original.mp3"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("cover submit status = %d body %s, want 200", w.Code, w.Body.String())
	}
	body := vendor.lastSubmitBody()
	for _, want := range []string{
		`"AudioInfos":[{"AudioUrl":"https://assets.example/original.mp3"}]`,
		// AdditionalParameters 是 JSON 字符串字段,内层引号带转义。
		`{\"is_instrumental\":false,\"lyrics\":\"夜色慢慢降落\"}`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("cover submit body missing %s:\n%s", want, body)
		}
	}
	if strings.Contains(body, "audio_ref") {
		t.Errorf("cover submit body leaked audio_ref:\n%s", body)
	}

	// 对照面:不带 audio_ref 的提交体不含 AudioInfos 键(逐字节不变)。
	w = env.postMusic(t, full, `{"model":"vod-minimax-music-3.0-cover","prompt":"纯音乐"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("plain submit status = %d body %s, want 200", w.Code, w.Body.String())
	}
	if body := vendor.lastSubmitBody(); strings.Contains(body, "AudioInfos") {
		t.Errorf("plain submit body carries AudioInfos:\n%s", body)
	}
}

// audio_ref 的形状守卫:data: 内联、形状不对、超长、素材缺失、种类不符
// 都在调度与预扣之前 400(不拨上游、不动账)。
func TestRelayMusicAudioRefGuards(t *testing.T) {
	env := newRelayEnv(t, nil)
	vendor := newFakeMPSVendor(t)
	env.seedMPSChannelPrio(t, vendor.server.URL, "cover-m", "MiniMaxMusic 3.0", 10)
	key, full := env.seedKey(t, 2_000_000)
	env.seedMusicPrice(t, "cover-m")
	env.wireCoverResolution(t)

	cases := []struct {
		name string
		ref  string
	}{
		{"data: 内联", "data:audio/mpeg;base64,AAAA"},
		{"形状不对", "/uploads/some-file.mp3"},
		{"id 不合法", "/api/assets/not-a-number/content"},
		{"素材缺失", "/api/assets/999999/content"},
	}
	for _, tc := range cases {
		body := `{"model":"cover-m","prompt":"cover","audio_ref":"` + tc.ref + `"}`
		w := env.postMusic(t, full, body)
		if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "invalid_request_error") {
			t.Errorf("%s: status = %d body %s, want 400 invalid_request_error", tc.name, w.Code, w.Body.String())
		}
	}
	if submits := len(vendor.bodies); submits != 0 {
		t.Errorf("vendor submits = %d, want 0 (all refused before dialing)", submits)
	}
	if balance := env.balanceOf(t, key.ID); balance != 2_000_000 {
		t.Errorf("balance = %d, want untouched (no pre-deduction on guards)", balance)
	}

	// http(s) 超长同码拒绝;合法 http(s) 照常受理。
	long := "https://assets.example/" + strings.Repeat("a", 4096)
	w := env.postMusic(t, full, `{"model":"cover-m","prompt":"cover","audio_ref":"`+long+`"}`)
	if w.Code != http.StatusBadRequest {
		t.Errorf("overlong audio_ref status = %d, want 400", w.Code)
	}
	w = env.postMusic(t, full, `{"model":"cover-m","prompt":"cover","audio_ref":"https://assets.example/original.mp3"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("http(s) audio_ref status = %d body %s, want 200", w.Code, w.Body.String())
	}
	if body := vendor.lastSubmitBody(); !strings.Contains(body, `"AudioUrl":"https://assets.example/original.mp3"`) {
		t.Errorf("http(s) audio_ref body = %s, want the verbatim address", body)
	}
}

// 内容寻址路径的服务端解引用:自有存储公网地址(object_key + public base)
// 优先,请求体就地改写,MPS 收到的是解出后的地址;素材行只有厂商原址时
// 原址回落;kind 非 audio 的素材行拒收。
func TestRelayMusicAudioRefResolvesContentPath(t *testing.T) {
	env := newRelayEnv(t, nil)
	vendor := newFakeMPSVendor(t)
	env.seedMPSChannelPrio(t, vendor.server.URL, "cover-m", "MiniMaxMusic 3.0", 10)
	_, full := env.seedKey(t, 2_000_000)
	env.seedMusicPrice(t, "cover-m")
	env.wireCoverResolution(t)

	// 自有字节 + 公网基座:解出永久地址并改写请求体。
	a := env.seedAudioAsset(t, "uploads/20261104/abc.mp3", "")
	w := env.postMusic(t, full,
		`{"model":"cover-m","prompt":"cover","audio_ref":"/api/assets/`+itoa(a.ID)+`/content"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("content-path submit status = %d body %s, want 200", w.Code, w.Body.String())
	}
	if body := vendor.lastSubmitBody(); !strings.Contains(body, `"AudioUrl":"https://cdn.example/uploads/20261104/abc.mp3"`) {
		t.Errorf("content-path body = %s, want the resolved public address", body)
	}
	if strings.Contains(vendor.lastSubmitBody(), "/api/assets/") {
		t.Errorf("content path leaked to the vendor: %s", vendor.lastSubmitBody())
	}

	// 无自有字节、只有厂商原址:原址回落(18 号票顺序)。
	b := env.seedAudioAsset(t, "", "https://vendor.example/song-master.mp3")
	w = env.postMusic(t, full,
		`{"model":"cover-m","prompt":"cover","audio_ref":"/assets/`+itoa(b.ID)+`/content"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("vendor-url fallback status = %d body %s, want 200", w.Code, w.Body.String())
	}
	if body := vendor.lastSubmitBody(); !strings.Contains(body, `"AudioUrl":"https://vendor.example/song-master.mp3"`) {
		t.Errorf("fallback body = %s, want the vendor address", body)
	}

	// 种类不符(视频素材行):400。
	c, err := env.stores.assets.Create(context.Background(), asset.Asset{Kind: asset.KindVideo})
	if err != nil {
		t.Fatalf("seed video asset: %v", err)
	}
	w = env.postMusic(t, full,
		`{"model":"cover-m","prompt":"cover","audio_ref":"/api/assets/`+itoa(c.ID)+`/content"}`)
	if w.Code != http.StatusBadRequest {
		t.Errorf("wrong-kind asset status = %d body %s, want 400", w.Code, w.Body.String())
	}
}

// 翻唱参考打到 vod 族渠道 = 请求级 400 invalid_request_error:预扣原退、
// 不留用量行、不记熔断账(下一拍同渠道的正常请求照常受理)。
func TestRelayVODMusicRejectsAudioRefAsRequestError(t *testing.T) {
	env := newRelayEnv(t, nil)
	vod := newFakeVODVendor(t)
	env.seedVODMusicChannel(t, vod.server.URL, "mix-cover", "MiniMaxMusic 3.0")
	key, full := env.seedKey(t, 2_000_000)
	env.seedMusicPrice(t, "mix-cover")

	w := env.postMusic(t, full,
		`{"model":"mix-cover","prompt":"cover","audio_ref":"https://assets.example/original.mp3"}`)
	if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "invalid_request_error") {
		t.Fatalf("status = %d body %s, want 400 invalid_request_error", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "参考翻唱") {
		t.Errorf("body = %s, want the cover guidance copy", w.Body.String())
	}
	// adaptor 本地拒绝,vod 没被拨号。
	if vodSubmits := len(vod.bodies); vodSubmits != 0 {
		t.Errorf("vod vendor submits = %d, want 0 (refused at the adaptor seam)", vodSubmits)
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

	// 不记熔断账:同一渠道随后不带 audio_ref 的请求照常受理。
	w = env.postMusic(t, full, `{"model":"mix-cover","prompt":"纯音乐"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("follow-up plain submit status = %d body %s, want 200", w.Code, w.Body.String())
	}
	if vodSubmits := len(vod.bodies); vodSubmits != 1 {
		t.Errorf("vod vendor submits = %d, want 1 (channel not breaker-tripped)", vodSubmits)
	}
}

// itoa spells strconv.Itoa short for the ref-building lines above.
func itoa(n int64) string {
	return strconv.FormatInt(n, 10)
}
