package canvas

import (
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/gin-gonic/gin"

	"github.com/gachal/InfiniteChance/internal/apierr"
)

// MaxGraphBytes caps the whole-graph request body. A canvas JSON of a few
// hundred nodes stays orders of magnitude below this; the cap only exists
// so a runaway editor cannot write unbounded documents into MySQL.
const MaxGraphBytes = 4 << 20 // 4 MiB

const maxNameRunes = 128

// Handlers serves the creator canvas endpoints. canvas/server mounts them
// under an authed group — every route requires a gateway-issued session.
// IPs 支撑创建/移动时的 IP 存在性校验(50 号票:画布必归属 IP);nil 时
// 校验退化为直接落库(仅测试形态,生产装配恒非 nil)。
type Handlers struct {
	Store Store
	IPs   IPStore
}

// DefaultCanvasType is the create-time default when the request omits type
// (50 号票:新建请求缺省 type 落 general;存量收编口径是 animation,两者
// 各司其职)。type 不校验枚举值,原样落库(注册表驱动,ADR 0004 定案 2)。
const DefaultCanvasType = "general"

// RegisterRoutes mounts (relative to the group, which the binary mounts at
// /canvases behind the JWT middleware):
//
//	GET    /                    — list summaries (no graph);?ip_id= 过滤
//	POST   /                    — create {name, ip_id, type?}, answers the detail
//	GET    /:id                 — detail incl. whole-graph JSON
//	PATCH  /:id                 — partial update {name?/type?/ip_id?}(移动 = 改 ip_id)
//	DELETE /:id                 — remove (204)
//	PUT    /:id/graph           — auto-save {graph, version}; optimistic lock
func RegisterRoutes(group *gin.RouterGroup, h *Handlers) {
	group.GET("", h.List)
	group.POST("", h.Create)
	group.GET("/:id", h.Get)
	group.PATCH("/:id", h.Update)
	group.DELETE("/:id", h.Delete)
	group.PUT("/:id/graph", h.SaveGraph)
}

