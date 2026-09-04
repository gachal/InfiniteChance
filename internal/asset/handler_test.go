package asset_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/gachal/InfiniteChance/internal/asset"
	"github.com/gachal/InfiniteChance/internal/objectstore"
)

// fakeStore is the in-memory Store double for handler tests; the MySQL
// store's own semantics live in its store tests.
type fakeStore struct {
	mu     sync.Mutex
	assets map[int64]asset.Asset
	seq    int64
}

func newFakeStore() *fakeStore {
	return &fakeStore{assets: map[int64]asset.Asset{}}
}

func (f *fakeStore) Create(_ context.Context, a asset.Asset) (asset.Asset, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.seq++
	a.ID = f.seq
	a.CreatedAt = time.Date(2026, 9, 4, 12, 0, 0, 0, time.UTC)
	f.assets[a.ID] = a
	return a, nil
}

func (f *fakeStore) Get(_ context.Context, id int64) (asset.Asset, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	a, ok := f.assets[id]
	if !ok {
		return asset.Asset{}, asset.ErrNotFound
	}
	return a, nil
}

func (f *fakeStore) List(_ context.Context, fl asset.Filter) ([]asset.Listed, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []asset.Listed
	for id := f.seq; id >= 1; id-- {
		a, ok := f.assets[id]
		if !ok {
			continue
		}
		if fl.Kind != "" && a.Kind != fl.Kind {
			continue
		}
		if fl.CanvasID > 0 && a.CanvasID != fl.CanvasID {
			continue
		}
		out = append(out, asset.Listed{Asset: a, CanvasName: canvasNameOf(a.CanvasID)})
	}
	return out, nil
}

func (f *fakeStore) Delete(_ context.Context, id int64) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.assets[id]; !ok {
		return asset.ErrNotFound
	}
	delete(f.assets, id)
	return nil
}

func canvasNameOf(id int64) string {
	if id == 7 {
		return "主画布"
	}
	return ""
}

// handlerEnv wires the asset routes the way the binary does (minus JWT on
// the library group) with a real FileSystem storage over a temp dir.
type assetEnv struct {
	engine  *gin.Engine
	store   *fakeStore
	storage *objectstore.FileSystem
}

func newAssetEnv(t *testing.T) *assetEnv {
	t.Helper()
	gin.SetMode(gin.TestMode)
	store := newFakeStore()
	storage, err := objectstore.NewFileSystem(t.TempDir())
	if err != nil {
		t.Fatalf("objectstore: %v", err)
	}
	h := &asset.Handlers{Store: store, Storage: storage}
	r := gin.New()
	asset.RegisterContentRoutes(r.Group("/assets"), h)
	asset.RegisterLibraryRoutes(r.Group("/assets"), h)
	return &assetEnv{engine: r, store: store, storage: storage}
}

func (e *assetEnv) do(t *testing.T, method, path string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, nil)
	w := httptest.NewRecorder()
	e.engine.ServeHTTP(w, req)
	return w
}

func idPath(id int64, suffix string) string {
	return "/assets/" + strconv.FormatInt(id, 10) + suffix
}

func TestContentServesStoredObjectBytes(t *testing.T) {
	env := newAssetEnv(t)
	ctx := context.Background()
	created, _ := env.store.Create(ctx, asset.Asset{
		Kind: asset.KindImage, CanvasID: 7, URL: "https://img.example/old.png",
		ObjectKey: "canvases/7/ct_a/image.png", ContentType: "image/png", SizeBytes: 9,
	})
	if err := env.storage.Put(ctx, "canvases/7/ct_a/image.png", strings.NewReader("png-bytes"), 9, "image/png"); err != nil {
		t.Fatalf("seed object: %v", err)
	}

	w := env.do(t, http.MethodGet, idPath(created.ID, "/content"))
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	if w.Body.String() != "png-bytes" {
		t.Errorf("body = %q, want the stored bytes", w.Body.String())
	}
	if ct := w.Header().Get("Content-Type"); ct != "image/png" {
		t.Errorf("content type = %q", ct)
	}
}

func TestContentDownloadSetsAttachment(t *testing.T) {
	env := newAssetEnv(t)
	ctx := context.Background()
	created, _ := env.store.Create(ctx, asset.Asset{
		Kind: asset.KindVideo, CanvasID: 1, URL: "https://vid.example/old.mp4",
		ObjectKey: "canvases/1/ct_v/video.mp4", ContentType: "video/mp4", SizeBytes: 3,
	})
	_ = env.storage.Put(ctx, "canvases/1/ct_v/video.mp4", strings.NewReader("abc"), 3, "video/mp4")

	w := env.do(t, http.MethodGet, idPath(created.ID, "/content?download=1"))
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	cd := w.Header().Get("Content-Disposition")
	if !strings.Contains(cd, "attachment") || !strings.Contains(cd, ".mp4") {
		t.Errorf("content disposition = %q, want an mp4 attachment", cd)
	}
}

