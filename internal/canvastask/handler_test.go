package canvastask_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/gachal/InfiniteChance/internal/asset"
	"github.com/gachal/InfiniteChance/internal/canvas"
	"github.com/gachal/InfiniteChance/internal/canvastask"
	"github.com/gachal/InfiniteChance/internal/pricing"
)

// fakeTasks is the in-memory Store double; handler tests run at the HTTP
// seam without MySQL (状态机的并发语义由 MySQL store 测试覆盖,这里只求
// 忠实反映状态与绑定).
type fakeTasks struct {
	tasks map[string]canvastask.Task
}

func newFakeTasks() *fakeTasks {
	return &fakeTasks{tasks: map[string]canvastask.Task{}}
}

func (f *fakeTasks) Create(_ context.Context, t canvastask.Task) (canvastask.Task, error) {
	if t.Status == "" {
		t.Status = canvastask.StatusQueued
	}
	now := time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC)
	t.CreatedAt, t.UpdatedAt = now, now
	f.tasks[t.ID] = t
	return t, nil
}

func (f *fakeTasks) Get(_ context.Context, id string) (canvastask.Task, error) {
	t, ok := f.tasks[id]
	if !ok {
		return canvastask.Task{}, canvastask.ErrNotFound
	}
	return t, nil
}

