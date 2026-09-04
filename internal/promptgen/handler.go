package promptgen

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/gin-gonic/gin"

	"github.com/gachal/InfiniteChance/internal/apierr"
	"github.com/gachal/InfiniteChance/internal/asset"
	"github.com/gachal/InfiniteChance/internal/canvas"
	"github.com/gachal/InfiniteChance/internal/pricing"
	"github.com/gachal/InfiniteChance/internal/prompttemplate"
)

// 输入上限与既有约定对齐:node_id 与节点 id 列约定同宽(VARCHAR(128)),
// model 走 pricing 的公开模型名上限,topic 是用户手输的一段话,
// video_url 对齐 canvastask 参考图地址的上限(厂商可拉取的 http(s) 地址
// 或素材内容寻址路径)。
const (
	maxNodeIDRunes   = 128
	maxTopicRunes    = 4000
	maxVideoRefRunes = 4096
)

// TemplateSource is the slice of the template store the handlers need: the
// action fetches the chosen template per request, so admin edits take effect
// immediately (no cache — 11 号票的「即时反映」).
type TemplateSource interface {
	Get(ctx context.Context, id int64) (prompttemplate.Template, error)
	ListEnabled(ctx context.Context) ([]prompttemplate.Template, error)
}

// CanvasGetter is the slice of the canvas store the handlers need: a
// generation belongs to an existing canvas.
type CanvasGetter interface {
	Get(ctx context.Context, id int64) (canvas.Canvas, error)
}

// AssetGetter is the slice of the asset store the handlers need: a video
// reference in the content-addressed form resolves through it to the
// address the asset row holds.
type AssetGetter interface {
	Get(ctx context.Context, id int64) (asset.Asset, error)
}

// ModelPricer is the slice of the pricing store the handlers need: the
// submit-time price check and the chat-model catalog.
type ModelPricer interface {
	List(ctx context.Context) ([]pricing.Price, error)
	ByModel(ctx context.Context, publicModel string) (pricing.Price, error)
}

// Gateway is the slice of the gateway client the handlers need; tests
// substitute fakes.
type Gateway interface {
	GenerateChat(ctx context.Context, req ChatRequest) (ChatResult, error)
}

// Handlers serves the creator prompt-generation endpoints. canvas/server
// mounts them on the authed /canvases group. A nil Gateway means
// canvas/server runs without a service key: generations are refused up
// front instead of failing after the user waits.
type Handlers struct {
	Templates TemplateSource
	Canvases  CanvasGetter
	Assets    AssetGetter
	Models    ModelPricer
	Gateway   Gateway
}

// RegisterRoutes mounts (relative to the group, which the binary mounts at
// /canvases behind the JWT middleware, alongside the canvas CRUD routes):
//
//	POST /:id/generate-prompt — {node_id?, template_id, topic, model} → text
//	POST /:id/reverse-prompt  — {node_id?, video_url, model} → text
func RegisterRoutes(group *gin.RouterGroup, h *Handlers) {
	group.POST("/:id/generate-prompt", h.Generate)
	group.POST("/:id/reverse-prompt", h.Reverse)
}

type generateInput struct {
	NodeID     string `json:"node_id"`
	TemplateID int64  `json:"template_id"`
	Topic      string `json:"topic"`
	Model      string `json:"model"`
}

