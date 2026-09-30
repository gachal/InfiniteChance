package asset_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
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
	// createErr makes Create fail (upload tests: 行落库失败时对象字节要回收).
	createErr error
}

func newFakeStore() *fakeStore {
	return &fakeStore{assets: map[int64]asset.Asset{}}
}

func (f *fakeStore) Create(_ context.Context, a asset.Asset) (asset.Asset, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.createErr != nil {
		return asset.Asset{}, f.createErr
	}
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

// failingStorage is an objectstore.Store double whose Open always answers
// openErr — the shape of a cloud read dying mid-transfer, 26 号票读兜底的
// 触发条件。Put/Delete 是测试不会走到的占位。
type failingStorage struct {
	openErr error
}

func (f *failingStorage) Put(context.Context, string, io.Reader, int64, string) error {
	return nil
}

func (f *failingStorage) Open(_ context.Context, _ string) (io.ReadCloser, error) {
	return nil, f.openErr
}

func (f *failingStorage) Delete(context.Context, string) error { return nil }

// newFailingStorageEnv wires the routes over a store whose Open always
// fails, mirroring newAssetEnv minus the real FileSystem.
func newFailingStorageEnv(t *testing.T, openErr error) *assetEnv {
	t.Helper()
	gin.SetMode(gin.TestMode)
	store := newFakeStore()
	h := &asset.Handlers{Store: store, Storage: &failingStorage{openErr: openErr}}
	r := gin.New()
	asset.RegisterContentRoutes(r.Group("/assets"), h)
	asset.RegisterLibraryRoutes(r.Group("/assets"), h)
	return &assetEnv{engine: r, store: store}
}

func TestContentOpenTransportErrorRedirectsToRowURL(t *testing.T) {
	// 服务端出网坏的实证形态:Open EOF(非「不存在」),行内还有厂商
	// 出处 —— 预览 302 直连,浏览器走用户自己的网络取字节。
	env := newFailingStorageEnv(t, errors.New("net: connection reset by peer"))
	created, _ := env.store.Create(context.Background(), asset.Asset{
		Kind: asset.KindImage, CanvasID: 1, URL: "https://img.example/vendored.png",
		ObjectKey: "canvases/1/ct_net/image.png", ContentType: "image/png", SizeBytes: 4,
	})

	w := env.do(t, http.MethodGet, idPath(created.ID, "/content"))
	if w.Code != http.StatusFound || w.Header().Get("Location") != "https://img.example/vendored.png" {
		t.Errorf("status = %d location = %q, want a 302 to the row URL",
			w.Code, w.Header().Get("Location"))
	}
}

func TestContentOpenTransportErrorDownloadProxiesRowURL(t *testing.T) {
	// attachment 套不上 302:下载入口回退为代理流出,与历史行同款契约。
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		_, _ = io.WriteString(w, "vendored-bytes")
	}))
	defer srv.Close()

	env := newFailingStorageEnv(t, errors.New("EOF"))
	created, _ := env.store.Create(context.Background(), asset.Asset{
		Kind: asset.KindImage, CanvasID: 1, URL: srv.URL + "/old.png",
		ObjectKey: "canvases/1/ct_net2/image.png", ContentType: "image/png", SizeBytes: 14,
	})

	w := env.do(t, http.MethodGet, idPath(created.ID, "/content?download=1"))
	if w.Code != http.StatusOK || w.Body.String() != "vendored-bytes" {
		t.Fatalf("status = %d body = %q, want the proxied bytes", w.Code, w.Body.String())
	}
	if cd := w.Header().Get("Content-Disposition"); !strings.Contains(cd, "attachment") {
		t.Errorf("content disposition = %q, want an attachment", cd)
	}
}

func TestContentOpenNotExistKeepsNotFound(t *testing.T) {
	// 对象真没了不回退:即使行内还有 http(s) 地址,也不把「对象没了」
	// 伪装成「网络坏了」——14 号票占位契约不变。
	env := newFailingStorageEnv(t, fs.ErrNotExist)
	created, _ := env.store.Create(context.Background(), asset.Asset{
		Kind: asset.KindImage, CanvasID: 1, URL: "https://img.example/still-listed.png",
		ObjectKey: "canvases/1/ct_gone2/image.png", ContentType: "image/png",
	})

	w := env.do(t, http.MethodGet, idPath(created.ID, "/content"))
	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404 (ErrNotExist 不回退)", w.Code)
	}
}

func TestContentOpenTransportErrorWithoutHTTPURLKeepsNotFound(t *testing.T) {
	// 行内没有可回退的 http(s) 地址(上传行的 url 留空、b64 历史行的
	// data: URI):维持 404 现状路径。
	env := newFailingStorageEnv(t, errors.New("net: connection reset by peer"))
	uploaded, _ := env.store.Create(context.Background(), asset.Asset{
		Kind: asset.KindImage, CanvasID: 1, URL: "",
		ObjectKey: "uploads/20260930/u1.png", ContentType: "image/png",
	})
	dataURI, _ := env.store.Create(context.Background(), asset.Asset{
		Kind: asset.KindImage, CanvasID: 1, URL: "data:image/png;base64,cG5n",
		ObjectKey: "canvases/1/ct_b64/image.png", ContentType: "image/png",
	})

	for _, id := range []int64{uploaded.ID, dataURI.ID} {
		if w := env.do(t, http.MethodGet, idPath(id, "/content")); w.Code != http.StatusNotFound {
			t.Errorf("asset %d: status = %d, want 404 (无可回退地址)", id, w.Code)
		}
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
