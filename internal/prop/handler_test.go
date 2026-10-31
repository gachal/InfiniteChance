package prop_test

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
	"github.com/gachal/InfiniteChance/internal/prop"
)

// fakeStore is the in-memory Store double; handler tests run at the HTTP
// seam without either database. 保序:props 按 updated_at DESC,images 按
// sort+id。
type fakeStore struct {
	mu     sync.Mutex
	prop   map[int64]prop.Prop
	nextID int64
}

func newFakeStore() *fakeStore {
	return &fakeStore{prop: map[int64]prop.Prop{}, nextID: 1}
}

func (f *fakeStore) CreateProp(_ context.Context, ipID int64, in prop.PropInput) (prop.Prop, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	now := time.Date(2026, 10, 31, 8, 0, 0, 0, time.UTC)
	p := prop.Prop{
		ID: f.nextID, IPID: ipID, Name: in.Name,
		Alias: in.Alias, Description: in.Description, Detail: in.Detail,
		Images: []prop.Image{}, CreatedAt: now, UpdatedAt: now,
	}
	f.nextID++
	f.prop[p.ID] = p
	return p, nil
}

func (f *fakeStore) ListProps(_ context.Context, ipID int64) ([]prop.Prop, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []prop.Prop
	for _, p := range f.prop {
		if p.IPID == ipID {
			out = append(out, p)
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

func (f *fakeStore) GetProp(_ context.Context, id int64) (prop.Prop, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	p, ok := f.prop[id]
	if !ok {
		return prop.Prop{}, prop.ErrPropNotFound
	}
	return p, nil
}

func (f *fakeStore) UpdateProp(_ context.Context, id int64, u *prop.PropUpdate) (prop.Prop, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	p, ok := f.prop[id]
	if !ok {
		return prop.Prop{}, prop.ErrPropNotFound
	}
	if u.Name != nil {
		p.Name = *u.Name
	}
	if u.Alias != nil {
		p.Alias = *u.Alias
	}
	if u.Description != nil {
		p.Description = *u.Description
	}
	if u.Detail != nil {
		p.Detail = *u.Detail
	}
	f.prop[id] = p
	return p, nil
}

func (f *fakeStore) DeleteProp(_ context.Context, id int64) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.prop[id]; !ok {
		return prop.ErrPropNotFound
	}
	delete(f.prop, id)
	return nil
}

func (f *fakeStore) CreateImage(_ context.Context, propID int64, in prop.ImageInput) (prop.Image, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	p, ok := f.prop[propID]
	if !ok {
		return prop.Image{}, prop.ErrPropNotFound
	}
	img := prop.Image{
		ID: f.nextID, PropID: propID,
		AssetID: in.AssetID, Label: in.Label, Sort: in.Sort,
	}
	f.nextID++
	p.Images = append(p.Images, img)
	f.prop[propID] = p
	return img, nil
}

func (f *fakeStore) UpdateImage(_ context.Context, id int64, u *prop.ImageUpdate) (prop.Image, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for pid, p := range f.prop {
		for i := range p.Images {
			if p.Images[i].ID != id {
				continue
			}
			if u.Label != nil {
				p.Images[i].Label = *u.Label
			}
			if u.Sort != nil {
				p.Images[i].Sort = *u.Sort
			}
			f.prop[pid] = p
			return p.Images[i], nil
		}
	}
	return prop.Image{}, prop.ErrImageNotFound
}

func (f *fakeStore) DeleteImage(_ context.Context, id int64) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for pid, p := range f.prop {
		for i := range p.Images {
			if p.Images[i].ID == id {
				p.Images = append(p.Images[:i], p.Images[i+1:]...)
				f.prop[pid] = p
				return nil
			}
		}
	}
	return prop.ErrImageNotFound
}

func (f *fakeStore) GetImage(_ context.Context, id int64) (prop.Image, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, p := range f.prop {
		for i := range p.Images {
			if p.Images[i].ID == id {
				return p.Images[i], nil
			}
		}
	}
	return prop.Image{}, prop.ErrImageNotFound
}

func (f *fakeStore) EnsureSchema(context.Context) error { return nil }

// fakeIPs is the canvas.IPStore double: only the existence seam matters
// (建道具/列道具的 IP 存在性裁决)。
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

// newRouter mounts the prop route groups without auth (handler tests
// exercise the JSON seam, not the JWT middleware)。
func newRouter(store *fakeStore) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	h := &prop.Handlers{Store: store, IPs: newFakeIPs()}
	prop.RegisterIPRoutes(r.Group("/ips/:id/props"), h)
	prop.RegisterRoutes(r.Group("/props"), h)
	prop.RegisterImageRoutes(r.Group("/prop-images"), h)
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

// 建道具:201 + 完整树(空图列表)+ 封面投影字段。断言按 wire 形状
// (snake_case)解码,verify 的是 JSON 缝而非 Go 类型。
func TestHandlerCreateProp(t *testing.T) {
	store := newFakeStore()
	r := newRouter(store)

	w := do(r, "POST", "/ips/1/props", `{"name":"怀表","alias":"祖传怀表","description":"一句话","detail":"详细设定"}`)
	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d body %s", w.Code, w.Body.String())
	}
	var created struct {
		ID           int64        `json:"id"`
		IPID         int64        `json:"ip_id"`
		Name         string       `json:"name"`
		Alias        string       `json:"alias"`
		CoverAssetID int64        `json:"cover_asset_id"`
		Images       []prop.Image `json:"images"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if created.Name != "怀表" || created.IPID != 1 || created.Alias != "祖传怀表" ||
		created.CoverAssetID != 0 || len(created.Images) != 0 {
		t.Fatalf("created = %+v", created)
	}

	// 校验:名字必填。
	if w := do(r, "POST", "/ips/1/props", `{"name":"  "}`); w.Code != http.StatusBadRequest {
		t.Fatalf("empty name status = %d", w.Code)
	}
	// 校验:超长名字(256 runes)。
	long := strings.Repeat("具", 256)
	if w := do(r, "POST", "/ips/1/props", `{"name":"`+long+`"}`); w.Code != http.StatusBadRequest {
		t.Fatalf("long name status = %d", w.Code)
	}
	// 跨 IP:不存在的 IP 拒建(404)。
	if w := do(r, "POST", "/ips/99/props", `{"name":"怀表"}`); w.Code != http.StatusNotFound {
		t.Fatalf("missing ip status = %d", w.Code)
	}
	// 列表同样验 IP 存在性。
	if w := do(r, "GET", "/ips/99/props", ""); w.Code != http.StatusNotFound {
		t.Fatalf("list missing ip status = %d", w.Code)
	}
}

// 列表树形状:props 数组、图嵌套、cover_asset_id 投影。
func TestHandlerListTreeShape(t *testing.T) {
	store := newFakeStore()
	r := newRouter(store)
	p := mustCreate(t, store)

	if _, err := store.CreateImage(context.Background(), p.ID, prop.ImageInput{
		AssetID: 77, Label: "多视图",
	}); err != nil {
		t.Fatalf("seed image: %v", err)
	}

	w := do(r, "GET", "/ips/1/props", "")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}
	var body struct {
		Props []struct {
			ID           int64  `json:"id"`
			Name         string `json:"name"`
			CoverAssetID int64  `json:"cover_asset_id"`
			Images       []struct {
				AssetID int64  `json:"asset_id"`
				Label   string `json:"label"`
			} `json:"images"`
		} `json:"props"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(body.Props) != 1 {
		t.Fatalf("props = %+v", body.Props)
	}
	got := body.Props[0]
	if got.ID != p.ID || got.Name != "怀表" || got.CoverAssetID != 77 {
		t.Fatalf("prop = %+v", got)
	}
	if len(got.Images) != 1 || got.Images[0].AssetID != 77 || got.Images[0].Label != "多视图" {
		t.Fatalf("images = %+v", got.Images)
	}
}

// 图操作错误映射与删除:PATCH label 200;不存在的实体 404;删道具 204;
// 畸形请求体与 id 0 → 400。
func TestHandlerImageErrorsAndDelete(t *testing.T) {
	store := newFakeStore()
	r := newRouter(store)
	p := mustCreate(t, store)
	img, err := store.CreateImage(context.Background(), p.ID, prop.ImageInput{AssetID: 5})
	if err != nil {
		t.Fatalf("seed image: %v", err)
	}

	if w := do(r, "PATCH", "/prop-images/"+itoa(img.ID), `{"label":"使用态"}`); w.Code != http.StatusOK {
		t.Fatalf("patch label status = %d body %s", w.Code, w.Body.String())
	}
	if _, err := store.GetImage(context.Background(), img.ID); err != nil {
		t.Fatalf("get image: %v", err)
	}
	if w := do(r, "PATCH", "/prop-images/999", `{"label":"x"}`); w.Code != http.StatusNotFound {
		t.Fatalf("missing image status = %d", w.Code)
	}
	if w := do(r, "POST", "/props/999/images", `{"asset_id":1}`); w.Code != http.StatusNotFound {
		t.Fatalf("image under missing prop status = %d", w.Code)
	}
	if w := do(r, "POST", "/props/"+itoa(p.ID)+"/images", `{"asset_id":0}`); w.Code != http.StatusBadRequest {
		t.Fatalf("asset_id 0 status = %d", w.Code)
	}
	if w := do(r, "DELETE", "/props/"+itoa(p.ID), ""); w.Code != http.StatusNoContent {
		t.Fatalf("delete prop status = %d", w.Code)
	}
	if w := do(r, "GET", "/props/"+itoa(p.ID), ""); w.Code != http.StatusNotFound {
		t.Fatalf("get after delete status = %d", w.Code)
	}
	if w := do(r, "POST", "/ips/1/props", `not-json`); w.Code != http.StatusBadRequest {
		t.Fatalf("bad body status = %d", w.Code)
	}
	if w := do(r, "DELETE", "/props/0", ""); w.Code != http.StatusBadRequest {
		t.Fatalf("id 0 status = %d", w.Code)
	}
}

// 道具 PATCH:name/alias/description/detail 部分更新。
func TestHandlerPatchProp(t *testing.T) {
	store := newFakeStore()
	r := newRouter(store)
	p := mustCreate(t, store)

	w := do(r, "PATCH", "/props/"+itoa(p.ID), `{"alias":"祖传怀表","detail":"黄铜表壳"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d body %s", w.Code, w.Body.String())
	}
	got, err := store.GetProp(context.Background(), p.ID)
	if err != nil || got.Alias != "祖传怀表" || got.Detail != "黄铜表壳" || got.Name != "怀表" {
		t.Fatalf("patched = %+v, %v", got, err)
	}
	// 空更新体 → 400。
	if w := do(r, "PATCH", "/props/"+itoa(p.ID), `{}`); w.Code != http.StatusBadRequest {
		t.Fatalf("empty patch status = %d", w.Code)
	}
	// 名字清空 → 400。
	if w := do(r, "PATCH", "/props/"+itoa(p.ID), `{"name":""}`); w.Code != http.StatusBadRequest {
		t.Fatalf("empty name patch status = %d", w.Code)
	}
}

func mustCreate(t *testing.T, store *fakeStore) prop.Prop {
	t.Helper()
	p, err := store.CreateProp(context.Background(), 1, prop.PropInput{Name: "怀表"})
	if err != nil {
		t.Fatalf("seed prop: %v", err)
	}
	return p
}

func itoa(n int64) string { return strconv.FormatInt(n, 10) }

// 路由形状冒烟:道具三组路由与画布面的 /ips/:id 同树注册(gin 对同层
// 不同名的通配符会在启动期 panic,65 号票的 /ips/:id/props 必须与 50 号票
// 的 /ips/:id、51 号票的 /ips/:id/characters 共用 :id)。注册成功即无冲突。
func TestRouteShapesCoexistWithIPRoutes(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	h := &prop.Handlers{Store: newFakeStore(), IPs: newFakeIPs()}
	canvas.RegisterIPRoutes(r.Group("/ips"), &canvas.IPHandlers{Store: nil})
	prop.RegisterIPRoutes(r.Group("/ips/:id/props"), h)
	prop.RegisterRoutes(r.Group("/props"), h)
	prop.RegisterImageRoutes(r.Group("/prop-images"), h)
}