// Generate renders the chosen template with the topic and relays it through
// the gateway chat surface. The text comes back to the editor synchronously:
// prompt generation is an ordinary chat call, not a task — nothing queues,
// nothing polls, and the node write happens client-side via autosave.
func (h *Handlers) Generate(c *gin.Context) {
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

	var in generateInput
	if err := c.ShouldBindJSON(&in); err != nil {
		apierr.InvalidRequest(c, "请求体必须是 {template_id, topic, model} JSON")
		return
	}
	nodeID := strings.TrimSpace(in.NodeID)
	if utf8.RuneCountInString(nodeID) > maxNodeIDRunes {
		apierr.InvalidRequest(c, "node_id 最多 128 个字符")
		return
	}
	if in.TemplateID < 1 {
		apierr.InvalidRequest(c, "template_id 必须是正整数")
		return
	}
	topic := strings.TrimSpace(in.Topic)
	if topic == "" {
		apierr.InvalidRequest(c, "topic 不能为空")
		return
	}
	if utf8.RuneCountInString(topic) > maxTopicRunes {
		apierr.InvalidRequest(c, "topic 最多 4000 个字符")
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

	tpl, err := h.Templates.Get(c.Request.Context(), in.TemplateID)
	if errors.Is(err, prompttemplate.ErrNotFound) {
		apierr.NotFound(c, "提示词模板不存在或已被删除")
		return
	}
	if err != nil {
		h.failStore(c, err)
		return
	}
	if !tpl.Enabled {
		apierr.Write(c, http.StatusBadRequest, "template_disabled", "提示词模板已停用")
		return
	}

	// 发起前先看价:模型没有按 token 计价时,聊天注定被网关拒绝 ——
	// 让用户立刻知道,而不是干等一次注定失败的上游调用。
	if !h.chatModelPriced(c, model) {
		return
	}

	result, err := h.Gateway.GenerateChat(c.Request.Context(), ChatRequest{
		Model:   model,
		Content: tpl.Render(topic),
		Source:  canvasSource(canvasID, nodeID, "prompt"),
	})
	if err != nil {
		// 上游失败原样透出:额度不足、模型不可用等都是用户可行动的信息,
		// 网关已按聊天轨完成记账/退款,这里只负责把原因带到编辑器。
		log.Printf("promptgen: %s %s: gateway: %v", c.Request.Method, c.Request.URL.Path, err)
		apierr.Write(c, http.StatusBadGateway, "upstream_error", err.Error())
		return
	}
	c.JSON(http.StatusOK, gin.H{"text": result.Content})
}

// chatModelPriced guards both chat-driven actions: a model without a token
// price is refused before the call (未配置价格的模型一律拒绝,不做静默兜底).
func (h *Handlers) chatModelPriced(c *gin.Context, model string) bool {
	price, err := h.Models.ByModel(c.Request.Context(), model)
	if errors.Is(err, pricing.ErrNotFound) {
		apierr.Write(c, http.StatusBadRequest, "model_not_priced",
			"模型 "+model+" 未配置 token 计价,请先在管理端配置价格")
		return false
	}
	if err != nil {
		h.failStore(c, err)
		return false
	}
	if price.Unit != pricing.UnitToken || price.Token == nil {
		apierr.Write(c, http.StatusBadRequest, "model_not_priced",
			"模型 "+model+" 不是 token 计价的聊天模型")
		return false
	}
	return true
}

// reverseInput is one video-to-prompt request. video_url 是视频节点持有的
// 地址:厂商 http(s) 地址原样透传,或素材内容寻址路径(厂商回 b64 时节点
// 落的地址)由服务端解出素材行的地址。
type reverseInput struct {
	NodeID   string `json:"node_id"`
	VideoURL string `json:"video_url"`
	Model    string `json:"model"`
}

// reverseInstruction is the fixed analysis brief for video-to-prompt (13 号
// 票). 与 generate-prompt 不同,这里没有模板依赖 —— 反推的诉求恒定:把
// 画面与运动写成一段可复用的提示词。输出语言跟随指令(中文)。
const reverseInstruction = "请分析这段视频,反推出一段可直接用于生成同样效果的提示词,供文生图或图生视频模型使用。" +
	"提示词需要覆盖画面与运动:画面包括主体与场景、构图与镜头、光影与色调、风格质感;" +
	"运动包括主体的动作与变化、镜头的推拉摇移与节奏。" +
	"只输出提示词本身,不要任何解释、前缀或分点,用一段连贯的文字完成。"

// Reverse analyses an existing video and answers a prompt that describes it:
// the video rides to a vision-capable chat model as a video_url content part
// through the gateway's chat surface — 同步聊天调用而非画布任务,用量按
// token 计费入网关用量日志(来源标记区分反推)。文本回到编辑器,由它落为
// 新的提示词节点衔接后续生图/生视频动作。
func (h *Handlers) Reverse(c *gin.Context) {
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

	var in reverseInput
	if err := c.ShouldBindJSON(&in); err != nil {
		apierr.InvalidRequest(c, "请求体必须是 {video_url, model} JSON")
		return
	}
	nodeID := strings.TrimSpace(in.NodeID)
	if utf8.RuneCountInString(nodeID) > maxNodeIDRunes {
		apierr.InvalidRequest(c, "node_id 最多 128 个字符")
		return
	}
	videoRef := strings.TrimSpace(in.VideoURL)
	if videoRef == "" {
		apierr.InvalidRequest(c, "video_url 不能为空")
		return
	}
	if utf8.RuneCountInString(videoRef) > maxVideoRefRunes {
		apierr.InvalidRequest(c, "video_url 最多 4096 个字符")
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
	if !h.chatModelPriced(c, model) {
		return
	}

	videoURL, err := h.resolveVideo(c.Request.Context(), videoRef)
	if err != nil {
		h.failVideoRef(c, err)
		return
	}

	result, err := h.Gateway.GenerateChat(c.Request.Context(), ChatRequest{
		Model:    model,
		Content:  reverseInstruction,
		VideoURL: videoURL,
		Source:   canvasSource(canvasID, nodeID, "video-prompt"),
	})
	if err != nil {
		log.Printf("promptgen: %s %s: gateway: %v", c.Request.Method, c.Request.URL.Path, err)
		apierr.Write(c, http.StatusBadGateway, "upstream_error", err.Error())
		return
	}
	c.JSON(http.StatusOK, gin.H{"text": result.Content})
}

// 视频引用解析的失败形状:引用形状不对(400)、素材不存在(404)、素材
// 不是视频(400)、内联 data: URI 视频(400,12 号票同款决策:data URI
// 进不了网关媒体契约 —— 无论是直接携带还是素材落库的 b64 产物,边界处
// 同码同因地拒绝,不留给网关预扣或上游拒收去炸难懂的错)。哨兵错误让
// resolveVideo 保持纯解析,状态码由这里定。
var (
	errVideoRefMalformed = errors.New("video_url 必须是 http(s) 地址或 /api/assets/{id}/content 内容寻址路径")
	errVideoAssetMissing = errors.New("素材不存在或已被删除")
	errVideoAssetKind    = errors.New("素材不是视频,或还没有可用的产物地址")
	errVideoAssetInline  = errors.New("该视频是内联 base64 产物,无法作为多模态输入;请使用带 http(s) 地址的视频")
)

// assetContentPrefix 是素材内容寻址路径的形状(10 号票定案):节点在厂商
// 回 b64 时持有的正是这个形式,编辑器原样上送,由服务端解出真实地址。
const assetContentPrefix = "/api/assets/"

// resolveVideo maps the editor's video reference to the address the vendor
// fetches: an http(s) URL passes through untouched; a content-addressed
// asset resolves through the store to the http(s) address it holds; an
// inline data: URI — carried directly or stored in the asset row — is
// refused (12 号票对参考图的同款决策),与其让几 MB 的请求体在网关预扣/
// 上游拒收处炸出难懂的错,不如在解析时就说明原因。
func (h *Handlers) resolveVideo(ctx context.Context, ref string) (string, error) {
	if strings.HasPrefix(ref, "http://") || strings.HasPrefix(ref, "https://") {
		return ref, nil
	}
	if strings.HasPrefix(ref, "data:") {
		return "", errVideoAssetInline
	}
	rest, ok := strings.CutPrefix(ref, assetContentPrefix)
	if !ok {
		return "", errVideoRefMalformed
	}
	idPart, suffix, found := strings.Cut(rest, "/")
	id, err := strconv.ParseInt(idPart, 10, 64)
	if err != nil || id < 1 || !found || suffix != "content" {
		return "", errVideoRefMalformed
	}
	a, err := h.Assets.Get(ctx, id)
	if errors.Is(err, asset.ErrNotFound) {
		return "", errVideoAssetMissing
	}
	if err != nil {
		return "", err
	}
	if a.Kind != asset.KindVideo || a.URL == "" {
		return "", errVideoAssetKind
	}
	if !strings.HasPrefix(a.URL, "http://") && !strings.HasPrefix(a.URL, "https://") {
		return "", errVideoAssetInline
	}
	return a.URL, nil
}

// failVideoRef maps a reference-resolution failure onto the admin-API error
// surface; store faults (non-sentinel) stay internal.
func (h *Handlers) failVideoRef(c *gin.Context, err error) {
	switch {
	case errors.Is(err, errVideoRefMalformed):
		apierr.Write(c, http.StatusBadRequest, "invalid_request", err.Error())
	case errors.Is(err, errVideoAssetKind):
		apierr.Write(c, http.StatusBadRequest, "asset_not_video", err.Error())
	case errors.Is(err, errVideoAssetInline):
		apierr.Write(c, http.StatusBadRequest, "video_inline_unsupported", err.Error())
	case errors.Is(err, errVideoAssetMissing):
		apierr.Write(c, http.StatusNotFound, "asset_not_found", err.Error())
	default:
		h.failStore(c, err)
	}
}

// canvasSource renders the canvas origin mark the gateway's usage log groups
// canvas spend by (10 号票的 source 列约定):synchronous actions carry no
// task id — the node binding and the action live in the mark. The prompt
// generation signs gen=prompt, the video reverse gen=video-prompt.
func canvasSource(canvasID int64, nodeID, action string) string {
	if nodeID == "" {
		return fmt.Sprintf("canvas=%d gen=%s", canvasID, action)
	}
	return fmt.Sprintf("canvas=%d node=%s gen=%s", canvasID, nodeID, action)
}

// requireGateway guards generations that cannot possibly run when no
// service key is configured.
func (h *Handlers) requireGateway(c *gin.Context) bool {
	if h.Gateway != nil {
		return true
	}
	apierr.Write(c, http.StatusServiceUnavailable, "gateway_unconfigured",
		"画布服务未配置网关服务 key(CANVAS_SERVICE_KEY),暂时不能生成")
	return false
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
	log.Printf("promptgen: %s %s: %v", c.Request.Method, c.Request.URL.Path, err)
	apierr.Internal(c, "服务内部错误,请稍后再试")
}

func (h *Handlers) failStore(c *gin.Context, err error) {
	log.Printf("promptgen: %s %s: %v", c.Request.Method, c.Request.URL.Path, err)
	apierr.Internal(c, "服务内部错误,请稍后再试")
}

// CatalogHandlers serves the editor's template catalog: enabled templates
// only, read from the store per request so admin edits land immediately.
type CatalogHandlers struct {
	Templates TemplateSource
}

// RegisterCatalogRoutes mounts (relative to the group, mounted at
// /prompt-templates behind the JWT middleware):
//
//	GET / — enabled templates as {id, name} options
func RegisterCatalogRoutes(group *gin.RouterGroup, h *CatalogHandlers) {
	group.GET("", h.List)
}

type templateOptionJSON struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

func (h *CatalogHandlers) List(c *gin.Context) {
	templates, err := h.Templates.ListEnabled(c.Request.Context())
	if err != nil {
		log.Printf("promptgen: list templates: %v", err)
		apierr.Internal(c, "服务内部错误,请稍后再试")
		return
	}
	options := make([]templateOptionJSON, 0, len(templates))
	for _, t := range templates {
		options = append(options, templateOptionJSON{ID: t.ID, Name: t.Name})
	}
	c.JSON(http.StatusOK, gin.H{"templates": options})
}

// ModelHandlers serves the chat-model catalog for the editor's generate UI:
// the public models priced on the token track — exactly what a prompt
// generation may use.
type ModelHandlers struct {
	Prices ModelPricer
}

// RegisterModelRoutes mounts (relative to the group, mounted at
// /prompt-models behind the JWT middleware):
//
//	GET / — token-track public model names
func RegisterModelRoutes(group *gin.RouterGroup, h *ModelHandlers) {
	group.GET("", h.List)
}

func (h *ModelHandlers) List(c *gin.Context) {
	prices, err := h.Prices.List(c.Request.Context())
	if err != nil {
		log.Printf("promptgen: list prices: %v", err)
		apierr.Internal(c, "服务内部错误,请稍后再试")
		return
	}
	models := make([]string, 0, len(prices))
	for _, p := range prices {
		if p.Unit == pricing.UnitToken && p.Token != nil {
			models = append(models, p.PublicModel)
		}
	}
	sort.Strings(models)
	c.JSON(http.StatusOK, gin.H{"models": models})
}
