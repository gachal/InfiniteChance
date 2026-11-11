# 角色库与消费链全景调查(03 号研究票)

日期:2026-11-11。分支:`research/character-library-survey`(worktree 调查,主工作区零改动)。

## 摘要

Lore 实体库(地点/组织/物种/物品等类型化条目)自我宣称「角色库已验证模式的横向复制」。本纪要以代码为真值核实该宣称的覆盖范围:①表结构面,characters 三表(51 号票)与 scenes/props 两表(65 号票)已构成「IP 实体库家族」,同构复制已有一次成功先例;②消费链面,角色拥有四条已验证注入链(聊天文本注入 + 物化固化、设定图生图拼装、cut 出场角色参考注入、元素 chip + 绑定行),其中后三条对实体类的入口「形状不排他」已预留;③可复用性,存储面与生图锚几乎可平移,聊天注入链的渲染函数可换芯复用,但实体版缺引用节点/连线/cut 出场标注/Agent 注入 chip,且「全树一次取全」目录与各截断口径都是按角色量级定的,Lore 长文本与大数量级是主要分叉点。

---

## 一、表结构

### 1.1 characters 三表(51 号票,含 57 号票加列)

双方言 EnsureSchema,MySQL 侧 `internal/character/store_mysql.go:26-62`,SQLite 侧 `internal/character/store_sqlite.go:29-61`:

- `characters`:`id` / `ip_id`(索引 `idx_characters_ip (ip_id,id)`,MySQL `store_mysql.go:37`)/ `name` VARCHAR(255) / `alias` VARCHAR(255) 默认'' / `description` VARCHAR(1024) 默认'' / `detail` TEXT NOT NULL(详细设定纯文本)/ `voice_id` BIGINT UNSIGNED NULL(57 号票音色绑定,角色级粒度——「声音跟人不跟衣服」,`internal/character/character.go:53-55`)/ `created_at` / `updated_at`。普通索引不加唯一,同 IP 重名允许(`store_mysql.go:22-24` 注释)。
- `character_variants`(造型变体):`id` / `character_id`(索引 `(character_id, sort, id)`)/ `name` / `note` VARCHAR(1024)(服装描述自由文本)/ `sort` INT DEFAULT 0 / 时间两列。
- `character_images`(设定图,纯素材引用):`id` / `character_id`(索引)/ `variant_id`(独立索引)/ `asset_id` / `label` VARCHAR(32) 默认'' / `sort`。**label 后端存字符串不校验枚举**;视图类集合 `ViewLabels = {三视图, 多视图}` 只是展示层投影约定(`character.go:25`)。前端白名单六类「三视图/多视图/表情集/色板/姿态/其他」在 `canvas/web/src/characters.ts:13`。

CRUD 关键语义:

- **创建角色自动建「默认」变体**(`store_mysql.go:171-175`);**最后一个变体不可删** `ErrLastVariant`(`store_mysql.go:438-450`,409 `variant_last_one`);图挂变体必须同角色 `ErrVariantNotFound` / `ErrVariantMismatch` 两码分开(`store_mysql.go:494-507`)。
- 变体/图动作 `touchCharacter` bump 角色 `updated_at`(卡墙按最近活动排序;MySQL `store_mysql.go:115-117`;SQLite 无 ON UPDATE 显式带时间,`store_sqlite.go:151-154`)。
- `ListCharacters` 一次取全树(角色 → 变体 → 图),`attachTree` 两查询按 variant_id 挂图,注释明言「单管理员量级小,画布侧目录一次拿整棵树」(`character.go:43-45`、`store_mysql.go:220-279`);指针稳定性教训见 `store_mysql.go:200-203` 注释。
- `CoverAssetID` 封面投影:逐变体取排序最前的视图类图,无则变体第一张,首个有图变体胜出,全空 = 0(`character.go:178-194`)。
- 删角色级联删变体与图行、素材行不动,单事务(`store_mysql.go:344-363`);被引素材删除后图行留「素材不可用」占位可手动移除(包注释 `character.go:11-13`)。
- 存量库加列迁移:MySQL 走 information_schema 探测(`store_mysql.go:68-96`),SQLite 走 PRAGMA table_info(`store_sqlite.go:74-122`)。57 号票 `voice_id` 是现有唯一迁移项。

### 1.2 scenes / props 两表(65 号票——「横向复制」的既成先例)

