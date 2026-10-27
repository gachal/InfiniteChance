package relay

// tencent-mps adaptor(60 号票)的单元测试:TC3 调用形态、网关音乐体 →
// CreateAigcAudioTask 参数翻译(模型串拆分、SceneType、StoreCosParam、
// is_instrumental、输出格式)、DescribeAigcAudioTask 轮询归一(任务态/产物
// /失败信息)、凭据缺口的 401 拒绝。桩是本地 httptest 服务器,按
// X-TC-Action 分发两个动作(vod_internal_test.go 同范式)。

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gachal/InfiniteChance/internal/channel"
)

// mpsTestChannel is one tencent-mps channel pointed at a stub endpoint.
func mpsTestChannel(baseURL string) channel.Channel {
	return channel.Channel{
		Name: "mps-stub", Type: channel.TypeTencentMps, BaseURL: baseURL,
		Config: map[string]string{
			"secret_id":  "AKIDtest",
			"secret_key": "mpstestkey",
			"bucket":     "music-1250000000",
			"region":     "ap-guangzhou",
		},
		Enabled: true,
	}
}

// mpsStub answers the two MPS actions the adaptor issues. submits records
// every CreateAigcAudioTask body; polls returns canned DescribeAigcAudioTask
// bodies in order (last one repeats).
type mpsStub struct {
	submits chan []byte
	polls   []string
	calls   int
}

func (s *mpsStub) handler(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	switch r.Header.Get("X-TC-Action") {
	case "CreateAigcAudioTask":
		if s.submits != nil {
			s.submits <- body
		}
		fmt.Fprint(w, `{"Response":{"TaskId":"audio-task-9"}}`)
	case "DescribeAigcAudioTask":
		s.calls++
		if len(s.polls) == 0 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		idx := min(s.calls-1, len(s.polls)-1)
		fmt.Fprint(w, s.polls[idx])
	default:
		w.WriteHeader(http.StatusNotFound)
	}
}

func newMPSStub(t *testing.T, polls ...string) (*mpsStub, *httptest.Server) {
	t.Helper()
	stub := &mpsStub{submits: make(chan []byte, 4), polls: polls}
	srv := httptest.NewServer(http.HandlerFunc(stub.handler))
	t.Cleanup(srv.Close)
	return stub, srv
}

func TestMPSAdaptorSubmitTranslatesGatewayBody(t *testing.T) {
	stub, srv := newMPSStub(t)
	a := newMPSAdaptor()

	payload := []byte(`{"model":"MiniMaxMusic 3.0","prompt":"轻快的电子爵士,带复古合成器"}`)
	upstream, err := a.MusicSubmit(context.Background(), mpsTestChannel(srv.URL), payload)
	if err != nil {
		t.Fatalf("MusicSubmit: %v", err)
	}
	if !upstream.OK || upstream.Status != http.StatusOK {
		t.Fatalf("upstream = %d ok=%v body=%s", upstream.Status, upstream.OK, upstream.Body)
	}

	var submit struct {
		ModelName            string         `json:"ModelName"`
		ModelVersion         string         `json:"ModelVersion"`
		SceneType            string         `json:"SceneType"`
		Prompt               string         `json:"Prompt"`
		StoreCosParam        map[string]any `json:"StoreCosParam"`
		AdditionalParameters string         `json:"AdditionalParameters"`
		ExtraParameters      map[string]any `json:"ExtraParameters"`
	}
	select {
	case raw := <-stub.submits:
		if err := json.Unmarshal(raw, &submit); err != nil {
			t.Fatalf("submit body: %v (%s)", err, raw)
		}
	default:
		t.Fatal("no submit reached the stub")
	}
	if submit.ModelName != "MiniMaxMusic" || submit.ModelVersion != "3.0" {
		t.Errorf("model = %q/%q, want MiniMaxMusic/3.0 (空格串拆分)", submit.ModelName, submit.ModelVersion)
	}
	if submit.SceneType != "music" {
		t.Errorf("SceneType = %q, want music", submit.SceneType)
	}
	if !strings.Contains(submit.Prompt, "电子爵士") {
		t.Errorf("Prompt = %q, want the style description verbatim", submit.Prompt)
	}
	if submit.StoreCosParam["CosBucketName"] != "music-1250000000" || submit.StoreCosParam["CosBucketRegion"] != "ap-guangzhou" {
		t.Errorf("StoreCosParam = %v, want the channel's output bucket/region", submit.StoreCosParam)
	}
	if submit.AdditionalParameters != `{"is_instrumental":true}` {
		t.Errorf("AdditionalParameters = %q, want the hardcoded is_instrumental for MiniMaxMusic", submit.AdditionalParameters)
	}
	if submit.ExtraParameters["OutputAudioFormat"] != "mp3" {
		t.Errorf("ExtraParameters = %v, want OutputAudioFormat mp3 (转存扩展名可预测)", submit.ExtraParameters)
	}

	// 提交应答:归一 {"task_id"}。
	var ok struct {
		TaskID string `json:"task_id"`
	}
	if err := json.Unmarshal(upstream.Body, &ok); err != nil || ok.TaskID != "audio-task-9" {
		t.Errorf("normalized submit body = %s, want {task_id:audio-task-9}", upstream.Body)
	}
}

