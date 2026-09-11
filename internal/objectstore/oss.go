package objectstore

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"strings"

	"github.com/aliyun/aliyun-oss-go-sdk/oss"

	"github.com/gachal/InfiniteChance/internal/settings"
)

// OSS stores objects in one Aliyun OSS bucket through the native SDK
// (19 号票:S3 兼容层被否,原生 SDK 是一等公民)。Keys keep the same
// slash-separated archival layout the FileSystem driver writes, so
// switching drivers is a config flip, not a key migration.
type OSS struct {
	client *oss.Client
	bucket string
}

// NewOSS builds a driver over the settings row's OSS connection. The SDK
// client is lazy — credentials surface on first use, matching「连通性由
// 驱动使用时暴露」.
func NewOSS(cfg settings.OSSConfig) (*OSS, error) {
	return newOSS(cfg)
}

// newOSS is the testable core: clientOpts let the driver suite point the
// SDK at a local fake (ForcePathStyle), production always goes plain.
func newOSS(cfg settings.OSSConfig, clientOpts ...oss.ClientOption) (*OSS, error) {
	if !cfg.Complete() {
		return nil, fmt.Errorf("objectstore: oss connection incomplete")
	}
	client, err := oss.New(strings.TrimSpace(cfg.Endpoint),
		cfg.AccessKey, cfg.SecretKey, clientOpts...)
	if err != nil {
		return nil, fmt.Errorf("objectstore: oss client: %w", err)
	}
	return &OSS{client: client, bucket: strings.TrimSpace(cfg.Bucket)}, nil
}

func (s *OSS) bucketHandle() (*oss.Bucket, error) {
	b, err := s.client.Bucket(s.bucket)
	if err != nil {
		return nil, fmt.Errorf("objectstore: oss bucket %q: %w", s.bucket, err)
	}
	return b, nil
}

func (s *OSS) Put(ctx context.Context, key string, r io.Reader, size int64, contentType string) error {
	if !validKey(key) {
		return fmt.Errorf("objectstore: invalid key %q", key)
	}
	b, err := s.bucketHandle()
	if err != nil {
		return err
	}
	if err := b.PutObject(key, r,
		oss.ContentLength(size), oss.ContentType(contentType), oss.WithContext(ctx)); err != nil {
		return fmt.Errorf("objectstore: oss put %q: %w", key, err)
	}
	return nil
}

func (s *OSS) Open(ctx context.Context, key string) (io.ReadCloser, error) {
	if !validKey(key) {
		return nil, fmt.Errorf("objectstore: invalid key %q", key)
	}
	b, err := s.bucketHandle()
	if err != nil {
		return nil, err
	}
	body, err := b.GetObject(key, oss.WithContext(ctx))
	if isOSSNotFound(err) {
		return nil, fmt.Errorf("objectstore: object %q: %w", key, fs.ErrNotExist)
	}
	if err != nil {
		return nil, fmt.Errorf("objectstore: oss open %q: %w", key, err)
	}
	return body, nil
}

func (s *OSS) Delete(ctx context.Context, key string) error {
	if !validKey(key) {
		return fmt.Errorf("objectstore: invalid key %q", key)
	}
	b, err := s.bucketHandle()
	if err != nil {
		return err
	}
	// 删除不存在的键在 OSS 侧本就回答 204;映射兜底只为契约自洽
	// (Store 的 Delete 是幂等 no-op)。
	if err := b.DeleteObject(key, oss.WithContext(ctx)); err != nil && !isOSSNotFound(err) {
		return fmt.Errorf("objectstore: oss delete %q: %w", key, err)
	}
	return nil
}

// isOSSNotFound maps the SDK's not-found shapes onto the store contract:
// Open answers an fs.ErrNotExist-compatible error for unknown keys, which
// Dynamic uses as the「OSS 未命中 → 回退本地」signal.
func isOSSNotFound(err error) bool {
	if err == nil {
		return false
	}
	var se oss.ServiceError
	if errors.As(err, &se) {
		return se.StatusCode == http.StatusNotFound || se.Code == "NoSuchKey"
	}
	return false
}
