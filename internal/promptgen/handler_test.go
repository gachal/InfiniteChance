package promptgen_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/gachal/InfiniteChance/internal/asset"
	"github.com/gachal/InfiniteChance/internal/canvas"
	"github.com/gachal/InfiniteChance/internal/pricing"
	"github.com/gachal/InfiniteChance/internal/promptgen"
	"github.com/gachal/InfiniteChance/internal/prompttemplate"
)

// ---- fakes ----

type fakeTemplates struct {
	byID map[int64]prompttemplate.Template
}

func (f fakeTemplates) Get(_ context.Context, id int64) (prompttemplate.Template, error) {
	t, ok := f.byID[id]
	if !ok {
		return prompttemplate.Template{}, prompttemplate.ErrNotFound
	}
	return t, nil
}

func (f fakeTemplates) ListEnabled(_ context.Context) ([]prompttemplate.Template, error) {
	ids := make([]int64, 0, len(f.byID))
	for id := range f.byID {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	out := make([]prompttemplate.Template, 0)
	for _, id := range ids {
		if t := f.byID[id]; t.Enabled {
			out = append(out, t)
		}
	}
	return out, nil
}

type fakeCanvases struct{}

func (fakeCanvases) Get(_ context.Context, id int64) (canvas.Canvas, error) {
	if id == 7 {
		return canvas.Canvas{ID: 7, Name: "测试画布", Version: 1}, nil
	}
	return canvas.Canvas{}, canvas.ErrNotFound
}

type fakePrices struct {
	byModel map[string]pricing.Price
}

func (f fakePrices) List(context.Context) ([]pricing.Price, error) {
	out := make([]pricing.Price, 0, len(f.byModel))
	for _, p := range f.byModel {
		out = append(out, p)
	}
	return out, nil
}

func (f fakePrices) ByModel(_ context.Context, model string) (pricing.Price, error) {
	p, ok := f.byModel[model]
	if !ok {
		return pricing.Price{}, pricing.ErrNotFound
	}
	return p, nil
}

type stubGateway struct {
	requests []promptgen.ChatRequest
	content  string
	err      error
}

func (f *stubGateway) GenerateChat(_ context.Context, req promptgen.ChatRequest) (promptgen.ChatResult, error) {
	f.requests = append(f.requests, req)
	if f.err != nil {
		return promptgen.ChatResult{}, f.err
	}
	return promptgen.ChatResult{Content: f.content}, nil
}

type fakeAssets struct {
	byID map[int64]asset.Asset
}

func (f fakeAssets) Get(_ context.Context, id int64) (asset.Asset, error) {
	a, ok := f.byID[id]
	if !ok {
		return asset.Asset{}, asset.ErrNotFound
	}
	return a, nil
}

// ---- test rig: routes wired exactly as canvas/server/main.go does ----

type envParams struct {
	templates     fakeTemplates
	assets        fakeAssets
	gateway       promptgen.Gateway
	publicBaseURL func(ctx context.Context) string
}

type handlerEnv struct {
	params *envParams
	server *httptest.Server
}

func newHandlerEnv(t *testing.T, mutate func(*envParams)) handlerEnv {
	t.Helper()
	gin.SetMode(gin.TestMode)

	params := &envParams{
		templates: fakeTemplates{byID: map[int64]prompttemplate.Template{
			1: {ID: 1, Name: "文生图-中文", Template: "请为主题「{topic}」写一段英文文生图提示词,只输出提示词本身。", Enabled: true},
			2: {ID: 2, Name: "已停用模板", Template: "{topic}", Enabled: false},
		}},
		assets: fakeAssets{byID: map[int64]asset.Asset{
			5: {ID: 5, Kind: asset.KindVideo, CanvasID: 7, URL: "https://cdn.example.com/generated.mp4"},
			6: {ID: 6, Kind: asset.KindImage, CanvasID: 7, URL: "https://cdn.example.com/pic.png"},
			7: {ID: 7, Kind: asset.KindVideo, CanvasID: 7, URL: "data:video/mp4;base64,AAAA"},
		}},
		gateway: &stubGateway{content: "a neon cyberpunk city at dusk"},
	}
	if mutate != nil {
		mutate(params)
	}

	engine := gin.New()
	promptgen.RegisterRoutes(engine.Group("/canvases"), &promptgen.Handlers{
		Templates: params.templates,
		Canvases:  fakeCanvases{},
		Assets:    params.assets,
		Models: fakePrices{byModel: map[string]pricing.Price{
			"chat-m": {PublicModel: "chat-m", Unit: pricing.UnitToken, Token: &pricing.TokenPrice{}},
			"img-m":  {PublicModel: "img-m", Unit: pricing.UnitCall, Call: &pricing.CallPrice{}},
		}},
		Gateway:       params.gateway,
		PublicBaseURL: params.publicBaseURL,
	})
	promptgen.RegisterCatalogRoutes(engine.Group("/prompt-templates"),
		&promptgen.CatalogHandlers{Templates: params.templates})
	promptgen.RegisterModelRoutes(engine.Group("/prompt-models"),
		&promptgen.ModelHandlers{Prices: fakePrices{byModel: map[string]pricing.Price{
			"chat-b": {PublicModel: "chat-b", Unit: pricing.UnitToken, Token: &pricing.TokenPrice{}},
			"chat-a": {PublicModel: "chat-a", Unit: pricing.UnitToken, Token: &pricing.TokenPrice{}},
			"img-m":  {PublicModel: "img-m", Unit: pricing.UnitCall, Call: &pricing.CallPrice{}},
		}}})

	server := httptest.NewServer(engine)
	t.Cleanup(server.Close)
	return handlerEnv{params: params, server: server}
}

func (env handlerEnv) do(t *testing.T, method, path string, body any) (*http.Response, string) {
	t.Helper()
	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal body: %v", err)
		}
		reader = strings.NewReader(string(raw))
	}
	req, err := http.NewRequest(method, env.server.URL+path, reader)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	res, err := env.server.Client().Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	t.Cleanup(func() { _ = res.Body.Close() })
	raw, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	return res, string(raw)
}

