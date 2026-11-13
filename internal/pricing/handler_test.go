package pricing_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/gachal/InfiniteChance/internal/pricing"
)

// fakeStore is the in-memory Store double; handler tests run at the HTTP
// seam without MySQL.
type fakeStore struct {
	mu     sync.Mutex
	prices map[string]pricing.Price
}

func newFakeStore() *fakeStore {
	return &fakeStore{prices: map[string]pricing.Price{}}
}

func (f *fakeStore) List(context.Context) ([]pricing.Price, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]pricing.Price, 0, len(f.prices))
	for _, p := range f.prices {
		out = append(out, p)
	}
	return out, nil
}

func (f *fakeStore) ByModel(_ context.Context, model string) (pricing.Price, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	p, ok := f.prices[model]
	if !ok {
		return pricing.Price{}, pricing.ErrNotFound
	}
	return p, nil
}

func (f *fakeStore) Upsert(_ context.Context, p pricing.Price) (pricing.Price, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.prices[p.PublicModel] = p
	return p, nil
}

func (f *fakeStore) Delete(_ context.Context, model string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.prices[model]; !ok {
		return pricing.ErrNotFound
	}
	delete(f.prices, model)
	return nil
}

func newPricingServer(store pricing.Store) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	group := r.Group("/admin")
	pricing.RegisterAdminRoutes(group, &pricing.Handlers{Store: store})
	return r
}

func doPricingJSON(r http.Handler, method, path string, body any) *httptest.ResponseRecorder {
	var buf bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			panic(err)
		}
	}
	req := httptest.NewRequest(method, path, &buf)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func TestPricingUpsertListDeleteRoundTrip(t *testing.T) {
	r := newPricingServer(newFakeStore())

	w := doPricingJSON(r, "PUT", "/admin/prices", map[string]any{
		"public_model":           "deepseek-chat",
		"unit":                   "token",
		"input_cny_per_mtokens":  0.44,
		"output_cny_per_mtokens": 1.32,
		"ratio":                  1.5,
	})
	if w.Code != http.StatusOK {
		t.Fatalf("Upsert status = %d body %s, want 200", w.Code, w.Body.String())
	}
	// Wire form uses human units; check them echoed back.
	var wire struct {
		PublicModel         string  `json:"public_model"`
		Unit                string  `json:"unit"`
		InputCNYPerMTokens  float64 `json:"input_cny_per_mtokens"`
		OutputCNYPerMTokens float64 `json:"output_cny_per_mtokens"`
		Ratio               float64 `json:"ratio"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &wire); err != nil {
		t.Fatalf("decode upsert response: %v", err)
	}
	if wire.PublicModel != "deepseek-chat" || wire.Unit != "token" ||
		wire.InputCNYPerMTokens != 0.44 || wire.OutputCNYPerMTokens != 1.32 || wire.Ratio != 1.5 {
		t.Fatalf("upsert response = %+v, want echoed human-unit price", wire)
	}

	w = doPricingJSON(r, "GET", "/admin/prices", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("List status = %d, want 200", w.Code)
	}
	var list struct {
		Prices []struct {
			PublicModel string  `json:"public_model"`
			Ratio       float64 `json:"ratio"`
		} `json:"prices"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &list); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if len(list.Prices) != 1 || list.Prices[0].PublicModel != "deepseek-chat" || list.Prices[0].Ratio != 1.5 {
		t.Fatalf("list = %+v, want one deepseek-chat row at ratio 1.5", list.Prices)
	}

	w = doPricingJSON(r, "DELETE", "/admin/prices?model=deepseek-chat", nil)
	if w.Code != http.StatusNoContent {
		t.Fatalf("Delete status = %d, want 204", w.Code)
	}
	w = doPricingJSON(r, "DELETE", "/admin/prices?model=deepseek-chat", nil)
	if w.Code != http.StatusNotFound {
		t.Fatalf("second Delete status = %d, want 404", w.Code)
	}
}