func TestContentMissingObjectAnswersNotFound(t *testing.T) {
	env := newAssetEnv(t)
	created, _ := env.store.Create(context.Background(), asset.Asset{
		Kind: asset.KindImage, CanvasID: 1, URL: "https://img.example/x.png",
		ObjectKey: "canvases/1/ct_gone/image.png", ContentType: "image/png",
	})
	w := env.do(t, http.MethodGet, idPath(created.ID, "/content"))
	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404 (节点显示占位而非报错)", w.Code)
	}
}

func TestContentLegacyRowsKeepOldContract(t *testing.T) {
	env := newAssetEnv(t)
	ctx := context.Background()
	httpAsset, _ := env.store.Create(ctx, asset.Asset{
		Kind: asset.KindImage, CanvasID: 1, URL: "https://img.example/direct.png",
	})
	dataAsset, _ := env.store.Create(ctx, asset.Asset{
		Kind: asset.KindImage, CanvasID: 1, URL: "data:image/png;base64,cG5n",
	})

	w := env.do(t, http.MethodGet, idPath(httpAsset.ID, "/content"))
	if w.Code != http.StatusFound || w.Header().Get("Location") != "https://img.example/direct.png" {
		t.Errorf("status = %d location = %q, want a redirect to the stored URL",
			w.Code, w.Header().Get("Location"))
	}
	w = env.do(t, http.MethodGet, idPath(dataAsset.ID, "/content"))
	if w.Code != http.StatusOK || w.Body.String() != "png" {
		t.Errorf("data URI: status = %d body = %q, want inline bytes", w.Code, w.Body.String())
	}
}

func TestListFiltersByKindAndCanvas(t *testing.T) {
	env := newAssetEnv(t)
	ctx := context.Background()
	env.store.Create(ctx, asset.Asset{Kind: asset.KindImage, CanvasID: 7, URL: "u1"})
	env.store.Create(ctx, asset.Asset{Kind: asset.KindVideo, CanvasID: 7, URL: "u2"})
	env.store.Create(ctx, asset.Asset{Kind: asset.KindImage, CanvasID: 8, URL: "u3"})

	w := env.do(t, http.MethodGet, "/assets?kind=image&canvas_id=7")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}
	var body struct {
		Assets []struct {
			ID         int64  `json:"id"`
			Kind       string `json:"kind"`
			CanvasName string `json:"canvas_name"`
			ContentURL string `json:"content_url"`
		} `json:"assets"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("response not JSON: %v", err)
	}
	if len(body.Assets) != 1 || body.Assets[0].Kind != "image" ||
		body.Assets[0].CanvasName != "主画布" {
		t.Fatalf("assets = %+v, want the one image of canvas 7 with its name", body.Assets)
	}
	want := "/api/assets/" + strconv.FormatInt(body.Assets[0].ID, 10) + "/content"
	if body.Assets[0].ContentURL != want {
		t.Errorf("content_url = %q, want the content-addressed path", body.Assets[0].ContentURL)
	}
}

func TestDeleteRemovesObjectThenRow(t *testing.T) {
	env := newAssetEnv(t)
	ctx := context.Background()
	created, _ := env.store.Create(ctx, asset.Asset{
		Kind: asset.KindImage, CanvasID: 7, URL: "https://img.example/a.png",
		ObjectKey: "canvases/7/ct_d/image.png", ContentType: "image/png", SizeBytes: 4,
	})
	_ = env.storage.Put(ctx, "canvases/7/ct_d/image.png", strings.NewReader("abcd"), 4, "image/png")

	w := env.do(t, http.MethodDelete, idPath(created.ID, ""))
	if w.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", w.Code)
	}
	if _, err := env.storage.Open(ctx, "canvases/7/ct_d/image.png"); err == nil {
		t.Errorf("object survived the delete")
	}
	if _, err := env.store.Get(ctx, created.ID); err != asset.ErrNotFound {
		t.Errorf("row get = %v, want ErrNotFound", err)
	}

	// 再删一次:404,不是 500。
	w = env.do(t, http.MethodDelete, idPath(created.ID, ""))
	if w.Code != http.StatusNotFound {
		t.Errorf("second delete status = %d, want 404", w.Code)
	}
}
