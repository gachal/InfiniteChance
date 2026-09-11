package asset

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"mime/multipart"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/gachal/InfiniteChance/internal/apierr"
)

// maxUploadBytes 与转存上限同规(transfer.go 的 maxTransferBytes):上传
// 素材与任务产物都是用户媒体,上限一致才好解释。包级变量让测试能调小
// 验证超限分支,生产行为仍是 256 MiB。
var maxUploadBytes = int64(maxTransferBytes)

// multipartOverhead 给 multipart 编码的边界、字段名等非文件字节留的余
// 量:请求体上限按「文件上限 + 余量」收口,超限请求在解析阶段就被掐断,
// 不会整份落进临时盘。
const multipartOverhead = 1 << 20

// uploadFormMemory 与 gin 引擎的 MaxMultipartMemory 缺省同值:multipart
// 解析时驻留内存的字节上限,超出部分落临时盘。
const uploadFormMemory = 32 << 20

// sniffHeadBytes is how many leading bytes the content sniffer reads: every
// signature we recognize (PNG/JPEG/GIF/RIFF/EBML/ISO-BMFF) lives well inside
// the first 512 bytes.
const sniffHeadBytes = 512

// Upload is the creator's upload entry (18 号票): user-local images and
// videos land in the asset library through their own bytes — 素材不再只由
// 生成 worker 落库。The file is archived into object storage under
// uploads/{yyyymmdd}/{uuid}.{ext} (与任务产物键 canvases/… 区分归档语义:
// 用户上传独立于任何画布/任务), and the row keeps url empty — 该列的语义
// 是「厂商 http(s) 原地址」,上传素材没有厂商出处。kind 声明意图(image/
// video),真正的类型由魔数嗅探裁决,客户端声明一律不受信。
func (h *Handlers) Upload(c *gin.Context) {
	if h.Storage == nil {
		apierr.Write(c, http.StatusServiceUnavailable, "storage_unconfigured",
			"素材存储未配置,暂时不能上传")
		return
	}

	// 请求体按文件上限收口,且必须发生在任何 multipart 解析之前 ——
	// PostForm/FormFile 都会触发整份解析(超内存部分落临时盘),晚包
	// 就拦不住超限上传。显式解析一次:PostForm 会吞掉解析错误,超限
	// 请求要在这里拿到真实原因再回答。
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxUploadBytes+multipartOverhead)
	if err := c.Request.ParseMultipartForm(uploadFormMemory); err != nil {
		if isOversizeParseError(err) {
			apierr.Write(c, http.StatusBadRequest, "file_too_large",
				fmt.Sprintf("文件超过上传上限(%d MiB)", maxUploadBytes>>20))
			return
		}
		apierr.InvalidRequest(c, "上传必须是 multipart 表单,含 file 文件与 kind 字段")
		return
	}

	kind := strings.TrimSpace(c.PostForm("kind"))
	if kind != KindImage && kind != KindVideo {
		apierr.InvalidRequest(c, "kind 必须是 image 或 video")
		return
	}

	fh, err := c.FormFile("file")
	if err != nil {
		apierr.InvalidRequest(c, "上传必须是 multipart 表单,含 file 文件与 kind 字段")
		return
	}
	if fh.Size > maxUploadBytes {
		apierr.Write(c, http.StatusBadRequest, "file_too_large",
			fmt.Sprintf("文件超过上传上限(%d MiB)", maxUploadBytes>>20))
		return
	}

	f, err := fh.Open()
	if err != nil {
		log.Printf("asset: open upload: %v", err)
		apierr.Internal(c, "服务内部错误,请稍后再试")
		return
	}
	defer f.Close()

	contentType, err := sniffUploadedFile(f, fh.Filename, kind)
	if err != nil {
		apierr.Write(c, http.StatusBadRequest, "file_type_mismatch", err.Error())
		return
	}

	key := uploadKey(time.Now(), contentType)
	if err := h.Storage.Put(c.Request.Context(), key, f, fh.Size, contentType); err != nil {
		log.Printf("asset: put upload %q: %v", key, err)
		apierr.Internal(c, "素材保存失败,请稍后再试")
		return
	}

	created, err := h.Store.Create(c.Request.Context(), Asset{
		Kind:        kind,
		URL:         "", // 上传素材没有厂商出处,url 列语义是厂商原地址
		ObjectKey:   key,
		ContentType: contentType,
		SizeBytes:   fh.Size,
	})
	if err != nil {
		// 行没落成就把已写字节回收:不留孤儿对象(Delete 幂等,no-op 安全)。
		if delErr := h.Storage.Delete(context.WithoutCancel(c.Request.Context()), key); delErr != nil {
			log.Printf("asset: cleanup upload %q after row failure: %v", key, delErr)
		}
		log.Printf("asset: create upload row: %v", err)
		apierr.Internal(c, "服务内部错误,请稍后再试")
		return
	}
	c.JSON(http.StatusCreated, gin.H{"asset": assetListJSON(created, "")})
}

// isOversizeParseError recognizes the failure MaxBytesReader injects when the
// body blew the cap mid-parse (multipart readers surface it wrapped).
func isOversizeParseError(err error) bool {
	var maxErr *http.MaxBytesError
	return errors.As(err, &maxErr)
}

