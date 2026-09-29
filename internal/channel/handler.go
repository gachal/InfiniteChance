package channel

import (
	"errors"
	"log"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/gachal/InfiniteChance/internal/apierr"
)

// Handlers serves the admin channel endpoints. The gateway mounts them
// under /admin behind the JWT session middleware.
type Handlers struct {
	Store  Store
	Tester *Tester
}

// RegisterAdminRoutes mounts:
//
//	GET    /admin/channels          — list (secrets never included)
//	POST   /admin/channels          — create
//	PUT    /admin/channels/:id      — replace config; empty api_key keeps stored
//	DELETE /admin/channels/:id      — remove
//	POST   /admin/channels/:id/test — one-click connectivity probe
func RegisterAdminRoutes(group *gin.RouterGroup, h *Handlers) {
	group.GET("/channels", h.List)
	group.POST("/channels", h.Create)
	group.PUT("/channels/:id", h.Update)
	group.DELETE("/channels/:id", h.Delete)
	group.POST("/channels/:id/test", h.Test)
}

// channelJSON is the wire form of a channel. The vendor secret is never
// serialized — has_key/key_hint let the admin see that one is stored.
// Capabilities always renders the effective set (nil legacy rows show as
// ["chat"]), so the admin sees exactly what scheduling will honor. Config's
// secret-key entries (IsSecretConfigKey) follow the same write-only rule:
// their value renders empty with a tail hint in config_hints, everything
// else (sub_app_id, region …) round-trips in the clear.
type channelJSON struct {
	ID           int64             `json:"id"`
	Name         string            `json:"name"`
	Type         string            `json:"type"`
	BaseURL      string            `json:"base_url"`
	HasKey       bool              `json:"has_key"`
	KeyHint      string            `json:"key_hint,omitempty"`
	Config       map[string]string `json:"config,omitempty"`
	ConfigHints  map[string]string `json:"config_hints,omitempty"`
	ModelMap     map[string]string `json:"model_map"`
	Capabilities []Capability      `json:"capabilities"`
	Priority     int               `json:"priority"`
	Weight       int               `json:"weight"`
	Enabled      bool              `json:"enabled"`
	CreatedAt    time.Time         `json:"created_at"`
	UpdatedAt    time.Time         `json:"updated_at"`
}

const keyHintRunes = 4

func toChannelJSON(ch Channel) channelJSON {
	caps := ch.Capabilities
	if len(caps) == 0 {
		caps = []Capability{CapChat}
	}
	body := channelJSON{
		ID: ch.ID, Name: ch.Name, Type: ch.Type, BaseURL: ch.BaseURL,
		HasKey: ch.APIKey != "", ModelMap: ch.ModelMap,
		Capabilities: caps,
		Priority:     ch.Priority, Weight: ch.Weight, Enabled: ch.Enabled,
		CreatedAt: ch.CreatedAt, UpdatedAt: ch.UpdatedAt,
	}
	if body.HasKey {
		runes := []rune(ch.APIKey)
		if len(runes) > keyHintRunes {
			runes = runes[len(runes)-keyHintRunes:]
		}
		body.KeyHint = "…" + string(runes)
	}
	body.Config, body.ConfigHints = maskConfig(ch.Config)
	return body
}

// maskConfig splits one stored config map into the echoable wire form
// (secret values blanked) and the per-key tail hints for the entries a
// secret is stored under.
func maskConfig(config map[string]string) (echo map[string]string, hints map[string]string) {
	if len(config) == 0 {
		return nil, nil
	}
	echo = make(map[string]string, len(config))
	for k, v := range config {
		if IsSecretConfigKey(k) && v != "" {
			if hints == nil {
				hints = map[string]string{}
			}
			runes := []rune(v)
			if len(runes) > keyHintRunes {
				runes = runes[len(runes)-keyHintRunes:]
			}
			hints[k] = "…" + string(runes)
			echo[k] = ""
			continue
		}
		echo[k] = v
	}
	return echo, hints
}

// mergeKeptSecrets folds "empty secret = keep stored" into an incoming
// config: every IsSecretConfigKey entry the admin left blank is replaced by
// the stored value, mirroring the api_key CASE rule inside the stores.
func mergeKeptSecrets(stored, incoming map[string]string) map[string]string {
	if incoming == nil {
		return nil
	}
	for k, v := range incoming {
		if v == "" && IsSecretConfigKey(k) && stored != nil {
			if old, ok := stored[k]; ok && old != "" {
				incoming[k] = old
			}
		}
	}
	return incoming
}

type channelInputJSON struct {
	Name         string            `json:"name"`
	Type         string            `json:"type"`
	BaseURL      string            `json:"base_url"`
	APIKey       string            `json:"api_key"`
	Config       map[string]string `json:"config"`
	ModelMap     map[string]string `json:"model_map"`
	Capabilities []Capability      `json:"capabilities"`
	Priority     int               `json:"priority"`
	Weight       int               `json:"weight"`
	Enabled      bool              `json:"enabled"`
}

