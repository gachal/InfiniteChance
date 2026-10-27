# 60 配乐生成(BGM)——腾讯 MPS 聚合渠道 + 创作态音频节点

Type: feat
Status: implemented(2026-10-27 实现收口:网关三端点 + `tencent-mps`/`music` 能力 + musictask 双方言 store + canvas kind=music 全链 + AudioNode 创作态 + admin 表单 + 文档回写;make test/lint/build 与新增单测全绿。部署欠账:渠道建 MPS + 配价 + `make up --build`;真 key 全链(提交/轮询/转存/退款/换道)与「前置验证」(a)–(d) 仍待账号侧过 —— 过不了再回 grilling)

## Question

用户要「继续强化图片节点和视频节点的功能」。业界 table stakes 批次(多候选宫格/inpaint/outpaint/seed/upscale/extend/compare…)整批被否;改由三个创作角色(分镜师/角色设计师/成片导演)的视角重切候选,用户从「成片导演」视角(视频节点是素材片段、画布得有出口)选了 **BGM/音效生成**。

事实底座(2026-10-27 调研):

- 仓内零 BGM/SFX 实现。唯一线索是 CONTEXT「待补」:腾讯 **MPS 域**(不是 VOD)`CreateAigcAudioTask`,原注「音乐超出网关现有能力面,需要时另立票」——即本票。
- 音频域地基已通(56/57 号票):`assets.kind=audio`、上传嗅探 mp3/wav/m4a/ogg/flac、`canvases/{canvasID}/{taskID}/audio.{ext}` 归档、音频节点播放、worker `archiveAudio` 转存骨架——**素材层零改动**。计价按次/按秒直接复用 `UnitCall`/`UnitSecond` + `CallPrice`,**零新单位**。
- 两条既定口径被本票碰到:57 号「音频产物不进生成对话框」(TTS 走 VoiceNode 内联表单)、57 号「audio 不作任何连线的源」。
- 网关 `/v1/audio/models` 目录只认 `unit=character`,按次配价的音乐模型会被筛掉。
- 上游:**音乐与音效是两个物种**(不同模型、端点、计价)。已接五类渠道无一有独立音效端点;音效事实标准 ElevenLabs `/v1/sound-generation` 大陆不可达。配乐上游四条:①腾讯 MPS `CreateAigcAudioTask`(product 862,`mps.tencentcloudapi.com`,一个接口聚合 MiniMaxMusic/ElevenLabs/Mureka/GL/Tme,异步 `TaskId` + `DescribeAigcAudioTask` 轮询,输出写 COS,20 QPS,TC3 签名,**单价未公开**——按量 + 资源包);②MiniMax 直连 `/v1/music_generation`(同步最简、天然按次,**2026-08-20 起付费音乐 API 不对新用户开放**,只有存量账号能用);③百炼 Fun-Music(邀测,仅华北2);④方舟 seedmusic(是否公开的证据互相矛盾)。
- **时长不可控**:MPS/MiniMax/Fun-Music 均无时长参数,产物为整曲(约 1–6 分钟);仓内无裁剪能力。
- 产物 URL 24h 过期——照「成功即转存自有存储」纪律,现成。

## Answer

定案(2026-10-27 grilling 两轮十问,全部按推荐):

1. **物种与范围**:音频域四物种定名并已回写 CONTEXT.md——**配乐**(纯音乐整曲)/ **歌曲**(带歌词人声)/ **音效**(≤30s 短音)/ **台词音频**(台词合成的产物)。本票**只做配乐**。音效拆 61 号票(drafted,挂「需新增海外渠道类型」前提);歌曲不做——开放歌词或人声开关即进入歌曲域,另票,不在本票顺手放开。分镜 cut「台词/音效」注记是文本字段(53 号票),不混称。

2. **上游与渠道**:主渠道 **腾讯云 MPS** `CreateAigcAudioTask`。新渠道类型 **`tencent-mps`** + 新能力位 **`music`**(第五种能力,与 chat/images/videos/audio 并列,显式开启互不串道);与 `tencent-vod` 并存,可共用同一对 SecretId/SecretKey(`internal/tc3` 复用,service=mps);渠道 `config` 增 **输出 COS bucket/region**(MPS 要求指定输出桶);模型映射 public_model → MPS `ModelName` + `ModelVersion`(如 MiniMaxMusic 3.0、Mureka instrumental 9.5);`SceneType="music"`;`AdditionalParameters` 按底层模型透传,**`is_instrumental` 写死 true**(Mureka 走 instrumental 版本)。MiniMax 直连不接(存量权限若在,可另票作备用渠道)。否决:复用 `audio` 能力位(语义从「TTS 四端点」膨胀成「音频生成族」);塞进 `tencent-vod` adaptor(两个产品域、两套任务契约,渠道类型失真)。

