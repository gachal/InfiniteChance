package scene

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
	"github.com/gachal/InfiniteChance/internal/canvas"
)

// Handlers serves the 场景库 endpoints (65 号票). canvas/server mounts them
// behind the JWT middleware —— 场景是创作侧实体,admin-web 不设场景管理页
// (底层素材行已可在素材域审计)。错误形状管理面统一。
type Handlers struct {
	Store Store
	// IPs 裁决建场景/列场景的 IP 存在性(场景严格限本 IP,不存在即 404)。
	IPs canvas.IPStore
}

// 列宽对齐 schema(name/alias VARCHAR(255)、description VARCHAR(1024)、
// label VARCHAR(32)、detail TEXT),按字符数而非字节裁;detail 的 20000
// runes 上限是中文形态下 TEXT 64KB 字节预算内的安全值(角色库同值)。
const (
	maxNameRunes        = 255
	maxDescriptionRunes = 1024
	maxLabelRunes       = 32
	maxDetailRunes      = 20000
)

// RegisterIPRoutes mounts the per-IP endpoints (relative to the group,
// which wiring mounts at /ips/:id/scenes behind the JWT middleware):
//
//	GET  / — 该 IP 的场景目录,完整树一次取全(场景 → 图)
//	POST / — create {name, alias?, description?, detail?}
func RegisterIPRoutes(group *gin.RouterGroup, h *Handlers) {
	group.GET("", h.List)
	group.POST("", h.Create)
}

// RegisterRoutes mounts the scene-scoped endpoints (relative to the group,
// mounted at /scenes):
//
//	GET    /:sid      — detail(含全部图行)
//	PATCH  /:sid      — partial update {name?/alias?/description?/detail?}
//	DELETE /:sid      — remove (204);级联删图行,素材行不动
//	POST   /:sid/images — {asset_id, label?, sort?}
func RegisterRoutes(group *gin.RouterGroup, h *Handlers) {
	group.GET("/:sid", h.Get)
	group.PATCH("/:sid", h.PatchScene)
	group.DELETE("/:sid", h.DeleteScene)
	group.POST("/:sid/images", h.CreateImage)
}

// RegisterImageRoutes mounts PATCH/DELETE /scene-images/:imgid
// (PATCH 支持 label/sort;DELETE 只删图行)。
func RegisterImageRoutes(group *gin.RouterGroup, h *Handlers) {
	group.PATCH("/:imgid", h.PatchImage)
	group.DELETE("/:imgid", h.DeleteImage)
}

// imageJSON is the wire form of one 场景设定图:纯素材引用(asset_id),
// 素材行被删后本行保留,展示面按素材可达性落「素材不可用」占位。
type imageJSON struct {
	ID      int64  `json:"id"`
	SceneID int64  `json:"scene_id"`
	AssetID int64  `json:"asset_id"`
	Label   string `json:"label"`
	Sort    int    `json:"sort"`
}

// sceneJSON is the wire form of one scene with its images; cover_asset_id
// 是卡片首图投影(排序最前的「全景」图优先,无则第一张,全空 0 = 卡片走
// 渐变+首字占位)。
type sceneJSON struct {
	ID           int64       `json:"id"`
	IPID         int64       `json:"ip_id"`
	Name         string      `json:"name"`
	Alias        string      `json:"alias"`
	Description  string      `json:"description"`
	Detail       string      `json:"detail"`
	CoverAssetID int64       `json:"cover_asset_id"`
	Images       []imageJSON `json:"images"`
	CreatedAt    time.Time   `json:"created_at"`
	UpdatedAt    time.Time   `json:"updated_at"`
}

func toImageJSON(img Image) imageJSON {
	return imageJSON{
		ID: img.ID, SceneID: img.SceneID,
		AssetID: img.AssetID, Label: img.Label, Sort: img.Sort,
	}
}

func toSceneJSON(s Scene) sceneJSON {
	images := make([]imageJSON, 0, len(s.Images))
	for _, img := range s.Images {
		images = append(images, toImageJSON(img))
	}
	return sceneJSON{
		ID: s.ID, IPID: s.IPID, Name: s.Name, Alias: s.Alias,
		Description: s.Description, Detail: s.Detail,
		CoverAssetID: CoverAssetID(s),
		Images:       images, CreatedAt: s.CreatedAt, UpdatedAt: s.UpdatedAt,
	}
}

func (h *Handlers) List(c *gin.Context) {
	ipID, ok := h.requireIP(c, c.Param("id"))
	if !ok {
		return
	}
	scenes, err := h.Store.ListScenes(c.Request.Context(), ipID)
	if err != nil {
		h.failStore(c, err)
		return
	}
	out := make([]sceneJSON, 0, len(scenes))
	for _, sc := range scenes {
		out = append(out, toSceneJSON(sc))
	}
	c.JSON(http.StatusOK, gin.H{"scenes": out})
}

