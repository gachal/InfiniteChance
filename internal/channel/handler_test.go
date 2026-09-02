package channel_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/gachal/InfiniteChance/internal/channel"
)

// fakeStore is the in-memory Store double; handler tests run at the HTTP
// seam without MySQL.
type fakeStore struct {
	channels map[int64]channel.Channel
	nextID   int64
}

func newFakeStore() *fakeStore {
	return &fakeStore{channels: map[int64]channel.Channel{}, nextID: 1}
}

func (f *fakeStore) List(context.Context) ([]channel.Channel, error) {
	out := make([]channel.Channel, 0, len(f.channels))
	for _, ch := range f.channels {
		out = append(out, ch)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Priority != out[j].Priority {
			return out[i].Priority > out[j].Priority
		}
		return out[i].ID < out[j].ID
	})
	return out, nil
}

func (f *fakeStore) Get(_ context.Context, id int64) (channel.Channel, error) {
	ch, ok := f.channels[id]
	if !ok {
		return channel.Channel{}, channel.ErrNotFound
	}
	return ch, nil
}

func (f *fakeStore) Create(_ context.Context, ch channel.Channel) (channel.Channel, error) {
	ch.ID = f.nextID
	f.nextID++
	f.channels[ch.ID] = ch
	return ch, nil
}

func (f *fakeStore) Update(_ context.Context, ch channel.Channel) (channel.Channel, error) {
	stored, ok := f.channels[ch.ID]
	if !ok {
		return channel.Channel{}, channel.ErrNotFound
	}
	if ch.APIKey == "" {
		ch.APIKey = stored.APIKey
	}
	f.channels[ch.ID] = ch
	return ch, nil
}

func (f *fakeStore) Delete(_ context.Context, id int64) error {
	if _, ok := f.channels[id]; !ok {
		return channel.ErrNotFound
	}
	delete(f.channels, id)
	return nil
}

var errStoreBroken = errors.New("store broken")

type brokenStore struct{ *fakeStore }

func (brokenStore) List(context.Context) ([]channel.Channel, error) {
	return nil, errStoreBroken
}

func newChannelServer(store channel.Store) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	group := r.Group("/admin")
	channel.RegisterAdminRoutes(group, &channel.Handlers{Store: store, Tester: &channel.Tester{}})
	return r
}

func doJSON(r http.Handler, method, path string, body any) *httptest.ResponseRecorder {
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

type errorBody struct {
	Error struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

func decodeError(t *testing.T, w *httptest.ResponseRecorder) errorBody {
	t.Helper()
	var body errorBody
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("response is not the standard error shape: %v\nbody: %s", err, w.Body.String())
	}
	if body.Error.Code == "" {
		t.Fatalf("error body missing code: %s", w.Body.String())
	}
	return body
}

const vendorSecret = "sk-vendor-secret-9876"

func validChannelBody() map[string]any {
	return map[string]any{
		"name":     "openai-main",
		"type":     "openai",
		"base_url": "https://api.openai.com/v1",
		"api_key":  vendorSecret,
		"model_map": map[string]string{
			"gpt-4o": "gpt-4o-2024-11-20",
		},
		"priority": 10,
		"weight":   1,
		"enabled":  true,
	}
}

func TestCreateChannelRoundTripsWithoutLeakingSecret(t *testing.T) {
	store := newFakeStore()
	r := newChannelServer(store)

	w := doJSON(r, http.MethodPost, "/admin/channels", validChannelBody())
	if w.Code != http.StatusCreated {
		t.Fatalf("create status = %d, want 201; body: %s", w.Code, w.Body.String())
	}
	if strings.Contains(w.Body.String(), vendorSecret) {
		t.Fatal("create response leaked the vendor secret")
	}

	var created struct {
		ID       int64             `json:"id"`
		Name     string            `json:"name"`
		Type     string            `json:"type"`
		BaseURL  string            `json:"base_url"`
		HasKey   bool              `json:"has_key"`
		KeyHint  string            `json:"key_hint"`
		ModelMap map[string]string `json:"model_map"`
		Priority int               `json:"priority"`
		Weight   int               `json:"weight"`
		Enabled  bool              `json:"enabled"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &created); err != nil {
		t.Fatalf("bad JSON: %v", err)
	}
	if created.ID <= 0 {
		t.Errorf("id = %d, want a positive stored id", created.ID)
	}
	if !created.HasKey {
		t.Error("has_key should be true after storing a secret")
	}
	if !strings.HasSuffix(created.KeyHint, "9876") {
		t.Errorf("key_hint = %q, want the last-4 hint", created.KeyHint)
	}
	if created.ModelMap["gpt-4o"] != "gpt-4o-2024-11-20" {
		t.Errorf("model_map = %v, want the submitted mapping", created.ModelMap)
	}

	// 列表同样不泄漏密钥,且能看到刚建的渠道。
	w = doJSON(r, http.MethodGet, "/admin/channels", nil)
	if strings.Contains(w.Body.String(), vendorSecret) {
		t.Fatal("list response leaked the vendor secret")
	}
	var list struct {
		Channels []struct {
			ID int64 `json:"id"`
		} `json:"channels"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &list); err != nil {
		t.Fatalf("bad list JSON: %v", err)
	}
	if len(list.Channels) != 1 || list.Channels[0].ID != created.ID {
		t.Errorf("list = %+v, want exactly the created channel", list.Channels)
	}
}

func TestCreateChannelValidatesInput(t *testing.T) {
	r := newChannelServer(newFakeStore())

	cases := []struct {
		name string
		mut  func(map[string]any)
	}{
		{"empty name", func(b map[string]any) { b["name"] = "  " }},
		{"unknown type", func(b map[string]any) { b["type"] = "hal9000" }},
		{"missing type", func(b map[string]any) { b["type"] = "" }},
		{"bad base url", func(b map[string]any) { b["base_url"] = "not-a-url" }},
		{"ftp base url", func(b map[string]any) { b["base_url"] = "ftp://x" }},
		{"missing secret", func(b map[string]any) { b["api_key"] = "" }},
		{"empty mapping key", func(b map[string]any) {
			b["model_map"] = map[string]string{"": "upstream"}
		}},
		{"negative priority", func(b map[string]any) { b["priority"] = -1 }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			body := validChannelBody()
			tc.mut(body)
			w := doJSON(r, http.MethodPost, "/admin/channels", body)
			if w.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400; body: %s", w.Code, w.Body.String())
			}
			if got := decodeError(t, w).Error.Code; got != "invalid_request" {
				t.Errorf("code = %q, want invalid_request", got)
			}
		})
	}
}

