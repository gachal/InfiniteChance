// Package prompttemplate implements the admin-maintained prompt templates
// behind the canvas's generate-prompt action (11 号票):a template is a
// prompt-writing instruction with a {topic} placeholder. The gateway owns the
// admin CRUD; canvas/server reads enabled rows from the same shared MySQL
// table when rendering a generation, so admin edits reach the canvas action
// immediately — no cache, no sync step.
package prompttemplate

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"
)

// ErrNotFound reports a template id that has no row.
var ErrNotFound = errors.New("prompttemplate: not found")

// TopicPlaceholder marks where the user's topic goes inside a template.
// Templates without it are rejected at the admin boundary — the rendered
// message would otherwise silently ignore what the user typed.
const TopicPlaceholder = "{topic}"

// 上限:名称与画布名同宽;模板内容与 canvastask 的 prompt 同为 8k 宽上限
// —— 真正的厂商上下文限制在网关转发时自然暴露。
const (
	maxNameRunes     = 128
	maxTemplateRunes = 8000
)

// Template is one prompt-writing instruction. Template carries the full
// instruction text including the {topic} placeholder.
type Template struct {
	ID        int64
	Name      string
	Template  string
	Enabled   bool
	CreatedAt time.Time
	UpdatedAt time.Time
}

// Input is the admin-supplied configuration.
type Input struct {
	Name     string
	Template string
	Enabled  bool
}

// Normalize trims and validates Input, returning a canonical copy.
func (in Input) Normalize() (Input, error) {
	in.Name = strings.TrimSpace(in.Name)
	in.Template = strings.TrimSpace(in.Template)

	if in.Name == "" {
		return in, fmt.Errorf("模板名称不能为空")
	}
	if utf8.RuneCountInString(in.Name) > maxNameRunes {
		return in, fmt.Errorf("模板名称最多 %d 个字符", maxNameRunes)
	}
	if in.Template == "" {
		return in, fmt.Errorf("模板内容不能为空")
	}
	if utf8.RuneCountInString(in.Template) > maxTemplateRunes {
		return in, fmt.Errorf("模板内容最多 %d 个字符", maxTemplateRunes)
	}
	if !strings.Contains(in.Template, TopicPlaceholder) {
		return in, fmt.Errorf("模板内容必须包含 %s 占位符,生成时会替换为输入的主题", TopicPlaceholder)
	}
	return in, nil
}

// Render fills the topic in, producing the chat message the canvas server
// relays through the gateway.
func (t Template) Render(topic string) string {
	return strings.ReplaceAll(t.Template, TopicPlaceholder, topic)
}

// Store persists prompt templates. The MySQL implementation backs the
// gateway's admin surface and canvas/server's read-side alike.
type Store interface {
	// List returns all templates, oldest first (stable admin ordering).
	List(ctx context.Context) ([]Template, error)
	// Get returns the template or ErrNotFound.
	Get(ctx context.Context, id int64) (Template, error)
	Create(ctx context.Context, t Template) (Template, error)
	// Update replaces the row; returns the updated row or ErrNotFound.
	Update(ctx context.Context, t Template) (Template, error)
	// Delete removes the row or reports ErrNotFound.
	Delete(ctx context.Context, id int64) error
	// ListEnabled returns enabled templates only — the canvas action's catalog.
	ListEnabled(ctx context.Context) ([]Template, error)
}
