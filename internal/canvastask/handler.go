package canvastask

import (
	"context"
	"errors"
	"log"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/gin-gonic/gin"

	"github.com/gachal/InfiniteChance/internal/apierr"
	"github.com/gachal/InfiniteChance/internal/canvas"
	"github.com/gachal/InfiniteChance/internal/pricing"
)

// 输入上限与既有约定对齐:node_id 与画布名同宽(列都是 VARCHAR(128)),
// model 走 pricing 的公开模型名上限,size 走 call 轨的尺寸上限。prompt 的
// 8k 字符是宽上限 —— 真正的厂商限制在网关转发时自然暴露,任务行会留下原因。
const (
	maxNodeIDRunes = 128
	maxPromptRunes = 8000
	listLimit      = 200
)

// CanvasGetter is the slice of the canvas store the handlers need: a task
// may only be created on an existing canvas.
type CanvasGetter interface {
	Get(ctx context.Context, id int64) (canvas.Canvas, error)
}

// ModelPricer is the slice of the pricing store the handlers need: the
// submit-time price check and the image-model catalog.
type ModelPricer interface {
	List(ctx context.Context) ([]pricing.Price, error)
	ByModel(ctx context.Context, publicModel string) (pricing.Price, error)
}

// Handlers serves the creator canvas-task endpoints. canvas/server mounts
// them on the authed /canvases group — every route requires a gateway-issued
// session. A nil Gateway means canvas/server runs without a service key:
// submits are refused up front instead of queueing tasks that can never run.
type Handlers struct {
	Tasks    Store
	Canvases CanvasGetter
	Models   ModelPricer
	Gateway  Gateway
}

// RegisterRoutes mounts (relative to the group, which the binary mounts at
// /canvases behind the JWT middleware, alongside the canvas CRUD routes):
//
//	POST /:id/tasks            — submit a generation {node_id, prompt, model, …}
//	GET  /:id/tasks            — the canvas's recent tasks (reconnect & reconcile)
//	GET  /:id/tasks/:tid       — one task (editor polling)
//	POST /:id/tasks/:tid/retry — requeue a failed task (原地重试)
func RegisterRoutes(group *gin.RouterGroup, h *Handlers) {
	group.POST("/:id/tasks", h.Create)
	group.GET("/:id/tasks", h.List)
	group.GET("/:id/tasks/:tid", h.Get)
	group.POST("/:id/tasks/:tid/retry", h.Retry)
}