func TestUpdateChannelKeepsSecretWhenBlank(t *testing.T) {
	store := newFakeStore()
	r := newChannelServer(store)
	created := doJSON(r, http.MethodPost, "/admin/channels", validChannelBody())
	var ch struct {
		ID      int64  `json:"id"`
		BaseURL string `json:"base_url"`
	}
	if err := json.Unmarshal(created.Body.Bytes(), &ch); err != nil {
		t.Fatalf("bad JSON: %v", err)
	}

	update := validChannelBody()
	update["api_key"] = ""
	update["base_url"] = "https://api.deepseek.com/v1"
	w := doJSON(r, http.MethodPut, fmt.Sprintf("/admin/channels/%d", ch.ID), update)
	if w.Code != http.StatusOK {
		t.Fatalf("update status = %d, want 200; body: %s", w.Code, w.Body.String())
	}

	stored, err := store.Get(t.Context(), ch.ID)
	if err != nil {
		t.Fatalf("Get after update: %v", err)
	}
	if stored.APIKey != vendorSecret {
		t.Errorf("api_key = %q, want the blank update to keep the stored secret", stored.APIKey)
	}
	if stored.BaseURL != "https://api.deepseek.com/v1" {
		t.Errorf("base_url = %q, want the update applied", stored.BaseURL)
	}

	w = doJSON(r, http.MethodPut, "/admin/channels/999", update)
	if w.Code != http.StatusNotFound {
		t.Fatalf("update missing status = %d, want 404", w.Code)
	}
	if got := decodeError(t, w).Error.Code; got != "not_found" {
		t.Errorf("code = %q, want not_found", got)
	}
}

func TestDeleteChannel(t *testing.T) {
	store := newFakeStore()
	r := newChannelServer(store)
	created := doJSON(r, http.MethodPost, "/admin/channels", validChannelBody())
	var ch struct {
		ID int64 `json:"id"`
	}
	if err := json.Unmarshal(created.Body.Bytes(), &ch); err != nil {
		t.Fatalf("bad JSON: %v", err)
	}

	if w := doJSON(r, http.MethodDelete, fmt.Sprintf("/admin/channels/%d", ch.ID), nil); w.Code != http.StatusNoContent {
		t.Fatalf("delete status = %d, want 204", w.Code)
	}
	if _, err := store.Get(t.Context(), ch.ID); !errors.Is(err, channel.ErrNotFound) {
		t.Errorf("Get after delete err = %v, want ErrNotFound", err)
	}
	if w := doJSON(r, http.MethodDelete, fmt.Sprintf("/admin/channels/%d", ch.ID), nil); w.Code != http.StatusNotFound {
		t.Fatalf("second delete status = %d, want 404", w.Code)
	}
	if w := doJSON(r, http.MethodDelete, "/admin/channels/not-a-number", nil); w.Code != http.StatusBadRequest {
		t.Fatalf("delete bad id status = %d, want 400", w.Code)
	}
}