type sceneCreateInput struct {
	Name        string `json:"name"`
	Alias       string `json:"alias"`
	Description string `json:"description"`
	Detail      string `json:"detail"`
}

func (h *Handlers) Create(c *gin.Context) {
	ipID, ok := h.requireIP(c, c.Param("id"))
	if !ok {
		return
	}
	var in sceneCreateInput
	if err := c.ShouldBindJSON(&in); err != nil {
		apierr.InvalidRequest(c, "请求体必须是包含 name 的 JSON")
		return
	}
	name := strings.TrimSpace(in.Name)
	if !validateText(c, name, maxNameRunes, "场景名不能为空", "场景名最多 255 个字符") {
		return
	}
	alias := strings.TrimSpace(in.Alias)
	if !validateOptional(c, alias, maxNameRunes, "别名最多 255 个字符") {
		return
	}
	description := strings.TrimSpace(in.Description)
	if !validateOptional(c, description, maxDescriptionRunes, "简介最多 1024 个字符") {
		return
	}
	detail := strings.TrimSpace(in.Detail)
	if !validateOptional(c, detail, maxDetailRunes, "详细设定最多 20000 个字符") {
		return
	}
	created, err := h.Store.CreateScene(c.Request.Context(), ipID, SceneInput{
		Name: name, Alias: alias, Description: description, Detail: detail,
	})
	if err != nil {
		h.failStore(c, err)
		return
	}
	c.JSON(http.StatusCreated, toSceneJSON(created))
}

func (h *Handlers) Get(c *gin.Context) {
	id, ok := bindID(c, "sid", "场景 id 必须是正整数")
	if !ok {
		return
	}
	sc, err := h.Store.GetScene(c.Request.Context(), id)
	if err != nil {
		h.failStore(c, err)
		return
	}
	c.JSON(http.StatusOK, toSceneJSON(sc))
}

// sceneUpdateInput is PATCH 的部分更新体:nil 字段不动;空串 = 清空可选
// 字段(名字除外,非空才合法)。
type sceneUpdateInput struct {
	Name        *string `json:"name"`
	Alias       *string `json:"alias"`
	Description *string `json:"description"`
	Detail      *string `json:"detail"`
}

func (h *Handlers) PatchScene(c *gin.Context) {
	id, ok := bindID(c, "sid", "场景 id 必须是正整数")
	if !ok {
		return
	}
	var in sceneUpdateInput
	if err := c.ShouldBindJSON(&in); err != nil {
		apierr.InvalidRequest(c, "请求体必须是 {name?/alias?/description?/detail?} 的 JSON")
		return
	}
	if in.Name == nil && in.Alias == nil && in.Description == nil && in.Detail == nil {
		apierr.InvalidRequest(c, "至少提供 name、alias、description 或 detail 之一")
		return
	}
	u := &SceneUpdate{}
	if in.Name != nil {
		name := strings.TrimSpace(*in.Name)
		if !validateText(c, name, maxNameRunes, "场景名不能为空", "场景名最多 255 个字符") {
			return
		}
		u.Name = &name
	}
	if in.Alias != nil {
		alias := strings.TrimSpace(*in.Alias)
		if !validateOptional(c, alias, maxNameRunes, "别名最多 255 个字符") {
			return
		}
		u.Alias = &alias
	}
	if in.Description != nil {
		description := strings.TrimSpace(*in.Description)
		if !validateOptional(c, description, maxDescriptionRunes, "简介最多 1024 个字符") {
			return
		}
		u.Description = &description
	}
	if in.Detail != nil {
		detail := strings.TrimSpace(*in.Detail)
		if !validateOptional(c, detail, maxDetailRunes, "详细设定最多 20000 个字符") {
			return
		}
		u.Detail = &detail
	}
	updated, err := h.Store.UpdateScene(c.Request.Context(), id, u)
	if err != nil {
		h.failStore(c, err)
		return
	}
	c.JSON(http.StatusOK, toSceneJSON(updated))
}

