// Package agenttools implements the script-pipeline domain tools the Agent
// tool loop exposes (66 号票,ADR 0006):create_character/create_scene/
// create_prop 把纯文本资产写入本 IP 库(65 号票三类实体),list_assets 读
// 库供角色对表,save_outline/save_script/save_storyboard 产出「图改动草案」
// 交前端应用(单一写者:本包永不改库内图 JSON),read_source 按 rune 区间
// 分段读剧本节点原文。工具实现 promptgen.AgentTool,经 wiring 装配注册表;
// 画布作用域(哪个画布/哪个 IP)由 SSE 端点注入请求 context(见
// promptgen.AgentScope),Execute 按它绑定。
package agenttools

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/gachal/InfiniteChance/internal/canvas"
	"github.com/gachal/InfiniteChance/internal/character"
	"github.com/gachal/InfiniteChance/internal/promptgen"
	"github.com/gachal/InfiniteChance/internal/prop"
	"github.com/gachal/InfiniteChance/internal/scene"
)

// Deps carries the store slices the tools need(窄接口,真实装配传
// character/scene/prop/canvas 的完整 Store —— Go 隐式接口天然满足)。
type Deps struct {
	Characters CharacterSource
	Scenes     SceneSource
	Props      PropSource
	Canvases   CanvasSource
}

// CharacterSource is the slice of the character store the tools use.
type CharacterSource interface {
	CreateCharacter(ctx context.Context, ipID int64, in character.CharacterInput) (character.Character, error)
	ListCharacters(ctx context.Context, ipID int64) ([]character.Character, error)
}

// SceneSource is the slice of the scene store the tools use.
type SceneSource interface {
	CreateScene(ctx context.Context, ipID int64, in scene.SceneInput) (scene.Scene, error)
	ListScenes(ctx context.Context, ipID int64) ([]scene.Scene, error)
}

// PropSource is the slice of the prop store the tools use.
type PropSource interface {
	CreateProp(ctx context.Context, ipID int64, in prop.PropInput) (prop.Prop, error)
	ListProps(ctx context.Context, ipID int64) ([]prop.Prop, error)
}

// CanvasSource is the slice of the canvas store read_source uses.
type CanvasSource interface {
	Get(ctx context.Context, id int64) (canvas.Canvas, error)
}

// NewRegistry wires the eight tools in exposure order(create 三兄弟 →
// list_assets → save 三兄弟 → read_source)并注册进一张新注册表。
func NewRegistry(deps Deps) *promptgen.AgentToolRegistry {
	reg := promptgen.NewAgentToolRegistry()
	reg.Register(newCreateCharacter(deps))
	reg.Register(newCreateScene(deps))
	reg.Register(newCreateProp(deps))
	reg.Register(newListAssets(deps))
	reg.Register(newSaveScript("save_outline", "分集大纲", deps))
	reg.Register(newSaveScript("save_script", "剧本", deps))
	reg.Register(newSaveStoryboard(deps))
	reg.Register(newReadSource(deps))
	return reg
}

// ---- 共用小件 ----

// functionDefinition marshals one OpenAI tools entry({"type":"function",
// "function":{…}});parameters 走 map 由 encoding/json 定形。
func functionDefinition(name, description string, parameters map[string]any) json.RawMessage {
	raw, err := json.Marshal(map[string]any{
		"type": "function",
		"function": map[string]any{
			"name":        name,
			"description": description,
			"parameters":  parameters,
		},
	})
	if err != nil {
		panic(fmt.Sprintf("agenttools: definition %q not marshalable: %v", name, err))
	}
	return raw
}

// scopeErr 是缺画布作用域时的错误(装配缺口/测试直调;正常流恒有作用域)。
func scopeErr() error {
	return fmt.Errorf("工具缺少画布上下文,无法确定操作的 IP;请通过 Agent 会话调用")
}

// scopeOf extracts the scope or answers scopeErr.
func scopeOf(ctx context.Context) (promptgen.AgentScope, error) {
	scope, ok := promptgen.AgentScopeFrom(ctx)
	if !ok {
		return promptgen.AgentScope{}, scopeErr()
	}
	return scope, nil
}

// argsOf parses the model's arguments JSON;空参数按空对象(63 号票循环已
// 兜底,这里再防一层)。
func argsOf(args json.RawMessage) (map[string]any, error) {
	if len(args) == 0 {
		args = json.RawMessage("{}")
	}
	var m map[string]any
	if err := json.Unmarshal(args, &m); err != nil {
		return nil, fmt.Errorf("调用参数不是合法 JSON 对象")
	}
	if m == nil {
		m = map[string]any{}
	}
	return m, nil
}

// stringArg 按可选字符串取参:键缺失/为 null 返回空串;类型不对报错
// (模型给了形状错误的参数,应让它看到原因)。
func stringArg(m map[string]any, key string) (string, error) {
	v, ok := m[key]
	if !ok || v == nil {
		return "", nil
	}
	s, ok := v.(string)
	if !ok {
		return "", fmt.Errorf("参数 %s 必须是字符串", key)
	}
	return s, nil
}

// numberArg 按可选数值取参。
func numberArg(m map[string]any, key string) (float64, bool, error) {
	v, ok := m[key]
	if !ok || v == nil {
		return 0, false, nil
	}
	n, ok := v.(float64)
	if !ok {
		return 0, false, fmt.Errorf("参数 %s 必须是数字", key)
	}
	return n, true, nil
}

// runeLen 是文本宽度口径(与前端 30 万字上限、read_source 区间同规)。
func runeLen(s string) int {
	return len([]rune(s))
}

// limitRunes 校验文本宽度:超限报错(文案与 51/65 号票 handler 同族)。
func limitRunes(field, value string, max int) error {
	if runeLen(value) > max {
		return fmt.Errorf("%s最多 %d 个字符", field, max)
	}
	return nil
}