func TestPricingCallTrackRoundTrip(t *testing.T) {
	r := newPricingServer(newFakeStore())

	// 按次轨:人类单位 USD 单价 + 尺寸系数,微美元整数落库后原样回读。
	w := doPricingJSON(r, "PUT", "/admin/prices", map[string]any{
		"public_model": "dall-e-3",
		"unit":         "call",
		"cny_per_call": 0.04,
		"size_factors": map[string]any{"1024x1024": 1.0, "1792x1024": 2.0},
	})
	if w.Code != http.StatusOK {
		t.Fatalf("Upsert status = %d body %s, want 200", w.Code, w.Body.String())
	}
	var wire struct {
		PublicModel string             `json:"public_model"`
		Unit        string             `json:"unit"`
		CNYPerCall  float64            `json:"cny_per_call"`
		SizeFactors map[string]float64 `json:"size_factors"`
		Ratio       float64            `json:"ratio"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &wire); err != nil {
		t.Fatalf("decode upsert response: %v", err)
	}
	if wire.PublicModel != "dall-e-3" || wire.Unit != "call" || wire.CNYPerCall != 0.04 ||
		wire.SizeFactors["1024x1024"] != 1 || wire.SizeFactors["1792x1024"] != 2 {
		t.Fatalf("upsert response = %+v, want echoed human-unit call price", wire)
	}
	if wire.Ratio != 0 {
		t.Errorf("ratio = %v on a call row, want 0 (token field meaningless here)", wire.Ratio)
	}

	// token 专属字段混进按次请求不致命:unit 决定载荷,余者忽略。
	w = doPricingJSON(r, "PUT", "/admin/prices", map[string]any{
		"public_model":          "gpt-image-1",
		"unit":                  "call",
		"cny_per_call":          0.02,
		"input_cny_per_mtokens": 5.0,
	})
	if w.Code != http.StatusOK {
		t.Fatalf("Upsert with stray token fields status = %d body %s, want 200", w.Code, w.Body.String())
	}

	// 回读:两行都是 call 轨,人类单位系数原样。
	w = doPricingJSON(r, "GET", "/admin/prices", nil)
	var list struct {
		Prices []struct {
			PublicModel string             `json:"public_model"`
			Unit        string             `json:"unit"`
			CNYPerCall  float64            `json:"cny_per_call"`
			SizeFactors map[string]float64 `json:"size_factors"`
		} `json:"prices"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &list); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if len(list.Prices) != 2 {
		t.Fatalf("list = %+v, want both call rows", list.Prices)
	}
	for _, row := range list.Prices {
		if row.Unit != "call" {
			t.Errorf("row %s unit = %s, want call", row.PublicModel, row.Unit)
		}
	}
}

