package relay

// tencent-vod adaptor(20 号票)的单元测试:TC3 调用形态、OpenAI→VOD 参数
// 翻译、异步轮询归一(处理中/成功/失败/业务错)、edits multipart →
// FileInfos Base64、尺寸映射表。桩是本地 httptest 服务器,按 X-TC-Action
// 分发 CreateAigcImageTask / DescribeTaskDetail 两个动作。

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gachal/InfiniteChance/internal/channel"
)

// vodTestChannel is one tencent-vod channel pointed at a stub endpoint.
func vodTestChannel(baseURL string) channel.Channel {
	return channel.Channel{
		Name: "vod-stub", Type: channel.TypeTencentVod, BaseURL: baseURL,
		Config: map[string]string{
			"secret_id":  "AKIDtest",
			"secret_key": "vodtestkey",
			"sub_app_id": "1500000000",
			"region":     "ap-guangzhou",
		},
		Enabled: true,
	}
}

// vodStub answers the two VOD actions the adaptor issues. submits records
// every CreateAigcImageTask body; polls returns canned DescribeTaskDetail
// bodies in order (last one repeats).
type vodStub struct {
	submits chan []byte
	polls   []string
	calls   int
}

func (s *vodStub) handler(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	switch r.Header.Get("X-TC-Action") {
	case "CreateAigcImageTask":
		if s.submits != nil {
			s.submits <- body
		}
		fmt.Fprint(w, `{"Response":{"TaskId":"task-123"}}`)
	case "DescribeTaskDetail":
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

func newVODStub(t *testing.T, polls ...string) (*vodStub, *httptest.Server) {
	t.Helper()
	stub := &vodStub{submits: make(chan []byte, 4), polls: polls}
	srv := httptest.NewServer(http.HandlerFunc(stub.handler))
	t.Cleanup(srv.Close)
	return stub, srv
}

func fastVODAdaptor(baseURL string) *vodAdaptor {
	a := newVODAdaptor()
	a.pollInterval = time.Millisecond
	a.budget = 2 * time.Second
	a.Client = &http.Client{}
	return a
}

func TestVODAdaptorGenerationsHappyPath(t *testing.T) {
	stub, srv := newVODStub(t,
		`{"Response":{"AigcImageTask":{"Status":"PROCESSING"}}}`,
		`{"Response":{"AigcImageTask":{"Status":"SUCCESS","Output":{"FileInfos":[{"FileUrl":"https://cdn/a.png"},{"FileUrl":"https://cdn/b.png"}]}}}}`)

	a := fastVODAdaptor(srv.URL)
	payload := []byte(`{"model":"OG image2.5_sunburst","prompt":"a cat under moonlight","n":2,"size":"1536x1024"}`)
	upstream, err := a.ImagesGenerations(context.Background(), vodTestChannel(srv.URL), payload)
	if err != nil {
		t.Fatalf("ImagesGenerations: %v", err)
	}
	if !upstream.OK || upstream.Status != http.StatusOK {
		t.Fatalf("upstream = %d ok=%v body=%s", upstream.Status, upstream.OK, upstream.Body)
	}

	// 提交体:模型串拆分、参数翻译、SubAppId、张数。
	var submit struct {
		SubAppID     int64          `json:"SubAppId"`
		ModelName    string         `json:"ModelName"`
		ModelVer     string         `json:"ModelVersion"`
		Prompt       string         `json:"Prompt"`
		OutputConfig map[string]any `json:"OutputConfig"`
	}
	select {
	case raw := <-stub.submits:
		if err := json.Unmarshal(raw, &submit); err != nil {
			t.Fatalf("submit body: %v (%s)", err, raw)
		}
	default:
		t.Fatal("no submit reached the stub")
	}
	if submit.ModelName != "OG" || submit.ModelVer != "image2.5_sunburst" {
		t.Fatalf("model split = %q / %q", submit.ModelName, submit.ModelVer)
	}
	if submit.Prompt != "a cat under moonlight" {
		t.Fatalf("prompt = %q", submit.Prompt)
	}
	if submit.SubAppID != 1500000000 {
		t.Fatalf("SubAppId = %d", submit.SubAppID)
	}
	if submit.OutputConfig["Resolution"] != "1K" || submit.OutputConfig["AspectRatio"] != "4:3" {
		t.Fatalf("OutputConfig = %v", submit.OutputConfig)
	}
	if submit.OutputConfig["StorageMode"] != "Temporary" {
		t.Fatalf("StorageMode = %v", submit.OutputConfig["StorageMode"])
	}
	if n, ok := submit.OutputConfig["OutputImageCount"].(float64); !ok || int(n) != 2 {
		t.Fatalf("OutputImageCount = %v", submit.OutputConfig["OutputImageCount"])
	}

	// 响应体:OpenAI images 形状,两张图。
	clientBody, delivered, err := a.NormalizeImages("og-image-2.5", upstream.Body)
	if err != nil || delivered != 2 {
		t.Fatalf("NormalizeImages = %d, %v (%s)", delivered, err, clientBody)
	}
	var images struct {
		Created int64 `json:"created"`
		Data    []struct {
			URL string `json:"url"`
		} `json:"data"`
	}
	if err := json.Unmarshal(clientBody, &images); err != nil {
		t.Fatalf("client body: %v (%s)", err, clientBody)
	}
	if len(images.Data) != 2 || images.Data[0].URL != "https://cdn/a.png" {
		t.Fatalf("data = %+v", images.Data)
	}
	if images.Created == 0 {
		t.Fatal("created missing")
	}
}

func TestVODAdaptorRejectsOverEightImages(t *testing.T) {
	_, srv := newVODStub(t)
	a := fastVODAdaptor(srv.URL)
	upstream, err := a.ImagesGenerations(context.Background(), vodTestChannel(srv.URL),
		[]byte(`{"model":"OG image2.5_sunburst","prompt":"x","n":9}`))
	if err != nil {
		t.Fatalf("ImagesGenerations: %v", err)
	}
	if upstream.OK || upstream.Status != http.StatusBadRequest {
		t.Fatalf("n=9 should refuse with 400, got %d (%s)", upstream.Status, upstream.Body)
	}
	if !strings.Contains(string(upstream.Body), "between 1 and 8") {
		t.Fatalf("refusal body = %s", upstream.Body)
	}
}

func TestVODAdaptorMapsBusinessErrors(t *testing.T) {
	cases := []struct {
		code   string
		status int
	}{
		{"AuthFailure.SignatureFailure", http.StatusUnauthorized},
		{"AuthFailure.SecretIdNotFound", http.StatusUnauthorized},
		{"InvalidParameter.ModelName", http.StatusBadRequest},
		{"MissingParameter", http.StatusBadRequest},
		{"LimitExceeded.AccountConcurrency", http.StatusTooManyRequests},
		{"FailedOperation.InternalError", http.StatusBadGateway},
		{"WhoKnows.What", http.StatusBadGateway},
	}
	for _, tc := range cases {
		if got := vodErrorStatus(tc.code); got != tc.status {
			t.Errorf("vodErrorStatus(%q) = %d, want %d", tc.code, got, tc.status)
		}
	}

	// 提交阶段的业务错走同一归一:HTTP 200 + Response.Error。
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, `{"Response":{"Error":{"Code":"AuthFailure.SecretIdNotFound","Message":"secret id not found"}}}`)
	}))
	defer srv.Close()
	a := fastVODAdaptor(srv.URL)
	upstream, err := a.ImagesGenerations(context.Background(), vodTestChannel(srv.URL),
		[]byte(`{"model":"OG image2.5_sunburst","prompt":"x"}`))
	if err != nil {
		t.Fatalf("ImagesGenerations: %v", err)
	}
	if upstream.OK || upstream.Status != http.StatusUnauthorized {
		t.Fatalf("auth failure should map to 401, got %d (%s)", upstream.Status, upstream.Body)
	}
	if summary := a.ErrorSummary(upstream.Body); !strings.Contains(summary, "SecretIdNotFound") {
		t.Fatalf("summary = %q", summary)
	}
}