func TestMPSAdaptorSubmitModelVariants(t *testing.T) {
	cases := []struct {
		upstream string
		name     string
		version  string
		extra    string
	}{
		{"Mureka instrumental_9.5", "Mureka", "instrumental_9.5", ""},
		{"GL", "GL", "", ""},
	}
	for _, tc := range cases {
		stub, srv := newMPSStub(t)
		a := newMPSAdaptor()
		payload, _ := json.Marshal(map[string]string{"model": tc.upstream, "prompt": "p"})
		if _, err := a.MusicSubmit(context.Background(), mpsTestChannel(srv.URL), payload); err != nil {
			t.Fatalf("MusicSubmit(%s): %v", tc.upstream, err)
		}
		var submit struct {
			ModelName            string `json:"ModelName"`
			ModelVersion         string `json:"ModelVersion"`
			AdditionalParameters string `json:"AdditionalParameters"`
		}
		select {
		case raw := <-stub.submits:
			if err := json.Unmarshal(raw, &submit); err != nil {
				t.Fatalf("submit body: %v", err)
			}
		default:
			t.Fatal("no submit reached the stub")
		}
		if submit.ModelName != tc.name || submit.ModelVersion != tc.version {
			t.Errorf("model(%q) = %q/%q, want %q/%q", tc.upstream, submit.ModelName, submit.ModelVersion, tc.name, tc.version)
		}
		if submit.AdditionalParameters != tc.extra {
			t.Errorf("AdditionalParameters(%q) = %q, want %q", tc.upstream, submit.AdditionalParameters, tc.extra)
		}
	}
}

func TestMPSAdaptorSubmitRejectsBadInput(t *testing.T) {
	a := newMPSAdaptor()

	// 缺凭据/桶的渠道:401 形拒绝,不打上游。
	broken := mpsTestChannel("http://127.0.0.1:1")
	broken.Config = map[string]string{"secret_id": "x", "secret_key": "y"}
	upstream, err := a.MusicSubmit(context.Background(), broken, []byte(`{"model":"GL","prompt":"p"}`))
	if err != nil {
		t.Fatalf("MusicSubmit on broken channel: %v", err)
	}
	if upstream.Status != http.StatusUnauthorized {
		t.Errorf("broken channel status = %d, want 401 refusal", upstream.Status)
	}

	// 空风格描述:400。
	_, srv := newMPSStub(t)
	upstream, err = a.MusicSubmit(context.Background(), mpsTestChannel(srv.URL), []byte(`{"model":"GL","prompt":"  "}`))
	if err != nil {
		t.Fatalf("MusicSubmit(empty prompt): %v", err)
	}
	if upstream.Status != http.StatusBadRequest {
		t.Errorf("empty prompt status = %d, want 400", upstream.Status)
	}

	// 超上限的风格描述:400(MPS 上限 2000 字)。
	long := strings.Repeat("雨", mpsMaxPromptRunes+1)
	upstream, err = a.MusicSubmit(context.Background(), mpsTestChannel(srv.URL),
		[]byte(`{"model":"GL","prompt":"`+long+`"}`))
	if err != nil {
		t.Fatalf("MusicSubmit(long prompt): %v", err)
	}
	if upstream.Status != http.StatusBadRequest {
		t.Errorf("long prompt status = %d, want 400", upstream.Status)
	}
}

func TestMPSAdaptorQueryNormalizesVendorAnswer(t *testing.T) {
	_, srv := newMPSStub(t,
		`{"Response":{"Status":"WAIT"}}`,
		`{"Response":{"Status":"RUN"}}`,
		`{"Response":{"Status":"DONE","AudioInfos":[{"Url":"https://music.cos.ap-guangzhou.myqcloud.com/a.mp3","Duration":183}]}}`)
	a := newMPSAdaptor()

	for _, want := range []struct {
		status   string
		audioURL string
	}{{"WAIT", ""}, {"RUN", ""}, {"DONE", "https://music.cos.ap-guangzhou.myqcloud.com/a.mp3"}} {
		upstream, err := a.MusicQuery(context.Background(), mpsTestChannel(srv.URL), "audio-task-9")
		if err != nil || !upstream.OK {
			t.Fatalf("MusicQuery: %v (body=%s)", err, upstreamBody(upstream))
		}
		var poll struct {
			TaskStatus string `json:"task_status"`
			AudioURL   string `json:"audio_url"`
			Error      *struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		if err := json.Unmarshal(upstream.Body, &poll); err != nil {
			t.Fatalf("poll body: %v (%s)", err, upstream.Body)
		}
		if poll.TaskStatus != want.status || poll.AudioURL != want.audioURL || poll.Error != nil {
			t.Errorf("poll = %+v, want status %q url %q no error", poll, want.status, want.audioURL)
		}
	}
}

func TestMPSAdaptorQueryCarriesFailureMessage(t *testing.T) {
	_, srv := newMPSStub(t, `{"Response":{"Status":"FAIL","Message":"tme audio url is empty"}}`)
	a := newMPSAdaptor()

	upstream, err := a.MusicQuery(context.Background(), mpsTestChannel(srv.URL), "audio-task-9")
	if err != nil || !upstream.OK {
		t.Fatalf("MusicQuery: %v (body=%s)", err, upstreamBody(upstream))
	}
	var poll struct {
		TaskStatus string `json:"task_status"`
		Error      *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(upstream.Body, &poll); err != nil {
		t.Fatalf("poll body: %v", err)
	}
	if poll.TaskStatus != "FAIL" || poll.Error == nil || poll.Error.Message != "tme audio url is empty" {
		t.Errorf("poll = %+v, want FAIL with the vendor message", poll)
	}
}

func upstreamBody(u *UpstreamResponse) string {
	if u == nil {
		return "(nil)"
	}
	return string(u.Body)
}