3. **网关契约:异步两面照 `/v1/videos`,取消面不做**——`POST /v1/audio/music`(`{model, prompt}`,受理回 `{id, status}` 任务句柄)、`GET /v1/audio/music/tasks/{id}`(五态 queued/running/succeeded/failed/canceled,成功带 `audio.url` 上游临时地址);**不做取消端点**(MPS 取消接口未查证;超时由 canvas worker 收尾判失败并退款,前端配乐任务不出取消按钮——与图片任务同款);错误形状照 `/v1` OpenAI error object。否决:网关内部封装成同步(HTTP 要挂 1–3 分钟,超时/断线/重试面全坏)。模型目录新开 `GET /v1/audio/music-models`(= 启用 `music` 能力渠道的 model_map ∩ `unit=call` 配价,响应 `{models, prices}` 同 38 号票 PriceSummary),**不动** `/v1/audio/models` 的 character 过滤。

4. **调度语义**:常规调度——受理时按优先级/权重选渠道、上游临时失败可换道(预扣原封带往下一渠道)、受理后钉死;熔断记账照 CONTEXT「调度与熔断」。**不照 TTS 钉渠道**:配乐没有绑定在某渠道的实体。

5. **计价:按次(`UnitCall`)**——一任务一首;每个底层模型各配一行价(管理台价格页手工录入);成本预估 = 单价,精确;复用 `CallPrice`/`CallSnapshot`,「预扣 → 成功实结 → 失败退款」时序照生图链;未配价一律 `model_not_priced` 拒绝,纪律不破。否决:按生成秒数实结(上游是否回报时长未证实;单价不公开时按秒更难配)。

6. **画布入口:AudioNode 创作态内联表单**——工具栏「创作」组加「配乐」,落一个空音频节点(创作态);表单 = 模型下拉(`/music-models` 目录,带 ¥/次)+ 风格描述框(≤2000 字,对齐 MPS Prompt 上限)+ 成本预估 + 提交;提交 = `canvas_task kind=music` 绑本节点(「结果节点先落库再提交」纪律),产物填回自身;卡面 = 内联 `<audio controls>` + MediaPrompt(prompt/model 配对,27 号票);data 增 **来源判别** `source: 'tts'|'music'|'insert'`(台词合成/配乐/素材插入),配乐产物记 `prompt` + `model`,`text`/`chars` 仍专属台词合成;仍只有左把手、不作源、不进生成对话框、无灯箱。否决:新增第 12 种节点类型(类型膨胀);生成对话框加 audio 模式(推翻 57 号定案,约 10 处模式分叉)。

7. **产物下游:本票不进视频链**——不自动收编为视频参考音频(各 adaptor 对音频参考语义不同:dashscope 映射成 `driving_audio` 驱动口型,整曲配乐塞进去是错的)。配乐产物 = 音频节点:播放、进素材库、素材面板与生成记录面板「插入画布」照 tts 行落音频节点。要喂视频另票逐渠道核实语义。

8. **表单范围收口**:不做参考音频翻唱(MPS `AudioInfos` cover 模式)、时长控制、结构化风格标签、主动重生成(照 57 号「试听重生成」out of scope 先例;失败重试走任务级 `ResetForRetry`)。

9. **canvas 任务编排**:`canvastask.go` 加 `KindMusic`;`handler.go` 加 `createMusicTask`(prompt 1–2000 字、`music-models` 目录命中、按次配价预检照 `createVoiceTask` 先例);`worker.go` 加 `runMusic`(照 `runVideo` 轮询骨架:提交 → 落远端句柄 → 轮询 → 终态;超时判失败退款;启动 `RequeueRunning` 恢复孤儿)+ 复用 `archiveAudio` 转存 + `FinalizeMusicSuccess` 同事务收尾;`canvas_tasks` **无需新列**(prompt/model/remote_task_id 现成,kind 为 VARCHAR),双方言 schema 零迁移。

## 产出面

