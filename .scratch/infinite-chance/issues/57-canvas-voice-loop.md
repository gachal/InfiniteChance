# 57 画布音色闭环(音色节点/音频节点/角色绑定)

Type: feat
Status: implemented(2026-10-26;feat/voice-tts)。定案背景:2026-10-26 grilling 定案,依赖 56 号票网关面(同分支实现)

## Question

56 号票落了网关 TTS 与音色定制面,画布侧消费闭环:音色的生产(节点内联表单)、台账与试听、台词合成落画布、角色绑定音色。定案取舍:角色绑定若没有消费路径就是空挂——台词合成本来就在五件套内(音色设计/克隆/管理/合成/绑定),是绑定的消费闭环,故五件全做;合成产物落画布需要音频节点载体(24 号票「音频无节点类型、面板不提供插入」随之推翻);音色台账挂 canvas 侧(创作资产语义,同 characters),全局作用域(音色可跨 IP 复用、量小好管理,不带 ip_id)。

## Answer

**voices 台账**(canvas 侧 store,双方言):

- 表 `voices`:`id`、`voice_id`、`channel_id`、`target_model`、`name`、`source_mode`(design|clone)、`source_prompt`(可空)、`sample_asset_id`(可空)、`preview_asset_id`(可空)、`status`(DEPLOYING|OK|UNDEPLOYED)、时间戳;`voice_id` 唯一索引。全局作用域,不带 ip_id。
- API(canvas JWT 面):`GET /voices`(全量)、`PATCH /voices/:id`(仅本地改名,上游无改名 API)、`DELETE /voices/:id`(**先调网关 delete_voice 成功再删行**;被 `characters.voice_id` 引用时拒绝 `voice_in_use`)。不代理 list/update_voice(56 号票口径),绑定/删除时经网关 query_voice 实时校准。

**canvas_tasks 两个新 kind**(状态机 queued/running/succeeded/failed,无 canceled——同步秒级~分钟级;双方言加 JSON 列 `voice_spec`):

- `kind=voice`(音色创建):提交 create → 拿 `voice_id` + 试听 Base64 → **试听转存素材行**(`kind=audio`,`canvases/` 档,ip 归属随画布)→ 轮询网关 query(2s 间隔、2min 上限)至 OK → 落 voices 行 + 任务成功;UNDEPLOYED → failed(原因「上游审核未通过」);轮询超时 → failed(**句柄保留**,重试续轮询不重复创建)。**remote 句柄纪律同视频**:voice_id 一收到先落任务行(`voice_spec.remote_voice_id`),重启恢复/重试带句柄先 query 校验;UNDEPLOYED 后重试丢弃句柄重建。
- `kind=tts`(台词合成):读 voices 行(校验 status=OK,取 channel_id 钉渠道)→ 调 `/v1/audio/speech` → 产物 URL 转存(`kind=audio` 素材行)→ 成功回填音频节点。

**音色节点(第 10 种节点类型 `voice`,创作组,`NODE_TYPE_LABEL` =「音色」)**:

- data 随身(整图 JSON):`{ voice_id?, name, mode: "design"|"clone", status, preview_asset_id?, target_model?, voice_prompt?, sample_asset_id? }`。**实现结论(评审回写)**:节点实存 `{ mode, name, target_model?, voice_prompt?, preview_text?, sample_asset_id?, status?, ledger_id? }` —— 音色的上游句柄与试听不进节点快照,`ledger_id` 指台账行、卡面按 voices 目录实时投影(台账是唯一事实源,快照会漂移);票面原形的 `voice_id`/`preview_asset_id` 由台账行承载。
- 创建表单**内联卡上**(不进生成对话框、不进编辑层):模式二选一——**设计** = 名称 + 音色描述(≤500 字,仅中英)+ 试听文本(15–200 字)+ 模型下拉;**克隆** = 名称 + 声音样本 chip(本机上传经 `/api/assets/upload` kind=audio、或素材库挑选,≤10MB 前端守卫)+ 试听文本 + 模型下拉;表单带价格预估(56 号票 PriceSummary)。样本 UI 提示「10–20 秒清晰人声,WAV/MP3/M4A」;时长质量交上游裁决。
- 提交 = canvas_task `kind=voice` 绑本节点;卡面状态徽章(部署中/可用/审核未通过/失败)+ 试听 `<audio>`(preview 素材);失败原地重试。
- 删除节点仅摘图不动台账;卡上「删除音色」动作 = 二次确认 + `DELETE /voices/:id`(voice_in_use 防护)。
- 连线:仅右把手可作源(→音频节点)。

