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
		"input_usd_per_mtokens":  0.44,
		"output_usd_per_mtokens": 1.32,
		"ratio":                  1.5,
	})
	if w.Code != http.StatusOK {
		t.Fatalf("Upsert status = %d body %s, want 200", w.Code, w.Body.String())
	}
	// Wire form uses human units; check them echoed back.
	var wire struct {
		PublicModel         string  `json:"public_model"`
		Unit                string  `json:"unit"`
		InputUSDPerMTokens  float64 `json:"input_usd_per_mtokens"`
		OutputUSDPerMTokens float64 `json:"output_usd_per_mtokens"`
		Ratio               float64 `json:"ratio"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &wire); err != nil {
		t.Fatalf("decode upsert response: %v", err)
	}
	if wire.PublicModel != "deepseek-chat" || wire.Unit != "token" ||
		wire.InputUSDPerMTokens != 0.44 || wire.OutputUSDPerMTokens != 1.32 || wire.Ratio != 1.5 {
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

func TestPricingUpsertValidation(t *testing.T) {
	r := newPricingServer(newFakeStore())

	for _, tc := range []struct {
		name string
		body map[string]any
	}{
		{"missing model", map[string]any{"unit": "token"}},
		{"call track rejected", map[string]any{"public_model": "m", "unit": "call"}},
		{"unknown unit", map[string]any{"public_model": "m", "unit": "bogus"}},
		{"absurd price", map[string]any{"public_model": "m", "unit": "token", "input_usd_per_mtokens": 1e9, "output_usd_per_mtokens": 1}},
		{"absurd ratio", map[string]any{"public_model": "m", "unit": "token", "ratio": 1001}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := doPricingJSON(r, "PUT", "/admin/prices", tc.body)
			if w.Code != http.StatusBadRequest {
				t.Fatalf("status = %d body %s, want 400", w.Code, w.Body.String())
			}
		})
	}
}
