package agenttools_test

// 66 号票八工具的单测:fakes 覆盖 store 切片,逐工具验 Execute 的入参校验、
// 作用域(IP)绑定、payload 形状与 read_source 的 rune 区间切分。

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"

	"github.com/gachal/InfiniteChance/internal/agenttools"
	"github.com/gachal/InfiniteChance/internal/canvas"
	"github.com/gachal/InfiniteChance/internal/character"
	"github.com/gachal/InfiniteChance/internal/promptgen"
	"github.com/gachal/InfiniteChance/internal/prop"
	"github.com/gachal/InfiniteChance/internal/scene"
)

// ---- fakes:只实现 agenttools.Deps 需要的切片 ----

type fakeCharacters struct {
	mu      sync.Mutex
	list    []character.Character
	created []struct {
		ipID int64
		in   character.CharacterInput
	}
	nextID int64
}

func (f *fakeCharacters) CreateCharacter(_ context.Context, ipID int64, in character.CharacterInput) (character.Character, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.nextID++
	f.created = append(f.created, struct {
		ipID int64
		in   character.CharacterInput
	}{ipID, in})
	return character.Character{ID: f.nextID, IPID: ipID, Name: in.Name, Alias: in.Alias, Description: in.Description, Detail: in.Detail}, nil
}

func (f *fakeCharacters) ListCharacters(_ context.Context, ipID int64) ([]character.Character, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []character.Character
	for _, c := range f.list {
		if c.IPID == ipID {
			out = append(out, c)
		}
	}
	return out, nil
}

type fakeScenes struct {
	mu      sync.Mutex
	list    []scene.Scene
	created []struct {
		ipID int64
		in   scene.SceneInput
	}
	nextID int64
}

func (f *fakeScenes) CreateScene(_ context.Context, ipID int64, in scene.SceneInput) (scene.Scene, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.nextID++
	f.created = append(f.created, struct {
		ipID int64
		in   scene.SceneInput
	}{ipID, in})
	return scene.Scene{ID: f.nextID, IPID: ipID, Name: in.Name, Alias: in.Alias, Description: in.Description, Detail: in.Detail}, nil
}

func (f *fakeScenes) ListScenes(_ context.Context, ipID int64) ([]scene.Scene, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []scene.Scene
	for _, s := range f.list {
		if s.IPID == ipID {
			out = append(out, s)
		}
	}
	return out, nil
}

type fakeProps struct {
	mu      sync.Mutex
	list    []prop.Prop
	created []struct {
		ipID int64
		in   prop.PropInput
	}
	nextID int64
}

func (f *fakeProps) CreateProp(_ context.Context, ipID int64, in prop.PropInput) (prop.Prop, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.nextID++
	f.created = append(f.created, struct {
		ipID int64
		in   prop.PropInput
	}{ipID, in})
	return prop.Prop{ID: f.nextID, IPID: ipID, Name: in.Name, Alias: in.Alias, Description: in.Description, Detail: in.Detail}, nil
}

func (f *fakeProps) ListProps(_ context.Context, ipID int64) ([]prop.Prop, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []prop.Prop
	for _, p := range f.list {
		if p.IPID == ipID {
			out = append(out, p)
		}
	}
	return out, nil
}

// fakeCanvasStore 用整图 JSON 回答 Get;图形制 = 编辑器 snapshot()
// (nodes/edges,节点 data 随身)。
type fakeCanvasStore struct {
	byID map[int64]canvas.Canvas
}

func (f *fakeCanvasStore) Get(_ context.Context, id int64) (canvas.Canvas, error) {
	if c, ok := f.byID[id]; ok {
		return c, nil
	}
	return canvas.Canvas{}, canvas.ErrNotFound
}

// ---- 装配辅助 ----

func scopeCtx(canvasID, ipID int64) context.Context {
	return promptgen.WithAgentScope(context.Background(), promptgen.AgentScope{CanvasID: canvasID, IPID: ipID})
}

