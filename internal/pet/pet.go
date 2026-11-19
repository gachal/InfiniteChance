// Package pet 承载桌宠(92 号票)的服务端两面:
//
//   - 网关 admin 面:形象上传端点(与 settings 的 /admin/settings/pet CRUD
//     同组挂载)。形象是系统资产 —— 只落 objectstore `uploads/` 前缀,不落
//     assets 行(创作素材库,票面明令禁止),配置存对象键。
//   - 画布面:配置读端点(canvas-web 拉取 pet 配置,settings 只读 reader
//     按请求读表热生效)与对象读端点 —— <img> 带不了 Authorization 头,
//     照 `GET /assets/{id}/content` 先例**故意不挂鉴权**(AGENTS 硬性约束
//     5 载明理由);防遍历纪律 = 路径只收桌宠配置登记过的对象键。
//
// 本包不 import settings 的 handler(配置 CRUD 在 settings 包),只吃其
// PetConfigReader;asset 只取导出的嗅探/扩展名薄包装(单一事实源,不复制
// 魔数表)。
package pet

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/gachal/InfiniteChance/internal/apierr"
	"github.com/gachal/InfiniteChance/internal/asset"
	"github.com/gachal/InfiniteChance/internal/objectstore"
	"github.com/gachal/InfiniteChance/internal/settings"
)

// maxImageBytes 是单张桌宠形象的上限:动图 GIF 体积可观,20 MiB 覆盖常规
// 素材包;包级变量让测试能调小验证超限分支(asset.maxUploadBytes 同款)。
var maxImageBytes = int64(20 << 20)

// multipartOverhead 给 multipart 编码的边界字节留余量(照 asset 上传纪律:
// MaxBytesReader 必须在任何 multipart 解析之前挂,超限请求在解析期被掐断,
// 不整份落临时盘)。
const multipartOverhead = 1 << 20

// uploadFormMemory 与 gin 引擎缺省同值(照 asset 上传纪律)。
const uploadFormMemory = 32 << 20

// sniffHeadBytes is how many leading bytes the sniffer reads(asset 同值:
// 全部已识别签名都在首 512 字节内)。
const sniffHeadBytes = 512

// objectCacheMaxAge 桌宠对象的浏览器缓存秒数:键是每次上传新生成的 uuid,
// 内容不可变,读端点可以放心让浏览器短缓存;3600 内换形象走新键不受影响。
const objectCacheMaxAge = 3600

// Handlers serves the pet surface. Storage 缺装配(nil)= 上传/读端点以
// storage_unconfigured / 500 拒绝,服务照常启动;Config 缺装配 = 配置读
// 端点 500(与各 store 同哲学,生产装配恒非 nil)。
type Handlers struct {
	// Config answers the current pet config per request(热生效)。
	Config settings.PetConfigReader
	// Storage receives pet images and serves object reads。
	Storage objectstore.Store
}

// RegisterAdminRoutes mounts on the gateway admin group(settings CRUD 同组):
//
//	POST /settings/pet/image — multipart 字段 file;GIF/WebP/PNG(APNG)
//	                         魔数嗅探裁决,落 uploads/{yyyymmdd}/{uuid}.{ext}
func RegisterAdminRoutes(group *gin.RouterGroup, h *Handlers) {
	group.POST("/settings/pet/image", h.UploadImage)
}

// RegisterConfigRoutes mounts on the canvas JWT group(group 挂 /pet):
//
//	GET /config — the current pet config for canvas-web(热生效)。
func RegisterConfigRoutes(group *gin.RouterGroup, h *Handlers) {
	group.GET("/config", h.GetConfig)
}

// RegisterObjectRoutes mounts the deliberately unauthenticated object read
// route(group 挂 /pet):
//
//	GET /object?key=… — 对象字节,只认桌宠配置登记过的键(防遍历)。
//
// 不挂 JWT 与 /assets/:id/content 同一理由:<img> 标签带不了 Authorization
// 头;键只出配置登记过的两个值,枚举面(admin CRUD/上传)照常挂鉴权。
func RegisterObjectRoutes(group *gin.RouterGroup, h *Handlers) {
	group.GET("/object", h.Object)
}

// petInfoJSON is the wire form of the pet config on the canvas side.
// Chips 恒为数组(空配置回 [] 不回 null,前端零判空)。
type petInfoJSON struct {
	Name             string   `json:"name"`
	Greeting         string   `json:"greeting"`
	Chips            []string `json:"chips"`
	Model            string   `json:"model"`
	IdleImage        string   `json:"idle_image"`
	InteractiveImage string   `json:"interactive_image"`
}

