package settings

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/gachal/InfiniteChance/internal/apierr"
)

// Handlers serves the admin settings endpoints. The gateway mounts them
// under /admin behind the JWT session middleware (19 号票).
type Handlers struct {
	Store Store
}

// RegisterAdminRoutes mounts:
//
//	GET /admin/settings/storage — the stored storage config (secrets never
//	                             included; has_* + tail-4 hints only)
//	PUT /admin/settings/storage — replace it; empty access_key/secret_key
//	                             keep the stored values
func RegisterAdminRoutes(group *gin.RouterGroup, h *Handlers) {
	group.GET("/settings/storage", h.GetStorage)
	group.PUT("/settings/storage", h.PutStorage)
}

// ossJSON is the wire form of the OSS block. The credentials are never
// serialized — has_access_key/has_secret plus tail-4 hints let the admin
// see that values are stored (渠道密钥同款).
type ossJSON struct {
	Endpoint      string `json:"endpoint"`
	Bucket        string `json:"bucket"`
	PublicBaseURL string `json:"public_base_url"`
	HasAccessKey  bool   `json:"has_access_key"`
	AccessKeyHint string `json:"access_key_hint,omitempty"`
	HasSecret     bool   `json:"has_secret"`
	SecretHint    string `json:"secret_hint,omitempty"`
}

// cosJSON is the wire form of the COS block (23 号票):字段名沿用腾讯控制
// 台词汇,密钥同款只写不读。
type cosJSON struct {
	Endpoint      string `json:"endpoint"`
	Bucket        string `json:"bucket"`
	PublicBaseURL string `json:"public_base_url"`
	HasSecretID   bool   `json:"has_secret_id"`
	SecretIDHint  string `json:"secret_id_hint,omitempty"`
	HasSecretKey  bool   `json:"has_secret_key"`
	SecretKeyHint string `json:"secret_key_hint,omitempty"`
}

// storageJSON is the wire form of the storage setting. updated_at is empty
// when the row has never been saved (零配置起步的 GET 也能直接渲染表单).
type storageJSON struct {
	Driver       string  `json:"driver"`
	RelayPersist bool    `json:"relay_persist"`
	OSS          ossJSON `json:"oss"`
	COS          cosJSON `json:"cos"`
	UpdatedAt    string  `json:"updated_at"`
}

// hintRunes is how many trailing runes of a stored credential the response
// leaks — enough to tell two configs apart, nothing more.
const hintRunes = 4

func tailHint(v string) string {
	runes := []rune(v)
	if len(runes) > hintRunes {
		runes = runes[len(runes)-hintRunes:]
	}
	return "…" + string(runes)
}

func toStorageJSON(cfg StorageConfig, updatedAt string) storageJSON {
	out := storageJSON{Driver: cfg.Driver, RelayPersist: cfg.RelayPersist, UpdatedAt: updatedAt}
	if cfg.OSS != nil {
		out.OSS.Endpoint = cfg.OSS.Endpoint
		out.OSS.Bucket = cfg.OSS.Bucket
		out.OSS.PublicBaseURL = cfg.OSS.PublicBaseURL
		if cfg.OSS.AccessKey != "" {
			out.OSS.HasAccessKey = true
			out.OSS.AccessKeyHint = tailHint(cfg.OSS.AccessKey)
		}
		if cfg.OSS.SecretKey != "" {
			out.OSS.HasSecret = true
			out.OSS.SecretHint = tailHint(cfg.OSS.SecretKey)
		}
	}
	if cfg.COS != nil {
		out.COS.Endpoint = cfg.COS.Endpoint
		out.COS.Bucket = cfg.COS.Bucket
		out.COS.PublicBaseURL = cfg.COS.PublicBaseURL
		if cfg.COS.SecretID != "" {
			out.COS.HasSecretID = true
			out.COS.SecretIDHint = tailHint(cfg.COS.SecretID)
		}
		if cfg.COS.SecretKey != "" {
			out.COS.HasSecretKey = true
			out.COS.SecretKeyHint = tailHint(cfg.COS.SecretKey)
		}
	}
	return out
}

// storageInputJSON is the PUT body. Empty access_key/secret_key keep the
// stored credentials; every other field replaces. relay_persist 缺省(nil)
// 沿用已存值 —— 一次只切驱动的 PUT 不顺手关掉直连转存。
type storageInputJSON struct {
	Driver       string           `json:"driver"`
	RelayPersist *bool            `json:"relay_persist"`
	OSS          *storageOSSInput `json:"oss"`
	COS          *storageCOSInput `json:"cos"`
}

type storageOSSInput struct {
	Endpoint      string `json:"endpoint"`
	Bucket        string `json:"bucket"`
	PublicBaseURL string `json:"public_base_url"`
	AccessKey     string `json:"access_key"`
	SecretKey     string `json:"secret_key"`
}

type storageCOSInput struct {
	Endpoint      string `json:"endpoint"`
	Bucket        string `json:"bucket"`
	PublicBaseURL string `json:"public_base_url"`
	SecretID      string `json:"secret_id"`
	SecretKey     string `json:"secret_key"`
}

// GetStorage answers the stored storage config. 缺行回答 local 缺省而非
// 404:管理页第一次打开就要能渲染表单。
func (h *Handlers) GetStorage(c *gin.Context) {
	cfg, updatedAt, err := h.storedConfig(c.Request.Context())
	if err != nil {
		h.failInternal(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"storage": toStorageJSON(cfg, updatedAt)})
}

