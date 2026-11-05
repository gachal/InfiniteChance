package relay

// 78 号票(歌曲翻唱)adaptor 层单元测试,真 key 打样反转后的形状钉:
// mps 族是翻唱通路 —— MusicSubmit 在 audio_ref 非空时提交体带
// AudioInfos([{AudioUrl:…}],MPS 域 AigcAudioReferenceAudioInfo 单字段
// 形状;实测拒 Type 收 AudioUrl),空时不带键(60/62 号票提交体逐字节
// 不变);vod 族 MusicSubmit 检测到 audio_ref 即按请求级失败拒绝
// (errAudioRefUnsupported —— 不换道、不记熔断账的 400
// invalid_request_error 由 relay 归一面落,这里只钉 adaptor 的拒绝;
// VOD 域收下 Type+Url 结构却不执行翻唱,静默丢参考不可接受)。

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"
)

// mps 翻唱参考:audio_ref 非空 → CreateAigcAudioTask 带 AudioInfos 单条
// AudioUrl;is_instrumental/lyrics 按请求携带;模型串拆分照旧。
func TestMPSAdaptorMusicSubmitCarriesAudioUrl(t *testing.T) {
	stub, srv := newMPSStub(t)
	a := newMPSAdaptor()

	payload := []byte(`{"model":"MiniMaxMusic 3.0","prompt":"city pop cover","instrumental":false,"lyrics":"夜色慢慢降落","audio_ref":"https://assets.example/original.mp3"}`)
	upstream, err := a.MusicSubmit(context.Background(), mpsTestChannel(srv.URL), payload)
	if err != nil {
		t.Fatalf("MusicSubmit: %v", err)
	}
	if !upstream.OK || upstream.Status != http.StatusOK {
		t.Fatalf("upstream = %d ok=%v body=%s", upstream.Status, upstream.OK, upstream.Body)
	}

	select {
	case raw := <-stub.submits:
		if !strings.Contains(string(raw), `"AudioInfos":[{"AudioUrl":"https://assets.example/original.mp3"}]`) {
			t.Fatalf("submit body missing the AudioUrl reference:\n%s", raw)
		}
		if !strings.Contains(string(raw), `\"is_instrumental\":false`) || !strings.Contains(string(raw), `夜色慢慢降落`) {
			t.Fatalf("submit body missing the song facts:\n%s", raw)
		}
		if strings.Contains(string(raw), "audio_ref") {
			t.Fatalf("mps submit body leaked audio_ref:\n%s", raw)
		}
		var submit struct {
			AudioInfos []map[string]any `json:"AudioInfos"`
			ModelName  string           `json:"ModelName"`
		}
		if err := json.Unmarshal([]byte(raw), &submit); err != nil {
			t.Fatalf("submit body: %v (%s)", err, raw)
		}
		if len(submit.AudioInfos) != 1 {
			t.Fatalf("AudioInfos = %v, want exactly one entry", submit.AudioInfos)
		}
		if submit.AudioInfos[0]["AudioUrl"] != "https://assets.example/original.mp3" {
			t.Fatalf("AudioUrl = %v, want the resolved reference address", submit.AudioInfos[0]["AudioUrl"])
		}
		if submit.ModelName != "MiniMaxMusic" {
			t.Fatalf("ModelName = %q", submit.ModelName)
		}
	default:
		t.Fatal("no submit reached the stub")
	}
}

// 无 audio_ref 的 mps 提交体逐字节不带 AudioInfos 键,纯音乐缺省
// is_instrumental:true(60/62 号票既有形态零改动)。
func TestMPSAdaptorMusicSubmitWithoutAudioRefAccepted(t *testing.T) {
	stub, srv := newMPSStub(t)
	a := newMPSAdaptor()

	payload := []byte(`{"model":"MiniMaxMusic 3.0","prompt":"纯音乐"}`)
	upstream, err := a.MusicSubmit(context.Background(), mpsTestChannel(srv.URL), payload)
	if err != nil {
		t.Fatalf("MusicSubmit: %v", err)
	}
	if !upstream.OK {
		t.Fatalf("upstream not ok: %s", upstream.Body)
	}
	select {
	case raw := <-stub.submits:
		if strings.Contains(string(raw), "AudioInfos") {
			t.Fatalf("submit body carries AudioInfos without audio_ref:\n%s", raw)
		}
		if !strings.Contains(string(raw), `\"is_instrumental\":true`) {
			t.Fatalf("plain submit body missing the default instrumental flag:\n%s", raw)
		}
	default:
		t.Fatal("no submit reached the stub")
	}
}

// vod 族 MusicSubmit 遇 audio_ref → 请求级拒绝(errAudioRefUnsupported),
// 不拨号、不产生提交体(VOD 域收下参考却不执行翻唱,静默丢不可接受)。
func TestVODAdaptorMusicSubmitRejectsAudioRef(t *testing.T) {
	_, srv := newVODStub(t)
	a := fastVODAdaptor(srv.URL)

	payload := []byte(`{"model":"MiniMaxMusic 3.0","prompt":"cover","audio_ref":"https://assets.example/original.mp3"}`)
	upstream, err := a.MusicSubmit(context.Background(), vodTestChannel(srv.URL), payload)
	if !errors.Is(err, errAudioRefUnsupported) {
		t.Fatalf("err = %v, want errAudioRefUnsupported", err)
	}
	if upstream != nil {
		t.Fatalf("upstream = %+v, want nil (refused before dialing)", upstream)
	}
}

// 对照面:不带 audio_ref 的 vod 提交照常受理且无 AudioInfos 键(拒绝只
// 盯参考字段,60/62 号票既有行为零改动)。
func TestVODAdaptorMusicSubmitWithoutAudioRefAccepted(t *testing.T) {
	stub, srv := newVODStub(t)
	a := fastVODAdaptor(srv.URL)

	payload := []byte(`{"model":"MiniMaxMusic 3.0","prompt":"纯音乐","instrumental":true}`)
	upstream, err := a.MusicSubmit(context.Background(), vodTestChannel(srv.URL), payload)
	if err != nil {
		t.Fatalf("MusicSubmit: %v", err)
	}
	if !upstream.OK {
		t.Fatalf("upstream not ok: %s", upstream.Body)
	}
	select {
	case raw := <-stub.submits:
		if strings.Contains(string(raw), "AudioInfos") {
			t.Fatalf("submit body carries AudioInfos without audio_ref:\n%s", raw)
		}
	default:
		t.Fatal("no submit reached the stub")
	}
}