func TestPricingSecondTrackRoundTrip(t *testing.T) {
	r := newPricingServer(newFakeStore())

	// 按秒轨(08 号票):视频模型单价按秒、系数按分辨率,字段与按次轨同形。
	w := doPricingJSON(r, "PUT", "/admin/prices", map[string]any{
		"public_model": "wan2.2",
		"unit":         "second",
		"cny_per_call": 0.10,
		"size_factors": map[string]any{"720p": 1.0, "1080p": 2.0},
	})
	if w.Code != http.StatusOK {
		t.Fatalf("Upsert status = %d body %s, want 200", w.Code, w.Body.String())
	}
	var wire struct {
		PublicModel string             `json:"public_model"`
		Unit        string             `json:"unit"`
		CNYPerCall  float64            `json:"cny_per_call"`
		SizeFactors map[string]float64 `json:"size_factors"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &wire); err != nil {
		t.Fatalf("decode upsert response: %v", err)
	}
	if wire.PublicModel != "wan2.2" || wire.Unit != "second" || wire.CNYPerCall != 0.10 ||
		wire.SizeFactors["1080p"] != 2 {
		t.Fatalf("upsert response = %+v, want echoed human-unit second price", wire)
	}
}

func TestPricingUpsertValidation(t *testing.T) {
	r := newPricingServer(newFakeStore())

	for _, tc := range []struct {
		name string
		body map[string]any
	}{
		{"missing model", map[string]any{"unit": "token"}},
		{"negative per-call price", map[string]any{"public_model": "m", "unit": "call", "cny_per_call": -1}},
		{"absurd per-call price", map[string]any{"public_model": "m", "unit": "call", "cny_per_call": 1001.0}},
		{"absurd size factor", map[string]any{"public_model": "m", "unit": "call", "cny_per_call": 0.04, "size_factors": map[string]any{"s": 1001.0}}},
		{"negative size factor", map[string]any{"public_model": "m", "unit": "call", "cny_per_call": 0.04, "size_factors": map[string]any{"s": -2.0}}},
		{"unknown unit", map[string]any{"public_model": "m", "unit": "bogus"}},
		{"absurd price", map[string]any{"public_model": "m", "unit": "token", "input_cny_per_mtokens": 1e9, "output_cny_per_mtokens": 1}},
		{"absurd ratio", map[string]any{"public_model": "m", "unit": "token", "ratio": 1001}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := doPricingJSON(r, "PUT", "/admin/prices", tc.body)
			if w.Code != http.StatusBadRequest {
				t.Fatalf("status = %d body %s, want 400", w.Code, w.Body.String())
			}
		})
	}

	// 免费按次价(0 美元/张)与 token 轨的 0 单价对称,合法。
	w := doPricingJSON(r, "PUT", "/admin/prices", map[string]any{"public_model": "m", "unit": "call", "cny_per_call": 0})
	if w.Code != http.StatusOK {
		t.Fatalf("free call price status = %d body %s, want 200", w.Code, w.Body.String())
	}
}

func TestPricingCharacterTrackRoundTrip(t *testing.T) {
	r := newPricingServer(newFakeStore())

	// 字符轨(56 号票):人类单位 元/万字符,微整数落库后原样回读;
	// call 轨字段混进请求被忽略(unit 决定载荷)。
	w := doPricingJSON(r, "PUT", "/admin/prices", map[string]any{
		"public_model":      "qwen-audio-3.1-tts-plus",
		"unit":              "character",
		"cny_per_10k_chars": 1.4,
		"cny_per_call":      9.9,
	})
	if w.Code != http.StatusOK {
		t.Fatalf("Upsert status = %d body %s, want 200", w.Code, w.Body.String())
	}
	var wire struct {
		PublicModel    string             `json:"public_model"`
		Unit           string             `json:"unit"`
		CNYPer10KChars float64            `json:"cny_per_10k_chars"`
		CNYPerCall     float64            `json:"cny_per_call"`
		SizeFactors    map[string]float64 `json:"size_factors"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &wire); err != nil {
		t.Fatalf("decode upsert response: %v", err)
	}
	if wire.PublicModel != "qwen-audio-3.1-tts-plus" || wire.Unit != "character" || wire.CNYPer10KChars != 1.4 {
		t.Fatalf("upsert response = %+v, want echoed human-unit character price", wire)
	}
	if wire.CNYPerCall != 0 || wire.SizeFactors != nil {
		t.Errorf("stray call fields = %v/%v on a character row, want zero/absent", wire.CNYPerCall, wire.SizeFactors)
	}

	// 越过护盾的单价被拒(400 形,统一管理面错误)。
	w = doPricingJSON(r, "PUT", "/admin/prices", map[string]any{
		"public_model": "m", "unit": "character", "cny_per_10k_chars": 1001.0,
	})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("over-shield price status = %d body %s, want 400", w.Code, w.Body.String())
	}

	// 回读:character 行人类单位单价原样。
	w = doPricingJSON(r, "GET", "/admin/prices", nil)
	var list struct {
		Prices []struct {
			PublicModel    string  `json:"public_model"`
			Unit           string  `json:"unit"`
			CNYPer10KChars float64 `json:"cny_per_10k_chars"`
		} `json:"prices"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &list); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if len(list.Prices) != 1 || list.Prices[0].Unit != "character" ||
		list.Prices[0].CNYPer10KChars != 1.4 {
		t.Fatalf("list = %+v, want the character row at 1.4", list.Prices)
	}
}

// 88 号票:价格行的模型能力声明 —— PUT 随行走 priceInputJSON.capabilities,
// 响应回显;越轨组合 400(管理面统一错误形状),空合法。
func TestPricingUpsertCapabilitiesRoundTrip(t *testing.T) {
	r := newPricingServer(newFakeStore())

	// call 轨合法子集:回显 + 落库。
	w := doPricingJSON(r, "PUT", "/admin/prices", map[string]any{
		"public_model": "og-image-2.5",
		"unit":         "call",
		"cny_per_call": 0.04,
		"capabilities": []string{"images", "panorama"},
	})
	if w.Code != http.StatusOK {
		t.Fatalf("Upsert status = %d body %s, want 200", w.Code, w.Body.String())
	}
	var wire struct {
		PublicModel  string   `json:"public_model"`
		Capabilities []string `json:"capabilities"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &wire); err != nil {
		t.Fatalf("decode upsert response: %v", err)
	}
	if len(wire.Capabilities) != 2 || wire.Capabilities[0] != "images" || wire.Capabilities[1] != "panorama" {
		t.Fatalf("capabilities echoed = %v, want [images panorama]", wire.Capabilities)
	}

	// 空数组:合法(= 不出现在任何下拉),回显 null/空。
	w = doPricingJSON(r, "PUT", "/admin/prices", map[string]any{
		"public_model": "plain-img", "unit": "call", "cny_per_call": 0.01, "capabilities": []string{},
	})
	if w.Code != http.StatusOK {
		t.Fatalf("empty capabilities status = %d body %s, want 200", w.Code, w.Body.String())
	}

	// 越轨组合:call 轨勾 chat → 400 + 统一错误形状。
	w = doPricingJSON(r, "PUT", "/admin/prices", map[string]any{
		"public_model": "bad-m", "unit": "call", "cny_per_call": 0.01, "capabilities": []string{"chat"},
	})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("out-of-track capabilities status = %d body %s, want 400", w.Code, w.Body.String())
	}
	var errShape struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &errShape); err != nil {
		t.Fatalf("error body not the admin shape: %v", err)
	}
	if errShape.Error.Code == "" || errShape.Error.Message == "" {
		t.Fatalf("error shape = %+v, want non-empty code+message", errShape.Error)
	}
}

