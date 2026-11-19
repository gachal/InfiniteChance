package assistant

// 六个写图草案工具(91 号票):create/update/delete_nodes、connect/
// disconnect_nodes、trigger_task。全部经 draftCore 壳「校验 → 挂起 →
// 回执」:工具不发图,前端应用器落图(新建节点 id 由前端生成、回执带回
// 供后续引用);trigger_task 的前端应用 = 走既有 POST /canvases/:id/tasks
// 与生成对话框完全同链路(计费/转存/回填零新风险)。
//
// create_nodes.edges 的 from/to 是「本批节点数组下标(number)或既有节点
// id(string)」的联合 —— 新节点还没有 id,同批连线按下标引用。

import (
	"fmt"

	"github.com/gachal/InfiniteChance/internal/promptgen"
)

// newCreateNodes wires create_nodes:批量建节点(类型/标题/内容/坐标/
// 连线),坐标缺省 = 前端视口落位(级联偏移照既有语义)。
func newCreateNodes(hub *Hub) promptgen.AgentTool {
	return draftCore{
		name: "create_nodes",
		description: "在当前画布上批量新建节点并可选连线。节点类型:agent(Agent 工作台)/image(图片)/video(视频)/analysis(分析)/previz(预演台)/character(角色)/storyboard(分镜)/name(分镜稿)/level(关卡)/voice(音色)/audio(音频)/script(剧本)/group(分组)/panorama(全景)。图片/视频节点把创作内容放 prompt,剧本系节点放 text;x/y 省略 = 前端在当前视口自动落位。edges 的 from/to 引用本批 nodes 的数组下标(数字)或画布上既有节点的 id(字符串)。一次调用完成一批,不要逐个建。",
		parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"nodes": map[string]any{
					"type": "array",
					"description": fmt.Sprintf("要新建的节点清单(最多 %d 个)", maxBatchNodes),
					"items": map[string]any{
						"type": "object",
						"properties": map[string]any{
							"type":  map[string]any{"type": "string", "description": "节点类型(见工具说明)"},
							"title": map[string]any{"type": "string", "description": "节点标题,可省略"},
							"prompt": map[string]any{"type": "string", "description": "创作提示词(image/video/panorama 节点)"},
							"text":  map[string]any{"type": "string", "description": "正文文本(script/agent 节点)"},
							"x":     map[string]any{"type": "number", "description": "画布坐标 x,省略 = 自动落位"},
							"y":     map[string]any{"type": "number", "description": "画布坐标 y,省略 = 自动落位"},
						},
						"required": []string{"type"},
					},
				},
				"edges": map[string]any{
					"type": "array",
					"description": "随批建立的连线(最多 %d 条);from/to 是本批下标或既有节点 id",
					"items": map[string]any{
						"type": "object",
						"properties": map[string]any{
							"from": map[string]any{"description": "起点:本批下标(number)或既有节点 id(string)"},
							"to":   map[string]any{"description": "终点:本批下标(number)或既有节点 id(string)"},
						},
					},
				},
			},
			"required": []string{"nodes"},
		},
		validate: validateCreateNodes,
		hub:      hub,
	}
}

func validateCreateNodes(args map[string]any) (string, error) {
	nodes, err := arrayField(args, "nodes")
	if err != nil {
		return "", err
	}
	if len(nodes) == 0 {
		return "", fmt.Errorf("参数 nodes 不能为空数组")
	}
	if len(nodes) > maxBatchNodes {
		return "", fmt.Errorf("一次最多新建 %d 个节点,请分批提交", maxBatchNodes)
	}
	for i, raw := range nodes {
		m, ok := raw.(map[string]any)
		if !ok {
			return "", fmt.Errorf("nodes[%d] 必须是对象", i)
		}
		typ, err := requiredString(m, "type")
		if err != nil {
			return "", fmt.Errorf("nodes[%d].%v", i, err)
		}
		if !canvasNodeTypes[typ] {
			return "", fmt.Errorf("nodes[%d].type %q 不是合法的节点类型", i, typ)
		}
		for _, field := range []string{"title", "prompt", "text"} {
			v, err := stringField(m, field)
			if err != nil {
				return "", fmt.Errorf("nodes[%d].%v", i, err)
			}
			if err := limitArg(fmt.Sprintf("nodes[%d].%s", i, field), v); err != nil {
				return "", err
			}
		}
	}
	edges, err := validateEdgeList(args, "edges", maxBatchEdges)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("新建 %d 个节点、%d 条连线", len(nodes), edges), nil
}