func toPetInfoJSON(cfg settings.PetConfig) petInfoJSON {
	chips := cfg.Chips
	if chips == nil {
		chips = []string{}
	}
	return petInfoJSON{
		Name:             cfg.Name,
		Greeting:         cfg.Greeting,
		Chips:            chips,
		Model:            cfg.Model,
		IdleImage:        cfg.IdleImage,
		InteractiveImage: cfg.InteractiveImage,
	}
}

// UploadImage archives one pet image into object storage under the uploads/
// prefix and answers the object key — 配置存键,URL 由读端点按登记键派生。
// 类型由魔数嗅探裁决(客户端声明不受信),白名单只收动图三族:GIF/WebP/
// PNG(APNG 随 PNG 嗅探天然可用);JPEG 是静态图,不收。
func (h *Handlers) UploadImage(c *gin.Context) {
	if h.Storage == nil {
		apierr.Write(c, http.StatusServiceUnavailable, "storage_unconfigured",
			"对象存储未配置,暂时不能上传桌宠形象")
		return
	}

	// 请求体按文件上限收口,且必须发生在任何 multipart 解析之前(照
	// asset 上传纪律:PostForm/FormFile 都会触发整份解析)。
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxImageBytes+multipartOverhead)
	if err := c.Request.ParseMultipartForm(uploadFormMemory); err != nil {
		if isOversizeParseError(err) {
			apierr.Write(c, http.StatusBadRequest, "file_too_large",
				fmt.Sprintf("文件超过上传上限(%d MiB)", maxImageBytes>>20))
			return
		}
		apierr.InvalidRequest(c, "上传必须是 multipart 表单,含 file 文件字段")
		return
	}

	fh, err := c.FormFile("file")
	if err != nil {
		apierr.InvalidRequest(c, "上传必须是 multipart 表单,含 file 文件字段")
		return
	}
	if fh.Size > maxImageBytes {
		apierr.Write(c, http.StatusBadRequest, "file_too_large",
			fmt.Sprintf("文件超过上传上限(%d MiB)", maxImageBytes>>20))
		return
	}

	f, err := fh.Open()
	if err != nil {
		log.Printf("pet: open upload: %v", err)
		apierr.Internal(c, "服务内部错误,请稍后再试")
		return
	}
	defer f.Close()

	head := make([]byte, sniffHeadBytes)
	n, err := io.ReadFull(f, head)
	if err != nil && !errors.Is(err, io.ErrUnexpectedEOF) && !errors.Is(err, io.EOF) {
		log.Printf("pet: read upload head: %v", err)
		apierr.InvalidRequest(c, "文件读取失败,请重试")
		return
	}
	contentType, ok := asset.SniffMagic(head[:n])
	if !ok || !petImageWhitelist(contentType) {
		apierr.Write(c, http.StatusBadRequest, "file_type_mismatch",
			"桌宠形象只接受 GIF / WebP / PNG(APNG)动图")
		return
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		log.Printf("pet: rewind upload: %v", err)
		apierr.InvalidRequest(c, "文件读取失败,请重试")
		return
	}

	key := petImageKey(time.Now(), contentType)
	if err := h.Storage.Put(c.Request.Context(), key, f, fh.Size, contentType); err != nil {
		log.Printf("pet: put image %q: %v", key, err)
		apierr.Internal(c, "形象保存失败,请稍后再试")
		return
	}
	// 没有行可落(桌宠形象不进 assets 库),Put 成功即成功;唯一残留是
	// 管理员上传后不保存配置的孤儿对象 —— 键随 upload 响应返回,下次
	// 上传自然换新键,uploads/ 孤儿可接受。
	c.JSON(http.StatusCreated, gin.H{"image": gin.H{
		"key":          key,
		"content_type": contentType,
		"size_bytes":   fh.Size,
	}})
}

// petImageWhitelist answers whether the sniffed type is an accepted pet
// image family: GIF / WebP / PNG(APNG 即 PNG)。
func petImageWhitelist(contentType string) bool {
	switch contentType {
	case "image/gif", "image/webp", "image/png":
		return true
	}
	return false
}