func mustArgs(t *testing.T, v any) json.RawMessage {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal args: %v", err)
	}
	return raw
}

func lookup(t *testing.T, reg *promptgen.AgentToolRegistry, name string) promptgen.AgentTool {
	t.Helper()
	tool, ok := reg.Lookup(name)
	if !ok {
		t.Fatalf("tool %q not registered", name)
	}
	return tool
}

// payloadMap 解析工具 payload 供断言。
func payloadMap(t *testing.T, raw json.RawMessage) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("payload not JSON: %v (%s)", err, raw)
	}
	return m
}

func runeCount(s string) int { return len([]rune(s)) }

// ---- create_* 三兄弟 ----

func TestCreateCharacterStoresUnderCanvasIP(t *testing.T) {
	store := &fakeCharacters{}
	reg := agenttools.NewRegistry(agenttools.Deps{Characters: store})
	tool := lookup(t, reg, "create_character")

	summary, payload, err := tool.Execute(scopeCtx(7, 3), mustArgs(t, map[string]any{
		"name": "林小满", "alias": "小满", "description": "16 岁的机械师学徒", "detail": "银色短发",
	}))
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if !strings.Contains(summary, "林小满") || !strings.Contains(summary, "id=1") {
		t.Errorf("summary = %q, want name and id", summary)
	}
	pm := payloadMap(t, payload)
	if pm["id"].(float64) != 1 || pm["kind"] != "character" || pm["name"] != "林小满" {
		t.Errorf("payload = %v, want {kind:character,id:1,name:林小满}", pm)
	}
	if store.created[0].ipID != 3 || store.created[0].in.Name != "林小满" || store.created[0].in.Alias != "小满" {
		t.Errorf("store got %+v, want ipID=3 with fields", store.created[0])
	}
}

func TestCreateSceneAndPropStoreUnderCanvasIP(t *testing.T) {
	scenes := &fakeScenes{}
	props := &fakeProps{}
	reg := agenttools.NewRegistry(agenttools.Deps{Scenes: scenes, Props: props})

	sceneTool := lookup(t, reg, "create_scene")
	summary, payload, err := sceneTool.Execute(scopeCtx(7, 3), mustArgs(t, map[string]any{"name": "废弃工厂"}))
	if err != nil {
		t.Fatalf("create_scene: %v", err)
	}
	if !strings.Contains(summary, "废弃工厂") || payloadMap(t, payload)["kind"] != "scene" {
		t.Errorf("scene summary/payload = %q / %v", summary, payloadMap(t, payload))
	}
	if scenes.created[0].ipID != 3 {
		t.Errorf("scene ipID = %d, want 3", scenes.created[0].ipID)
	}

	propTool := lookup(t, reg, "create_prop")
	summary, payload, err = propTool.Execute(scopeCtx(7, 3), mustArgs(t, map[string]any{"name": "怀表", "description": "黄铜怀表"}))
	if err != nil {
		t.Fatalf("create_prop: %v", err)
	}
	if !strings.Contains(summary, "怀表") || payloadMap(t, payload)["kind"] != "prop" {
		t.Errorf("prop summary/payload = %q / %v", summary, payloadMap(t, payload))
	}
	if props.created[0].ipID != 3 || props.created[0].in.Description != "黄铜怀表" {
		t.Errorf("prop store got %+v", props.created[0])
	}
}

