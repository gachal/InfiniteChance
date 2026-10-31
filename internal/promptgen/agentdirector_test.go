package promptgen_test

// 66 号票:「剧本导演」系统提示词注入 + 八工具全链(fake 网关循环)。
// 导演提示词以 system 消息开场、与技能注入正交叠加;全链一轮跑通
// create_character → list_assets → save_script + save_storyboard(同轮并行
// tool_calls 按序执行),验 SSE 事件序、IP 作用域、payload 形状与来源标记。

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/gachal/InfiniteChance/internal/agenttools"
	"github.com/gachal/InfiniteChance/internal/asset"
	"github.com/gachal/InfiniteChance/internal/canvas"
	"github.com/gachal/InfiniteChance/internal/character"
	"github.com/gachal/InfiniteChance/internal/promptgen"
	"github.com/gachal/InfiniteChance/internal/prop"
	"github.com/gachal/InfiniteChance/internal/pricing"
	"github.com/gachal/InfiniteChance/internal/prompttemplate"
	"github.com/gachal/InfiniteChance/internal/scene"
)

// ---- 导演提示词注入 ----

// 带工具的会话以 system 开场,未选技能时首条 user 指令换 agent 专用缺省
// (不再借用「提示词工程师」人设)。
func TestAgentStreamInjectsDirectorSystemPrompt(t *testing.T) {
	relay := &fakeRelay{rounds: []string{relayTextRound(t, "好的,先看方案。")}}
	relayServer := httptest.NewServer(relay.handler())
	t.Cleanup(relayServer.Close)

	env := newHandlerEnv(t, func(p *envParams) {
		p.gateway = promptgen.NewClient(relayServer.URL, "sk-relay")
		p.agentTools = promptgen.NewAgentToolRegistry()
		p.agentTools.Register(&stubAgentTool{name: "echo_tool"})
	})

	_, raw := env.do(t, http.MethodPost, "/canvases/7/agent/stream", map[string]any{
		"topic": "把这段小说变成短剧", "model": "chat-m",
	})
	if !strings.Contains(raw, "[DONE]") {
		t.Fatalf("stream = %s, want completion", raw)
	}
	msgs, _ := relay.request(0).Body["messages"].([]any)
	if len(msgs) != 2 {
		t.Fatalf("messages = %d, want system + user", len(msgs))
	}
	system, _ := msgs[0].(map[string]any)
	if system["role"] != "system" || !strings.Contains(system["content"].(string), "剧本导演") {
		t.Errorf("messages[0] = %v, want the director system prompt", system)
	}
	if !strings.Contains(system["content"].(string), "list_assets") {
		t.Errorf("system prompt = %v, want the reconciliation discipline", system)
	}
	user, _ := msgs[1].(map[string]any)
	content := user["content"].(string)
	if user["role"] != "user" || !strings.Contains(content, "把这段小说变成短剧") {
		t.Errorf("messages[1] = %v, want the rendered topic", user)
	}
	if strings.Contains(content, "提示词工程师") {
		t.Errorf("first instruction = %q, want the agent default (not the prompt-engineer persona)", content)
	}
}

// 技能注入正交叠加:选了技能时首条 user 指令仍是技能渲染文本,system
// 导演提示词在前。
func TestAgentStreamSkillStacksWithDirectorPrompt(t *testing.T) {
	relay := &fakeRelay{rounds: []string{relayTextRound(t, "按技能来。")}}
	relayServer := httptest.NewServer(relay.handler())
	t.Cleanup(relayServer.Close)

	env := newHandlerEnv(t, func(p *envParams) {
		p.gateway = promptgen.NewClient(relayServer.URL, "sk-relay")
		p.agentTools = promptgen.NewAgentToolRegistry()
		p.agentTools.Register(&stubAgentTool{name: "echo_tool"})
	})

	_, raw := env.do(t, http.MethodPost, "/canvases/7/agent/stream", map[string]any{
		"template_id": 1, "topic": "赛博朋克", "model": "chat-m",
	})
	if !strings.Contains(raw, "[DONE]") {
		t.Fatalf("stream = %s, want completion", raw)
	}
	msgs, _ := relay.request(0).Body["messages"].([]any)
	if len(msgs) != 2 {
		t.Fatalf("messages = %d, want system + skill instruction", len(msgs))
	}
	system, _ := msgs[0].(map[string]any)
	if system["role"] != "system" || !strings.Contains(system["content"].(string), "剧本导演") {
		t.Errorf("messages[0] = %v, want the director system prompt", system)
	}
	user, _ := msgs[1].(map[string]any)
	if !strings.Contains(user["content"].(string), "英文文生图提示词") {
		t.Errorf("messages[1] = %v, want the skill text intact", user)
	}
}

