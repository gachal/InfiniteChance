package apikey

import (
	"errors"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/gin-gonic/gin"

	"github.com/gachal/InfiniteChance/internal/apierr"
)

const (
	maxNameRunes  = 64
	quotaLogLimit = 100
)

// Handlers serves the admin API-key endpoints. The gateway mounts them
// under /admin behind the JWT session middleware.
type Handlers struct {
	Store Store
}

// RegisterAdminRoutes mounts:
//
//	GET  /admin/keys                — list (prefix + balance only)
//	POST /admin/keys                — create; full key value returned once
//	POST /admin/keys/:id/revoke     — idempotent revocation
//	POST /admin/keys/:id/topup      — manual quota credit
//	GET  /admin/keys/:id/quota-log  — ledger, newest first
func RegisterAdminRoutes(group *gin.RouterGroup, h *Handlers) {
	group.GET("/keys", h.List)
	group.POST("/keys", h.Create)
	group.POST("/keys/:id/revoke", h.Revoke)
	group.POST("/keys/:id/topup", h.TopUp)
	group.GET("/keys/:id/quota-log", h.QuotaLog)
}

// keyJSON is the wire form of a key. The secret never appears here — only
// the prefix — and quota crosses the wire as human USD.
type keyJSON struct {
	ID        int64      `json:"id"`
	Name      string     `json:"name"`
	Prefix    string     `json:"prefix"`
	QuotaUSD  float64    `json:"quota_usd"`
	Status    string     `json:"status"`
	ExpiresAt *time.Time `json:"expires_at"`
	RevokedAt *time.Time `json:"revoked_at"`
	CreatedAt time.Time  `json:"created_at"`
	UpdatedAt time.Time  `json:"updated_at"`
}

func toKeyJSON(k Key, now time.Time) keyJSON {
	return keyJSON{
		ID: k.ID, Name: k.Name, Prefix: k.Prefix,
		QuotaUSD:  MicrosToUSD(k.QuotaMicros),
		Status:    k.Status(now),
		ExpiresAt: k.ExpiresAt, RevokedAt: k.RevokedAt,
		CreatedAt: k.CreatedAt, UpdatedAt: k.UpdatedAt,
	}
}

type createdKeyJSON struct {
	keyJSON
	// Key is the full sk- value, answered exactly once — on creation only.
	Key string `json:"key"`
}

type keyListResponse struct {
	Keys []keyJSON `json:"keys"`
}

type createKeyInput struct {
	Name            string     `json:"name"`
	ExpiresAt       *time.Time `json:"expires_at"`
	InitialQuotaUSD *float64   `json:"initial_quota_usd"`
}

func (h *Handlers) Create(c *gin.Context) {
	var raw createKeyInput
	if err := c.ShouldBindJSON(&raw); err != nil {
		apierr.InvalidRequest(c, "请求体不是合法的 key 配置 JSON")
		return
	}
	name := strings.TrimSpace(raw.Name)
	if name == "" {
		apierr.InvalidRequest(c, "key 名称不能为空")
		return
	}
	if utf8.RuneCountInString(name) > maxNameRunes {
		apierr.InvalidRequest(c, fmt.Sprintf("key 名称最多 %d 个字符", maxNameRunes))
		return
	}

	now := time.Now()
	if raw.ExpiresAt != nil && !raw.ExpiresAt.After(now) {
		apierr.InvalidRequest(c, "过期时间必须是未来时间")
		return
	}

	var initialMicros int64
	if raw.InitialQuotaUSD != nil {
		micros, err := amountToMicros(*raw.InitialQuotaUSD)
		if err != nil {
			apierr.InvalidRequest(c, fmt.Sprintf("初始额度非法:%v", err))
			return
		}
		initialMicros = micros
	}

	full, err := Generate()
	if err != nil {
		h.failInternal(c, err)
		return
	}
	created, err := h.Store.Create(c.Request.Context(), Key{
		Name:        name,
		Prefix:      PrefixOf(full),
		KeyHash:     Hash(full),
		QuotaMicros: initialMicros,
		ExpiresAt:   raw.ExpiresAt,
	})
	if err != nil {
		h.failInternal(c, err)
		return
	}
	c.JSON(http.StatusCreated, createdKeyJSON{keyJSON: toKeyJSON(created, now), Key: full})
}

func (h *Handlers) List(c *gin.Context) {
	keys, err := h.Store.List(c.Request.Context())
	if err != nil {
		h.failInternal(c, err)
		return
	}
	now := time.Now()
	bodies := make([]keyJSON, 0, len(keys))
	for _, k := range keys {
		bodies = append(bodies, toKeyJSON(k, now))
	}
	c.JSON(http.StatusOK, keyListResponse{Keys: bodies})
}

