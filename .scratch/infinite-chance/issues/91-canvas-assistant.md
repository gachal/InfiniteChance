# 91 画布助手:画布级 Agent 会话 + 画布图工具 + 授权模式 + 右侧助手面板(2026-11-19 定案)

Type: feat
Status: implemented(2026-11-19,feat/91-canvas-assistant)

## Question

用户要求画布获得 agent 对话能力:入口为桌宠(92 号票)或面板入口,点击后右侧固定区域出现 agent 对话框;作用为**修改当前画布内容**——可新建节点、批量修改节点(如改某图片节点内容后,与其相关的图片/视频/剧本文字节点连带受影响),支持多轮对话、工具调用、新建 session;对话设两种授权形式「先询问我」/「完全授权」。

摸底发现的关键约束(2026-11-19 两路探索实证):

- 「Agent」一词已被 Agent 节点(节点级剧本导演,29/63/66 号票)占用;其会话历史存节点 `data.messages`,全仓库无 session 概念,服务端无会话状态。
- 现有九个 agent 工具全是 IP 库表读写;改图类工具(save_outline/save_script/save_storyboard)只产出「图改动草案」交前端应用,**单一写者:后端永不改库内图 JSON**(ADR 0006);「花钱操作(生图/生视频/TTS)永不进循环」是 63 号票起现役纪律。
- `tools`/`tool_choice` 网关零改动透传(tool-calling 已实证);canvas service key 计费链完整。
- 右侧固定面板有三个同范式先例(`--w-panel: 380px` / `--z-side-panel: 6` / 右缘互斥纪律)。

## 定案

- **术语切分(三词并存)**:新实体叫「**画布助手**」(canvas assistant)= 画布级全局对话助手,承载会话、工具、授权模式;「桌宠」(pet,92 号票)只是它在画布上的化身壳;「Agent 节点」维持节点级剧本导演工作台本义。代码键名一律 `assistant`(表名/端点/包名),与 agenttools/agent 节点永不混用。
- **会话实体与持久化**:新表 `canvas_assistant_sessions`(双方言 EnsureSchema,照 82 号票 migrations 纪律):`id` / `canvas_id` / `title`(首轮后自动生成)/ `mode`(授权档)/ `messages` JSON 一列(照画布 graph「一列文档」哲学,消息不做行级查询)/ `created_at` / `updated_at`。会话**绑死画布**:不跨画布移动,删画布级联删会话行(不留孤儿)。发给 LLM 的上下文窗口照 `agentHistoryMaxMessages = 40` 先例截断最旧。持久化纪律照 Agent 节点:assistant 轮落「最终回答全文 + 工具调用摘要行」,循环中间帧(assistant tool_calls / role=tool)不入持久历史。
- **端点**:挂 canvas JWT 面(与 `/canvases` 现有会话面同令牌体系)——`/canvases/:id/assistant/sessions` 建/列(按画布)/读/删 + SSE `POST /canvases/:id/assistant/stream`(携 session_id,服务端按会话载入历史)。
- **执行架构(延续「前端唯一写者」,ADR 0006)**:服务端 agentloop 持画布图工具的 schema,LLM 产出**结构化操作草案**,经 SSE `tool_call` 事件发前端,前端应用器落图 + autosave 照常(乐观锁天然归一,服务端永不改库内图 JSON)。**协议扩展**:现有 SSE 为单向流,画布图工具的应用结果(如新建节点的 id、应用成功/失败)须喂回 LLM 续循环——扩为「SSE + 前端回执」,新增会话作用域回执端点(POST,实现期定形);回执内容作为 tool_result 进循环。
- **工具首批(七新 + 三复用)**:
  - `read_canvas`:读图结构——节点清单(id/类型/标题/prompt 摘要/坐标)+ 边关系;规模与截断策略实现期裁量。
  - `create_nodes`:批量建节点(类型/内容/坐标/连线),坐标缺省 = 前端视口落位(照既有级联偏移语义)。
  - `update_nodes`:批量改节点内容(图片/视频节点 prompt、剧本节点文本等)。
  - `delete_nodes`:批量删节点(含断边)。
  - `connect_nodes` / `disconnect_nodes`:连边/断边。
  - `trigger_task`:对某节点触发 image/video 生成任务,参数对齐 composer 契约(prompt/model/尺寸/时长/比例/参考;参考图解析复用素材内容寻址既有契约,`data:` 拒绝照旧);前端应用 = 走既有 `POST /canvases/:id/tasks` 与生成对话框完全同链路(计费/转存/回填零新风险),任务来源标记沿用画布任务现状。
  - 复用:`list_assets` / `read_source` / `set_style`(现成,作用域已绑画布/IP)。
  - **第一批明确不做**:voice/tts/music/sfx 任务触发、删除任务、画布元数据(名称/IP 归属)修改、分组节点成员操作。
