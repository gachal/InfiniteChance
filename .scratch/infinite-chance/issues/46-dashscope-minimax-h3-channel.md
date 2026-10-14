# 46 阿里云百炼(DashScope)渠道:MiniMax H3 走百炼契约

Type: feat
Status: implemented(2026-10-13)

## Question

45 号票的 `minimax` 渠道类型只接 MiniMax 原生 V2 任务面。阿里云百炼(Model Studio)也上架了 MiniMax-H3,但走的是 DashScope 统一异步契约(`video-synthesis`),形状与原生完全不同——要求新增阿里云渠道类型承接 H3,对外调用契约(画布对话框与直连 API 的 `/v1/videos/generations`)依旧零变化。

阿里云侧调用契约(官方文档页实证,help.aliyun.com/zh/model-studio/minimax-video-generation-api-reference):

- **提交** `POST https://{WorkspaceId}.cn-beijing.maas.aliyuncs.com/api/v1/services/aigc/video-generation/video-synthesis`,请求头必带 `X-DashScope-Async: enable`(缺了报 "current user api does not support synchronous calls")+ Bearer 百炼 API Key;体为 `{"model":"MiniMax/MiniMax-H3","input":{"prompt":…,"media":[…]},"parameters":{…}}` 三段分离。
- **media 素材条目是扁平 `{type,url}`**:`first_frame`(首帧图 ≤1)、`last_frame`(尾帧 ≤1)、`image_url`(参考图 ≤9)、`feature`(参考视频 ≤3)、`driving_audio`(参考音频 ≤3);**图生视频(首/尾帧)与多模态参考互斥**。
- **parameters**:`resolution` **必选** 枚举 `768P | 2K`(无 480P);`duration` **必选** 整数 [4,15] 按秒计费;`ratio` 可选(adaptive 默认;文生视频必填且不能 adaptive,图生视频恒 adaptive);`watermark` 可选默认 false。
- **查询** `GET {base}/api/v1/tasks/{task_id}`:`output.task_status` 大写枚举 `PENDING/RUNNING/SUCCEEDED/FAILED/CANCELED/UNKNOWN`(UNKNOWN = 不存在或超 24h);产物 `output.video_url`(30 天有效);失败原因在 `output.code`/`output.message`;提交失败(4xx)的错误体是顶层 `{"code","message","request_id"}`(非 OpenAI error object 形)。
- **取消:DashScope 无公开的任务取消 API**(文档只有控制台「管理异步任务」);task_id 有效期 24h。
- 模型名带斜杠前缀 `MiniMax/MiniMax-H3`;仅华北2(北京)地域,模型/Endpoint/Key 须同地域。

## Answer

- **新渠道类型 `dashscope`**(阿里云百炼),走既有 adaptor 模式(25/45 号票先例):单 Bearer key(百炼 API Key)存渠道 `apikey` 列;BaseURL 必填为**工作空间域名根、不含路径**(如 `https://xxxx.cn-beijing.maas.aliyuncs.com`;旧 `dashscope.aliyuncs.com` 域同样可用,adaptor 统一拼 `/api/v1/...` 路径);MVP 仅 `videos` 能力(该异步面同样承载 PixVerse/万相等第三方视频模型,后续同类模型加映射与价格即可,不另开类型)。
- **公开模型名沿用 `minimax-h3` → 上游 `MiniMax/MiniMax-H3`**:价格按公开模型一份、画布 `resolutionPresetsFor`/`durationRangeFor` 按公开名分档(4–15s、自动/768P/2K)全部复用,**原生与百炼两渠道可同模型共存、换道语义(预扣原封带往下一渠道)才成立**。两渠道成本不同但同价是操作者取舍(票面记明,per-channel 价格另议);配价仍 second 轨,系数键 = 官方档位串 `768P`/`2K`。
- **adaptor(`internal/relay/dashscope.go`)翻译三方法**:提交体三段重组(prompt 进 `input.prompt`,首帧/尾帧/参考按扁平 `{type,url}` 进 `input.media`,`seconds → parameters.duration`(缺省补 `defaultVideoSeconds`,与 MiniMax 同款必填补齐,账实一致),`size → parameters.resolution`、`ratio → parameters.ratio` 档位串官方枚举直传不归一,`watermark` 不传 = 官方默认 false);查询响应归一 `output.task_status → task_status`(五态机 `MergeStatus` 本就大小写不敏感:PENDING/RUNNING/SUCCEEDED/CANCELED 天然归并,**UNKNOWN 走未知态归 failed**,任务不存在/过期即失败退款,语义正确)、`output.video_url → video_url`、`output.code/message → error.message`;usage 无 completion_tokens 不随行(second 轨结算用不到)。`ErrorSummary` 覆写:优先 `output.{code,message}`,回落顶层 `{code,message}`(DashScope 错误体不是 OpenAI 形),再回落基类。
- **取消面如实降级**:DashScope 无公开取消 API,adaptor 的 `VideosCancel` 返回合成 2xx——网关照常本地取消全款退、厂商侧任务自然耗尽(该任务的钱厂商照扣,票面记明);非 2xx 不拦本地取消的既有语义不受影响。
- **上游约束透传**:`resolution` 必选(画布「自动分辨率」= 不传 size,百炼渠道会 400 透出——对话框选显式档位即可)、文生视频 ratio 必填且不能 adaptive、图生视频恒 adaptive、首尾帧与多模态参考互斥,均交上游裁决 4xx 透回(与 45 号票同款处理)。
- 对外契约零变化:relay 骨架/video.go/videotask/调度熔断零改动;画布前端零改动(公开名沿用即复用全部分档)。
- **勘误(45 号票遗留文档缺陷,随本票修正)**:`minimax` 渠道 BaseURL **不含 `/v2` 版本路径**——tester 与 adaptor 代码都在 BaseURL 后拼接 `/v2/...` 完整路径,票面/CONTEXT.md/管理台提示写的「BaseURL 含 /v2(如 `https://api.minimaxi.com/v2`)」会拼出 `/v2/v2/...` 404。正确值:中国站 `https://api.minimaxi.com`、国际站 `https://api.minimax.io`。同步修正 channel.go 注释、CONTEXT.md、ChannelsView 提示、45 号票部署动作。