func TestListMapsStoreFailureTo500(t *testing.T) {
	w := doJSON(newChannelServer(brokenStore{newFakeStore()}), http.MethodGet, "/admin/channels", nil)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", w.Code)
	}
	if got := decodeError(t, w).Error.Code; got != "internal_error" {
		t.Errorf("code = %q, want internal_error", got)
	}
}

// TestChannelConnectivityProbe covers the one-click test against fake
// upstreams: a decidable verdict on every failure mode.
func TestChannelConnectivityProbe(t *testing.T) {
	var gotPath, gotAuth string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotAuth = r.URL.Path, r.Header.Get("Authorization")
		if r.Header.Get("Authorization") == "Bearer "+vendorSecret {
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `{"data":[{"id":"gpt-4o"},{"id":"gpt-4o-mini"}]}`)
			return
		}
		w.WriteHeader(http.StatusUnauthorized)
		fmt.Fprint(w, `{"error":{"message":"bad key"}}`)
	}))
	defer upstream.Close()

	store := newFakeStore()
	r := newChannelServer(store)
	created := doJSON(r, http.MethodPost, "/admin/channels", validChannelBody())
	var ch struct {
		ID int64 `json:"id"`
	}
	if err := json.Unmarshal(created.Body.Bytes(), &ch); err != nil {
		t.Fatalf("bad JSON: %v", err)
	}

	t.Run("healthy upstream answers ok with model count", func(t *testing.T) {
		body := validChannelBody()
		body["base_url"] = upstream.URL
		doJSON(r, http.MethodPut, fmt.Sprintf("/admin/channels/%d", ch.ID), body)

		w := doJSON(r, http.MethodPost, fmt.Sprintf("/admin/channels/%d/test", ch.ID), nil)
		if w.Code != http.StatusOK {
			t.Fatalf("test status = %d, want 200; body: %s", w.Code, w.Body.String())
		}
		var result channel.Result
		if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
			t.Fatalf("bad test JSON: %v", err)
		}
		if !result.OK {
			t.Errorf("ok = false, want true (error: %s)", result.Error)
		}
		if !strings.Contains(result.Detail, "2") {
			t.Errorf("detail = %q, want the model count 2", result.Detail)
		}
		if gotPath != "/models" || gotAuth != "Bearer "+vendorSecret {
			t.Errorf("upstream saw %s %q, want GET /models with the stored bearer", gotPath, gotAuth)
		}
	})

	t.Run("rejected upstream answers decidable failure", func(t *testing.T) {
		body := validChannelBody()
		body["api_key"] = "sk-wrong-key"
		body["base_url"] = upstream.URL
		doJSON(r, http.MethodPut, fmt.Sprintf("/admin/channels/%d", ch.ID), body)

		w := doJSON(r, http.MethodPost, fmt.Sprintf("/admin/channels/%d/test", ch.ID), nil)
		if w.Code != http.StatusOK {
			t.Fatalf("test status = %d, want 200", w.Code)
		}
		var result channel.Result
		if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
			t.Fatalf("bad test JSON: %v", err)
		}
		if result.OK {
			t.Error("ok = true, want false for a rejected probe")
		}
		if !strings.Contains(result.Error, "401") {
			t.Errorf("error = %q, want the upstream 401 echoed", result.Error)
		}
	})

	t.Run("unreachable upstream answers decidable failure", func(t *testing.T) {
		closed := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
		closed.Close()

		body := validChannelBody()
		body["base_url"] = closed.URL
		doJSON(r, http.MethodPut, fmt.Sprintf("/admin/channels/%d", ch.ID), body)

		w := doJSON(r, http.MethodPost, fmt.Sprintf("/admin/channels/%d/test", ch.ID), nil)
		if w.Code != http.StatusOK {
			t.Fatalf("test status = %d, want 200", w.Code)
		}
		var result channel.Result
		if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
			t.Fatalf("bad test JSON: %v", err)
		}
		if result.OK {
			t.Error("ok = true, want false for an unreachable upstream")
		}
		if result.Error == "" {
			t.Error("error empty, want a connection failure message")
		}
	})

	t.Run("test unknown channel is 404", func(t *testing.T) {
		w := doJSON(r, http.MethodPost, "/admin/channels/999/test", nil)
		if w.Code != http.StatusNotFound {
			t.Fatalf("status = %d, want 404", w.Code)
		}
	})
}

func TestNormalizeTrimsTrailingSlashOnBaseURL(t *testing.T) {
	input := channel.Input{
		Name: "x", Type: channel.TypeOpenAI,
		BaseURL: "https://api.openai.com/v1/",
		APIKey:  "k",
	}
	got, err := input.Normalize(true)
	if err != nil {
		t.Fatalf("Normalize: %v", err)
	}
	if got.BaseURL != "https://api.openai.com/v1" {
		t.Errorf("base_url = %q, want trailing slash trimmed", got.BaseURL)
	}
}
