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
	"github.com/gachal/InfiniteChance/internal/asset"
	"github.com/gachal/InfiniteChance/internal/canvas"
	"github.com/gachal/InfiniteChance/internal/pricing"
)

// 输入上限与既有约定对齐:node_id 与画布名同宽(列都是 VARCHAR(128)),
// model 走 pricing 的公开模型名上限,size 走 call 轨的尺寸上限,prompt 的
// 8k 字符是宽上限 —— 真正的厂商限制在网关转发时自然暴露,任务行会留下
// 原因。image_url 对齐网关视频契约对参考图地址的上限(relay 的
// maxImageURLRunes),超长在落任务行之前就拒掉。视频秒数的界与网关一致
// (1..pricing.MaxCallItems)。
const (
	maxNodeIDRunes   = 128
	maxPromptRunes   = 8000
	listLimit        = 200
	maxImageRefRunes = 4096
)

// defaultVideoSeconds matches the gateway's own default clip length
// (Kling/万相/Veo 都以 5s 档起步)。
const defaultVideoSeconds = int64(5)

// CanvasGetter is the slice of the canvas store the handlers need: a task
// may only be created on an existing canvas.
type CanvasGetter interface {
	Get(ctx context.Context, id int64) (canvas.Canvas, error)
}

// AssetGetter is the slice of the asset store the handlers need: a video
// reference in the content-addressed form resolves through it to the
// vendor address the asset row holds (14 号票起节点普遍持有素材引用).
type AssetGetter interface {
	Get(ctx context.Context, id int64) (asset.Asset, error)
}

// ModelPricer is the slice of the pricing store the handlers need: the
// submit-time price check and the image/video model catalogs.
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
	Assets   AssetGetter
	Gateway  Gateway
}

// RegisterRoutes mounts (relative to the group, which the binary mounts at
// /canvases behind the JWT middleware, alongside the canvas CRUD routes):
//
//	POST /:id/tasks             — submit a generation {node_id, prompt, model, …}
//	GET  /:id/tasks             — the canvas's recent tasks (reconnect & reconcile)
//	GET  /:id/tasks/:tid        — one task (editor polling)
//	POST /:id/tasks/:tid/retry  — requeue a failed task (原地重试)
//	POST /:id/tasks/:tid/cancel — withdraw an active video task (12 号票)
func RegisterRoutes(group *gin.RouterGroup, h *Handlers) {
	group.POST("/:id/tasks", h.Create)
	group.GET("/:id/tasks", h.List)
	group.GET("/:id/tasks/:tid", h.Get)
	group.POST("/:id/tasks/:tid/retry", h.Retry)
	group.POST("/:id/tasks/:tid/cancel", h.Cancel)
}