// validateEdgeList 校验 from/to 联合引用:数字 = 本批下标,字符串 = 既有
// 节点 id;其余形状报错。返回边数(卡片摘要用)。
func validateEdgeList(args map[string]any, key string, max int) (int, error) {
	edges, err := arrayField(args, key)
	if err != nil {
		return 0, err
	}
	if len(edges) > max {
		return 0, fmt.Errorf("一次最多操作 %d 条连线,请分批提交", max)
	}
	for i, raw := range edges {
		m, ok := raw.(map[string]any)
		if !ok {
			return 0, fmt.Errorf("%s[%d] 必须是对象", key, i)
		}
		if _, err := edgeEndpoint(m, "from", key, i); err != nil {
			return 0, err
		}
		if _, err := edgeEndpoint(m, "to", key, i); err != nil {
			return 0, err
		}
	}
	return len(edges), nil
}

func edgeEndpoint(m map[string]any, field, key string, i int) (string, error) {
	v, ok := m[field]
	if !ok || v == nil {
		return "", fmt.Errorf("%s[%d].%s 不能为空", key, i, field)
	}
	switch t := v.(type) {
	case float64:
		return fmt.Sprintf("#%d", int(t)), nil
	case string:
		if t == "" {
			return "", fmt.Errorf("%s[%d].%s 不能为空字符串", key, i, field)
		}
		return t, nil
	default:
		return "", fmt.Errorf("%s[%d].%s 必须是数字(本批下标)或字符串(节点 id)", key, i, field)
	}
}

// newUpdateNodes wires update_nodes:批量改节点内容。
func newUpdateNodes(hub *Hub) promptgen.AgentTool {
	return draftCore{
		name: "update_nodes",
		description: "批量修改画布上既有节点的内容:图片/视频节点改 prompt,剧本系节点改 text,均可改标题 title。每个节点至少给一个要改的字段;整段覆盖而不是追加。改节点时先用 read_canvas 检查与它相连的下游节点,主动向用户提示连带影响。",
		parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"nodes": map[string]any{
					"type": "array",
					"description": fmt.Sprintf("要修改的节点清单(最多 %d 个)", maxBatchNodes),
					"items": map[string]any{
						"type": "object",
						"properties": map[string]any{
							"id":    map[string]any{"type": "string", "description": "目标节点 id(read_canvas 或新建回执给出)"},
							"title": map[string]any{"type": "string", "description": "改为该标题"},
							"prompt": map[string]any{"type": "string", "description": "改为该创作提示词(image/video/panorama)"},
							"text":  map[string]any{"type": "string", "description": "改为该正文(script/agent)"},
						},
						"required": []string{"id"},
					},
				},
			},
			"required": []string{"nodes"},
		},
		validate: func(args map[string]any) (string, error) {
			nodes, err := arrayField(args, "nodes")
			if err != nil {
				return "", err
			}
			if len(nodes) == 0 {
				return "", fmt.Errorf("参数 nodes 不能为空数组")
			}
			if len(nodes) > maxBatchNodes {
				return "", fmt.Errorf("一次最多修改 %d 个节点,请分批提交", maxBatchNodes)
			}
			for i, raw := range nodes {
				m, ok := raw.(map[string]any)
				if !ok {
					return "", fmt.Errorf("nodes[%d] 必须是对象", i)
				}
				if _, err := requiredString(m, "id"); err != nil {
					return "", fmt.Errorf("nodes[%d].%v", i, err)
				}
				touched := 0
				for _, field := range []string{"title", "prompt", "text"} {
					v, err := stringField(m, field)
					if err != nil {
						return "", fmt.Errorf("nodes[%d].%v", i, err)
					}
					if v != "" {
						touched++
					}
					if err := limitArg(fmt.Sprintf("nodes[%d].%s", i, field), v); err != nil {
						return "", err
					}
				}
				if touched == 0 {
					return "", fmt.Errorf("nodes[%d] 没有给出任何要修改的字段", i)
				}
			}
			return fmt.Sprintf("修改 %d 个节点", len(nodes)), nil
		},
		hub: hub,
	}
}