// uploadKey lays an uploaded object under its upload date:
// uploads/{yyyymmdd}/{uuid}.{ext} — 按天归档、uuid 防碰撞与猜测;日期取
// UTC(与用量日志按天汇总同一约定)。
func uploadKey(now time.Time, contentType string) string {
	return fmt.Sprintf("uploads/%s/%s%s", now.UTC().Format("20060102"), uuid.NewString(), extensionOf(contentType))
}

// sniffUploadedFile decides the file's true content type from its leading
// magic bytes, cross-checked against the filename extension, and verifies the
// sniffed family matches the declared kind. f is rewound to its start before
// returning so the caller streams the whole file.
func sniffUploadedFile(f multipart.File, filename, kind string) (string, error) {
	head := make([]byte, sniffHeadBytes)
	n, err := io.ReadFull(f, head)
	if err != nil && !errors.Is(err, io.ErrUnexpectedEOF) && !errors.Is(err, io.EOF) {
		return "", errors.New("文件读取失败,请重试")
	}
	head = head[:n]
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return "", errors.New("文件读取失败,请重试")
	}

	contentType, ok := sniffMagic(head)
	if !ok {
		return "", errors.New("无法识别的文件类型,支持 png/jpeg/webp/gif 图片与 mp4/webm/mov/avi 视频")
	}
	if extType, ok := extensionContentType(filepath.Ext(filename)); ok && extType != contentType {
		return "", fmt.Errorf("文件扩展名与实际内容不符(扩展名是 %s,内容是 %s)", filepath.Ext(filename), contentType)
	}
	// kind 已在入口收敛为 image/video,直接比对嗅探出的家族。
	if familyOf(contentType) != kind {
		return "", fmt.Errorf("文件内容是%s,与声明的 kind=%s 不符", familyOf(contentType), kind)
	}
	return contentType, nil
}

// sniffMagic maps leading magic bytes to the media types uploads accept.
// 刻意自家实现而不是 http.DetectContentType:它认不全 ISO-BMFF 的品牌
// (qt/avc1/dash 等都漏)也不分 webp/avi 的 RIFF 载荷,上传白名单要的是
// 精确裁决。
func sniffMagic(head []byte) (string, bool) {
	switch {
	case bytes.HasPrefix(head, []byte("\x89PNG\r\n\x1a\n")):
		return "image/png", true
	case bytes.HasPrefix(head, []byte("\xff\xd8\xff")):
		return "image/jpeg", true
	case bytes.HasPrefix(head, []byte("GIF87a")), bytes.HasPrefix(head, []byte("GIF89a")):
		return "image/gif", true
	case bytes.HasPrefix(head, []byte("\x1a\x45\xdf\xa3")):
		return "video/webm", true
	}
	if bytes.HasPrefix(head, []byte("RIFF")) && len(head) >= 12 {
		switch string(head[8:12]) {
		case "WEBP":
			return "image/webp", true
		case "AVI ":
			return "video/x-msvideo", true
		}
	}
	// ISO-BMFF(mp4/mov):[4:8] 是 "ftyp",[8:12] 是品牌;qt 系品牌落
	// quicktime,其余品牌(isom/mp41/mp42/avc1/dash/…)统一按 mp4。
	if len(head) >= 12 && string(head[4:8]) == "ftyp" {
		if brand := string(head[8:12]); strings.HasPrefix(brand, "qt") {
			return "video/quicktime", true
		}
		return "video/mp4", true
	}
	return "", false
}

// uploadableMedia 是上传白名单的正典表:扩展名 ↔ 媒体类型。扩展名侧的
// 佐证查询(extensionContentType)从它派生;键扩展名经 transfer.go 的
// extensionOf(类型→扩展名)给出 —— 两张方向表由
// TestUploadableMediaTablesAgree 锁定一致,新增格式只改这一处。
var uploadableMedia = []struct {
	ext string
	typ string
}{
	{".png", "image/png"},
	{".jpg", "image/jpeg"},
	{".jpeg", "image/jpeg"},
	{".webp", "image/webp"},
	{".gif", "image/gif"},
	{".mp4", "video/mp4"},
	{".m4v", "video/mp4"},
	{".webm", "video/webm"},
	{".mov", "video/quicktime"},
	{".avi", "video/x-msvideo"},
}

var uploadableByExt = func() map[string]string {
	m := make(map[string]string, len(uploadableMedia))
	for _, e := range uploadableMedia {
		m[e.ext] = e.typ
	}
	return m
}()

// extensionContentType maps a filename extension to the media type uploads
// accept; the second return is false for unknown or absent extensions (魔数
// 是权威,扩展名只做一致性佐证,未知扩展名不单独定罪).
func extensionContentType(ext string) (string, bool) {
	t, ok := uploadableByExt[strings.ToLower(ext)]
	return t, ok
}

// familyOf answers whether a media type is the image or the video family.
func familyOf(contentType string) string {
	if strings.HasPrefix(contentType, "video/") {
		return KindVideo
	}
	return KindImage
}