- `internal/channel`:`CapMusic = "music"` 能力位;渠道类型 `tencent-mps`(config 校验:secret/bucket/region;密钥只写不读照旧)。
- `internal/relay`:`adaptor.go` 加音乐方法族(`MusicSubmit`/`MusicQuery`),其余 adaptor 落不支持桩;新 `mps.go` adaptor(TC3 签名、`CreateAigcAudioTask`/`DescribeAigcAudioTask` 翻译、MPS 任务态 → 网关五态、`is_instrumental` 写死、输出桶参数、产物 COS 地址解析);`relay.go` 挂 `/v1/audio/music`、`/v1/audio/music/tasks/:id`、`/v1/audio/music-models`;新 `music.go` handler(调度换道 + 按次预扣/结算/退款);`models.go` 加 `MusicModels`。
- `internal/pricing`:零新单位;确认 call 轨摘要可直接复用。
- `internal/canvastask`:`KindMusic`、`createMusicTask`、`runMusic`、`FinalizeMusicSuccess`(双方言 store);`client.go` 加 `SubmitMusic`/`QueryMusic`/`ListMusicModels`;`handler.go` 挂 `/music-models` 代理;生成记录面板 kind=music 行。
- `packages/api`:`createCanvasTask` kind 联合加 `music`、`listMusicModels()`。
- `canvas/web`:`graph.ts`(AudioNodeData 加 `source`/`prompt`/`model`;`audioSyncPatch` 回填)、`CanvasToolbar.vue`(「创作」组「配乐」入口)、`AudioNode.vue`(创作态内联表单 + 预估价 + 提交 + 失败重试)、`CanvasEditorView.vue`(`submitMusicTask` 照 `onVoiceSpeech` 链:先落节点 → flush → create task;目录拉取 `/music-models`)、`GenerationRecordsPanel.vue`(music 行插入)。
- `admin-web`:渠道表单加 `tencent-mps` 类型与「音乐」能力勾选、config 字段(bucket/region);价格页按次轨照旧。
- `docs/api.md`:三端点 curl 用例;`README.md` 渠道类型表加一行;`CONTEXT.md` 已回写(2026-10-27:配乐/音效/歌曲三词条、音频节点 60 号增补、待补注记),实现收口时「渠道」「画布任务」「计价」词条各补一句(能力位 music、kind=music、按次)。
- 不动:`GenerationComposer.vue`/`composer.ts`(58 冻结领域模块、57 口径)、`connection.ts` 矩阵(audio 不作源不变)、素材层、`/v1/audio/models` 过滤。

## 验证点

**前置验证(实现前先过,过不了回到 grilling)**:

- (a) 腾讯云账号开通 MPS;控制台能否看到 AIGC 音频单价——看不到则按资源包折算估价录入(自用网关自负盈亏,但「未配价拒绝」不破)。
- (b) MPS 输出桶:`DescribeAigcAudioTask` 回的是可直取地址还是对象键;桶私有时拉回需签名下载。
- (c) MPS 取消接口有无——有则补第三面。
- (d) `DescribeAigcAudioTask` 输出是否带时长/格式(决定转存扩展名由 Content-Type 还是任务字段定;按秒结算预留)。

**实现验证**:

- 真 key 全链:提交/轮询/成功转存/失败退款/超时收尾/换道(第二 MPS 渠道或禁用主渠道模拟);产物在 URL 过期前转存成功。
- 计费不变量:预扣 = 单价×1、成功实结、失败全退、`api_key_quota_log` 流水;未配价拒绝 `model_not_priced`;能力位未开的渠道不被调度。
- 密钥只写不读:渠道 GET 只回 `has_key` + 尾 4 位;config 里 secret 同款。
- 目录:`/music-models` 只含 music 能力渠道 ∩ call 配价;`/audio-models` 不受影响(TTS 下拉不混入音乐模型)。
- 画布:工具栏「配乐」落创作态节点;描述空/超 2000 禁发;预估价随模型切换;提交后状态徽章(排队/生成中/失败+重试);产物回填播放;刷新后创作态表单随整图 JSON 保留;素材面板/生成记录插入落音频节点;audio 节点不可作源、不唤起对话框。
- 双方言:`EnsureSchema` 零迁移,但 kind=music 在 MySQL 与 SQLite 都走完整链;桌面版(无 Redis)冒烟。
- `make test`、`make lint`、`pnpm --filter canvas-web build`(vue-tsc)、浏览器点验;部署欠账:渠道建 MPS + 配价 + `make up --build`。

## Out of Scope(另票/不做)

- 音效生成(61 号票,需新增海外渠道类型)。
- 歌曲:歌词/人声开关、`lyrics_optimizer`。
- 参考音频翻唱(cover)、时长控制、裁剪/截取、结构化风格标签。
- 主动重生成(先定「覆盖还是落新节点」;音频不作源让链式迭代不成立)。
- 配乐进视频链(自动收编 `reference_audio`)——逐渠道核实音频参考语义后另票。
- 分镜 cut「台词/音效」注记直发生成;成片时间线/导出(候选 #8/#11 未选)。
- MiniMax 直连音乐渠道(存量权限另票)、百炼 Fun-Music(邀测)、方舟 seedmusic(开放性未证)。
- 网关取消面(MPS 取消接口查证后再议)、按秒实结轨。
