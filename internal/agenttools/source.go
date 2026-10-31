package agenttools

// read_source(66 号票定案 1):按 script 节点 id + rune 区间分段读原文,
// 避免 10 万字全文进每轮对话。图形制对后端是不透明 JSON(ADR 0001 先例),
// 这里按编辑器 snapshot 的形状(nodes[].id/type/data)只读解析,不改图;
// 省略 node_id 时返回画布上全部剧本节点的目录(id/标题/字数)—— 模型凭
// 它拿到可读的 node_id 再分段消费。

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/gachal/InfiniteChance/internal/promptgen"
)

// sourceSegmentRunes 是缺省的读取段长(66 号票:区间 1-2 万 rune/段,取
// 中值;模型可用 length 参数在窗口内自调)。
const sourceSegmentRunes = 12000

// graphNode 是整图 JSON 里本工具消费的最小节点形状(snapshot 形状:
// nodes[].{id,type,data};data 按需解 text/title)。
type graphNode struct {
	ID   string `json:"id"`
	Type string `json:"type"`
	Data struct {
		Title *string `json:"title"`
		Text  *string `json:"text"`
	} `json:"data"`
}

type readSourceTool struct {
	deps Deps
}

func newReadSource(deps Deps) promptgen.AgentTool { return readSourceTool{deps: deps} }

func (t readSourceTool) Name() string { return "read_source" }

func (t readSourceTool) Definition() json.RawMessage {
	return functionDefinition(t.Name(),
		"分段读取画布上剧本(script)节点的正文(小说原文/大纲/剧本)。长文按区间逐段读完再动笔;省略 node_id 返回全部剧本节点目录。",
		map[string]any{
			"type": "object",
			"properties": map[string]any{
				"node_id": map[string]any{"type": "string", "description": "剧本节点的 id;省略 = 列出画布上全部剧本节点的目录"},
				"start":   map[string]any{"type": "number", "description": "起始位置(按字符计,从 1 开始),缺省 1"},
				"length":  map[string]any{"type": "number", "description": fmt.Sprintf("本次读取的字符数,缺省 %d", sourceSegmentRunes)},
			},
		})
}

func (t readSourceTool) Execute(ctx context.Context, args json.RawMessage) (string, json.RawMessage, error) {
	scope, err := scopeOf(ctx)
	if err != nil {
		return "", nil, err
	}
	m, err := argsOf(args)
	if err != nil {
		return "", nil, err
	}
	nodeID, err := stringArg(m, "node_id")
	if err != nil {
		return "", nil, err
	}
	if strings.TrimSpace(nodeID) == "" {
		return t.listScriptNodes(ctx, scope.CanvasID)
	}
	return t.readSegment(ctx, scope.CanvasID, strings.TrimSpace(nodeID), m)
}

// scriptNodes parses the canvas graph and answers the script-typed nodes
// carrying a text field(64 号票:剧本节点一个类型三种用途,正文都在
// data.text)。其他类型节点(如 agent 的会话文本)不在读取范围内。
func (t *readSourceTool) scriptNodes(ctx context.Context, canvasID int64) ([]graphNode, error) {
	if t.deps.Canvases == nil {
		return nil, fmt.Errorf("画布存储未装配,无法读取原文")
	}
	cv, err := t.deps.Canvases.Get(ctx, canvasID)
	if err != nil {
		return nil, fmt.Errorf("读取画布失败:%w", err)
	}
	var doc struct {
		Nodes []graphNode `json:"nodes"`
	}
	if len(cv.Graph) > 0 {
		if err := json.Unmarshal(cv.Graph, &doc); err != nil {
			return nil, fmt.Errorf("画布图不是可解析的 JSON:%w", err)
		}
	}
	var out []graphNode
	for _, n := range doc.Nodes {
		if n.Type != "script" || n.Data.Text == nil {
			continue
		}
		out = append(out, n)
	}
	return out, nil
}

func (t *readSourceTool) listScriptNodes(ctx context.Context, canvasID int64) (string, json.RawMessage, error) {
	nodes, err := t.scriptNodes(ctx, canvasID)
	if err != nil {
		return "", nil, err
	}
	rows := make([]map[string]any, 0, len(nodes))
	var titles []string
	for _, n := range nodes {
		if strings.TrimSpace(*n.Data.Text) == "" {
			continue // 空正文节点不进目录(无可消费物,直接按 id 读会另报「没有正文」)
		}
		title := ""
		if n.Data.Title != nil {
			title = *n.Data.Title
		}
		rows = append(rows, map[string]any{"id": n.ID, "title": title, "chars": runeLen(*n.Data.Text)})
		titles = append(titles, fmt.Sprintf("%s(%s,%d 字)", n.ID, title, runeLen(*n.Data.Text)))
	}
	if len(rows) == 0 {
		return "画布上没有带正文的剧本节点", json.RawMessage(`{"nodes":[]}`), nil
	}
	summary := fmt.Sprintf("画布上有 %d 个剧本节点:%s", len(rows), strings.Join(titles, "、"))
	payload, err := json.Marshal(map[string]any{"nodes": rows})
	if err != nil {
		return summary, nil, nil
	}
	return summary, payload, nil
}

// readSegment answers one rune range [start, start+length-1](1 起计,双端
// 闭区间;超尾截到文末并在摘要注明)。
func (t *readSourceTool) readSegment(ctx context.Context, canvasID int64, nodeID string, m map[string]any) (string, json.RawMessage, error) {
	nodes, err := t.scriptNodes(ctx, canvasID)
	if err != nil {
		return "", nil, err
	}
	var target *graphNode
	for i := range nodes {
		if nodes[i].ID == nodeID {
			target = &nodes[i]
			break
		}
	}
	if target == nil {
		return "", nil, fmt.Errorf("画布上没有找到 id 为 %s 的剧本节点;省略 node_id 可先取目录", nodeID)
	}
	text := *target.Data.Text
	if strings.TrimSpace(text) == "" {
		return "", nil, fmt.Errorf("节点 %s 没有正文", nodeID)
	}
	total := runeLen(text)

	start := 1
	if v, ok, err := numberArg(m, "start"); err != nil {
		return "", nil, err
	} else if ok {
		start = int(v)
	}
	length := sourceSegmentRunes
	if v, ok, err := numberArg(m, "length"); err != nil {
		return "", nil, err
	} else if ok {
		if v <= 0 {
			return "", nil, fmt.Errorf("参数 length 必须是正数")
		}
		length = int(v)
	}
	if start < 1 {
		start = 1
	}
	if start > total {
		return "", nil, fmt.Errorf("起点 %d 超出正文长度 %d,请从更小的位置读起", start, total)
	}

	runes := []rune(text)
	end := start - 1 + length
	if end > total {
		end = total
	}
	segment := string(runes[start-1 : end])
	atEnd := end == total

	title := ""
	if target.Data.Title != nil {
		title = *target.Data.Title
	}
	summary := fmt.Sprintf("已读「%s」第 %d-%d 字(共 %d 字)", title, start, end, total)
	if atEnd {
		summary += ",已到文末"
	}
	payload, err := json.Marshal(map[string]any{
		"node_id": nodeID, "title": title,
		"start": start, "end": end, "total_chars": total,
		"text": segment,
	})
	if err != nil {
		return summary, nil, nil
	}
	return summary, payload, nil
}
