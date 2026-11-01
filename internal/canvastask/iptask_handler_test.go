package canvastask_test

// 无画布任务端点族(70 号票)的 HTTP 缝测试:与 handler_test.go 共用同一
// 批 fake(fakeTasks/fakePrices/fakeAssets/okGateway),只新增 IP 存在性
// 与角色树的桩。

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/gachal/InfiniteChance/internal/canvas"
	"github.com/gachal/InfiniteChance/internal/canvastask"
	"github.com/gachal/InfiniteChance/internal/character"
)

// fakeIPStore 只回答两问:存在性与画面风格(69 号票 Style 消费);其余
// CRUD 嵌接口恒零值(handler 不触)。
type fakeIPStore struct {
	canvas.IPStore
}

func (fakeIPStore) GetIP(_ context.Context, id int64) (canvas.IP, error) {
	if id == 3 {
		return canvas.IP{ID: 3, Name: "测试企划", Style: "水彩淡彩"}, nil
	}
	return canvas.IP{}, canvas.ErrIPNotFound
}

func (fakeIPStore) IPExists(_ context.Context, id int64) (bool, error) {
	return id == 3, nil
}

// fakeCharacters 是 IP 任务 handler 需要的角色切片;其余方法嵌接口恒零值。
type fakeCharacters struct {
	character.Store
	byID map[int64]character.Character
}

func (f fakeCharacters) GetCharacter(_ context.Context, id int64) (character.Character, error) {
	if c, ok := f.byID[id]; ok {
		return c, nil
	}
	return character.Character{}, character.ErrCharacterNotFound
}

// ipTaskEnv wires the /ips/:id/tasks routes the way wiring.go does
// (minus JWT), sharing the fakeTasks world of the canvas-handler env.
type ipTaskEnv struct {
	engine *gin.Engine
	tasks  *fakeTasks
}

func newIPTaskEnv(gateway *okGateway) *ipTaskEnv {
	gin.SetMode(gin.TestMode)
	tasks := newFakeTasks()
	r := gin.New()
	var gw canvastask.Gateway
	if gateway != nil {
		gw = gateway
	}
	canvastask.RegisterIPRoutes(r.Group("/ips/:id/tasks"), &canvastask.Handlers{
		Tasks: tasks,
		IPs:   fakeIPStore{},
		Characters: fakeCharacters{byID: map[int64]character.Character{
			11: {ID: 11, IPID: 3, Name: "林小满", Variants: []character.Variant{
				{ID: 21, CharacterID: 11, Name: "冬季制服", Note: "深蓝色大衣"},
			}},
			12: {ID: 12, IPID: 9, Name: "别的企划的角色", Variants: []character.Variant{
				{ID: 22, CharacterID: 12, Name: "默认"},
			}},
		}},
		Models:  fakePrices{},
		Assets:  &fakeAssets{},
		Gateway: gw,
	})
	return &ipTaskEnv{engine: r, tasks: tasks}
}

func (e *ipTaskEnv) do(t *testing.T, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	w := httptest.NewRecorder()
	e.engine.ServeHTTP(w, req)
	return w
}

func TestIPCreateAssemblesPromptAndLandsCanvasFreeTask(t *testing.T) {
	e := newIPTaskEnv(&okGateway{url: "https://img.example/out.png"})
	w := e.do(t, http.MethodPost, "/ips/3/tasks",
		`{"character_id":11,"variant_id":21,"sheet_type":"三视图","model":"img-m","size":"1024x1024"}`)
	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d body %s, want 201", w.Code, w.Body.String())
	}
	tasks := e.tasks.all()
	if len(tasks) != 1 {
		t.Fatalf("tasks = %+v, want one row", tasks)
	}
	got := tasks[0]
	if got.CanvasID != 0 || got.NodeID != "" || got.IPID != 3 {
		t.Fatalf("canvas/node/ip = %d/%q/%d, want 0/空/3(无画布任务)", got.CanvasID, got.NodeID, got.IPID)
	}
	if got.Kind != canvastask.KindImage {
		t.Fatalf("kind = %q, want image(IP 端点仅 image)", got.Kind)
	}
	// 拼装落行可审计:风格段(该 IP 定了水彩淡彩)+ 模板 + 四件套 + 变体。
	for _, want := range []string{
		"【画面风格】水彩淡彩\n",
		"角色设定图·三视图:",
		"角色:林小满",
		"造型:冬季制服",
		"服装:深蓝色大衣",
	} {
		if !strings.Contains(got.Prompt, want) {
			t.Fatalf("prompt %q missing %q", got.Prompt, want)
		}
	}
	if got.Model != "img-m" || got.Size != "1024x1024" {
		t.Fatalf("model/size = %q/%q, want img-m/1024x1024", got.Model, got.Size)
	}
}