func errorBody(t *testing.T, raw string) (code, message string) {
	t.Helper()
	var parsed struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
		t.Fatalf("parse error body %q: %v", raw, err)
	}
	return parsed.Error.Code, parsed.Error.Message
}

// ---- generate-prompt ----

func TestGenerateRendersTemplateAndRelaysThroughGateway(t *testing.T) {
	env := newHandlerEnv(t, nil)

	res, raw := env.do(t, http.MethodPost, "/canvases/7/generate-prompt", map[string]any{
		"node_id":     "prompt-1-1",
		"template_id": 1,
		"topic":       "赛博朋克城市",
		"model":       "chat-m",
	})
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", res.StatusCode, raw)
	}
	var got struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal([]byte(raw), &got); err != nil {
		t.Fatalf("parse body: %v", err)
	}
	if got.Text != "a neon cyberpunk city at dusk" {
		t.Errorf("text = %q", got.Text)
	}

	gateway := env.params.gateway.(*stubGateway)
	if len(gateway.requests) != 1 {
		t.Fatalf("gateway calls = %d, want 1", len(gateway.requests))
	}
	req := gateway.requests[0]
	if req.Model != "chat-m" {
		t.Errorf("model = %q", req.Model)
	}
	// 模板改动即时生效的依据:内容按当次请求的模板渲染。
	if req.Content != "请为主题「赛博朋克城市」写一段英文文生图提示词,只输出提示词本身。" {
		t.Errorf("content = %q, want topic filled into the template", req.Content)
	}
	if req.Source != "canvas=7 node=prompt-1-1 gen=prompt" {
		t.Errorf("source = %q, want canvas origin mark", req.Source)
	}
}

func TestGenerateWithoutNodeIDStillMarksCanvasSource(t *testing.T) {
	env := newHandlerEnv(t, nil)

	res, raw := env.do(t, http.MethodPost, "/canvases/7/generate-prompt", map[string]any{
		"template_id": 1, "topic": "海边的日落", "model": "chat-m",
	})
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status = %d; body = %s", res.StatusCode, raw)
	}
	req := env.params.gateway.(*stubGateway).requests[0]
	if req.Source != "canvas=7 gen=prompt" {
		t.Errorf("source = %q, want canvas-only mark", req.Source)
	}
}

func TestGenerateOnMissingCanvasAnswers404(t *testing.T) {
	env := newHandlerEnv(t, nil)
	res, raw := env.do(t, http.MethodPost, "/canvases/99/generate-prompt", map[string]any{
		"template_id": 1, "topic": "任意", "model": "chat-m",
	})
	if res.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want 404; body = %s", res.StatusCode, raw)
	}
	code, _ := errorBody(t, raw)
	if code != "not_found" {
		t.Errorf("code = %q", code)
	}
}

func TestGenerateWithUnknownTemplateAnswers404(t *testing.T) {
	env := newHandlerEnv(t, nil)
	res, raw := env.do(t, http.MethodPost, "/canvases/7/generate-prompt", map[string]any{
		"template_id": 42, "topic": "任意", "model": "chat-m",
	})
	if res.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want 404; body = %s", res.StatusCode, raw)
	}
}

func TestGenerateWithDisabledTemplateAnswers400(t *testing.T) {
	env := newHandlerEnv(t, nil)
	res, raw := env.do(t, http.MethodPost, "/canvases/7/generate-prompt", map[string]any{
		"template_id": 2, "topic": "任意", "model": "chat-m",
	})
	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body = %s", res.StatusCode, raw)
	}
	code, _ := errorBody(t, raw)
	if code != "template_disabled" {
		t.Errorf("code = %q, want template_disabled", code)
	}
}