func TestCreateToolsRejectBadInput(t *testing.T) {
	store := &fakeCharacters{}
	reg := agenttools.NewRegistry(agenttools.Deps{Characters: store})
	cases := []struct {
		name   string
		tool   string
		ctx    context.Context
		args   map[string]any
		errSub string
	}{
		{"缺作用域", "create_character", context.Background(), map[string]any{"name": "张三"}, "画布上下文"},
		{"空名", "create_character", scopeCtx(7, 3), map[string]any{"name": "  "}, "名称不能为空"},
		{"超长名", "create_character", scopeCtx(7, 3), map[string]any{"name": strings.Repeat("名", 256)}, "名称最多 255"},
		{"超长简介", "create_scene", scopeCtx(7, 3), map[string]any{"name": "场景", "description": strings.Repeat("简", 1025)}, "简介最多 1024"},
		{"超长细节", "create_prop", scopeCtx(7, 3), map[string]any{"name": "道具", "detail": strings.Repeat("细", 20001)}, "详细设定最多 20000"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tool := lookup(t, reg, tc.tool)
			_, _, err := tool.Execute(tc.ctx, mustArgs(t, tc.args))
			if err == nil {
				t.Fatalf("want error, got none")
			}
			if !strings.Contains(err.Error(), tc.errSub) {
				t.Errorf("error = %q, want contains %q", err, tc.errSub)
			}
		})
	}
	if len(store.created) != 0 {
		t.Errorf("failed calls must not write, got %+v", store.created)
	}
}

// ---- list_assets ----

func TestListAssetsReturnsCatalogShape(t *testing.T) {
	reg := agenttools.NewRegistry(agenttools.Deps{
		Characters: &fakeCharacters{list: []character.Character{
			{ID: 11, IPID: 3, Name: "林小满", Alias: "小满", Description: "机械师学徒", Detail: "银色短发"},
			{ID: 12, IPID: 9, Name: "别家角色", Detail: "不属于本 IP"},
		}},
		Scenes: &fakeScenes{list: []scene.Scene{
			{ID: 21, IPID: 3, Name: "废弃工厂", Description: "第一幕主场", Detail: "锈蚀钢架"},
		}},
		Props: &fakeProps{},
	})
	tool := lookup(t, reg, "list_assets")

	summary, payload, err := tool.Execute(scopeCtx(7, 3), mustArgs(t, map[string]any{}))
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if !strings.Contains(summary, "角色 1") || !strings.Contains(summary, "场景 1") || !strings.Contains(summary, "道具 0") {
		t.Errorf("summary = %q, want per-kind counts", summary)
	}
	pm := payloadMap(t, payload)
	chars, _ := pm["characters"].([]any)
	if len(chars) != 1 {
		t.Fatalf("characters = %v, want only this-IP rows", pm["characters"])
	}
	c := chars[0].(map[string]any)
	if c["id"].(float64) != 11 || c["name"] != "林小满" || c["alias"] != "小满" || c["description"] != "机械师学徒" || c["detail"] != "银色短发" {
		t.Errorf("character row = %v, want name/alias/description/detail", c)
	}
	if _, has := c["cover_asset_id"]; has {
		t.Errorf("character row carries cover_asset_id %v, want text fields only", c)
	}
	scenes, _ := pm["scenes"].([]any)
	if len(scenes) != 1 {
		t.Fatalf("scenes = %v, want one row", pm["scenes"])
	}
	props, _ := pm["props"].([]any)
	if props == nil || len(props) != 0 {
		t.Errorf("props = %v, want empty array (not null)", pm["props"])
	}
}

// ---- save_outline / save_script ----

func TestSaveScriptDraftsPayload(t *testing.T) {
	reg := agenttools.NewRegistry(agenttools.Deps{})
	for _, name := range []string{"save_outline", "save_script"} {
		t.Run(name, func(t *testing.T) {
			tool := lookup(t, reg, name)
			summary, payload, err := tool.Execute(context.Background(), mustArgs(t, map[string]any{
				"title": "第3集 假戏真做", "text": "【第1场|内|夜|仓库】\n林小满:……",
			}))
			if err != nil {
				t.Fatalf("execute: %v", err)
			}
			if !strings.Contains(summary, "第3集 假戏真做") || !strings.Contains(summary, "字") {
				t.Errorf("summary = %q, want title and char count", summary)
			}
			pm := payloadMap(t, payload)
			if pm["title"] != "第3集 假戏真做" || !strings.Contains(pm["text"].(string), "第1场") {
				t.Errorf("payload = %v, want {title,text}", pm)
			}
		})
	}
}

