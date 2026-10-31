package agenttools

// 写图类工具(66 号票定案 1,ADR 0006 单一写者):save_outline /
// save_script / save_storyboard 产出「图改动草案」payload,由前端应用进
// 本地图状态、走既有 autosave —— 后端(本包)永不改库内图 JSON,Execute
// 只做参数校验与草案定形,无任何副作用。

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/gachal/InfiniteChance/internal/promptgen"
)

// maxScriptRunes 与前端 script 节点的 30 万字上限同宽(64 号票口径;
// 后端按 rune 计,略严于 UTF-16 code unit —— 对护栏语义足够)。
const maxScriptRunes = 300_000

// ---- save_outline / save_script(同一载体,文案各异)----

type saveScriptTool struct {
	name  string // save_outline | save_script
	label string // 「分集大纲」|「剧本」
}

func newSaveScript(name, label string, _ Deps) promptgen.AgentTool {
	return saveScriptTool{name: name, label: label}
}

func (t saveScriptTool) Name() string { return t.name }

func (t saveScriptTool) Definition() json.RawMessage {
	return functionDefinition(t.Name(),
		fmt.Sprintf("保存%s为画布节点(payload = title + text,前端自动落图)。产物分批落:每集一个节点,不要把多集并进一次调用。", t.label),
		map[string]any{
			"type": "object",
			"properties": map[string]any{
				"title": map[string]any{"type": "string", "description": t.label + "标题,如「分集大纲」「第3集 假戏真做」"},
				"text":  map[string]any{"type": "string", "description": t.label + "正文(纯文本;剧本场次用 markdown 场头约定,如「【第1场|内|夜|仓库】」)"},
			},
			"required": []string{"text"},
		})
}

func (t saveScriptTool) Execute(_ context.Context, args json.RawMessage) (string, json.RawMessage, error) {
	m, err := argsOf(args)
	if err != nil {
		return "", nil, err
	}
	title, err := stringArg(m, "title")
	if err != nil {
		return "", nil, err
	}
	text, err := stringArg(m, "text")
	if err != nil {
		return "", nil, err
	}
	title = strings.TrimSpace(title)
	text = strings.TrimSpace(text)
	if text == "" {
		return "", nil, fmt.Errorf("正文不能为空")
	}
	if runeLen(text) > maxScriptRunes {
		return "", nil, fmt.Errorf("正文最多 %d 个字符,请分批保存", maxScriptRunes)
	}
	summary := fmt.Sprintf("已保存%s「%s」(共 %d 字),节点将落上画布", t.label, title, runeLen(text))
	payload, err := json.Marshal(map[string]any{"title": title, "text": text})
	if err != nil {
		return summary, nil, nil
	}
	return summary, payload, nil
}

// ---- save_storyboard ----

// storyboardCutDraft is one cut of the payload(53 号票 cut 结构 + 67 号票
// 扩字段;asset_id 恒 0 = 画面空,前端「未配画面」占位)。
type storyboardCutDraft struct {
	AssetID      int64    `json:"asset_id"`
	Lines        string   `json:"lines"`
	CameraMoves  []string `json:"camera_moves,omitempty"`
	Seconds      *int     `json:"seconds,omitempty"`
	Note         string   `json:"note,omitempty"`
	Shot         string   `json:"shot,omitempty"`
	CharacterIDs []int64  `json:"character_ids,omitempty"`
}

type saveStoryboardTool struct{}

func newSaveStoryboard(_ Deps) promptgen.AgentTool { return saveStoryboardTool{} }

func (t saveStoryboardTool) Name() string { return "save_storyboard" }

func (t saveStoryboardTool) Definition() json.RawMessage {
	return functionDefinition(t.Name(),
		"保存一集的分镜板(絵コンテ)为画布节点,前端自动落图;cuts 每个元素是一镜,画面由用户后续配置、不要编造。",
		map[string]any{
			"type": "object",
			"properties": map[string]any{
				"title": map[string]any{"type": "string", "description": "分镜板名,如「EP03 分镜」"},
				"cuts": map[string]any{
					"type":     "array",
					"items":    storyboardCutSchema,
					"minItems": 1,
				},
			},
			"required": []string{"cuts"},
		})
}

