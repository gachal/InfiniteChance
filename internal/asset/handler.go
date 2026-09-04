package asset

import (
	"errors"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/gachal/InfiniteChance/internal/apierr"
	"github.com/gachal/InfiniteChance/internal/objectstore"
)

// transferClient bounds the legacy download proxy; the worker's transfers
// use the same ceiling (transfer.go).
var transferClient = &http.Client{Timeout: transferTimeout}

// Handlers serves the creator asset endpoints. 10 号票落了内容寻址;14 号票
// 的素材库管理(列表/过滤/删除/下载)在同一组 Handlers 上继续长。
type Handlers struct {
	Store Store
	// Storage backs preview/download for transferred artifacts; nil keeps
	// the legacy behavior (redirect to the stored URL / inline data: URI).
	Storage objectstore.Store
}

// RegisterContentRoutes mounts (relative to the group, which the binary
// mounts at /assets OUTSIDE the JWT group):
//
//	GET /:id/content — the artifact: storage bytes, 302 to its URL, or
//	                   inline for data URIs; ?download=1 sends attachment
//
// The content route deliberately sits OUTSIDE the JWT group: nodes preview
// artifacts with plain <img>/<video> tags, and media elements cannot attach
// an Authorization header. Artifacts are generated media — the same bytes
// the vendor's CDN serves publicly — so reading one by id is not a secret;
// every mutating or enumerating asset surface (below) stays behind auth,
// and the redirect is not an open redirect: the target is the stored URL of
// that row.
func RegisterContentRoutes(group *gin.RouterGroup, h *Handlers) {
	group.GET("/:id/content", h.Content)
}

// RegisterLibraryRoutes mounts the library surfaces (relative to the group,
// which the binary mounts at /assets behind the JWT middleware):
//
//	GET    /     — list with kind / canvas_id filters (素材面板与管理页共用)
//	DELETE /:id  — remove one asset: object first, then the row
func RegisterLibraryRoutes(group *gin.RouterGroup, h *Handlers) {
	group.GET("", h.List)
	group.DELETE("/:id", h.Delete)
}

// listJSON is the wire form of one list row: content_url 恒为内容寻址路径
// (预览与跨画布复用统一走它),url 仅排障时关心的原始地址.
type listJSON struct {
	ID          int64  `json:"id"`
	Kind        string `json:"kind"`
	CanvasID    int64  `json:"canvas_id"`
	CanvasName  string `json:"canvas_name"`
	TaskID      string `json:"task_id"`
	Model       string `json:"model"`
	Prompt      string `json:"prompt"`
	URL         string `json:"url"`
	ContentType string `json:"content_type"`
	SizeBytes   int64  `json:"size_bytes"`
	ContentURL  string `json:"content_url"`
	CreatedAt   string `json:"created_at"`
}

// Content serves the artifact for <img>/<video> previews and downloads.
// 转存过的素材直接流出自有字节;历史行回退旧契约 —— http(s) 地址 302 重
// 定向(厂商 CDN 交付字节),data: URI(厂商回 b64 的历史产物)解码内联,
// 因为浏览器拒绝重定向到 data: 位置。?download=1 加 attachment 头,管理
// 页与素材面板的下载按钮走同一入口。
func (h *Handlers) Content(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id < 1 {
		apierr.InvalidRequest(c, "素材 id 必须是正整数")
		return
	}
	a, err := h.Store.Get(c.Request.Context(), id)
	if errors.Is(err, ErrNotFound) {
		apierr.NotFound(c, "素材不存在")
		return
	}
	if err != nil {
		log.Printf("asset: get %d: %v", id, err)
		apierr.Internal(c, "服务内部错误,请稍后再试")
		return
	}

	// 下载的 attachment 头必须在写字节之前挂好:响应头随首次写入一起
	// 出门,之后设置是 no-op(评审发现的真 bug)。
	download := c.Query("download") == "1"
	if download {
		h.setDisposition(c, a)
	}

	if a.ObjectKey != "" && h.Storage != nil {
		obj, err := h.Storage.Open(c.Request.Context(), a.ObjectKey)
		if err != nil {
			// 行在而对象不在(手工清理/存储故障):按缺失回答,节点显示
			// 占位而非报错。
			log.Printf("asset: open object %q of asset %d: %v", a.ObjectKey, id, err)
			apierr.NotFound(c, "素材内容不可用")
			return
		}
		defer obj.Close()
		size := a.SizeBytes
		if size <= 0 {
			size = -1 // 未知大小:DataFromReader 之外的字节流出口负责
		}
		if size > 0 {
			c.DataFromReader(http.StatusOK, size, a.ContentType, obj, nil)
		} else {
			c.Header("Content-Type", a.ContentType)
			c.Status(http.StatusOK)
			_, _ = io.Copy(c.Writer, obj)
		}
		return
	}

	if !strings.HasPrefix(a.URL, "data:") {
		if download {
			// 历史行没有自有字节,attachment 无法套在 302 上:下载入口
			// 在这里退化为代理流出厂商字节。
			h.proxyLegacy(c, a)
			return
		}
		c.Redirect(http.StatusFound, a.URL)
		return
	}
	payload, mimeType, ok := splitDataURI(a.URL)
	if !ok {
		// 库里的 data URI 坏了:按缺失回答,前端 <img> 显示破图而非报错。
		log.Printf("asset: asset %d has an unparseable data URI", id)
		apierr.NotFound(c, "素材内容不可用")
		return
	}
	c.Data(http.StatusOK, mimeType, payload)
}

