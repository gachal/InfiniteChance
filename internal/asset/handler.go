package asset

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
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

// IPExistenceChecker is the slice of the canvas IP store the upload seam
// needs(50 号票):可选 ip_id 只校验存在性,归属语义由调用方声明。生产装配
// 传 canvas.IPStore(结构化满足),测试传替身。
type IPExistenceChecker interface {
	IPExists(ctx context.Context, id int64) (bool, error)
}

// Handlers serves the creator asset endpoints. 10 号票落了内容寻址;14 号票
// 的素材库管理(列表/过滤/删除/下载)在同一组 Handlers 上继续长;49 号票
// 的缩略端点用 PublicBaseURL 解析活跃驱动的公网基座。IPs 支撑上传的可选
// ip_id 存在性校验(50 号票);nil 时带 ip_id 的上传以 500 拒(装配缺口,
// 生产恒非 nil)。
type Handlers struct {
	Store Store
	// Storage backs preview/download for transferred artifacts; nil keeps
	// the legacy behavior (redirect to the stored URL / inline data: URI).
	Storage objectstore.Store
	// PublicBaseURL answers the active driver's public base (settings 注入,
	// 19 号票接缝);nil = 未接线,缩略端点恒回落 content。
	PublicBaseURL func(ctx context.Context) string
	// IPs backs the optional upload ip_id existence check (50 号票)。
	IPs IPExistenceChecker
}

// thumbnailEdge is the one-tier thumbnail semantic (49 号票):等比缩小到
// 768 限定框内、只缩不放、不转格式 —— 各云驱动据此拼各自的参数串。
const thumbnailEdge = 768

// thumbCacheMaxAge lets browsers reuse a thumb redirect briefly: the target
// (public base + object key + params) is stable until the driver flips,
// and the frontend onerror fallback covers a stale target anyway.
const thumbCacheMaxAge = 300

// RegisterContentRoutes mounts (relative to the group, which the binary
// mounts at /assets OUTSIDE the JWT group):
//
//	GET /:id/content — the artifact: storage bytes, 302 to its URL, or
//	                   inline for data URIs; ?download=1 sends attachment
//	GET /:id/thumb   — display-layer thumbnail: 302 to the cloud-processed
//	                   URL, or 302 back to content (49 号票,同款不挂 JWT)
//
// The content route deliberately sits OUTSIDE the JWT group: nodes preview
// artifacts with plain <img>/<video> tags, and media elements cannot attach
// an Authorization header. Artifacts are generated media — the same bytes
// the vendor's CDN serves publicly — so reading one by id is not a secret;
// every mutating or enumerating asset surface (below) stays behind auth,
// and the redirect is not an open redirect: the target is the stored URL of
// that row. thumb shares the same reasoning — it only ever redirects to the
// row's own object address or back to content.
func RegisterContentRoutes(group *gin.RouterGroup, h *Handlers) {
	group.GET("/:id/content", h.Content)
	group.GET("/:id/thumb", h.Thumb)
}

// RegisterLibraryRoutes mounts the library surfaces (relative to the group,
// which the binary mounts at /assets behind the JWT middleware):
//
//	GET    /        — list with kind / canvas_id filters (素材面板与管理页共用)
//	POST   /upload  — user-local media into the library (18 号票上传入口)
//	DELETE /:id     — remove one asset: object first, then the row
func RegisterLibraryRoutes(group *gin.RouterGroup, h *Handlers) {
	group.GET("", h.List)
	group.POST("/upload", h.Upload)
	group.DELETE("/:id", h.Delete)
}

// listJSON is the wire form of one list row: content_url 恒为内容寻址路径
// (预览与跨画布复用统一走它),url 仅排障时关心的原始地址;thumbnail_url
// 是显示层缩略的相对路径(49 号票,所有 kind 恒下发 —— 兜底语义收在
// thumb 端点内,前端零判空);last_used_at 是 47 号票「解引用即算」的时点,
// 前端挑选场景按它排最近使用.
type listJSON struct {
	ID           int64  `json:"id"`
	Kind         string `json:"kind"`
	CanvasID     int64  `json:"canvas_id"`
	CanvasName   string `json:"canvas_name"`
	TaskID       string `json:"task_id"`
	Model        string `json:"model"`
	Prompt       string `json:"prompt"`
	URL          string `json:"url"`
	ContentType  string `json:"content_type"`
	SizeBytes    int64  `json:"size_bytes"`
	ContentURL   string `json:"content_url"`
	ThumbnailURL string `json:"thumbnail_url"`
	// IP 归属(50 号票):ip_id=0 = 全局(直连产物与历史行),ip_name 是
	// 展示事实(join ips),前端按 ip_id==0 显示「全局」。
	IPID       int64  `json:"ip_id"`
	IPName     string `json:"ip_name"`
	CreatedAt  string `json:"created_at"`
	LastUsedAt string `json:"last_used_at"`
}