func TestGenerateWithNonTokenModelAnswers400(t *testing.T) {
	cases := []struct {
		name  string
		model string
	}{
		{"call-track model", "img-m"},
		{"unpriced model", "no-such-model"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			env := newHandlerEnv(t, nil)
			res, raw := env.do(t, http.MethodPost, "/canvases/7/generate-prompt", map[string]any{
				"template_id": 1, "topic": "任意", "model": tc.model,
			})
			if res.StatusCode != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400; body = %s", res.StatusCode, raw)
			}
			code, _ := errorBody(t, raw)
			if code != "model_not_priced" {
				t.Errorf("code = %q, want model_not_priced", code)
			}
		})
	}
}

func TestGenerateValidation(t *testing.T) {
	cases := []struct {
		name string
		body map[string]any
	}{
		{"missing topic", map[string]any{"template_id": 1, "model": "chat-m"}},
		{"missing template_id", map[string]any{"topic": "任意", "model": "chat-m"}},
		{"missing model", map[string]any{"template_id": 1, "topic": "任意"}},
		{"oversized node_id", map[string]any{"template_id": 1, "topic": "任意", "model": "chat-m", "node_id": strings.Repeat("x", 200)}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			env := newHandlerEnv(t, nil)
			res, raw := env.do(t, http.MethodPost, "/canvases/7/generate-prompt", tc.body)
			if res.StatusCode != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400; body = %s", res.StatusCode, raw)
			}
		})
	}
}

func TestGenerateWithoutGatewayAnswers503(t *testing.T) {
	env := newHandlerEnv(t, func(p *envParams) { p.gateway = nil })
	res, raw := env.do(t, http.MethodPost, "/canvases/7/generate-prompt", map[string]any{
		"template_id": 1, "topic": "任意", "model": "chat-m",
	})
	if res.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503; body = %s", res.StatusCode, raw)
	}
	code, _ := errorBody(t, raw)
	if code != "gateway_unconfigured" {
		t.Errorf("code = %q, want gateway_unconfigured", code)
	}
}

func TestGenerateSurfacesGatewayFailureAsUpstreamError(t *testing.T) {
	env := newHandlerEnv(t, func(p *envParams) {
		p.gateway = &stubGateway{err: errors.New("gateway 402: 余额不足")}
	})
	res, raw := env.do(t, http.MethodPost, "/canvases/7/generate-prompt", map[string]any{
		"template_id": 1, "topic": "任意", "model": "chat-m",
	})
	if res.StatusCode != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502; body = %s", res.StatusCode, raw)
	}
	code, message := errorBody(t, raw)
	if code != "upstream_error" || !strings.Contains(message, "余额不足") {
		t.Errorf("error = %q/%q, want upstream_error with the reason", code, message)
	}
}

// ---- template catalog: admin edits reflect immediately ----

func TestTemplateCatalogListsEnabledOnlyAndReflectsAdminEdits(t *testing.T) {
	env := newHandlerEnv(t, nil)

	// 初始:启用中的模板可见,停用的不可见。
	res, raw := env.do(t, http.MethodGet, "/prompt-templates", nil)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", res.StatusCode)
	}
	var got struct {
		Templates []struct {
			ID   int64  `json:"id"`
			Name string `json:"name"`
		} `json:"templates"`
	}
	if err := json.Unmarshal([]byte(raw), &got); err != nil {
		t.Fatalf("parse body: %v", err)
	}
	if len(got.Templates) != 1 || got.Templates[0].ID != 1 {
		t.Fatalf("templates = %+v, want only template 1", got.Templates)
	}

	// 管理端新建 → 画布侧下一次请求即见。
	env.params.templates.byID[3] = prompttemplate.Template{ID: 3, Name: "新模板", Template: "{topic}", Enabled: true}
	_, raw = env.do(t, http.MethodGet, "/prompt-templates", nil)
	if err := json.Unmarshal([]byte(raw), &got); err != nil {
		t.Fatalf("parse body: %v", err)
	}
	if len(got.Templates) != 2 || got.Templates[1].ID != 3 {
		t.Fatalf("templates after create = %+v, want template 3 appended", got.Templates)
	}

	// 管理端停用 → 立即从目录消失。
	disabled := env.params.templates.byID[3]
	disabled.Enabled = false
	env.params.templates.byID[3] = disabled
	_, raw = env.do(t, http.MethodGet, "/prompt-templates", nil)
	if err := json.Unmarshal([]byte(raw), &got); err != nil {
		t.Fatalf("parse body: %v", err)
	}
	if len(got.Templates) != 1 {
		t.Fatalf("templates after disable = %+v, want template 3 gone", got.Templates)
	}

	// 管理端删除 → 立即消失,且用旧 id 生成会得到 404。
	delete(env.params.templates.byID, 1)
	_, raw = env.do(t, http.MethodGet, "/prompt-templates", nil)
	if err := json.Unmarshal([]byte(raw), &got); err != nil {
		t.Fatalf("parse body: %v", err)
	}
	if len(got.Templates) != 0 {
		t.Fatalf("templates after delete = %+v, want empty", got.Templates)
	}
	res, raw = env.do(t, http.MethodPost, "/canvases/7/generate-prompt", map[string]any{
		"template_id": 1, "topic": "任意", "model": "chat-m",
	})
	if res.StatusCode != http.StatusNotFound {
		t.Fatalf("generate with deleted template: status = %d, want 404", res.StatusCode)
	}
}

