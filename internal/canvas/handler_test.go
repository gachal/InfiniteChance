package canvas_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/gachal/InfiniteChance/internal/canvas"
)

// fakeStore is the in-memory Store double; handler tests run at the HTTP
// seam without MySQL.
type fakeStore struct {
	canvases map[int64]canvas.Canvas
	nextID   int64
	calls    atomic.Int64 // SaveGraph 调用计数:断言保存真的穿过 store
	broken   error        // 非 nil 时所有调用都返回它
}

func newFakeStore() *fakeStore {
	return &fakeStore{canvases: map[int64]canvas.Canvas{}, nextID: 1}
}

func (f *fakeStore) List(context.Context) ([]canvas.Canvas, error) {
	if f.broken != nil {
		return nil, f.broken
	}
	out := make([]canvas.Canvas, 0, len(f.canvases))
	for _, c := range f.canvases {
		out = append(out, c)
	}
	// 稳定排序:按 id 倒序(测试只关心集合内容)。
	for i := range out {
		for j := i + 1; j < len(out); j++ {
			if out[j].ID > out[i].ID {
				out[i], out[j] = out[j], out[i]
			}
		}
	}
	return out, nil
}

func (f *fakeStore) Get(_ context.Context, id int64) (canvas.Canvas, error) {
	if f.broken != nil {
		return canvas.Canvas{}, f.broken
	}
	c, ok := f.canvases[id]
	if !ok {
		return canvas.Canvas{}, canvas.ErrNotFound
	}
	return c, nil
}

func (f *fakeStore) Create(_ context.Context, name string, graph []byte) (canvas.Canvas, error) {
	if f.broken != nil {
		return canvas.Canvas{}, f.broken
	}
	now := time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC)
	c := canvas.Canvas{
		ID: f.nextID, Name: name, Graph: bytes.Clone(graph), Version: 1,
		CreatedAt: now, UpdatedAt: now,
	}
	f.nextID++
	f.canvases[c.ID] = c
	return c, nil
}

func (f *fakeStore) Rename(_ context.Context, id int64, name string) (canvas.Canvas, error) {
	if f.broken != nil {
		return canvas.Canvas{}, f.broken
	}
	c, ok := f.canvases[id]
	if !ok {
		return canvas.Canvas{}, canvas.ErrNotFound
	}
	c.Name = name
	f.canvases[id] = c
	return c, nil
}

func (f *fakeStore) Delete(_ context.Context, id int64) error {
	if f.broken != nil {
		return f.broken
	}
	if _, ok := f.canvases[id]; !ok {
		return canvas.ErrNotFound
	}
	delete(f.canvases, id)
	return nil
}

func (f *fakeStore) SaveGraph(_ context.Context, id int64, graph []byte, expectedVersion int64) (canvas.Canvas, error) {
	f.calls.Add(1)
	if f.broken != nil {
		return canvas.Canvas{}, f.broken
	}
	c, ok := f.canvases[id]
	if !ok {
		return canvas.Canvas{}, canvas.ErrNotFound
	}
	if c.Version != expectedVersion {
		return canvas.Canvas{}, canvas.ErrVersionConflict
	}
	c.Version++
	c.Graph = bytes.Clone(graph)
	f.canvases[id] = c
	return c, nil
}

// newTestRouter mounts the canvas routes on a bare engine. Auth itself is
// the middleware's job (covered by the auth package); here we exercise the
// handler contract.
func newTestRouter(h *canvas.Handlers) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	group := r.Group("/canvases")
	canvas.RegisterRoutes(group, h)
	return r
}

func do(r *gin.Engine, method, path, body string) *httptest.ResponseRecorder {
	var req *http.Request
	if body != "" {
		req = httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
	} else {
		req = httptest.NewRequest(method, path, nil)
	}
	res := httptest.NewRecorder()
	r.ServeHTTP(res, req)
	return res
}

func seed(t *testing.T, store *fakeStore) canvas.Canvas {
	t.Helper()
	c, err := store.Create(context.Background(), "测试画布", []byte(`{"nodes":[],"edges":[]}`))
	if err != nil {
		t.Fatalf("seed: %v", err)
	}
	return c
}

