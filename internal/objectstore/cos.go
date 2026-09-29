package objectstore

import (
	"context"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/url"
	"strings"

	cos "github.com/tencentyun/cos-go-sdk-v5"

	"github.com/gachal/InfiniteChance/internal/settings"
)

// COS stores objects in one Tencent COS bucket through the official SDK
// (23 号票:原生 SDK × N 路线,19 号票 OSS 先例延续)。Keys keep the same
// slash-separated archival layout the FileSystem/OSS drivers write, so
// switching drivers is a config flip, not a key migration.
type COS struct {
	client *cos.Client
}

// NewCOS builds a driver over the settings row's COS connection. The SDK
// client is lazy — credentials surface on first use, matching「连通性由
// 驱动使用时暴露」.
func NewCOS(cfg settings.COSConfig) (*COS, error) {
	return newCOS(cfg)
}

// newCOS is the testable core: bucketURL lets the driver suite point the
// SDK at a local fake, production always derives it from the config.
func newCOS(cfg settings.COSConfig, bucketURL ...*url.URL) (*COS, error) {
	if !cfg.Complete() {
		return nil, fmt.Errorf("objectstore: cos connection incomplete")
	}
	var u *url.URL
	if len(bucketURL) > 0 {
		u = bucketURL[0]
	} else {
		derived, err := cosBucketURL(cfg.Endpoint, cfg.Bucket)
		if err != nil {
			return nil, err
		}
		u = derived
	}
	client := cos.NewClient(&cos.BaseURL{BucketURL: u}, &http.Client{
		Transport: &cos.AuthorizationTransport{
			SecretID:  cfg.SecretID,
			SecretKey: cfg.SecretKey,
		},
	})
	return &COS{client: client}, nil
}

// cosBucketURL derives the SDK's bucket URL from the region endpoint and
// the APPID-suffixed bucket name:
// https://cos.ap-guangzhou.myqcloud.com + demo-1250000000 →
// https://demo-1250000000.cos.ap-guangzhou.myqcloud.com.
// 裸主机名(缺协议头)按 https 补齐,与 OSS 端点的宽容读取同款。
func cosBucketURL(endpoint, bucket string) (*url.URL, error) {
	e := strings.TrimSpace(endpoint)
	if e == "" {
		return nil, fmt.Errorf("objectstore: cos endpoint is empty")
	}
	if !strings.Contains(e, "://") {
		e = "https://" + e
	}
	u, err := url.Parse(e)
	if err != nil || u.Host == "" {
		return nil, fmt.Errorf("objectstore: cos endpoint %q is invalid", endpoint)
	}
	b := strings.TrimSpace(bucket)
	if b == "" {
		return nil, fmt.Errorf("objectstore: cos bucket is empty")
	}
	u.Host = b + "." + u.Host
	u.Path = ""
	return u, nil
}

func (s *COS) Put(ctx context.Context, key string, r io.Reader, size int64, contentType string) error {
	if !validKey(key) {
		return fmt.Errorf("objectstore: invalid key %q", key)
	}
	// ContentLength 显式给:转存/上传的 reader 是 *bytes.Reader 时 SDK 能
	// 自推,但接口契约不保证调用方形态,长度我们自己就有。
	_, err := s.client.Object.Put(ctx, key, r, &cos.ObjectPutOptions{
		ObjectPutHeaderOptions: &cos.ObjectPutHeaderOptions{
			ContentType:   contentType,
			ContentLength: size,
		},
	})
	if err != nil {
		return fmt.Errorf("objectstore: cos put %q: %w", key, err)
	}
	return nil
}

func (s *COS) Open(ctx context.Context, key string) (io.ReadCloser, error) {
	if !validKey(key) {
		return nil, fmt.Errorf("objectstore: invalid key %q", key)
	}
	resp, err := s.client.Object.Get(ctx, key, nil)
	if isCOSNotFound(err) {
		return nil, fmt.Errorf("objectstore: object %q: %w", key, fs.ErrNotExist)
	}
	if err != nil {
		return nil, fmt.Errorf("objectstore: cos open %q: %w", key, err)
	}
	return resp.Body, nil
}

func (s *COS) Delete(ctx context.Context, key string) error {
	if !validKey(key) {
		return fmt.Errorf("objectstore: invalid key %q", key)
	}
	// COS 对不存在的键也回答 2xx(DeleteObject 的幂等语义);映射兜底
	// 只为契约自洽(Store 的 Delete 是幂等 no-op)。
	_, err := s.client.Object.Delete(ctx, key, nil)
	if err != nil && !isCOSNotFound(err) {
		return fmt.Errorf("objectstore: cos delete %q: %w", key, err)
	}
	return nil
}

// isCOSNotFound maps the SDK's not-found shapes onto the store contract:
// Open answers an fs.ErrNotExist-compatible error for unknown keys, which
// Dynamic uses as the「云未命中 → 回退本地」signal。SDK 的 IsNotFoundError
// 判 404(RetryError 穿透取末次),NoSuchKey 是 belt-and-braces。
func isCOSNotFound(err error) bool {
	if err == nil {
		return false
	}
	if cos.IsNotFoundError(err) {
		return true
	}
	if er, ok := cos.IsCOSError(err); ok {
		return er.Code == "NoSuchKey"
	}
	return false
}