// ---- chat-model catalog ----

func TestPromptModelCatalogListsTokenTrackOnlySorted(t *testing.T) {
	env := newHandlerEnv(t, nil)
	res, raw := env.do(t, http.MethodGet, "/prompt-models", nil)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", res.StatusCode)
	}
	var got struct {
		Models []string `json:"models"`
	}
	if err := json.Unmarshal([]byte(raw), &got); err != nil {
		t.Fatalf("parse body: %v", err)
	}
	if len(got.Models) != 2 || got.Models[0] != "chat-a" || got.Models[1] != "chat-b" {
		t.Fatalf("models = %+v, want sorted token-track models only", got.Models)
	}
}

// ---- reverse-prompt (13 号票:视频反推提示词) ----

func TestReverseSendsVideoToGatewayAndReturnsPrompt(t *testing.T) {
	env := newHandlerEnv(t, nil)

	res, raw := env.do(t, http.MethodPost, "/canvases/7/reverse-prompt", map[string]any{
		"node_id":   "video-1-1",
		"video_url": "https://vendor.example.com/clip.mp4",
		"model":     "chat-m",
	})
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", res.StatusCode, raw)
	}
	var got struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal([]byte(raw), &got); err != nil {
		t.Fatalf("parse body: %v", err)
	}
	if got.Text != "a neon cyberpunk city at dusk" {
		t.Errorf("text = %q", got.Text)
	}

	gateway := env.params.gateway.(*stubGateway)
	if len(gateway.requests) != 1 {
		t.Fatalf("gateway calls = %d, want 1", len(gateway.requests))
	}
	req := gateway.requests[0]
	if req.Model != "chat-m" {
		t.Errorf("model = %q", req.Model)
	}
	if req.VideoURL != "https://vendor.example.com/clip.mp4" {
		t.Errorf("video url = %q, want the passed address", req.VideoURL)
	}
	if req.Content == "" {
		t.Errorf("content = empty, want the fixed reverse instruction")
	}
	if req.Source != "canvas=7 node=video-1-1 gen=video-prompt" {
		t.Errorf("source = %q, want the canvas origin mark", req.Source)
	}
}

func TestReverseWithoutNodeIDStillMarksCanvasSource(t *testing.T) {
	env := newHandlerEnv(t, nil)

	res, raw := env.do(t, http.MethodPost, "/canvases/7/reverse-prompt", map[string]any{
		"video_url": "https://vendor.example.com/clip.mp4", "model": "chat-m",
	})
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status = %d; body = %s", res.StatusCode, raw)
	}
	req := env.params.gateway.(*stubGateway).requests[0]
	if req.Source != "canvas=7 gen=video-prompt" {
		t.Errorf("source = %q, want canvas-only mark", req.Source)
	}
}

func TestReverseResolvesAssetContentReference(t *testing.T) {
	env := newHandlerEnv(t, nil)

	// 视频节点在厂商回 b64 时持有内容寻址路径:服务端解出素材行的地址,
	// 编辑器无需知道素材 URL 本体。
	res, raw := env.do(t, http.MethodPost, "/canvases/7/reverse-prompt", map[string]any{
		"video_url": "/api/assets/5/content", "model": "chat-m",
	})
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", res.StatusCode, raw)
	}
	req := env.params.gateway.(*stubGateway).requests[0]
	if req.VideoURL != "https://cdn.example.com/generated.mp4" {
		t.Errorf("video url = %q, want the asset's stored address", req.VideoURL)
	}
}

func TestReverseValidation(t *testing.T) {
	cases := []struct {
		name string
		body map[string]any
	}{
		{"missing video_url", map[string]any{"model": "chat-m"}},
		{"missing model", map[string]any{"video_url": "https://vendor.example.com/clip.mp4"}},
		{"refusing scheme", map[string]any{"video_url": "file:///etc/passwd", "model": "chat-m"}},
		{"oversized node_id", map[string]any{"video_url": "https://vendor.example.com/clip.mp4", "model": "chat-m", "node_id": strings.Repeat("x", 200)}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			env := newHandlerEnv(t, nil)
			res, raw := env.do(t, http.MethodPost, "/canvases/7/reverse-prompt", tc.body)
			if res.StatusCode != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400; body = %s", res.StatusCode, raw)
			}
		})
	}
}

func TestReverseWithMalformedAssetPathAnswers400(t *testing.T) {
	cases := []struct {
		name string
		ref  string
	}{
		{"non-numeric id", "/api/assets/not-a-number/content"},
		{"wrong suffix", "/api/assets/5/other"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			env := newHandlerEnv(t, nil)
			res, raw := env.do(t, http.MethodPost, "/canvases/7/reverse-prompt", map[string]any{
				"video_url": tc.ref, "model": "chat-m",
			})
			if res.StatusCode != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400; body = %s", res.StatusCode, raw)
			}
		})
	}
}