// taskJSON is the wire form of a task. image_url / video_url let the editor
// render the result without a second round-trip to the asset; asset_id keeps
// the library reference (14 号票的跨画布复用按它走). seconds belongs to
// video tasks; the reference image stays server-side (它可能是超长的 data:
// URI,不该随每次轮询漂在任务列表里 —— 编辑器自己持有它,在图片节点上).
type taskJSON struct {
	ID        string    `json:"id"`
	CanvasID  int64     `json:"canvas_id"`
	NodeID    string    `json:"node_id"`
	Kind      string    `json:"kind"`
	Prompt    string    `json:"prompt"`
	Model     string    `json:"model"`
	Size      string    `json:"size"`
	Seconds   int64     `json:"seconds"`
	Status    Status    `json:"status"`
	Attempts  int64     `json:"attempts"`
	Error     string    `json:"error"`
	AssetID   int64     `json:"asset_id"`
	ImageURL  string    `json:"image_url"`
	VideoURL  string    `json:"video_url"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

func toTaskJSON(t Task) taskJSON {
	return taskJSON{
		ID: t.ID, CanvasID: t.CanvasID, NodeID: t.NodeID, Kind: t.Kind,
		Prompt: t.Prompt, Model: t.Model, Size: t.Size, Seconds: t.Seconds,
		Status: t.Status, Attempts: t.Attempts, Error: t.Error, AssetID: t.AssetID,
		ImageURL: t.ImageURL, VideoURL: t.VideoURL,
		CreatedAt: t.CreatedAt, UpdatedAt: t.UpdatedAt,
	}
}

type createInput struct {
	NodeID   string `json:"node_id"`
	Kind     string `json:"kind"`
	Prompt   string `json:"prompt"`
	Model    string `json:"model"`
	Size     string `json:"size"`
	Seconds  *int64 `json:"seconds"`
	ImageURL string `json:"image_url"`
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
	if kind != KindImage && kind != KindVideo {
		apierr.InvalidRequest(c, "kind 只支持 image(文生图)或 video(图生视频)")
		return
	}
	// 图生视频的专属入参:参考图片与时长。图片任务上两者无意义,不收。
	var seconds int64
	var imageRef string
	if kind == KindVideo {
		seconds = defaultVideoSeconds
		if in.Seconds != nil {
			if *in.Seconds < 1 || *in.Seconds > int64(pricing.MaxCallItems) {
				apierr.InvalidRequest(c, "seconds 需在 1 到 "+
					strconv.FormatInt(int64(pricing.MaxCallItems), 10)+" 之间")
				return
			}
			seconds = *in.Seconds
		}
		imageRef = strings.TrimSpace(in.ImageURL)
		// 参考图片的两种引用:厂商能拉取的 http(s) 地址原样透传;素材
		// 内容寻址路径(14 号票起图片节点普遍持有素材引用)由服务端解出
		// 素材行的厂商地址。内联 data: URI 在这里就拒掉,不落成注定失败
		// 的任务行。
		resolved, err := h.resolveImageRef(c.Request.Context(), imageRef)
		if err != nil {
			h.failImageRef(c, err)
			return
		}
		imageRef = resolved
		if utf8.RuneCountInString(imageRef) > maxImageRefRunes {
			apierr.InvalidRequest(c, "image_url 最多 4096 个字符")
			return
		}
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

	// 提交前先看价:模型没有按相应轨道计价时,生成注定失败 —— 让用户
	// 立刻知道,而不是排队后才在节点上看到失败。生图走按次轨(07 号票),
	// 视频走按秒轨(08 号票),互不串道。
	price, err := h.Models.ByModel(c.Request.Context(), model)
	if errors.Is(err, pricing.ErrNotFound) {
		apierr.Write(c, http.StatusBadRequest, "model_not_priced",
			"模型 "+model+" 未配置计价,请先在管理端配置价格")
		return
	}
	if err != nil {
		h.failStore(c, err)
		return
	}
	switch kind {
	case KindImage:
		if price.Unit != pricing.UnitCall || price.Call == nil {
			apierr.Write(c, http.StatusBadRequest, "model_not_priced",
				"模型 "+model+" 不是按次计价的生图模型")
			return
		}
	case KindVideo:
		if price.Unit != pricing.UnitSecond || price.Call == nil {
			apierr.Write(c, http.StatusBadRequest, "model_not_priced",
				"模型 "+model+" 不是按秒计价的视频模型")
			return
		}
	}

	id, err := NewID()
	if err != nil {
		h.failStore(c, err)
		return
	}
	task, err := h.Tasks.Create(c.Request.Context(), Task{
		ID: id, CanvasID: canvasID, NodeID: nodeID, Kind: kind,
		Prompt: prompt, Model: model, Size: size,
		Seconds: seconds, ImageRef: imageRef,
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

// Cancel withdraws one active video task (12 号票):the gateway task is
// canceled first — 尽力而为,网关对在途任务照常本地取消并全额退预扣 —
// then the row closes guarded on queued/running. A worker finishing in
// parallel simply loses the race and its outcome is discarded; a task that
// already reached a terminal state answers its stored row untouched
// (取消不改写历史,与网关的取消契约同形). Canceling before the submit was
// accepted (remote handle still empty) is a local close only — the worker
// notices the lost race when it tries to attach the handle and cancels the
// gateway task itself.
func (h *Handlers) Cancel(c *gin.Context) {
	canvasID, ok := bindID(c)
	if !ok {
		return
	}
	if !h.requireGateway(c) {
		return
	}
	task, ok := h.loadTask(c, canvasID)
	if !ok {
		return
	}
	if Terminal(task.Status) {
		c.JSON(http.StatusOK, gin.H{"task": toTaskJSON(task)})
		return
	}
	if task.Kind != KindVideo {
		// 同步生图没有可撤销的提交(10 号票定案):取消语义只属于视频。
		apierr.Conflict(c, "task_not_cancelable", "只有进行中的视频任务可以取消")
		return
	}
	if task.RemoteTaskID != "" {
		if err := h.Gateway.CancelVideo(c.Request.Context(), task.RemoteTaskID); err != nil {
			// 网关侧取消失败不拦本地取消:预扣最迟由 worker 的轮询超时
			// 兜底取消;这里只留日志。
			log.Printf("canvastask: cancel remote %s for %s: %v", task.RemoteTaskID, task.ID, err)
		}
	}
	fresh, err := h.Tasks.Cancel(c.Request.Context(), task.ID, canvasID)
	switch {
	case err == nil:
		c.JSON(http.StatusOK, gin.H{"task": toTaskJSON(fresh)})
	case errors.Is(err, ErrNotFound):
		apierr.NotFound(c, "任务不存在")
	case errors.Is(err, ErrNotCancelable):
		// 载入与更新之间被 worker 收尾抢先:终态既成,如实回放。
		final, ok := h.loadTask(c, canvasID)
		if !ok {
			return
		}
		c.JSON(http.StatusOK, gin.H{"task": toTaskJSON(final)})
	default:
		h.failStore(c, err)
	}
}

// 图生视频参考图解析的失败形状,与 promptgen 的视频引用解析同形:引用
// 形状不对(400)、素材不存在(404)、素材没有可用的厂商地址(400)、
// 内联 data: URI(400,data URI 进不了网关媒体契约 —— 12 号票同款决策)。
var (
	errImageRefEmpty     = errors.New("图生视频需要参考图片(image_url)")
	errImageRefMalformed = errors.New("image_url 必须是 http(s) 地址或 /api/assets/{id}/content 内容寻址路径")
	errImageRefInline    = errors.New("内联 base64 参考图不受支持,请使用带 http(s) 地址的图片素材")
	errImageAssetMissing = errors.New("素材不存在或已被删除")
	errImageAssetNoURL   = errors.New("该素材没有可用的 http(s) 原始地址,无法作为参考图")
)

// assetContentPrefix 是素材内容寻址路径的形状(10 号票定案):14 号票起
// 图片节点普遍持有这个形式的引用,编辑器原样上送,由服务端解出素材行的
// 厂商地址供网关与上游拉取。
const assetContentPrefix = "/api/assets/"

// resolveImageRef maps the editor's reference image to the address the
// vendor fetches: an http(s) URL passes through untouched; a content-
// addressed asset resolves through the store to the http(s) address it
// holds; inline data: URIs — carried directly or stored in the asset row —
// are refused before an unworkable task row lands.
func (h *Handlers) resolveImageRef(ctx context.Context, ref string) (string, error) {
	if ref == "" {
		return "", errImageRefEmpty
	}
	if strings.HasPrefix(ref, "http://") || strings.HasPrefix(ref, "https://") {
		return ref, nil
	}
	if strings.HasPrefix(ref, "data:") {
		return "", errImageRefInline
	}
	rest, ok := strings.CutPrefix(ref, assetContentPrefix)
	if !ok || !strings.HasSuffix(rest, "/content") {
		return "", errImageRefMalformed
	}
	rest = strings.TrimSuffix(rest, "/content")
	id, err := strconv.ParseInt(rest, 10, 64)
	if err != nil || id < 1 {
		return "", errImageRefMalformed
	}
	if h.Assets == nil {
		return "", errImageAssetMissing
	}
	a, err := h.Assets.Get(ctx, id)
	if errors.Is(err, asset.ErrNotFound) {
		return "", errImageAssetMissing
	}
	if err != nil {
		return "", err
	}
	if !strings.HasPrefix(a.URL, "http://") && !strings.HasPrefix(a.URL, "https://") {
		return "", errImageAssetNoURL
	}
	return a.URL, nil
}

// failImageRef maps resolveImageRef's sentinels onto their wire responses.
func (h *Handlers) failImageRef(c *gin.Context, err error) {
	switch {
	case errors.Is(err, errImageAssetMissing):
		apierr.NotFound(c, err.Error())
	case errors.Is(err, errImageRefEmpty),
		errors.Is(err, errImageRefMalformed),
		errors.Is(err, errImageRefInline),
		errors.Is(err, errImageAssetNoURL):
		apierr.InvalidRequest(c, err.Error())
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

// ModelHandlers serves the editor's model catalogs: the call-track models a
// text-to-image submit may use (10 号票) and the second-track models an
// image-to-video submit may use (12 号票).
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

// RegisterVideoModelRoutes mounts (relative to the group, mounted at
// /video-models behind the JWT middleware):
//
//	GET / — list video-capable public model names
func RegisterVideoModelRoutes(group *gin.RouterGroup, h *ModelHandlers) {
	group.GET("", h.ListVideos)
}

func (h *ModelHandlers) List(c *gin.Context) {
	h.listTrack(c, pricing.UnitCall)
}

func (h *ModelHandlers) ListVideos(c *gin.Context) {
	h.listTrack(c, pricing.UnitSecond)
}

// listTrack answers the public model names priced on one item track, sorted
// — the catalog is the pricing table's projection, nothing more (渠道侧的
// capabilities 在网关调度时兜底,这里不重复校验).
func (h *ModelHandlers) listTrack(c *gin.Context, unit pricing.Unit) {
	prices, err := h.Prices.List(c.Request.Context())
	if err != nil {
		log.Printf("canvastask: list prices: %v", err)
		apierr.Internal(c, "服务内部错误,请稍后再试")
		return
	}
	models := make([]string, 0, len(prices))
	for _, p := range prices {
		if p.Unit == unit && p.Call != nil {
			models = append(models, p.PublicModel)
		}
	}
	sort.Strings(models)
	c.JSON(http.StatusOK, gin.H{"models": models})
}