func TestVODAdaptorTaskFailure(t *testing.T) {
	_, srv := newVODStub(t,
		`{"Response":{"AigcImageTask":{"Status":"PROCESSING"}}}`,
		`{"Response":{"AigcImageTask":{"Status":"FAIL","ErrCode":"ContentModerationBlocked","Message":"prompt rejected"}}}`)
	a := fastVODAdaptor(srv.URL)
	upstream, err := a.ImagesGenerations(context.Background(), vodTestChannel(srv.URL),
		[]byte(`{"model":"OG image2.5_sunburst","prompt":"x"}`))
	if err != nil {
		t.Fatalf("ImagesGenerations: %v", err)
	}
	if upstream.OK || upstream.Status != http.StatusBadGateway {
		t.Fatalf("task failure should map to 502, got %d", upstream.Status)
	}
	if summary := a.ErrorSummary(upstream.Body); !strings.Contains(summary, "ContentModerationBlocked") {
		t.Fatalf("summary = %q", summary)
	}
}

func TestVODAdaptorMissingCredentialsRefuses(t *testing.T) {
	_, srv := newVODStub(t)
	a := fastVODAdaptor(srv.URL)
	ch := vodTestChannel(srv.URL)
	ch.Config = map[string]string{"sub_app_id": "1"}
	upstream, err := a.ImagesGenerations(context.Background(), ch,
		[]byte(`{"model":"OG image2.5_sunburst","prompt":"x"}`))
	if err != nil {
		t.Fatalf("ImagesGenerations: %v", err)
	}
	// 凭据缺失 = 渠道配置坏:401 语义,不换道不记熔断账。
	if upstream.OK || upstream.Status != http.StatusUnauthorized {
		t.Fatalf("missing credentials should refuse 401, got %d", upstream.Status)
	}
}