func TestReverseWithInlineVideoAssetAnswers400(t *testing.T) {
	env := newHandlerEnv(t, nil)
	res, raw := env.do(t, http.MethodPost, "/canvases/7/reverse-prompt", map[string]any{
		"video_url": "/api/assets/7/content", "model": "chat-m",
	})
	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body = %s", res.StatusCode, raw)
	}
	code, _ := errorBody(t, raw)
	if code != "video_inline_unsupported" {
		t.Errorf("code = %q, want video_inline_unsupported", code)
	}
}

func TestReverseWithInlineDataURIAnswersInlineUnsupported(t *testing.T) {
	// video_url 直接携带内联 data: URI 与素材落库的 b64 产物同码同因:
	// 都在解析边界拒绝,而非笼统的 invalid_request。
	env := newHandlerEnv(t, nil)
	res, raw := env.do(t, http.MethodPost, "/canvases/7/reverse-prompt", map[string]any{
		"video_url": "data:video/mp4;base64,AAAA", "model": "chat-m",
	})
	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body = %s", res.StatusCode, raw)
	}
	code, _ := errorBody(t, raw)
	if code != "video_inline_unsupported" {
		t.Errorf("code = %q, want video_inline_unsupported", code)
	}
}

func TestReverseWithUnknownAssetAnswers404(t *testing.T) {
	env := newHandlerEnv(t, nil)
	res, raw := env.do(t, http.MethodPost, "/canvases/7/reverse-prompt", map[string]any{
		"video_url": "/api/assets/99/content", "model": "chat-m",
	})
	if res.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want 404; body = %s", res.StatusCode, raw)
	}
	code, _ := errorBody(t, raw)
	if code != "asset_not_found" {
		t.Errorf("code = %q, want asset_not_found", code)
	}
}

func TestReverseWithNonVideoAssetAnswers400(t *testing.T) {
	env := newHandlerEnv(t, nil)
	res, raw := env.do(t, http.MethodPost, "/canvases/7/reverse-prompt", map[string]any{
		"video_url": "/api/assets/6/content", "model": "chat-m",
	})
	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body = %s", res.StatusCode, raw)
	}
	code, _ := errorBody(t, raw)
	if code != "asset_not_video" {
		t.Errorf("code = %q, want asset_not_video", code)
	}
}

func TestReverseWithNonTokenModelAnswers400(t *testing.T) {
	cases := []struct {
		name  string
		model string
	}{
		{"call-track model", "img-m"},
		{"unpriced model", "no-such-model"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			env := newHandlerEnv(t, nil)
			res, raw := env.do(t, http.MethodPost, "/canvases/7/reverse-prompt", map[string]any{
				"video_url": "https://vendor.example.com/clip.mp4", "model": tc.model,
			})
			if res.StatusCode != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400; body = %s", res.StatusCode, raw)
			}
			code, _ := errorBody(t, raw)
			if code != "model_not_priced" {
				t.Errorf("code = %q, want model_not_priced", code)
			}
		})
	}
}

func TestReverseOnMissingCanvasAnswers404(t *testing.T) {
	env := newHandlerEnv(t, nil)
	res, raw := env.do(t, http.MethodPost, "/canvases/99/reverse-prompt", map[string]any{
		"video_url": "https://vendor.example.com/clip.mp4", "model": "chat-m",
	})
	if res.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want 404; body = %s", res.StatusCode, raw)
	}
}

func TestReverseWithoutGatewayAnswers503(t *testing.T) {
	env := newHandlerEnv(t, func(p *envParams) { p.gateway = nil })
	res, raw := env.do(t, http.MethodPost, "/canvases/7/reverse-prompt", map[string]any{
		"video_url": "https://vendor.example.com/clip.mp4", "model": "chat-m",
	})
	if res.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503; body = %s", res.StatusCode, raw)
	}
	code, _ := errorBody(t, raw)
	if code != "gateway_unconfigured" {
		t.Errorf("code = %q, want gateway_unconfigured", code)
	}
}

func TestReverseSurfacesGatewayFailureAsUpstreamError(t *testing.T) {
	env := newHandlerEnv(t, func(p *envParams) {
		p.gateway = &stubGateway{err: errors.New("gateway 400: 该模型不支持视频输入")}
	})
	res, raw := env.do(t, http.MethodPost, "/canvases/7/reverse-prompt", map[string]any{
		"video_url": "https://vendor.example.com/clip.mp4", "model": "chat-m",
	})
	if res.StatusCode != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502; body = %s", res.StatusCode, raw)
	}
	code, message := errorBody(t, raw)
	if code != "upstream_error" || !strings.Contains(message, "不支持视频输入") {
		t.Errorf("error = %q/%q, want upstream_error with the reason", code, message)
	}
}

// ---- analyze(17 号票)----