func TestSaveScriptRejectsEmptyOrOverlongText(t *testing.T) {
	reg := agenttools.NewRegistry(agenttools.Deps{})
	tool := lookup(t, reg, "save_script")
	if _, _, err := tool.Execute(context.Background(), mustArgs(t, map[string]any{"text": "   "})); err == nil {
		t.Error("empty text must be rejected")
	}
	if _, _, err := tool.Execute(context.Background(), mustArgs(t, map[string]any{"text": strings.Repeat("字", 300001)})); err == nil {
		t.Error("overlong text must be rejected")
	}
}

// ---- save_storyboard ----

func TestSaveStoryboardDraftsCutsWithEmptyFrames(t *testing.T) {
	reg := agenttools.NewRegistry(agenttools.Deps{})
	tool := lookup(t, reg, "save_storyboard")
	summary, payload, err := tool.Execute(context.Background(), mustArgs(t, map[string]any{
		"title": "EP03 分镜",
		"cuts": []any{
			map[string]any{"lines": "林小满:别过来!", "camera_moves": []any{"推", "手持"}, "shot": "近", "character_ids": []any{11}, "seconds": 8},
			map[string]any{"note": "转场空镜"},
		},
	}))
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if !strings.Contains(summary, "2") {
		t.Errorf("summary = %q, want cut count", summary)
	}
	pm := payloadMap(t, payload)
	if pm["title"] != "EP03 分镜" {
		t.Errorf("title = %v", pm["title"])
	}
	cuts, _ := pm["cuts"].([]any)
	if len(cuts) != 2 {
		t.Fatalf("cuts = %v", pm["cuts"])
	}
	first := cuts[0].(map[string]any)
	if first["asset_id"].(float64) != 0 {
		t.Errorf("cut asset_id = %v, want 0 (画面空,前端占位)", first)
	}
	moves, _ := first["camera_moves"].([]any)
	if len(moves) != 2 || moves[0] != "推" || moves[1] != "手持" {
		t.Errorf("camera_moves = %v", first["camera_moves"])
	}
	if first["shot"] != "近" || first["lines"] != "林小满:别过来!" {
		t.Errorf("shot/lines = %v / %v", first["shot"], first["lines"])
	}
	ids, _ := first["character_ids"].([]any)
	if len(ids) != 1 || ids[0].(float64) != 11 {
		t.Errorf("character_ids = %v", first["character_ids"])
	}
	if first["seconds"].(float64) != 8 {
		t.Errorf("seconds = %v", first["seconds"])
	}
	second := cuts[1].(map[string]any)
	if second["asset_id"].(float64) != 0 || second["lines"] != "" || second["note"] != "转场空镜" {
		t.Errorf("second cut = %v, want zeroed frame with note", second)
	}
	if _, has := second["camera_moves"]; has {
		t.Errorf("second cut carries camera_moves %v, want omitted", second)
	}
}

func TestSaveStoryboardRejectsBadCuts(t *testing.T) {
	reg := agenttools.NewRegistry(agenttools.Deps{})
	tool := lookup(t, reg, "save_storyboard")
	cases := []struct {
		name string
		args map[string]any
	}{
		{"缺 cuts", map[string]any{"title": "x"}},
		{"空 cuts", map[string]any{"cuts": []any{}}},
		{"cut 非对象", map[string]any{"cuts": []any{"一个镜头"}}},
		{"camera_moves 非数组", map[string]any{"cuts": []any{map[string]any{"camera_moves": "推"}}}},
		{"character_ids 非正整数", map[string]any{"cuts": []any{map[string]any{"character_ids": []any{-1}}}}},
		{"seconds 负数", map[string]any{"cuts": []any{map[string]any{"seconds": -3}}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, _, err := tool.Execute(context.Background(), mustArgs(t, tc.args)); err == nil {
				t.Fatalf("want error, got none")
			}
		})
	}
}

// ---- read_source ----

