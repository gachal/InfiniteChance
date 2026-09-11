// Package settings carries the runtime-editable dynamic configuration
// (19 号票): a KV table where each row is one named JSON document. The
// `storage` row picks the object-store driver (local|oss) and carries the
// OSS connection. 归属照 prompt_templates 先例 —— 网关挂管理 CRUD,
// canvas/server 同库只读、按请求读表即时生效;桌面版(SQLite 方言)settings
// 行缺省即 local,零配置不受影响。
package settings

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"
	"unicode/utf8"
)

// ErrNotFound reports a setting name that has no row.
var ErrNotFound = errors.New("settings: not found")

// Storage drivers (19 号票定案:local 为内置缺省,oss 走阿里云原生 SDK)。
const (
	DriverLocal = "local"
	DriverOSS   = "oss"
)

// NameStorage is the settings row that drives object storage.
const NameStorage = "storage"

// maxNameRunes matches the settings 表的 name 列宽(VARCHAR(64));行名是
// 代码里的常量而非用户输入,校验只为把越界拦在写入前而不是让 DB 报错。
const maxNameRunes = 64

// Setting is one KV row. Value is the raw JSON document — callers decode it
// into their own config shape.
type Setting struct {
	Name      string
	Value     json.RawMessage
	UpdatedAt time.Time
}

// Store persists settings rows. The gateway's admin surface writes; the
// canvas side reads the same shared table per request.
type Store interface {
	// Get returns the row or ErrNotFound.
	Get(ctx context.Context, name string) (Setting, error)
	// Put upserts one row's JSON value and returns it with UpdatedAt.
	Put(ctx context.Context, name string, value []byte) (Setting, error)
}

// StorageConfig is the decoded `storage` row: which driver new objects go
// to, plus the OSS connection when that driver is selected. Empty Driver
// (缺行/空文档)按 local 处理 —— 与桌面版零配置同一语义。
type StorageConfig struct {
	// Driver is local|oss; anything else reads as local (defensive: 行由
	// 管理 API 严校验后才入库,这里兜住手改库的坏值)。
	Driver string     `json:"driver"`
	OSS    *OSSConfig `json:"oss,omitempty"`
}

// OSSConfig is the Aliyun OSS connection (19 号票:原生 SDK,不走 S3 兼容
// 层)。AccessKey/SecretKey 只写不读 —— 管理 API 的响应仅带 has_* 提示与
// 尾 4 位,与渠道密钥同款。
type OSSConfig struct {
	Endpoint      string `json:"endpoint"`
	Bucket        string `json:"bucket"`
	PublicBaseURL string `json:"public_base_url"`
	AccessKey     string `json:"access_key"`
	SecretKey     string `json:"secret_key"`
}

// Complete reports whether the connection has everything a working OSS
// client needs. Incomplete rows never answer "oss" — Dynamic 降级 local,
// 素材面不因半行配置瘫痪。
func (c *OSSConfig) Complete() bool {
	return c != nil && c.Endpoint != "" && c.Bucket != "" &&
		c.AccessKey != "" && c.SecretKey != ""
}

// EffectiveDriver answers the driver Dynamic should honor: oss only when
// the row says so AND the connection is complete.
func (c StorageConfig) EffectiveDriver() string {
	if c.Driver == DriverOSS && c.OSS.Complete() {
		return DriverOSS
	}
	return DriverLocal
}

// DefaultStorageConfig is the zero-config shape: everything local, byte
// identical to pre-19 号票 behavior.
func DefaultStorageConfig() StorageConfig {
	return StorageConfig{Driver: DriverLocal}
}

// validateName keeps the row name inside the VARCHAR(64) column; names are
// package constants, so this only guards against future drift.
func validateName(name string) error {
	if name == "" || utf8.RuneCountInString(name) > maxNameRunes {
		return fmt.Errorf("settings: invalid name %q", name)
	}
	return nil
}

