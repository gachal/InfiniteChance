// 桌宠配置(92 号票):settings KV 新 key `pet`,照 `storage` 行先例 ——
// 网关挂管理 CRUD、canvas 侧同库只读、按请求读表热生效、Normalize 校验。
// 桌宠是画布助手(91 号票)的化身壳:配置只承载形象/文案/缺省模型,
// 不含任何密钥,字段全部原值回显。
package settings

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strings"
	"unicode/utf8"
)

// NamePet is the settings row that carries the canvas pet configuration.
const NamePet = "pet"

// pet 字段长度上限:名称/模型是短标签,问候语与 chips 是面板首屏文案。
// 上限校验收在 Normalize,超限 400 而不是让前端渲染破版。
const (
	maxPetNameRunes     = 64
	maxPetGreetingRunes = 500
	maxPetChipRunes     = 100
	maxPetChips         = 24
	maxPetModelRunes    = 128
)

// PetConfig is the decoded `pet` row (92 号票)。零值 = 未配置:前端走 CSS
// 缺省形象、面板用内置兜底文案 —— 与桌面版零配置起步同一语义。
//
// IdleImage/InteractiveImage 是 objectstore 对象键(uploads/ 前缀,92 号票
// 上传端点落桶):桌宠形象是系统资产,不落 assets 行,配置只存键。
type PetConfig struct {
	// Name 是悬浮提示与助手面板标题用名。
	Name string `json:"name"`
	// Greeting 是助手面板新会话首屏文案。
	Greeting string `json:"greeting"`
	// Chips 是面板首屏快捷指令(「换一批」轮换;点击 = 预填输入框不发送)。
	Chips []string `json:"chips,omitempty"`
	// Model 是新会话对话模型初值(目录以 GET /prompt-models 实时为准,
	// 配置值不在目录时由前端回落首项)。
	Model string `json:"model,omitempty"`
	// IdleImage 待机动图对象键;InteractiveImage 互动动图对象键(可选)。
	IdleImage        string `json:"idle_image,omitempty"`
	InteractiveImage string `json:"interactive_image,omitempty"`
}

// DefaultPetConfig is the zero-config shape: 未配置的画布桌宠走前端内置
// CSS 缺省形象与面板兜底文案。
func DefaultPetConfig() PetConfig {
	return PetConfig{}
}

// normalizePetObjectKey 校验一个形象对象键:空 = 未配置放行;非空必须是
// uploads/ 前缀的规范键(objectstore.FileSystem.validKey 同族纪律 ——
// 绝对路径、穿越段、反斜杠、空段都拒;读端点只认配置登记键,此处再拦
// 一道,双保险防遍历)。
func normalizePetObjectKey(key string) (string, error) {
	key = strings.TrimSpace(key)
	if key == "" {
		return "", nil
	}
	if !strings.HasPrefix(key, "uploads/") {
		return "", fmt.Errorf("形象对象键必须以 uploads/ 开头(桌宠形象由管理台上传落桶):%q", key)
	}
	if strings.ContainsRune(key, '\\') || strings.HasPrefix(key, "/") {
		return "", fmt.Errorf("形象对象键含非法字符:%q", key)
	}
	for _, seg := range strings.Split(strings.TrimPrefix(key, "uploads/"), "/") {
		if seg == "" || seg == "." || seg == ".." {
			return "", fmt.Errorf("形象对象键含非法路径段:%q", key)
		}
	}
	return key, nil
}

// NormalizePetConfig trims the editable fields in place and validates the
// merged shape a fresh PUT must satisfy before it lands. chips 收敛形状:
// 逐条 trim、空串剔除、全空规约成 nil(库内不存空数组壳);形象键缺失
// 合法(纯文案配置),非空则必须是 uploads/ 规范键。
func NormalizePetConfig(c *PetConfig) error {
	c.Name = strings.TrimSpace(c.Name)
	if utf8.RuneCountInString(c.Name) > maxPetNameRunes {
		return fmt.Errorf("名称不能超过 %d 字", maxPetNameRunes)
	}
	c.Greeting = strings.TrimSpace(c.Greeting)
	if utf8.RuneCountInString(c.Greeting) > maxPetGreetingRunes {
		return fmt.Errorf("问候语不能超过 %d 字", maxPetGreetingRunes)
	}
	c.Model = strings.TrimSpace(c.Model)
	if utf8.RuneCountInString(c.Model) > maxPetModelRunes {
		return fmt.Errorf("缺省对话模型不能超过 %d 字", maxPetModelRunes)
	}

	chips := make([]string, 0, len(c.Chips))
	for _, chip := range c.Chips {
		chip = strings.TrimSpace(chip)
		if chip == "" {
			continue
		}
		if utf8.RuneCountInString(chip) > maxPetChipRunes {
			return fmt.Errorf("单条建议指令不能超过 %d 字", maxPetChipRunes)
		}
		chips = append(chips, chip)
	}
	if len(chips) > maxPetChips {
		return fmt.Errorf("建议指令不能超过 %d 条", maxPetChips)
	}
	if len(chips) == 0 {
		chips = nil
	}
	c.Chips = chips

	var err error
	if c.IdleImage, err = normalizePetObjectKey(c.IdleImage); err != nil {
		return err
	}
	if c.InteractiveImage, err = normalizePetObjectKey(c.InteractiveImage); err != nil {
		return err
	}
	return nil
}

// PetConfigReader answers the currently effective pet config; wiring 注入
// settings.NewPetReader 的产物。调用量小,按请求读表不加缓存(热生效,
// prompt_templates/storage 先例)。
type PetConfigReader func(ctx context.Context) (PetConfig, error)

// NewPetReader reads the `pet` row per call. 缺行/坏行按缺省回答:未配置
// 即「前端 CSS 缺省形象」,坏 JSON 不至于让画布面瘫痪。
func NewPetReader(s Store) PetConfigReader {
	return func(ctx context.Context) (PetConfig, error) {
		row, err := s.Get(ctx, NamePet)
		if errors.Is(err, ErrNotFound) {
			return DefaultPetConfig(), nil
		}
		if err != nil {
			return DefaultPetConfig(), err
		}
		var cfg PetConfig
		if err := json.Unmarshal(row.Value, &cfg); err != nil {
			log.Printf("settings: pet row undecodable, falling back to default: %v", err)
			return DefaultPetConfig(), nil
		}
		return cfg, nil
	}
}
