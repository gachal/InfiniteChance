package character

import (
	"context"
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
	"github.com/gachal/InfiniteChance/internal/voice"
)

// Handlers serves the 角色库 endpoints (51 号票). canvas/server mounts them
// behind the JWT middleware —— 角色是创作侧实体,admin-web 不设角色管理页
// (底层素材行已可在素材域审计)。错误形状管理面统一。
type Handlers struct {
	Store Store
	// IPs 裁决建角色/列角色的 IP 存在性(角色严格限本 IP,不存在即 404;
	// 消费面 52 号票的 character.ip_id == canvas.ip_id 校验另在画布侧)。
	IPs canvas.IPStore
	// Voices 裁决角色绑定音色的存在性与可用性(57 号票:只认台账里
	// status=OK 的行);nil 时绑定请求以 500 暴露装配缺口(生产装配恒非
	// nil,既有测试装配不受扰)。
	Voices VoiceChecker
	// Gateway 是绑定实时校准的网关查询切片(57 号票评审 F1):台账行的
	// status 只反映最后一次校准,绑定/换绑前经网关 query_voice 复核上游
	// 生命周期,仅 OK 放行。nil 时绑定(非解除)以 gateway_unconfigured
	// 拒绝 —— 与 /voices 删除面同一姿势,解除与角色其余字段不受影响。
	Gateway voice.UpstreamQuerier
}

// voiceSource 是绑定校准查询的 X-InfiniteChance-Source 审计值:与音色台账
// 删除面的 canvas=voices 同族,网关侧把这次 query 归因到角色绑定动作。
const voiceSource = "canvas=characters"

// VoiceChecker is the slice of the voice store the PATCH handler needs:
// 绑定校验只问「行存在且状态可用」,不引整个台账接口。
type VoiceChecker interface {
	Get(ctx context.Context, id int64) (voice.Voice, error)
}

// 列宽对齐 schema(name/alias VARCHAR(255)、description/note VARCHAR(1024)、
// label VARCHAR(32)、detail TEXT),按字符数而非字节裁;detail 的 20000
// runes 上限是中文形态下 TEXT 64KB 字节预算内的安全值。
const (
	maxNameRunes        = 255
	maxDescriptionRunes = 1024
	maxLabelRunes       = 32
	maxDetailRunes      = 20000
)

// RegisterIPRoutes mounts the per-IP endpoints (relative to the group,
// which wiring mounts at /ips/:id/characters behind the JWT middleware):
//
//	GET  / — 该 IP 的角色目录,完整树一次取全(角色 → 变体 → 图)
//	POST / — create {name, alias?, description?, detail?}(建角色 + 默认变体)
func RegisterIPRoutes(group *gin.RouterGroup, h *Handlers) {
	group.GET("", h.List)
	group.POST("", h.Create)
}

// RegisterRoutes mounts the character-scoped endpoints (relative to the
// group, mounted at /characters):
//
//	GET    /:cid         — detail(含完整树)
//	PATCH  /:cid         — partial update {name?/alias?/description?/detail?}
//	DELETE /:cid         — remove (204);级联删变体与图行,素材行不动
//	POST   /:cid/variants — {name, note?}
//	POST   /:cid/images   — {variant_id, asset_id, label?, sort?}
func RegisterRoutes(group *gin.RouterGroup, h *Handlers) {
	group.GET("/:cid", h.Get)
	group.PATCH("/:cid", h.PatchCharacter)
	group.DELETE("/:cid", h.DeleteCharacter)
	group.POST("/:cid/variants", h.CreateVariant)
	group.POST("/:cid/images", h.CreateImage)
}

// RegisterVariantRoutes mounts PATCH/DELETE /character-variants/:vid
// (最后一个变体拒删 → 409 variant_last_one)。
func RegisterVariantRoutes(group *gin.RouterGroup, h *Handlers) {
	group.PATCH("/:vid", h.PatchVariant)
	group.DELETE("/:vid", h.DeleteVariant)
}

// RegisterImageRoutes mounts PATCH/DELETE /character-images/:imgid
// (PATCH 支持 label/sort/换变体;DELETE 只删图行)。
func RegisterImageRoutes(group *gin.RouterGroup, h *Handlers) {
	group.PATCH("/:imgid", h.PatchImage)
	group.DELETE("/:imgid", h.DeleteImage)
}

