package agenttools

// 库表类工具(66 号票定案 1):create_character / create_scene / create_prop
// 把纯文本资产写入本 IP 的资产库(65 号票三类实体;不出设定图 —— 设定图
// 是用户在资产库的人工动作),list_assets 读全库供角色对表。Execute 落库
// 即完成(ADR 0006:库表类后端直写),payload 回带实体 id 供模型在
// save_storyboard 的 character_ids 等处引用;前端对这些工具注册空应用器
// (无可落图之物,63 号票「未知写图类才报错」的契约不受影响)。

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/gachal/InfiniteChance/internal/character"
	"github.com/gachal/InfiniteChance/internal/promptgen"
	"github.com/gachal/InfiniteChance/internal/prop"
	"github.com/gachal/InfiniteChance/internal/scene"
)

// 文本字段宽度:与 51/65 号票 handler 同规(name/alias、description、detail)。
const (
	maxNameRunes        = 255
	maxDescriptionRunes = 1024
	maxDetailRunes      = 20000
)

// textAssetArgs is the shared argument shape of the three create tools.
type textAssetArgs struct {
	Name        string
	Alias       string
	Description string
	Detail      string
}

// parseTextAssetArgs validates one create call's arguments:名称必填,可选
// 字段按各自宽度封顶(空串 = 未填,与创建面同义)。
func parseTextAssetArgs(args json.RawMessage) (textAssetArgs, error) {
	m, err := argsOf(args)
	if err != nil {
		return textAssetArgs{}, err
	}
	var out textAssetArgs
	if out.Name, err = stringArg(m, "name"); err != nil {
		return textAssetArgs{}, err
	}
	if out.Alias, err = stringArg(m, "alias"); err != nil {
		return textAssetArgs{}, err
	}
	if out.Description, err = stringArg(m, "description"); err != nil {
		return textAssetArgs{}, err
	}
	if out.Detail, err = stringArg(m, "detail"); err != nil {
		return textAssetArgs{}, err
	}
	out.Name = strings.TrimSpace(out.Name)
	if out.Name == "" {
		return textAssetArgs{}, fmt.Errorf("名称不能为空")
	}
	if err := limitRunes("名称", out.Name, maxNameRunes); err != nil {
		return textAssetArgs{}, err
	}
	out.Alias = strings.TrimSpace(out.Alias)
	if err := limitRunes("别名", out.Alias, maxNameRunes); err != nil {
		return textAssetArgs{}, err
	}
	out.Description = strings.TrimSpace(out.Description)
	if err := limitRunes("简介", out.Description, maxDescriptionRunes); err != nil {
		return textAssetArgs{}, err
	}
	out.Detail = strings.TrimSpace(out.Detail)
	if err := limitRunes("详细设定", out.Detail, maxDetailRunes); err != nil {
		return textAssetArgs{}, err
	}
	return out, nil
}

// textAssetDefinition is the shared parameter schema of the three create
// tools(kind 只影响描述文案)。
func textAssetDefinition(name, kind string) json.RawMessage {
	return functionDefinition(name, "创建一个新的"+kind+"并写入本 IP 资产库,返回实体 id。"+kind+"已存在时不要调用(先 list_assets 对表)。", map[string]any{
		"type": "object",
		"properties": map[string]any{
			"name":        map[string]any{"type": "string", "description": kind + "名(必填)"},
			"alias":       map[string]any{"type": "string", "description": "别名/昵称/外号,可省略"},
			"description": map[string]any{"type": "string", "description": "一句话简介(最多 1024 字)"},
			"detail":      map[string]any{"type": "string", "description": "详细设定:外貌/性格/布置/用途等(最多 20000 字)"},
		},
		"required": []string{"name"},
	})
}

// createResult 组装 create 三兄弟统一形状的 summary + payload(kind 是
// 稳定英文 token 供前端/后续链路判别,label 是中文展示文案)。
func createResult(kind, label, name string, id int64) (string, json.RawMessage) {
	summary := fmt.Sprintf("已创建%s「%s」(id=%d),已写入资产库", label, name, id)
	payload, err := json.Marshal(map[string]any{"kind": kind, "id": id, "name": name})
	if err != nil {
		return summary, nil
	}
	return summary, payload
}

// ---- create_character ----

type createCharacterTool struct {
	deps Deps
}

func newCreateCharacter(deps Deps) promptgen.AgentTool { return createCharacterTool{deps: deps} }

func (t createCharacterTool) Name() string { return "create_character" }

func (t createCharacterTool) Definition() json.RawMessage {
	return textAssetDefinition(t.Name(), "角色")
}

func (t createCharacterTool) Execute(ctx context.Context, args json.RawMessage) (string, json.RawMessage, error) {
	scope, err := scopeOf(ctx)
	if err != nil {
		return "", nil, err
	}
	in, err := parseTextAssetArgs(args)
	if err != nil {
		return "", nil, err
	}
	if t.deps.Characters == nil {
		return "", nil, fmt.Errorf("角色库未装配,无法创建角色")
	}
	ch, err := t.deps.Characters.CreateCharacter(ctx, scope.IPID, character.CharacterInput{
		Name: in.Name, Alias: in.Alias, Description: in.Description, Detail: in.Detail,
	})
	if err != nil {
		return "", nil, err
	}
	summary, payload := createResult("character", "角色", ch.Name, ch.ID)
	return summary, payload, nil
}

// ---- create_scene ----

type createSceneTool struct {
	deps Deps
}

