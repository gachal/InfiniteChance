# 61 音效生成(SFX)——VOD AIGC 聚合渠道(2026-10-28 重开改写)

Type: feat
Status: implemented(2026-10-28 完成于 `feat/61-vod-sfx`。实现注记:**seconds 列复用**——`canvas_tasks` 不加列,video 语义扩为 video/sfx 两用,61 号票 schema 迁移量归零;**目录端点**——网关 `GET /v1/audio/sfx-models`(CapSfx ∩ second 配价)+ canvas `/sfx-models` 代理 + `ListSfxModels` client + 前端下拉,集成方对票面「表单 = 模型 + 描述 + 秒数」的补全;**actual=reserved 定格**——second 轨恒等结清(video.go GetVideoTask succeeded 同款先例),计费不变量以恒等满足;**同步等待独立预算** `vodSfxPollInterval=5s` / `vodSfxTotalBudget=3min`(adaptor 字段化,测试可收紧),超时 504 按临时失败退款换道;**恢复语义照 runTts**——同步任务无远端句柄,重启回队按全新调用重跑,双计费窗口由网关同步面必然终态兜底(runSfx 注释留痕);worker 时限独立旋钮 `WithSfxTaskTimeout` 默认 10min。**部署欠账**:①VOD 控制台「权限管理」给 Kling 音效开模型白名单;②渠道勾 sfx 能力位 + model_map 加 `vod-kling-sfx`→`Kling sfx`;③估价配价走 **second 轨**(`unit=second`,`cny_per_call` = 元/秒,Kling 刊例未公开,核对后修正);④`make up --build` 重建网关与画布服务。EL sound_t2s_v2 后续 model_map 加行即可支持(秒数域 [1,30],域校验按模型映射,本票未加)。原 Status:drafted(2026-10-27 随 60 号票 Q1 拆出;物种定义已回写 CONTEXT.md「音效」词条;**2026-10-28 渠道前提重写**——原挂「新增海外渠道类型(ElevenLabs)」,因 VOD AIGC 聚合出现大陆可达的独立音效上游而触发本票自设的「渠道选择重开」验证点;62 号票(歌曲,同走 VOD)先行实现,本票紧随)

## Question

60 号票 Q1 定案把「BGM/音效生成」拆成两票:配乐先行(60),音效独立。音效与配乐是**不同模型、不同端点、不同计价**的物种(≤30 秒短音频)。本票记录事实底座与定案。

事实底座(2026-10-28 更新;`[官方]` = 厂商文档或用户提供的接入指南摘录):

- **腾讯云 VOD AIGC 聚合**(接入指南 3.13 章)`[官方]`:文生音效两个模型——**Kling**(`SceneType=sfx`,`Duration` [3,10] 秒)与 **EL sound_t2s_v2**(`Duration` [1,30] 秒);与歌曲(62 号票)同走 `CreateAigcAudioTask` @ `vod.tencentcloudapi.com` → `DescribeTaskDetail` 轮询,渠道凭据与 20 号票生图共用;**大陆直连**。OutputConfig 同族:`StorageMode` Temporary(临时 URL 7 天)/Permanent、`OutputAudioFormat`。
- **原 ElevenLabs 调研底座(渠道已否,留作历史参照)**:业界事实标准 `POST /v1/sound-generation` `[官方]`:`text` + `duration_seconds`(0.5–30)+ `prompt_influence` + `loop`;同步返回音频字节流;按秒计 credit;**大陆不可达**——此可达性前提是本次重开的直接原因。
- 其余排查线(2026-10-27 调研,仍成立):已接渠道中 MiniMax 无独立音效端点;百炼 Fun-Music 只做音乐;方舟 Doubao-Seed-Audio 1.0 是一体化成品音轨不是独立音效素材,API 开放状态存疑 `[二手]`;OpenAI 无;Stable Audio 3.0 音乐与音效同端点靠 `TrackType` 区分 `[二手]`,未核实。

## 定案(2026-10-28 grilling;原 ElevenLabs 草案的契约骨架保留,渠道段重写)

1. **渠道**:`tencent-vod`(与 62 号票同渠道同凭据),首批模型 **Kling sfx**(秒数域 [3,10])。能力位**独立 `sfx`**,不复用 60 号票的 `music`——物种/计价/时长域不同,能力显式开启互不串道(原判不变)。EL sound_t2s_v2 列后续(秒数域 [1,30],model_map 加行 + 域校验按模型映射)。
2. **契约**:同步 `POST /v1/audio/sfx`(`{model, text, seconds}` → 直接回音频 URL),照 `/v1/audio/speech` 同步形状,无需异步三面。VOD sfx 虽是异步任务,但照 `vod.go` 生图先例**在 adaptor 内同步等待**(独立预算,建议 3 分钟 + 5 秒轮询;vodPollInterval/vodTotalBudget 有测试可收紧的先例)。**`loop` 字段删除**——Kling/EL 上游均无循环参数,原 ElevenLabs 特性无处翻译。`seconds` 网关统一 1–30 预校验,模型域(Kling [3,10])交上游裁。
3. **计价**:按秒(`UnitSecond`)——请求 `seconds` 预扣 → 实结多退少补(照「预扣 → 多退少补 → 失败退款」时序);Kling sfx 在 VOD 的刊例未公开收录,**估价录入挂部署欠账**(与 62 号票音乐同款),`model_not_priced` 兜底。
4. **画布**:复用 AudioNode 创作态(`source` 加 `'sfx'`),表单 = 模型 + 描述 + 秒数;产物落音频节点;不进对话框、不作源。`canvas_tasks` 加 `seconds` 列(MySQL+SQLite 双方言同改;text 复用 prompt 列)。产物照旧 `archiveAudio` 转存。
5. **可达性**:前提删除(大陆直连),网关无需代理/海外部署。

## 验证点(真 key,实现前)

- VOD 控制台「权限管理」给 Kling 音效开模型白名单。
- `SceneType=sfx` 全链:同步端点出音频、转存落素材、秒数与产物时长一致(计费实结依据)。
- 刊例核对后修正估价配价。

## Out of Scope

- 一体化音频(Seed-Audio 式成品音轨)——物种不同,不冒充音效。
- 音效进视频链、分镜注记直发生成、成片时间线。
- 视频生音效(Kling 吃参考视频的场景)——文生音效先行,要时另票。
