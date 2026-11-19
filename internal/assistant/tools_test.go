package assistant

// 工具面测试(91 号票):注册表形状钉测、read_canvas 护栏、草案参数
// 校验(草案形状钉测)。

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/gachal/InfiniteChance/internal/canvas"
	"github.com/gachal/InfiniteChance/internal/promptgen"
)

// fakeCanvases 是 read_canvas/set_style 的画布只读桩(agenttools.CanvasSource
// 接口形状)。
type fakeCanvases struct {
	graph map[int64][]byte
}

func (f fakeCanvases) Get(_ context.Context, id int64) (canvas.Canvas, error) {
	g, ok := f.graph[id]
	if !ok {
		return canvas.Canvas{}, canvas.ErrNotFound
	}
	return canvas.Canvas{ID: id, Graph: g}, nil
}

// TestRegistryExposesAssistantToolFace 钉注册表成员与暴露序(七新 +
// 三复用;复用三件注册实现,不复制)。
func TestRegistryExposesAssistantToolFace(t *testing.T) {
	hub := NewHub()
	registry := NewRegistry(Deps{Hub: hub})
	want := []string{
		"read_canvas",
		"create_nodes", "update_nodes", "delete_nodes",
		"connect_nodes", "disconnect_nodes", "trigger_task",
		"list_assets", "read_source", "set_style",
	}
	// Definitions 与 Names 同序:逐个解析定义,函数名对表。
	defs := registry.Definitions()
	if len(defs) != len(want) {
		t.Fatalf("definitions = %d, want %d", len(defs), len(want))
	}
	for i, raw := range defs {
		var def struct {
			Type     string `json:"type"`
			Function struct {
				Name        string          `json:"name"`
				Description string          `json:"description"`
				Parameters  json.RawMessage `json:"parameters"`
			} `json:"function"`
		}
		if err := json.Unmarshal(raw, &def); err != nil {
			t.Fatalf("definition %d not JSON: %v", i, err)
		}
		if def.Type != "function" || def.Function.Name != want[i] || def.Function.Description == "" || len(def.Function.Parameters) == 0 {
			t.Fatalf("definition %d = %s, want function schema for %q", i, raw, want[i])
		}
	}
	// lookup 面:七个草案 + read_canvas 都能按名找到。
	for _, name := range want {
		if _, ok := registry.Lookup(name); !ok {
			t.Errorf("tool %q not registered", name)
		}
	}
}

// TestReadCanvasGuardrails 钉摘要限长与节点清单截断(票面裁量注记的
// 「大画布防 prompt 爆炸」护栏)。
func TestReadCanvasGuardrails(t *testing.T) {
	// 250 个节点:前 200 展开;其中 1 号节点 prompt 超长。
	type node struct {
		ID       string `json:"id"`
		Type     string `json:"type"`
		Position struct {
			X float64 `json:"x"`
			Y float64 `json:"y"`
		} `json:"position"`
		Data struct {
			Title  string `json:"title"`
			Prompt string `json:"prompt"`
			Text   string `json:"text"`
		} `json:"data"`
	}
	var doc struct {
		Nodes []node `json:"nodes"`
		Edges []map[string]any `json:"edges"`
	}
	for i := 0; i < 250; i++ {
		n := node{ID: strings.Repeat("n", 1) + itoa(i), Type: "image"}
		n.Position.X = float64(i)
		n.Position.Y = float64(-i)
		if i == 1 {
			n.Data.Prompt = strings.Repeat("长", readCanvasSummaryRunes+50)
		}
		doc.Nodes = append(doc.Nodes, n)
	}
	doc.Edges = append(doc.Edges, map[string]any{"id": "e-0-1", "source": doc.Nodes[0].ID, "target": doc.Nodes[1].ID})
	raw, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	tol := readCanvasTool{deps: Deps{Canvases: fakeCanvases{graph: map[int64][]byte{7: raw}}}}
	ctx := promptgen.WithAgentScope(context.Background(), promptgen.AgentScope{CanvasID: 7, IPID: 3})

	summary, payload, err := tol.Execute(ctx, nil)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	var out struct {
		Nodes []struct {
			ID      string `json:"id"`
			Type    string `json:"type"`
			Prompt  string `json:"prompt"`
			X       float64 `json:"x"`
			CutMore bool   `json:"summary_truncated"`
		} `json:"nodes"`
		Edges     []map[string]any `json:"edges"`
		NodeCount int              `json:"node_count"`
		Truncated bool             `json:"truncated"`
	}
	if err := json.Unmarshal(payload, &out); err != nil {
		t.Fatalf("payload: %v", err)
	}
	if out.NodeCount != 250 || !out.Truncated || len(out.Nodes) != readCanvasMaxNodes {
		t.Fatalf("node_count=%d truncated=%v rows=%d, want 250/true/200", out.NodeCount, out.Truncated, len(out.Nodes))
	}
	if !strings.Contains(summary, "只展开前 200 个") {
		t.Errorf("summary = %q, want the truncation note", summary)
	}
	long := out.Nodes[1]
	if runes := len([]rune(long.Prompt)); runes != readCanvasSummaryRunes+1 { // +1 = 尾部省略号
		t.Errorf("long prompt = %d runes, want %d (+ellipsis)", runes, readCanvasSummaryRunes+1)
	}
	if !long.CutMore {
		t.Error("summary_truncated not set on the cut node")
	}
	if len(out.Edges) != 1 || out.Edges[0]["source"] != doc.Nodes[0].ID {
		t.Errorf("edges = %v, want the single edge", out.Edges)
	}
}

