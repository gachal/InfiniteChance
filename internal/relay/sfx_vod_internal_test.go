package relay

// 61 号票(音效生成,VOD Kling sfx)的 adaptor 单元测试:SfxSynthesis 是
// 同步方法 —— 提交 CreateAigcAudioTask(SceneType=sfx,Duration=seconds,
// 无 AdditionalParameters)后在 adaptor 内以独立预算(vodSfxPollInterval/
// vodSfxTotalBudget)轮询 DescribeTaskDetail 到终态:SUCCESS 归一
// {"audio":{"url"}}、FAIL 502、超预算 504(临时失败,relay 退款换道);
// text/seconds 在 adaptor 侧再校验一道(拨号之前 400)。

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"
)

// sfxHappyPolls 是音效任务的两拍剧本:处理中 → 成功交付 Output URL。
func sfxHappyPolls() []string {
	return []string{
		`{"Response":{"AigcAudioTask":{"Status":"PROCESSING"}}}`,
		`{"Response":{"AigcAudioTask":{"Status":"SUCCESS","Output":{"FileInfos":[{"FileUrl":"https://cdn/boom.mp3"}]}}}}`,
	}
}

// 61 号票:提交体形状 —— SceneType=sfx、模型串空格拆分、Prompt=text、
// Duration=seconds、OutputConfig(Temporary + mp3)、SubAppId,且不带
// AdditionalParameters(Kling 无需);终态 SUCCESS 归一 {"audio":{"url"}}。
func TestVODAdaptorSfxBuildsAigcAudioTask(t *testing.T) {
	stub, srv := newVODStub(t, sfxHappyPolls()...)
	a := fastVODAdaptor(srv.URL)
	upstream, err := a.SfxSynthesis(context.Background(), vodTestChannel(srv.URL),
		[]byte(`{"model":"Kling sfx","text":"雷声轰鸣,由远及近","seconds":5}`))
	if err != nil {
		t.Fatalf("SfxSynthesis: %v", err)
	}
	if !upstream.OK || upstream.Status != http.StatusOK {
		t.Fatalf("upstream = %d ok=%v body=%s", upstream.Status, upstream.OK, upstream.Body)
	}
	var delivered struct {
		Audio *struct {
			URL string `json:"url"`
		} `json:"audio"`
	}
	if err := json.Unmarshal(upstream.Body, &delivered); err != nil {
		t.Fatalf("delivered body: %v (%s)", err, upstream.Body)
	}
	if delivered.Audio == nil || delivered.Audio.URL != "https://cdn/boom.mp3" {
		t.Fatalf("delivered = %s, want audio.url of the Output FileUrl", upstream.Body)
	}

	var req struct {
		SubAppID             int64          `json:"SubAppId"`
		SceneType            string         `json:"SceneType"`
		ModelName            string         `json:"ModelName"`
		ModelVer             string         `json:"ModelVersion"`
		Prompt               string         `json:"Prompt"`
		Duration             int64          `json:"Duration"`
		OutputConfig         map[string]any `json:"OutputConfig"`
		AdditionalParameters *string        `json:"AdditionalParameters"`
	}
	select {
	case raw := <-stub.submits:
		if err := json.Unmarshal(raw, &req); err != nil {
			t.Fatalf("submit body: %v (%s)", err, raw)
		}
	default:
		t.Fatal("no submit reached the stub")
	}
	if req.SceneType != "sfx" || req.ModelName != "Kling" || req.ModelVer != "sfx" {
		t.Fatalf("scene/model = %q %q %q", req.SceneType, req.ModelName, req.ModelVer)
	}
	if req.Prompt != "雷声轰鸣,由远及近" {
		t.Fatalf("prompt = %q", req.Prompt)
	}
	if req.Duration != 5 {
		t.Fatalf("Duration = %d, want 5", req.Duration)
	}
	if req.SubAppID != 1500000000 {
		t.Fatalf("SubAppId = %d", req.SubAppID)
	}
	if req.OutputConfig["StorageMode"] != "Temporary" || req.OutputConfig["OutputAudioFormat"] != "mp3" {
		t.Fatalf("OutputConfig = %v", req.OutputConfig)
	}
	if req.AdditionalParameters != nil {
		t.Fatalf("AdditionalParameters = %q, want absent (Kling 无需,61 号票定案)", *req.AdditionalParameters)
	}
}

