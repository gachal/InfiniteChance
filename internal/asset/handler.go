package asset

import (
	"encoding/base64"
	"log"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/gachal/InfiniteChance/internal/apierr"
)

// Handlers serves the creator asset endpoints. 10 号票只需要内容寻址:
// 14 号票的素材库管理(列表/过滤/删除/下载)在同一个 Handlers 上继续长。
type Handlers struct {
	Store Store
}

// RegisterRoutes mounts (relative to the group):
//
//	GET /:id/content — the artifact: 302 to its URL, or inline for data URIs
//
// The content route deliberately sits OUTSIDE the JWT group: nodes preview
// artifacts with plain <img> tags, and image elements cannot attach an
// Authorization header. Artifacts are generated media — the same bytes the
// vendor's CDN serves publicly — so reading one by id is not a secret; every
// mutating or enumerating asset surface (14 号票) stays behind auth, and the
// route is not an open redirect: the target is the stored URL of that row.
func RegisterRoutes(group *gin.RouterGroup, h *Handlers) {
	group.GET("/:id/content", h.Content)
}

// Content serves the artifact for <img>/<video> previews. http(s) URLs
// redirect (the vendor's CDN serves the bytes); data: URIs — vendors that
// answered base64 — are decoded and written inline, because browsers refuse
// a redirect to a data: location.
func (h *Handlers) Content(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id < 1 {
		apierr.InvalidRequest(c, "素材 id 必须是正整数")
		return
	}
	a, err := h.Store.Get(c.Request.Context(), id)
	if err == ErrNotFound {
		apierr.NotFound(c, "素材不存在")
		return
	}
	if err != nil {
		log.Printf("asset: get %d: %v", id, err)
		apierr.Internal(c, "服务内部错误,请稍后再试")
		return
	}

	if !strings.HasPrefix(a.URL, "data:") {
		c.Redirect(http.StatusFound, a.URL)
		return
	}
	mime, payload, ok := splitDataURI(a.URL)
	if !ok {
		// 库里的 data URI 坏了:按缺失回答,前端 <img> 显示破图而非报错。
		log.Printf("asset: asset %d has an unparseable data URI", id)
		apierr.NotFound(c, "素材内容不可用")
		return
	}
	c.Data(http.StatusOK, mime, payload)
}

// splitDataURI decodes `data:<mime>;base64,<payload>` into its parts.
func splitDataURI(url string) (mime string, payload []byte, ok bool) {
	rest := strings.TrimPrefix(url, "data:")
	comma := strings.Index(rest, ",")
	if comma < 0 {
		return "", nil, false
	}
	meta, encoded := rest[:comma], rest[comma+1:]
	if !strings.HasSuffix(meta, ";base64") {
		return "", nil, false
	}
	mime = strings.TrimSuffix(meta, ";base64")
	if mime == "" {
		mime = "application/octet-stream"
	}
	payload, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return "", nil, false
	}
	return mime, payload, true
}