// PutStorage replaces the storage config. 半套密钥没有意义:access_key 与
// secret_key 要么都给新的,要么都留空沿用已存的一对。
func (h *Handlers) PutStorage(c *gin.Context) {
	var raw storageInputJSON
	if err := c.ShouldBindJSON(&raw); err != nil {
		apierr.InvalidRequest(c, "请求体不是合法的存储配置 JSON")
		return
	}

	// 既有行先读出来做空字段合并;行坏了让管理员看见 500 而不是静默覆盖。
	stored, _, err := h.storedConfig(c.Request.Context())
	if err != nil {
		h.failInternal(c, err)
		return
	}
	cfg := StorageConfig{Driver: raw.Driver, RelayPersist: stored.RelayPersist}
	if raw.RelayPersist != nil {
		cfg.RelayPersist = *raw.RelayPersist
	} else if cfg.Driver == DriverLocal && stored.RelayPersist {
		// 缺省沿用撞上「行不允许 local+persist」:显式送 true 才 400,缺省
		// 继承时自动关掉 —— 切回本地卷永远是一次 PUT 的事,不让管理员
		// 卡在「先挑回云驱动取消勾选」的死胡同。
		cfg.RelayPersist = false
	}
	if raw.OSS == nil {
		// 不带 oss 块 = 只切驱动,连接原样 —— 靠已存连接把 local 切回
		// oss 是一次 PUT 的事。
		cfg.OSS = stored.OSS
	} else {
		if (raw.OSS.AccessKey == "") != (raw.OSS.SecretKey == "") {
			apierr.InvalidRequest(c, "access_key 与 secret_key 要么都填写,要么都留空沿用已存密钥")
			return
		}
		cfg.OSS = &OSSConfig{
			Endpoint:      raw.OSS.Endpoint,
			Bucket:        raw.OSS.Bucket,
			PublicBaseURL: raw.OSS.PublicBaseURL,
			AccessKey:     raw.OSS.AccessKey,
			SecretKey:     raw.OSS.SecretKey,
		}
		// 空字段沿用已存值:endpoint/bucket 没有「清空」语义(半截连接
		// 只是惰性配置),AK/SK 成对沿用 —— 切回 local 再切回 oss 不必
		// 重录连接。public_base_url 例外:空 = 显式停用自有公网地址。
		if stored.OSS != nil {
			if cfg.OSS.Endpoint == "" {
				cfg.OSS.Endpoint = stored.OSS.Endpoint
			}
			if cfg.OSS.Bucket == "" {
				cfg.OSS.Bucket = stored.OSS.Bucket
			}
			if cfg.OSS.AccessKey == "" && cfg.OSS.SecretKey == "" {
				cfg.OSS.AccessKey = stored.OSS.AccessKey
				cfg.OSS.SecretKey = stored.OSS.SecretKey
			}
		}
	}
	if raw.COS == nil {
		// cos 块同款:不带 = 连接原样。
		cfg.COS = stored.COS
	} else {
		if (raw.COS.SecretID == "") != (raw.COS.SecretKey == "") {
			apierr.InvalidRequest(c, "secret_id 与 secret_key 要么都填写,要么都留空沿用已存密钥")
			return
		}
		cfg.COS = &COSConfig{
			Endpoint:      raw.COS.Endpoint,
			Bucket:        raw.COS.Bucket,
			PublicBaseURL: raw.COS.PublicBaseURL,
			SecretID:      raw.COS.SecretID,
			SecretKey:     raw.COS.SecretKey,
		}
		if stored.COS != nil {
			if cfg.COS.Endpoint == "" {
				cfg.COS.Endpoint = stored.COS.Endpoint
			}
			if cfg.COS.Bucket == "" {
				cfg.COS.Bucket = stored.COS.Bucket
			}
			if cfg.COS.SecretID == "" && cfg.COS.SecretKey == "" {
				cfg.COS.SecretID = stored.COS.SecretID
				cfg.COS.SecretKey = stored.COS.SecretKey
			}
		}
	}
	if err := NormalizeStorageConfig(&cfg); err != nil {
		apierr.InvalidRequest(c, err.Error())
		return
	}

	value, err := json.Marshal(cfg)
	if err != nil {
		h.failInternal(c, err)
		return
	}
	row, err := h.Store.Put(c.Request.Context(), NameStorage, value)
	if err != nil {
		h.failInternal(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"storage": toStorageJSON(cfg, row.UpdatedAt.Format(time.RFC3339Nano))})
}

// storedConfig reads the stored row, answering the local default for a
// missing one. The second return is the row's updated_at (empty when the
// row has never been saved).
func (h *Handlers) storedConfig(ctx context.Context) (StorageConfig, string, error) {
	row, err := h.Store.Get(ctx, NameStorage)
	if errors.Is(err, ErrNotFound) {
		return DefaultStorageConfig(), "", nil
	}
	if err != nil {
		return DefaultStorageConfig(), "", err
	}
	var cfg StorageConfig
	if err := json.Unmarshal(row.Value, &cfg); err != nil {
		return DefaultStorageConfig(), "", err
	}
	return withDefaultDriver(cfg), row.UpdatedAt.Format(time.RFC3339Nano), nil
}

func (h *Handlers) failInternal(c *gin.Context, err error) {
	log.Printf("settings: %s %s: %v", c.Request.Method, c.Request.URL.Path, err)
	apierr.Internal(c, "服务内部错误,请稍后再试")
}