// 列表回显能力:GET /admin/prices 每行带 capabilities(88 号票,价格页
// 能力列的数据源)。
func TestPricingListEchoesCapabilities(t *testing.T) {
	store := newFakeStore()
	store.mu.Lock()
	store.prices["og-image-2.5"] = pricing.Price{
		PublicModel: "og-image-2.5", Unit: pricing.UnitCall,
		Call:         &pricing.CallPrice{CNYPerCallMicros: 40_000},
		Capabilities: []string{"images"},
	}
	store.prices["plain-img"] = pricing.Price{
		PublicModel: "plain-img", Unit: pricing.UnitCall, Call: &pricing.CallPrice{},
	}
	store.mu.Unlock()

	r := newPricingServer(store)
	w := doPricingJSON(r, "GET", "/admin/prices", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("List status = %d, want 200", w.Code)
	}
	var list struct {
		Prices []struct {
			PublicModel  string   `json:"public_model"`
			Capabilities []string `json:"capabilities"`
		} `json:"prices"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &list); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	seen := map[string][]string{}
	for _, p := range list.Prices {
		seen[p.PublicModel] = p.Capabilities
	}
	if len(seen["og-image-2.5"]) != 1 || seen["og-image-2.5"][0] != "images" {
		t.Fatalf("og-image-2.5 capabilities = %v, want [images]", seen["og-image-2.5"])
	}
	if len(seen["plain-img"]) != 0 {
		t.Fatalf("plain-img capabilities = %v, want empty", seen["plain-img"])
	}
}