// graphJSON 拼一张编辑器 snapshot 形状的图:一个原文 script 节点 + 一个
// 空正文 script 节点 + 一个 agent 节点(非 script 类型不在读取范围)。
func graphJSON(scriptID, text string) string {
	return `{"nodes":[
		{"id":` + mustJSONString(scriptID) + `,"type":"script","position":{"x":0,"y":0},"data":{"title":` + mustJSONString("原文:雾都") + `,"text":` + mustJSONString(text) + `}},
		{"id":"script-empty-2","type":"script","position":{"x":20,"y":20},"data":{"title":"剧本","text":""}},
		{"id":"agent-1-1","type":"agent","position":{"x":10,"y":10},"data":{"text":"会话文本"}}
	],"edges":[]}`
}

func mustJSONString(s string) string {
	raw, _ := json.Marshal(s)
	return string(raw)
}

func newSourceRegistry(t *testing.T, graph string) (*fakeCanvasStore, promptgen.AgentTool) {
	t.Helper()
	store := &fakeCanvasStore{byID: map[int64]canvas.Canvas{
		7: {ID: 7, IPID: 3, Graph: []byte(graph)},
	}}
	reg := agenttools.NewRegistry(agenttools.Deps{Canvases: store})
	return store, lookup(t, reg, "read_source")
}

func TestReadSourceReadsRuneSegments(t *testing.T) {
	// 18000 个 rune 的中文正文:确保按 rune 而非 byte 切。
	text := strings.Repeat("雾都夜行第一幕。", 2250) // 8 runes × 2250 = 18000 runes
	_, tool := newSourceRegistry(t, graphJSON("script-9-1", text))

	t.Run("首页缺省区间", func(t *testing.T) {
		summary, payload, err := tool.Execute(scopeCtx(7, 3), mustArgs(t, map[string]any{"node_id": "script-9-1"}))
		if err != nil {
			t.Fatalf("execute: %v", err)
		}
		pm := payloadMap(t, payload)
		if pm["start"].(float64) != 1 || pm["end"].(float64) != 12000 {
			t.Errorf("start/end = %v/%v, want 1..12000", pm["start"], pm["end"])
		}
		if got := runeCount(pm["text"].(string)); got != 12000 {
			t.Errorf("segment runes = %d, want 12000", got)
		}
		if !strings.Contains(summary, "18000") || !strings.Contains(summary, "12000") {
			t.Errorf("summary = %q, want total and read counts", summary)
		}
	})
	t.Run("中段区间", func(t *testing.T) {
		_, payload, err := tool.Execute(scopeCtx(7, 3), mustArgs(t, map[string]any{"node_id": "script-9-1", "start": 12001, "length": 5999}))
		if err != nil {
			t.Fatalf("execute: %v", err)
		}
		pm := payloadMap(t, payload)
		if pm["start"].(float64) != 12001 || pm["end"].(float64) != 17999 {
			t.Errorf("start/end = %v/%v, want 12001..17999", pm["start"], pm["end"])
		}
	})
	t.Run("超尾截断到文末", func(t *testing.T) {
		summary, payload, err := tool.Execute(scopeCtx(7, 3), mustArgs(t, map[string]any{"node_id": "script-9-1", "start": 17000, "length": 12000}))
		if err != nil {
			t.Fatalf("execute: %v", err)
		}
		pm := payloadMap(t, payload)
		if pm["end"].(float64) != 18000 {
			t.Errorf("end = %v, want clamped to 18000", pm["end"])
		}
		if !strings.Contains(summary, "文末") {
			t.Errorf("summary = %q, want end-of-text notice", summary)
		}
	})
	t.Run("整文一次读完", func(t *testing.T) {
		_, payload, err := tool.Execute(scopeCtx(7, 3), mustArgs(t, map[string]any{"node_id": "script-9-1", "start": 1, "length": 18000}))
		if err != nil {
			t.Fatalf("execute: %v", err)
		}
		if got := runeCount(payloadMap(t, payload)["text"].(string)); got != 18000 {
			t.Errorf("runes = %d, want 18000", got)
		}
	})
}

