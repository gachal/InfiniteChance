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
type Handlers struct {
	Store Store
}

// RegisterRoutes mounts (relative to the group, which the binary mounts at
// /canvases behind the JWT middleware):
//
//	GET    /                    — list summaries (no graph)
//	POST   /                    — create {name}, answers the detail
//	GET    /:id                 — detail incl. whole-graph JSON
//	PATCH  /:id                 — rename {name}
//	DELETE /:id                 — remove (204)
//	PUT    /:id/graph           — auto-save {graph, version}; optimistic lock
func RegisterRoutes(group *gin.RouterGroup, h *Handlers) {
	group.GET("", h.List)
	group.POST("", h.Create)
	group.GET("/:id", h.Get)
	group.PATCH("/:id", h.Rename)
	group.DELETE("/:id", h.Delete)
	group.PUT("/:id/graph", h.SaveGraph)
}

// summaryJSON is the wire form of a list item: no graph — the list page
// needs only names, and whole-graph documents can be large.
type summaryJSON struct {
	ID        int64     `json:"id"`
	Name      string    `json:"name"`
	Version   int64     `json:"version"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type detailJSON struct {
	ID        int64           `json:"id"`
	Name      string          `json:"name"`
	Version   int64           `json:"version"`
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
		CreatedAt: c.CreatedAt, UpdatedAt: c.UpdatedAt,
	}
}

func toDetail(c Canvas) detailJSON {
	return detailJSON{
		ID: c.ID, Name: c.Name, Version: c.Version, Graph: json.RawMessage(c.Graph),
		CreatedAt: c.CreatedAt, UpdatedAt: c.UpdatedAt,
	}
}

func (h *Handlers) List(c *gin.Context) {
	canvases, err := h.Store.List(c.Request.Context())
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

type nameInput struct {
	Name string `json:"name"`
}

func (h *Handlers) Create(c *gin.Context) {
	name, ok := bindName(c)
	if !ok {
		return
	}
	created, err := h.Store.Create(c.Request.Context(), name, []byte(`{"nodes":[],"edges":[]}`))
	if err != nil {
		h.failStore(c, err)
		return
	}
	c.JSON(http.StatusCreated, toDetail(created))
}

func (h *Handlers) Get(c *gin.Context) {
	cv, ok := h.load(c)
	if !ok {
		return
	}
	c.JSON(http.StatusOK, toDetail(cv))
}

func (h *Handlers) Rename(c *gin.Context) {
	name, ok := bindName(c)
	if !ok {
		return
	}
	id, ok := bindID(c)
	if !ok {
		return
	}
	renamed, err := h.Store.Rename(c.Request.Context(), id, name)
	if err != nil {
		h.failStore(c, err)
		return
	}
	c.JSON(http.StatusOK, toDetail(renamed))
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

// bindName decodes {name} and enforces the shared name policy.
func bindName(c *gin.Context) (string, bool) {
	var in nameInput
	if err := c.ShouldBindJSON(&in); err != nil {
		apierr.InvalidRequest(c, "请求体必须是包含 name 的 JSON")
		return "", false
	}
	name := strings.TrimSpace(in.Name)
	if name == "" {
		apierr.InvalidRequest(c, "画布名不能为空")
		return "", false
	}
	if utf8.RuneCountInString(name) > maxNameRunes {
		apierr.InvalidRequest(c, "画布名最多 128 个字符")
		return "", false
	}
	return name, true
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