func (f *fakeTasks) ListByCanvas(_ context.Context, canvasID int64, limit int) ([]canvastask.Task, error) {
	var out []canvastask.Task
	for _, t := range f.tasks {
		if t.CanvasID == canvasID {
			out = append(out, t)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func (f *fakeTasks) Claim(context.Context) (canvastask.Task, error) {
	return canvastask.Task{}, canvastask.ErrNotFound
}

func (f *fakeTasks) RequeueRunning(context.Context) (int64, error) { return 0, nil }

func (f *fakeTasks) FinalizeSuccess(_ context.Context, id string, _ asset.Asset) (canvastask.Task, bool, error) {
	t, err := f.Get(context.Background(), id)
	if err != nil {
		return canvastask.Task{}, false, err
	}
	return t, t.Status == canvastask.StatusRunning, nil
}

func (f *fakeTasks) FinalizeFailure(_ context.Context, id, errMsg string) (canvastask.Task, bool, error) {
	t, err := f.Get(context.Background(), id)
	if err != nil {
		return canvastask.Task{}, false, err
	}
	if t.Status != canvastask.StatusRunning {
		return t, false, nil
	}
	t.Status = canvastask.StatusFailed
	t.Error = errMsg
	f.tasks[t.ID] = t
	return t, true, nil
}

func (f *fakeTasks) ResetForRetry(_ context.Context, id string, canvasID int64) (canvastask.Task, error) {
	t, err := f.Get(context.Background(), id)
	if err != nil {
		return canvastask.Task{}, err
	}
	if t.Status != canvastask.StatusFailed || t.CanvasID != canvasID {
		return canvastask.Task{}, canvastask.ErrNotRetryable
	}
	t.Status = canvastask.StatusQueued
	t.Error = ""
	f.tasks[t.ID] = t
	return t, nil
}

func (f *fakeTasks) FinalizeVideoSuccess(_ context.Context, id string, _ asset.Asset) (canvastask.Task, bool, error) {
	t, err := f.Get(context.Background(), id)
	if err != nil {
		return canvastask.Task{}, false, err
	}
	return t, t.Status == canvastask.StatusRunning, nil
}

func (f *fakeTasks) FinalizeCanceled(_ context.Context, id string) (canvastask.Task, bool, error) {
	t, err := f.Get(context.Background(), id)
	if err != nil {
		return canvastask.Task{}, false, err
	}
	if t.Status != canvastask.StatusRunning {
		return t, false, nil
	}
	t.Status = canvastask.StatusCanceled
	f.tasks[t.ID] = t
	return t, true, nil
}

func (f *fakeTasks) Cancel(_ context.Context, id string, canvasID int64) (canvastask.Task, error) {
	t, err := f.Get(context.Background(), id)
	if err != nil {
		return canvastask.Task{}, err
	}
	if t.CanvasID != canvasID || (t.Status != canvastask.StatusQueued && t.Status != canvastask.StatusRunning) {
		return canvastask.Task{}, canvastask.ErrNotCancelable
	}
	t.Status = canvastask.StatusCanceled
	f.tasks[t.ID] = t
	return t, nil
}

func (f *fakeTasks) AttachRemote(_ context.Context, id, remoteTaskID string) (canvastask.Task, bool, error) {
	t, err := f.Get(context.Background(), id)
	if err != nil {
		return canvastask.Task{}, false, err
	}
	if t.Status != canvastask.StatusRunning {
		return t, false, nil
	}
	t.RemoteTaskID = remoteTaskID
	f.tasks[t.ID] = t
	return t, true, nil
}

// fakeCanvases answers Get like a two-canvas world.
type fakeCanvases struct{}

func (fakeCanvases) Get(_ context.Context, id int64) (canvas.Canvas, error) {
	if id == 7 {
		return canvas.Canvas{ID: 7, Name: "主画布"}, nil
	}
	return canvas.Canvas{}, canvas.ErrNotFound
}

// fakePrices answers ByModel for one call-track and one token-track model.
type fakePrices struct{}

func (fakePrices) ByModel(_ context.Context, model string) (pricing.Price, error) {
	switch model {
	case "img-m":
		return pricing.Price{PublicModel: model, Unit: pricing.UnitCall,
			Call: &pricing.CallPrice{USDPerCallMicros: 40_000}}, nil
	case "vid-m":
		return pricing.Price{PublicModel: model, Unit: pricing.UnitSecond,
			Call: &pricing.CallPrice{USDPerCallMicros: 5_000}}, nil
	case "chat-m":
		return pricing.Price{PublicModel: model, Unit: pricing.UnitToken,
			Token: &pricing.TokenPrice{}}, nil
	default:
		return pricing.Price{}, pricing.ErrNotFound
	}
}

func (fakePrices) List(_ context.Context) ([]pricing.Price, error) {
	return []pricing.Price{
		{PublicModel: "chat-m", Unit: pricing.UnitToken, Token: &pricing.TokenPrice{}},
		{PublicModel: "zeta-img", Unit: pricing.UnitCall, Call: &pricing.CallPrice{}},
		{PublicModel: "alpha-img", Unit: pricing.UnitCall, Call: &pricing.CallPrice{}},
		{PublicModel: "vid-m", Unit: pricing.UnitSecond, Call: &pricing.CallPrice{}},
		{PublicModel: "zed-vid", Unit: pricing.UnitSecond, Call: &pricing.CallPrice{}},
	}, nil
}

// handlerEnv wires the /canvases and model-catalog routes the way the binary
// does (minus JWT), with the given stores and gateway.
type handlerEnv struct {
	engine  *gin.Engine
	tasks   *fakeTasks
	gateway *okGateway
}

func newHandlerEnv(gateway *okGateway) *handlerEnv {
	gin.SetMode(gin.TestMode)
	tasks := newFakeTasks()
	r := gin.New()
	var gw canvastask.Gateway
	if gateway != nil {
		gw = gateway
	}
	group := r.Group("/canvases")
	canvastask.RegisterRoutes(group, &canvastask.Handlers{
		Tasks: tasks, Canvases: fakeCanvases{}, Models: fakePrices{}, Gateway: gw,
	})
	canvastask.RegisterModelRoutes(r.Group("/image-models"), &canvastask.ModelHandlers{Prices: fakePrices{}})
	canvastask.RegisterVideoModelRoutes(r.Group("/video-models"), &canvastask.ModelHandlers{Prices: fakePrices{}})
	return &handlerEnv{engine: r, tasks: tasks, gateway: gateway}
}

func (e *handlerEnv) do(t *testing.T, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	w := httptest.NewRecorder()
	e.engine.ServeHTTP(w, req)
	return w
}

func (e *handlerEnv) postTask(t *testing.T, body string) *httptest.ResponseRecorder {
	t.Helper()
	return e.do(t, http.MethodPost, "/canvases/7/tasks", body)
}

// okGateway answers every generation with the same delivered image and
// records video-cancel calls so tests can assert the gateway task was
// canceled alongside the row.
type okGateway struct {
	url     string
	cancels []string
}

func (g *okGateway) GenerateImage(context.Context, canvastask.ImageRequest) (canvastask.ImageResult, error) {
	return canvastask.ImageResult{URL: g.url}, nil
}

func (g *okGateway) SubmitVideo(context.Context, canvastask.VideoRequest) (canvastask.VideoSubmitResult, error) {
	return canvastask.VideoSubmitResult{TaskID: "vt_handler_ok"}, nil
}

func (g *okGateway) PollVideo(context.Context, string) (canvastask.VideoPoll, error) {
	return canvastask.VideoPoll{Status: "running"}, nil
}

func (g *okGateway) CancelVideo(_ context.Context, taskID string) error {
	g.cancels = append(g.cancels, taskID)
	return nil
}

func TestHandlerCreateTaskQueuesWork(t *testing.T) {
	env := newHandlerEnv(&okGateway{url: "https://img.example/ok.png"})

	w := env.postTask(t, `{"node_id":"image-1-1","prompt":"一只在月光下奔跑的猫","model":"img-m","size":"1024x1024"}`)
	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d body %s, want 201", w.Code, w.Body.String())
	}
	var resp struct {
		Task struct {
			ID       string `json:"id"`
			CanvasID int64  `json:"canvas_id"`
			NodeID   string `json:"node_id"`
			Kind     string `json:"kind"`
			Status   string `json:"status"`
			Prompt   string `json:"prompt"`
			Model    string `json:"model"`
			Attempts int64  `json:"attempts"`
			ImageURL string `json:"image_url"`
		} `json:"task"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("response not JSON: %v", err)
	}
	task := resp.Task
	if !strings.HasPrefix(task.ID, "ct_") || task.CanvasID != 7 || task.NodeID != "image-1-1" ||
		task.Kind != "image" || task.Status != "queued" || task.Attempts != 0 || task.ImageURL != "" {
		t.Fatalf("task = %+v, want a queued ct_ task bound to the node", task)
	}
	if _, ok := env.tasks.tasks[task.ID]; !ok {
		t.Errorf("task %s not stored", task.ID)
	}
}

func TestHandlerCreateTaskValidations(t *testing.T) {
	env := newHandlerEnv(&okGateway{url: "https://img.example/ok.png"})

	cases := []struct {
		name string
		code int
		body string
		path string
	}{
		{"未知画布", http.StatusNotFound, `{"node_id":"n","prompt":"p","model":"img-m"}`, "/canvases/99/tasks"},
		{"缺 node_id", http.StatusBadRequest, `{"prompt":"p","model":"img-m"}`, "/canvases/7/tasks"},
		{"未知 kind", http.StatusBadRequest, `{"node_id":"n","prompt":"p","model":"img-m","kind":"audio"}`, "/canvases/7/tasks"},
		{"video 缺参考图", http.StatusBadRequest, `{"node_id":"n","prompt":"p","model":"vid-m","kind":"video"}`, "/canvases/7/tasks"},
		{"video 参考图非 http", http.StatusBadRequest, `{"node_id":"n","prompt":"p","model":"vid-m","kind":"video","image_url":"data:image/png;base64,AAAA"}`, "/canvases/7/tasks"},
		{"video 秒数越界", http.StatusBadRequest, `{"node_id":"n","prompt":"p","model":"vid-m","kind":"video","image_url":"https://img.example/a.png","seconds":101}`, "/canvases/7/tasks"},
		{"video 秒数为零", http.StatusBadRequest, `{"node_id":"n","prompt":"p","model":"vid-m","kind":"video","image_url":"https://img.example/a.png","seconds":0}`, "/canvases/7/tasks"},
		{"video 模型非按秒轨", http.StatusBadRequest, `{"node_id":"n","prompt":"p","model":"img-m","kind":"video","image_url":"https://img.example/a.png"}`, "/canvases/7/tasks"},
		{"空 prompt", http.StatusBadRequest, `{"node_id":"n","prompt":"  ","model":"img-m"}`, "/canvases/7/tasks"},
		{"缺 model", http.StatusBadRequest, `{"node_id":"n","prompt":"p"}`, "/canvases/7/tasks"},
		{"模型未配价", http.StatusBadRequest, `{"node_id":"n","prompt":"p","model":"nope"}`, "/canvases/7/tasks"},
		{"生图模型非按次轨", http.StatusBadRequest, `{"node_id":"n","prompt":"p","model":"chat-m"}`, "/canvases/7/tasks"},
		{"坏画布 id", http.StatusBadRequest, `{}`, "/canvases/zero/tasks"},
	}
	for _, tc := range cases {
		w := env.do(t, http.MethodPost, tc.path, tc.body)
		if w.Code != tc.code {
			t.Errorf("%s: status = %d body %s, want %d", tc.name, w.Code, w.Body.String(), tc.code)
		}
	}

	// model_not_priced 的机器码要稳定,前端按它提示。
	w := env.do(t, http.MethodPost, "/canvases/7/tasks", `{"node_id":"n","prompt":"p","model":"nope"}`)
	if !strings.Contains(w.Body.String(), "model_not_priced") {
		t.Errorf("body = %s, want the model_not_priced code", w.Body.String())
	}
}

func TestHandlerCreateWithoutGatewayAnswers503(t *testing.T) {
	env := newHandlerEnv(nil)
	w := env.postTask(t, `{"node_id":"n","prompt":"p","model":"img-m"}`)
	if w.Code != http.StatusServiceUnavailable || !strings.Contains(w.Body.String(), "gateway_unconfigured") {
		t.Fatalf("status = %d body %s, want 503 gateway_unconfigured", w.Code, w.Body.String())
	}
}

func TestHandlerListAndGetTasks(t *testing.T) {
	env := newHandlerEnv(&okGateway{url: "https://img.example/ok.png"})
	if w := env.postTask(t, `{"node_id":"image-1-1","prompt":"p1","model":"img-m"}`); w.Code != http.StatusCreated {
		t.Fatalf("seed 1 = %d, want 201", w.Code)
	}
	if w := env.postTask(t, `{"node_id":"image-1-2","prompt":"p2","model":"img-m"}`); w.Code != http.StatusCreated {
		t.Fatalf("seed 2 = %d, want 201", w.Code)
	}
	ids := make([]string, 0, 2)
	for id := range env.tasks.tasks {
		ids = append(ids, id)
	}

	w := env.do(t, http.MethodGet, "/canvases/7/tasks", "")
	if w.Code != http.StatusOK {
		t.Fatalf("list status = %d, want 200", w.Code)
	}
	var list struct {
		Tasks []struct {
			ID     string `json:"id"`
			Status string `json:"status"`
		} `json:"tasks"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &list); err != nil {
		t.Fatalf("list not JSON: %v", err)
	}
	if len(list.Tasks) != 2 {
		t.Fatalf("tasks = %d, want 2", len(list.Tasks))
	}

	w = env.do(t, http.MethodGet, "/canvases/7/tasks/"+ids[0], "")
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), ids[0]) {
		t.Fatalf("get = %d %s, want the task echoed", w.Code, w.Body.String())
	}
	// 别的画布的任务按不存在回答。
	w = env.do(t, http.MethodGet, "/canvases/8/tasks/"+ids[0], "")
	if w.Code != http.StatusNotFound {
		t.Errorf("cross-canvas get = %d, want 404", w.Code)
	}
	w = env.do(t, http.MethodGet, "/canvases/7/tasks/ct_missing", "")
	if w.Code != http.StatusNotFound {
		t.Errorf("missing get = %d, want 404", w.Code)
	}
}

