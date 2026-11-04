package relay

// 78 号票(歌曲翻唱)adaptor 层单元测试:vod 族 MusicSubmit 在 audio_ref
// 非空时提交体带 AudioInfos([{AudioUrl:…}],官方数据结构页形状)、空时不带
// 键(60/62 号票提交体逐字节不变);mps 族 MusicSubmit 检测到 audio_ref
// 即按请求级失败拒绝(errAudioRefUnsupported —— 不换道、不记熔断账的
// 400 invalid_request_error 由 relay 归一面落,这里只钉 adaptor 的拒绝)。

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"
)

// vod 翻唱参考:audio_ref 非空 → CreateAigcAudioTask 带 AudioInfos 单条
// AudioUrl;模型串拆分与 OutputConfig 与 62 号票形态一致。
func TestVODAdaptorMusicSubmitCarriesAudioInfos(t *testing.T) {
	stub, srv := newVODStub(t)
	a := fastVODAdaptor(srv.URL)

	payload := []byte(`{"model":"MiniMaxMusic 3.0","prompt":"city pop cover","instrumental":false,"lyrics":"夜色慢慢降落","audio_ref":"https://assets.example/original.mp3"}`)
	upstream, err := a.MusicSubmit(context.Background(), vodTestChannel(srv.URL), payload)
	if err != nil {
		t.Fatalf("MusicSubmit: %v", err)
	}
	if !upstream.OK || upstream.Status != http.StatusOK {
		t.Fatalf("upstream = %d ok=%v body=%s", upstream.Status, upstream.OK, upstream.Body)
	}

	var submit struct {
		AudioInfos []map[string]any `json:"AudioInfos"`
		ModelName  string           `json:"ModelName"`
	}
	select {
	case raw := <-stub.submits:
		if err := json.Unmarshal(raw, &submit); err != nil {
			t.Fatalf("submit body: %v (%s)", err, raw)
		}
		if !strings.Contains(string(raw), `"AudioInfos"`) {
			t.Fatalf("submit body missing AudioInfos key:\n%s", raw)
		}
	default:
		t.Fatal("no submit reached the stub")
	}
	if len(submit.AudioInfos) != 1 {
		t.Fatalf("AudioInfos = %v, want exactly one entry", submit.AudioInfos)
	}
	if submit.AudioInfos[0]["AudioUrl"] != "https://assets.example/original.mp3" {
		t.Fatalf("AudioUrl = %v, want the resolved reference address", submit.AudioInfos[0]["AudioUrl"])
	}
	if _, hasType := submit.AudioInfos[0]["Type"]; hasType {
		t.Fatalf("AudioInfos[0] carries a Type key; the official data-structure page shape is AudioUrl-only")
	}
	if submit.ModelName != "MiniMaxMusic" {
		t.Fatalf("ModelName = %q", submit.ModelName)
	}
}

// 无 audio_ref 的提交体逐字节不带 AudioInfos 键(60/62 号票既有形态零改动)。
func TestVODAdaptorMusicSubmitOmittedAudioRefStaysByteStable(t *testing.T) {
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

// mps 族 MusicSubmit 遇 audio_ref → 请求级拒绝(errAudioRefUnsupported),
// 不拨号、不产生提交体。
func TestMPSAdaptorMusicSubmitRefusesAudioRef(t *testing.T) {
	_, srv := newMPSStub(t)
	a := newMPSAdaptor()

	payload := []byte(`{"model":"MiniMaxMusic 3.0","prompt":"cover","audio_ref":"https://assets.example/original.mp3"}`)
	upstream, err := a.MusicSubmit(context.Background(), mpsTestChannel(srv.URL), payload)
	if !errors.Is(err, errAudioRefUnsupported) {
		t.Fatalf("err = %v, want errAudioRefUnsupported", err)
	}
	if upstream != nil {
		t.Fatalf("upstream = %+v, want nil (refused before dialing)", upstream)
	}
}

// 对照面:不带 audio_ref 的 mps 提交照常受理(拒绝只盯参考字段)。
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
		if strings.Contains(string(raw), "audio_ref") {
			t.Fatalf("mps submit body leaked audio_ref:\n%s", raw)
		}
	default:
		t.Fatal("no submit reached the stub")
	}
}
