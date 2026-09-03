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
	"github.com/gachal/InfiniteChance/internal/canvas"
	"github.com/gachal/InfiniteChance/internal/pricing"
	"github.com/gachal/InfiniteChance/internal/prompttemplate"
)

// 输入上限与既有约定对齐:node_id 与节点 id 列约定同宽(VARCHAR(128)),
// model 走 pricing 的公开模型名上限,topic 是用户手输的一段话。
const (
	maxNodeIDRunes = 128
	maxTopicRunes  = 4000
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
	Models    ModelPricer
	Gateway   Gateway
}

// RegisterRoutes mounts (relative to the group, which the binary mounts at
// /canvases behind the JWT middleware, alongside the canvas CRUD routes):
//
//	POST /:id/generate-prompt — {node_id?, template_id, topic, model} → text
func RegisterRoutes(group *gin.RouterGroup, h *Handlers) {
	group.POST("/:id/generate-prompt", h.Generate)
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
	price, err := h.Models.ByModel(c.Request.Context(), model)
	if errors.Is(err, pricing.ErrNotFound) {
		apierr.Write(c, http.StatusBadRequest, "model_not_priced",
			"模型 "+model+" 未配置 token 计价,请先在管理端配置价格")
		return
	}
	if err != nil {
		h.failStore(c, err)
		return
	}
	if price.Unit != pricing.UnitToken || price.Token == nil {
		apierr.Write(c, http.StatusBadRequest, "model_not_priced",
			"模型 "+model+" 不是 token 计价的聊天模型")
		return
	}

	result, err := h.Gateway.GenerateChat(c.Request.Context(), ChatRequest{
		Model:   model,
		Content: tpl.Render(topic),
		Source:  sourceMark(canvasID, nodeID),
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

// sourceMark mirrors the canvastask worker's origin mark so the gateway's
// usage log groups canvas spend by it. No task id here — the action is
// synchronous; the node binding lives in the mark.
func sourceMark(canvasID int64, nodeID string) string {
	if nodeID == "" {
		return fmt.Sprintf("canvas=%d gen=prompt", canvasID)
	}
	return fmt.Sprintf("canvas=%d node=%s gen=prompt", canvasID, nodeID)
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