func TestReadSourceRejectsBadRanges(t *testing.T) {
	text := strings.Repeat("字", 100)
	_, tool := newSourceRegistry(t, graphJSON("script-9-1", text))
	cases := []struct {
		name   string
		noScope bool
		args   map[string]any
		errSub string
	}{
		{"起点超尾", false, map[string]any{"node_id": "script-9-1", "start": 101}, "超出正文长度"},
		{"节点不存在", false, map[string]any{"node_id": "script-gone", "start": 1}, "没有找到"},
		{"非 script 节点", false, map[string]any{"node_id": "agent-1-1", "start": 1}, "没有找到"},
		{"节点无正文", false, map[string]any{"node_id": "script-empty-2", "start": 1}, "没有正文"},
		{"非正 length", false, map[string]any{"node_id": "script-9-1", "start": 1, "length": 0}, "length"},
		{"缺画布作用域", true, map[string]any{"node_id": "script-9-1", "start": 1}, "画布上下文"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			if !tc.noScope {
				ctx = scopeCtx(7, 3)
			}
			_, _, err := tool.Execute(ctx, mustArgs(t, tc.args))
			if err == nil {
				t.Fatalf("want error, got none")
			}
			if !strings.Contains(err.Error(), tc.errSub) {
				t.Errorf("error = %q, want contains %q", err, tc.errSub)
			}
		})
	}
}

func TestReadSourceWithoutNodeIDListsScriptNodes(t *testing.T) {
	_, tool := newSourceRegistry(t, graphJSON("script-9-1", strings.Repeat("字", 9)))
	summary, payload, err := tool.Execute(scopeCtx(7, 3), mustArgs(t, map[string]any{}))
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if !strings.Contains(summary, "script-9-1") || !strings.Contains(summary, "原文:雾都") {
		t.Errorf("summary = %q, want the node catalog", summary)
	}
	pm := payloadMap(t, payload)
	nodes, _ := pm["nodes"].([]any)
	if len(nodes) != 1 {
		t.Fatalf("nodes = %v, want the one script node", pm["nodes"])
	}
	row := nodes[0].(map[string]any)
	if row["id"] != "script-9-1" || row["title"] != "原文:雾都" || row["chars"].(float64) != 9 {
		t.Errorf("node row = %v, want id/title/chars", row)
	}
	if _, has := row["text"]; has {
		t.Errorf("catalog row carries text %v, want metadata only", row)
	}
}

// ---- 注册表 ----

func TestRegistryExposesAllToolsInOrder(t *testing.T) {
	reg := agenttools.NewRegistry(agenttools.Deps{Characters: &fakeCharacters{}, Scenes: &fakeScenes{}, Props: &fakeProps{}})
	want := []string{"create_character", "create_scene", "create_prop", "list_assets", "save_outline", "save_script", "save_storyboard", "read_source"}
	defs := reg.Definitions()
	if len(defs) != len(want) {
		t.Fatalf("definitions = %d, want %d", len(defs), len(want))
	}
	for i, name := range want {
		var def struct {
			Type     string `json:"type"`
			Function struct {
				Name       string         `json:"name"`
				Parameters map[string]any `json:"parameters"`
			} `json:"function"`
		}
		if err := json.Unmarshal(defs[i], &def); err != nil {
			t.Fatalf("definition %d not JSON: %v", i, err)
		}
		if def.Type != "function" || def.Function.Name != name {
			t.Errorf("definition %d = %s/%s, want function/%s", i, def.Type, def.Function.Name, name)
		}
		if def.Function.Parameters == nil || def.Function.Parameters["type"] != "object" {
			t.Errorf("definition %d parameters = %v, want an object schema", i, def.Function.Parameters)
		}
		tool := lookup(t, reg, name)
		if tool.Name() != name {
			t.Errorf("tool.Name() = %q, want %q", tool.Name(), name)
		}
	}
}