- **级联影响 = 助手责任制**:LLM 用 read_canvas 拿到节点+边关系后**主动连带提案**(如改图片节点 A 时,按来源边发现下游视频 B,连带提出改 B 的 prompt 或触发 B 重生成),全部进变更清单走授权闸门;**系统零自动级联**(无自动重算依赖、无自动重跑)。
- **授权模式两档**:
  - **「先询问我」(新会话默认)**:读操作(读画布/读资产库)自由执行;一切写操作(建/改/删节点、连线、触发任务)按轮汇总成**变更清单卡片**,用户整批批准或拒绝(不做逐项勾选),批准后前端应用器执行。
  - **「完全授权」**:图修改直接应用,**触发生成任务也直接提交**(真花钱,走 service key 计费);MVP **不设**任务数上限(单管理员自用形态,靠用量审计页对账)。
  - 授权档存 session 上(各会话各记各的),面板输入区可随时切。
  - **领域划界**:「花钱操作永不进循环」纪律(63 号票)对 **Agent 节点循环原样维持**;授权例外仅存在于画布助手 `trigger_task`,且受授权模式闸门约束——CONTEXT.md 已同步划界。
- **模型与计费**:面板输入区上方模型下拉,目录复用 `GET /prompt-models`;92 号票 pet 配置的「缺省对话模型」作新会话初值。对话计费照服务级 key 聊天 token 轨,来源标记 `canvas={id} gen=assistant`(无 node 段),审计可区分。
- **面板 UI**:右侧固定面板照三面板范式(`--w-panel` 族宽度、`--z-side-panel`、右缘互斥纪律——开助手面板关素材/记录/历史等);含会话切换下拉 + 新建、工具过程时间线(照 AgentNode `tool-timeline` 范式)、「先询问我」模式下的变更清单确认卡;92 号票桌宠缺位时以顶栏按钮作面板入口(桌宠点击同开此面板)。

## 裁量与注记

- 建议 chips(开场快捷指令)与问候语是 pet 配置(92 号票);chips 点击 = **预填助手输入框**不直接发送(照玩法预设「写进提示词框再人工改」范式)。
- session title 生成方式(LLM 短标题 vs 首条消息截断)实现期裁量,倾向 LLM 生成失败回落截断。
- 新建节点 id 由前端应用器生成(图纯函数扩展,`graph.ts` 照既有 newNode 语义),回执带 id 供 LLM 后续引用。
- read_canvas 的节点内容摘要限长策略实现期定(大画布防 prompt 爆炸),钉测护栏。
- 「先询问我」下用户拒绝变更清单 = 该批操作不应用,回执如实告知 LLM(「用户拒绝了这批变更」),由它决定后续话术。
- 会话与 Agent 节点零共用存储;两者 UI 范式(时间线/历史)可共享组件但数据通路独立。

## 验证点

- make test / lint / build 全绿;双方言 EnsureSchema 形状对账(82 号票纪律)。
- SSE + 回执闭环集成测试(照 agentstream 测试范式):工具草案 → 前端应用 → 回执 → 续循环。
- 两授权档行为钉测:先询问我(未批准不应用)、完全授权(直接应用 + 任务直提);拒绝路径回执。
- 七新工具 schema 与草案形状钉测;trigger_task 走既有任务端点(计费/来源标记 grep 回验)。
- 浏览器点验:面板开合互斥、多 session 切换/新建、变更清单卡、完全授权全链(真渠道)、断线重试。