// Content serves the artifact for <img>/<video> previews and downloads.
// 转存过的素材直接流出自有字节;历史行回退旧契约 —— http(s) 地址 302 重
// 定向(厂商 CDN 交付字节),data: URI(厂商回 b64 的历史产物)解码内联,
// 因为浏览器拒绝重定向到 data: 位置。?download=1 加 attachment 头,管理
// 页与素材面板的下载按钮走同一入口。自有字节读不动但对象未必没了(26 号
// 票读兜底,服务端出网坏的实证形态)时,回落行内 http(s) 地址:预览 302
// 让浏览器直连,下载走代理。
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
			// 26 号票读兜底:Open 的错不是「对象不存在」(传输/凭证/5xx
			// 类,服务端出网坏的实证形态),且行内还留着 http(s) 出处时,
			// 预览 302 直连行内地址、下载代理流出——浏览器走用户自己的
			// 网络取字节,素材预览不随服务端出网瘫痪。
			if !errors.Is(err, fs.ErrNotExist) && isHTTPURL(a.URL) {
				log.Printf("asset: open object %q of asset %d failed, falling back to the row URL: %v",
					a.ObjectKey, id, err)
				h.serveRowURL(c, a, download)
				return
			}
			// 行在而对象不在(手工清理/存储故障),或行内没有可回退的
			// http(s) 地址:按缺失回答,节点显示占位而非报错——不把
			// 「对象没了」伪装成「网络坏了」。
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
		// 历史行没有自有字节:预览 302 由厂商 CDN 交付,attachment 套不
		// 上 302,下载入口退化为代理流出厂商字节。
		h.serveRowURL(c, a, download)
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

// Thumb serves the display-layer thumbnail as a pure 302 (49 号票):存储里
// 每张图仍只有原字节一份,缩略是「读时的 URL 变换」—— 云厂商原生图片
// 处理按活跃驱动拼参,不落第二份字节。语义矩阵:
//
//  1. 行不存在 → 404(同 content);
//  2. 图片行有 object_key 且活跃驱动支持处理、公网基座已配 → 302
//     public_base_url + object_key + 处理参数(带短 Cache-Control);
//  3. 其余一切(历史行、data: URI、local 驱动、非图片 kind 的防御分支、
//     公网基座未配)→ 302 到 content,content 的既有语义全量兜底。
//
// 与 content 同组、刻意不挂 JWT(10 号票同理由:<img> 带不了 Authorization
// 头);重定向目标恒为该行自身对象地址或 content,非开放重定向。已知降级
// 路径(不做存在性探测):驱动切换前仅存 local 的存量对象,云驱动活跃时
// 缩略 URL 打到云侧 404 —— 前端 onerror 回落兜底。
func (h *Handlers) Thumb(c *gin.Context) {
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

	if target, ok := h.thumbnailTarget(c.Request.Context(), a); ok {
		c.Header("Cache-Control", fmt.Sprintf("private, max-age=%d", thumbCacheMaxAge))
		c.Redirect(http.StatusFound, target)
		return
	}
	// content 的相对路径保持同形:dev 代理 / 部署 nginx / 桌面 webmux 三
	// 形态都把 /api 前缀导向画布服务。
	c.Redirect(http.StatusFound, contentPath(a.ID))
}

// thumbnailTarget answers the cloud-processed thumbnail URL when the row,
// driver, and public base all qualify (语义矩阵第 2 支);anything else
// answers false — the caller falls back to content. 拼接复用 PublicAddress
// 的「基座 + 对象键」规则(18 号票先例),不另写一份。
func (h *Handlers) thumbnailTarget(ctx context.Context, a Asset) (string, bool) {
	if a.Kind != KindImage || h.Storage == nil || h.PublicBaseURL == nil {
		return "", false
	}
	param := h.Storage.ThumbnailParam(ctx, thumbnailEdge)
	if param == "" {
		return "", false
	}
	base := strings.TrimSpace(h.PublicBaseURL(ctx))
	if base == "" {
		return "", false
	}
	addr, ok := PublicAddress(a, base)
	if !ok {
		return "", false
	}
	return addr + param, true
}

// isHTTPURL reports whether u is an absolute http(s) address — the only
// shape the 26 号票 read fallback may send a browser to.
func isHTTPURL(u string) bool {
	return strings.HasPrefix(u, "http://") || strings.HasPrefix(u, "https://")
}