`internal/scene/store_mysql.go:25-46`(props 同构 `internal/prop/store_mysql.go:26-46`):

- `scenes`:`id` / `ip_id`(索引 `(ip_id,id)`)/ `name` / `alias` / `description` VARCHAR(1024) / `detail` TEXT / 时间两列——**与 characters 同构但砍掉变体层与 voice_id**。65 号票定案 1:「变体语义(日景/夜景、使用态)用 images 的 label 表达,不上 variants 表」。
- `scene_images`:`id` / `scene_id` / `asset_id` / `label` / `sort`——同 character_images 砍 variant_id。
- 封面投影 `PrimaryLabels = {全景}`(`scene.go:22`、投影函数 `scene.go:123-134`,「角色库规则降一维」);前端 label 白名单:场景「全景/日景/夜景/局部/其他」、道具「多视图/细节/使用态/其他」(`canvas/web/src/assetlib.ts:15-20`)。
- 删除语义/占位哲学/目录树 API 全部照角色库平移(`scene.go:1-10` 包注释;路由形状同族,`internal/scene/handler.go:38-61`)。65 号票口径注记:实体删除 API 已在、UI 无入口;增补「主资产」封面投影。

**装配**:`canvas/server/main.go:55-65` 依次 `characters` → `scenes` → `props` → `voices` EnsureSchema(幂等,顺序自含无依赖)。

### 1.3 设定图生成侧的表结构增补(70 号票)

- `canvas_tasks` 双方言加列 `ip_id BIGINT UNSIGNED NOT NULL DEFAULT 0`(0 = 画布任务,存量行不回填;`internal/canvastask/store_mysql.go:28、78-80`)。任务行允许 `canvas_id=0`(无画布任务)。
- 转存对象键四分归档:`canvas_id=0` 走 `asset.TransferIP` 落 **`ips/{ipID}/{taskID}/{kind}.{ext}`** 前缀,素材行 `ip_id` 读任务行优先(`internal/canvastask/worker.go:415-428`;`internal/asset/transfer.go:51-61` TransferIP 与共享 transfer 体)。硬性约束 7 的三分归档自此为四分,48 号票跨前缀复用与按 object_key 计数删除防护不变。
- 素材行本身带 `ip_id` 列(`worker.go:439` 一带的 INSERT 可见),IP 级产物在素材域按 IP 归属可枚举。

### 1.4 character_section 物化——不是表,是节点 data 契约(e97102b,52 号票历史固化)

- `AgentNodeData.character_section` 随画布节点 data 持久(`canvas/web/src/graph.ts:39-45`);`character_ids` 同存。
- 服务端请求体可选 `character_ids[]` 与 `character_section`(已固化段)双字段(`internal/promptgen/handler.go:166-175`);固化段非空时**逐字使用、不再访问角色目录**(角色已删照常回放),优先于 ids;空 = 按 ids 取最新数据渲染(`handler.go:416-430`)。
- 上限:单次注入 ≤20 角色(`handler.go:201-204`),固化段回传 ≤50 万 runes(`handler.go:43-47`),超限整单拒绝。
- 响应随行:同步 JSON 带 `character_section`(`handler.go:571`),流式以 `data: {"character_section":…}` 首帧带回、增量之前先发(`handler.go:580-615`;`internal/promptgen/agentstream.go:79`)。
- 前端裁决纯函数 `agentCharacterInjection` 三分支:有固化段只回传固化段(不上送 ids);无固化段有选择上送 ids;皆无不注入(`graph.ts:51-62`);chip 变更/换技能/新会话作废固化段(commit e97102b 提交说明;发送成功把响应带回的段持久进节点 data,`canvas/web/src/views/CanvasEditorView.vue:2295-2297`)。

## 二、消费链全景

### 2.1 聊天文本注入(52 号票,promptgen)

