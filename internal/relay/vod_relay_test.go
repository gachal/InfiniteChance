package relay_test

// 20 号票的端到端测试:tencent-vod 渠道走完整 /v1/images/generations 与
// /v1/images/edits 骨架 —— 按渠道类型分发到 VOD adaptor、OpenAI 生图请求
// 翻译成 CreateAigcImageTask、异步轮询封装成同步响应、次轨按实交张数
// 结算。假 VOD 服务按 X-TC-Action 分发两个动作;若请求误落进 OpenAI
// adaptor(GET /models、POST /images/generations 路径)会 404,分发
// 正确性即刻可见。

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gachal/InfiniteChance/internal/channel"
	"github.com/gachal/InfiniteChance/internal/usage"
)

// fakeVOD answers one CreateAigcImageTask + one SUCCESS DescribeTaskDetail.
func fakeVOD(t *testing.T, urls ...string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.ReadAll(r.Body)
		switch r.Header.Get("X-TC-Action") {
		case "CreateAigcImageTask":
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `{"Response":{"TaskId":"task-e2e"}}`)
		case "DescribeTaskDetail":
			infos := make([]string, len(urls))
			for i, u := range urls {
				infos[i] = fmt.Sprintf(`{"FileUrl":%q}`, u)
			}
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprintf(w, `{"Response":{"AigcImageTask":{"Status":"SUCCESS","Output":{"FileInfos":[%s]}}}}`,
				strings.Join(infos, ","))
		default:
			// 落进 OpenAI adaptor 的请求会打到这里(路径式调用),404
			// 让测试立刻失败。
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func (e *relayEnv) seedVodChannel(t *testing.T, baseURL string) channel.Channel {
	t.Helper()
	ch, err := e.stores.channels.Create(context.Background(), channel.Channel{
		Name: "tencent-vod", Type: channel.TypeTencentVod, BaseURL: baseURL,
		Config: map[string]string{
			"secret_id": "AKIDe2e", "secret_key": "e2e-secret", "sub_app_id": "1500000000",
		},
		ModelMap:     map[string]string{"og-image-2.5": "OG image2.5_sunburst"},
		Capabilities: []channel.Capability{channel.CapImages},
		Priority:     10, Weight: 1, Enabled: true,
	})
	if err != nil {
		t.Fatalf("seed vod channel: %v", err)
	}
	return ch
}

func TestImagesGenerationsThroughTencentVodChannel(t *testing.T) {
	srv := fakeVOD(t, "https://cdn.example/vod-out.png")
	e := newRelayEnv(t, nil) // nil = 按渠道类型分发(本测试的主角)
	e.seedVodChannel(t, srv.URL)
	e.seedImagePrice(t, "og-image-2.5", nil) // $0.04/张,尺寸系数 ×1.0
	key, fullKey := e.seedKey(t, 400_000)

	w := e.postImages(t, fullKey, `{"model":"og-image-2.5","prompt":"月光下奔跑的猫","size":"1024x1024"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	var images struct {
		Data []struct {
			URL string `json:"url"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &images); err != nil {
		t.Fatalf("body is not images JSON: %v (%s)", err, w.Body.String())
	}
	if len(images.Data) != 1 || images.Data[0].URL != "https://cdn.example/vod-out.png" {
		t.Fatalf("data = %+v", images.Data)
	}

	// 结算:预扣 $0.04 × 1,实交 1 张,差额为零;用量行 unit=call。
	if got := e.balanceOf(t, key.ID); got != 400_000-40_000 {
		t.Fatalf("balance = %d, want 360000", got)
	}
	rows := e.usageRows(t)
	if len(rows) != 1 || rows[0].Unit != "call" || rows[0].ChargeMicros != 40_000 {
		t.Fatalf("usage rows = %+v", rows)
	}
	if rows[0].Status != usage.StatusSuccess || rows[0].UpstreamError != "" {
		t.Fatalf("usage status = %s, upstream_error = %q", rows[0].Status, rows[0].UpstreamError)
	}
}

func TestImagesEditsThroughTencentVodChannel(t *testing.T) {
	srv := fakeVOD(t, "https://cdn.example/vod-edited.png")
	e := newRelayEnv(t, nil)
	e.seedVodChannel(t, srv.URL)
	e.seedImagePrice(t, "og-image-2.5", nil)
	_, fullKey := e.seedKey(t, 400_000)

	// OpenAI edits 形状:multipart,一个图片分节 + 文本字段。
	body := &strings.Builder{}
	mw := multipart.NewWriter(body)
	_ = mw.WriteField("model", "og-image-2.5")
	_ = mw.WriteField("prompt", "让它下雪")
	fw, _ := mw.CreateFormFile("image", "ref.png")
	_, _ = fw.Write([]byte("fake-reference-image"))
	_ = mw.Close()

	req := httptest.NewRequest(http.MethodPost, "/v1/images/edits", strings.NewReader(body.String()))
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.Header.Set("Authorization", "Bearer "+fullKey)
	rec := httptest.NewRecorder()
	e.engine.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("edits status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "vod-edited.png") {
		t.Fatalf("edits body = %s", rec.Body.String())
	}
}
