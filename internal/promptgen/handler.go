package promptgen

import (
	"context"
	"encoding/json"
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
// media_url 对齐 canvastask 参考图地址的上限(厂商可拉取的 http(s) 地址
// 或素材内容寻址路径)。
const (
	maxNodeIDRunes   = 128
	maxTopicRunes    = 4000
	maxMediaRefRunes = 4096
)

// 单条消息的媒体附件上限(32 号票,混合允许):图片对齐 21 号票图生图
// image_urls ≤4,视频对齐 24 号票参考视频 ≤1(聊天轨视频分节计价贵,
// 1 条封顶是成本护栏);保守自限,上游不认时 4xx 原样透出。
const (
	maxMediaImagesPerMessage = 4
	maxMediaVideosPerMessage = 1
)

// missingAssetSessionHint 拼在素材缺失错误后的行动指引(32 号票):历史
// 媒体全量重发,历史里引用的素材被删后每一轮都会撞同一个 404,指路开新
// 会话而不是静默降级。
const missingAssetSessionHint = ";若引用的素材已被删除,请开新会话后重试"

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
// substitute fakes. StreamChat backs the 31 号票 streaming variant of the
// prompt generation (same conversation, stream=true on the relay).
type Gateway interface {
	GenerateChat(ctx context.Context, req ChatRequest) (ChatResult, error)
	StreamChat(ctx context.Context, req ChatRequest, onDelta func(string)) error
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
	// PublicBaseURL answers the configured public base address of the
	// object storage for LLM-reachable media resolution (18 号票解析顺序:
	// 自有公网地址优先、厂商原址回落)。19 号票落地 settings 前没有配置
	// 来源,留 nil = 未配置,解析行为与升级前一致。
	PublicBaseURL func(ctx context.Context) string
}

// RegisterRoutes mounts (relative to the group, which the binary mounts at
// /canvases behind the JWT middleware, alongside the canvas CRUD routes):
//
//	POST /:id/generate-prompt        — {node_id?, template_id?, topic, model, history?} → text
//	POST /:id/generate-prompt/stream — 同请求体,SSE 增量返回(31 号票)
//	POST /:id/reverse-prompt         — {node_id?, video_url, model} → text
//	POST /:id/analyze                — {node_id?, media_url, media_kind, model} → text
func RegisterRoutes(group *gin.RouterGroup, h *Handlers) {
	group.POST("/:id/generate-prompt", h.Generate)
	group.POST("/:id/generate-prompt/stream", h.GenerateStream)
	group.POST("/:id/reverse-prompt", h.Reverse)
	group.POST("/:id/analyze", h.Analyze)
}

type generateInput struct {
	NodeID string `json:"node_id"`
	// TemplateID 可选(29 号票技能改为可选):0/缺省 = 未选技能,用内置
	// 通用「提示词书写」指令作首条;> 0 按技能目录裁决存在与启用。
	TemplateID int64        `json:"template_id"`
	Topic      string       `json:"topic"`
	Model      string       `json:"model"`
	History    []chatTurnIn `json:"history"`
	// Media 是本轮输入携带的媒体附件(32 号票):素材内容寻址路径或厂商
	// http(s) 地址,服务端解出 LLM 可达地址;历史轮的媒体随各轮自带。
	Media []chatMediaIn `json:"media"`
}

// chatMediaIn is one media attachment on a conversation turn (32 号票):
// ref 是素材内容寻址路径或厂商 http(s) 地址,kind 声明 image|video。
type chatMediaIn struct {
	Ref  string `json:"ref"`
	Kind string `json:"kind"`
}

// chatTurnIn is one history turn of the Agent node's multi-round conversation
// (29 号票):role+content,user/assistant 交替,由编辑器随节点 data 持久化;
// 32 号票起 user 轮可带 media(assistant 恒纯文本),resolved 是服务端解出
// 的 LLM 可达地址(不参与 JSON)。
type chatTurnIn struct {
	Role     string        `json:"role"`
	Content  string        `json:"content"`
	Media    []chatMediaIn `json:"media"`
	resolved []MediaPart
}

// agentHistoryMaxMessages 是服务端收下的历史消息条数上限:与前端「20 轮
// 上限、超出截断最旧」的纪律同宽(一轮 = 一条 user + 一条 assistant),
// 超限整单拒绝 —— 历史裁剪是编辑器的职责,服务端只挡明显失控的请求。
const agentHistoryMaxMessages = 40

// builtinPromptInstruction 是未选技能时的通用「提示词书写」首条指令(29
// 号票,反推/分析固定指令先例):与技能同形带 {topic} 占位,渲染后作会话
// 首条 user 消息。多轮里它钉住首轮主题,后续输入作为追加的 user 消息表达
// 修改意见。
const builtinPromptInstruction = "你是提示词工程师。本次对话的主题是「{topic}」:请据此写一段可直接用于 AI 生图或生视频模型的提示词," +
	"覆盖主体与场景、构图与镜头、光影与色调、风格质感;" +
	"主题之后的输入都是对当前提示词的修改意见,请在已有提示词的基础上按意见改写,保持前后连贯。" +
	"只输出提示词本身,不要任何解释、前缀或分点,用一段连贯的文字完成。"

// normalizeHistory validates the conversation history: 每轮 role 必须是
// user|assistant、内容非空且与 topic 同宽,总轮数不超上限;32 号票起 user
// 轮可带媒体附件(assistant 恒纯文本),逐条过媒体形状校验。
func normalizeHistory(raw []chatTurnIn) ([]chatTurnIn, error) {
	turns := make([]chatTurnIn, 0, len(raw))
	for _, t := range raw {
		role := strings.TrimSpace(t.Role)
		content := strings.TrimSpace(t.Content)
		if role != "user" && role != "assistant" {
			return nil, fmt.Errorf("history 的 role 必须是 user 或 assistant")
		}
		if content == "" {
			return nil, fmt.Errorf("history 的 content 不能为空")
		}
		if utf8.RuneCountInString(content) > maxTopicRunes {
			return nil, fmt.Errorf("history 单条内容最多 %d 个字符", maxTopicRunes)
		}
		media, err := normalizeTurnMedia(t.Media)
		if err != nil {
			return nil, err
		}
		if role == "assistant" && len(media) > 0 {
			return nil, fmt.Errorf("history 的 assistant 轮不能携带媒体,media 只允许挂在 user 轮")
		}
		turns = append(turns, chatTurnIn{Role: role, Content: content, Media: media})
	}
	if len(turns) > agentHistoryMaxMessages {
		return nil, fmt.Errorf("history 最多 %d 条(20 轮),请在编辑器里开新会话", agentHistoryMaxMessages)
	}
	return turns, nil
}

// normalizeTurnMedia validates one message's media attachments (32 号票):
// kind ∈ image|video、ref 非空且与媒体地址同宽,单条消息 ≤4 图 + ≤1 视频
// (图片对齐 21 号票 image_urls、视频对齐 24 号票参考视频;混合允许,
// 超限整单拒绝 —— 前端同款纪律,服务端只挡明显失控的请求)。
func normalizeTurnMedia(raw []chatMediaIn) ([]chatMediaIn, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	media := make([]chatMediaIn, 0, len(raw))
	images, videos := 0, 0
	for _, m := range raw {
		kind := strings.TrimSpace(m.Kind)
		if kind != MediaKindImage && kind != MediaKindVideo {
			return nil, fmt.Errorf("媒体的 kind 必须是 image 或 video")
		}
		ref := strings.TrimSpace(m.Ref)
		if ref == "" {
			return nil, fmt.Errorf("媒体的 ref 不能为空")
		}
		if utf8.RuneCountInString(ref) > maxMediaRefRunes {
			return nil, fmt.Errorf("媒体的 ref 最多 %d 个字符", maxMediaRefRunes)
		}
		if kind == MediaKindImage {
			images++
		} else {
			videos++
		}
		media = append(media, chatMediaIn{Ref: ref, Kind: kind})
	}
	if images > maxMediaImagesPerMessage {
		return nil, fmt.Errorf("单条消息最多 %d 张图片", maxMediaImagesPerMessage)
	}
	if videos > maxMediaVideosPerMessage {
		return nil, fmt.Errorf("单条消息最多 %d 个视频", maxMediaVideosPerMessage)
	}
	return media, nil
}

// conversationMessages renders the final chat messages (29 号票验证点的
// 形状,32 号票多模态化):首条指令(技能渲染文本,技能可选)+ 历史 +
// 本轮输入。首轮主题取历史里最早一条 user 消息 —— 会话中指令钉住首轮
// 主题,本轮输入只作为最后一条 user 消息表达修改意见。带媒体的 user 轮
// content 走分节数组(媒体在前、该轮文本在后,17 号票形状),无媒体轮
// 恒纯文本,指令首条恒纯文本 —— 首轮(无历史)带媒体时媒体随指令分节,
// 否则媒体无处可挂。历史媒体全量重发:每轮请求解出会话出现过的全部媒体。
func conversationMessages(instructionText string, history []chatTurnIn, topic string, currentMedia []MediaPart) []ChatMessage {
	firstTopic := topic
	for _, t := range history {
		if t.Role == "user" {
			firstTopic = t.Content
			break
		}
	}
	instruction := strings.ReplaceAll(instructionText, prompttemplate.TopicPlaceholder, firstTopic)
	msgs := make([]ChatMessage, 0, len(history)+2)
	if len(history) == 0 {
		if len(currentMedia) > 0 {
			return []ChatMessage{{Role: "user", Parts: MediaTextParts(currentMedia, instruction)}}
		}
		return []ChatMessage{{Role: "user", Content: instruction}}
	}
	msgs = append(msgs, ChatMessage{Role: "user", Content: instruction})
	for _, t := range history {
		if len(t.resolved) > 0 {
			msgs = append(msgs, ChatMessage{Role: t.Role, Parts: MediaTextParts(t.resolved, t.Content)})
			continue
		}
		msgs = append(msgs, ChatMessage{Role: t.Role, Content: t.Content})
	}
	if len(currentMedia) > 0 {
		msgs = append(msgs, ChatMessage{Role: "user", Parts: MediaTextParts(currentMedia, topic)})
	} else {
		msgs = append(msgs, ChatMessage{Role: "user", Content: topic})
	}
	return msgs
}

// generatePrep 是 Generate 与 GenerateStream 共用的前置产物(31 号票):
// 流式与同步的校验完全同套 —— 校验不过回答 JSON 错误,流不能开始。
type generatePrep struct {
	canvasID        int64
	nodeID          string
	instructionText string
	history         []chatTurnIn
	// currentMedia 是本轮输入的媒体(32 号票):已解出 LLM 可达地址。
	currentMedia []MediaPart
	topic        string
	model        string
}

// prepareGenerate runs the whole validation prologue the two prompt
// generation endpoints share: canvas exists, gateway configured, body and
// history well-formed, skill (optional) present and enabled, model priced.
// Every failure is answered as the admin-API JSON error before any response
// body of the caller's shape is committed.
func (h *Handlers) prepareGenerate(c *gin.Context) (generatePrep, bool) {
	canvasID, ok := bindID(c)
	if !ok {
		return generatePrep{}, false
	}
	if _, err := h.Canvases.Get(c.Request.Context(), canvasID); err != nil {
		h.failCanvas(c, err)
		return generatePrep{}, false
	}
	if !h.requireGateway(c) {
		return generatePrep{}, false
	}

	var in generateInput
	if err := c.ShouldBindJSON(&in); err != nil {
		apierr.InvalidRequest(c, "请求体必须是 {template_id?, topic, model} JSON")
		return generatePrep{}, false
	}
	nodeID := strings.TrimSpace(in.NodeID)
	if utf8.RuneCountInString(nodeID) > maxNodeIDRunes {
		apierr.InvalidRequest(c, "node_id 最多 128 个字符")
		return generatePrep{}, false
	}
	if in.TemplateID < 0 {
		apierr.InvalidRequest(c, "template_id 必须是非负整数,0 表示不选技能")
		return generatePrep{}, false
	}
	topic := strings.TrimSpace(in.Topic)
	if topic == "" {
		apierr.InvalidRequest(c, "topic 不能为空")
		return generatePrep{}, false
	}
	if utf8.RuneCountInString(topic) > maxTopicRunes {
		apierr.InvalidRequest(c, "topic 最多 4000 个字符")
		return generatePrep{}, false
	}
	model := strings.TrimSpace(in.Model)
	if model == "" {
		apierr.InvalidRequest(c, "model 不能为空")
		return generatePrep{}, false
	}
	if utf8.RuneCountInString(model) > pricing.ModelNameRunes {
		apierr.InvalidRequest(c, "model 名最多 200 个字符")
		return generatePrep{}, false
	}
	history, err := normalizeHistory(in.History)
	if err != nil {
		apierr.InvalidRequest(c, err.Error())
		return generatePrep{}, false
	}
	// 本轮输入的媒体附件(32 号票):与历史轮同套形状校验 —— media 只
	// 挂 user 轮的规则对本轮天然成立,assistant 轮的拒绝只在 history 里。
	currentTurnMedia, err := normalizeTurnMedia(in.Media)
	if err != nil {
		apierr.InvalidRequest(c, err.Error())
		return generatePrep{}, false
	}

	// 32 号票:history 与本轮的媒体逐条解引用(http(s) 直传 / 内容寻址
	// 解出素材真实地址并校验 kind / data: URI 拒绝,17 号票同套)。历史
	// 媒体全量重发,任何一条解不出 LLM 可达地址都整单拒绝 —— 流前校验,
	// 照旧回答 JSON 错误、流不开始;历史素材被删时文案指路开新会话。
	for i := range history {
		resolved, ok := h.resolveMediaRefs(c, history[i].Media, "history 里的媒体引用")
		if !ok {
			return generatePrep{}, false
		}
		history[i].resolved = resolved
	}
	currentMedia, ok := h.resolveMediaRefs(c, currentTurnMedia, "media")
	if !ok {
		return generatePrep{}, false
	}

	// 技能可选(29 号票):未选技能时首条指令用内置通用「提示词书写」,
	// 不查模板目录 —— 会话没有技能依赖,换技能才开新会话是编辑器语义。
	instructionText := builtinPromptInstruction
	if in.TemplateID > 0 {
		tpl, err := h.Templates.Get(c.Request.Context(), in.TemplateID)
		if errors.Is(err, prompttemplate.ErrNotFound) {
			apierr.NotFound(c, "技能不存在或已被删除")
			return generatePrep{}, false
		}
		if err != nil {
			h.failStore(c, err)
			return generatePrep{}, false
		}
		if !tpl.Enabled {
			apierr.Write(c, http.StatusBadRequest, "template_disabled", "技能已停用")
			return generatePrep{}, false
		}
		instructionText = tpl.Template
	}

	// 发起前先看价:模型没有按 token 计价时,聊天注定被网关拒绝 ——
	// 让用户立刻知道,而不是干等一次注定失败的上游调用。
	if !h.chatModelPriced(c, model) {
		return generatePrep{}, false
	}
	return generatePrep{
		canvasID:        canvasID,
		nodeID:          nodeID,
		instructionText: instructionText,
		history:         history,
		currentMedia:    currentMedia,
		topic:           topic,
		model:           model,
	}, true
}

// Generate renders the chosen template with the topic and relays it through
// the gateway chat surface. The text comes back to the editor synchronously:
// prompt generation is an ordinary chat call, not a task — nothing queues,
// nothing polls, and the node write happens client-side via autosave.
func (h *Handlers) Generate(c *gin.Context) {
	prep, ok := h.prepareGenerate(c)
	if !ok {
		return
	}

	result, err := h.Gateway.GenerateChat(c.Request.Context(), ChatRequest{
		Model:        prep.model,
		Conversation: conversationMessages(prep.instructionText, prep.history, prep.topic, prep.currentMedia),
		Source:       canvasSource(prep.canvasID, prep.nodeID, "prompt"),
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

// sseDataFrame 是一帧 SSE data 事件的前缀;载荷恒为 JSON(换行已转义),
// 单帧即合法事件。
var sseDataFrame = []byte("data: ")

// GenerateStream relays the same conversation as Generate but as server-sent
// events (31 号票):deltas reach the editor while the model is still writing.
// 帧形状 —— data: {"delta":"…"} 增量、data: {"error":{code,message}} 流中
// 失败、data: [DONE] 成功收尾。X-Accel-Buffering: no 让 nginx 按请求关
// proxy_buffering(gzip_types 不含 text/event-stream,无压缩缓冲),部署反
// 代与 dev 代理都无需另配。失败语义:已下发的增量不回收 —— 编辑器保留部
// 分文本,不追加历史不投递;网关已按聊天轨完成记账/退款。
func (h *Handlers) GenerateStream(c *gin.Context) {
	prep, ok := h.prepareGenerate(c)
	if !ok {
		return
	}

	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	c.Header("X-Accel-Buffering", "no")
	c.Status(http.StatusOK)

	writeEvent := func(payload []byte) {
		out := make([]byte, 0, len(sseDataFrame)+len(payload)+2)
		out = append(out, sseDataFrame...)
		out = append(out, payload...)
		out = append(out, '\n', '\n')
		_, _ = c.Writer.Write(out)
		c.Writer.Flush()
	}

	err := h.Gateway.StreamChat(c.Request.Context(), ChatRequest{
		Model:        prep.model,
		Conversation: conversationMessages(prep.instructionText, prep.history, prep.topic, prep.currentMedia),
		Source:       canvasSource(prep.canvasID, prep.nodeID, "prompt"),
	}, func(delta string) {
		body, mErr := json.Marshal(gin.H{"delta": delta})
		if mErr != nil {
			return
		}
		writeEvent(body)
	})
	if err != nil {
		log.Printf("promptgen: %s %s: gateway stream: %v", c.Request.Method, c.Request.URL.Path, err)
		if body, mErr := json.Marshal(gin.H{
			"error": gin.H{"code": "upstream_error", "message": err.Error()},
		}); mErr == nil {
			writeEvent(body)
		}
		return
	}
	writeEvent(sseDoneMarker)
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
	if utf8.RuneCountInString(videoRef) > maxMediaRefRunes {
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

	videoURL, err := h.resolveMedia(c.Request.Context(), videoRef, asset.KindVideo)
	if err != nil {
		h.failMediaRef(c, err, "video_url", "video_inline_unsupported", asset.KindVideo, "")
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

// analyzeInput is one analyze request (17 号票):media_url 是视频/图片
// 节点持有的地址,与反推同一套引用规则;media_kind 声明输入种类,决定
// 多模态分节的形状(video_url / image_url)与内容寻址素材的种类校验。
// node_id 用于用量归因。
type analyzeInput struct {
	NodeID    string `json:"node_id"`
	MediaURL  string `json:"media_url"`
	MediaKind string `json:"media_kind"`
	Model     string `json:"model"`
}

// analyzeInstruction is the fixed analysis brief (17 号票,固定常量同
// reverseInstruction 先例,无模板依赖 —— 可配置的分析模板需要多占位符
// 的模板系统,超出 MVP):视频输出结构化分镜 markdown(分镜表 + 整体
// 风格概述),图片输出同一套观察维度的画面理解。输出语言跟随指令(中文)。
const analyzeInstruction = "请分析这段媒体,输出结构化的中文 markdown 分析报告。" +
	"若输入是视频:先给分镜表(markdown 表格,列为「序号|时间码|画面内容|镜头运动|转场|声音台词」,逐镜头一行)," +
	"再给整体风格概述(画面基调、色调光影、节奏与叙事手法)。" +
	"若输入是图片:按画面内容、构图与镜头、光影色调、风格质感四节描述。" +
	"只输出分析报告本身,不要任何解释、前缀或结语。"

// Analyze understands an existing video or image and answers a structured
// markdown report (storyboard table for video): the media rides to a vision
// chat model as a video_url / image_url content part through the gateway's
// chat surface — 同步聊天调用而非画布任务(11/13 号票先例),用量按
// token 计费入网关用量日志(来源标记 gen=analyze)。文本回到编辑器,由
// 它写入分析节点(先落图后调用,编辑器负责)。
func (h *Handlers) Analyze(c *gin.Context) {
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

	var in analyzeInput
	if err := c.ShouldBindJSON(&in); err != nil {
		apierr.InvalidRequest(c, "请求体必须是 {media_url, media_kind, model} JSON")
		return
	}
	nodeID := strings.TrimSpace(in.NodeID)
	if utf8.RuneCountInString(nodeID) > maxNodeIDRunes {
		apierr.InvalidRequest(c, "node_id 最多 128 个字符")
		return
	}
	mediaRef := strings.TrimSpace(in.MediaURL)
	if mediaRef == "" {
		apierr.InvalidRequest(c, "media_url 不能为空")
		return
	}
	if utf8.RuneCountInString(mediaRef) > maxMediaRefRunes {
		apierr.InvalidRequest(c, "media_url 最多 4096 个字符")
		return
	}
	var kind string
	switch in.MediaKind {
	case "video":
		kind = asset.KindVideo
	case "image":
		kind = asset.KindImage
	default:
		apierr.InvalidRequest(c, "media_kind 必须是 video 或 image")
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

	mediaURL, err := h.resolveMedia(c.Request.Context(), mediaRef, kind)
	if err != nil {
		h.failMediaRef(c, err, "media_url", "media_inline_unsupported", kind, "")
		return
	}

	req := ChatRequest{
		Model:   model,
		Content: analyzeInstruction,
		Source:  canvasSource(canvasID, nodeID, "analyze"),
	}
	if kind == asset.KindVideo {
		req.VideoURL = mediaURL
	} else {
		req.ImageURL = mediaURL
	}
	result, err := h.Gateway.GenerateChat(c.Request.Context(), req)
	if err != nil {
		log.Printf("promptgen: %s %s: gateway: %v", c.Request.Method, c.Request.URL.Path, err)
		apierr.Write(c, http.StatusBadGateway, "upstream_error", err.Error())
		return
	}
	c.JSON(http.StatusOK, gin.H{"text": result.Content})
}

// 媒体引用解析的失败形状:引用形状不对(400)、素材不存在(404)、素材
// 种类与请求不符(400)、内联 data: URI 媒体(400,12 号票同款决策:
// data URI 进不了网关媒体契约 —— 无论是直接携带还是素材落库的 b64 产物,
// 边界处同码同因地拒绝,不留给网关预扣或上游拒收去炸难懂的错)。哨兵只
// 标原因;用户可见的文案与错误码按端点补齐(field 名反推是 video_url、
// 分析是 media_url;内联拒绝码 13 号票定 video_inline_unsupported、
// 17 号票定 media_inline_unsupported),resolveMedia 保持纯解析。
var (
	errMediaRefMalformed = errors.New("media reference must be an http(s) URL or a /api/assets/{id}/content path")
	errMediaAssetMissing = errors.New("asset does not exist or has been deleted")
	errMediaAssetKind    = errors.New("asset kind does not match the requested media kind, or holds no usable address")
	errMediaAssetInline  = errors.New("asset holds an inline base64 payload and cannot ride as multimodal input")
)

// assetContentPrefix 是素材内容寻址路径的形状(10 号票定案):节点在厂商
// 回 b64 时持有的正是这个形式,编辑器原样上送,由服务端解出真实地址。
const assetContentPrefix = "/api/assets/"

// resolveMedia maps the editor's media reference to the address the vendor
// fetches; kind selects which asset kind a content-addressed reference must
// be. An http(s) URL passes through untouched; a content-addressed asset
// resolves through the store with 18 号票的顺序:自有存储公网地址(object_key
// 拼 public_base_url,永久)优先,厂商原址(约 24h 过期)回落;an inline
// data: URI — carried directly or stored in the asset row — is refused
// (12 号票对参考图的同款决策),与其让几 MB 的请求体在网关预扣/上游拒收处
// 炸难懂的错,不如在解析时就说明原因。
func (h *Handlers) resolveMedia(ctx context.Context, ref string, kind string) (string, error) {
	if strings.HasPrefix(ref, "http://") || strings.HasPrefix(ref, "https://") {
		return ref, nil
	}
	if strings.HasPrefix(ref, "data:") {
		return "", errMediaAssetInline
	}
	rest, ok := strings.CutPrefix(ref, assetContentPrefix)
	if !ok {
		return "", errMediaRefMalformed
	}
	idPart, suffix, found := strings.Cut(rest, "/")
	id, err := strconv.ParseInt(idPart, 10, 64)
	if err != nil || id < 1 || !found || suffix != "content" {
		return "", errMediaRefMalformed
	}
	a, err := h.Assets.Get(ctx, id)
	if errors.Is(err, asset.ErrNotFound) {
		return "", errMediaAssetMissing
	}
	if err != nil {
		return "", err
	}
	if a.Kind != kind {
		return "", errMediaAssetKind
	}
	if addr, ok := asset.PublicAddress(a, h.publicBaseURL(ctx)); ok {
		return addr, nil
	}
	if a.URL == "" {
		// 上传素材(url 为空)在未配公网地址时解析不出 — 「还没有可用
		// 的产物地址」如实相告,19 号票配置后即通。
		return "", errMediaAssetKind
	}
	if !strings.HasPrefix(a.URL, "http://") && !strings.HasPrefix(a.URL, "https://") {
		return "", errMediaAssetInline
	}
	return a.URL, nil
}

// publicBaseURL reads the configured public base through the optional
// provider; nil or empty means "not configured".
func (h *Handlers) publicBaseURL(ctx context.Context) string {
	if h.PublicBaseURL == nil {
		return ""
	}
	return h.PublicBaseURL(ctx)
}

// resolveMediaRefs resolves one message's media attachments to the
// LLM-reachable addresses the gateway relays (32 号票,17 号票同套引用
// 规则):http(s) 直传、内容寻址解出素材真实地址并校验 kind、data: URI
// 拒绝。任何一条失败都经 failMediaRef 回答请求(流前校验)并报告 false。
func (h *Handlers) resolveMediaRefs(c *gin.Context, media []chatMediaIn, fieldLabel string) ([]MediaPart, bool) {
	if len(media) == 0 {
		return nil, true
	}
	parts := make([]MediaPart, 0, len(media))
	for _, m := range media {
		kind := asset.KindImage
		if m.Kind == MediaKindVideo {
			kind = asset.KindVideo
		}
		url, err := h.resolveMedia(c.Request.Context(), m.Ref, kind)
		if err != nil {
			h.failMediaRef(c, err, fieldLabel, "media_inline_unsupported", kind, missingAssetSessionHint)
			return nil, false
		}
		parts = append(parts, MediaPart{Kind: m.Kind, URL: url})
	}
	return parts, true
}

// failMediaRef maps a reference-resolution failure onto the admin-API error
// surface; store faults (non-sentinel) stay internal. fieldLabel names the
// request field the reference arrived on, inlineCode is the endpoint's
// inline-refusal code, want carries the requested asset kind (决定种类
// 不符的错误码与文案:反推与分析视频用 asset_not_video,分析图片用
// asset_not_image),missingHint 拼在素材缺失文案后(生成端点指路开新
// 会话,反推/分析无此语义传空)。
func (h *Handlers) failMediaRef(c *gin.Context, err error, fieldLabel, inlineCode string, want string, missingHint string) {
	switch {
	case errors.Is(err, errMediaRefMalformed):
		apierr.Write(c, http.StatusBadRequest, "invalid_request",
			fieldLabel+" 必须是 http(s) 地址或 /api/assets/{id}/content 内容寻址路径")
	case errors.Is(err, errMediaAssetKind):
		code := "asset_not_video"
		label := "视频"
		if want == asset.KindImage {
			code = "asset_not_image"
			label = "图片"
		}
		apierr.Write(c, http.StatusBadRequest, code,
			fmt.Sprintf("素材不是%s,或还没有可用的产物地址", label))
	case errors.Is(err, errMediaAssetInline):
		apierr.Write(c, http.StatusBadRequest, inlineCode,
			"该媒体是内联 base64 产物,无法作为多模态输入;请使用带 http(s) 地址的媒体")
	case errors.Is(err, errMediaAssetMissing):
		apierr.Write(c, http.StatusNotFound, "asset_not_found", "素材不存在或已被删除"+missingHint)
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

// CatalogHandlers serves the editor's skill catalog (29 号票 UI 改名,路径
// 不动): enabled templates only, read from the store per request so admin
// edits land immediately.
type CatalogHandlers struct {
	Templates TemplateSource
}

// RegisterCatalogRoutes mounts (relative to the group, mounted at
// /prompt-templates behind the JWT middleware):
//
//	GET / — enabled skills as {id, name, description, target} options
func RegisterCatalogRoutes(group *gin.RouterGroup, h *CatalogHandlers) {
	group.GET("", h.List)
}

type templateOptionJSON struct {
	ID          int64  `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Target      string `json:"target"`
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
		options = append(options, templateOptionJSON{
			ID: t.ID, Name: t.Name, Description: t.Description, Target: t.Target,
		})
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
		// 带折算表的是视频 token 价(25 号票),不进聊天目录。
		if p.Unit == pricing.UnitToken && p.Token != nil && !p.Token.HasVideoRates() {
			models = append(models, p.PublicModel)
		}
	}
	sort.Strings(models)
	c.JSON(http.StatusOK, gin.H{"models": models})
}
