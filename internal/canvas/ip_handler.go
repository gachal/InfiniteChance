package canvas

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

// IPHandlers serves the IP 企划 endpoints (50 号票). canvas/server mounts
// them behind the JWT middleware like /canvases — IP 增删改全在创作侧,
// admin-web 不设 IP 管理页。
type IPHandlers struct {
	Store IPStore
}

// maxIPNameRunes / maxIPDescriptionRunes 对齐 ips 列宽(name VARCHAR(255)、
// description VARCHAR(1024)),按字符数而非字节裁,中文一句话 logline 放得下。
const (
	maxIPNameRunes        = 255
	maxIPDescriptionRunes = 1024
)

// RegisterIPRoutes mounts (relative to the group, which the binary mounts at
// /ips behind the JWT middleware):
//
//	GET    /      — list with canvas_count(排序键 = IP 与名下画布最近更新取大者)
//	POST   /      — create {name, description?, cover_asset_id?}
//	GET    /:id   — detail
//	PATCH  /:id   — partial update {name?/description?/cover_asset_id?}(0 = 清封面)
//	DELETE /:id   — remove (204);名下有画布或有直接素材 → 409 ip_not_empty
func RegisterIPRoutes(group *gin.RouterGroup, h *IPHandlers) {
	group.GET("", h.List)
	group.POST("", h.Create)
	group.GET("/:id", h.Get)
	group.PATCH("/:id", h.Update)
	group.DELETE("/:id", h.Delete)
}