func TestIPCreateValidatesIPCharacterVariantAndType(t *testing.T) {
	e := newIPTaskEnv(&okGateway{url: "https://img.example/out.png"})
	cases := []struct {
		name string
		path string
		body string
		want int
	}{
		{"IP 不存在", "/ips/404/tasks", `{"character_id":11,"variant_id":21,"sheet_type":"三视图","model":"img-m"}`, http.StatusNotFound},
		{"角色不存在", "/ips/3/tasks", `{"character_id":99,"variant_id":21,"sheet_type":"三视图","model":"img-m"}`, http.StatusNotFound},
		{"角色不属于本 IP", "/ips/3/tasks", `{"character_id":12,"variant_id":22,"sheet_type":"三视图","model":"img-m"}`, http.StatusNotFound},
		{"变体不属于该角色", "/ips/3/tasks", `{"character_id":11,"variant_id":22,"sheet_type":"三视图","model":"img-m"}`, http.StatusBadRequest},
		{"类型不在五类", "/ips/3/tasks", `{"character_id":11,"variant_id":21,"sheet_type":"其他","model":"img-m"}`, http.StatusBadRequest},
		{"模型未配价", "/ips/3/tasks", `{"character_id":11,"variant_id":21,"sheet_type":"三视图","model":"nope"}`, http.StatusBadRequest},
		{"背景值非法", "/ips/3/tasks", `{"character_id":11,"variant_id":21,"sheet_type":"三视图","model":"img-m","background":"black"}`, http.StatusBadRequest},
		{"缺模型", "/ips/3/tasks", `{"character_id":11,"variant_id":21,"sheet_type":"三视图"}`, http.StatusBadRequest},
	}
	for _, tc := range cases {
		w := e.do(t, http.MethodPost, tc.path, tc.body)
		if w.Code != tc.want {
			t.Fatalf("%s: status = %d body %s, want %d", tc.name, w.Code, w.Body.String(), tc.want)
		}
	}
}

func TestIPCreateRejectsWithoutGateway(t *testing.T) {
	e := newIPTaskEnv(nil)
	w := e.do(t, http.MethodPost, "/ips/3/tasks",
		`{"character_id":11,"variant_id":21,"sheet_type":"三视图","model":"img-m"}`)
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503 gateway_unconfigured", w.Code)
	}
	if !strings.Contains(w.Body.String(), "gateway_unconfigured") {
		t.Fatalf("body missing gateway_unconfigured: %s", w.Body.String())
	}
}

func TestIPCreateResolvesImageRefs(t *testing.T) {
	e := newIPTaskEnv(&okGateway{url: "https://img.example/out.png"})
	w := e.do(t, http.MethodPost, "/ips/3/tasks",
		`{"character_id":11,"variant_id":21,"sheet_type":"多视图","model":"img-m","image_urls":["/api/assets/5/content","data:image/png;base64,AAAA"]}`)
	// data: URI 进不了网关媒体契约:整条拒收,不落任务行。
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d body %s, want 400", w.Code, w.Body.String())
	}
	if len(e.tasks.all()) != 0 {
		t.Fatalf("rejected submit must not land a task row")
	}

	w = e.do(t, http.MethodPost, "/ips/3/tasks",
		`{"character_id":11,"variant_id":21,"sheet_type":"多视图","model":"img-m","image_urls":["/api/assets/5/content"]}`)
	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d body %s, want 201", w.Code, w.Body.String())
	}
	got := e.tasks.all()[0]
	if len(got.ImageRefs) != 1 || got.ImageRefs[0] != "https://img.example/ref.png" {
		t.Fatalf("image_refs = %v, want resolved vendor address", got.ImageRefs)
	}
}

func TestIPListAndIPGetScopeByIP(t *testing.T) {
	e := newIPTaskEnv(&okGateway{url: "https://img.example/out.png"})
	if w := e.do(t, http.MethodPost, "/ips/3/tasks",
		`{"character_id":11,"variant_id":21,"sheet_type":"三视图","model":"img-m"}`); w.Code != http.StatusCreated {
		t.Fatalf("seed create: %d %s", w.Code, w.Body.String())
	}
	created := e.tasks.all()[0]

	w := e.do(t, http.MethodGet, "/ips/3/tasks", "")
	if w.Code != http.StatusOK {
		t.Fatalf("list status = %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), created.ID) {
		t.Fatalf("list missing created task: %s", w.Body.String())
	}

	// 单查轮询:归属 IP 命中。
	w = e.do(t, http.MethodGet, "/ips/3/tasks/"+created.ID, "")
	if w.Code != http.StatusOK {
		t.Fatalf("get status = %d", w.Code)
	}
	// 别的 IP 查不到(命名空间按 IP 收窄,同画布任务语义)。
	w = e.do(t, http.MethodGet, "/ips/8/tasks/"+created.ID, "")
	if w.Code != http.StatusNotFound {
		t.Fatalf("cross-ip get = %d, want 404", w.Code)
	}
	// 不存在的任务 404。
	w = e.do(t, http.MethodGet, "/ips/3/tasks/ct_missing", "")
	if w.Code != http.StatusNotFound {
		t.Fatalf("missing get = %d, want 404", w.Code)
	}
}