- **入口 UI**:Agent 输入区角色 chip,显式选择列表、可多枚按选择序、不做消息自动检测(`canvas/web/src/components/nodes/AgentNode.vue:10、171、846-852`)。
- **校验**(服务端,`handler.go:510-544`):逐 id 校验存在(404 `character_not_found`)与 IP 归属 `character.ip_id == canvas.ip_id`(400 `character_ip_mismatch`);任何一条不过整单拒绝,流式变体沿用「没过完校验不给流」。
- **渲染形状**(每角色一段,`handler.go:488-503`):`【角色设定】\n名字:…\n别名:…\n简介:…\n设定:\n<detail>`——**只带角色级四件套,变体名与服装描述不注入**(视觉一致性走图像锚)。多角色按请求序各渲染一段、段间空行(`handler.go:543`)。空可选字段照常出行,不替用户编内容。
- **注入位置**(`handler.go:292-305` conversationMessages):拼在技能渲染文本之后(`\n\n` 连接),仍属首条 user 指令——「角色设定段对模型是背景资料,永远先于任何一轮用户输入」。
- **截断**:**聊天注入侧对 detail 本身无截断**,实际上限 = 固化段 50 万 runes 整单拒绝线(`handler.go:47`)。与生图拼装的 2000 runes 是两套口径。
- **边界**:媒体节点不加角色引用;token 成本记 Agent 调用自身账(52 号票);Agent 聊天注入不走 IP 风格段(69 号票,`canvas/web/src/ipStyle.ts:9-10`)。
- **写库侧配套**(66 号票,消费链的供给侧):Agent 工具循环有 `create_character` / `create_scene` / `create_prop` 纯文本入库工具与 `list_assets` 读全库对表工具(`internal/agenttools/agenttools.go:2-3、71`;`internal/agenttools/assets.go:105-238`)——三类实体的「Agent 写入」通道已存在,但**读回上下文走工具调用而非系统提示词注入**。

### 2.2 设定图生成(70 号票,生图注入)

- **五类型代码常量**(label 白名单六类去「其他」):三视图(默认)/多视图/表情集/色板/姿态(`internal/canvastask/iptask.go:30-39`);类型 → prompt 模板映射 `sheetTypeTemplates`(`iptask.go:45-51`),不进技能库。
- **服务端拼装 `BuildSheetPrompt`**(`iptask.go:74-98`):顺序 = 类型模板行 → `角色:` → `别名:` → `简介:` → `详细设定:` → `造型:` → `服装:`,IP 风格段(69)放最前单独成行,最后按 rune 硬截断 `MaxSheetPromptRunes = 2000`(`iptask.go:56、102-108`,不加省略号)。**detail 在四件套里可长至 20000,截断是拼装固定尾步**(`iptask.go:53-55` 注释)。
- **无画布任务端点**(`POST /api/ips/:id/tasks`,`iptask.go:123-127、147-295`):请求体只收 `character_id/variant_id/sheet_type/model/size/background/image_urls`,**不收 prompt**——拼装是唯一口径、落任务行可审计(`iptask.go:129-139`);票面注记明确「端点形状不排他——场景/道具将来复用另票」(`iptask.go:7`)。校验链:IP 存在 404 → 角色存在且本 IP 404(跨 IP 不泄露目录)→ 变体隶属 400 → sheet_type 枚举 → image_urls ≤9 同画布图生图解引用 → 按次计价 `model_not_priced`(`iptask.go:154-266`)。
- **前端同形**:`canvas/web/src/sheet.ts` 的 `SHEET_TYPE_TEMPLATES` / `buildSheetPrompt` 与服务端逐字一致,两侧字面量测试互钉防漂移(`sheet.ts:8-11、23-76`;`iptask.go:43-44` 注释)。前端拼装只服务入口一(composer 预填)与类型切换重拼;入口二(抽屉对话框)prompt 由服务端唯一拼装。
- **两入口**:
  - 入口一(画布):CharacterNode「生成设定图」→ `spawnProductNode` 落空图片节点 + 「角色→图片」来源边 + composer 预填(类型默认三视图、变体 = 卡片当前选中变体、B3 参考图默认 = `latestVariantImage` 变体最近一张,可手动移除)(`CanvasEditorView.vue:3155-3213`;`sheet.ts:80-90`)。**锚定 = 前端内存记账** `sheetPending`(task→角色/变体/label),任务成功在 `syncTaskToCanvas` 兑现 `anchorSheetImage` → `POST /characters/:cid/images`;刷新丢映射不自动锚,产物仍是素材、抽屉可手动锚(`CanvasEditorView.vue:441、498-521、3128、631`)。
  - 入口二(抽屉):AssetDrawer 轻量生成对话框 → 无画布任务 → 对话框内轮询 → 成功自动锚(70 号票 B5)。