func TestHandlerRetryRequeuesFailedTask(t *testing.T) {
	env := newHandlerEnv(&okGateway{url: "https://img.example/ok.png"})
	w := env.postTask(t, `{"node_id":"image-1-1","prompt":"p","model":"img-m"}`)
	var created struct {
		Task struct {
			ID string `json:"id"`
		} `json:"task"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &created); err != nil {
		t.Fatalf("create not JSON: %v", err)
	}
	id := created.Task.ID

	// 排队中的任务不可重试。
	if r := env.do(t, http.MethodPost, "/canvases/7/tasks/"+id+"/retry", ""); r.Code != http.StatusConflict {
		t.Fatalf("queued retry = %d, want 409", r.Code)
	}
	// 失败后可原地重试:回 200 且状态回 queued。
	failed := env.tasks.tasks[id]
	failed.Status = canvastask.StatusFailed
	failed.Error = "boom"
	env.tasks.tasks[id] = failed
	r := env.do(t, http.MethodPost, "/canvases/7/tasks/"+id+"/retry", "")
	if r.Code != http.StatusOK {
		t.Fatalf("failed retry = %d body %s, want 200", r.Code, r.Body.String())
	}
	if got := env.tasks.tasks[id]; got.Status != canvastask.StatusQueued || got.Error != "" {
		t.Errorf("task after retry = %+v, want queued with cleared failure", got)
	}
	// 未知任务 404。
	if r := env.do(t, http.MethodPost, "/canvases/7/tasks/ct_missing/retry", ""); r.Code != http.StatusNotFound {
		t.Errorf("missing retry = %d, want 404", r.Code)
	}
}

func TestHandlerRetryWithoutGatewayAnswers503(t *testing.T) {
	env := newHandlerEnv(nil)
	if r := env.do(t, http.MethodPost, "/canvases/7/tasks/ct_x/retry", ""); r.Code != http.StatusServiceUnavailable {
		t.Errorf("retry without gateway = %d, want 503", r.Code)
	}
}

func TestHandlerImageModelsListsCallTrackOnly(t *testing.T) {
	env := newHandlerEnv(&okGateway{url: "x"})
	w := env.do(t, http.MethodGet, "/image-models", "")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	var resp struct {
		Models []string `json:"models"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("not JSON: %v", err)
	}
	if len(resp.Models) != 2 || resp.Models[0] != "alpha-img" || resp.Models[1] != "zeta-img" {
		t.Errorf("models = %v, want the two call-track models sorted", resp.Models)
	}
}