func TestVODAdaptorEditsBuildsBase64FileInfos(t *testing.T) {
	stub, srv := newVODStub(t,
		`{"Response":{"AigcImageTask":{"Status":"SUCCESS","Output":{"FileInfos":[{"FileUrl":"https://cdn/edited.png"}]}}}}`)
	a := fastVODAdaptor(srv.URL)

	// 编辑请求的 multipart:两个图片分节 + 文本字段(model 已被 relay 重写)。
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	_ = mw.WriteField("model", "OG image2.5_sunburst")
	_ = mw.WriteField("prompt", "make it snow")
	_ = mw.WriteField("n", "1")
	_ = mw.WriteField("size", "1024x1024")
	for i, content := range []string{"fake-png-bytes-1", "fake-png-bytes-2"} {
		fw, _ := mw.CreateFormFile("image", fmt.Sprintf("ref-%d.png", i))
		_, _ = fw.Write([]byte(content))
	}
	_ = mw.Close()

	upstream, err := a.ImagesEdits(context.Background(), vodTestChannel(srv.URL), mw.FormDataContentType(), buf.Bytes())
	if err != nil || !upstream.OK {
		t.Fatalf("ImagesEdits = ok:%v err:%v body:%s", upstream.OK, err, upstream.Body)
	}

	var submit struct {
		ModelName string `json:"ModelName"`
		Prompt    string `json:"Prompt"`
		FileInfos []struct {
			Type   string `json:"Type"`
			Base64 string `json:"Base64"`
		} `json:"FileInfos"`
	}
	select {
	case raw := <-stub.submits:
		if err := json.Unmarshal(raw, &submit); err != nil {
			t.Fatalf("submit body: %v (%s)", err, raw)
		}
	default:
		t.Fatal("no submit reached the stub")
	}
	if submit.ModelName != "OG" || submit.Prompt != "make it snow" {
		t.Fatalf("model/prompt = %q / %q", submit.ModelName, submit.Prompt)
	}
	if len(submit.FileInfos) != 2 {
		t.Fatalf("FileInfos len = %d, want 2", len(submit.FileInfos))
	}
	want1 := base64.StdEncoding.EncodeToString([]byte("fake-png-bytes-1"))
	if submit.FileInfos[0].Type != "Base64" || submit.FileInfos[0].Base64 != want1 {
		t.Fatalf("FileInfos[0] = %+v", submit.FileInfos[0])
	}
}