// TestDraftValidationPins 钉草案工具的形状校验(失败不挂起、给模型可读
// 原因):未知节点类型 / 超批量 / trigger_task 的 kind 白名单与参考上限。
func TestDraftValidationPins(t *testing.T) {
	if _, err := validateCreateNodes(map[string]any{"nodes": []any{map[string]any{"type": "music"}}}); err == nil || !strings.Contains(err.Error(), "不是合法的节点类型") {
		t.Errorf("unknown type err = %v, want type rejection", err)
	}
	many := make([]any, maxBatchNodes+1)
	for i := range many {
		many[i] = map[string]any{"type": "image"}
	}
	if _, err := validateCreateNodes(map[string]any{"nodes": many}); err == nil || !strings.Contains(err.Error(), "分批") {
		t.Errorf("oversize batch err = %v, want batch limit", err)
	}
	if _, err := validateTriggerTask(map[string]any{"node_id": "a", "model": "m", "prompt": "p", "kind": "music"}); err == nil || !strings.Contains(err.Error(), "只支持 image 或 video") {
		t.Errorf("bad kind err = %v, want kind whitelist", err)
	}
	urls := make([]any, maxTaskRefs+1)
	for i := range urls {
		urls[i] = "https://x"
	}
	if _, err := validateTriggerTask(map[string]any{"node_id": "a", "model": "m", "prompt": "p", "image_urls": urls}); err == nil || !strings.Contains(err.Error(), "最多 9 条") {
		t.Errorf("too many refs err = %v, want ref cap", err)
	}
	if _, err := validateSimpleEdges(map[string]any{"edges": []any{map[string]any{"source": "a"}}}, "connect"); err == nil || !strings.Contains(err.Error(), "target") {
		t.Errorf("missing target err = %v, want target required", err)
	}
	// 合法形状返回卡片摘要(变更清单文案)。
	line, err := validateCreateNodes(map[string]any{
		"nodes": []any{map[string]any{"type": "image", "prompt": "立绘"}, map[string]any{"type": "script", "text": "第一集"}},
		"edges": []any{map[string]any{"from": float64(0), "to": float64(1)}},
	})
	if err != nil || line != "新建 2 个节点、1 条连线" {
		t.Errorf("card line = %q (err %v), want batch summary", line, err)
	}
}

// TestSystemPromptDeclaresMode 钉授权档进系统提示词(两档行为差异的
// 服务端声明面)。
func TestSystemPromptDeclaresMode(t *testing.T) {
	if !strings.Contains(SystemPrompt(ModeAsk), "先询问我") || strings.Contains(SystemPrompt(ModeAsk), "自动应用") {
		t.Error("ask mode prompt wrong")
	}
	if !strings.Contains(SystemPrompt(ModeFull), "完全授权") || !strings.Contains(SystemPrompt(ModeFull), "自动应用") {
		t.Error("full mode prompt wrong")
	}
}