func TestHandlerCreateVideoTaskQueuesWork(t *testing.T) {
	env := newHandlerEnv(&okGateway{url: "https://img.example/ok.png"})

	body := `{"node_id":"video-1-1","kind":"video","prompt":"镜头缓缓推进","model":"vid-m","image_url":"https://img.example/cat.png"}`
	w := env.postTask(t, body)
	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d body %s, want 201", w.Code, w.Body.String())
	}
	var resp struct {
		Task struct {
			ID      string `json:"id"`
			Kind    string `json:"kind"`
			Status  string `json:"status"`
			Seconds int64  `json:"seconds"`
		} `json:"task"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("response not JSON: %v", err)
	}
	task := resp.Task
	if !strings.HasPrefix(task.ID, "ct_") || task.Kind != "video" ||
		task.Status != "queued" || task.Seconds != 5 {
		t.Fatalf("task = %+v, want a queued video task defaulting to 5s", task)
	}
	// 参考图与秒数落库(worker 提交网关时要用);参考图不进任务 JSON。
	stored := env.tasks.tasks[task.ID]
	if stored.ImageRef != "https://img.example/cat.png" || stored.Seconds != 5 {
		t.Errorf("stored = %+v, want the reference image and clip length on the row", stored)
	}

	// 显式秒数生效。
	w = env.postTask(t, `{"node_id":"video-1-2","kind":"video","prompt":"p","model":"vid-m","image_url":"https://img.example/a.png","seconds":10}`)
	if w.Code != http.StatusCreated {
		t.Fatalf("explicit seconds status = %d body %s, want 201", w.Code, w.Body.String())
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("response not JSON: %v", err)
	}
	if resp.Task.Seconds != 10 {
		t.Errorf("seconds = %d, want 10", resp.Task.Seconds)
	}
}

func TestHandlerCancelClosesActiveVideoTask(t *testing.T) {
	env := newHandlerEnv(&okGateway{url: "x"})
	w := env.postTask(t, `{"node_id":"video-1-1","kind":"video","prompt":"p","model":"vid-m","image_url":"https://img.example/a.png"}`)
	var created struct {
		Task struct {
			ID string `json:"id"`
		} `json:"task"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &created); err != nil {
		t.Fatalf("create not JSON: %v", err)
	}
	id := created.Task.ID

	// 任务在途(网关句柄已回填)时取消:行关闭 + 网关任务同步取消。
	running := env.tasks.tasks[id]
	running.Status = canvastask.StatusRunning
	running.RemoteTaskID = "vt_remote_1"
	env.tasks.tasks[id] = running

	r := env.do(t, http.MethodPost, "/canvases/7/tasks/"+id+"/cancel", "")
	if r.Code != http.StatusOK {
		t.Fatalf("cancel status = %d body %s, want 200", r.Code, r.Body.String())
	}
	if got := env.tasks.tasks[id]; got.Status != canvastask.StatusCanceled {
		t.Errorf("task after cancel = %+v, want canceled", got)
	}
	if len(env.gateway.cancels) != 1 || env.gateway.cancels[0] != "vt_remote_1" {
		t.Errorf("gateway cancels = %v, want exactly the remote handle canceled", env.gateway.cancels)
	}

	// 终态不可取消,但如实回放现况(与网关取消契约同形)。
	r = env.do(t, http.MethodPost, "/canvases/7/tasks/"+id+"/cancel", "")
	if r.Code != http.StatusOK || !strings.Contains(r.Body.String(), "canceled") {
		t.Errorf("terminal cancel = %d %s, want the stored row echoed", r.Code, r.Body.String())
	}
	if len(env.gateway.cancels) != 1 {
		t.Errorf("gateway cancels = %v, want the terminal task left alone", env.gateway.cancels)
	}

	// 未知任务 404;别的画布的任务同样 404。
	if r := env.do(t, http.MethodPost, "/canvases/7/tasks/ct_missing/cancel", ""); r.Code != http.StatusNotFound {
		t.Errorf("missing cancel = %d, want 404", r.Code)
	}
}