**音频节点(第 11 种节点类型 `audio`,无工具栏空锚点)**:

- data:`{ asset_id, text, model?, chars? }`(27 号票产物-元数据配对)。卡面 = 内联 `<audio controls>` + 台词摘录。
- 只作为台词合成产物与素材面板/AssetPicker 音频行「插入画布」出现;素材删除「素材不可用」占位同媒体节点;无灯箱入口、不进生成对话框。
- 连线:仅左把手可作目标(音色→音频,新边型「voice → 音频」合成来源语义,絵コンテ→图片同族;connection 轻校验矩阵前端拒绝其余组合,后端不校验)。

**台词合成入口**:音色节点卡「合成台词」→ 表单:台词 textarea(≤600)+ 可选演绎指令(instruction)+ 字符预估价 → 提交前「右侧音频节点 + voice→audio 边先落库再提交」(生图纪律)→ kind=tts。

**角色绑定**:

- `characters` 双方言加列 `voice_id`(可空,逻辑引用 voices);`PATCH /characters/:cid` 收 voice_id(校验 voices 行存在且 status=OK)。
- 绑定入口 = CharacterDrawer(IP 详情角色抽屉)加「音色」区:voices 全量 picker(状态徽章 + 试听)+ 解除;画布 CharacterNode 显示绑定音色徽章 + 试听。
- 粒度**角色级**(声音跟人不跟衣服,变体级显式不做);promptgen 角色设定注入**不带**音色(音色是 TTS 侧语义)。

**目录端点**:`GET /audio-models`(canvas JWT,服务级 key 经网关,过滤 audio 能力渠道 + 字符轨配价模型,带 38 号票 PriceSummary 带价)——音色节点创建表单的模型下拉与价格预估共用。

**面板收尾**:素材面板与 AssetPicker 音频行加「插入画布」(落音频节点,asset 引用 + 内容寻址;推翻 24 号票注记);生成记录面板音频行(图标 + 内联试听 + 插入画布);`recordKindLabel` 三元。**实现结论(评审回写)**:「插入画布」落在素材面板与生成记录面板;AssetPicker 音频行保持选择语义(toggle,图片/视频行同款 —— 它的职责是挑参考/样本,插入由调用方接线),不设插入按钮。

## 产出面

- 后端:`internal/canvas`(voices store + handler + characters 加列)、`internal/canvastask`(kind 常量与白名单、`voice_spec` 列、worker 分派两 run、Finalize)。
- 网关侧注记:「服务级 key 经网关」定案的支撑端点 `GET /v1/audio/models`(字符轨合成模型目录)由 57 号票实现在 `internal/relay`(56 号票端点组之外)。
- `canvas/web`:`graph.ts`(voice/audio 类型)、`voice.ts` 纯函数(表单校验/状态映射,vitest)、`VoiceNode.vue`/`AudioNode.vue`、connection 矩阵、小地图配色、工具栏创作组「音色」、CharacterDrawer、素材面板/AssetPicker/生成记录面板。
- `packages/api`:voices/characters/audio-models 请求层。
- CONTEXT.md:「画布任务」「连线」「节点」「素材」词条增补 + 新增「音色节点」「音频节点」词条(已随定案落笔)。

## 部署侧动作(代码外)

无手动迁移(双方言 EnsureSchema + 存量加列走既有迁移纪律);`make up --build` 重建;桌面版自动获得。

## 验证点

- 设计全链:表单校验(500/15–200/600)→ 任务部署中徽章 → OK → 试听可放 → 台账可见;克隆全链(样本上传/挑选、超 10MB 拒收、非音频拒收)。
- 合成:预估价 → 音频节点 + 连线 → 播放;素材删除占位;生成记录音频行;面板插入音频节点。
- 绑定:CharacterDrawer 选/解除 → CharacterNode 徽章试听;绑定中删音色 `voice_in_use`;解除后可删。
- 计费对账:创建扣 0.01、合成按字符扣(对照上游账单)、失败退款。
- `make test` + `make lint` + `pnpm --filter canvas-web build` + 浏览器点验 + 桌面冒烟;真 key 全链共用 56 号验证点清单。

## Out of Scope(另票)

- 分镜/ネーム台词批量配音(读 cut lines 一键多任务);从角色抽屉直接发起合成(消费路径经音色节点);变体级绑定;音色试听重新生成(重建新音色节点代替);voices 管理页(台账仅 picker 与节点呈现);音频节点进视频对话框参考条的拖拽直达(经素材库挑选已有路径);CosyVoice/flash 档(依赖 56 目录链,配价即接)。
