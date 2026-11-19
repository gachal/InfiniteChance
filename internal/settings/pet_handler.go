// 桌宠配置的管理 CRUD(92 号票):挂 /admin/settings/pet,照 storage 行
// 的 handler 先例 —— 缺行 GET 回缺省而非 404(管理页首开即渲染表单),
// PUT 缺省(未携带)字段沿用已存、显式空串/空数组 = 清空。桌宠配置无
// 密钥,全部字段原值回显(密钥外字段原值回显的 settings 纪律)。
package settings

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/gachal/InfiniteChance/internal/apierr"
)

// petJSON is the wire form of the pet setting. 桌宠形象键是对象键字符串
// (uploads/ 前缀),不是 URL —— 浏览器取用走画布侧读端点(pet 包)。
type petJSON struct {
	Name             string   `json:"name"`
	Greeting         string   `json:"greeting"`
	Chips            []string `json:"chips"`
	Model            string   `json:"model"`
	IdleImage        string   `json:"idle_image"`
	InteractiveImage string   `json:"interactive_image"`
	UpdatedAt        string   `json:"updated_at"`
}

// petInputJSON is the PUT body. 指针字段表达「未携带 = 沿用已存」(照
// storageInputJSON 的合并哲学):显式空串/空数组才是清空。chips 用指针
// 是为了区分「不带 chips(不动)」与「带空数组(清空)」。
type petInputJSON struct {
	Name             *string   `json:"name"`
	Greeting         *string   `json:"greeting"`
	Chips            *[]string `json:"chips"`
	Model            *string   `json:"model"`
	IdleImage        *string   `json:"idle_image"`
	InteractiveImage *string   `json:"interactive_image"`
}

func toPetJSON(cfg PetConfig, updatedAt string) petJSON {
	chips := cfg.Chips
	if chips == nil {
		chips = []string{}
	}
	return petJSON{
		Name:             cfg.Name,
		Greeting:         cfg.Greeting,
		Chips:            chips,
		Model:            cfg.Model,
		IdleImage:        cfg.IdleImage,
		InteractiveImage: cfg.InteractiveImage,
		UpdatedAt:        updatedAt,
	}
}

// GetPet answers the stored pet config. 缺行回答缺省配置而非 404:管理页
// 第一次打开就要能渲染表单。
func (h *Handlers) GetPet(c *gin.Context) {
	cfg, updatedAt, err := h.storedPet(c.Request.Context())
	if err != nil {
		h.failInternal(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"pet": toPetJSON(cfg, updatedAt)})
}

// PutPet replaces the pet config with the merge semantics of the input
// pointers, then normalizes and stores.
func (h *Handlers) PutPet(c *gin.Context) {
	var raw petInputJSON
	if err := c.ShouldBindJSON(&raw); err != nil {
		apierr.InvalidRequest(c, "请求体不是合法的桌宠配置 JSON")
		return
	}

	// 既有行先读出来做合并;行坏了让管理员看见 500 而不是静默覆盖。
	stored, _, err := h.storedPet(c.Request.Context())
	if err != nil {
		h.failInternal(c, err)
		return
	}

	cfg := stored
	applyString := func(dst *string, src *string) {
		if src != nil {
			*dst = *src
		}
	}
	applyString(&cfg.Name, raw.Name)
	applyString(&cfg.Greeting, raw.Greeting)
	applyString(&cfg.Model, raw.Model)
	applyString(&cfg.IdleImage, raw.IdleImage)
	applyString(&cfg.InteractiveImage, raw.InteractiveImage)
	if raw.Chips != nil {
		cfg.Chips = *raw.Chips
	}

	if err := NormalizePetConfig(&cfg); err != nil {
		apierr.InvalidRequest(c, err.Error())
		return
	}

	value, err := json.Marshal(cfg)
	if err != nil {
		h.failInternal(c, err)
		return
	}
	row, err := h.Store.Put(c.Request.Context(), NamePet, value)
	if err != nil {
		h.failInternal(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"pet": toPetJSON(cfg, row.UpdatedAt.Format(time.RFC3339Nano))})
}

// storedPet reads the stored pet row, answering the default config for a
// missing one. The second return is the row's updated_at (empty when the
// row has never been saved).
func (h *Handlers) storedPet(ctx context.Context) (PetConfig, string, error) {
	row, err := h.Store.Get(ctx, NamePet)
	if errors.Is(err, ErrNotFound) {
		return DefaultPetConfig(), "", nil
	}
	if err != nil {
		return DefaultPetConfig(), "", err
	}
	var cfg PetConfig
	if err := json.Unmarshal(row.Value, &cfg); err != nil {
		return DefaultPetConfig(), "", err
	}
	return cfg, row.UpdatedAt.Format(time.RFC3339Nano), nil
}