func TestHandlerCancelQueuedTaskSkipsGatewayCancel(t *testing.T) {
	env := newHandlerEnv(&okGateway{url: "x"})
	w := env.postTask(t, `{"node_id":"video-1-1","kind":"video","prompt":"p","model":"vid-m","image_url":"https://img.example/a.png"}`)
	var created struct {
		Task struct {
			ID string `json:"id"`
		} `json:"task"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &created); err != nil {
		t.Fatalf("create not JSON: %v", err)
	}
	// 还在排队、远端句柄为空:取消只关行,网关侧无账可平。
	r := env.do(t, http.MethodPost, "/canvases/7/tasks/"+created.Task.ID+"/cancel", "")
	if r.Code != http.StatusOK {
		t.Fatalf("queued cancel = %d body %s, want 200", r.Code, r.Body.String())
	}
	if got := env.tasks.tasks[created.Task.ID]; got.Status != canvastask.StatusCanceled {
		t.Errorf("task after cancel = %+v, want canceled", got)
	}
	if len(env.gateway.cancels) != 0 {
		t.Errorf("gateway cancels = %v, want none before the submit was accepted", env.gateway.cancels)
	}
}

func TestHandlerVideoModelsListsSecondTrackOnly(t *testing.T) {
	env := newHandlerEnv(&okGateway{url: "x"})
	w := env.do(t, http.MethodGet, "/video-models", "")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	var resp struct {
		Models []string `json:"models"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("not JSON: %v", err)
	}
	if len(resp.Models) != 2 || resp.Models[0] != "vid-m" || resp.Models[1] != "zed-vid" {
		t.Errorf("models = %v, want the two second-track models sorted", resp.Models)
	}
}

func TestHandlerCancelRejectsImageTasks(t *testing.T) {
	env := newHandlerEnv(&okGateway{url: "x"})
	w := env.postTask(t, `{"node_id":"image-1-1","prompt":"p","model":"img-m"}`)
	var created struct {
		Task struct {
			ID string `json:"id"`
		} `json:"task"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &created); err != nil {
		t.Fatalf("create not JSON: %v", err)
	}

	// 同步生图没有可撤销的提交:取消语义只属于视频任务。
	r := env.do(t, http.MethodPost, "/canvases/7/tasks/"+created.Task.ID+"/cancel", "")
	if r.Code != http.StatusConflict || !strings.Contains(r.Body.String(), "task_not_cancelable") {
		t.Fatalf("image cancel = %d %s, want 409 task_not_cancelable", r.Code, r.Body.String())
	}
	if got := env.tasks.tasks[created.Task.ID]; got.Status != canvastask.StatusQueued {
		t.Errorf("task after cancel attempt = %+v, want the queued state to stand", got)
	}
	if len(env.gateway.cancels) != 0 {
		t.Errorf("gateway cancels = %v, want none for an image task", env.gateway.cancels)
	}
}