// 终态失败:FAIL → 502(上游失败,可换道);失败原因随体。
func TestVODAdaptorSfxTaskFailure(t *testing.T) {
	_, srv := newVODStub(t,
		`{"Response":{"AigcAudioTask":{"Status":"FAIL","ErrCode":"ContentModerationBlocked","Message":"prompt rejected"}}}`)
	a := fastVODAdaptor(srv.URL)
	upstream, err := a.SfxSynthesis(context.Background(), vodTestChannel(srv.URL),
		[]byte(`{"model":"Kling sfx","text":"x","seconds":3}`))
	if err != nil {
		t.Fatalf("SfxSynthesis: %v", err)
	}
	if upstream.OK || upstream.Status != http.StatusBadGateway {
		t.Fatalf("task failure should map to 502, got %d (%s)", upstream.Status, upstream.Body)
	}
	if summary := a.ErrorSummary(upstream.Body); !strings.Contains(summary, "ContentModerationBlocked") {
		t.Fatalf("summary = %q", summary)
	}
}

// 超预算:任务一直在处理中,budget 收紧后按 504 收尾 —— 临时失败,relay
// 退款换道(腾讯侧任务可能仍在跑,照生图超时同款已知泄漏)。
func TestVODAdaptorSfxBudgetTimeout(t *testing.T) {
	_, srv := newVODStub(t, `{"Response":{"AigcAudioTask":{"Status":"PROCESSING"}}}`)
	a := fastVODAdaptor(srv.URL)
	a.sfxPollInterval = time.Millisecond
	a.sfxBudget = 20 * time.Millisecond
	upstream, err := a.SfxSynthesis(context.Background(), vodTestChannel(srv.URL),
		[]byte(`{"model":"Kling sfx","text":"x","seconds":10}`))
	if err != nil {
		t.Fatalf("SfxSynthesis: %v", err)
	}
	if upstream.OK || upstream.Status != http.StatusGatewayTimeout {
		t.Fatalf("budget timeout should map to 504, got %d (%s)", upstream.Status, upstream.Body)
	}
}

// text/seconds 校验:空 text / 超 2000 runes / 秒数出 1–30 域,都在拨号
// 之前 400(网关 handler 已拦一道,adaptor 是第二道防线)。
func TestVODAdaptorSfxValidation(t *testing.T) {
	_, srv := newVODStub(t, sfxHappyPolls()...)
	a := fastVODAdaptor(srv.URL)
	ch := vodTestChannel(srv.URL)
	long := strings.Repeat("雨", 2001)
	cases := map[string]string{
		`{"model":"Kling sfx","text":"  ","seconds":5}`:           "'text'",
		`{"model":"Kling sfx","text":"` + long + `","seconds":5}`: "2000",
		`{"model":"Kling sfx","text":"boom","seconds":0}`:         "'seconds'",
		`{"model":"Kling sfx","text":"boom","seconds":31}`:        "'seconds'",
		`{"model":"  ","text":"boom","seconds":5}`:                "model",
	}
	for body, want := range cases {
		upstream, err := a.SfxSynthesis(context.Background(), ch, []byte(body))
		if err != nil || upstream.OK || upstream.Status != http.StatusBadRequest {
			t.Fatalf("body %s: got %d/%v, want 400", body, upstream.Status, err)
		}
		if !strings.Contains(string(upstream.Body), want) {
			t.Fatalf("body %s: refusal %s missing %q", body, upstream.Body, want)
		}
	}
}
