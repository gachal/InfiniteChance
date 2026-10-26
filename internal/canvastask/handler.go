package canvastask

import (
	"context"
	"errors"
	"fmt"
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
	"github.com/gachal/InfiniteChance/internal/voice"
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
	// maxImageRefs 是一次图生图提交的参考图上限(21 号票定 4,43 号票放
	// 宽到 9):对齐 VOD adaptor 的厂商侧参考图配额(relay 的
	// vodMaxImageRefs = 9),与视频参考图同值;openai 型上游无数量自限
	// (gpt-image 官方口径更宽),超限 4xx 透出即可。字节面的真守卫在
	// worker(client.go 单张 8MiB、合计 24MiB)与网关 32MiB 请求体上限,
	// 与数量无关。前端同款常量拦在入口。
	maxImageRefs = 9
	// maxRatioRunes 对齐网关生图 ratio 的短串形状(22 号票):值不校验枚举,
	// 交上游裁 —— 档位串("16:9" 等)远小于此,只拦异常长串。
	maxRatioRunes = 32

	// 音色闭环的契约上限(57 号票,与 internal/relay/audio.go 同源):名称
	// 对齐台账列宽、音色描述与演绎指令对齐 56 号票 voice_prompt 上限、试听
	// 文本 15–200 字、台词 ≤600 字符。真正的厂商裁决在上游,超限 4xx 透出。
	maxVoiceNameRunes   = 255
	maxVoicePromptRunes = 500
	minPreviewTextRunes = 15
	maxPreviewTextRunes = 200
	maxSpeechTextRunes  = 600
	maxInstructionRunes = 500
)

// CanvasGetter is the slice of the canvas store the handlers need: a task
// may only be created on an existing canvas.
type CanvasGetter interface {
	Get(ctx context.Context, id int64) (canvas.Canvas, error)
}

