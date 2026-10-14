# 45 MiniMax H3 视频渠道(调用契约与 Seedance 兼容)

Type: feat
Status: implemented(2026-10-13)

## Question

网关新增 MiniMax H3 视频生成支持,要求调用方法与现有 seedance-2/2.5 完全兼容——调用方(画布对话框与直连 API)继续打同一套 `/v1/videos/generations` 契约(prompt/seconds/size/ratio/image/last_image/references),只换 model 名。

MiniMax 官方 V2 视频任务面(platform.minimax.io 文档实证):`POST {base}/v2/video_generation` 提交(应答 `{"task_id":…}` 纯数字串)、`GET {base}/v2/query/video_generation/{id}` 轮询(**响应包一层 `{"task":{…}}`**,产物在 `content.url`,状态枚举 queued/running/succeeded/failed/cancelled)、`DELETE {base}/v2/video_generation/{id}` 取消(queued→cancelled 不收费、succeeded/failed→deleted、running 报错)。Bearer 单 key。content 分节与角色枚举(first_frame/last_frame/reference_image/reference_video/reference_audio)与 Ark 同构。

## Answer

- **新渠道类型 `minimax`**,走既有 adaptor 模式(25 号票先例):单 Bearer key 存渠道 apikey 列,BaseURL 必填且为裸域名(中国站 `https://api.minimaxi.com`、国际站 `https://api.minimax.io`;`/v2` 版本路径由 adaptor 与连通测试拼接,46 号票勘误——本票初版误写「含 /v2」会拼出 `/v2/v2`);MVP 仅 `videos` 能力,聊天/生图显式不支持桩。
- **计价走 second 轨**(与 Seedance 的 token 轨不同):官方按秒计价(768P $0.08/s、2K $0.13/s,中国站另有人民币价),second 轨「预扣 = 每秒单价 × 分辨率系数 × 秒数、成功定格为实扣零差额」天然对齐;配价后画布 `/video-models` 目录自动收录。token 轨方案否决——需自定 token 折算率,且 MiniMax 的 token 语义(输入素材折算)与按秒定价不一致,预扣与结算会漂移。
- **size 档位串以各厂商官方枚举为准、纯透传不归一**:H3 官方档 `480P`/`768P`/`2K`(大写,2K 仅 H3;480P 仅 H3-Max),Seedance 保持 `480p`/`720p`/`1080p`(小写)。计价 FactorMicros 键即官方串。不做 720p→768P 之类映射(语义漂移,不造魔法)。
- **画布前端按模型分档**(durationRangeFor 同款套路):`durationRangeFor` 加 minimax-h3(4–15)/minimax-h3-max(5–15)分支(公开名连字符与上游名大写两形态都认,max 先判);分辨率预设改造为 `resolutionPresetsFor(model)`(H3→自动/768P/2K,H3-Max→自动/480P/768P,其余回落默认档 `VIDEO_RESOLUTION_PRESETS` 原四档);模型切换时已选档不属于新模型回落「自动分辨率」(watch,防静默持无效档提交必 400)。
- 对外契约零变化:relay 骨架/video.go/videotask/调度熔断零改动;公开模型名 `minimax-h3`(ModelMap 映射上游 `MiniMax-H3`);adaptor 模型无关,H3-Max 配上映射与价格即可用,辅助端点(H3-Context-IR prompt 增强、Regeneration 超分)显式不做,另票。

## 上游约束(透传交上游裁决,4xx 透回)

- 纯文生视频时 ratio 必填且不能 adaptive(对话框比例留空会被上游 400 透出,与 Seedance 编辑型契约提示同款处理);有媒体输入时 ratio 缺省 adaptive。
- content 必须含非空 text(网关 prepareVideo 已强制 prompt 非空,天然满足);duration 必填整数(网关 seconds 缺省 5,天然满足)。
- 参考图 ≤9(官方前 5 张免费)、参考视频 ≤3 且每段 2–15s 合计 ≤15s、参考音频 ≤3、混传合计 ≤12 文件——画布限参考视频/音频各 1、三类合计 ≤9,更严,天然兼容不放开。