func TestAnalyzeSendsVideoToGatewayAndReturnsText(t *testing.T) {
	env := newHandlerEnv(t, nil)

	res, raw := env.do(t, http.MethodPost, "/canvases/7/analyze", map[string]any{
		"node_id":    "video-1-1",
		"media_url":  "https://vendor.example.com/clip.mp4",
		"media_kind": "video",
		"model":      "chat-m",
	})
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", res.StatusCode, raw)
	}
	var got struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal([]byte(raw), &got); err != nil {
		t.Fatalf("parse body: %v", err)
	}
	if got.Text != "a neon cyberpunk city at dusk" {
		t.Errorf("text = %q", got.Text)
	}

	gateway := env.params.gateway.(*stubGateway)
	if len(gateway.requests) != 1 {
		t.Fatalf("gateway calls = %d, want 1", len(gateway.requests))
	}
	req := gateway.requests[0]
	if req.Model != "chat-m" {
		t.Errorf("model = %q", req.Model)
	}
	if req.VideoURL != "https://vendor.example.com/clip.mp4" {
		t.Errorf("video url = %q, want the passed address", req.VideoURL)
	}
	if req.ImageURL != "" {
		t.Errorf("image url = %q, want empty for a video analysis", req.ImageURL)
	}
	if req.Content == "" {
		t.Errorf("content = empty, want the fixed analysis instruction")
	}
	if req.Source != "canvas=7 node=video-1-1 gen=analyze" {
		t.Errorf("source = %q, want the canvas origin mark", req.Source)
	}
}

func TestAnalyzeImageKindSendsImageAndMarksSource(t *testing.T) {
	env := newHandlerEnv(t, nil)

	res, raw := env.do(t, http.MethodPost, "/canvases/7/analyze", map[string]any{
		"node_id":    "image-1-1",
		"media_url":  "https://vendor.example.com/pic.png",
		"media_kind": "image",
		"model":      "chat-m",
	})
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", res.StatusCode, raw)
	}
	req := env.params.gateway.(*stubGateway).requests[0]
	if req.ImageURL != "https://vendor.example.com/pic.png" {
		t.Errorf("image url = %q, want the passed address", req.ImageURL)
	}
	if req.VideoURL != "" {
		t.Errorf("video url = %q, want empty for an image analysis", req.VideoURL)
	}
	if req.Source != "canvas=7 node=image-1-1 gen=analyze" {
		t.Errorf("source = %q, want the canvas origin mark", req.Source)
	}
}

func TestAnalyzeWithoutNodeIDStillMarksCanvasSource(t *testing.T) {
	env := newHandlerEnv(t, nil)

	res, raw := env.do(t, http.MethodPost, "/canvases/7/analyze", map[string]any{
		"media_url": "https://vendor.example.com/clip.mp4", "media_kind": "video", "model": "chat-m",
	})
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status = %d; body = %s", res.StatusCode, raw)
	}
	req := env.params.gateway.(*stubGateway).requests[0]
	if req.Source != "canvas=7 gen=analyze" {
		t.Errorf("source = %q, want canvas-only mark", req.Source)
	}
}

func TestAnalyzeResolvesAssetContentReference(t *testing.T) {
	cases := []struct {
		name      string
		mediaKind string
		assetID   int64
		want      string
	}{
		{"video asset", "video", 5, "https://cdn.example.com/generated.mp4"},
		{"image asset", "image", 6, "https://cdn.example.com/pic.png"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			env := newHandlerEnv(t, nil)

			// 分析来源的媒体普遍持有内容寻址路径:服务端解出素材行的地址,
			// 编辑器无需知道素材 URL 本体。
			res, raw := env.do(t, http.MethodPost, "/canvases/7/analyze", map[string]any{
				"media_url":  fmt.Sprintf("/api/assets/%d/content", tc.assetID),
				"media_kind": tc.mediaKind,
				"model":      "chat-m",
			})
			if res.StatusCode != http.StatusOK {
				t.Fatalf("status = %d, want 200; body = %s", res.StatusCode, raw)
			}
			req := env.params.gateway.(*stubGateway).requests[0]
			got := req.VideoURL
			if tc.mediaKind == "image" {
				got = req.ImageURL
			}
			if got != tc.want {
				t.Errorf("resolved url = %q, want the asset's stored address", got)
			}
		})
	}
}

func TestAnalyzeValidation(t *testing.T) {
	cases := []struct {
		name string
		body map[string]any
	}{
		{"missing media_url", map[string]any{"media_kind": "video", "model": "chat-m"}},
		{"missing media_kind", map[string]any{"media_url": "https://vendor.example.com/clip.mp4", "model": "chat-m"}},
		{"unknown media_kind", map[string]any{"media_url": "https://vendor.example.com/clip.mp4", "media_kind": "audio", "model": "chat-m"}},
		{"missing model", map[string]any{"media_url": "https://vendor.example.com/clip.mp4", "media_kind": "video"}},
		{"refusing scheme", map[string]any{"media_url": "file:///etc/passwd", "media_kind": "video", "model": "chat-m"}},
		{"oversized node_id", map[string]any{"media_url": "https://vendor.example.com/clip.mp4", "media_kind": "video", "model": "chat-m", "node_id": strings.Repeat("x", 200)}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			env := newHandlerEnv(t, nil)
			res, raw := env.do(t, http.MethodPost, "/canvases/7/analyze", tc.body)
			if res.StatusCode != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400; body = %s", res.StatusCode, raw)
			}
		})
	}
}