// input projects the wire body onto the validated domain input.
func (raw channelInputJSON) input() Input {
	return Input{
		Name: raw.Name, Type: raw.Type, BaseURL: raw.BaseURL, APIKey: raw.APIKey,
		Config: raw.Config, ModelMap: raw.ModelMap, Capabilities: raw.Capabilities,
		Priority: raw.Priority, Weight: raw.Weight,
		Enabled: raw.Enabled,
	}
}

// row projects a validated input onto a stored row.
func (in Input) row(id int64) Channel {
	return Channel{
		ID:   id,
		Name: in.Name, Type: in.Type, BaseURL: in.BaseURL, APIKey: in.APIKey,
		Config: in.Config, ModelMap: in.ModelMap, Capabilities: in.Capabilities,
		Priority: in.Priority, Weight: in.Weight,
		Enabled: in.Enabled,
	}
}

type listResponse struct {
	Channels []channelJSON `json:"channels"`
}

func (h *Handlers) List(c *gin.Context) {
	channels, err := h.Store.List(c.Request.Context())
	if err != nil {
		h.failInternal(c, err)
		return
	}
	bodies := make([]channelJSON, 0, len(channels))
	for _, ch := range channels {
		bodies = append(bodies, toChannelJSON(ch))
	}
	c.JSON(http.StatusOK, listResponse{Channels: bodies})
}

func (h *Handlers) Create(c *gin.Context) {
	var raw channelInputJSON
	if err := c.ShouldBindJSON(&raw); err != nil {
		apierr.InvalidRequest(c, "请求体不是合法的渠道配置 JSON")
		return
	}
	input, err := raw.input().Normalize(true)
	if err != nil {
		apierr.InvalidRequest(c, err.Error())
		return
	}

	created, err := h.Store.Create(c.Request.Context(), input.row(0))
	if err != nil {
		h.failInternal(c, err)
		return
	}
	c.JSON(http.StatusCreated, toChannelJSON(created))
}

func (h *Handlers) Update(c *gin.Context) {
	id, ok := parseID(c)
	if !ok {
		return
	}
	var raw channelInputJSON
	if err := c.ShouldBindJSON(&raw); err != nil {
		apierr.InvalidRequest(c, "请求体不是合法的渠道配置 JSON")
		return
	}
	// 更新时密钥可留空 = 保留已存密钥;config 同款语义——请求体不带
	// config 字段(整个键缺省)也保留已存配置,带了才进入「敏感键留空
	// = 保留已存值」的合并。与 api_key 的 CASE WHEN '' 完全对称,防止
	// 只改优先级之类的裸 API 调用顺手抹掉渠道凭据。
	current, err := h.Store.Get(c.Request.Context(), id)
	if errors.Is(err, ErrNotFound) {
		apierr.NotFound(c, "渠道不存在")
		return
	}
	if err != nil {
		h.failInternal(c, err)
		return
	}
	in := raw.input()
	if in.Config == nil {
		in.Config = current.Config
	} else {
		in.Config = mergeKeptSecrets(current.Config, in.Config)
	}
	input, err := in.Normalize(false)
	if err != nil {
		apierr.InvalidRequest(c, err.Error())
		return
	}

	updated, err := h.Store.Update(c.Request.Context(), input.row(id))
	if errors.Is(err, ErrNotFound) {
		apierr.NotFound(c, "渠道不存在")
		return
	}
	if err != nil {
		h.failInternal(c, err)
		return
	}
	c.JSON(http.StatusOK, toChannelJSON(updated))
}

func (h *Handlers) Delete(c *gin.Context) {
	id, ok := parseID(c)
	if !ok {
		return
	}
	err := h.Store.Delete(c.Request.Context(), id)
	if errors.Is(err, ErrNotFound) {
		apierr.NotFound(c, "渠道不存在")
		return
	}
	if err != nil {
		h.failInternal(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

func (h *Handlers) Test(c *gin.Context) {
	id, ok := parseID(c)
	if !ok {
		return
	}
	ch, err := h.Store.Get(c.Request.Context(), id)
	if errors.Is(err, ErrNotFound) {
		apierr.NotFound(c, "渠道不存在")
		return
	}
	if err != nil {
		h.failInternal(c, err)
		return
	}
	if h.Tester == nil {
		log.Printf("channel: tester not wired, cannot test channel %d", id)
		apierr.Internal(c, "服务内部错误,请稍后再试")
		return
	}
	// 探测本身总有可判定的结论,ok=false 也是一次成功执行的测试。
	c.JSON(http.StatusOK, h.Tester.Test(c.Request.Context(), ch))
}

// parseID reads the :id path parameter as an integer id.
func parseID(c *gin.Context) (int64, bool) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		apierr.InvalidRequest(c, "路径参数 id 必须是正整数")
		return 0, false
	}
	return id, true
}

func (h *Handlers) failInternal(c *gin.Context, err error) {
	log.Printf("channel: %s %s: %v", c.Request.Method, c.Request.URL.Path, err)
	apierr.Internal(c, "服务内部错误,请稍后再试")
}