## 实现注记(2026-11-19)

- 分支 `feat/91-canvas-assistant`(基于 feat/canvas-assistant-pet 顶点,域内四提交 + docs):会话存储双方言与删画布级联 → 工具面与回执循环 → 端点与路由挂载 → 前端面板与应用器。
- **回执协议定形**:循环不改 `runAgentLoop` 公共形状(assistant 侧循环变体承接挂起语义),草案工具 emit `tool_call` 后登记在途调用并阻塞等回执;回执中枢按 `(sessionID, callID)` 双键匹配,回执端点 `POST /canvases/:id/assistant/sessions/:sid/receipts`(body `{call_id, status: applied|failed|rejected, message?, result?}`),无挂起 409 `no_pending_call`、错号 409 `call_mismatch`;applied 的 result 带新建节点 id 映射(`{nodes:[{index,id}]}`)或任务 id(`{task_id}`)供 LLM 后续引用。
- **超时纪律**:回执等待上限 5 分钟;超时该轮以 `receipt_timeout` error 帧收尾(无 [DONE]),整轮不持久化、可重试;断线(ctx 取消)同族不持久、静默不打日志。一会话一在途流(并发流 409 `session_busy`)。
- **授权档键名** `ask`(先询问我,新会话默认)/ `full`(完全授权),存 `canvas_assistant_sessions.mode`,PATCH 即时切;授权闸门在前端执行面(ask 出确认卡、full 直接应用),服务端不抽工具定义——两档同工具面、系统提示词声明当前档。拒绝回执如实告知 LLM(「用户拒绝了这批变更」)。
- **持久化纪律**:整轮成功才落库(user 全文 + assistant 最终回答全文 + 工具调用摘要行),循环中间帧不入库;发给 LLM 的窗口 40 条截最旧(照 agentHistoryMaxMessages 先例)。会话标题:LLM 生成(10 秒超时)失败回落首条消息截 24 字。
- **服务端永不改库内图 JSON**(ADR 0006):read_canvas 只读解析(单节点摘要 120 rune、清单 200 节点截断护栏,均钉测);六个草案工具只校验形状(轻量预检:类型白名单/批量上限/trigger_task 的 kind 白名单与参考上限),语义校验(EDGE_MATRIX、任务契约)归前端应用器与既有画布任务端点。
- **trigger_task 走既有任务端点**:前端应用 = composer 提交链同构(markChanged → flush → client.createCanvasTask → taskSync.track),grep 回验来源标记 `canvas={id} gen=assistant`(流)/`canvas={id} node=…`(任务行,既有)零新改动;未新建结果节点,无草稿随迁面。
- **migrations 四处同步**:`261119_canvas_assistant_sessions.{sql,sqlite.sql}` + 双方言 EnsureSchema + chain_test.go 装配清单 + desktop/runtime.go;删画布级联经 canvas.Handlers.SessionCleanup(canvas 声明接口、assistant 实现、wiring 注入),清理失败保留画布行等重试。
- **测试数**:internal/assistant 18 个用例(store 双方言 CRUD/级联、回执中枢、循环闭环/拒绝/失败/超时/坏参数、注册表与护栏、SSE 全链集成含标题生成与超时不持久)+ canvas 包级联 3 个;前端 assistantPanel 纯函数 6 个、api 包 6 个、AGENT_TOOL_LABELS 钉测 9→16。make test / lint / build 全绿(MySQL compose 在跑,真分支已过)。
- **遗留(浏览器点验项)**:面板开合互斥、多 session 切换/新建、变更清单卡、完全授权全链(真渠道计费)、断线重试;建议 chips/问候语留 92 号票(面板新会话首屏空态即接缝);新会话模型初值暂用目录首项,92 号票 pet 缺省对话模型落地后接入。