// proxyLegacy streams a legacy http(s) asset's remote bytes so the download
// button works with one Content-Disposition contract regardless of row age.
// 预览不经过这里(仍走 302,让厂商 CDN 直接交付)。
func (h *Handlers) proxyLegacy(c *gin.Context, a Asset) {
	req, err := http.NewRequestWithContext(c.Request.Context(), http.MethodGet, a.URL, nil)
	if err != nil {
		apierr.NotFound(c, "素材内容不可用")
		return
	}
	resp, err := transferClient.Do(req)
	if err != nil {
		log.Printf("asset: proxy download of asset %d: %v", a.ID, err)
		apierr.NotFound(c, "素材内容不可用")
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		log.Printf("asset: proxy download of asset %d: HTTP %d", a.ID, resp.StatusCode)
		apierr.NotFound(c, "素材内容不可用")
		return
	}
	contentType := mediatype(resp.Header.Get("Content-Type"))
	if contentType == "" {
		contentType = defaultContentType(a.Kind)
	}
	// attachment 头同上:写字节之前挂好。
	h.setDisposition(c, a)
	if resp.ContentLength > 0 {
		c.DataFromReader(http.StatusOK, resp.ContentLength, contentType, resp.Body, nil)
	} else {
		c.Header("Content-Type", contentType)
		c.Status(http.StatusOK)
		_, _ = io.Copy(c.Writer, resp.Body)
	}
}

// setDisposition stamps the attachment header for download requests; the
// filename carries the asset id,扩展名按内容类型补齐.
func (h *Handlers) setDisposition(c *gin.Context, a Asset) {
	ct := a.ContentType
	if ct == "" {
		ct = defaultContentType(a.Kind)
	}
	c.Header("Content-Disposition",
		"attachment; filename=\"asset-"+strconv.FormatInt(a.ID, 10)+"-"+a.Kind+extensionOf(ct)+"\"")
}

// List answers the library, newest first. kind 过滤取 image/video 之外的
// 值一律空结果而非报错 —— 前端的下拉里不会有别的值,宽度留给将来.
func (h *Handlers) List(c *gin.Context) {
	f := Filter{
		Kind:  strings.TrimSpace(c.Query("kind")),
		Limit: defaultListLimit,
	}
	if v := c.Query("canvas_id"); v != "" {
		id, err := strconv.ParseInt(v, 10, 64)
		if err != nil || id < 1 {
			apierr.InvalidRequest(c, "canvas_id 必须是正整数")
			return
		}
		f.CanvasID = id
	}
	if v := c.Query("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 {
			apierr.InvalidRequest(c, "limit 必须是正整数")
			return
		}
		f.Limit = n
	}
	if v := c.Query("offset"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 0 {
			apierr.InvalidRequest(c, "offset 必须是非负整数")
			return
		}
		f.Offset = n
	}

	rows, err := h.Store.List(c.Request.Context(), f)
	if err != nil {
		log.Printf("asset: list: %v", err)
		apierr.Internal(c, "服务内部错误,请稍后再试")
		return
	}
	out := make([]listJSON, 0, len(rows))
	for _, row := range rows {
		out = append(out, listJSON{
			ID: row.ID, Kind: row.Kind, CanvasID: row.CanvasID,
			CanvasName: row.CanvasName, TaskID: row.TaskID, Model: row.Model,
			Prompt:     row.Prompt,
			URL:        row.URL,
			ContentType: row.ContentType,
			SizeBytes:  row.SizeBytes,
			ContentURL: contentPath(row.ID),
			CreatedAt:  row.CreatedAt.Format(timeFormat),
		})
	}
	c.JSON(http.StatusOK, gin.H{"assets": out})
}

// contentPath is the canonical preview/reuse address of an asset. 画布前
// 端的 dev 代理与线上部署都把 /api 前缀导向画布服务,节点数据里存的就是
// 这个相对路径 —— 跨画布复用同一素材 = 引用同一路径。
func contentPath(id int64) string {
	return "/api/assets/" + strconv.FormatInt(id, 10) + "/content"
}

// timeFormat 与前端的时间契约:RFC3339(JSON 生态的标准形状).
const timeFormat = time.RFC3339Nano

// Delete removes one asset: the object goes first, so a failed delete
// leaves the row in place for a retry and no orphaned bytes outlive the
// row. 历史行(无对象)直接删行;对象删除但行删除失败的窄缝里,预览按
// 「内容不可用」降级,不报错。
func (h *Handlers) Delete(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id < 1 {
		apierr.InvalidRequest(c, "素材 id 必须是正整数")
		return
	}
	a, err := h.Store.Get(c.Request.Context(), id)
	if errors.Is(err, ErrNotFound) {
		apierr.NotFound(c, "素材不存在")
		return
	}
	if err != nil {
		log.Printf("asset: get %d: %v", id, err)
		apierr.Internal(c, "服务内部错误,请稍后再试")
		return
	}
	if a.ObjectKey != "" && h.Storage != nil {
		if err := h.Storage.Delete(c.Request.Context(), a.ObjectKey); err != nil {
			log.Printf("asset: delete object %q of asset %d: %v", a.ObjectKey, id, err)
			apierr.Internal(c, "素材文件删除失败,请稍后再试")
			return
		}
	}
	if err := h.Store.Delete(c.Request.Context(), id); err != nil {
		if errors.Is(err, ErrNotFound) {
			apierr.NotFound(c, "素材不存在")
			return
		}
		log.Printf("asset: delete %d: %v", id, err)
		apierr.Internal(c, "服务内部错误,请稍后再试")
		return
	}
	c.Status(http.StatusNoContent)
}