// 注册表为空(63 号票机制态):无 system、无 tools、指令维持原缺省 ——
// 请求与升级前逐字段一致。
func TestAgentStreamEmptyRegistryStaysPlain(t *testing.T) {
	relay := &fakeRelay{rounds: []string{relayTextRound(t, "纯会话。")}}
	relayServer := httptest.NewServer(relay.handler())
	t.Cleanup(relayServer.Close)

	env := newHandlerEnv(t, func(p *envParams) {
		p.gateway = promptgen.NewClient(relayServer.URL, "sk-relay")
	})

	_, raw := env.do(t, http.MethodPost, "/canvases/7/agent/stream", map[string]any{
		"topic": "写个提示词", "model": "chat-m",
	})
	if !strings.Contains(raw, "[DONE]") {
		t.Fatalf("stream = %s, want completion", raw)
	}
	req := relay.request(0)
	if _, has := req.Body["tools"]; has {
		t.Errorf("tools = %v, want the field absent", req.Body["tools"])
	}
	msgs, _ := req.Body["messages"].([]any)
	if len(msgs) != 1 {
		t.Fatalf("messages = %v, want the single user turn", msgs)
	}
	first, _ := msgs[0].(map[string]any)
	if first["role"] != "user" || !strings.Contains(first["content"].(string), "提示词工程师") {
		t.Errorf("messages[0] = %v, want the legacy builtin instruction", first)
	}
}

// ---- 全链:fake 网关 + 真工具注册表 ----

// chainCharacters/chainScenes/chainProps:全链测试用的库桩(Create+List)。
type chainCharacters struct {
	mu      sync.Mutex
	created []struct {
		ipID int64
		name string
	}
	nextID int64
}

func (f *chainCharacters) CreateCharacter(_ context.Context, ipID int64, in character.CharacterInput) (character.Character, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.nextID++
	f.created = append(f.created, struct {
		ipID int64
		name string
	}{ipID, in.Name})
	return character.Character{ID: f.nextID, IPID: ipID, Name: in.Name}, nil
}

func (f *chainCharacters) ListCharacters(_ context.Context, ipID int64) ([]character.Character, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []character.Character
	for _, c := range f.created {
		if c.ipID == ipID {
			out = append(out, character.Character{ID: int64(len(out) + 1), IPID: ipID, Name: c.name})
		}
	}
	return out, nil
}

type chainScenes struct{ created []scene.SceneInput }

func (f *chainScenes) CreateScene(_ context.Context, ipID int64, in scene.SceneInput) (scene.Scene, error) {
	f.created = append(f.created, in)
	return scene.Scene{ID: int64(len(f.created)), IPID: ipID, Name: in.Name}, nil
}

func (f *chainScenes) ListScenes(context.Context, int64) ([]scene.Scene, error) { return nil, nil }

type chainProps struct{ created []prop.PropInput }

func (f *chainProps) CreateProp(_ context.Context, ipID int64, in prop.PropInput) (prop.Prop, error) {
	f.created = append(f.created, in)
	return prop.Prop{ID: int64(len(f.created)), IPID: ipID, Name: in.Name}, nil
}

func (f *chainProps) ListProps(context.Context, int64) ([]prop.Prop, error) { return nil, nil }

// chainCanvases 以编辑器 snapshot 形状回答画布 7(带原文 script 节点)。
type chainCanvases struct{}

func (chainCanvases) Get(_ context.Context, id int64) (canvas.Canvas, error) {
	if id != 7 {
		return canvas.Canvas{}, canvas.ErrNotFound
	}
	graph := `{"nodes":[
		{"id":"script-src-1","type":"script","position":{"x":0,"y":0},"data":{"title":"原文:雾都夜行","text":"雾都夜行原文正文。"}},
		{"id":"agent-1-1","type":"agent","position":{"x":10,"y":10},"data":{"text":""}}
	],"edges":[]}`
	return canvas.Canvas{ID: 7, Name: "EP01", Version: 1, IPID: 3, Graph: []byte(graph)}, nil
}