// serveRowURL delivers an asset by its stored http(s) URL: preview 302 lets
// the browser fetch the bytes from its own network (vendor CDN for legacy
// rows, direct line during a server-egress outage for the 26 号票 fallback),
// download proxies them so the attachment header keeps one contract.
func (h *Handlers) serveRowURL(c *gin.Context, a Asset, download bool) {
	if download {
		h.proxyLegacy(c, a)
		return
	}
	c.Redirect(http.StatusFound, a.URL)
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

// List answers the library, newest first. kind 过滤取 image/video/audio
// 之外的值一律空结果而非报错 —— 前端的下拉里不会有别的值,宽度留给将来.
// sort = last_used 走最近使用序(47 号票挑选场景),缺省与其它值保持
// id DESC 的创建序(管理端审计视角不变);未知 sort 值显式 400,不静默
// 当缺省 —— 静默会让「想按最近使用排却拿到创建序」无从排查.
func (h *Handlers) List(c *gin.Context) {
	f := Filter{
		Kind:  strings.TrimSpace(c.Query("kind")),
		Limit: defaultListLimit,
	}
	if v := strings.TrimSpace(c.Query("sort")); v != "" {
		if v != SortLastUsed {
			apierr.InvalidRequest(c, "sort 仅支持 last_used")
			return
		}
		f.Sort = v
	}
	// IP 归属过滤(50 号票):scope=ip 须带 ip_id(语义 = 本 IP + 全局);
	// global 仅全局;all/缺省不过滤;未知值显式 400,不静默当缺省 —— 与
	// sort 同款纪律,静默会让「想按 IP 看却拿到全库」无从排查。
	if v := strings.TrimSpace(c.Query("scope")); v != "" {
		switch v {
		case ScopeIP, ScopeGlobal, ScopeAll:
			f.Scope = v
		default:
			apierr.InvalidRequest(c, "scope 仅支持 ip、global 或 all")
			return
		}
	}
	if v := c.Query("ip_id"); v != "" {
		id, err := strconv.ParseInt(v, 10, 64)
		if err != nil || id < 1 {
			apierr.InvalidRequest(c, "ip_id 必须是正整数")
			return
		}
		f.IPID = id
		if f.Scope == "" {
			f.Scope = ScopeIP
		}
	}
	if f.Scope == ScopeIP && f.IPID == 0 {
		apierr.InvalidRequest(c, "scope=ip 必须带 ip_id")
		return
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
		out = append(out, assetListJSON(row.Asset, row.CanvasName, row.IPName))
	}
	c.JSON(http.StatusOK, gin.H{"assets": out})
}

// assetListJSON renders one asset row in the library's wire shape; the list
// page and the upload response share it, so a freshly uploaded asset lands
// in the editor exactly like a listed one.
func assetListJSON(a Asset, canvasName, ipName string) listJSON {
	return listJSON{
		ID: a.ID, Kind: a.Kind, CanvasID: a.CanvasID,
		CanvasName: canvasName, TaskID: a.TaskID, Model: a.Model,
		Prompt:       a.Prompt,
		URL:          a.URL,
		ContentType:  a.ContentType,
		SizeBytes:    a.SizeBytes,
		ContentURL:   contentPath(a.ID),
		ThumbnailURL: thumbPath(a.ID),
		IPID:         a.IPID,
		IPName:       ipName,
		CreatedAt:    a.CreatedAt.Format(timeFormat),
		LastUsedAt:   a.LastUsedAt.Format(timeFormat),
	}
}

// contentPath is the canonical preview/reuse address of an asset. 画布前
// 端的 dev 代理与线上部署都把 /api 前缀导向画布服务,节点数据里存的就是
// 这个相对路径 —— 跨画布复用同一素材 = 引用同一路径。
func contentPath(id int64) string {
	return "/api/assets/" + strconv.FormatInt(id, 10) + "/content"
}

// thumbPath is the display-layer thumbnail address (49 号票):同一相对路径
// 形态,浏览器按 302 落地云处理 URL 或 content 兜底 —— object_key 与桶
// 地址继续不出 API。
func thumbPath(id int64) string {
	return "/api/assets/" + strconv.FormatInt(id, 10) + "/thumb"
}

// timeFormat 与前端的时间契约:RFC3339(JSON 生态的标准形状).
const timeFormat = time.RFC3339Nano

// Delete removes one asset: the object goes first, so a failed delete
// leaves the row in place for a retry and no orphaned bytes outlive the
// row. 历史行(无对象)直接删行;对象删除但行删除失败的窄缝里,预览按
// 「内容不可用」降级,不报错。48 号票删除防护:删行前按 object_key 数
// 兄弟行 —— 共享对象(上传复用/回填改指的结果)下 >0 只删行,其余行
// 预览不坏;=0 才物理删对象。计数失败按防护处理,不冒删共享字节的险。
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
		siblings, err := h.Store.CountOthersOnObjectKey(c.Request.Context(), a.ObjectKey, id)
		if err != nil {
			log.Printf("asset: count siblings on %q of asset %d: %v", a.ObjectKey, id, err)
			apierr.Internal(c, "素材文件删除失败,请稍后再试")
			return
		}
		if siblings == 0 {
			if err := h.Storage.Delete(c.Request.Context(), a.ObjectKey); err != nil {
				log.Printf("asset: delete object %q of asset %d: %v", a.ObjectKey, id, err)
				apierr.Internal(c, "素材文件删除失败,请稍后再试")
				return
			}
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