func TestAnalyzeWithMalformedAssetPathAnswers400(t *testing.T) {
	cases := []struct {
		name string
		ref  string
	}{
		{"non-numeric id", "/api/assets/not-a-number/content"},
		{"wrong suffix", "/api/assets/5/other"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			env := newHandlerEnv(t, nil)
			res, raw := env.do(t, http.MethodPost, "/canvases/7/analyze", map[string]any{
				"media_url": tc.ref, "media_kind": "video", "model": "chat-m",
			})
			if res.StatusCode != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400; body = %s", res.StatusCode, raw)
			}
		})
	}
}

func TestAnalyzeWithInlineAssetAnswersMediaInlineUnsupported(t *testing.T) {
	// 素材行落的 b64 产物与直接携带的 data: URI 同码同因:都在解析边界以
	// media_inline_unsupported 拒绝(17 号票定的分析端点码)。
	cases := []struct {
		name string
		ref  string
	}{
		{"inline video asset", "/api/assets/7/content"},
		{"direct data uri", "data:video/mp4;base64,AAAA"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			env := newHandlerEnv(t, nil)
			res, raw := env.do(t, http.MethodPost, "/canvases/7/analyze", map[string]any{
				"media_url": tc.ref, "media_kind": "video", "model": "chat-m",
			})
			if res.StatusCode != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400; body = %s", res.StatusCode, raw)
			}
			code, _ := errorBody(t, raw)
			if code != "media_inline_unsupported" {
				t.Errorf("code = %q, want media_inline_unsupported", code)
			}
		})
	}
}

func TestAnalyzeWithUnknownAssetAnswers404(t *testing.T) {
	env := newHandlerEnv(t, nil)
	res, raw := env.do(t, http.MethodPost, "/canvases/7/analyze", map[string]any{
		"media_url": "/api/assets/99/content", "media_kind": "video", "model": "chat-m",
	})
	if res.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want 404; body = %s", res.StatusCode, raw)
	}
	code, _ := errorBody(t, raw)
	if code != "asset_not_found" {
		t.Errorf("code = %q, want asset_not_found", code)
	}
}

func TestAnalyzeWithKindMismatchedAssetAnswers400(t *testing.T) {
	cases := []struct {
		name      string
		mediaKind string
		assetID   int64
		wantCode  string
	}{
		{"image asked for as video", "video", 6, "asset_not_video"},
		{"video asked for as image", "image", 5, "asset_not_image"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			env := newHandlerEnv(t, nil)
			res, raw := env.do(t, http.MethodPost, "/canvases/7/analyze", map[string]any{
				"media_url":  fmt.Sprintf("/api/assets/%d/content", tc.assetID),
				"media_kind": tc.mediaKind,
				"model":      "chat-m",
			})
			if res.StatusCode != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400; body = %s", res.StatusCode, raw)
			}
			code, _ := errorBody(t, raw)
			if code != tc.wantCode {
				t.Errorf("code = %q, want %s", code, tc.wantCode)
			}
		})
	}
}

func TestAnalyzeWithNonTokenModelAnswers400(t *testing.T) {
	cases := []struct {
		name  string
		model string
	}{
		{"call-track model", "img-m"},
		{"unpriced model", "no-such-model"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			env := newHandlerEnv(t, nil)
			res, raw := env.do(t, http.MethodPost, "/canvases/7/analyze", map[string]any{
				"media_url": "https://vendor.example.com/clip.mp4", "media_kind": "video", "model": tc.model,
			})
			if res.StatusCode != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400; body = %s", res.StatusCode, raw)
			}
			code, _ := errorBody(t, raw)
			if code != "model_not_priced" {
				t.Errorf("code = %q, want model_not_priced", code)
			}
		})
	}
}

func TestAnalyzeOnMissingCanvasAnswers404(t *testing.T) {
	env := newHandlerEnv(t, nil)
	res, raw := env.do(t, http.MethodPost, "/canvases/99/analyze", map[string]any{
		"media_url": "https://vendor.example.com/clip.mp4", "media_kind": "video", "model": "chat-m",
	})
	if res.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want 404; body = %s", res.StatusCode, raw)
	}
}

func TestAnalyzeWithoutGatewayAnswers503(t *testing.T) {
	env := newHandlerEnv(t, func(p *envParams) { p.gateway = nil })
	res, raw := env.do(t, http.MethodPost, "/canvases/7/analyze", map[string]any{
		"media_url": "https://vendor.example.com/clip.mp4", "media_kind": "video", "model": "chat-m",
	})
	if res.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503; body = %s", res.StatusCode, raw)
	}
	code, _ := errorBody(t, raw)
	if code != "gateway_unconfigured" {
		t.Errorf("code = %q, want gateway_unconfigured", code)
	}
}