func TestListAnswersSummariesWithoutGraph(t *testing.T) {
	store := newFakeStore()
	seed(t, store)
	r := newTestRouter(&canvas.Handlers{Store: store})

	res := do(r, http.MethodGet, "/canvases", "")
	if res.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", res.Code, res.Body)
	}
	var body struct {
		Canvases []struct {
			ID      int64           `json:"id"`
			Name    string          `json:"name"`
			Version int64           `json:"version"`
			Graph   json.RawMessage `json:"graph"`
		} `json:"canvases"`
	}
	if err := json.Unmarshal(res.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(body.Canvases) != 1 {
		t.Fatalf("canvases = %d, want 1", len(body.Canvases))
	}
	if body.Canvases[0].Name != "测试画布" {
		t.Fatalf("name = %q", body.Canvases[0].Name)
	}
	// 列表不带图:列表页只需要名字,整图 JSON 可能很大。
	if body.Canvases[0].Graph != nil {
		t.Fatalf("list item carries graph: %s", body.Canvases[0].Graph)
	}
}

func TestCreateAnswersDetailWithGraph(t *testing.T) {
	store := newFakeStore()
	r := newTestRouter(&canvas.Handlers{Store: store})

	res := do(r, http.MethodPost, "/canvases", `{"name":"新画布"}`)
	if res.Code != http.StatusCreated {
		t.Fatalf("status = %d, body = %s", res.Code, res.Body)
	}
	var detail struct {
		ID      int64           `json:"id"`
		Name    string          `json:"name"`
		Version int64           `json:"version"`
		Graph   json.RawMessage `json:"graph"`
	}
	if err := json.Unmarshal(res.Body.Bytes(), &detail); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if detail.ID == 0 || detail.Name != "新画布" || detail.Version != 1 {
		t.Fatalf("detail = %+v", detail)
	}
	var graph map[string]any
	if err := json.Unmarshal(detail.Graph, &graph); err != nil {
		t.Fatalf("graph decode: %v (%s)", err, detail.Graph)
	}
	// 新画布直接可用:空 nodes/edges,前端挂载即可编辑。
	if _, ok := graph["nodes"]; !ok {
		t.Fatalf("graph has no nodes: %s", detail.Graph)
	}
}

func TestCreateRejectsBlankAndOverlongNames(t *testing.T) {
	store := newFakeStore()
	r := newTestRouter(&canvas.Handlers{Store: store})

	res := do(r, http.MethodPost, "/canvases", `{"name":"   "}`)
	if res.Code != http.StatusBadRequest {
		t.Fatalf("blank name status = %d", res.Code)
	}
	long := strings.Repeat("长", 129)
	res = do(r, http.MethodPost, "/canvases", fmt.Sprintf(`{"name":%q}`, long))
	if res.Code != http.StatusBadRequest {
		t.Fatalf("overlong name status = %d", res.Code)
	}
	res = do(r, http.MethodPost, "/canvases", `not-json`)
	if res.Code != http.StatusBadRequest {
		t.Fatalf("bad body status = %d", res.Code)
	}
}

func TestGetReturnsGraph(t *testing.T) {
	store := newFakeStore()
	c := seed(t, store)
	r := newTestRouter(&canvas.Handlers{Store: store})

	res := do(r, http.MethodGet, fmt.Sprintf("/canvases/%d", c.ID), "")
	if res.Code != http.StatusOK {
		t.Fatalf("status = %d", res.Code)
	}
	if !strings.Contains(res.Body.String(), `"graph"`) {
		t.Fatalf("detail missing graph: %s", res.Body)
	}
}

func TestGetUnknownIDAnswers404Envelope(t *testing.T) {
	store := newFakeStore()
	r := newTestRouter(&canvas.Handlers{Store: store})

	res := do(r, http.MethodGet, "/canvases/999", "")
	if res.Code != http.StatusNotFound {
		t.Fatalf("status = %d", res.Code)
	}
	var body struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(res.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Error.Code != "not_found" {
		t.Fatalf("code = %q, want not_found", body.Error.Code)
	}
}

func TestRenameUpdatesNameOnly(t *testing.T) {
	store := newFakeStore()
	c := seed(t, store)
	r := newTestRouter(&canvas.Handlers{Store: store})

	res := do(r, http.MethodPatch, fmt.Sprintf("/canvases/%d", c.ID), `{"name":"改名"}`)
	if res.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", res.Code, res.Body)
	}
	got := store.canvases[c.ID]
	if got.Name != "改名" || got.Version != 1 {
		t.Fatalf("after rename = %+v", got)
	}
}

func TestDeleteAnswers204(t *testing.T) {
	store := newFakeStore()
	c := seed(t, store)
	r := newTestRouter(&canvas.Handlers{Store: store})

	res := do(r, http.MethodDelete, fmt.Sprintf("/canvases/%d", c.ID), "")
	if res.Code != http.StatusNoContent {
		t.Fatalf("status = %d", res.Code)
	}
	if _, ok := store.canvases[c.ID]; ok {
		t.Fatal("canvas still stored after delete")
	}
}