// imageJSON is the wire form of one 设定图:纯素材引用(asset_id),
// 素材行被删后本行保留,展示面按素材可达性落「素材不可用」占位。
type imageJSON struct {
	ID          int64  `json:"id"`
	CharacterID int64  `json:"character_id"`
	VariantID   int64  `json:"variant_id"`
	AssetID     int64  `json:"asset_id"`
	Label       string `json:"label"`
	Sort        int    `json:"sort"`
}

// variantJSON is the wire form of one 造型变体。
type variantJSON struct {
	ID          int64       `json:"id"`
	CharacterID int64       `json:"character_id"`
	Name        string      `json:"name"`
	Note        string      `json:"note"`
	Sort        int         `json:"sort"`
	Images      []imageJSON `json:"images"`
	CreatedAt   time.Time   `json:"created_at"`
	UpdatedAt   time.Time   `json:"updated_at"`
}

// characterJSON is the wire form of one character with its full tree;
// cover_asset_id 是卡片首图投影(首个变体排序最前的视图类图,无则任意
// 第一张,全空 0 = 卡片走渐变+首字占位)。voice_id 是绑定音色的台账行
// 引用(57 号票,0 = 未绑定)。
type characterJSON struct {
	ID           int64         `json:"id"`
	IPID         int64         `json:"ip_id"`
	Name         string        `json:"name"`
	Alias        string        `json:"alias"`
	Description  string        `json:"description"`
	Detail       string        `json:"detail"`
	CoverAssetID int64         `json:"cover_asset_id"`
	VoiceID      int64         `json:"voice_id"`
	Variants     []variantJSON `json:"variants"`
	CreatedAt    time.Time     `json:"created_at"`
	UpdatedAt    time.Time     `json:"updated_at"`
}

func toImageJSON(img Image) imageJSON {
	return imageJSON{
		ID: img.ID, CharacterID: img.CharacterID, VariantID: img.VariantID,
		AssetID: img.AssetID, Label: img.Label, Sort: img.Sort,
	}
}

func toVariantJSON(v Variant) variantJSON {
	images := make([]imageJSON, 0, len(v.Images))
	for _, img := range v.Images {
		images = append(images, toImageJSON(img))
	}
	return variantJSON{
		ID: v.ID, CharacterID: v.CharacterID, Name: v.Name, Note: v.Note,
		Sort: v.Sort, Images: images, CreatedAt: v.CreatedAt, UpdatedAt: v.UpdatedAt,
	}
}

func toCharacterJSON(c Character) characterJSON {
	variants := make([]variantJSON, 0, len(c.Variants))
	for _, v := range c.Variants {
		variants = append(variants, toVariantJSON(v))
	}
	return characterJSON{
		ID: c.ID, IPID: c.IPID, Name: c.Name, Alias: c.Alias,
		Description: c.Description, Detail: c.Detail,
		CoverAssetID: CoverAssetID(c), VoiceID: c.VoiceID,
		Variants: variants, CreatedAt: c.CreatedAt, UpdatedAt: c.UpdatedAt,
	}
}

func (h *Handlers) List(c *gin.Context) {
	ipID, ok := h.requireIP(c, c.Param("id"))
	if !ok {
		return
	}
	characters, err := h.Store.ListCharacters(c.Request.Context(), ipID)
	if err != nil {
		h.failStore(c, err)
		return
	}
	out := make([]characterJSON, 0, len(characters))
	for _, ch := range characters {
		out = append(out, toCharacterJSON(ch))
	}
	c.JSON(http.StatusOK, gin.H{"characters": out})
}