func newCreateScene(deps Deps) promptgen.AgentTool { return createSceneTool{deps: deps} }

func (t createSceneTool) Name() string { return "create_scene" }

func (t createSceneTool) Definition() json.RawMessage {
	return textAssetDefinition(t.Name(), "场景")
}

func (t createSceneTool) Execute(ctx context.Context, args json.RawMessage) (string, json.RawMessage, error) {
	scope, err := scopeOf(ctx)
	if err != nil {
		return "", nil, err
	}
	in, err := parseTextAssetArgs(args)
	if err != nil {
		return "", nil, err
	}
	if t.deps.Scenes == nil {
		return "", nil, fmt.Errorf("场景库未装配,无法创建场景")
	}
	sc, err := t.deps.Scenes.CreateScene(ctx, scope.IPID, scene.SceneInput{
		Name: in.Name, Alias: in.Alias, Description: in.Description, Detail: in.Detail,
	})
	if err != nil {
		return "", nil, err
	}
	summary, payload := createResult("scene", "场景", sc.Name, sc.ID)
	return summary, payload, nil
}

// ---- create_prop ----

type createPropTool struct {
	deps Deps
}

func newCreateProp(deps Deps) promptgen.AgentTool { return createPropTool{deps: deps} }

func (t createPropTool) Name() string { return "create_prop" }

func (t createPropTool) Definition() json.RawMessage {
	return textAssetDefinition(t.Name(), "道具")
}

func (t createPropTool) Execute(ctx context.Context, args json.RawMessage) (string, json.RawMessage, error) {
	scope, err := scopeOf(ctx)
	if err != nil {
		return "", nil, err
	}
	in, err := parseTextAssetArgs(args)
	if err != nil {
		return "", nil, err
	}
	if t.deps.Props == nil {
		return "", nil, fmt.Errorf("道具库未装配,无法创建道具")
	}
	p, err := t.deps.Props.CreateProp(ctx, scope.IPID, prop.PropInput{
		Name: in.Name, Alias: in.Alias, Description: in.Description, Detail: in.Detail,
	})
	if err != nil {
		return "", nil, err
	}
	summary, payload := createResult("prop", "道具", p.Name, p.ID)
	return summary, payload, nil
}

// ---- list_assets ----

// assetCatalog is the payload shape of list_assets(66 号票定案 1:角色带
// name/alias/description/detail,场景/道具同字段;不出设定图引用)。
type assetCatalog struct {
	Characters []catalogCharacter `json:"characters"`
	Scenes     []catalogEntity    `json:"scenes"`
	Props      []catalogEntity    `json:"props"`
}

type catalogCharacter struct {
	ID          int64  `json:"id"`
	Name        string `json:"name"`
	Alias       string `json:"alias"`
	Description string `json:"description"`
	Detail      string `json:"detail"`
}

// catalogEntity is the scene/prop row(与角色行同字段,单列类型便于复用)。
type catalogEntity = catalogCharacter

type listAssetsTool struct {
	deps Deps
}

func newListAssets(deps Deps) promptgen.AgentTool { return listAssetsTool{deps: deps} }

func (t listAssetsTool) Name() string { return "list_assets" }

func (t listAssetsTool) Definition() json.RawMessage {
	return functionDefinition(t.Name(),
		"列出本 IP 资产库的全部角色/场景/道具(名称/别名/简介/详细设定)。动笔之前必须先调用它做角色对表。",
		map[string]any{"type": "object", "properties": map[string]any{}})
}

func (t listAssetsTool) Execute(ctx context.Context, _ json.RawMessage) (string, json.RawMessage, error) {
	scope, err := scopeOf(ctx)
	if err != nil {
		return "", nil, err
	}
	catalog := assetCatalog{
		Characters: []catalogCharacter{},
		Scenes:     []catalogEntity{},
		Props:      []catalogEntity{},
	}
	if t.deps.Characters != nil {
		rows, err := t.deps.Characters.ListCharacters(ctx, scope.IPID)
		if err != nil {
			return "", nil, fmt.Errorf("读角色库失败:%w", err)
		}
		for _, c := range rows {
			catalog.Characters = append(catalog.Characters, catalogCharacter{
				ID: c.ID, Name: c.Name, Alias: c.Alias, Description: c.Description, Detail: c.Detail,
			})
		}
	}
	if t.deps.Scenes != nil {
		rows, err := t.deps.Scenes.ListScenes(ctx, scope.IPID)
		if err != nil {
			return "", nil, fmt.Errorf("读场景库失败:%w", err)
		}
		for _, s := range rows {
			catalog.Scenes = append(catalog.Scenes, catalogEntity{
				ID: s.ID, Name: s.Name, Alias: s.Alias, Description: s.Description, Detail: s.Detail,
			})
		}
	}
	if t.deps.Props != nil {
		rows, err := t.deps.Props.ListProps(ctx, scope.IPID)
		if err != nil {
			return "", nil, fmt.Errorf("读道具库失败:%w", err)
		}
		for _, p := range rows {
			catalog.Props = append(catalog.Props, catalogEntity{
				ID: p.ID, Name: p.Name, Alias: p.Alias, Description: p.Description, Detail: p.Detail,
			})
		}
	}
	summary := fmt.Sprintf("本 IP 资产:角色 %d / 场景 %d / 道具 %d", len(catalog.Characters), len(catalog.Scenes), len(catalog.Props))
	payload, err := json.Marshal(catalog)
	if err != nil {
		return summary, nil, nil
	}
	return summary, payload, nil
}
