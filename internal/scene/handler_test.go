package scene_test

import (
	"bytes"
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

	"github.com/gachal/InfiniteChance/internal/canvas"
	"github.com/gachal/InfiniteChance/internal/scene"
)

// fakeStore is the in-memory Store double; handler tests run at the HTTP
// seam without either database. 保序:scenes 按 updated_at DESC,images 按
// sort+id。
type fakeStore struct {
	mu     sync.Mutex
	scene  map[int64]scene.Scene
	nextID int64
}

func newFakeStore() *fakeStore {
	return &fakeStore{scene: map[int64]scene.Scene{}, nextID: 1}
}

func (f *fakeStore) CreateScene(_ context.Context, ipID int64, in scene.SceneInput) (scene.Scene, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	now := time.Date(2026, 10, 31, 8, 0, 0, 0, time.UTC)
	s := scene.Scene{
		ID: f.nextID, IPID: ipID, Name: in.Name,
		Alias: in.Alias, Description: in.Description, Detail: in.Detail,
		Images: []scene.Image{}, CreatedAt: now, UpdatedAt: now,
	}
	f.nextID++
	f.scene[s.ID] = s
	return s, nil
}

func (f *fakeStore) ListScenes(_ context.Context, ipID int64) ([]scene.Scene, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []scene.Scene
	for _, s := range f.scene {
		if s.IPID == ipID {
			out = append(out, s)
		}
	}
	for i := range out {
		for j := i + 1; j < len(out); j++ {
			if out[j].UpdatedAt.After(out[i].UpdatedAt) {
				out[i], out[j] = out[j], out[i]
			}
		}
	}
	return out, nil
}

func (f *fakeStore) GetScene(_ context.Context, id int64) (scene.Scene, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	s, ok := f.scene[id]
	if !ok {
		return scene.Scene{}, scene.ErrSceneNotFound
	}
	return s, nil
}

func (f *fakeStore) UpdateScene(_ context.Context, id int64, u *scene.SceneUpdate) (scene.Scene, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	s, ok := f.scene[id]
	if !ok {
		return scene.Scene{}, scene.ErrSceneNotFound
	}
	if u.Name != nil {
		s.Name = *u.Name
	}
	if u.Alias != nil {
		s.Alias = *u.Alias
	}
	if u.Description != nil {
		s.Description = *u.Description
	}
	if u.Detail != nil {
		s.Detail = *u.Detail
	}
	f.scene[id] = s
	return s, nil
}

func (f *fakeStore) DeleteScene(_ context.Context, id int64) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.scene[id]; !ok {
		return scene.ErrSceneNotFound
	}
	delete(f.scene, id)
	return nil
}

func (f *fakeStore) CreateImage(_ context.Context, sceneID int64, in scene.ImageInput) (scene.Image, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	s, ok := f.scene[sceneID]
	if !ok {
		return scene.Image{}, scene.ErrSceneNotFound
	}
	img := scene.Image{
		ID: f.nextID, SceneID: sceneID,
		AssetID: in.AssetID, Label: in.Label, Sort: in.Sort,
	}
	f.nextID++
	s.Images = append(s.Images, img)
	f.scene[sceneID] = s
	return img, nil
}

func (f *fakeStore) UpdateImage(_ context.Context, id int64, u *scene.ImageUpdate) (scene.Image, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for sid, s := range f.scene {
		for i := range s.Images {
			if s.Images[i].ID != id {
				continue
			}
			if u.Label != nil {
				s.Images[i].Label = *u.Label
			}
			if u.Sort != nil {
				s.Images[i].Sort = *u.Sort
			}
			f.scene[sid] = s
			return s.Images[i], nil
		}
	}
	return scene.Image{}, scene.ErrImageNotFound
}

func (f *fakeStore) DeleteImage(_ context.Context, id int64) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for sid, s := range f.scene {
		for i := range s.Images {
			if s.Images[i].ID == id {
				s.Images = append(s.Images[:i], s.Images[i+1:]...)
				f.scene[sid] = s
				return nil
			}
		}
	}
	return scene.ErrImageNotFound
}

func (f *fakeStore) GetImage(_ context.Context, id int64) (scene.Image, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, s := range f.scene {
		for i := range s.Images {
			if s.Images[i].ID == id {
				return s.Images[i], nil
			}
		}
	}
	return scene.Image{}, scene.ErrImageNotFound
}

func (f *fakeStore) EnsureSchema(context.Context) error { return nil }

// fakeIPs is the canvas.IPStore double: only the existence seam matters
// (建场景/列场景的 IP 存在性裁决)。
type fakeIPs struct {
	exists map[int64]bool
}

func newFakeIPs() *fakeIPs { return &fakeIPs{exists: map[int64]bool{1: true}} }