func TestSaveGraphAnswersNewVersion(t *testing.T) {
	store := newFakeStore()
	c := seed(t, store)
	r := newTestRouter(&canvas.Handlers{Store: store})

	payload := `{"graph":{"nodes":[{"id":"n1","type":"prompt"}],"edges":[]},"version":1}`
	res := do(r, http.MethodPut, fmt.Sprintf("/canvases/%d/graph", c.ID), payload)
	if res.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", res.Code, res.Body)
	}
	var body struct {
		Version int64 `json:"version"`
	}
	if err := json.Unmarshal(res.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Version != 2 {
		t.Fatalf("version = %d, want 2", body.Version)
	}
	if store.calls.Load() != 1 {
		t.Fatalf("SaveGraph calls = %d, want 1", store.calls.Load())
	}
}

func TestSaveGraphConflictAnswers409WithStableCode(t *testing.T) {
	store := newFakeStore()
	c := seed(t, store)
	r := newTestRouter(&canvas.Handlers{Store: store})

	// 版本 1 已被别的标签页消费:这次以 1 保存必冲突。
	store.canvases[c.ID] = canvas.Canvas{ID: c.ID, Name: c.Name, Version: 2, Graph: c.Graph}

	res := do(r, http.MethodPut, fmt.Sprintf("/canvases/%d/graph", c.ID),
		`{"graph":{"nodes":[]},"version":1}`)
	if res.Code != http.StatusConflict {
		t.Fatalf("status = %d, body = %s", res.Code, res.Body)
	}
	var body struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(res.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Error.Code != "version_conflict" {
		t.Fatalf("code = %q, want version_conflict", body.Error.Code)
	}
}

func TestSaveGraphValidation(t *testing.T) {
	store := newFakeStore()
	c := seed(t, store)
	path := fmt.Sprintf("/canvases/%d/graph", c.ID)
	r := newTestRouter(&canvas.Handlers{Store: store})

	cases := []struct {
		name    string
		payload string
	}{
		{"missing version", `{"graph":{}}`},
		{"zero version", `{"graph":{},"version":0}`},
		{"negative version", `{"graph":{},"version":-3}`},
		{"missing graph", `{"version":1}`},
		{"graph not an object", `{"graph":[1,2],"version":1}`},
		{"graph broken json", `{"graph":{"nodes":},"version":1}`},
	}
	for _, tc := range cases {
		res := do(r, http.MethodPut, path, tc.payload)
		if res.Code != http.StatusBadRequest {
			t.Fatalf("%s: status = %d, want 400", tc.name, res.Code)
		}
	}
}

func TestSaveGraphRejectsOversizedBody(t *testing.T) {
	store := newFakeStore()
	c := seed(t, store)
	r := newTestRouter(&canvas.Handlers{Store: store})

	// 超过上限的大图:400 而不是写穿数据库。
	big := `{"graph":{"nodes":["` + strings.Repeat("x", canvas.MaxGraphBytes) + `"]},"version":1}`
	res := do(r, http.MethodPut, fmt.Sprintf("/canvases/%d/graph", c.ID), big)
	if res.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 for oversized graph", res.Code)
	}
}

func TestStoreFailureAnswers500(t *testing.T) {
	store := newFakeStore()
	seed(t, store)
	store.broken = errors.New("store broken")
	r := newTestRouter(&canvas.Handlers{Store: store})

	res := do(r, http.MethodGet, "/canvases", "")
	if res.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", res.Code)
	}
}

func TestMalformedIDAnswers400(t *testing.T) {
	store := newFakeStore()
	r := newTestRouter(&canvas.Handlers{Store: store})

	// GET /canvases/abc 命中 Get;PUT /canvases/abc/graph 命中 SaveGraph。
	// (GET …/graph 未注册,走 gin 的 404,与本契约无关。)
	if res := do(r, http.MethodGet, "/canvases/abc", ""); res.Code != http.StatusBadRequest {
		t.Fatalf("GET /canvases/abc: status = %d, want 400", res.Code)
	}
	if res := do(r, http.MethodPut, "/canvases/abc/graph", `{"graph":{},"version":1}`); res.Code != http.StatusBadRequest {
		t.Fatalf("PUT /canvases/abc/graph: status = %d, want 400", res.Code)
	}
}