// withDefaultDriver reads an empty driver as local (缺行/空文档的宽容读取,
// 读侧两代入口 —— 读取器与管理 GET —— 共用)。
func withDefaultDriver(cfg StorageConfig) StorageConfig {
	if cfg.Driver == "" {
		cfg.Driver = DriverLocal
	}
	return cfg
}

// NormalizeStorageConfig trims the editable fields in place and validates
// the merged shape a fresh PUT must satisfy before it lands. Credentials
// are checked for completeness, not truth —— 连通性由驱动使用时暴露。
// 「留空 = 保留原密」由调用方先并进本结构,「启用 OSS 但没有可用连接」的
// 行才进不了库;driver=local 时 oss 块是惰性配置(切回本地不必清空连接),
// 只校验 public_base_url 的形状。
func NormalizeStorageConfig(c *StorageConfig) error {
	c.Driver = strings.TrimSpace(c.Driver)
	if c.Driver == "" {
		return fmt.Errorf("driver 不能为空")
	}
	if c.Driver != DriverLocal && c.Driver != DriverOSS {
		return fmt.Errorf("driver 必须是 local 或 oss")
	}
	if c.OSS == nil {
		if c.Driver == DriverOSS {
			return fmt.Errorf("启用 OSS 需要连接配置(endpoint/bucket/AccessKey/SecretKey)")
		}
		return nil
	}
	c.OSS.Endpoint = strings.TrimSpace(c.OSS.Endpoint)
	c.OSS.Bucket = strings.TrimSpace(c.OSS.Bucket)
	c.OSS.PublicBaseURL = strings.TrimSpace(c.OSS.PublicBaseURL)
	if c.Driver == DriverOSS && (c.OSS.Endpoint == "" || c.OSS.Bucket == "") {
		return fmt.Errorf("启用 OSS 需要 endpoint 与 bucket")
	}
	if c.Driver == DriverOSS && !c.OSS.Complete() {
		return fmt.Errorf("启用 OSS 需要完整的 AccessKey 与 SecretKey")
	}
	if c.OSS.PublicBaseURL != "" &&
		!strings.HasPrefix(c.OSS.PublicBaseURL, "http://") &&
		!strings.HasPrefix(c.OSS.PublicBaseURL, "https://") {
		return fmt.Errorf("public_base_url 必须是 http(s) 地址")
	}
	return nil
}

// StorageConfigReader answers the currently effective storage config; the
// wiring side injects one backed by the settings table. 调用量小,按请求
// 读表不加缓存(prompt_templates 先例)。
type StorageConfigReader func(ctx context.Context) (StorageConfig, error)

// NewStorageReader reads the `storage` row per call. 缺行/坏行按 local
// 缺省回答:与「未配置 = 本地卷」同一行为,坏 JSON 不至于让素材面瘫痪。
func NewStorageReader(s Store) StorageConfigReader {
	return func(ctx context.Context) (StorageConfig, error) {
		row, err := s.Get(ctx, NameStorage)
		if errors.Is(err, ErrNotFound) {
			return DefaultStorageConfig(), nil
		}
		if err != nil {
			return DefaultStorageConfig(), err
		}
		var cfg StorageConfig
		if err := json.Unmarshal(row.Value, &cfg); err != nil {
			log.Printf("settings: storage row undecodable, falling back to local: %v", err)
			return DefaultStorageConfig(), nil
		}
		return withDefaultDriver(cfg), nil
	}
}

// PublicBaseURL adapts the `storage` row to the 18 号票 provider seam: the
// public base of the object store, empty when unconfigured (解析链回落厂商
// 原址,行为与升级前一致)。nil Store keeps the pre-19 号票 behavior.
func PublicBaseURL(s Store) func(ctx context.Context) string {
	if s == nil {
		return nil
	}
	reader := NewStorageReader(s)
	return func(ctx context.Context) string {
		cfg, err := reader(ctx)
		if err != nil {
			log.Printf("settings: read storage for public base: %v", err)
			return ""
		}
		if cfg.OSS == nil {
			return ""
		}
		return strings.TrimSpace(cfg.OSS.PublicBaseURL)
	}
}
