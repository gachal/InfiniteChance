package voice

import (
	"errors"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/gin-gonic/gin"

	"github.com/gachal/InfiniteChance/internal/apierr"
)

// Handlers serves the 音色台账 endpoints (57 号票). canvas/server mounts
// them behind the JWT middleware —— 音色是创作侧资产(同 characters 的
// 挂法),admin-web 不设管理页。List/Patch 是纯本地台账面;Delete 先经
// 网关 delete_voice 成功再删行(上游删除失败原样透传状态与摘要,行不动
// —— 上游还在扣着工作空间配额时台账不能先走)。
type Handlers struct {
	Store Store
	// Gateway 是网关 client 的删除切片;nil = canvas/server 未配置服务
	// key,删除动作以 gateway_unconfigured 拒绝(列表/改名不受影响)。
	Gateway UpstreamRemover
}

// maxNameRunes 对齐 schema(name VARCHAR(255)),按字符数裁。
const maxNameRunes = 255

// voiceSource 是删除动作的 X-InfiniteChance-Source 审计值:usage 日志据
// 此把这次上游删除归因到画布音色台账(与 worker 的 canvas=%d task=%s 同族)。
const voiceSource = "canvas=voices"

// RegisterRoutes mounts (relative to the group, which wiring mounts at
// /voices behind the JWT middleware):
//
//	GET    /      — 全量台账(updated_at DESC)
//	PATCH  /:id   — 改名 {name}(仅本地,上游无改名 API)
//	DELETE /:id   — 先网关 delete_voice 成功再删行;被角色绑定 409
func RegisterRoutes(group *gin.RouterGroup, h *Handlers) {
	group.GET("", h.List)
	group.PATCH("/:id", h.Patch)
	group.DELETE("/:id", h.Delete)
}

// voiceJSON is the wire form of one ledger row; sample/preview 是素材行
// 引用(0 = 无),前端按内容寻址路径拼预览。
type voiceJSON struct {
	ID             int64     `json:"id"`
	VoiceID        string    `json:"voice_id"`
	ChannelID      int64     `json:"channel_id"`
	TargetModel    string    `json:"target_model"`
	Name           string    `json:"name"`
	SourceMode     string    `json:"source_mode"`
	SourcePrompt   string    `json:"source_prompt"`
	SampleAssetID  int64     `json:"sample_asset_id"`
	PreviewAssetID int64     `json:"preview_asset_id"`
	Status         string    `json:"status"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

func toVoiceJSON(v Voice) voiceJSON {
	return voiceJSON{
		ID: v.ID, VoiceID: v.VoiceID, ChannelID: v.ChannelID,
		TargetModel: v.TargetModel, Name: v.Name, SourceMode: v.SourceMode,
		SourcePrompt: v.SourcePrompt, SampleAssetID: v.SampleAssetID,
		PreviewAssetID: v.PreviewAssetID, Status: v.Status,
		CreatedAt: v.CreatedAt, UpdatedAt: v.UpdatedAt,
	}
}

func (h *Handlers) List(c *gin.Context) {
	voices, err := h.Store.List(c.Request.Context())
	if err != nil {
		h.failStore(c, err)
		return
	}
	out := make([]voiceJSON, 0, len(voices))
	for _, v := range voices {
		out = append(out, toVoiceJSON(v))
	}
	c.JSON(http.StatusOK, gin.H{"voices": out})
}

// voiceUpdateInput is PATCH 的更新体:只有 name(56 号票口径:上游无改名
// API,台账不代理 update_voice)。
type voiceUpdateInput struct {
	Name *string `json:"name"`
}

func (h *Handlers) Patch(c *gin.Context) {
	id, ok := bindID(c)
	if !ok {
		return
	}
	var in voiceUpdateInput
	if err := c.ShouldBindJSON(&in); err != nil {
		apierr.InvalidRequest(c, "请求体必须是 {name} 的 JSON")
		return
	}
	if in.Name == nil {
		apierr.InvalidRequest(c, "至少提供 name")
		return
	}
	name := strings.TrimSpace(*in.Name)
	if name == "" {
		apierr.InvalidRequest(c, "音色名不能为空")
		return
	}
	if utf8.RuneCountInString(name) > maxNameRunes {
		apierr.InvalidRequest(c, "音色名最多 255 个字符")
		return
	}
	updated, err := h.Store.Update(c.Request.Context(), id, &VoiceUpdate{Name: &name})
	if err != nil {
		h.failStore(c, err)
		return
	}
	c.JSON(http.StatusOK, toVoiceJSON(updated))
}

// Delete runs the two-step removal: the upstream voice goes first (its
// rejection passes through untouched), then the ledger row. 引用防护在
// 网关调用之前裁决 —— 上游已删而本地拒收才是真正难收拾的错序。
func (h *Handlers) Delete(c *gin.Context) {
	id, ok := bindID(c)
	if !ok {
		return
	}
	if h.Gateway == nil {
		apierr.Write(c, http.StatusServiceUnavailable, "gateway_unconfigured",
			"画布服务未配置网关服务 key(CANVAS_SERVICE_KEY),暂时不能删除音色")
		return
	}
	v, err := h.Store.Get(c.Request.Context(), id)
	if err != nil {
		h.failStore(c, err)
		return
	}
	if bound, err := h.Store.BoundCharacters(c.Request.Context(), id); err != nil {
		h.failStore(c, err)
		return
	} else if bound > 0 {
		apierr.Conflict(c, "voice_in_use", "音色仍被角色绑定,请先在角色详情中解除绑定")
		return
	}
	if err := h.Gateway.DeleteVoice(c.Request.Context(), v.ChannelID, v.VoiceID, voiceSource); err != nil {
		var ue *UpstreamError
		if errors.As(err, &ue) {
			// 网关已应答:状态与摘要原样透传(渠道没了/上游 404/熔断中
			// 各自的语义都在),台账行不动。
			apierr.Write(c, ue.Status, "upstream_error", ue.Message)
			return
		}
		// 传输层失败(网关不可达等):502,行不动,可重试。
		apierr.Write(c, http.StatusBadGateway, "upstream_error", err.Error())
		return
	}
	if err := h.Store.Delete(c.Request.Context(), id); err != nil {
		// 上游已删而本地删行失败(被引竞态/存储故障):行还在,日志留痕,
		// 管理员可重删(上游 delete 语义对已删音色幂等与否由上游裁决,
		// 重试路径会再次透传其应答)。
		h.failStore(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

// bindID parses one numeric path segment; nonsense ids answer 400 so a
// broken client sees its own bug instead of a misleading 404.
func bindID(c *gin.Context) (int64, bool) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id < 1 {
		apierr.InvalidRequest(c, "音色 id 必须是正整数")
		return 0, false
	}
	return id, true
}

// failStore maps store sentinels onto their wire responses; everything
// else is logged and answered with the generic 500.
func (h *Handlers) failStore(c *gin.Context, err error) {
	switch {
	case err == nil:
		return
	case errors.Is(err, ErrVoiceNotFound):
		apierr.NotFound(c, "音色不存在")
	case errors.Is(err, ErrVoiceInUse):
		apierr.Conflict(c, "voice_in_use", "音色仍被角色绑定,请先解除绑定")
	default:
		log.Printf("voice: %s %s: %v", c.Request.Method, c.Request.URL.Path, err)
		apierr.Internal(c, "服务内部错误,请稍后再试")
	}
}