func (f *fakeIPs) IPExists(_ context.Context, id int64) (bool, error) {
	return f.exists[id], nil
}

func (f *fakeIPs) ListIPs(context.Context) ([]canvas.IP, error)    { return nil, nil }
func (f *fakeIPs) GetIP(context.Context, int64) (canvas.IP, error) { return canvas.IP{}, canvas.ErrIPNotFound }
func (f *fakeIPs) CreateIP(context.Context, string, string, int64) (canvas.IP, error) {
	return canvas.IP{}, nil
}
func (f *fakeIPs) UpdateIP(context.Context, int64, *canvas.IPUpdate) (canvas.IP, error) {
	return canvas.IP{}, nil
}
func (f *fakeIPs) DeleteIP(context.Context, int64) error { return nil }
func (f *fakeIPs) EnsureSchema(context.Context) error    { return nil }

// newRouter mounts the scene route groups without auth (handler tests
// exercise the JSON seam, not the JWT middleware)。
func newRouter(store *fakeStore) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	h := &scene.Handlers{Store: store, IPs: newFakeIPs()}
	scene.RegisterIPRoutes(r.Group("/ips/:id/scenes"), h)
	scene.RegisterRoutes(r.Group("/scenes"), h)
	scene.RegisterImageRoutes(r.Group("/scene-images"), h)
	return r
}