### 2.3 cut 出场角色注入(70 号票 C + 71 号票 A 修正)

- **取图规则**(`pickCutCharacterRefs`,`sheet.ts:142-170`):按 `cut.character_ids` 序逐角色取「默认变体」(名为「默认」的变体,改名后回落首个,`defaultCharacterVariant` `sheet.ts:104-108`);取图 `pickVariantAssetId` 优先级 = 该变体最新一张视图类(label ∈ 五类型)> 该变体任意设定图 > `cover_asset_id` 封面投影;全无图 = 0 → **静默跳过**(不报错不阻塞);cap 截断,`dropped` 计数供 toast。
- **cut→画面直通**(生图无互斥):参考条整条替换为 picks(序 = character_ids 序),超 9 截断 + toast(`CanvasEditorView.vue:3088-3102`);角色设定图只进参考条**不进 prompt**。
- **cut→视频直通**(71 号票 A 修正首帧/参考互斥——Seedance/MiniMax H3 三家必拒混用):`buildCutVideoPrefill`(`canvas/web/src/cutVideo.ts:44-68`)——`cut.asset_id > 0` = 画面作真首帧、**设定图一律不注入**,`skippedInject` 让 wiring 出说明 toast;无画面才注入且以**元素 chip 形态**进绑定行(带 `element: characterElement(characterId, variantId)`)。装配层 `cutPrefillVideoRefs` / `cutPrefillNotice`(`canvas/web/src/videoPrefill.ts:16-39`)。

### 2.4 元素参考注入(71 号票,「实体设定注入」最完整的现役形态)

- **数据形态**:`ComposerElement { type: 'character'|'scene'|'prop', entityId, variantId? }`,只活在 composer 草稿,不上网关不落库;**名称不存快照**,渲染与发送时按 entityId 从 IP 目录实时取(`canvas/web/src/composer.ts:16-24`;目录 `canvas/web/src/element.ts:25-29 ElementCatalog`)。实体或变体已删 → chip **退化无名参考图**,唯一行为门 `hasResolvedElement`(`element.ts:111-119`):角标消失、不进绑定行、不进超 4 计数,但仍占参考额度与编号位。
- **挑选合并**(`mergeElementPick`,`element.ts:141-180`):同实体(角色按 entityId+variantId,场景/道具按 entityId)同图再点 = 取消、异图 = 原位替换(一元素一图)、新实体走 append 系既有守卫(URL 形状/去重/上限),超限整条拒收;视频模式元素**恒 `reference_image`**(`element.ts:190-200`)。装配层折算(`applyElementPickToDraft` 落回上传草稿)在 `canvas/web/src/composerElements.ts:24-46`。
- **绑定行**(`buildElementLine`,`element.ts:216-243`):`出场元素:小蓝@图片1(角色)、天台@图片3(场景)`;只有可解析元素进行,无元素 = 空串(行为逐字节不变);编号 = 参考序,结构化取自 `refmention.refIndexNumbers`(图片模式按全部条目序、视频模式只数 reference_image)。**发送拼装 `insertElementLine`**(`element.ts:246-272`):插在风格段之后(稿首恰为「段+换行+正文」按整段长度切,退回首行启发式)、否则置最前;已含同一绑定行不叠加;绑定行随任务行 prompt 落库可审计。
- **数量提示**:`CHARACTER_ELEMENT_HINT_LIMIT = 4`(Seedance 超 4 人稳定性下降,只提示不拦发送,`element.ts:279-296`)。
- **玩法预设**:`presets.ts` 代码常量四模板,`{角色1}/{角色2}/{场景}` 槽按参考条元素序填名(`composerElements.ts` 与 `videoPrefill.ts` 消费)。
- **直通**:CharacterNode「生成登场视频」→ 落空**视频**节点 + 「角色→视频」边,`buildEntrancePrefill`(`videoPrefill.ts:44-80`)——参考条整条替换为该角色元素 chip(变体 = 卡片当前选中变体,取图与 cut 注入共用 `pickVariantAssetId`),提示词 = `prependStyleSegment(单角色登场模板填名, ipStyle)`;变体与角色都无图不带 chip + toast(`CanvasEditorView.vue:3215-3237`)。
- **连线矩阵**:`EDGE_MATRIX.character = ['image', 'video']`——角色**仅可作生成来源边的源**,不可作任何边的目标(`canvas/web/src/connection.ts:45-51`);52 号票「不作任何连线端点」口径由 70/71 号票两轮修订至今。

