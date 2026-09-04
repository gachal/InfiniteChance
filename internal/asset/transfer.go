package asset

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"strings"
	"time"

	"github.com/gachal/InfiniteChance/internal/objectstore"
)

// Transfer bounds one artifact's trip into storage: 产物普遍几 MB 到十几 MB
// (视频分钟级生成物也在此量级),上限与超时都按宽裕取。超限的产物按转存
// 失败处理 —— 让任务行给出原因,而不是把截断的字节当产物入库。
const (
	maxTransferBytes = 256 << 20 // 256 MiB
	transferTimeout  = 5 * time.Minute
)

// Stored is one artifact's settled place in storage.
type Stored struct {
	Key         string
	ContentType string
	SizeBytes   int64
}

// Transfer archives one generated artifact into object storage: a data: URI
// is decoded in place, an http(s) URL is downloaded. The key files the
// object under its canvas and task (图片/视频按画布/任务归档); the content
// type is sanitized to bare mediatype. Callers finalize the task with the
// returned facts, so 素材落库与任务终态仍同事务.
func Transfer(ctx context.Context, st objectstore.Store, hc *http.Client,
	canvasID int64, taskID, kind, url string,
) (Stored, error) {
	if st == nil {
		return Stored{}, errors.New("asset: object storage not configured")
	}
	if hc == nil {
		hc = &http.Client{Timeout: transferTimeout}
	}

	var payload io.Reader
	var contentType string
	var size int64
	if data, ct, ok := splitDataURI(url); ok {
		payload = strings.NewReader(string(data))
		contentType = ct
		size = int64(len(data))
	} else {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return Stored{}, fmt.Errorf("产物地址无效: %w", err)
		}
		resp, err := hc.Do(req)
		if err != nil {
			return Stored{}, fmt.Errorf("产物下载失败: %w", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return Stored{}, fmt.Errorf("产物下载失败: HTTP %d", resp.StatusCode)
		}
		contentType = mediatype(resp.Header.Get("Content-Type"))
		body, err := io.ReadAll(io.LimitReader(resp.Body, maxTransferBytes+1))
		if err != nil {
			return Stored{}, fmt.Errorf("产物下载失败: %w", err)
		}
		if int64(len(body)) > maxTransferBytes {
			return Stored{}, fmt.Errorf("产物超过转存上限(%d MiB)", maxTransferBytes>>20)
		}
		payload = strings.NewReader(string(body))
		size = int64(len(body))
	}

	// 厂商偶尔给 application/octet-stream:没有信息量,按产物种类回退
	// 默认类型,预览与扩展名都不至于落到 .bin。
	if contentType == "" || contentType == "application/octet-stream" {
		contentType = defaultContentType(kind)
	}
	key := ObjectKey(canvasID, taskID, kind, contentType)
	if err := st.Put(ctx, key, payload, size, contentType); err != nil {
		return Stored{}, fmt.Errorf("产物转存失败: %w", err)
	}
	return Stored{Key: key, ContentType: contentType, SizeBytes: size}, nil
}

// ObjectKey builds the archival key for one artifact:
// canvases/{canvasID}/{taskID}/{kind}.{ext} — 按画布/任务归档的目录形状
// 直接落在卷上。
func ObjectKey(canvasID int64, taskID, kind, contentType string) string {
	ext := extensionOf(contentType)
	return fmt.Sprintf("canvases/%d/%s/%s%s", canvasID, taskID, kind, ext)
}

// mediatype strips parameters, keeping "image/png" from
// "image/png; charset=binary". Empty or unparseable input stays empty.
func mediatype(v string) string {
	if v == "" {
		return ""
	}
	ct, _, err := mime.ParseMediaType(v)
	if err != nil {
		return ""
	}
	return ct
}

// extensionOf maps the mediatypes generated media actually arrives in to a
// stable extension; unknown types file as .bin (预览不依赖扩展名,Content
// 路由回给的是 content_type).
func extensionOf(contentType string) string {
	sub := contentType
	if i := strings.Index(sub, "/"); i >= 0 {
		sub = sub[i+1:]
	}
	switch sub {
	case "png", "jpeg", "webp", "gif", "mp4", "webm", "mov":
		return "." + sub
	case "jpg":
		return ".jpeg"
	case "quicktime":
		return ".mov"
	case "x-msvideo":
		return ".avi"
	default:
		return ".bin"
	}
}

func defaultContentType(kind string) string {
	if kind == KindVideo {
		return "video/mp4"
	}
	return "image/png"
}

// splitDataURI decodes `data:<mime>;base64,<payload>` into its parts.
// (Content 路由与转存共用一个解析。)
func splitDataURI(url string) ([]byte, string, bool) {
	rest := strings.TrimPrefix(url, "data:")
	comma := strings.Index(rest, ",")
	if comma < 0 {
		return nil, "", false
	}
	meta, encoded := rest[:comma], rest[comma+1:]
	if !strings.HasSuffix(meta, ";base64") {
		return nil, "", false
	}
	mimeType := strings.TrimSuffix(meta, ";base64")
	if mimeType == "" {
		mimeType = "application/octet-stream"
	}
	payload, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return nil, "", false
	}
	return payload, mimeType, true
}