func TestVODTaskResultWalksOutputOnly(t *testing.T) {
	body := []byte(`{"Response":{"AigcImageTask":{
		"Status":"SUCCESS",
		"Input":{"FileInfos":[{"FileUrl":"https://input-ref/must-ignore.png"}]},
		"Output":{"FileInfos":[{"FileUrl":"https://out/1.png"}]}}}}`)
	status, urls, failMsg := vodTaskResult(body)
	if status != "SUCCESS" || len(urls) != 1 || urls[0] != "https://out/1.png" || failMsg != "" {
		t.Fatalf("status=%q urls=%v fail=%q", status, urls, failMsg)
	}

	// 平铺形态(无 Aigc 子对象)与顶层 Status 兜底。
	flat := []byte(`{"Response":{"Status":"FINISH","FileInfos":[{"FileUrl":"https://flat/1.png"}]}}`)
	status, urls, _ = vodTaskResult(flat)
	if status != "FINISH" || len(urls) != 1 {
		t.Fatalf("flat status=%q urls=%v", status, urls)
	}
}

func TestVODSizeConfig(t *testing.T) {
	cases := []struct {
		size               string
		resolution, aspect string
	}{
		{"1024x1024", "1K", "1:1"},
		{"1536x1024", "1K", "4:3"},
		{"1024x1536", "1K", "3:4"},
		{"2048x2048", "2K", "1:1"},
		{"2048x1152", "2K", "16:9"}, // 21 号票:长边判定,横版 2048 系是 2K
		{"1152x2048", "2K", "9:16"},
		{"3072x2048", "2K", "4:3"},
		{"768x1280", "1K", "9:16"},
		{"1792x768", "1K", "21:9"},
		{"auto", "1K", "adaptive"},
		{"", "1K", "adaptive"},
		{"not-a-size", "1K", "adaptive"},
		{"0x0", "1K", "adaptive"},
	}
	for _, tc := range cases {
		resolution, aspect := vodSizeConfig(tc.size)
		if resolution != tc.resolution || aspect != tc.aspect {
			t.Errorf("vodSizeConfig(%q) = %q,%q want %q,%q", tc.size, resolution, aspect, tc.resolution, tc.aspect)
		}
	}
}

func TestVODAdaptorChatAndVideoRefuse(t *testing.T) {
	_, srv := newVODStub(t)
	a := fastVODAdaptor(srv.URL)
	ch := vodTestChannel(srv.URL)
	if _, err := a.ChatCompletions(context.Background(), ch, nil); err == nil {
		t.Error("ChatCompletions should refuse on the VOD adaptor")
	}
	if _, err := a.VideosSubmit(context.Background(), ch, nil); err == nil {
		t.Error("VideosSubmit should refuse on the VOD adaptor")
	}
}

// 21 号票:generations 带 image(URL)即图生图——URL 型 FileInfos(腾讯
// 自拉)、顺序保持;ratio 显式优先于 size 推导;约束(http(s) only、
// data: 拒、≤9 张)在提交前裁。
func TestVODAdaptorGenerationsWithImageRefs(t *testing.T) {
	stub, srv := newVODStub(t,
		`{"Response":{"AigcImageTask":{"Status":"SUCCESS","Output":{"FileInfos":[{"FileUrl":"https://cdn/out.png"}]}}}}`)
	a := fastVODAdaptor(srv.URL)

	payload := []byte(`{
		"model":"OG image2.5_flare_low",
		"prompt":"图1 保持人物五官,图2 是场景参考",
		"n":1,
		"size":"1024x1024",
		"ratio":"16:9",
		"image":["https://example.com/ref-1.png","https://example.com/ref-2.jpg"]
	}`)
	upstream, err := a.ImagesGenerations(context.Background(), vodTestChannel(srv.URL), payload)
	if err != nil || !upstream.OK {
		t.Fatalf("ImagesGenerations = ok:%v err:%v body:%s", upstream.OK, err, upstream.Body)
	}

	var submit struct {
		OutputConfig map[string]any `json:"OutputConfig"`
		FileInfos    []struct {
			Type string `json:"Type"`
			Url  string `json:"Url"`
		} `json:"FileInfos"`
	}
	select {
	case raw := <-stub.submits:
		if err := json.Unmarshal(raw, &submit); err != nil {
			t.Fatalf("submit body: %v (%s)", err, raw)
		}
	default:
		t.Fatal("no submit reached the stub")
	}
	if len(submit.FileInfos) != 2 ||
		submit.FileInfos[0].Type != "Url" || submit.FileInfos[0].Url != "https://example.com/ref-1.png" ||
		submit.FileInfos[1].Url != "https://example.com/ref-2.jpg" {
		t.Fatalf("FileInfos = %+v", submit.FileInfos)
	}
	// size 1024x1024 推导 1:1,但 ratio 16:9 显式优先。
	if submit.OutputConfig["AspectRatio"] != "16:9" || submit.OutputConfig["Resolution"] != "1K" {
		t.Fatalf("OutputConfig = %v", submit.OutputConfig)
	}
}