// newDeleteNodes wires delete_nodes:批量删节点(连线随节点自动断,由
// 前端应用器处理)。
func newDeleteNodes(hub *Hub) promptgen.AgentTool {
	return draftCore{
		name: "delete_nodes",
		description: "批量删除画布上的节点(与其相连的连线一并移除)。删除前用 read_canvas 确认节点 id;删除影响下游内容生成,谨慎操作。",
		parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"ids": map[string]any{
					"type": "array",
					"description": fmt.Sprintf("要删除的节点 id 清单(最多 %d 个)", maxBatchIDs),
					"items":       map[string]any{"type": "string"},
				},
			},
			"required": []string{"ids"},
		},
		validate: func(args map[string]any) (string, error) {
			ids, err := arrayField(args, "ids")
			if err != nil {
				return "", err
			}
			if len(ids) == 0 {
				return "", fmt.Errorf("参数 ids 不能为空数组")
			}
			if len(ids) > maxBatchIDs {
				return "", fmt.Errorf("一次最多删除 %d 个节点,请分批提交", maxBatchIDs)
			}
			for i, raw := range ids {
				if _, ok := raw.(string); !ok || raw.(string) == "" {
					return "", fmt.Errorf("ids[%d] 必须是非空字符串(节点 id)", i)
				}
			}
			return fmt.Sprintf("删除 %d 个节点", len(ids)), nil
		},
		hub: hub,
	}
}

// newConnectNodes wires connect_nodes:连边。
func newConnectNodes(hub *Hub) promptgen.AgentTool {
	return draftCore{
		name: "connect_nodes",
		description: "在画布上批量连线(source→target,方向有向)。连线类型组合有创作语义的合法性校验(如剧本可连到图片/视频节点、图片可连到视频首帧),非法组合会在应用时报错回执。",
		parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"edges": map[string]any{
					"type": "array",
					"description": fmt.Sprintf("要建立的连线(最多 %d 条)", maxBatchEdges),
					"items": map[string]any{
						"type": "object",
						"properties": map[string]any{
							"source": map[string]any{"type": "string", "description": "起点节点 id"},
							"target": map[string]any{"type": "string", "description": "终点节点 id"},
						},
						"required": []string{"source", "target"},
					},
				},
			},
			"required": []string{"edges"},
		},
		validate: func(args map[string]any) (string, error) {
			return validateSimpleEdges(args, "connect")
		},
		hub: hub,
	}
}

// newDisconnectNodes wires disconnect_nodes:断边。
func newDisconnectNodes(hub *Hub) promptgen.AgentTool {
	return draftCore{
		name: "disconnect_nodes",
		description: "批量断开画布上已有的连线(按 source→target 定位)。",
		parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"edges": map[string]any{
					"type": "array",
					"description": fmt.Sprintf("要断开的连线(最多 %d 条)", maxBatchEdges),
					"items": map[string]any{
						"type": "object",
						"properties": map[string]any{
							"source": map[string]any{"type": "string", "description": "起点节点 id"},
							"target": map[string]any{"type": "string", "description": "终点节点 id"},
						},
						"required": []string{"source", "target"},
					},
				},
			},
			"required": []string{"edges"},
		},
		validate: func(args map[string]any) (string, error) {
			return validateSimpleEdges(args, "disconnect")
		},
		hub: hub,
	}
}

func validateSimpleEdges(args map[string]any, verb string) (string, error) {
	edges, err := arrayField(args, "edges")
	if err != nil {
		return "", err
	}
	if len(edges) == 0 {
		return "", fmt.Errorf("参数 edges 不能为空数组")
	}
	if len(edges) > maxBatchEdges {
		return "", fmt.Errorf("一次最多%s %d 条连线,请分批提交", verb, maxBatchEdges)
	}
	for i, raw := range edges {
		m, ok := raw.(map[string]any)
		if !ok {
			return "", fmt.Errorf("edges[%d] 必须是对象", i)
		}
		if _, err := requiredString(m, "source"); err != nil {
			return "", fmt.Errorf("edges[%d].%v", i, err)
		}
		if _, err := requiredString(m, "target"); err != nil {
			return "", fmt.Errorf("edges[%d].%v", i, err)
		}
	}
	return fmt.Sprintf("%s %d 条连线", map[string]string{"connect": "连", "disconnect": "断开"}[verb], len(edges)), nil
}