func (h *Handlers) DeleteScene(c *gin.Context) {
	id, ok := bindID(c, "sid", "场景 id 必须是正整数")
	if !ok {
		return
	}
	if err := h.Store.DeleteScene(c.Request.Context(), id); err != nil {
		h.failStore(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

type imageCreateInput struct {
	AssetID int64  `json:"asset_id"`
	Label   string `json:"label"`
	Sort    *int   `json:"sort"`
}

func (h *Handlers) CreateImage(c *gin.Context) {
	id, ok := bindID(c, "sid", "场景 id 必须是正整数")
	if !ok {
		return
	}
	var in imageCreateInput
	if err := c.ShouldBindJSON(&in); err != nil {
		apierr.InvalidRequest(c, "请求体必须是 {asset_id} 的 JSON")
		return
	}
	if in.AssetID < 1 {
		apierr.InvalidRequest(c, "asset_id 必须是正整数(素材行的引用)")
		return
	}
	label := strings.TrimSpace(in.Label)
	if !validateOptional(c, label, maxLabelRunes, "标签最多 32 个字符") {
		return
	}
	sort := 0
	if in.Sort != nil {
		if !validateSort(c, *in.Sort) {
			return
		}
		sort = *in.Sort
	}
	created, err := h.Store.CreateImage(c.Request.Context(), id, ImageInput{
		AssetID: in.AssetID, Label: label, Sort: sort,
	})
	if err != nil {
		h.failStore(c, err)
		return
	}
	c.JSON(http.StatusCreated, toImageJSON(created))
}

// imageUpdateInput 支持 label/sort(nil 字段不动)。
type imageUpdateInput struct {
	Label *string `json:"label"`
	Sort  *int    `json:"sort"`
}

func (h *Handlers) PatchImage(c *gin.Context) {
	id, ok := bindID(c, "imgid", "设定图 id 必须是正整数")
	if !ok {
		return
	}
	var in imageUpdateInput
	if err := c.ShouldBindJSON(&in); err != nil {
		apierr.InvalidRequest(c, "请求体必须是 {label?/sort?} 的 JSON")
		return
	}
	if in.Label == nil && in.Sort == nil {
		apierr.InvalidRequest(c, "至少提供 label 或 sort 之一")
		return
	}
	u := &ImageUpdate{}
	if in.Label != nil {
		label := strings.TrimSpace(*in.Label)
		if !validateOptional(c, label, maxLabelRunes, "标签最多 32 个字符") {
			return
		}
		u.Label = &label
	}
	if in.Sort != nil {
		if !validateSort(c, *in.Sort) {
			return
		}
		u.Sort = in.Sort
	}
	updated, err := h.Store.UpdateImage(c.Request.Context(), id, u)
	if err != nil {
		h.failStore(c, err)
		return
	}
	c.JSON(http.StatusOK, toImageJSON(updated))
}

func (h *Handlers) DeleteImage(c *gin.Context) {
	id, ok := bindID(c, "imgid", "设定图 id 必须是正整数")
	if !ok {
		return
	}
	if err := h.Store.DeleteImage(c.Request.Context(), id); err != nil {
		h.failStore(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

// requireIP parses the :id path segment and answers the IP's existence
// (场景严格限本 IP:不存在的 IP 拒建拒列,404 而非空列表)。
func (h *Handlers) requireIP(c *gin.Context, raw string) (int64, bool) {
	id, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || id < 1 {
		apierr.InvalidRequest(c, "IP id 必须是正整数")
		return 0, false
	}
	exists, err := h.IPs.IPExists(c.Request.Context(), id)
	if err != nil {
		log.Printf("scene ips: %s %s: %v", c.Request.Method, c.Request.URL.Path, err)
		apierr.Internal(c, "服务内部错误,请稍后再试")
		return 0, false
	}
	if !exists {
		apierr.NotFound(c, "IP 企划不存在")
		return 0, false
	}
	return id, true
}

// validateText enforces the non-empty / width policy of one required field;
// false = 已应答 400。
func validateText(c *gin.Context, value string, maxRunes int, emptyMsg, tooLongMsg string) bool {
	if value == "" {
		apierr.InvalidRequest(c, emptyMsg)
		return false
	}
	return validateOptional(c, value, maxRunes, tooLongMsg)
}

// validateOptional enforces the width policy of one optional field
// (空串 = 未填,合法);false = 已应答 400。
func validateOptional(c *gin.Context, value string, maxRunes int, tooLongMsg string) bool {
	if utf8.RuneCountInString(value) > maxRunes {
		apierr.InvalidRequest(c, tooLongMsg)
		return false
	}
	return true
}

// validateSort rejects negative sort values(展示排序用非负整数,UI 按
// 下标赋值)。
func validateSort(c *gin.Context, sort int) bool {
	if sort < 0 {
		apierr.InvalidRequest(c, "sort 必须是非负整数")
		return false
	}
	return true
}

// bindID parses one numeric path segment; nonsense ids answer 400 so a
// broken client sees its own bug instead of a misleading 404.
func bindID(c *gin.Context, param, invalidMsg string) (int64, bool) {
	id, err := strconv.ParseInt(c.Param(param), 10, 64)
	if err != nil || id < 1 {
		apierr.InvalidRequest(c, invalidMsg)
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
	case errors.Is(err, ErrSceneNotFound):
		apierr.NotFound(c, "场景不存在")
	case errors.Is(err, ErrImageNotFound):
		apierr.NotFound(c, "设定图不存在")
	default:
		log.Printf("scene: %s %s: %v", c.Request.Method, c.Request.URL.Path, err)
		apierr.Internal(c, "服务内部错误,请稍后再试")
	}
}