// ipJSON is the wire form of one IP. latest_at 是「最近更新」的展示时点
// (排序键同源:IP 自身与名下画布 max(updated_at) 取大者),驱动卡片墙。
type ipJSON struct {
	ID           int64     `json:"id"`
	Name         string    `json:"name"`
	Description  string    `json:"description"`
	CoverAssetID int64     `json:"cover_asset_id"`
	CanvasCount  int64     `json:"canvas_count"`
	LatestAt     time.Time `json:"latest_at"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

func toIPJSON(ip IP) ipJSON {
	return ipJSON{
		ID: ip.ID, Name: ip.Name, Description: ip.Description,
		CoverAssetID: ip.CoverAssetID, CanvasCount: ip.CanvasCount,
		LatestAt: ip.LatestAt, CreatedAt: ip.CreatedAt, UpdatedAt: ip.UpdatedAt,
	}
}

func (h *IPHandlers) List(c *gin.Context) {
	ips, err := h.Store.ListIPs(c.Request.Context())
	if err != nil {
		h.failStore(c, err)
		return
	}
	out := make([]ipJSON, 0, len(ips))
	for _, ip := range ips {
		out = append(out, toIPJSON(ip))
	}
	c.JSON(http.StatusOK, gin.H{"ips": out})
}

type ipCreateInput struct {
	Name         string `json:"name"`
	Description  string `json:"description"`
	CoverAssetID int64  `json:"cover_asset_id"`
}

func (h *IPHandlers) Create(c *gin.Context) {
	var in ipCreateInput
	if err := c.ShouldBindJSON(&in); err != nil {
		apierr.InvalidRequest(c, "请求体必须是包含 name 的 JSON")
		return
	}
	name := strings.TrimSpace(in.Name)
	if !validateIPText(c, name, maxIPNameRunes, "IP 名称不能为空", "IP 名称最多 255 个字符") {
		return
	}
	// 简介可选(可空,一句话 logline):非空时只限长。
	description := strings.TrimSpace(in.Description)
	if description != "" && utf8.RuneCountInString(description) > maxIPDescriptionRunes {
		apierr.InvalidRequest(c, "简介最多 1024 个字符")
		return
	}
	if in.CoverAssetID < 0 {
		apierr.InvalidRequest(c, "cover_asset_id 必须是非负整数(0 = 无封面)")
		return
	}
	created, err := h.Store.CreateIP(c.Request.Context(), name, description, in.CoverAssetID)
	if err != nil {
		h.failStore(c, err)
		return
	}
	c.JSON(http.StatusCreated, toIPJSON(created))
}

func (h *IPHandlers) Get(c *gin.Context) {
	ip, ok := h.load(c)
	if !ok {
		return
	}
	c.JSON(http.StatusOK, toIPJSON(ip))
}

// ipUpdateInput is PATCH 的部分更新体:nil 字段不动;cover_asset_id 显式
// 0 = 清除封面(回「渐变+首字」占位)。
type ipUpdateInput struct {
	Name         *string `json:"name"`
	Description  *string `json:"description"`
	CoverAssetID *int64  `json:"cover_asset_id"`
}

func (h *IPHandlers) Update(c *gin.Context) {
	id, ok := bindIPID(c)
	if !ok {
		return
	}
	var in ipUpdateInput
	if err := c.ShouldBindJSON(&in); err != nil {
		apierr.InvalidRequest(c, "请求体必须是 {name?/description?/cover_asset_id?} 的 JSON")
		return
	}
	if in.Name == nil && in.Description == nil && in.CoverAssetID == nil {
		apierr.InvalidRequest(c, "至少提供 name、description 或 cover_asset_id 之一")
		return
	}
	u := &IPUpdate{}
	if in.Name != nil {
		name := strings.TrimSpace(*in.Name)
		if !validateIPText(c, name, maxIPNameRunes, "IP 名称不能为空", "IP 名称最多 255 个字符") {
			return
		}
		u.Name = &name
	}
	if in.Description != nil {
		// 简介可选:空串 = 清空;非空时只限长。
		description := strings.TrimSpace(*in.Description)
		if utf8.RuneCountInString(description) > maxIPDescriptionRunes {
			apierr.InvalidRequest(c, "简介最多 1024 个字符")
			return
		}
		u.Description = &description
	}
	if in.CoverAssetID != nil {
		if *in.CoverAssetID < 0 {
			apierr.InvalidRequest(c, "cover_asset_id 必须是非负整数(0 = 无封面)")
			return
		}
		u.CoverAssetID = in.CoverAssetID
	}
	updated, err := h.Store.UpdateIP(c.Request.Context(), id, u)
	if err != nil {
		h.failStore(c, err)
		return
	}
	c.JSON(http.StatusOK, toIPJSON(updated))
}

func (h *IPHandlers) Delete(c *gin.Context) {
	id, ok := bindIPID(c)
	if !ok {
		return
	}
	if err := h.Store.DeleteIP(c.Request.Context(), id); err != nil {
		h.failStore(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

// validateIPText enforces the non-empty / width policy of one text field;
// false = 已应答 400。
func validateIPText(c *gin.Context, value string, maxRunes int, emptyMsg, tooLongMsg string) bool {
	if value == "" {
		apierr.InvalidRequest(c, emptyMsg)
		return false
	}
	if utf8.RuneCountInString(value) > maxRunes {
		apierr.InvalidRequest(c, tooLongMsg)
		return false
	}
	return true
}

// bindIPID parses the :id path segment; nonsense ids answer 400 so a broken
// client sees its own bug instead of a misleading 404.
func bindIPID(c *gin.Context) (int64, bool) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id < 1 {
		apierr.InvalidRequest(c, "IP id 必须是正整数")
		return 0, false
	}
	return id, true
}

// failStore maps IP store sentinels onto their wire responses; everything
// else is logged and answered with the generic 500.
func (h *IPHandlers) failStore(c *gin.Context, err error) {
	switch {
	case err == nil:
		return
	case errors.Is(err, ErrIPNotFound):
		apierr.NotFound(c, "IP 企划不存在")
	case errors.Is(err, ErrIPNotEmpty):
		apierr.Conflict(c, "ip_not_empty", "IP 名下还有画布或素材,先移走再删除")
	default:
		log.Printf("canvas ips: %s %s: %v", c.Request.Method, c.Request.URL.Path, err)
		apierr.Internal(c, "服务内部错误,请稍后再试")
	}
}

// load fetches the IP or answers the store's error.
func (h *IPHandlers) load(c *gin.Context) (IP, bool) {
	id, ok := bindIPID(c)
	if !ok {
		return IP{}, false
	}
	ip, err := h.Store.GetIP(c.Request.Context(), id)
	if err != nil {
		h.failStore(c, err)
		return IP{}, false
	}
	return ip, true
}