func (h *Handlers) Revoke(c *gin.Context) {
	id, ok := parseID(c)
	if !ok {
		return
	}
	k, err := h.Store.Revoke(c.Request.Context(), id, time.Now())
	if errors.Is(err, ErrKeyNotFound) {
		apierr.NotFound(c, "key 不存在")
		return
	}
	if err != nil {
		h.failInternal(c, err)
		return
	}
	c.JSON(http.StatusOK, toKeyJSON(k, time.Now()))
}

type topUpInput struct {
	AmountUSD *float64 `json:"amount_usd"`
}

func (h *Handlers) TopUp(c *gin.Context) {
	id, ok := parseID(c)
	if !ok {
		return
	}
	var raw topUpInput
	if err := c.ShouldBindJSON(&raw); err != nil {
		apierr.InvalidRequest(c, "请求体不是合法的充值 JSON")
		return
	}
	if raw.AmountUSD == nil {
		apierr.InvalidRequest(c, "缺少充值金额 amount_usd")
		return
	}
	micros, err := amountToMicros(*raw.AmountUSD)
	if err != nil {
		apierr.InvalidRequest(c, fmt.Sprintf("充值金额非法:%v", err))
		return
	}

	// 只给还活着的 key 充值:给已吊销/已过期 key 加余额会误导管理员,
	// 让其看起来可用(实际上一律 401)。
	target, err := h.Store.ByID(c.Request.Context(), id)
	if errors.Is(err, ErrKeyNotFound) {
		apierr.NotFound(c, "key 不存在")
		return
	}
	if err != nil {
		h.failInternal(c, err)
		return
	}
	if target.Status(time.Now()) != StatusActive {
		apierr.Conflict(c, "key_not_active", "key 已吊销或已过期,不能充值")
		return
	}

	k, err := h.Store.TopUp(c.Request.Context(), id, micros, ReasonManualTopUp)
	if errors.Is(err, ErrKeyNotFound) {
		apierr.NotFound(c, "key 不存在")
		return
	}
	if err != nil {
		h.failInternal(c, err)
		return
	}
	c.JSON(http.StatusOK, toKeyJSON(k, time.Now()))
}

// amountToMicros validates a human USD amount and converts it to quota
// micros — the single gate shared by initial quota and top-up.
func amountToMicros(usd float64) (int64, error) {
	if usd <= 0 || usd > MaxTopUpUSD {
		return 0, fmt.Errorf("需大于 0 且不超过 %.0f 美元", float64(MaxTopUpUSD))
	}
	micros := USDToMicros(usd)
	if micros <= 0 {
		return 0, fmt.Errorf("太小,至少需要 0.000001 美元")
	}
	return micros, nil
}

type quotaLogResponse struct {
	Entries []quotaEntryJSON `json:"entries"`
}

type quotaEntryJSON struct {
	ID         int64     `json:"id"`
	DeltaUSD   float64   `json:"delta_usd"`
	BalanceUSD float64   `json:"balance_usd"`
	Reason     string    `json:"reason"`
	CreatedAt  time.Time `json:"created_at"`
}

func (h *Handlers) QuotaLog(c *gin.Context) {
	id, ok := parseID(c)
	if !ok {
		return
	}
	// 与吊销/充值一致:不存在的 key 报 404,而不是空流水。
	if _, err := h.Store.ByID(c.Request.Context(), id); errors.Is(err, ErrKeyNotFound) {
		apierr.NotFound(c, "key 不存在")
		return
	} else if err != nil {
		h.failInternal(c, err)
		return
	}
	entries, err := h.Store.QuotaLog(c.Request.Context(), id, quotaLogLimit)
	if err != nil {
		h.failInternal(c, err)
		return
	}
	bodies := make([]quotaEntryJSON, 0, len(entries))
	for _, e := range entries {
		bodies = append(bodies, quotaEntryJSON{
			ID:         e.ID,
			DeltaUSD:   MicrosToUSD(e.DeltaMicros),
			BalanceUSD: MicrosToUSD(e.BalanceMicros),
			Reason:     e.Reason,
			CreatedAt:  e.CreatedAt,
		})
	}
	c.JSON(http.StatusOK, quotaLogResponse{Entries: bodies})
}

func parseID(c *gin.Context) (int64, bool) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		apierr.InvalidRequest(c, "路径参数 id 必须是正整数")
		return 0, false
	}
	return id, true
}

func (h *Handlers) failInternal(c *gin.Context, err error) {
	log.Printf("apikey: %s %s: %v", c.Request.Method, c.Request.URL.Path, err)
	apierr.Internal(c, "服务内部错误,请稍后再试")
}