func do(r *gin.Engine, method, path, body string) *httptest.ResponseRecorder {
	var reader *bytes.Reader
	if body == "" {
		reader = bytes.NewReader(nil)
	} else {
		reader = bytes.NewReader([]byte(body))
	}
	req := httptest.NewRequest(method, path, reader)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

// 建场景:201 + 完整树(空图列表)+ 封面投影字段。断言按 wire 形状
// (snake_case)解码,verify 的是 JSON 缝而非 Go 类型。
func TestHandlerCreateScene(t *testing.T) {
	store := newFakeStore()
	r := newRouter(store)

	w := do(r, "POST", "/ips/1/scenes", `{"name":"老宅院","alias":"陈家大院","description":"一句话","detail":"详细设定"}`)
	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d body %s", w.Code, w.Body.String())
	}
	var created struct {
		ID           int64       `json:"id"`
		IPID         int64       `json:"ip_id"`
		Name         string      `json:"name"`
		Alias        string      `json:"alias"`
		CoverAssetID int64       `json:"cover_asset_id"`
		Images       []scene.Image `json:"images"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if created.Name != "老宅院" || created.IPID != 1 || created.Alias != "陈家大院" ||
		created.CoverAssetID != 0 || len(created.Images) != 0 {
		t.Fatalf("created = %+v", created)
	}

	// 校验:名字必填。
	if w := do(r, "POST", "/ips/1/scenes", `{"name":"  "}`); w.Code != http.StatusBadRequest {
		t.Fatalf("empty name status = %d", w.Code)
	}
	// 校验:超长名字(256 runes)。
	long := strings.Repeat("景", 256)
	if w := do(r, "POST", "/ips/1/scenes", `{"name":"`+long+`"}`); w.Code != http.StatusBadRequest {
		t.Fatalf("long name status = %d", w.Code)
	}
	// 跨 IP:不存在的 IP 拒建(404)。
	if w := do(r, "POST", "/ips/99/scenes", `{"name":"老宅院"}`); w.Code != http.StatusNotFound {
		t.Fatalf("missing ip status = %d", w.Code)
	}
	// 列表同样验 IP 存在性。
	if w := do(r, "GET", "/ips/99/scenes", ""); w.Code != http.StatusNotFound {
		t.Fatalf("list missing ip status = %d", w.Code)
	}
}

// 列表树形状:scenes 数组、图嵌套、cover_asset_id 投影。
func TestHandlerListTreeShape(t *testing.T) {
	store := newFakeStore()
	r := newRouter(store)
	s := mustCreate(t, store)

	if _, err := store.CreateImage(context.Background(), s.ID, scene.ImageInput{
		AssetID: 77, Label: "全景",
	}); err != nil {
		t.Fatalf("seed image: %v", err)
	}

	w := do(r, "GET", "/ips/1/scenes", "")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}
	var body struct {
		Scenes []struct {
			ID           int64  `json:"id"`
			Name         string `json:"name"`
			CoverAssetID int64  `json:"cover_asset_id"`
			Images       []struct {
				AssetID int64  `json:"asset_id"`
				Label   string `json:"label"`
			} `json:"images"`
		} `json:"scenes"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(body.Scenes) != 1 {
		t.Fatalf("scenes = %+v", body.Scenes)
	}
	got := body.Scenes[0]
	if got.ID != s.ID || got.Name != "老宅院" || got.CoverAssetID != 77 {
		t.Fatalf("scene = %+v", got)
	}
	if len(got.Images) != 1 || got.Images[0].AssetID != 77 || got.Images[0].Label != "全景" {
		t.Fatalf("images = %+v", got.Images)
	}
}

// 图操作错误映射与删除:PATCH label 200;不存在的实体 404;删场景 204;
// 畸形请求体与 id 0 → 400。
func TestHandlerImageErrorsAndDelete(t *testing.T) {
	store := newFakeStore()
	r := newRouter(store)
	s := mustCreate(t, store)
	img, err := store.CreateImage(context.Background(), s.ID, scene.ImageInput{AssetID: 5})
	if err != nil {
		t.Fatalf("seed image: %v", err)
	}

	if w := do(r, "PATCH", "/scene-images/"+itoa(img.ID), `{"label":"夜景"}`); w.Code != http.StatusOK {
		t.Fatalf("patch label status = %d body %s", w.Code, w.Body.String())
	}
	if _, err := store.GetImage(context.Background(), img.ID); err != nil {
		t.Fatalf("get image: %v", err)
	}
	if w := do(r, "PATCH", "/scene-images/999", `{"label":"x"}`); w.Code != http.StatusNotFound {
		t.Fatalf("missing image status = %d", w.Code)
	}
	if w := do(r, "POST", "/scenes/999/images", `{"asset_id":1}`); w.Code != http.StatusNotFound {
		t.Fatalf("image under missing scene status = %d", w.Code)
	}
	if w := do(r, "POST", "/scenes/"+itoa(s.ID)+"/images", `{"asset_id":0}`); w.Code != http.StatusBadRequest {
		t.Fatalf("asset_id 0 status = %d", w.Code)
	}
	if w := do(r, "DELETE", "/scenes/"+itoa(s.ID), ""); w.Code != http.StatusNoContent {
		t.Fatalf("delete scene status = %d", w.Code)
	}
	if w := do(r, "GET", "/scenes/"+itoa(s.ID), ""); w.Code != http.StatusNotFound {
		t.Fatalf("get after delete status = %d", w.Code)
	}
	if w := do(r, "POST", "/ips/1/scenes", `not-json`); w.Code != http.StatusBadRequest {
		t.Fatalf("bad body status = %d", w.Code)
	}
	if w := do(r, "DELETE", "/scenes/0", ""); w.Code != http.StatusBadRequest {
		t.Fatalf("id 0 status = %d", w.Code)
	}
}

// 场景 PATCH:name/alias/description/detail 部分更新。
func TestHandlerPatchScene(t *testing.T) {
	store := newFakeStore()
	r := newRouter(store)
	s := mustCreate(t, store)

	w := do(r, "PATCH", "/scenes/"+itoa(s.ID), `{"alias":"陈家大院","detail":"两进院落"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d body %s", w.Code, w.Body.String())
	}
	got, err := store.GetScene(context.Background(), s.ID)
	if err != nil || got.Alias != "陈家大院" || got.Detail != "两进院落" || got.Name != "老宅院" {
		t.Fatalf("patched = %+v, %v", got, err)
	}
	// 空更新体 → 400。
	if w := do(r, "PATCH", "/scenes/"+itoa(s.ID), `{}`); w.Code != http.StatusBadRequest {
		t.Fatalf("empty patch status = %d", w.Code)
	}
	// 名字清空 → 400。
	if w := do(r, "PATCH", "/scenes/"+itoa(s.ID), `{"name":""}`); w.Code != http.StatusBadRequest {
		t.Fatalf("empty name patch status = %d", w.Code)
	}
}

func mustCreate(t *testing.T, store *fakeStore) scene.Scene {
	t.Helper()
	s, err := store.CreateScene(context.Background(), 1, scene.SceneInput{Name: "老宅院"})
	if err != nil {
		t.Fatalf("seed scene: %v", err)
	}
	return s
}

func itoa(n int64) string { return strconv.FormatInt(n, 10) }

// 路由形状冒烟:场景三组路由与画布面的 /ips/:id 及角色库同树注册(gin 对
// 同层不同名的通配符会在启动期 panic,65 号票的 /ips/:id/scenes、/ips/:id/
// props 必须与 50 号票的 /ips/:id、51 号票的 /ips/:id/characters 共用
// :id)。注册成功即无冲突。
func TestRouteShapesCoexistWithIPRoutes(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	h := &scene.Handlers{Store: newFakeStore(), IPs: newFakeIPs()}
	canvas.RegisterIPRoutes(r.Group("/ips"), &canvas.IPHandlers{Store: nil})
	scene.RegisterIPRoutes(r.Group("/ips/:id/scenes"), h)
	scene.RegisterRoutes(r.Group("/scenes"), h)
	scene.RegisterImageRoutes(r.Group("/scene-images"), h)
}
