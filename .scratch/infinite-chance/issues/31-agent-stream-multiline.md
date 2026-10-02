# 31 Agent 流式生成与多行输入

Type: quick-followup(29 号票追加;用户 2026-10-02 直接下令,免 grilling)
Status: approved(定案与实现同日)

## Question

Agent 节点的提示词生成目前同步整段返回,推理模型动辄数十秒,文本区全程空白;聊天输入框是单行 input,写不了长的修改意见。

## Answer

1. **流式 = 真 SSE 端到端**:新增 `POST /canvases/:id/generate-prompt/stream`,canvas/server 以 `stream:true` 调网关既有聊天流式面(05 号票透传与流式计费零改动),把 `choices[].delta.content` 增量以 `data: {"delta":"…"}` 帧中继给浏览器;成功以 `data: [DONE]` 收尾,流中途失败发 `data: {"error":{code,message}}` 后关流。同步端点 `generate-prompt` 原样保留(API 兼容);反推/分析不动。
2. **校验前置,流才开始**:画布存在、网关已配、请求体/history/技能存在与启用/模型计价全部过完才回 SSE 头——校验失败照旧 JSON 错误形状,状态码语义不丢。SSE 响应带 `X-Accel-Buffering: no`:nginx 按请求关 proxy_buffering(gzip_types 不含 text/event-stream,无压缩缓冲;vite dev 代理天然流式),两层代理零改动。
3. **失败语义**:流开始前失败 = 现状 JSON 错误;流开始后失败 = error 帧,已流出文本保留在节点文本区(不回滚),不追加历史、不投递。成功(DONE)才追加 messages 历史(20 轮截断)并自动投递下游媒体节点。
4. **多行输入**:Agent 输入框 input → textarea,自动增高(1 行起、约 5 行封顶再滚动);Enter 提交、Shift+Enter 换行;`/` 技能浮层键盘交互(↑↓回车 Esc)不变,浮层开着时方向键/回车仍归浮层;IME 选词回车不提交(isComposing 既有纪律)。**追补(用户 2026-10-02 下令)**:输入框可拖动右下角 grip 调高(`resize: vertical` + 原生 resize 事件 + 角区按住移动的指针兜底),拖过后高度归用户所有——auto-grow 退位,CSS 不设 max-height 不挡拖大;高度属组件本地 UI 态,不入节点 data。
5. **落盘节奏**:流式增量只写节点内存态(updateNodeData 不标脏,autosave 防抖不被高频打扰);收尾一次性 markDirty——成功 = 全文 + 历史,失败 = 已流出部分(重开画布可见当时进展)。

## 产出面

`internal/promptgen`(client StreamChat、handler GenerateStream + 校验前置抽公共)、路由挂载、`packages/api`(generatePromptStream 的 fetch-SSE 解析)、`canvas/web`(AgentNode textarea 自动增高、编辑器流式接线)、`docs/api.md` 流式用例、CONTEXT.md「技能」词条补流式语义。

## 验证点

- Go:StreamChat 帧解析(delta 累积、[DONE] 收尾、流前非 2xx 报错、流中 error 帧、无 DONE 即断流报错、空 delta 收尾报错);GenerateStream 的 SSE 形状与校验前置(校验失败仍是 JSON 非 SSE)。
- TS:generatePromptStream 的事件切分(跨 chunk 粘包、[DONE]、error 帧、非 2xx 抛 ApiError)。
- 计费对账:流式与同步同走网关聊天轨,用量日志形状不变(网关侧零改动是前提)。