// summaryJSON is the wire form of a list item: no graph — the list page
// needs only names, and whole-graph documents can be large. ip_id/ip_name/
// type 是 50 号票的分组面(详情页按类型分组、编辑器面包屑同源)。
type summaryJSON struct {
	ID        int64     `json:"id"`
	Name      string    `json:"name"`
	Version   int64     `json:"version"`
	IPID      int64     `json:"ip_id"`
	IPName    string    `json:"ip_name"`
	Type      string    `json:"type"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type detailJSON struct {
	ID        int64           `json:"id"`
	Name      string          `json:"name"`
	Version   int64           `json:"version"`
	IPID      int64           `json:"ip_id"`
	IPName    string          `json:"ip_name"`
	Type      string          `json:"type"`
	Graph     json.RawMessage `json:"graph"`
	CreatedAt time.Time       `json:"created_at"`
	UpdatedAt time.Time       `json:"updated_at"`
}

// saveResponse tells the editor the version to send with its next save.
type saveResponse struct {
	Version   int64     `json:"version"`
	UpdatedAt time.Time `json:"updated_at"`
}

func toSummary(c Canvas) summaryJSON {
	return summaryJSON{
		ID: c.ID, Name: c.Name, Version: c.Version,
		IPID: c.IPID, IPName: c.IPName, Type: c.Type,
		CreatedAt: c.CreatedAt, UpdatedAt: c.UpdatedAt,
	}
}

func toDetail(c Canvas) detailJSON {
	return detailJSON{
		ID: c.ID, Name: c.Name, Version: c.Version,
		IPID: c.IPID, IPName: c.IPName, Type: c.Type,
		Graph:     json.RawMessage(c.Graph),
		CreatedAt: c.CreatedAt, UpdatedAt: c.UpdatedAt,
	}
}

func (h *Handlers) List(c *gin.Context) {
	var ipID int64
	if v := c.Query("ip_id"); v != "" {
		id, err := strconv.ParseInt(v, 10, 64)
		if err != nil || id < 1 {
			apierr.InvalidRequest(c, "ip_id 必须是正整数")
			return
		}
		ipID = id
	}
	canvases, err := h.Store.List(c.Request.Context(), ipID)
	if err != nil {
		h.failStore(c, err)
		return
	}
	summaries := make([]summaryJSON, 0, len(canvases))
	for _, cv := range canvases {
		summaries = append(summaries, toSummary(cv))
	}
	c.JSON(http.StatusOK, gin.H{"canvases": summaries})
}

type createInput struct {
	Name string `json:"name"`
	IPID *int64 `json:"ip_id"`
	Type string `json:"type"`
}

func (h *Handlers) Create(c *gin.Context) {
	var in createInput
	if err := c.ShouldBindJSON(&in); err != nil {
		apierr.InvalidRequest(c, "请求体必须是包含 name 与 ip_id 的 JSON")
		return
	}
	name := strings.TrimSpace(in.Name)
	if name == "" {
		apierr.InvalidRequest(c, "画布名不能为空")
		return
	}
	if utf8.RuneCountInString(name) > maxNameRunes {
		apierr.InvalidRequest(c, "画布名最多 128 个字符")
		return
	}
	if in.IPID == nil || *in.IPID < 1 {
		apierr.Write(c, http.StatusBadRequest, "ip_not_found", "画布必须归属一个 IP 企划")
		return
	}
	if err := h.requireIP(c, *in.IPID); err != nil {
		return
	}
	canvasType := strings.TrimSpace(in.Type)
	if canvasType == "" {
		canvasType = DefaultCanvasType
	}
	created, err := h.Store.Create(c.Request.Context(), *in.IPID, name, []byte(`{"nodes":[],"edges":[]}`), canvasType)
	if err != nil {
		h.failStore(c, err)
		return
	}
	c.JSON(http.StatusCreated, toDetail(created))
}

// requireIP validates an IP id's existence for the create/move seams,
// answering 400 ip_not_found when absent. 存在性校验由 IPStore 裁决。
func (h *Handlers) requireIP(c *gin.Context, ipID int64) error {
	if h.IPs == nil {
		return nil
	}
	ok, err := h.IPs.IPExists(c.Request.Context(), ipID)
	if err != nil {
		log.Printf("canvas: ip exists %d: %v", ipID, err)
		apierr.Internal(c, "服务内部错误,请稍后再试")
		return err
	}
	if !ok {
		apierr.Write(c, http.StatusBadRequest, "ip_not_found", "IP 企划不存在")
		return ErrIPNotFound
	}
	return nil
}

func (h *Handlers) Get(c *gin.Context) {
	cv, ok := h.load(c)
	if !ok {
		return
	}
	c.JSON(http.StatusOK, toDetail(cv))
}

// updateInput is PATCH 的部分更新体:nil 字段不动(50 号票:name/type/
// ip_id 三者可任选;ip_id 即「画布移动」)。
type updateInput struct {
	Name *string `json:"name"`
	Type *string `json:"type"`
	IPID *int64  `json:"ip_id"`
}

func (h *Handlers) Update(c *gin.Context) {
	id, ok := bindID(c)
	if !ok {
		return
	}
	var in updateInput
	if err := c.ShouldBindJSON(&in); err != nil {
		apierr.InvalidRequest(c, "请求体必须是 {name?/type?/ip_id?} 的 JSON")
		return
	}
	if in.Name == nil && in.Type == nil && in.IPID == nil {
		apierr.InvalidRequest(c, "至少提供 name、type 或 ip_id 之一")
		return
	}
	ctx := c.Request.Context()
	if in.Name != nil {
		name := strings.TrimSpace(*in.Name)
		if name == "" {
			apierr.InvalidRequest(c, "画布名不能为空")
			return
		}
		if utf8.RuneCountInString(name) > maxNameRunes {
			apierr.InvalidRequest(c, "画布名最多 128 个字符")
			return
		}
		if _, err := h.Store.Rename(ctx, id, name); err != nil {
			h.failStore(c, err)
			return
		}
	}
	if in.Type != nil {
		canvasType := strings.TrimSpace(*in.Type)
		if canvasType == "" {
			apierr.InvalidRequest(c, "type 不能为空")
			return
		}
		if _, err := h.Store.SetType(ctx, id, canvasType); err != nil {
			h.failStore(c, err)
			return
		}
	}
	if in.IPID != nil {
		if *in.IPID < 1 {
			apierr.Write(c, http.StatusBadRequest, "ip_not_found", "画布必须归属一个 IP 企划")
			return
		}
		if err := h.requireIP(c, *in.IPID); err != nil {
			return
		}
		if _, err := h.Store.Move(ctx, id, *in.IPID); err != nil {
			h.failStore(c, err)
			return
		}
	}
	cv, err := h.Store.Get(ctx, id)
	if err != nil {
		h.failStore(c, err)
		return
	}
	c.JSON(http.StatusOK, toDetail(cv))
}

func (h *Handlers) Delete(c *gin.Context) {
	id, ok := bindID(c)
	if !ok {
		return
	}
	if err := h.Store.Delete(c.Request.Context(), id); err != nil {
		h.failStore(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

type graphInput struct {
	Graph   json.RawMessage `json:"graph"`
	Version *int64          `json:"version"`
}

func (h *Handlers) SaveGraph(c *gin.Context) {
	// 上限在读 Body 之前挂上:超限请求在读的过程中就被截断,
	// 不会先把大文档整块吞进内存。
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, MaxGraphBytes)

	id, ok := bindID(c)
	if !ok {
		return
	}
	var in graphInput
	if err := c.ShouldBindJSON(&in); err != nil {
		apierr.InvalidRequest(c, "请求体必须是 {graph, version},且大小不超过 4 MiB")
		return
	}
	if in.Version == nil || *in.Version < 1 {
		apierr.InvalidRequest(c, "version 必须是不小于 1 的整数")
		return
	}
	if len(in.Graph) == 0 || !isJSONObject(in.Graph) {
		apierr.InvalidRequest(c, "graph 必须是 JSON 对象(整图文档)")
		return
	}

	saved, err := h.Store.SaveGraph(c.Request.Context(), id, in.Graph, *in.Version)
	if err != nil {
		h.failStore(c, err)
		return
	}
	c.JSON(http.StatusOK, saveResponse{Version: saved.Version, UpdatedAt: saved.UpdatedAt})
}

// isJSONObject reports whether doc is exactly one JSON object — the shape
// the editor owns. Anything else (arrays, scalars, trailing garbage) is a
// client bug we'd rather hear about now.
func isJSONObject(doc []byte) bool {
	dec := json.NewDecoder(strings.NewReader(string(doc)))
	var obj map[string]json.RawMessage
	if err := dec.Decode(&obj); err != nil {
		return false
	}
	return dec.Decode(new(json.RawMessage)) == io.EOF
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

// failStore maps store sentinels onto their wire responses; everything else
// is logged and answered with the generic 500.
func (h *Handlers) failStore(c *gin.Context, err error) {
	switch {
	case err == nil:
		return
	case errors.Is(err, ErrNotFound):
		apierr.NotFound(c, "画布不存在")
	case errors.Is(err, ErrVersionConflict):
		apierr.Conflict(c, "version_conflict", "画布已在其他窗口被修改")
	default:
		log.Printf("canvas: %s %s: %v", c.Request.Method, c.Request.URL.Path, err)
		apierr.Internal(c, "服务内部错误,请稍后再试")
	}
}

// load fetches the canvas or answers the store's error.
func (h *Handlers) load(c *gin.Context) (Canvas, bool) {
	id, ok := bindID(c)
	if !ok {
		return Canvas{}, false
	}
	cv, err := h.Store.Get(c.Request.Context(), id)
	if err != nil {
		h.failStore(c, err)
		return Canvas{}, false
	}
	return cv, true
}