// petImageKey lays a pet image under its upload date,同一键格式先例
// (asset.uploadKey):uploads/{yyyymmdd}/{uuid}.{ext} —— 按天归档、uuid
// 防碰撞与猜测,扩展名随嗅探出的内容类型落(asset.ExtensionOf 同表)。
func petImageKey(now time.Time, contentType string) string {
	return fmt.Sprintf("uploads/%s/%s%s", now.UTC().Format("20060102"), uuid.NewString(), asset.ExtensionOf(contentType))
}

// isOversizeParseError recognizes the failure MaxBytesReader injects when
// the body blew the cap mid-parse(asset 同款三行 helper,机械复制不涉
// 魔数表的单一事实源纪律)。
func isOversizeParseError(err error) bool {
	var maxErr *http.MaxBytesError
	return errors.As(err, &maxErr)
}

// GetConfig answers the current pet config for canvas-web(热生效:reader
// 按请求读 settings 表)。缺行/坏行由 reader 回缺省,配置读不出来时前端
// 走 CSS 缺省形象,不瘫痪。
func (h *Handlers) GetConfig(c *gin.Context) {
	if h.Config == nil {
		log.Printf("pet: config reader not wired")
		apierr.Internal(c, "服务内部错误,请稍后再试")
		return
	}
	cfg, err := h.Config(c.Request.Context())
	if err != nil {
		log.Printf("pet: read config: %v", err)
		apierr.Internal(c, "服务内部错误,请稍后再试")
		return
	}
	c.JSON(http.StatusOK, gin.H{"pet": toPetInfoJSON(cfg)})
}

// Object serves a pet image's bytes for <img> previews. 故意不挂鉴权
// (RegisterObjectRoutes 注释载明理由);防遍历 = 键必须与当前配置登记的
// idle_image / interactive_image 之一精确相等 —— 不是任意 uploads/ 键都
// 放行,改配置即刻生效(旧键下一请求即 404)。
func (h *Handlers) Object(c *gin.Context) {
	key := strings.TrimSpace(c.Query("key"))
	if key == "" {
		apierr.InvalidRequest(c, "缺少 key 参数")
		return
	}
	if h.Config == nil {
		log.Printf("pet: config reader not wired")
		apierr.Internal(c, "服务内部错误,请稍后再试")
		return
	}
	if h.Storage == nil {
		apierr.Write(c, http.StatusServiceUnavailable, "storage_unconfigured",
			"对象存储未配置,桌宠形象不可用")
		return
	}
	cfg, err := h.Config(c.Request.Context())
	if err != nil {
		log.Printf("pet: read config for object: %v", err)
		apierr.Internal(c, "服务内部错误,请稍后再试")
		return
	}
	registered := cfg.IdleImage == key || cfg.InteractiveImage == key
	if !registered {
		// 404 而非 403:不区分「未登记」与「不存在」,不给遍历探针回声。
		apierr.NotFound(c, "桌宠形象不存在")
		return
	}

	obj, err := h.Storage.Open(c.Request.Context(), key)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			apierr.NotFound(c, "桌宠形象不存在")
			return
		}
		log.Printf("pet: open object %q: %v", key, err)
		apierr.Internal(c, "服务内部错误,请稍后再试")
		return
	}
	defer obj.Close()

	// 内容类型再嗅探一次(读侧权威与写侧同一张魔数表):不信任键的
	// 扩展名,也不把 content_type 冗余进配置。
	head := make([]byte, sniffHeadBytes)
	n, err := io.ReadFull(obj, head)
	if err != nil && !errors.Is(err, io.ErrUnexpectedEOF) && !errors.Is(err, io.EOF) {
		log.Printf("pet: sniff object %q: %v", key, err)
		apierr.Internal(c, "服务内部错误,请稍后再试")
		return
	}
	contentType := "application/octet-stream"
	if ct, ok := asset.SniffMagic(head[:n]); ok {
		contentType = ct
	}

	c.Header("Content-Type", contentType)
	c.Header("Cache-Control", fmt.Sprintf("private, max-age=%d", objectCacheMaxAge))
	c.Status(http.StatusOK)
	if n > 0 {
		if _, err := c.Writer.Write(head[:n]); err != nil {
			return
		}
	}
	_, _ = io.Copy(c.Writer, obj)
}

// 动态兑现 Store 接缝(编译期对账,与 objectstore 包同款纪律)。
var _ objectstore.Store = (*objectstore.Dynamic)(nil)