func TestVODAdaptorSingleStringRefForm(t *testing.T) {
	stub, srv := newVODStub(t,
		`{"Response":{"AigcImageTask":{"Status":"SUCCESS","Output":{"FileInfos":[{"FileUrl":"https://cdn/out.png"}]}}}}`)
	a := fastVODAdaptor(srv.URL)
	upstream, err := a.ImagesGenerations(context.Background(), vodTestChannel(srv.URL),
		[]byte(`{"model":"OG image2.5_flare_low","prompt":"x","image":"https://example.com/one.png"}`))
	if err != nil || !upstream.OK {
		t.Fatalf("single-string form = ok:%v err:%v body:%s", upstream.OK, err, upstream.Body)
	}
	var submit struct {
		FileInfos []map[string]string `json:"FileInfos"`
	}
	select {
	case raw := <-stub.submits:
		_ = json.Unmarshal(raw, &submit)
	default:
		t.Fatal("no submit reached the stub")
	}
	if len(submit.FileInfos) != 1 || submit.FileInfos[0]["Url"] != "https://example.com/one.png" {
		t.Fatalf("FileInfos = %v", submit.FileInfos)
	}
}

func TestVODAdaptorImageRefConstraints(t *testing.T) {
	_, srv := newVODStub(t)
	a := fastVODAdaptor(srv.URL)

	cases := map[string]string{
		`{"model":"m","prompt":"x","image":"data:image/png;base64,AAAA"}`: "http(s)",
		`{"model":"m","prompt":"x","image":["https://a/1.png",""]}`:       "empty",
		`{"model":"m","prompt":"x","image":["  "]}`:                       "empty",
		`{"model":"m","prompt":"x","image":"ftp://a/1.png"}`:              "http(s)",
	}
	for body, want := range cases {
		upstream, err := a.ImagesGenerations(context.Background(), vodTestChannel(srv.URL), []byte(body))
		if err != nil || upstream.OK || upstream.Status != http.StatusBadRequest {
			t.Fatalf("body %s: got %d/%v, want 400", body, upstream.Status, err)
		}
		if !strings.Contains(string(upstream.Body), want) {
			t.Fatalf("body %s: refusal %s missing %q", body, upstream.Body, want)
		}
	}

	// 10 张参考图:超 9 上限。
	refs := make([]string, 10)
	for i := range refs {
		refs[i] = fmt.Sprintf("https://example.com/%d.png", i)
	}
	raw, _ := json.Marshal(map[string]any{"model": "m", "prompt": "x", "image": refs})
	upstream, err := a.ImagesGenerations(context.Background(), vodTestChannel(srv.URL), raw)
	if err != nil || upstream.OK || upstream.Status != http.StatusBadRequest {
		t.Fatalf("10 refs: got %d/%v, want 400", upstream.Status, err)
	}
	if !strings.Contains(string(upstream.Body), "at most 9") {
		t.Fatalf("10 refs refusal = %s", upstream.Body)
	}
}
