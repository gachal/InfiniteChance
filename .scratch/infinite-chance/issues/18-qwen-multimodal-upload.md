# 18 千问多模态支持与素材上传

Type: grilling
Status: proposed（草案——grilling 会话用户未到场,按推荐解落稿,待确认后转定案）

## Question

千问模型的支持边界如何定(渠道形态、新 adaptor 还是复用)?纯文字已通的前提下,多模态"文件"如何到达上游模型(base64 内联 / 厂商文件上传 API / 自有存储公网 URL)?要不要用户上传入口?

## Answer

1. **渠道形态:零新 adaptor 代码**。`openai` 渠道类型按设计即覆盖 Qwen 兼容模式(`https://dashscope.aliyuncs.com/compatible-mode/v1`,01 号票矩阵、CONTEXT.md「渠道」条目)。纯文字已通正是该决策的验证。开通千问是**管理端运营操作**而非开发任务:建渠道(BaseURL + 密钥 + capabilities 显式含 `chat`)+ 在 `model_prices` 为 `qwen-plus` / `qwen-max` / `qwen-vl-max` / `qwen-vl-plus` 等配 token 轨价格(未配价模型一律 `model_not_priced` 拒绝,无静默兜底)。

2. **多模态透传已就绪**:网关对 chat 全量透传,`video_url` 内容分节已被 13 号票反推验证;`image_url` 分节同形状,网关零改动。真正的缺口不在转发,在**文件从哪来**——上游模型只能取公网可达的 http(s) 地址。

3. **三条路评估,定自有存储公网 URL**:
   - (a) data URI 内联——**拒绝**。同款先例:12/13 号票「data URI 进不了网关媒体契约」(`video_inline_unsupported`);几 MB~几百 MB 请求体在网关预扣与上游拒收处炸难懂的错,且 qwen-vl 视频输入不支持 base64,图片 base64 也有低上限。
   - (b) 厂商文件上传 API(百炼临时文件,oss:// 短时效地址)——**拒绝**。绑死单一渠道,换渠道即失效,与「渠道可插拔」的领域模型冲突;契约复杂、时效短。
   - (c) **自有对象存储公网地址**——采纳(与 19 号票联动)。厂商无关、地址永久、分析与图生视频参考图共用同一条「LLM 可达地址」解析链。

4. **「LLM 可达地址」解析顺序升级**(`resolveMedia`/图生视频参考图共用):素材行有 `object_key` 且对象存储公网地址可用(19 号票 `public_base_url`)→ 拼 `public_base_url/{object_key}`(自有、永久,优先);否则回落素材行 `url`(厂商原址,约 24h 过期,现状行为);两者皆无 → 按现有错误形状拒绝(`asset_not_video` / inline 拒绝)。现状「本地卷素材无公网地址、厂商原址会过期」的缺口由此闭合。

5. **上传入口:新增 `POST /api/assets/upload`**(canvas/server,multipart,挂 JWT 会话)。填补「素材只由生成 worker 落库、用户自己的视频/图片进不了画布」的空白:
   - kind 白名单 `image` / `video`;单文件 ≤ 256MiB(与转存上限同规,`asset/transfer.go`);Content-Type 嗅探校验(扩展名 + 魔数),不信任客户端声明。
   - 对象键布局:`uploads/{yyyymmdd}/{uuid}.{ext}` —— 与任务产物键 `canvases/{canvasID}/{taskID}/{kind}.{ext}`(14 号票)区分归档语义。
   - assets 行:`url = ''`(上传素材没有厂商出处,该列语义是「厂商 http(s) 原地址」)、`object_key/content_type/size_bytes` 照 14 号票列;`canvas_id/task_id/model/prompt` 留空(素材独立于画布存在,10 号票语义)。
   - 编辑器联动:工具栏「上传」按钮 → 上传成功落对应媒体节点(`asset_id` + `/api/assets/{id}/content` 内容寻址,与素材面板插入同语义);素材面板后续也可挂同一入口。

6. **promptgen client 扩展**:`ChatRequest` 增加 `ImageURL` 字段,多模态分节数组顺序媒体在前(`video_url`/`image_url`)、文本在后,与现有 `userMessage` 同构。

7. **明确 out of scope**:DashScope 原生 adaptor(通义万相文本生图/视频合成,`X-DashScope-Async` 异步任务派,01 号票矩阵)需要渠道类型新增 `dashscope` + adaptor 内状态归并转换,与「多模态分析」无依赖关系,需要时另立票。
