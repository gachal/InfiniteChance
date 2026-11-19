package assistant

// read_canvas(91 号票):读整图结构 —— 节点清单(id/类型/标题/prompt
// 与正文摘要/坐标)+ 边关系。图形制对后端是不透明 JSON(ADR 0001 先例),
// 这里按编辑器 snapshot 形状(nodes[].{id,type,position,data} /
// edges[].{id,source,target})只读解析。护栏钉测对象:单节点内容摘要
// ≤ readCanvasSummaryRunes rune、节点清单 ≤ readCanvasMaxNodes 个(超出
// 截断并在摘要注明)—— 大画布防 prompt 爆炸。

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/gachal/InfiniteChance/internal/promptgen"
)

const (
	// readCanvasSummaryRunes 是单节点内容(title/prompt/text)摘要上限:
	// 模型只需要「这个节点是什么」,长文用 read_source 分段精读。
	readCanvasSummaryRunes = 120
	// readCanvasMaxNodes 是节点清单上限:超出截断(按图内顺序取前 N 个)。
	readCanvasMaxNodes = 200
)

// canvasGraphNode / canvasGraphEdge 是整图 JSON 里本工具消费的最小只读
// 形状(agenttools.graphNode 同族)。data 按需解 title/text/prompt:
// 图片/视频节点的创作内容在 prompt,剧本系在 text,其余类型仅标题。
type canvasGraphNode struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	Position struct {
		X float64 `json:"x"`
		Y float64 `json:"y"`
	} `json:"position"`
	Data struct {
		Title  *string `json:"title"`
		Text   *string `json:"text"`
		Prompt *string `json:"prompt"`
	} `json:"data"`
}

type canvasGraphEdge struct {
	ID     string `json:"id"`
	Source string `json:"source"`
	Target string `json:"target"`
}

type readCanvasTool struct {
	deps Deps
}

func newReadCanvas(deps Deps) promptgen.AgentTool { return readCanvasTool{deps: deps} }

func (t readCanvasTool) Name() string { return "read_canvas" }

func (t readCanvasTool) Definition() json.RawMessage {
	return functionDefinition(t.Name(),
		"读取当前画布的结构:节点清单(id/类型/标题/内容摘要/坐标)与全部连线。写操作之前先读图;修改某个节点时用它检查相连的下游节点,主动向用户提示连带影响(系统不会自动级联)。长文正文请配合 read_source 分段精读。",
		map[string]any{
			"type":       "object",
			"properties": map[string]any{},
		})
}

func (t readCanvasTool) Execute(ctx context.Context, args json.RawMessage) (string, json.RawMessage, error) {
	scope, ok := promptgen.AgentScopeFrom(ctx)
	if !ok {
		return "", nil, fmt.Errorf("工具缺少画布上下文,请通过画布助手会话调用")
	}
	if t.deps.Canvases == nil {
		return "", nil, fmt.Errorf("画布存储未装配,无法读取画布结构")
	}
	cv, err := t.deps.Canvases.Get(ctx, scope.CanvasID)
	if err != nil {
		return "", nil, fmt.Errorf("读取画布失败:%w", err)
	}
	var doc struct {
		Nodes []canvasGraphNode `json:"nodes"`
		Edges []canvasGraphEdge `json:"edges"`
	}
	if len(cv.Graph) > 0 {
		if err := json.Unmarshal(cv.Graph, &doc); err != nil {
			return "", nil, fmt.Errorf("画布图不是可解析的 JSON:%w", err)
		}
	}

	truncated := len(doc.Nodes) > readCanvasMaxNodes
	listed := doc.Nodes
	if truncated {
		listed = doc.Nodes[:readCanvasMaxNodes]
	}

	type nodeRow struct {
		ID      string  `json:"id"`
		Type    string  `json:"type"`
		Title   string  `json:"title,omitempty"`
		Prompt  string  `json:"prompt,omitempty"`
		Text    string  `json:"text,omitempty"`
		X       float64 `json:"x"`
		Y       float64 `json:"y"`
		CutMore bool    `json:"summary_truncated,omitempty"`
	}
	rows := make([]nodeRow, 0, len(listed))
	for _, n := range listed {
		row := nodeRow{ID: n.ID, Type: n.Type, X: n.Position.X, Y: n.Position.Y}
		if n.Data.Title != nil {
			row.Title, row.CutMore = summarize(n.Data.Title, row.CutMore)
		}
		if n.Data.Prompt != nil {
			row.Prompt, row.CutMore = summarize(n.Data.Prompt, row.CutMore)
		}
		if n.Data.Text != nil {
			row.Text, row.CutMore = summarize(n.Data.Text, row.CutMore)
		}
		rows = append(rows, row)
	}

	edges := make([]map[string]any, 0, len(doc.Edges))
	edgeTitles := make([]string, 0, len(doc.Edges))
	for _, e := range doc.Edges {
		edges = append(edges, map[string]any{"id": e.ID, "source": e.Source, "target": e.Target})
		edgeTitles = append(edgeTitles, fmt.Sprintf("%s→%s", e.Source, e.Target))
	}

	payload, err := json.Marshal(map[string]any{
		"nodes":      rows,
		"edges":      edges,
		"node_count": len(doc.Nodes),
		"truncated":  truncated,
	})
	if err != nil {
		return "", nil, err
	}
	summary := fmt.Sprintf("画布上有 %d 个节点、%d 条边:%s", len(doc.Nodes), len(doc.Edges), strings.Join(edgeTitles, "、"))
	if summary == fmt.Sprintf("画布上有 %d 个节点、%d 条边:", len(doc.Nodes), len(doc.Edges)) {
		summary = "画布是空的:没有节点与连线"
	}
	if truncated {
		summary += fmt.Sprintf("(节点清单只展开前 %d 个)", readCanvasMaxNodes)
	}
	return summary, payload, nil
}

// summarize 截断一个内容字段到 readCanvasSummaryRunes rune,截过置位
// cutMore(节点的 summary_truncated,提示模型用 read_source 精读)。
func summarize(value *string, cutMore bool) (string, bool) {
	if value == nil {
		return "", cutMore
	}
	runes := []rune(*value)
	if len(runes) <= readCanvasSummaryRunes {
		return *value, cutMore
	}
	return string(runes[:readCanvasSummaryRunes]) + "…", true
}
