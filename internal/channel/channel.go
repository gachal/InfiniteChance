// Package channel implements vendor channel management: one stored route to
// an upstream AI vendor — type, base URL, secret, model-name mapping,
// priority/weight, enabled — plus the admin CRUD surface and the one-click
// connectivity probe. Channel selection/failover itself is ticket 06.
package channel

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"
)

// ErrNotFound reports a channel id that has no row.
var ErrNotFound = errors.New("channel: not found")

// TypeOpenAI marks an OpenAI-compatible upstream: OpenAI itself, DeepSeek,
// Kimi, GLM, Qwen compatible-mode, Ark, Gemini's OpenAI layer — all share one
// adaptor per the spec, and all are probed with GET {base_url}/models.
const TypeOpenAI = "openai"

// SupportedTypes lists the channel types this build can relay and probe.
var SupportedTypes = []string{TypeOpenAI}

// SupportedType reports whether t is a channel type this build understands.
func SupportedType(t string) bool {
	for _, known := range SupportedTypes {
		if t == known {
			return true
		}
	}
	return false
}

// Channel is one upstream vendor connection. APIKey is the vendor secret:
// stored so the gateway can sign upstream requests, but write-only through
// the admin API — responses carry the key hint, never the key.
type Channel struct {
	ID        int64
	Name      string
	Type      string
	BaseURL   string
	APIKey    string
	ModelMap  map[string]string // 公开模型名 → 上游模型名
	Priority  int               // 数值越大越优先(调度细则由 06 号票定案)
	Weight    int               // 同优先级内的加权
	Enabled   bool
	CreatedAt time.Time
	UpdatedAt time.Time
}

// Input is the admin-supplied configuration. APIKey empty on update means
// "keep the stored secret"; on create it is required.
type Input struct {
	Name     string
	Type     string
	BaseURL  string
	APIKey   string
	ModelMap map[string]string
	Priority int
	Weight   int
	Enabled  bool
}

const (
	maxNameRunes      = 64
	maxBaseURLRunes   = 512
	maxAPIKeyRunes    = 4096
	maxModelMapEntry  = 100
	maxModelNameRunes = 200
	maxPriority       = 1_000_000
	maxWeight         = 1_000_000
)

// Normalize trims and validates Input, returning a canonical copy.
func (in Input) Normalize(requireKey bool) (Input, error) {
	in.Name = strings.TrimSpace(in.Name)
	in.BaseURL = strings.TrimSpace(in.BaseURL)
	in.APIKey = strings.TrimSpace(in.APIKey)

	if in.Name == "" {
		return in, fmt.Errorf("渠道名称不能为空")
	}
	if utf8.RuneCountInString(in.Name) > maxNameRunes {
		return in, fmt.Errorf("渠道名称最多 %d 个字符", maxNameRunes)
	}
	if !SupportedType(in.Type) {
		return in, fmt.Errorf("不支持的渠道类型:%s", in.Type)
	}

	parsed, err := url.Parse(in.BaseURL)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return in, fmt.Errorf("BaseURL 必须是 http(s) 地址,例如 https://api.openai.com/v1")
	}
	if utf8.RuneCountInString(in.BaseURL) > maxBaseURLRunes {
		return in, fmt.Errorf("BaseURL 最多 %d 个字符", maxBaseURLRunes)
	}
	in.BaseURL = strings.TrimRight(in.BaseURL, "/")

	if requireKey && in.APIKey == "" {
		return in, fmt.Errorf("渠道密钥不能为空")
	}
	if in.APIKey != "" && utf8.RuneCountInString(in.APIKey) > maxAPIKeyRunes {
		return in, fmt.Errorf("渠道密钥最多 %d 个字符", maxAPIKeyRunes)
	}

	if in.ModelMap == nil {
		in.ModelMap = map[string]string{}
	}
	normalized := make(map[string]string, len(in.ModelMap))
	for public, upstream := range in.ModelMap {
		public = strings.TrimSpace(public)
		upstream = strings.TrimSpace(upstream)
		if public == "" || upstream == "" {
			return in, fmt.Errorf("模型映射的公开名与上游名都不能为空")
		}
		if utf8.RuneCountInString(public) > maxModelNameRunes || utf8.RuneCountInString(upstream) > maxModelNameRunes {
			return in, fmt.Errorf("模型名最多 %d 个字符", maxModelNameRunes)
		}
		normalized[public] = upstream
	}
	in.ModelMap = normalized
	if len(normalized) > maxModelMapEntry {
		return in, fmt.Errorf("模型映射最多 %d 条", maxModelMapEntry)
	}

	if in.Priority < 0 || in.Priority > maxPriority {
		return in, fmt.Errorf("优先级需在 0 到 %d 之间", maxPriority)
	}
	if in.Weight < 0 || in.Weight > maxWeight {
		return in, fmt.Errorf("权重需在 0 到 %d 之间", maxWeight)
	}
	return in, nil
}

// Store persists channels. The MySQL implementation backs the gateway;
// returned rows always carry ID and timestamps.
type Store interface {
	List(ctx context.Context) ([]Channel, error)
	// Get returns the channel or ErrNotFound.
	Get(ctx context.Context, id int64) (Channel, error)
	Create(ctx context.Context, ch Channel) (Channel, error)
	// Update replaces the row; an empty ch.APIKey keeps the stored secret.
	// Returns the updated row or ErrNotFound.
	Update(ctx context.Context, ch Channel) (Channel, error)
	// Delete removes the row or reports ErrNotFound.
	Delete(ctx context.Context, id int64) error
}