func TestAnalyzeSurfacesGatewayFailureAsUpstreamError(t *testing.T) {
	env := newHandlerEnv(t, func(p *envParams) {
		p.gateway = &stubGateway{err: errors.New("gateway 400: 该模型不支持视觉输入")}
	})
	res, raw := env.do(t, http.MethodPost, "/canvases/7/analyze", map[string]any{
		"media_url": "https://vendor.example.com/clip.mp4", "media_kind": "video", "model": "chat-m",
	})
	if res.StatusCode != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502; body = %s", res.StatusCode, raw)
	}
	code, message := errorBody(t, raw)
	if code != "upstream_error" || !strings.Contains(message, "不支持视觉输入") {
		t.Errorf("error = %q/%q, want upstream_error with the reason", code, message)
	}
}

// ---- 18 号票:「LLM 可达地址」解析顺序(自有公网地址优先、厂商原址回落)----

// TestReversePrefersPublicStorageAddress 验证转存过的素材在配置了公网
// 基址时优先走自有地址:厂商原址约 24h 过期,自有地址永久。
func TestReversePrefersPublicStorageAddress(t *testing.T) {
	env := newHandlerEnv(t, func(p *envParams) {
		p.assets.byID[5] = asset.Asset{
			ID: 5, Kind: asset.KindVideo, CanvasID: 7,
			URL:       "https://cdn.example.com/generated.mp4",
			ObjectKey: "canvases/7/ct_v/video.mp4",
		}
		p.publicBaseURL = func(context.Context) string { return "https://assets.example.org" }
	})

	res, raw := env.do(t, http.MethodPost, "/canvases/7/reverse-prompt", map[string]any{
		"video_url": "/api/assets/5/content", "model": "chat-m",
	})
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", res.StatusCode, raw)
	}
	req := env.params.gateway.(*stubGateway).requests[0]
	if req.VideoURL != "https://assets.example.org/canvases/7/ct_v/video.mp4" {
		t.Errorf("video url = %q, want the public storage address", req.VideoURL)
	}
}

// TestReverseWithoutPublicBaseFallsBackToVendorURL 验证未配置公网基址时
// 回落厂商原址 —— 19 号票落地前的现状行为。
func TestReverseWithoutPublicBaseFallsBackToVendorURL(t *testing.T) {
	env := newHandlerEnv(t, func(p *envParams) {
		p.assets.byID[5] = asset.Asset{
			ID: 5, Kind: asset.KindVideo, CanvasID: 7,
			URL:       "https://cdn.example.com/generated.mp4",
			ObjectKey: "canvases/7/ct_v/video.mp4",
		}
		// publicBaseURL 留 nil:未配置。
	})

	res, raw := env.do(t, http.MethodPost, "/canvases/7/reverse-prompt", map[string]any{
		"video_url": "/api/assets/5/content", "model": "chat-m",
	})
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", res.StatusCode, raw)
	}
	req := env.params.gateway.(*stubGateway).requests[0]
	if req.VideoURL != "https://cdn.example.com/generated.mp4" {
		t.Errorf("video url = %q, want the vendor address fallback", req.VideoURL)
	}
}

// TestAnalyzeResolvesUploadedAssetViaPublicAddress 验证上传素材(url 为空、
// 只有自有字节)在公网基址配置后可作多模态输入;未配置时按「还没有可用
// 的产物地址」拒绝 —— 上传素材没有厂商原址可回落。
func TestAnalyzeResolvesUploadedAssetViaPublicAddress(t *testing.T) {
	uploaded := asset.Asset{
		ID: 8, Kind: asset.KindImage, CanvasID: 0, URL: "",
		ObjectKey: "uploads/20260911/1b0e0c2a-9d4d-4c1e-8f3a-2f6c8d5e7a91.png",
	}
	env := newHandlerEnv(t, func(p *envParams) {
		p.assets.byID[8] = uploaded
		p.publicBaseURL = func(context.Context) string { return "https://assets.example.org/" }
	})

	res, raw := env.do(t, http.MethodPost, "/canvases/7/analyze", map[string]any{
		"media_url": "/api/assets/8/content", "media_kind": "image", "model": "chat-m",
	})
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", res.StatusCode, raw)
	}
	req := env.params.gateway.(*stubGateway).requests[0]
	if req.ImageURL != "https://assets.example.org/"+uploaded.ObjectKey {
		t.Errorf("image url = %q, want the public address (base trailing slash trimmed)", req.ImageURL)
	}
}

func TestAnalyzeUploadedAssetWithoutPublicBaseAnswers400(t *testing.T) {
	env := newHandlerEnv(t, func(p *envParams) {
		p.assets.byID[8] = asset.Asset{
			ID: 8, Kind: asset.KindImage, URL: "",
			ObjectKey: "uploads/20260911/1b0e0c2a-9d4d-4c1e-8f3a-2f6c8d5e7a91.png",
		}
	})

	res, raw := env.do(t, http.MethodPost, "/canvases/7/analyze", map[string]any{
		"media_url": "/api/assets/8/content", "media_kind": "image", "model": "chat-m",
	})
	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body = %s", res.StatusCode, raw)
	}
	code, _ := errorBody(t, raw)
	if code != "asset_not_image" {
		t.Errorf("code = %q, want asset_not_image (还没有可用的产物地址)", code)
	}
}