// AssetGetter is the slice of the asset store the handlers need: a video
// reference in the content-addressed form resolves through it to the
// vendor address the asset row holds (14 号票起节点普遍持有素材引用);
// TouchLastUsed 是 47 号票「解引用即算」的写入点,同一切片内顺带暴露.
type AssetGetter interface {
	Get(ctx context.Context, id int64) (asset.Asset, error)
	TouchLastUsed(ctx context.Context, id int64) error
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
	// Voices 裁决台词合成的台账行存在性与可用性(57 号票);nil 时
	// kind=tts 提交以 500 暴露装配缺口(生产装配恒非 nil)。
	Voices VoiceLedger
	// PublicBaseURL answers the configured public base address of the
	// object storage for the reference-image resolution (18 号票解析顺序:
	// 自有公网地址优先、厂商原址回落)。19 号票落地 settings 前没有配置
	// 来源,留 nil = 未配置,解析行为与升级前一致。
	PublicBaseURL func(ctx context.Context) string
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
	ID         string    `json:"id"`
	CanvasID   int64     `json:"canvas_id"`
	NodeID     string    `json:"node_id"`
	Kind       string    `json:"kind"`
	Prompt     string    `json:"prompt"`
	Model      string    `json:"model"`
	Size       string    `json:"size"`
	Ratio      string    `json:"ratio"`
	Background string    `json:"background"`
	Seconds    int64     `json:"seconds"`
	Status     Status    `json:"status"`
	Attempts   int64     `json:"attempts"`
	Error      string    `json:"error"`
	AssetID    int64     `json:"asset_id"`
	ImageURL   string    `json:"image_url"`
	VideoURL   string    `json:"video_url"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

func toTaskJSON(t Task) taskJSON {
	return taskJSON{
		ID: t.ID, CanvasID: t.CanvasID, NodeID: t.NodeID, Kind: t.Kind,
		Prompt: t.Prompt, Model: t.Model, Size: t.Size, Ratio: t.Ratio,
		Background: t.Background, Seconds: t.Seconds,
		Status: t.Status, Attempts: t.Attempts, Error: t.Error, AssetID: t.AssetID,
		ImageURL: t.ImageURL, VideoURL: t.VideoURL,
		CreatedAt: t.CreatedAt, UpdatedAt: t.UpdatedAt,
	}
}

type createInput struct {
	NodeID     string       `json:"node_id"`
	Kind       string       `json:"kind"`
	Prompt     string       `json:"prompt"`
	Model      string       `json:"model"`
	Size       string       `json:"size"`
	Ratio      string       `json:"ratio"`
	Background string       `json:"background"`
	Seconds    *int64       `json:"seconds"`
	ImageURL   string       `json:"image_url"`
	ImageURLs  []string     `json:"image_urls"`
	VideoRefs  []videoRefIn `json:"video_refs"`
	Voice      *voiceIn     `json:"voice"`
	Tts        *ttsIn       `json:"tts"`
}

// voiceIn is the kind=voice 提交的专属入参 (57 号票):创建表单的内联事实
// (mode/name/target_model + design/clone 各自入参),落 voice_spec JSON
// 列;克隆样本按素材行 id 引用,handler 就地解出 http(s) 地址(24 号票
// 「提交时解析、行自足」先例)。
type voiceIn struct {
	Mode          string `json:"mode"`
	Name          string `json:"name"`
	TargetModel   string `json:"target_model"`
	VoicePrompt   string `json:"voice_prompt"`
	PreviewText   string `json:"preview_text"`
	SampleAssetID int64  `json:"sample_asset_id"`
}

// ttsIn is the kind=tts 提交的专属入参:voice_ledger_id 指向 voices 台账
// 行(text/instruction 是台词与可选演绎指令;台词本身落任务行 prompt 列)。
type ttsIn struct {
	VoiceLedgerID int64  `json:"voice_ledger_id"`
	Text          string `json:"text"`
	Instruction   string `json:"instruction"`
}

// videoRefIn is one wire-form video reference (24 号票):{url, kind, role}。
// url 接受厂商 http(s) 地址或素材内容寻址路径;kind/role 的合法组合与
// 数量上限见 videoRefRoleKind / maxVideoRefsByRole。
type videoRefIn struct {
	URL  string `json:"url"`
	Kind string `json:"kind"`
	Role string `json:"role"`
}

// videoRefRoleKind 是角色 → 媒体种类的正典映射:首帧/尾帧/参考图必为
// 图片,参考视频/参考音频各归其位。kind 声明与角色不符直接拒 —— 一条
// 「首帧视频」是客户端的 bug,不是上游该裁的语义。
var videoRefRoleKind = map[string]string{
	RoleFirstFrame:   asset.KindImage,
	RoleLastFrame:    asset.KindImage,
	RoleReferenceImg: asset.KindImage,
	RoleReferenceVid: asset.KindVideo,
	RoleReferenceAud: asset.KindAudio,
}

// maxVideoRefImages 是视频任务参考图上限(42 号票:与图生图 maxImageRefs
// 解耦,对齐 Seedance 2.0 官方口径 9 张;2.5 支持更多但网关 references
// 契约上限 9,不跟。43 号票图生图亦放宽到 9,两常量今日同值,仍各自独立
// 演进。上游真实上限仍是验证点,超限 4xx 透出即可)。
const maxVideoRefImages = 9

// maxVideoRefsTotal 是三类参考(reference_*)合计上限,与网关 /v1/videos 的
// references 数组上限同值(internal/relay maxVideoReferences,25 号票契约);
// 首帧/尾帧走 image/last_image 单串,不计入。
const maxVideoRefsTotal = 9

// maxVideoRefsByRole 是每个角色的数量上限(24 号票自限,前端同款纪律;
// 42 号票参考图放宽见 maxVideoRefImages):首帧/尾帧各 1,参考图 9,
// 参考视频/参考音频各 1。
var maxVideoRefsByRole = map[string]int{
	RoleFirstFrame:   1,
	RoleLastFrame:    1,
	RoleReferenceImg: maxVideoRefImages,
	RoleReferenceVid: 1,
	RoleReferenceAud: 1,
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
	switch kind {
	case KindVoice:
		h.createVoiceTask(c, canvasID, nodeID, in)
		return
	case KindTts:
		h.createTtsTask(c, canvasID, nodeID, in)
		return
	case KindImage, KindVideo:
	default:
		apierr.InvalidRequest(c, "kind 只支持 image(文生图)、video(图生视频)、voice(音色创建)或 tts(台词合成)")
		return
	}
	// 视频任务的专属入参(24 号票对话框范式):结构化参考 video_refs、
	// 显式比例 ratio、时长 seconds(缺省 = 自动,不传维持厂商缺省)。
	// 12 号票的单串 image_url 照常兼容(落 image_ref 列,worker 两种形
	// 态都认);video_refs 与 image_url 同时出现时以 video_refs 为准。
	// 无任何参考 = 文生视频,合法提交。
	var seconds int64
	var imageRef string
	var videoRefs []VideoRef
	var ratio string
	if kind == KindVideo {
		if in.Seconds != nil {
			if *in.Seconds < 1 || *in.Seconds > int64(pricing.MaxCallItems) {
				apierr.InvalidRequest(c, "seconds 需在 1 到 "+
					strconv.FormatInt(int64(pricing.MaxCallItems), 10)+" 之间")
				return
			}
			seconds = *in.Seconds
		}
		ratio = strings.TrimSpace(in.Ratio)
		if utf8.RuneCountInString(ratio) > maxRatioRunes {
			apierr.InvalidRequest(c, "ratio 最多 32 个字符")
			return
		}
		if len(in.VideoRefs) > 0 {
			if strings.TrimSpace(in.ImageURL) != "" {
				apierr.InvalidRequest(c, "video_refs 与 image_url 不能同时提供(image_url 是 12 号票的单串旧形态)")
				return
			}
			counts := make(map[string]int, len(maxVideoRefsByRole))
			referenceCount := 0
			for _, raw := range in.VideoRefs {
				resolved, err := h.resolveVideoRef(c.Request.Context(), raw)
				if err != nil {
					h.failVideoRef(c, err)
					return
				}
				// 角色数量上限在前端同款纪律拒收(超限整条不收);素材
				// 去重不在此做 —— 同一素材扮两种角色对上游也有语义。
				if counts[resolved.Role]+1 > maxVideoRefsByRole[resolved.Role] {
					apierr.InvalidRequest(c, errVideoRefTooMany.Error())
					return
				}
				// 三类参考合计上限(42 号票):与网关 references 契约同值,
				// 首帧/尾帧走单串不占额度。
				if resolved.Role != RoleFirstFrame && resolved.Role != RoleLastFrame {
					referenceCount++
					if referenceCount > maxVideoRefsTotal {
						apierr.InvalidRequest(c, errVideoRefTotalTooMany.Error())
						return
					}
				}
				counts[resolved.Role]++
				videoRefs = append(videoRefs, resolved)
			}
		} else {
			// 单串参考图(12 号票旧形态):参考图片的两种引用 —— 厂商能拉
			// 取的 http(s) 地址原样透传;素材内容寻址路径(14 号票起图片节
			// 点普遍持有素材引用)由服务端解出素材行的厂商地址。内联 data:
			// URI 在这里就拒掉,不落成注定失败的任务行。
			imageRef = strings.TrimSpace(in.ImageURL)
			if imageRef != "" {
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
		}
	}
	// 图生图的专属入参(21 号票):参考图片列表。逐条走与视频同一个解引
	// 用(顺序保留),超限在落任务行之前就拒;video 任务上不读,与上面的
	// seconds/image_url 对图片任务的态度对称。
	var imageRefs []string
	if kind == KindImage && len(in.ImageURLs) > 0 {
		if len(in.ImageURLs) > maxImageRefs {
			apierr.InvalidRequest(c, "image_urls 最多 "+
				strconv.Itoa(maxImageRefs)+" 条")
			return
		}
		for _, raw := range in.ImageURLs {
			resolved, err := h.resolveImageRef(c.Request.Context(), strings.TrimSpace(raw))
			if err != nil {
				h.failImageRef(c, err)
				return
			}
			if utf8.RuneCountInString(resolved) > maxImageRefRunes {
				apierr.InvalidRequest(c, "image_urls 每条最多 4096 个字符")
				return
			}
			imageRefs = append(imageRefs, resolved)
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
	// 透明背景(37 号票):仅图片任务、仅 transparent 一个枚举值 —— 其他
	// 值在落任务行之前 400,与网关 /v1 的校验同形(严格全等,不 trim,
	// 行上的值就是上送的值);视频透明显式 out of scope,视频任务不读
	// 该字段。
	background := ""
	if kind == KindImage && in.Background != "" {
		if in.Background != "transparent" {
			apierr.InvalidRequest(c, "background 仅支持 transparent")
			return
		}
		background = in.Background
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
		// 视频按模型所配轨道结算(25 号票):second 或 token 皆收,其余
		// 轨道照旧拒绝 —— 与网关 /v1/videos 的计价校验同款放宽。
		if (price.Unit != pricing.UnitSecond || price.Call == nil) &&
			(price.Unit != pricing.UnitToken || price.Token == nil) {
			apierr.Write(c, http.StatusBadRequest, "model_not_priced",
				"模型 "+model+" 不是按秒或按 token 计价的视频模型")
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
		Prompt: prompt, Model: model, Size: size, Ratio: ratio, Background: background,
		Seconds: seconds, ImageRef: imageRef, VideoRefs: videoRefs, ImageRefs: imageRefs,
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

// 图生视频/图生图参考图解析的失败形状,与 promptgen 的视频引用解析同形:
// 引用形状不对(400)、素材不存在(404)、素材没有可用的厂商地址(400)、
// 内联 data: URI(400,data URI 进不了网关媒体契约 —— 12 号票同款决策)。
// 文案对两种任务通用(21 号票起列表条目走同一条解析)。
var (
	errImageRefEmpty     = errors.New("缺少参考图片地址(image_url)")
	errImageRefTooLong   = errors.New("参考地址最多 4096 个字符")
	errImageRefMalformed = errors.New("参考图必须是 http(s) 地址或 /api/assets/{id}/content 内容寻址路径")
	errImageRefInline    = errors.New("内联 base64 参考图不受支持,请使用带 http(s) 地址的图片素材")
	errImageAssetMissing = errors.New("素材不存在或已被删除")
	errImageAssetNoURL   = errors.New("该素材没有可用的 http(s) 原始地址,无法作为参考图")

	// 视频参考(24 号票)的专属失败形状:角色/种类不符、单角色超限是客
	// 户端的结构错误;地址解析失败复用 image 侧同款哨兵。
	errVideoRefEmpty        = errors.New("video_refs 条目缺少 url")
	errVideoRefBadRole      = errors.New("video_refs 的 role 必须是 first_frame/last_frame/reference_image/reference_video/reference_audio 之一,且 kind 与角色相符")
	errVideoRefTooMany      = errors.New("video_refs 角色数量超限(首帧/尾帧/参考视频/参考音频各 1,参考图最多 9)")
	errVideoRefTotalTooMany = errors.New("video_refs 参考条目合计超限(参考图/参考视频/参考音频合计最多 9)")
	errVideoAssetKind       = errors.New("素材种类与参考角色不符(首帧/尾帧/参考图须为图片素材,参考视频须为视频素材,参考音频须为音频素材)")
)

// assetContentPrefix 是素材内容寻址路径的形状(10 号票定案):14 号票起
// 图片节点普遍持有这个形式的引用,编辑器原样上送,由服务端解出素材行的
// 厂商地址供网关与上游拉取。
const assetContentPrefix = "/api/assets/"

// resolveMediaURL maps one media reference to the address the vendor fetches
// —— 图生图参考图与视频参考(24 号票)共用的解引用:http(s) 原样透传
// (素材行 kind 答 "",id 答 0,无从查证);素材内容寻址路径经素材表按
// 18 号票顺序解出(自有存储公网地址优先、厂商原址约 24h 回落),并顺带
// 回答素材行的 kind 与 id 供角色校验与 47 号票的 touch;内联 data: URI
// (直带或落过素材行的)都拒 —— 12 号票「data URI 进不了网关媒体契约」。
// 空串由调用方各自拒(两种契约的空值语义不同),长度上限同样由调用方在
// 解出的地址上统一把关。touch 由调用方在各自校验全部通过后执行
// (47 号票「校验失败不 touch」);任务行存的是已解析地址,重试复用行内
// 地址不再二次解引用,不重复计。
func (h *Handlers) resolveMediaURL(ctx context.Context, ref string) (addr, assetKind string, assetID int64, err error) {
	if strings.HasPrefix(ref, "http://") || strings.HasPrefix(ref, "https://") {
		return ref, "", 0, nil
	}
	if strings.HasPrefix(ref, "data:") {
		return "", "", 0, errImageRefInline
	}
	rest, ok := strings.CutPrefix(ref, assetContentPrefix)
	if !ok || !strings.HasSuffix(rest, "/content") {
		return "", "", 0, errImageRefMalformed
	}
	rest = strings.TrimSuffix(rest, "/content")
	id, err := strconv.ParseInt(rest, 10, 64)
	if err != nil || id < 1 {
		return "", "", 0, errImageRefMalformed
	}
	if h.Assets == nil {
		return "", "", 0, errImageAssetMissing
	}
	a, err := h.Assets.Get(ctx, id)
	if errors.Is(err, asset.ErrNotFound) {
		return "", "", 0, errImageAssetMissing
	}
	if err != nil {
		return "", "", 0, err
	}
	if addr, ok := asset.PublicAddress(a, h.publicBaseURL(ctx)); ok {
		return addr, a.Kind, a.ID, nil
	}
	if !strings.HasPrefix(a.URL, "http://") && !strings.HasPrefix(a.URL, "https://") {
		return "", "", 0, errImageAssetNoURL
	}
	return a.URL, a.Kind, a.ID, nil
}

// touchAsset 记录一次「素材被服务端解引用」(47 号票):排序信号而非一致性
// 事实,尽力而为 —— 失败只留日志,不拦生成;行已消失(并发删除)不算错。
func (h *Handlers) touchAsset(ctx context.Context, id int64) {
	if err := h.Assets.TouchLastUsed(ctx, id); err != nil && !errors.Is(err, asset.ErrNotFound) {
		log.Printf("canvastask: touch asset %d last_used: %v", id, err)
	}
}

// resolveImageRef maps the editor's reference image to the address the
// vendor fetches(图生视频单串/图生图列表共用):解引用与 kind 回答见
// resolveMediaURL,这里只多拒空串;解出素材行即 touch(47 号票)—— 调用
// 方随后的地址长度校验是请求形状检查,不是素材语义校验,不回滚 touch。
func (h *Handlers) resolveImageRef(ctx context.Context, ref string) (string, error) {
	if ref == "" {
		return "", errImageRefEmpty
	}
	addr, _, assetID, err := h.resolveMediaURL(ctx, ref)
	if err != nil {
		return "", err
	}
	if assetID > 0 {
		h.touchAsset(ctx, assetID)
	}
	return addr, nil
}

// publicBaseURL reads the configured public base through the optional
// provider; nil or empty means "not configured".
func (h *Handlers) publicBaseURL(ctx context.Context) string {
	if h.PublicBaseURL == nil {
		return ""
	}
	return h.PublicBaseURL(ctx)
}

// resolveVideoRef maps one wire-form video reference (24 号票) onto the
// resolved row form: the role must be known and its declared kind must
// match; the address dereferences through resolveMediaURL, and a content-
// addressed asset must carry the kind the role demands —— 首帧给了视频素
// 材这类结构错误在落任务行之前就拒掉(http(s) 地址无从查证,声明 kind
// 已与角色对齐)。角色数量上限由调用方在组装列表时按 maxVideoRefsByRole
// 计数把关(这里只看单条形状)。全部校验通过才 touch(47 号票「校验失败
// 不 touch」—— kind 不符或地址超长的拒收不刷新最近使用)。
func (h *Handlers) resolveVideoRef(ctx context.Context, in videoRefIn) (VideoRef, error) {
	url := strings.TrimSpace(in.URL)
	role := strings.TrimSpace(in.Role)
	kind := strings.TrimSpace(in.Kind)
	if url == "" {
		return VideoRef{}, errVideoRefEmpty
	}
	wantKind, ok := videoRefRoleKind[role]
	if !ok || kind != wantKind {
		return VideoRef{}, errVideoRefBadRole
	}
	addr, assetKind, assetID, err := h.resolveMediaURL(ctx, url)
	if err != nil {
		return VideoRef{}, err
	}
	if utf8.RuneCountInString(addr) > maxImageRefRunes {
		return VideoRef{}, errImageRefTooLong
	}
	if assetKind != "" && assetKind != wantKind {
		return VideoRef{}, errVideoAssetKind
	}
	if assetID > 0 {
		h.touchAsset(ctx, assetID)
	}
	return VideoRef{URL: addr, Kind: kind, Role: role}, nil
}

// failVideoRef maps resolveVideoRef's sentinels onto their wire responses;
// the shared address sentinels answer the same shapes as failImageRef.
func (h *Handlers) failVideoRef(c *gin.Context, err error) {
	switch {
	case errors.Is(err, errVideoRefEmpty),
		errors.Is(err, errVideoRefBadRole),
		errors.Is(err, errVideoAssetKind):
		apierr.InvalidRequest(c, err.Error())
	default:
		h.failImageRef(c, err)
	}
}

// failImageRef maps resolveImageRef's sentinels onto their wire responses.
func (h *Handlers) failImageRef(c *gin.Context, err error) {
	switch {
	case errors.Is(err, errImageAssetMissing):
		apierr.NotFound(c, err.Error())
	case errors.Is(err, errImageRefEmpty),
		errors.Is(err, errImageRefTooLong),
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
	// second 轨全部 + 带折算表的 token 轨(25 号票视频 token 价;聊天
	// token 价整表为空,TokenPrice.HasVideoRates 判别不歧义)。
	prices, err := h.Prices.List(c.Request.Context())
	if err != nil {
		log.Printf("canvastask: list prices: %v", err)
		apierr.Internal(c, "服务内部错误,请稍后再试")
		return
	}
	models := make([]string, 0, len(prices))
	summaries := make(map[string]pricing.PriceSummary, len(prices))
	for _, p := range prices {
		switch {
		case p.Unit == pricing.UnitSecond && p.Call != nil:
		case p.Unit == pricing.UnitToken && p.Token != nil && p.Token.HasVideoRates():
		default:
			continue
		}
		models = append(models, p.PublicModel)
		summaries[p.PublicModel] = p.Summary() // 38 号票:目录随行带价,前端做预估
	}
	sort.Strings(models)
	c.JSON(http.StatusOK, gin.H{"models": models, "prices": summaries})
}

// listTrack answers the public model names priced on one item track, sorted
// — the catalog is the pricing table's projection, nothing more (渠道侧的
// capabilities 在网关调度时兜底,这里不重复校验). prices 映射随名单带
// 人类人民币单价与系数(38 号票),models 字段形状不变、旧消费者不受扰。
func (h *ModelHandlers) listTrack(c *gin.Context, unit pricing.Unit) {
	prices, err := h.Prices.List(c.Request.Context())
	if err != nil {
		log.Printf("canvastask: list prices: %v", err)
		apierr.Internal(c, "服务内部错误,请稍后再试")
		return
	}
	models := make([]string, 0, len(prices))
	summaries := make(map[string]pricing.PriceSummary, len(prices))
	for _, p := range prices {
		if p.Unit == unit && p.Call != nil {
			models = append(models, p.PublicModel)
			summaries[p.PublicModel] = p.Summary()
		}
	}
	sort.Strings(models)
	c.JSON(http.StatusOK, gin.H{"models": models, "prices": summaries})
}

// createVoiceTask lands one 音色创建 task (57 号票):表单事实就地校验
// (失败在落行前,不让注定失败的任务进队列)、克隆样本就地解出 http(s)
// 地址、定制次轨配价预检 —— 之后 worker 接手(选渠道 → create → 轮询
// → 台账落行)。
func (h *Handlers) createVoiceTask(c *gin.Context, canvasID int64, nodeID string, in createInput) {
	spec := in.Voice
	if spec == nil {
		apierr.InvalidRequest(c, "kind=voice 需要提供 voice 表单 {mode, name, target_model, preview_text}")
		return
	}
	mode := strings.TrimSpace(spec.Mode)
	if mode != voice.SourceModeDesign && mode != voice.SourceModeClone {
		apierr.InvalidRequest(c, "mode 必须是 design(描述设计)或 clone(样本克隆)")
		return
	}
	name := strings.TrimSpace(spec.Name)
	if name == "" {
		apierr.InvalidRequest(c, "name 不能为空")
		return
	}
	if utf8.RuneCountInString(name) > maxVoiceNameRunes {
		apierr.InvalidRequest(c, "name 最多 255 个字符")
		return
	}
	targetModel := strings.TrimSpace(spec.TargetModel)
	if targetModel == "" {
		apierr.InvalidRequest(c, "target_model 不能为空")
		return
	}
	if utf8.RuneCountInString(targetModel) > pricing.ModelNameRunes {
		apierr.InvalidRequest(c, "target_model 最多 200 个字符")
		return
	}
	voicePrompt := strings.TrimSpace(spec.VoicePrompt)
	previewText := strings.TrimSpace(spec.PreviewText)
	if mode == voice.SourceModeDesign {
		if voicePrompt == "" {
			apierr.InvalidRequest(c, "design 模式需要提供 voice_prompt(音色描述)")
			return
		}
		if utf8.RuneCountInString(voicePrompt) > maxVoicePromptRunes {
			apierr.InvalidRequest(c, "voice_prompt 最多 500 个字符")
			return
		}
	}
	if n := utf8.RuneCountInString(previewText); n < minPreviewTextRunes || n > maxPreviewTextRunes {
		apierr.InvalidRequest(c, fmt.Sprintf("preview_text 需在 %d 到 %d 个字符之间", minPreviewTextRunes, maxPreviewTextRunes))
		return
	}

	out := VoiceSpec{
		Mode:        mode,
		Name:        name,
		TargetModel: targetModel,
		VoicePrompt: voicePrompt,
		PreviewText: previewText,
	}
	if mode == voice.SourceModeClone {
		addr, err := h.resolveAudioSample(c.Request.Context(), spec.SampleAssetID)
		if err != nil {
			h.failAudioSample(c, err)
			return
		}
		out.SampleAssetID = spec.SampleAssetID
		out.AudioURL = addr
	}

	// 提交前先看价:音色定制走 voice-enrollment 的按次轨(56 号票),
	// 未配价或轨道不符的提交注定失败,当场拒绝。
	price, err := h.Models.ByModel(c.Request.Context(), enrollmentModel)
	if errors.Is(err, pricing.ErrNotFound) {
		apierr.Write(c, http.StatusBadRequest, "model_not_priced",
			"音色定制模型 voice-enrollment 未配置计价,请先在管理端配置价格")
		return
	}
	if err != nil {
		h.failStore(c, err)
		return
	}
	if price.Unit != pricing.UnitCall || price.Call == nil {
		apierr.Write(c, http.StatusBadRequest, "model_not_priced",
			"模型 voice-enrollment 不是按次计价的音色定制模型")
		return
	}

	h.submitTask(c, canvasID, nodeID, Task{
		Kind: KindVoice, Prompt: previewText, Model: targetModel, VoiceSpec: out,
	})
}

// createTtsTask lands one 台词合成 task (57 号票):运行时读台账行取钉定
// 渠道与上游句柄,这里只校验台账行当前可用、文本长度与字符轨配价 ——
// 台词落任务行 prompt 列(原文计费口径与网关一致,不 trim)。
func (h *Handlers) createTtsTask(c *gin.Context, canvasID int64, nodeID string, in createInput) {
	spec := in.Tts
	if spec == nil || spec.VoiceLedgerID < 1 {
		apierr.InvalidRequest(c, "kind=tts 需要提供 tts 表单 {voice_ledger_id, text}")
		return
	}
	if h.Voices == nil {
		log.Printf("canvastask: tts submit without a voices store (装配缺口)")
		apierr.Internal(c, "服务内部错误,请稍后再试")
		return
	}
	ledger, err := h.Voices.Get(c.Request.Context(), spec.VoiceLedgerID)
	if errors.Is(err, voice.ErrVoiceNotFound) {
		apierr.InvalidRequest(c, "音色不存在或已被删除")
		return
	}
	if err != nil {
		h.failStore(c, err)
		return
	}
	if ledger.Status != voice.StatusOK {
		apierr.InvalidRequest(c, "音色「"+ledger.Name+"」当前状态 "+ledger.Status+",尚不可合成台词")
		return
	}
	if n := utf8.RuneCountInString(spec.Text); n < 1 {
		apierr.InvalidRequest(c, "text 不能为空")
		return
	} else if n > maxSpeechTextRunes {
		apierr.InvalidRequest(c, fmt.Sprintf("text 最多 %d 个字符", maxSpeechTextRunes))
		return
	}
	instruction := strings.TrimSpace(spec.Instruction)
	if utf8.RuneCountInString(instruction) > maxInstructionRunes {
		apierr.InvalidRequest(c, fmt.Sprintf("instruction 最多 %d 个字符", maxInstructionRunes))
		return
	}

	price, err := h.Models.ByModel(c.Request.Context(), ledger.TargetModel)
	if errors.Is(err, pricing.ErrNotFound) {
		apierr.Write(c, http.StatusBadRequest, "model_not_priced",
			"模型 "+ledger.TargetModel+" 未配置计价,请先在管理端配置价格")
		return
	}
	if err != nil {
		h.failStore(c, err)
		return
	}
	if price.Unit != pricing.UnitCharacter || price.Character == nil {
		apierr.Write(c, http.StatusBadRequest, "model_not_priced",
			"模型 "+ledger.TargetModel+" 不是按字符计价的台词合成模型")
		return
	}

	h.submitTask(c, canvasID, nodeID, Task{
		Kind: KindTts, Prompt: spec.Text, Model: ledger.TargetModel,
		VoiceSpec: VoiceSpec{LedgerID: spec.VoiceLedgerID, Instruction: instruction},
	})
}

// submitTask mints the task id, persists the row queued and answers 201 —
// the shared tail of all four kinds (worker owns everything after this).
func (h *Handlers) submitTask(c *gin.Context, canvasID int64, nodeID string, t Task) {
	id, err := NewID()
	if err != nil {
		h.failStore(c, err)
		return
	}
	t.ID = id
	t.CanvasID = canvasID
	t.NodeID = nodeID
	task, err := h.Tasks.Create(c.Request.Context(), t)
	if err != nil {
		h.failStore(c, err)
		return
	}
	c.JSON(http.StatusCreated, gin.H{"task": toTaskJSON(task)})
}

// resolveAudioSample maps the clone form's sample asset id onto the address
// the vendor fetches (57 号票):素材行 kind 必须是 audio,解析顺序与参考图
// 同一条(18 号票:自有存储公网地址优先、厂商原址约 24h 回落),data:
// URI 与无地址行拒绝 —— 与「data URI 进不了网关媒体契约」同款纪律。解出
// 即 touch(47 号票「校验通过才算」)。
func (h *Handlers) resolveAudioSample(ctx context.Context, assetID int64) (string, error) {
	if assetID < 1 {
		return "", errAudioSampleMissing
	}
	if h.Assets == nil {
		return "", errAudioSampleAssetMissing
	}
	a, err := h.Assets.Get(ctx, assetID)
	if errors.Is(err, asset.ErrNotFound) {
		return "", errAudioSampleAssetMissing
	}
	if err != nil {
		return "", err
	}
	if a.Kind != asset.KindAudio {
		return "", errAudioSampleKind
	}
	if addr, ok := asset.PublicAddress(a, h.publicBaseURL(ctx)); ok {
		h.touchAsset(ctx, assetID)
		return addr, nil
	}
	if strings.HasPrefix(a.URL, "http://") || strings.HasPrefix(a.URL, "https://") {
		if utf8.RuneCountInString(a.URL) > maxImageRefRunes {
			return "", errAudioSampleTooLong
		}
		h.touchAsset(ctx, assetID)
		return a.URL, nil
	}
	return "", errAudioSampleNoURL
}

// 克隆样本解析的失败形状(与参考图同族):400 形状错/类型不符/无地址,
// 404 素材不存在。
var (
	errAudioSampleEmpty        = errors.New("clone 模式需要提供声音样本素材(sample_asset_id)")
	errAudioSampleAssetMissing = errors.New("声音样本素材不存在或已被删除")
	errAudioSampleKind         = errors.New("声音样本必须是音频素材(kind=audio)")
	errAudioSampleNoURL        = errors.New("该素材没有可用的 http(s) 地址,无法作为声音样本")
	errAudioSampleTooLong      = errors.New("样本地址最多 4096 个字符")
	errAudioSampleMissing      = errAudioSampleEmpty
)

// failAudioSample maps resolveAudioSample's sentinels onto their wire
// responses.
func (h *Handlers) failAudioSample(c *gin.Context, err error) {
	switch {
	case errors.Is(err, errAudioSampleAssetMissing):
		apierr.NotFound(c, err.Error())
	case errors.Is(err, errAudioSampleEmpty),
		errors.Is(err, errAudioSampleKind),
		errors.Is(err, errAudioSampleNoURL),
		errors.Is(err, errAudioSampleTooLong):
		apierr.InvalidRequest(c, err.Error())
	default:
		h.failStore(c, err)
	}
}

// RegisterAudioModelRoutes mounts (relative to the group, mounted at
// /audio-models behind the JWT middleware):
//
//	GET / — 字符轨配价且 audio 能力渠道可服务的合成模型(带价,57 号票)
func RegisterAudioModelRoutes(group *gin.RouterGroup, h *Handlers) {
	group.GET("", h.ListAudioModels)
}

// ListAudioModels proxies the gateway's /v1/audio/models with the service
// key —— 「过滤 audio 能力渠道」的维度在渠道表,归网关管;canvas 只做
// 带服务级 key 的代理。网关未配置时 503 gateway_unconfigured(提交同款)。
func (h *Handlers) ListAudioModels(c *gin.Context) {
	if !h.requireGateway(c) {
		return
	}
	catalog, err := h.Gateway.ListAudioModels(c.Request.Context())
	if err != nil {
		log.Printf("canvastask: list audio models: %v", err)
		apierr.Write(c, http.StatusBadGateway, "upstream_error",
			"获取音频模型目录失败:"+err.Error())
		return
	}
	c.JSON(http.StatusOK, gin.H{"models": catalog.Models, "prices": catalog.Prices})
}