// newTriggerTask wires trigger_task:对某节点触发 image/video 生成任务。
// 参数对齐画布任务 composer 契约(白名单即 composer 档位表);前端应用 =
// 走既有 POST /canvases/:id/tasks 同链路,服务端那套 seconds 范围/参考图
// 上限/data: 拒绝/model_not_priced 校验原样生效,本工具只做形状预检。
func newTriggerTask(hub *Hub) promptgen.AgentTool {
	return draftCore{
		name: "trigger_task",
		description: "对一个画布节点触发媒体生成任务(kind=image 生图 / kind=video 生视频)。model 必须用目录里的真实模型名,不确定就先问用户;prompt 是任务的创作提示词。图片任务最多 9 条 image_urls 参考图(http(s) 地址或素材内容寻址路径,不能是 data: 内联);视频任务 seconds 1-100、video_refs 结构化参考 {url,kind,role}(kind: image/video/audio;role: first_frame/last_frame/reference_image/reference_video/reference_audio)。触发生成会真实消耗额度,只在用户明确要求时调用。",
		parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"node_id": map[string]any{"type": "string", "description": "目标节点 id(产物会写回该节点)"},
				"kind":    map[string]any{"type": "string", "description": "image | video,缺省 image"},
				"prompt":  map[string]any{"type": "string", "description": "生成提示词"},
				"model":   map[string]any{"type": "string", "description": "模型名(与节点生成对话框目录一致)"},
				"size":    map[string]any{"type": "string", "description": "分辨率档位,如 2048x2048(与对话框档位一致)"},
				"ratio":   map[string]any{"type": "string", "description": "视频画幅比,如 16:9"},
				"background": map[string]any{"type": "string", "description": "仅 image:transparent = 透明背景"},
				"seconds": map[string]any{"type": "number", "description": "仅 video:时长(秒,1-100)"},
				"image_urls": map[string]any{
					"type": "array",
					"description": fmt.Sprintf("仅 image:参考图清单(最多 %d 条)", maxTaskRefs),
					"items":       map[string]any{"type": "string"},
				},
				"video_refs": map[string]any{
					"type": "array",
					"description": fmt.Sprintf("仅 video:结构化参考清单(合计最多 %d 条)", maxTaskRefs),
					"items": map[string]any{
						"type": "object",
						"properties": map[string]any{
							"url":  map[string]any{"type": "string"},
							"kind": map[string]any{"type": "string", "description": "image | video | audio"},
							"role": map[string]any{"type": "string", "description": "first_frame | last_frame | reference_image | reference_video | reference_audio"},
						},
						"required": []string{"url", "kind", "role"},
					},
				},
				"scene_type": map[string]any{"type": "string", "description": "仅 image:3d_panorama = 全景生成"},
			},
			"required": []string{"node_id", "model", "prompt"},
		},
		validate: validateTriggerTask,
		hub:      hub,
	}
}

func validateTriggerTask(args map[string]any) (string, error) {
	nodeID, err := requiredString(args, "node_id")
	if err != nil {
		return "", err
	}
	if err := limitArg("node_id", nodeID); err != nil {
		return "", err
	}
	model, err := requiredString(args, "model")
	if err != nil {
		return "", err
	}
	if err := limitArg("model", model); err != nil {
		return "", err
	}
	prompt, err := stringField(args, "prompt")
	if err != nil {
		return "", err
	}
	if err := limitArg("prompt", prompt); err != nil {
		return "", err
	}
	kind, err := stringField(args, "kind")
	if err != nil {
		return "", err
	}
	if kind == "" {
		kind = "image"
	}
	if kind != "image" && kind != "video" {
		return "", fmt.Errorf("参数 kind 只支持 image 或 video(其余媒体类型暂不开放)")
	}
	for _, field := range []string{"size", "ratio", "background", "scene_type"} {
		v, err := stringField(args, field)
		if err != nil {
			return "", err
		}
		if err := limitArg(field, v); err != nil {
			return "", err
		}
	}
	if v, ok, err := numberField(args, "seconds"); err != nil {
		return "", err
	} else if ok && (v < 1 || v > 100) {
		return "", fmt.Errorf("参数 seconds 必须在 1-100 之间(最终裁决在画布任务端点)")
	}
	if urls, err := arrayField(args, "image_urls"); err != nil {
		return "", err
	} else if len(urls) > maxTaskRefs {
		return "", fmt.Errorf("image_urls 最多 %d 条", maxTaskRefs)
	}
	if refs, err := arrayField(args, "video_refs"); err != nil {
		return "", err
	} else if len(refs) > maxTaskRefs {
		return "", fmt.Errorf("video_refs 合计最多 %d 条", maxTaskRefs)
	}
	return fmt.Sprintf("提交 %s 生成任务(%s,%s)", map[string]string{"image": "生图", "video": "视频"}[kind], nodeID, model), nil
}

func numberField(m map[string]any, key string) (float64, bool, error) {
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