## 产出面

- `internal/channel/channel.go`:`TypeMiniMax = "minimax"` 入 `SupportedTypes`;`tester.go` `testMiniMax`(假任务 GET 探测,404/400 = 凭据连通全通,401/403 = 凭据失败;MiniMax 错误体佐证字段是 `error.type`,与 Ark 的 `error.code` 不同)。
- `internal/relay/minimax.go`:adaptor(提交翻译复用 `arkMediaPart`/`arkReferenceRole`——分节同构;查询归一剥 `{"task":{…}}` 包裹、`content.url → video_url`、`error{code|type,message}` 兜底取 message、`usage.completion_tokens` 随行;取消 DELETE;2xx 无 task_id 防御 502);`relay.go` `adaptorFor` 加分支。
- `internal/relay/minimax_test.go`:fake V2 上游端到端——翻译契约(六分节/双命名/官方档透传/Bearer/路径)、second 轨账务(预扣 500000 → 成功不动账无 settle、usage 行 unit=second)、失败退款、cancelled 归并退款、取消走 DELETE、2xx 无 task_id 拒绝不建行。
- `packages/api` `CHANNEL_TYPES` 加 `'minimax'`;`admin-web` ChannelsView 类型下拉「MiniMax(H3 视频生成)」+ isMiniMax 提示(单 Bearer key、BaseURL 含 /v2、仅 videos)。
- `canvas/web` `composer.ts`(时长区间 + `resolutionPresetsFor`)、`GenerationComposer.vue`(下拉按模型收敛 + 切换回落 watch)、`composer.test.ts`(新用例)。
- `CONTEXT.md`:渠道/新「MiniMax H3 渠道」条目/任务/生成对话框条目回写;`docs/api.md` 配价示例补充。

## 部署侧动作(代码外)

1. 建渠道:type=minimax、BaseURL=`https://api.minimaxi.com`(国际站 `https://api.minimax.io`;裸域名,`/v2` 路径由网关拼接——46 号票勘误)、APIKey、ModelMap `minimax-h3→MiniMax-H3`(H3-Max 再加 `minimax-h3-max→MiniMax-H3-Max`)、勾 videos;连通测试点验。
2. 配价(second 轨、微人民币):`PUT /admin/prices`,per_call = 每秒基准价,factor_micros 以 `768P`/`2K`(及 H3-Max 的 `480P`)为键;美元官方价(768P $0.08/s、2K $0.13/s)按中国站人民币价折算,38 号票全链人民币纪律。
3. `make up --build` 重建。

## 验证点(真 key 实证,照 25 号票纪律)

- 查询响应包裹形状与字段已按官方文档页实证(`{"task":{…}}`、`content.url`);真 key 复核:提交/轮询/取消的具体错误码、任务不存在探测的状态码(400 invalid task_id 还是 404)、文生视频不选比例的 4xx 文案、`cancelled` 双 L 归并。
- H3-Max 全链路(配价 + 480P/768P 档)。
- 画布:模型下拉出 H3(配价后)、分辨率档 768P/2K、时长 4–15、切模型回落自动档。
- 已跑:`go test ./internal/relay/ -run TestMiniMax`(4 全绿)+ canvas/web vitest(122 全绿)+ vue-tsc。

## 勘误(2026-10-13,46 号票随票修正)

BaseURL 不含 `/v2` 版本路径:tester 与 adaptor 代码都在 BaseURL 后拼接完整 `/v2/...` 路径,本票初版「BaseURL 必填含 /v2」与代码矛盾,照做会请求 `/v2/v2/...` 404。正确值:中国站 `https://api.minimaxi.com`、国际站 `https://api.minimax.io`。channel.go 注释、CONTEXT.md、管理台提示已同步修正。渠道尚未部署(45 号票部署欠账),无存量配置受影响。