// relayMultiToolRound 拼一轮多工具调用的 SSE 全文(并行 tool_calls 按
// index 聚合的真实形状)。
func relayMultiToolRound(t *testing.T, calls []struct{ id, name string; args map[string]any }) string {
	t.Helper()
	var b strings.Builder
	for i, tc := range calls {
		argsRaw, err := json.Marshal(tc.args)
		if err != nil {
			t.Fatalf("marshal args: %v", err)
		}
		b.WriteString(relayChunk(t, map[string]any{
			"choices": []any{map[string]any{"delta": map[string]any{"tool_calls": []any{map[string]any{
				"index": i, "id": tc.id, "type": "function",
				"function": map[string]any{"name": tc.name, "arguments": string(argsRaw)},
			}}}}},
		}))
	}
	b.WriteString(relayChunk(t, map[string]any{
		"choices": []any{map[string]any{"delta": map[string]any{}, "finish_reason": "tool_calls"}},
	}))
	b.WriteString("data: [DONE]\n\n")
	return b.String()
}

// 全链主干:一轮 create → 一轮 list → 一轮并行 save_script + save_storyboard
// → 文本收尾。验:IP 作用域、库表直写、读库反馈回流、写图草案 payload、
// 事件序与 done 摘要。
func TestAgentStreamPipelineToolsFullChain(t *testing.T) {
	rounds := []string{
		relayToolRound(t, "", "call_1", "create_character", map[string]any{"name": "林小满", "alias": "小满"}),
		relayToolRound(t, "", "call_2", "list_assets", map[string]any{}),
		relayMultiToolRound(t, []struct {
			id   string
			name string
			args map[string]any
		}{
			{"call_3", "save_script", map[string]any{"title": "第1集 雾都夜行", "text": "【第1场|内|夜|仓库】\n林小满:……"}},
			{"call_4", "save_storyboard", map[string]any{"title": "EP01 分镜", "cuts": []any{
				map[string]any{"shot": "远", "lines": "城市空镜", "camera_moves": []any{"推"}, "character_ids": []any{1}},
			}}},
		}),
		relayTextRound(t, "第 1 集剧本与分镜已落图。"),
	}
	relay := &fakeRelay{rounds: rounds}
	relayServer := httptest.NewServer(relay.handler())
	t.Cleanup(relayServer.Close)

	characters := &chainCharacters{}
	registry := agenttools.NewRegistry(agenttools.Deps{
		Characters: characters,
		Scenes:     &chainScenes{},
		Props:      &chainProps{},
		Canvases:   chainCanvases{},
	})

	gin.SetMode(gin.TestMode)
	engine := gin4Test(t, registry, promptgen.NewClient(relayServer.URL, "sk-relay"))
	server := httptest.NewServer(engine)
	t.Cleanup(server.Close)

	res, raw := http4Test(t, server, map[string]any{
		"node_id": "agent-1-1", "topic": "读原文写第 1 集", "model": "chat-m",
	})
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, body = %s", res.StatusCode, raw)
	}

	// 事件序:4 组 tool_call/tool_result + delta + done + [DONE](无
	// character_section 注入,流首直接是工具帧)。
	var calls, results []map[string]any
	var done map[string]any
	for _, payload := range sseFrames(t, raw) {
		m := frameMap(t, payload)
		if m == nil {
			continue
		}
		switch m["type"] {
		case "tool_call":
			calls = append(calls, m)
		case "tool_result":
			results = append(results, m)
		case "done":
			done = m
		}
	}
	if len(calls) != 4 || len(results) != 4 {
		t.Fatalf("calls/results = %d/%d, want 4/4", len(calls), len(results))
	}
	for i, want := range []string{"create_character", "list_assets", "save_script", "save_storyboard"} {
		if calls[i]["name"] != want {
			t.Errorf("call[%d] = %v, want %s(按序执行)", i, calls[i], want)
		}
	}

	// 库表类直写:角色入库且挂在画布所属 IP(3)下。
	if len(characters.created) != 1 || characters.created[0].ipID != 3 || characters.created[0].name != "林小满" {
		t.Fatalf("characters.created = %+v, want 林小满 under ip 3", characters.created)
	}
	// 读库反馈回流:第 3 轮请求的 messages 里,tool 结果带上了新建角色。
	msgs3, _ := relay.request(2).Body["messages"].([]any)
	var feedback strings.Builder
	for _, m := range msgs3 {
		if mm, ok := m.(map[string]any); ok && mm["role"] == "tool" {
			feedback.WriteString(mm["content"].(string))
		}
	}
	if !strings.Contains(feedback.String(), "林小满") {
		t.Errorf("round-3 tool feedback = %s, want the created character visible to the model", feedback.String())
	}

	// 写图类草案:save_script payload = {title,text};save_storyboard 的
	// cuts 画面空(asset_id 0)且带 shot/character_ids。
	byName := map[string]map[string]any{}
	for _, r := range results {
		byName[r["name"].(string)] = r
	}
	scriptPayload, _ := byName["save_script"]["payload"].(map[string]any)
	if scriptPayload["title"] != "第1集 雾都夜行" || !strings.Contains(scriptPayload["text"].(string), "第1场") {
		t.Errorf("save_script payload = %v, want {title,text}", scriptPayload)
	}
	sbPayload, _ := byName["save_storyboard"]["payload"].(map[string]any)
	cuts, _ := sbPayload["cuts"].([]any)
	if len(cuts) != 1 {
		t.Fatalf("storyboard cuts = %v", sbPayload["cuts"])
	}
	cut := cuts[0].(map[string]any)
	if cut["asset_id"].(float64) != 0 || cut["shot"] != "远" {
		t.Errorf("cut = %v, want empty frame + shot", cut)
	}
	if ids, _ := cut["character_ids"].([]any); len(ids) != 1 || ids[0].(float64) != 1 {
		t.Errorf("cut character_ids = %v, want [1]", cut["character_ids"])
	}

	// 收尾:done.text = 最终回答,summary 汇总四工具。
	if done == nil || done["text"] != "第 1 集剧本与分镜已落图。" {
		t.Fatalf("done = %v, want the final answer", done)
	}
	summary := done["summary"].(string)
	for _, want := range []string{"create_character", "list_assets", "save_script", "save_storyboard"} {
		if !strings.Contains(summary, want) {
			t.Errorf("done summary = %q, want %s in the tool line", summary, want)
		}
	}

	// 上行:第一轮即带 8 个工具定义 + 导演 system,来源 gen=agent。
	req0 := relay.request(0)
	tools, _ := req0.Body["tools"].([]any)
	if len(tools) != 8 {
		t.Errorf("round-1 tools = %d, want the eight definitions", len(tools))
	}
	msgs0, _ := req0.Body["messages"].([]any)
	if msgs0[0].(map[string]any)["role"] != "system" {
		t.Errorf("round-1 messages[0] = %v, want the director system", msgs0[0])
	}
	// 原文可读:模型发出的 read_source 不在本轮脚本里,但画布原文节点在
	// 注册表可见 —— 单独直调一次验形状(不经网关)。
	if tool, ok := registry.Lookup("read_source"); ok {
		scopeCtx := promptgen.WithAgentScope(context.Background(), promptgen.AgentScope{CanvasID: 7, IPID: 3})
		args, _ := json.Marshal(map[string]any{})
		summary, payload, err := tool.Execute(scopeCtx, args)
		if err != nil {
			t.Fatalf("read_source: %v", err)
		}
		if !strings.Contains(summary, "script-src-1") || !strings.Contains(summary, "雾都夜行") {
			t.Errorf("read_source summary = %q, want the source node catalog", summary)
		}
		var pm map[string]any
		if json.Unmarshal(payload, &pm) != nil || pm["nodes"] == nil {
			t.Errorf("read_source payload = %s, want the nodes array", payload)
		}
	}
	if req0.Source != "canvas=7 node=agent-1-1 gen=agent" {
		t.Errorf("source = %q, want the agent origin mark", req0.Source)
	}
}