### 2.5 ips.style 注入链(69 号票,双侧同形参照)

- 服务端 `canvas.StyleSegment`:`「画面风格:」+ trim 后文本`,未定 = 空串(`internal/canvas/ip.go:41-47`);字段上限 `MaxIPStyleRunes = 500`(`ip.go:22-26`);73 号票把标记从「【画面风格】」换形为「画面风格:」躲厂商 `【】` 字幕渲染(`ip.go:32-36` 注释)。
- 前端同形三函数:`styleSegment` / `prependStyleSegment` / `styledSendPrompt`(`canvas/web/src/ipStyle.ts:20-53`);防叠段靠 `prompt.includes(segment)`(`ipStyle.ts:41`);空提示词全空直通预填仍只答风格段供续写(`ipStyle.ts:38-40`)。
- 注入面:三条直通预填(角色设定图 / cut 画面 / cut→视频)自动拼段 + composer 图片/视频模式「带 IP 风格」开关(默认关,发送时拼、不改写输入框);Agent 聊天注入不动(69 号票 Q7)。
- **这是「实体设定注入」的最近参照**:固定标记段 + 双侧同形纯函数 + 两侧字面量测试互钉 + 防叠 + 随任务行落库可审计,全套模式在 70 号票被 BuildSheetPrompt 直接复用。

## 三、可复用性评估(Lore 视角,只列事实与差异)

### 3.1 复制已成立的部分(有代码先例)

1. **「IP 实体库家族」模式已有一次横向复制成功先例**:65 号票把 characters 三表砍掉变体层与 voice_id 即得 scenes/props 两表,列结构、目录树 API、封面投影、级联删除、「素材不可用」占位、前端 label 白名单全套平移(§1.2)。Lore 的地点/组织/物种/物品若按「每类型一套实体表+images 表」走,最小复制面 = 照 scene 包再改 label 白名单与封面主资产集合。
2. **IP 归属与消费面校验模式可直接平移**:创建带 ip_id、列表按 ip_id 过滤、注入/生成时服务端 `ip_id == canvas.ip_id` 校验 + 两码分开(404 not_found / 400 ip_mismatch)——三处消费链(聊天 `handler.go:537-539`、无画布任务 `iptask.go:182-186`、元素目录限本 IP)同款。
3. **聊天注入链可换芯复用**:52 号票的「段渲染 + 校验 + 固化段物化」三层里,只有 `characterSection` 渲染函数(`handler.go:494-503`)是角色特定的;校验骨架(`resolveCharacterSection`)、物化契约(e97102b,`graph.ts:51-62` 三分支裁决 + 节点 data 持久 + 服务端逐字复用不查目录)与实体无关。
4. **风格段模式(69)是字段级注入的现成模板**:固定标记 + 双侧同形 + 防叠 + 截断口径字段级,「实体设定注入」若以段形态进 prompt,70 号票已示范该模式的直接复用。
5. **实体类生图入口已预留**:`POST /api/ips/:id/tasks` 端点形状「不排他——场景/道具将来复用另票」(`iptask.go:7`);`asset.TransferIP` 归档函数按 ipID 通用(`transfer.go:55-61`);worker 分流只看 `canvas_id==0`(`worker.go:419-428`)。
6. **Agent 写入库通道已有三件套**:`create_character/create_scene/create_prop` + `list_assets`(`agenttools/assets.go`),Lore 实体照 ADR 0006 库表类工具范式加注册即可。

### 3.2 不成立 / 需要新定案的部分(差异点清单)