## 产出面

- `internal/channel/channel.go`:`TypeDashScope = "dashscope"` 入 `SupportedTypes`;TypeMiniMax 注释勘误。
- `internal/channel/tester.go`:`testDashScope`(GET 假任务探测:2xx 含 `UNKNOWN` 体 / 404 / 400 = 凭据连通全通——UNKNOWN 即「任务不存在」的另一种回答形态;401/403 = 凭据失败;错误体佐证取顶层 `code`)。
- `internal/relay/dashscope.go`:adaptor(见上);`relay.go` `adaptorFor` 加分支。
- `internal/relay/dashscope_test.go`:fake DashScope 上游端到端——翻译契约(三段分离/扁平 media 五类/必填 duration 补缺省/官方档透传/X-DashScope-Async 头/Bearer/路径/output.task_id 提取)、second 轨账务(预扣 → 成功定格零差额 / 失败退款 / UNKNOWN 归 failed 退款)、取消合成 2xx 不拦本地退款、2xx 无 task_id 拒绝不建行。
- `internal/channel/dashscope_test.go`:Normalize 类型规则 + 连通探测分类(ark_test.go 同款表驱动)。
- `packages/api` `CHANNEL_TYPES` 加 `'dashscope'`;`admin-web` ChannelsView 类型下拉「阿里云百炼(DashScope 视频生成)」+ isDashScope 提示;**minimax 提示勘误**(BaseURL 不含 /v2)。
- `CONTEXT.md`:渠道条目(类型清单 + minimax BaseURL 勘误 + dashscope BaseURL 规则)、「阿里云百炼渠道」新条目、任务条目 adaptor 清单补 dashscope;`docs/api.md` 渠道示例补 dashscope。

## 部署侧动作(代码外)

1. 百炼控制台开通 MiniMax-H3 并授权,取百炼 API Key 与工作空间域名(`https://{WorkspaceId}.cn-beijing.maas.aliyuncs.com`)。
2. 建渠道:type=`dashscope`、BaseURL=工作空间域名根(不含路径)、APIKey、ModelMap `minimax-h3→MiniMax/MiniMax-H3`、勾 videos;连通测试点验。
3. 配价:若与原生渠道共用公开名 `minimax-h3` 则**无需新配价**(second 轨同价共用);配 `768P`/`2K` 系数即可(45 号票已配则零动作)。
4. 真机注意:百炼仅华北2(北京)地域;媒体输入约束比原生严(首尾帧与参考互斥)。

## 验证点(真 key 实证,照 25/45 号票纪律)

- 假任务探测的真实回答形态(404 `NotFoundTaskId` 还是 200+UNKNOWN 体)与 401 文案;提交缺 `X-DashScope-Async` 的报错是否如文档。
- 真任务全链路:提交/轮询/产物转存;`UNKNOWN` 归 failed 的真实触发(任务过期场景可遇不可求,代码路径已测)。
- 画布:模型下拉出 H3(百炼渠道建好配价后)、自动档在百炼渠道提交被 400 透出的文案、768P/2K 正常出片。
- 已跑:`go test ./internal/relay/ ./internal/channel/`(relay 5 新用例 + channel 2 新用例全绿)+ `make lint` + 前端 vue-tsc/vitest。