type characterCreateInput struct {
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
	var in characterCreateInput
	if err := c.ShouldBindJSON(&in); err != nil {
		apierr.InvalidRequest(c, "请求体必须是包含 name 的 JSON")
		return
	}
	name := strings.TrimSpace(in.Name)
	if !validateText(c, name, maxNameRunes, "角色名不能为空", "角色名最多 255 个字符") {
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
	created, err := h.Store.CreateCharacter(c.Request.Context(), ipID, CharacterInput{
		Name: name, Alias: alias, Description: description, Detail: detail,
	})
	if err != nil {
		h.failStore(c, err)
		return
	}
	c.JSON(http.StatusCreated, toCharacterJSON(created))
}

func (h *Handlers) Get(c *gin.Context) {
	id, ok := bindID(c, "cid", "角色 id 必须是正整数")
	if !ok {
		return
	}
	ch, err := h.Store.GetCharacter(c.Request.Context(), id)
	if err != nil {
		h.failStore(c, err)
		return
	}
	c.JSON(http.StatusOK, toCharacterJSON(ch))
}

// characterUpdateInput is PATCH 的部分更新体:nil 字段不动;空串 = 清空
// 可选字段(名字除外,非空才合法)。voice_id 显式给出即「绑定/换绑」,
// 0 = 解除(57 号票)。
type characterUpdateInput struct {
	Name        *string `json:"name"`
	Alias       *string `json:"alias"`
	Description *string `json:"description"`
	Detail      *string `json:"detail"`
	VoiceID     *int64  `json:"voice_id"`
}

func (h *Handlers) PatchCharacter(c *gin.Context) {
	id, ok := bindID(c, "cid", "角色 id 必须是正整数")
	if !ok {
		return
	}
	var in characterUpdateInput
	if err := c.ShouldBindJSON(&in); err != nil {
		apierr.InvalidRequest(c, "请求体必须是 {name?/alias?/description?/detail?/voice_id?} 的 JSON")
		return
	}
	if in.Name == nil && in.Alias == nil && in.Description == nil && in.Detail == nil && in.VoiceID == nil {
		apierr.InvalidRequest(c, "至少提供 name、alias、description、detail 或 voice_id 之一")
		return
	}
	u := &CharacterUpdate{}
	if in.Name != nil {
		name := strings.TrimSpace(*in.Name)
		if !validateText(c, name, maxNameRunes, "角色名不能为空", "角色名最多 255 个字符") {
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
	if in.VoiceID != nil {
		if !h.validateVoiceBinding(c, *in.VoiceID) {
			return
		}
		u.VoiceID = in.VoiceID
	}
	updated, err := h.Store.UpdateCharacter(c.Request.Context(), id, u)
	if err != nil {
		h.failStore(c, err)
		return
	}
	c.JSON(http.StatusOK, toCharacterJSON(updated))
}

func (h *Handlers) DeleteCharacter(c *gin.Context) {
	id, ok := bindID(c, "cid", "角色 id 必须是正整数")
	if !ok {
		return
	}
	if err := h.Store.DeleteCharacter(c.Request.Context(), id); err != nil {
		h.failStore(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

type variantCreateInput struct {
	Name string `json:"name"`
	Note string `json:"note"`
}

func (h *Handlers) CreateVariant(c *gin.Context) {
	id, ok := bindID(c, "cid", "角色 id 必须是正整数")
	if !ok {
		return
	}
	var in variantCreateInput
	if err := c.ShouldBindJSON(&in); err != nil {
		apierr.InvalidRequest(c, "请求体必须是包含 name 的 JSON")
		return
	}
	name := strings.TrimSpace(in.Name)
	if !validateText(c, name, maxNameRunes, "变体名不能为空", "变体名最多 255 个字符") {
		return
	}
	note := strings.TrimSpace(in.Note)
	if !validateOptional(c, note, maxDescriptionRunes, "服装描述最多 1024 个字符") {
		return
	}
	created, err := h.Store.CreateVariant(c.Request.Context(), id, name, note)
	if err != nil {
		h.failStore(c, err)
		return
	}
	c.JSON(http.StatusCreated, toVariantJSON(created))
}

// variantUpdateInput 支持 name/note/sort(改名字与服装描述、展示排序)。
type variantUpdateInput struct {
	Name *string `json:"name"`
	Note *string `json:"note"`
	Sort *int    `json:"sort"`
}

func (h *Handlers) PatchVariant(c *gin.Context) {
	id, ok := bindID(c, "vid", "变体 id 必须是正整数")
	if !ok {
		return
	}
	var in variantUpdateInput
	if err := c.ShouldBindJSON(&in); err != nil {
		apierr.InvalidRequest(c, "请求体必须是 {name?/note?/sort?} 的 JSON")
		return
	}
	if in.Name == nil && in.Note == nil && in.Sort == nil {
		apierr.InvalidRequest(c, "至少提供 name、note 或 sort 之一")
		return
	}
	u := &VariantUpdate{}
	if in.Name != nil {
		name := strings.TrimSpace(*in.Name)
		if !validateText(c, name, maxNameRunes, "变体名不能为空", "变体名最多 255 个字符") {
			return
		}
		u.Name = &name
	}
	if in.Note != nil {
		note := strings.TrimSpace(*in.Note)
		if !validateOptional(c, note, maxDescriptionRunes, "服装描述最多 1024 个字符") {
			return
		}
		u.Note = &note
	}
	if in.Sort != nil {
		if !validateSort(c, *in.Sort) {
			return
		}
		u.Sort = in.Sort
	}
	updated, err := h.Store.UpdateVariant(c.Request.Context(), id, u)
	if err != nil {
		h.failStore(c, err)
		return
	}
	c.JSON(http.StatusOK, toVariantJSON(updated))
}

func (h *Handlers) DeleteVariant(c *gin.Context) {
	id, ok := bindID(c, "vid", "变体 id 必须是正整数")
	if !ok {
		return
	}
	if err := h.Store.DeleteVariant(c.Request.Context(), id); err != nil {
		h.failStore(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

type imageCreateInput struct {
	VariantID int64  `json:"variant_id"`
	AssetID   int64  `json:"asset_id"`
	Label     string `json:"label"`
	Sort      *int   `json:"sort"`
}

func (h *Handlers) CreateImage(c *gin.Context) {
	id, ok := bindID(c, "cid", "角色 id 必须是正整数")
	if !ok {
		return
	}
	var in imageCreateInput
	if err := c.ShouldBindJSON(&in); err != nil {
		apierr.InvalidRequest(c, "请求体必须是 {variant_id, asset_id} 的 JSON")
		return
	}
	if in.VariantID < 1 {
		apierr.InvalidRequest(c, "variant_id 必须是正整数")
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
		VariantID: in.VariantID, AssetID: in.AssetID, Label: label, Sort: sort,
	})
	if err != nil {
		h.failStore(c, err)
		return
	}
	c.JSON(http.StatusCreated, toImageJSON(created))
}

// imageUpdateInput 支持 label/sort/换变体(nil 字段不动;变体必须同角色)。
type imageUpdateInput struct {
	Label     *string `json:"label"`
	Sort      *int    `json:"sort"`
	VariantID *int64  `json:"variant_id"`
}

func (h *Handlers) PatchImage(c *gin.Context) {
	id, ok := bindID(c, "imgid", "设定图 id 必须是正整数")
	if !ok {
		return
	}
	var in imageUpdateInput
	if err := c.ShouldBindJSON(&in); err != nil {
		apierr.InvalidRequest(c, "请求体必须是 {label?/sort?/variant_id?} 的 JSON")
		return
	}
	if in.Label == nil && in.Sort == nil && in.VariantID == nil {
		apierr.InvalidRequest(c, "至少提供 label、sort 或 variant_id 之一")
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
	if in.VariantID != nil {
		if *in.VariantID < 1 {
			apierr.InvalidRequest(c, "variant_id 必须是正整数")
			return
		}
		u.VariantID = in.VariantID
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

// validateVoiceBinding rejects a character binding to a voice that is not
// usable right now(57 号票:只认台账里 status=OK 的行;0 = 解除,直接
// 放行不校准)。false = 已应答 4xx/5xx。
func (h *Handlers) validateVoiceBinding(c *gin.Context, voiceID int64) bool {
	if voiceID == 0 {
		return true
	}
	if voiceID < 0 {
		apierr.InvalidRequest(c, "voice_id 必须是非负整数(0 = 解除绑定)")
		return false
	}
	if h.Voices == nil {
		log.Printf("character: voice binding without a voices store (装配缺口)")
		apierr.Internal(c, "服务内部错误,请稍后再试")
		return false
	}
	v, err := h.Voices.Get(c.Request.Context(), voiceID)
	if errors.Is(err, voice.ErrVoiceNotFound) {
		apierr.InvalidRequest(c, "绑定的音色不存在")
		return false
	}
	if err != nil {
		log.Printf("character: load voice %d: %v", voiceID, err)
		apierr.Internal(c, "服务内部错误,请稍后再试")
		return false
	}
	if v.Status != voice.StatusOK {
		apierr.InvalidRequest(c, "音色「"+v.Name+"」当前不可用(状态 "+v.Status+"),暂不能绑定")
		return false
	}
	return h.calibrateVoiceBinding(c, v)
}

// calibrateVoiceBinding re-checks one locally-OK ledger row against the
// upstream lifecycle before a character may bind it (57 号票评审 F1):
// 台账行只反映最后一次校准 —— 上游已删/已翻 UNDEPLOYED 的音色照样可被
// 绑定是校准缺口,这里的实时 query 才是绑定时刻的事实。仅 OK 放行;
// 网关应答的拒绝按状态透传(上游 404 = 音色已不存在,404 语义;其余
// 4xx 原样),传输层失败 502 可重试。
func (h *Handlers) calibrateVoiceBinding(c *gin.Context, v voice.Voice) bool {
	if h.Gateway == nil {
		log.Printf("character: voice binding without a gateway client (装配缺口)")
		apierr.Write(c, http.StatusServiceUnavailable, "gateway_unconfigured",
			"画布服务未配置网关服务 key(CANVAS_SERVICE_KEY),暂时不能绑定音色")
		return false
	}
	status, err := h.Gateway.QueryVoice(c.Request.Context(), v.ChannelID, v.VoiceID, voiceSource)
	if err != nil {
		var ue *voice.UpstreamError
		if errors.As(err, &ue) {
			if ue.Status == http.StatusNotFound {
				apierr.NotFound(c, "音色「"+v.Name+"」在上游已不存在(可能已被删除),不能绑定")
				return false
			}
			// 网关已应答:状态与摘要原样透传(渠道没了/熔断中各自的语义
			// 都在),台账行与绑定都不动。
			apierr.Write(c, ue.Status, "upstream_error", ue.Message)
			return false
		}
		// 传输层失败(网关不可达等):502,可重试。
		log.Printf("character: calibrate voice %d (upstream %s): %v", v.ID, v.VoiceID, err)
		apierr.Write(c, http.StatusBadGateway, "upstream_error", err.Error())
		return false
	}
	switch status {
	case voice.StatusOK:
		return true
	case voice.StatusUndeployed:
		apierr.Conflict(c, "voice_undeployed", "音色「"+v.Name+"」未通过上游审核(UNDEPLOYED),不能绑定")
	case voice.StatusDeploying:
		apierr.Conflict(c, "voice_not_ready", "音色「"+v.Name+"」仍在部署中,部署完成后再绑定")
	default:
		log.Printf("character: voice %d query answered unknown status %q", v.ID, status)
		apierr.Write(c, http.StatusBadGateway, "upstream_error", "网关音色状态应答异常: "+status)
	}
	return false
}

// requireIP parses the :id path segment and answers the IP's existence
// (角色严格限本 IP:不存在的 IP 拒建拒列,404 而非空列表)。
func (h *Handlers) requireIP(c *gin.Context, raw string) (int64, bool) {
	id, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || id < 1 {
		apierr.InvalidRequest(c, "IP id 必须是正整数")
		return 0, false
	}
	exists, err := h.IPs.IPExists(c.Request.Context(), id)
	if err != nil {
		log.Printf("character ips: %s %s: %v", c.Request.Method, c.Request.URL.Path, err)
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
	case errors.Is(err, ErrCharacterNotFound):
		apierr.NotFound(c, "角色不存在")
	case errors.Is(err, ErrVariantNotFound):
		apierr.NotFound(c, "造型变体不存在")
	case errors.Is(err, ErrImageNotFound):
		apierr.NotFound(c, "设定图不存在")
	case errors.Is(err, ErrLastVariant):
		apierr.Conflict(c, "variant_last_one", "最后一个变体不可删除,角色至少要有一个造型")
	case errors.Is(err, ErrVariantMismatch):
		apierr.InvalidRequest(c, "变体不属于该角色")
	default:
		log.Printf("character: %s %s: %v", c.Request.Method, c.Request.URL.Path, err)
		apierr.Internal(c, "服务内部错误,请稍后再试")
	}
}