// storyboardCutSchema 描述单个 cut 的参数形状(景别/出场角色按 67 号票扩
// 字段;camera_moves 运镜多选白名单见导演提示词)。
var storyboardCutSchema = map[string]any{
	"type": "object",
	"properties": map[string]any{
		"shot":           map[string]any{"type": "string", "description": "景别,单选:远/全/中/近/特写"},
		"lines":          map[string]any{"type": "string", "description": "这一镜的台词/音效"},
		"camera_moves":   map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "运镜,可多选:推/拉/摇/移/跟/固定/手持/变焦"},
		"seconds":        map[string]any{"type": "number", "description": "预估时长(秒)"},
		"note":           map[string]any{"type": "string", "description": "画面内容描述/备注"},
		"character_ids":  map[string]any{"type": "array", "items": map[string]any{"type": "number"}, "description": "出场角色的资产 id(list_assets/create_* 拿到的 id)"},
	},
}

func (t saveStoryboardTool) Execute(_ context.Context, args json.RawMessage) (string, json.RawMessage, error) {
	m, err := argsOf(args)
	if err != nil {
		return "", nil, err
	}
	title, err := stringArg(m, "title")
	if err != nil {
		return "", nil, err
	}
	title = strings.TrimSpace(title)
	rawCuts, ok := m["cuts"]
	if !ok {
		return "", nil, fmt.Errorf("cuts 不能为空:每个元素是一镜")
	}
	list, ok := rawCuts.([]any)
	if !ok {
		return "", nil, fmt.Errorf("参数 cuts 必须是数组")
	}
	if len(list) == 0 {
		return "", nil, fmt.Errorf("cuts 至少要有一个分镜")
	}
	cuts := make([]storyboardCutDraft, 0, len(list))
	for i, rc := range list {
		cut, err := parseStoryboardCut(rc)
		if err != nil {
			return "", nil, fmt.Errorf("cuts[%d]:%w", i, err)
		}
		cuts = append(cuts, cut)
	}
	summary := fmt.Sprintf("已保存分镜板「%s」共 %d 个分镜,节点将落上画布", title, len(cuts))
	payload, err := json.Marshal(map[string]any{"title": title, "cuts": cuts})
	if err != nil {
		return summary, nil, nil
	}
	return summary, payload, nil
}

// parseStoryboardCut validates one cut from the model:画面恒空(asset_id 0
// —— 生图是花钱操作,永不进循环),字段按前端 StoryboardCut 形状定形。
func parseStoryboardCut(raw any) (storyboardCutDraft, error) {
	obj, ok := raw.(map[string]any)
	if !ok {
		return storyboardCutDraft{}, fmt.Errorf("每个 cut 必须是对象")
	}
	cut := storyboardCutDraft{AssetID: 0, Lines: ""}
	if v, err := stringArg(obj, "lines"); err != nil {
		return storyboardCutDraft{}, err
	} else {
		cut.Lines = v
	}
	if v, err := stringArg(obj, "shot"); err != nil {
		return storyboardCutDraft{}, err
	} else {
		cut.Shot = v
	}
	if v, err := stringArg(obj, "note"); err != nil {
		return storyboardCutDraft{}, err
	} else {
		cut.Note = v
	}
	if v, ok, err := numberArg(obj, "seconds"); err != nil {
		return storyboardCutDraft{}, err
	} else if ok {
		seconds := int(v)
		if seconds < 0 {
			return storyboardCutDraft{}, fmt.Errorf("seconds 不能为负数")
		}
		cut.Seconds = &seconds
	}
	if v, ok := obj["camera_moves"]; ok && v != nil {
		arr, ok := v.([]any)
		if !ok {
			return storyboardCutDraft{}, fmt.Errorf("camera_moves 必须是字符串数组")
		}
		cut.CameraMoves = make([]string, 0, len(arr))
		for _, item := range arr {
			s, ok := item.(string)
			if !ok {
				return storyboardCutDraft{}, fmt.Errorf("camera_moves 必须是字符串数组")
			}
			cut.CameraMoves = append(cut.CameraMoves, s)
		}
	}
	if v, ok := obj["character_ids"]; ok && v != nil {
		arr, ok := v.([]any)
		if !ok {
			return storyboardCutDraft{}, fmt.Errorf("character_ids 必须是数字数组")
		}
		cut.CharacterIDs = make([]int64, 0, len(arr))
		for _, item := range arr {
			n, ok := item.(float64)
			if !ok || n != float64(int64(n)) || n <= 0 {
				return storyboardCutDraft{}, fmt.Errorf("character_ids 必须是正整数(角色资产 id)")
			}
			cut.CharacterIDs = append(cut.CharacterIDs, int64(n))
		}
	}
	return cut, nil
}