// taskJSON is the wire form of a task. image_url lets the editor render the
// result without a second round-trip to the asset; asset_id keeps the
// library reference (14 号票的跨画布复用按它走).
type taskJSON struct {
	ID        string    `json:"id"`
	CanvasID  int64     `json:"canvas_id"`
	NodeID    string    `json:"node_id"`
	Kind      string    `json:"kind"`
	Prompt    string    `json:"prompt"`
	Model     string    `json:"model"`
	Size      string    `json:"size"`
	Status    Status    `json:"status"`
	Attempts  int64     `json:"attempts"`
	Error     string    `json:"error"`
	AssetID   int64     `json:"asset_id"`
	ImageURL  string    `json:"image_url"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

func toTaskJSON(t Task) taskJSON {
	return taskJSON{
		ID: t.ID, CanvasID: t.CanvasID, NodeID: t.NodeID, Kind: t.Kind,
		Prompt: t.Prompt, Model: t.Model, Size: t.Size, Status: t.Status,
		Attempts: t.Attempts, Error: t.Error, AssetID: t.AssetID, ImageURL: t.ImageURL,
		CreatedAt: t.CreatedAt, UpdatedAt: t.UpdatedAt,
	}
}

type createInput struct {
	NodeID string `json:"node_id"`
	Kind   string `json:"kind"`
	Prompt string `json:"prompt"`
	Model  string `json:"model"`
	Size   string `json:"size"`
}

// Create accepts one generation for the canvas. The task row lands queued —
// the worker owns everything after this point, so the submit returns as soon
// as the row is durable and the browser may vanish freely.
func (h *Handlers) Create(c *gin.Context) {
	canvasID, ok := bindID(c)
	if !ok {
		return
	}
	if _, err := h.Canvases.Get(c.Request.Context(), canvasID); err != nil {
		h.failCanvas(c, err)
		return
	}
	if !h.requireGateway(c) {
		return
	}

	var in createInput
	if err := c.ShouldBindJSON(&in); err != nil {
		apierr.InvalidRequest(c, "请求体必须是 {node_id, prompt, model} JSON")
		return
	}
	nodeID := strings.TrimSpace(in.NodeID)
	if nodeID == "" {
		apierr.InvalidRequest(c, "node_id 不能为空")
		return
	}
	if utf8.RuneCountInString(nodeID) > maxNodeIDRunes {
		apierr.InvalidRequest(c, "node_id 最多 128 个字符")
		return
	}
	kind := strings.TrimSpace(in.Kind)
	if kind == "" {
		kind = KindImage
	}
	if kind != KindImage {
		apierr.InvalidRequest(c, "目前只支持 image 类型的生成任务")
		return
	}
	prompt := strings.TrimSpace(in.Prompt)
	if prompt == "" {
		apierr.InvalidRequest(c, "prompt 不能为空")
		return
	}
	if utf8.RuneCountInString(prompt) > maxPromptRunes {
		apierr.InvalidRequest(c, "prompt 最多 8000 个字符")
		return
	}
	model := strings.TrimSpace(in.Model)
	if model == "" {
		apierr.InvalidRequest(c, "model 不能为空")
		return
	}
	if utf8.RuneCountInString(model) > pricing.ModelNameRunes {
		apierr.InvalidRequest(c, "model 名最多 200 个字符")
		return
	}
	size := strings.TrimSpace(in.Size)
	if utf8.RuneCountInString(size) > pricing.MaxSizeRunes {
		apierr.InvalidRequest(c, "size 最多 64 个字符")
		return
	}

	// 提交前先看价:模型没有按次计价时,生成注定失败 —— 让用户立刻知道,
	// 而不是排队后才在节点上看到失败。
	price, err := h.Models.ByModel(c.Request.Context(), model)
	if errors.Is(err, pricing.ErrNotFound) {
		apierr.Write(c, http.StatusBadRequest, "model_not_priced",
			"模型 "+model+" 未配置按次计价,请先在管理端配置价格")
		return
	}
	if err != nil {
		h.failStore(c, err)
		return
	}
	if price.Unit != pricing.UnitCall || price.Call == nil {
		apierr.Write(c, http.StatusBadRequest, "model_not_priced",
			"模型 "+model+" 不是按次计价的生图模型")
		return
	}

	id, err := NewID()
	if err != nil {
		h.failStore(c, err)
		return
	}
	task, err := h.Tasks.Create(c.Request.Context(), Task{
		ID: id, CanvasID: canvasID, NodeID: nodeID, Kind: kind,
		Prompt: prompt, Model: model, Size: size,
	})
	if err != nil {
		h.failStore(c, err)
		return
	}
	c.JSON(http.StatusCreated, gin.H{"task": toTaskJSON(task)})
}

// List answers the canvas's recent tasks, newest first. The editor calls it
// once on load (reconcile task-bound nodes back into the graph) and on every
// poll tick while tasks are in flight.
func (h *Handlers) List(c *gin.Context) {
	canvasID, ok := bindID(c)
	if !ok {
		return
	}
	tasks, err := h.Tasks.ListByCanvas(c.Request.Context(), canvasID, listLimit)
	if err != nil {
		h.failStore(c, err)
		return
	}
	out := make([]taskJSON, 0, len(tasks))
	for _, t := range tasks {
		out = append(out, toTaskJSON(t))
	}
	c.JSON(http.StatusOK, gin.H{"tasks": out})
}

// Get answers one task. A task from another canvas answers 404 like a
// missing one — the id namespace is scoped by canvas.
func (h *Handlers) Get(c *gin.Context) {
	canvasID, ok := bindID(c)
	if !ok {
		return
	}
	task, ok := h.loadTask(c, canvasID)
	if !ok {
		return
	}
	c.JSON(http.StatusOK, gin.H{"task": toTaskJSON(task)})
}

// Retry sends one failed task back to the queue. The row keeps its id and
// node binding, so the editor's retry button just re-polls the same task.
func (h *Handlers) Retry(c *gin.Context) {
	canvasID, ok := bindID(c)
	if !ok {
		return
	}
	if !h.requireGateway(c) {
		return
	}
	taskID := strings.TrimSpace(c.Param("tid"))
	task, err := h.Tasks.ResetForRetry(c.Request.Context(), taskID, canvasID)
	switch {
	case err == nil:
		c.JSON(http.StatusOK, gin.H{"task": toTaskJSON(task)})
	case errors.Is(err, ErrNotFound):
		apierr.NotFound(c, "任务不存在")
	case errors.Is(err, ErrNotRetryable):
		apierr.Conflict(c, "task_not_retryable", "只有失败的任务可以重试")
	default:
		h.failStore(c, err)
	}
}

// requireGateway guards mutations that would queue unworkable work when no
// service key is configured.
func (h *Handlers) requireGateway(c *gin.Context) bool {
	if h.Gateway != nil {
		return true
	}
	apierr.Write(c, http.StatusServiceUnavailable, "gateway_unconfigured",
		"画布服务未配置网关服务 key(CANVAS_SERVICE_KEY),暂时不能生成")
	return false
}

// loadTask fetches a task id from the path and verifies it belongs to the
// canvas in the path.
func (h *Handlers) loadTask(c *gin.Context, canvasID int64) (Task, bool) {
	taskID := strings.TrimSpace(c.Param("tid"))
	task, err := h.Tasks.Get(c.Request.Context(), taskID)
	if errors.Is(err, ErrNotFound) {
		apierr.NotFound(c, "任务不存在")
		return Task{}, false
	}
	if err != nil {
		h.failStore(c, err)
		return Task{}, false
	}
	if task.CanvasID != canvasID {
		apierr.NotFound(c, "任务不存在")
		return Task{}, false
	}
	return task, true
}

// bindID parses the :id path segment; nonsense ids answer 400 so a broken
// client sees its own bug instead of a misleading 404.
func bindID(c *gin.Context) (int64, bool) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id < 1 {
		apierr.InvalidRequest(c, "画布 id 必须是正整数")
		return 0, false
	}
	return id, true
}

func (h *Handlers) failCanvas(c *gin.Context, err error) {
	if errors.Is(err, canvas.ErrNotFound) {
		apierr.NotFound(c, "画布不存在")
		return
	}
	log.Printf("canvastask: %s %s: %v", c.Request.Method, c.Request.URL.Path, err)
	apierr.Internal(c, "服务内部错误,请稍后再试")
}

func (h *Handlers) failStore(c *gin.Context, err error) {
	log.Printf("canvastask: %s %s: %v", c.Request.Method, c.Request.URL.Path, err)
	apierr.Internal(c, "服务内部错误,请稍后再试")
}

// ModelHandlers serves the image-model catalog for the editor's generate UI:
// the public models priced on the call track — exactly what a text-to-image
// submit may use.
type ModelHandlers struct {
	Prices ModelPricer
}

// RegisterModelRoutes mounts (relative to the group, mounted at
// /image-models behind the JWT middleware):
//
//	GET / — list image-capable public model names
func RegisterModelRoutes(group *gin.RouterGroup, h *ModelHandlers) {
	group.GET("", h.List)
}

func (h *ModelHandlers) List(c *gin.Context) {
	prices, err := h.Prices.List(c.Request.Context())
	if err != nil {
		log.Printf("canvastask: list prices: %v", err)
		apierr.Internal(c, "服务内部错误,请稍后再试")
		return
	}
	models := make([]string, 0, len(prices))
	for _, p := range prices {
		if p.Unit == pricing.UnitCall && p.Call != nil {
			models = append(models, p.PublicModel)
		}
	}
	sort.Strings(models)
	c.JSON(http.StatusOK, gin.H{"models": models})
}