1. **变体维是角色独有**:「每换一次装 = 成套新视图」的用户领域口径(51 号票)驱动了 variants 表、最后变体拒删、B3 参考图默认、默认变体取图规则(`sheet.ts:104-108`)整条链;scene/prop 已示范砍掉该维的降维形态(label 表达状态)。Lore 条目多数无「造型」语义,但物种(形态)/组织(分支)是否有变体维是票面必须回答的第一问——两档先例(有/无)代码都在。
2. **设定图五类型锚是角色专用**:三视图/多视图/表情集/色板/姿态模板常量(`iptask.go:45-51`)与视图类优先投影(`character.go:25`)不可语义平移;scene/prop 各自另立白名单(`assetlib.ts:15-20`)。Lore 需要「类型 → 白名单 → 主资产集合 → prompt 模板」四件各自重立。
3. **正文长度与注入形状分叉**:characters.detail 是 TEXT 但注入形状按「四件套分行」设计,聊天侧不截断(上限 = 固化段 50 万 runes 整单拒绝,`handler.go:47`)、生图侧 2000 runes 硬截(`iptask.go:56`)。Lore 若正文是长文档(世界观圣经式),52 的「逐字段一行」渲染不再适用——而「风格指南/世界观圣经等大文档」的载体问题在 53 号票被显式推迟、CONTEXT.md IP 条维持推迟口径,实体字段 vs 大文档节点之争尚无定案。
4. **数量级假设**:全树目录一次取全(`character.go:43-45`「单管理员量级小」)、单次注入 ≤20 角色(`handler.go:204`)、参考条 ≤9、超 4 角色提示——全部按角色量级定。Lore 世界观条目数可能远超角色数,目录取数形状与注入上限是否沿用需单独评估。
5. **消费面覆盖差**:角色独有「引用节点(CharacterNode)+ 连线矩阵源端 + cut 出场标注 character_ids + Agent 聊天 chip」四件消费入口;scene/prop 只有「目录 + AssetPicker 挑选 + 落图节点」而**无注入链**(65 号票 Out of Scope 明言 Agent 注入 chip 二期、cut 引用随二期)。Lore 若宣称复制「角色库已验证模式」,实际可复制的是存储面 + 生图锚 + 52 渲染骨架;引用节点/连线/cut 标注对实体类型都是未验证地带(元素 chip 的 `ComposerElement.type` 枚举 `'character'|'scene'|'prop'` 若扩 `lore`,要动 `composer.ts:16-24`、`element.ts` 类型标签与绑定行一族)。
6. **前端分组与 chip 标注族**:AssetPicker「角色→变体两级 / 场景/道具一级」分组(71 号票 B3)、元素 chip 类型小标「角色/场景/道具」(`element.ts:208-212 ELEMENT_TYPE_LABELS`)、绑定行小标同源——新增实体类型是前端多处分叉点的枚举扩展,不是纯后端加表。
7. **物化的实体版语义待定**:character_section 固化「角色后改不影响已发生对话」(历史事实);Lore 设定若同样固化,「设定改了旧对话不跟」是否合意(世界观往往要求全局一致)是产品语义问题,机制本身可直接复用。

---

## 给 04 号票的事实输入

1. **「IP 实体库」已是家族模式而非角色独有**:scene/prop 与 characters 同构且无变体(65 号票,internal/scene/store_mysql.go:25-46),Lore 最小复制面 = 照 scene 包改 label 白名单与主资产集合;级联删除、封面投影、「素材不可用」占位、双方言迁移全套先例可直接平移。
2. **注入链三层各有截断口径,互不通用**:聊天注入不截 detail(实际上限 = 固化段 50 万 runes,promptgen/handler.go:47)、生图拼装 2000 runes 硬截(iptask.go:56)、风格字段 500 字(ip.go:26)——Lore 正文若超角色量级,截断策略与注入形状(四件套分行 vs 长文档分段)是必须先定的第一分叉。
3. **52 号票物化契约(e97102b)实体无关**:固化段随 Agent 节点 data 持久、服务端逐字复用不再访问角色目录、三分支载荷裁决(graph.ts:51-62)——Lore 聊天注入只需换渲染函数(handler.go:494-503 一处)即可复用整条校验-物化-回放链。
4. **实体版生图入口已预留但模板层是角色专用**:`POST /api/ips/:id/tasks` 形状不排他、`TransferIP` 按 ipID 通用(iptask.go:7、transfer.go:55-61);但五类型模板常量、视图类 label 集合、默认变体取图规则都是角色语义,Lore 需自立「类型 → 模板/白名单/主资产」三件。
5. **消费链的真实缺口在画布侧而非存储侧**:角色独有引用节点 + 连线源端 + cut 出场标注 + Agent chip 四件入口;scene/prop(以及任何新实体)只有目录 + AssetPicker,注入链整体缺位(65 号票 Out of Scope)——「横向复制」的宣称对存储面成立,对消费面只部分成立,实体版消费面需要自己的票。
