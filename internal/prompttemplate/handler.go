package prompttemplate

import (
	"errors"
	"log"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/gachal/InfiniteChance/internal/apierr"
)

// Handlers serves the admin prompt-template endpoints. The gateway mounts
// them under /admin behind the JWT session middleware.
type Handlers struct {
	Store Store
}

// RegisterAdminRoutes mounts:
//
//	GET    /admin/prompt-templates     — list
//	POST   /admin/prompt-templates     — create
//	PUT    /admin/prompt-templates/:id — replace
//	DELETE /admin/prompt-templates/:id — remove
func RegisterAdminRoutes(group *gin.RouterGroup, h *Handlers) {
	group.GET("/prompt-templates", h.List)
	group.POST("/prompt-templates", h.Create)
	group.PUT("/prompt-templates/:id", h.Update)
	group.DELETE("/prompt-templates/:id", h.Delete)
}

// templateJSON is the wire form of a template. The full instruction text is
// admin-facing data — it round-trips so the edit form can show it.
type templateJSON struct {
	ID        int64     `json:"id"`
	Name      string    `json:"name"`
	Template  string    `json:"template"`
	Enabled   bool      `json:"enabled"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

func toTemplateJSON(t Template) templateJSON {
	return templateJSON{
		ID: t.ID, Name: t.Name, Template: t.Template, Enabled: t.Enabled,
		CreatedAt: t.CreatedAt, UpdatedAt: t.UpdatedAt,
	}
}

// templateInputJSON projects the wire body. enabled 缺省按启用处理:
// 新建模板的预期状态就是可用,显式 false 才停用。
type templateInputJSON struct {
	Name     string `json:"name"`
	Template string `json:"template"`
	Enabled  *bool  `json:"enabled"`
}

func (raw templateInputJSON) input() Input {
	enabled := true
	if raw.Enabled != nil {
		enabled = *raw.Enabled
	}
	return Input{Name: raw.Name, Template: raw.Template, Enabled: enabled}
}

func (in Input) row(id int64) Template {
	return Template{ID: id, Name: in.Name, Template: in.Template, Enabled: in.Enabled}
}

type listResponse struct {
	Templates []templateJSON `json:"templates"`
}

func (h *Handlers) List(c *gin.Context) {
	templates, err := h.Store.List(c.Request.Context())
	if err != nil {
		h.failInternal(c, err)
		return
	}
	bodies := make([]templateJSON, 0, len(templates))
	for _, t := range templates {
		bodies = append(bodies, toTemplateJSON(t))
	}
	c.JSON(http.StatusOK, listResponse{Templates: bodies})
}

func (h *Handlers) Create(c *gin.Context) {
	var raw templateInputJSON
	if err := c.ShouldBindJSON(&raw); err != nil {
		apierr.InvalidRequest(c, "请求体不是合法的模板 JSON")
		return
	}
	input, err := raw.input().Normalize()
	if err != nil {
		apierr.InvalidRequest(c, err.Error())
		return
	}

	created, err := h.Store.Create(c.Request.Context(), input.row(0))
	if err != nil {
		h.failInternal(c, err)
		return
	}
	c.JSON(http.StatusCreated, toTemplateJSON(created))
}

func (h *Handlers) Update(c *gin.Context) {
	id, ok := parseID(c)
	if !ok {
		return
	}
	var raw templateInputJSON
	if err := c.ShouldBindJSON(&raw); err != nil {
		apierr.InvalidRequest(c, "请求体不是合法的模板 JSON")
		return
	}
	input, err := raw.input().Normalize()
	if err != nil {
		apierr.InvalidRequest(c, err.Error())
		return
	}

	updated, err := h.Store.Update(c.Request.Context(), input.row(id))
	if errors.Is(err, ErrNotFound) {
		apierr.NotFound(c, "模板不存在")
		return
	}
	if err != nil {
		h.failInternal(c, err)
		return
	}
	c.JSON(http.StatusOK, toTemplateJSON(updated))
}

func (h *Handlers) Delete(c *gin.Context) {
	id, ok := parseID(c)
	if !ok {
		return
	}
	err := h.Store.Delete(c.Request.Context(), id)
	if errors.Is(err, ErrNotFound) {
		apierr.NotFound(c, "模板不存在")
		return
	}
	if err != nil {
		h.failInternal(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

// parseID reads the :id path parameter as an integer id.
func parseID(c *gin.Context) (int64, bool) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		apierr.InvalidRequest(c, "路径参数 id 必须是正整数")
		return 0, false
	}
	return id, true
}

func (h *Handlers) failInternal(c *gin.Context, err error) {
	log.Printf("prompttemplate: %s %s: %v", c.Request.Method, c.Request.URL.Path, err)
	apierr.Internal(c, "服务内部错误,请稍后再试")
}