// gin4Test/http4Test 拼全链用的 gin 引擎与请求(不依赖 handler_test.go 的
// newHandlerEnv —— 那边的装配固定了 fake store 族)。
func gin4Test(t *testing.T, registry *promptgen.AgentToolRegistry, gateway promptgen.Gateway) http.Handler {
	t.Helper()
	engine := gin.New()
	promptgen.RegisterRoutes(engine.Group("/canvases"), &promptgen.Handlers{
		Templates: fakeTemplates{byID: map[int64]prompttemplate.Template{}},
		Canvases:  chainCanvases{},
		Assets:    &fakeAssets{byID: map[int64]asset.Asset{}},
		Characters: fakeCharacters{byID: map[int64]character.Character{}},
		Models: fakePrices{byModel: map[string]pricing.Price{
			"chat-m": {PublicModel: "chat-m", Unit: pricing.UnitToken, Token: &pricing.TokenPrice{}},
		}},
		Gateway:    gateway,
		AgentTools: registry,
	})
	return engine
}

func http4Test(t *testing.T, server *httptest.Server, body map[string]any) (*http.Response, string) {
	t.Helper()
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal body: %v", err)
	}
	res, err := server.Client().Post(server.URL+"/canvases/7/agent/stream", "application/json", strings.NewReader(string(raw)))
	if err != nil {
		t.Fatalf("do request: %v", err)
	}
	t.Cleanup(func() { _ = res.Body.Close() })
	payload, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	return res, string(payload)
}
