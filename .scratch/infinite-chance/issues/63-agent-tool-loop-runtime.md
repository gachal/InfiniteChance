# 63 Agent 工具循环运行时(canvas/server tool loop)(2026-10-31 grilling 定案)

Type: feat
Status: implemented(2026-10-31 实现;feat/script-pipeline,五票与评审修复均已并入)。**口径注记**:①执行器与 SSE 端点落 `internal/promptgen`(agentloop/agentstream/agenttool,复用 prepareGenerate 校验前置),领域工具在 `internal/agenttools` 实现 `promptgen.AgentTool`、经 wiring 注册(服务器与桌面同路);②`done` 事件较票面多 `summary` 字段(持久摘要行所需最小增补,前后端契约已对齐),`done.text` 为循环各轮文本按序拼接;③真渠道 tools 透传实测留部署后点验(httptest 全链与网关透传复核已做);④`docs/api.md` 新端点 curl 用例待补。定案背景:2026-10-31 grilling 定案,「小说→剧本→分镜」全链五票(63–67)的底座;实现顺序建议 63 → 64/65 并行 → 66(67 可先行);架构决策记录见 [ADR 0006](../../../docs/adr/0006-canvas-agent-tool-loop-frontend-graph-apply.md);平台参照:Vidu Agent「对话+故事板」、剪映 Hub「无限画布+对话框」、即梦对话改稿)

## Question

Agent 节点(29-32/59 号票)是「多模态提示词工作台」:`promptgen` 请求体只有 model/messages/stream,全仓无 tools/tool_calls 字段;「多轮」只是把节点 data 的 history 原样重发,一轮补全即结束。「一句话/小说 → 剧本 → 分镜」需要 LLM 在循环中**自主调用领域工具**(建角色、写剧本、写分镜板)。本票落循环运行时底座——工具集本体在 66 号票,script 载体在 64 号票,资产实体在 65 号票,絵コンテ扩字段在 67 号票。

事实底座(2026-10-31 代码实证):

- **网关聊天面对 `tools` 天然透传**:`chatRequest` 只解析自身所需字段(model/stream/max_tokens/max_completion_tokens/messages),注释明言「Everything else rides through untouched」(`internal/relay/handler.go:43-51`);请求侧与流式帧侧的 model 重写同走 `rewriteModel` 的 `map[string]json.RawMessage` 保形(`handler.go:698-709`),流帧侧 `inspectChunk` 只为记账解 usage(choices 解为 RawMessage 数组仅判空,`adaptor.go:296-317`)。结论:**请求携 tools、响应含 tool_calls delta,网关零改动**(openai 兼容渠道;非 openai 形渠道聊天本就是显式不支持桩,无影响)。
- 计费:tool_calls 响应照报 usage,token 轨全链照旧;估算 prompt tokens 用 body 体积启发(`estimateTokens`),tools 定义计入 body、估算自然变准,无需改估算逻辑。

## 定案(2026-10-31 grilling)

1. **循环执行器在 canvas/server**:新 SSE 端点(JWT 会话面)承载「带工具的 Agent 会话」;执行器以服务级 key 调网关 `/v1/chat/completions`,请求体带 `tools`/`tool_choice`,逐轮解析上游 tool_calls → 执行工具 → 以 role=tool 消息续请求,直至 LLM 给出最终文本回答或触护栏。浏览器断线 = 循环中断(已流出文本与已应用图改动保留,可重试)——对话本身是在线交互,不追求 canvas_tasks 式离线续跑。
2. **SSE 事件协议**(前后端契约,ADR 0006 载体):`{type:"delta",text}` 文本增量 / `{type:"tool_call",id,name,args}` 工具调用开始 / `{type:"tool_result",id,name,summary,payload}` 工具结果(summary 人读文本,payload 结构化供前端应用)/ `{type:"done",text}` 最终回答 / `{type:"error",{code,message}}`。31 号票 SSE 纪律同族:校验前置,没过完校验不给流(流前失败仍是 JSON 错误形状)。
3. **单一写者原则不破(ADR 0006)**:工具结果一律不改库内图 JSON——写图类 tool_result(节点/边草案)由**前端应用**到本地图状态、走既有 scene-change → updateNodeData → autosave 链;库表类(建角色/场景/道具、读库)由后端经 domain store 直接读写库。后端直写图被否决(前端 autosave 独占写图,后端直写会被覆盖;图版本合并成本高)。
4. **工具 = 代码内建领域工具**(OpenAI tools 数组暴露给 LLM),不做用户可配置工具(单用户 YAGNI);29 号票技能(prompt_templates)保持纯提示词注入,与本机制正交不动。
5. **花钱操作永不进循环**:生图/生视频/TTS 不作为工具;循环只能把分镜卡等「备好参数」,实际生成仍走生成对话框人工确认。文本类工具(建资产/写产物/读库)自主执行。
6. **护栏**:单次请求 ≤15 轮工具调用,触顶注入收尾指令强制 LLM 总结已完成事项并指引用户续跑;循环中间帧(tool_calls/tool 消息)**不入节点 data 持久历史**——持久 messages[] 仍只存 user/assistant 纯文本轮(assistant = 最终回答 + 工具调用摘要行),新会话重放不带工具帧、不重放工具副作用。
7. **计费与审计**:循环每轮 LLM 调用照聊天 token 轨(工具定义与工具结果都是 token),一次用户请求 = 循环内多轮 /v1 调用、各自独立计费入 usage_logs;来源标记 `gen=agent`。
8. **不加渠道「tool calling」能力位**:单用户自知模型能力;不支持 tools 的上游 4xx 原样透出(节点可见可重试原则),错误文案指路换模型;常用渠道 tools 透传实测列为本票验证点。
9. **前端过程展示**:Agent 节点内工具调用过程可视化(工具 chip/折叠时间线,即梦式「正在创建角色…」);写图类 tool_result 到达即应用(节点/边落图),应用失败(校验拒等罕见路径)在会话内显式报错不静默。

## 验证点(实现前/中)

- 常用 openai 兼容渠道(DeepSeek/GLM/Kimi/方舟等)tools 请求透传 + 流式 tool_calls delta 聚合实测;不支持 tools 的上游 4xx 形态记录。
- 15 轮护栏触发路径;断线中断后的重试语义(已应用图改动不重复应用——重试从持久历史重建,不带工具帧)。

## Out of Scope

- 花钱工具进循环(生图/生视频/TTS);批量生成(三期)。
- 用户可配置工具/工具市场;技能挂工具。
- 循环离线续跑(canvas_tasks 式任务化,ADR 0006 Consequences);上游并行 tool_calls 按序执行,不做真并行调度。
