# 01 厂商 API 适配矩阵

Type: research
Status: resolved
Blocked by:

## Question

候选厂商在**聊天、生图、生视频**三类能力上的 API 形态、鉴权方式、同步/异步模式、计费单位与价格档位分别是什么?产出一张适配矩阵,标注:哪些厂商三类齐全、哪些只有部分能力;生视频厂商异步任务模式的共性(提交/轮询/回调、任务生命周期状态);各家与 OpenAI 兼容风格的偏差点。这是网关 API 契约(03)与计费模型(04)的直接输入。

候选厂商清单(调研中可增删,标注理由):
- 聊天:OpenAI、Anthropic、Google Gemini、DeepSeek、阿里通义 Qwen、智谱 GLM、Moonshot Kimi、字节豆包
- 生图:OpenAI gpt-image、即梦、豆包 Seedream、通义万相、Flux(经聚合平台)
- 生视频:快手可灵 Kling、即梦、Vidu、通义万相、Google Veo、Runway、MiniMax 海螺

## Answer

> 调研日期:2026-09-01。价格为数量级参考,以各官方价格页实时数据为准。

## 一、适配矩阵总览

| 厂商 × 能力 | 端点风格 | 鉴权 | 同步/异步 | 计费单位与档位 | 与 OpenAI 兼容偏差 |
|---|---|---|---|---|---|
| **OpenAI · 聊天** | `POST /v1/chat/completions`(及 `/v1/responses`) | `Authorization: Bearer sk-...` | 同步 + SSE 流式 | token;gpt-5 约 $1.25/M 入、$10/M 出,nano 档低至 $0.05/M 入 | ——(基准) |
| **Anthropic · 聊天** | `POST /v1/messages` | `x-api-key` + `anthropic-version: 2023-06-01` 头 | 同步 + SSE(自有事件流) | token;旗舰约 $3~15/M 入、$15~75/M 出量级 | **自成体系**:无 chat/completions;system 为顶层参数、max_tokens 必填、content 分块、usage 字段名不同 |
| **Gemini · 聊天** | 原生 `POST /v1beta/models/{m}:generateContent`;**官方 OpenAI 兼容层** base `.../v1beta/openai/` | `x-goog-api-key` 头(兼容层可用 Bearer) | 同步 + `:streamGenerateContent` | token;Gemini 3 Pro $2/$12 per M,Flash $0.5/$3 per M | 兼容层可直接用 OpenAI SDK,但部分参数(logprobs 等)不支持;原生协议路径/响应结构完全不同 |
| **DeepSeek · 聊天** | `POST https://api.deepseek.com/chat/completions` | Bearer | 同步 + SSE | token;v4-flash 峰值 $0.44/M 入(未命中)/$1.32/M 出,**错峰半价** | 几乎零偏差;另提供 `/anthropic` 兼容端点与 Responses API |
| **Qwen(DashScope)· 聊天** | 兼容 base `https://dashscope.aliyuncs.com/compatible-mode/v1`;原生 DashScope 协议并存 | Bearer | 同步 + SSE | token;qwen-plus ¥0.8/M 入、¥2/M 出;qwen-max ¥20/M 入、¥60/M 出(分档计价) | 兼容层可用;**部分模型(Qwen-Audio 等)仅支持原生协议** |
| **智谱 GLM · 聊天** | `POST https://open.bigmodel.cn/api/paas/v4/chat/completions` | Bearer API Key(或 JWT) | 同步 + SSE | token;GLM-4.7 约 ¥2/M 入、¥8/M 出;Flash 档免费 | 请求/响应同 OpenAI 格式,但**路径前缀是 `/api/paas/v4`**;另有 `/api/anthropic` 兼容端点 |
| **Kimi(Moonshot)· 聊天** | `https://api.moonshot.cn/v1/chat/completions`(+ `/v1/responses`) | Bearer | 同步 + SSE | token;K3 ¥20/M 入(cache hit ¥2)、¥100/M 出 | 几乎零偏差;扩展参数 `thinking` 需走 extra_body;另提供 `/anthropic` 兼容端点 |
| **豆包(火山方舟)· 聊天** | `POST https://ark.cn-beijing.volces.com/api/v3/chat/completions`(+ Responses API) | Bearer ARK API Key(或 AK/SK 签名) | 同步 + SSE | token;Doubao-Seed 系列 ¥0.8/M 入起、¥2/M 出起 | 兼容层可用;model 传**推理接入点 ep-xxx 或模型名**;Coding Plan 是独立 base(`/api/coding/v3`,不可混用) |
| **OpenAI · 生图** | `POST /v1/images/generations`(gpt-image) | Bearer | **同步**(返回 b64_json) | 按 image token 计费($40/M 输出 token);1024² 约 $0.011(low)~$0.042(medium)~$0.167(high) | 基准 |
| **豆包 Seedream(方舟)· 生图** | `POST /api/v3/images/generations` | Bearer ARK Key | **同步**(OpenAI images 风格) | 按张;Seedream 4.0 约 ¥0.2/张 | 基本同 OpenAI images API(路径前缀 `/api/v3`) |
| **通义万相(DashScope)· 生图** | `POST /api/v1/services/aigc/text2image/image-synthesis` + `X-DashScope-Async: enable` | Bearer | **异步** task_id → 轮询 `/api/v1/tasks/{id}` | 按张;wanx2.1-t2i-turbo ¥0.14/张、wanx2.0 ¥0.04/张;失败不计费 | **非 OpenAI 风格**:专用 header + 任务模式 |
| **Flux(fal.ai)** | `POST https://queue.fal.run/{model-id}` → 轮询 `/requests/{id}/status` | `Authorization: Key $FAL_KEY`(注意前缀是 Key 不是 Bearer) | **异步**队列(IN_QUEUE/IN_PROGRESS/COMPLETED);支持 webhook、SSE | 按百万像素;schnell $0.003/MP、dev $0.025/MP、Flux Pro 约 $0.04~0.055/MP(1MP≈一张 1024²) | **非 OpenAI 风格**:自定义队列协议 |
| **Flux(Replicate)** | `POST /v1/models/{owner}/{name}/predictions` | Bearer r8_ token | 异步 prediction;`Prefer: wait`(≤60s)可近似同步 | 按 output 秒/张计价;Flux 1.1 pro 约 $0.04/张 | 非 OpenAI 风格:prediction 协议 |
| **可灵 Kling · 生视频** | `POST /v1/videos/text2video`、`/image2video` → `GET /v1/videos/{type}/{task_id}` | AccessKey+SecretKey **本地签 JWT** → Bearer | 异步 task;状态 `submitted/processing/succeed/failed`;支持 callback_url | 按秒,1 积分=¥1;Kling 3.0 Turbo 720P ¥0.8/s、1080P ¥1.0/s | 自有 REST,非 OpenAI 风格;**JWT 短时效需网关自动重签** |
| **豆包 Seedance(方舟)· 生视频** | `POST /api/v3/contents/generations/tasks` → `GET .../tasks/{id}` | Bearer ARK Key | 异步 task;状态 `queued/running/succeeded/failed/cancelled`;结果 `content.video_url`;queued 可取消 | **按千 token**(视频也按 token 计费!);1.0 pro ¥0.015/千token(5s 1080p ≈ ¥3.7)、pro-fast ≈ ¥1/5s;2.x ≈ ¥1/s;仅成功计费 | 自有 REST;时长/分辨率走 prompt 内 `--dur/--resolution` 语法或参数字段,与 OpenAI 风格差异大 |
| **Vidu · 生视频** | `POST /ent/v2/img2video`(等)→ `GET /ent/v2/tasks/{task_id}/creations` | `Authorization: Token <key>`(**非 Bearer**) | 异步 task;状态 `created/queueing/processing/success/failed`;支持 callback_url | 积分制,1 积分 ≈ ¥0.031(国内)/$0.005(国际);约 8 积分/秒 ≈ **¥0.25/s 量级** | 自有 REST;鉴权 header 名不同 |
| **通义万相(DashScope)· 生视频** | `POST /api/v1/services/aigc/video-generation/video-synthesis` + `X-DashScope-Async: enable` → `GET /api/v1/tasks/{id}` | Bearer | 异步 task;状态 `PENDING/RUNNING/SUCCEEDED/FAILED/CANCELED/UNKNOWN`;task_id 与结果 URL **24h 有效** | 按秒;万相 2.x 约 ¥0.2~0.7/s 量级(以价格页为准) | 自有 REST;**百炼同一任务接口还托管 Vidu/Kling/PixVerse 等三方视频模型**,等于一个现成的统一视频任务面 |
| **Google Veo(Gemini API)· 生视频** | `POST /v1beta/models/{veo-model}:predictLongRunning` → 轮询 operation 直至 `done:true` → `video.uri` | `x-goog-api-key` | **异步 LRO**(Google 长操作模式,非 task_id/状态枚举) | 按秒;官方约 $0.10/s(Fast 档有效价)~$0.40/s(标准),主流 8s 片长 | 自成一派:LRO operation 轮询,产物 URL 下载仍需带 key |
| **Runway · 生视频** | `POST /v1/text_to_video` 等 → `GET /v1/tasks/{id}` | Bearer + `X-Runway-Version` 头 | 异步 task;状态 `PENDING/THROTTLED/RUNNING/SUCCEEDED/FAILED/CANCELLED`;THROTTLED=超并发未排队 | credit 制,1 credit=$0.01;gen4_turbo 12 credits/s ≈ **$0.12/s** | 自有 REST;多了版本头与 THROTTLED 态 |
| **MiniMax 海螺 · 生视频** | `POST /v1/video_generation` → `GET /v1/query/video_generation?task_id=` | Bearer | 异步 task;状态 `Queueing/Preparing/Processing/Success/Fail`;**成功返回 file_id,还需 `GET /v1/file/retrieve` 两跳取 URL** | 按条(6s)计;Hailuo 768p 约 $0.28/条 ≈ $0.05/s 量级 | 自有 REST;查询走 query param 而非路径;产物两跳,最特殊 |

**能力覆盖结论**:聊天 8 家全兼容或准兼容 OpenAI 风格(Anthropic 是唯一完全自有格式的);生图里 OpenAI/Seedream 可用同一套「同步 images」契约,万相/fal 必须走任务模式;**生视频 7 家全部是异步任务模式,无任何同步 API**;三类齐全的单一厂商只有 OpenAI(聊天+生图,无视频)与火山方舟(聊天+Seedream+Seedance)、DashScope(聊天+万相图+万相视频)——后两者是我们打通三类能力成本最低的国内源。

## 二、生视频异步任务模式共性(网关自定义视频接口的直接依据)

**共性 1:统一的「提交 → task_id → 轮询 → 终态取 URL」生命周期,没有任何厂商提供同步返回。**

| 厂商 | 提交端点 | 查询端点 | 排队态 | 运行态 | 成功 | 失败 | 取消/其他 |
|---|---|---|---|---|---|---|---|
| Kling | `POST /v1/videos/text2video` | `GET .../{task_id}` | `submitted` | `processing` | `succeed` | `failed` | callback_url |
| Seedance(方舟) | `POST /api/v3/contents/generations/tasks` | `GET .../tasks/{id}` | `queued` | `running` | `succeeded` | `failed` | `cancelled`(仅 queued 可取消,24h 清理) |
| Vidu | `POST /ent/v2/img2video` | `GET /tasks/{id}/creations` | `queueing`(前有 `created`) | `processing` | `success` | `failed` | callback_url |
| 万相(DashScope) | `POST .../video-synthesis` | `GET /api/v1/tasks/{id}` | `PENDING` | `RUNNING` | `SUCCEEDED` | `FAILED` | `CANCELED`、`UNKNOWN`(24h 过期) |
| Veo(Gemini) | `POST ...:predictLongRunning` | 轮询 operation | —(隐含) | `done:false` | `done:true` + `video.uri` | error 字段 | LRO,无显式枚举 |
| Runway | `POST /v1/text_to_video` | `GET /v1/tasks/{id}` | `PENDING`/`THROTTLED` | `RUNNING` | `SUCCEEDED` | `FAILED` | `CANCELLED` |
| MiniMax | `POST /v1/video_generation` | `GET /v1/query/video_generation?task_id=` | `Queueing`/`Preparing` | `Processing` | `Success`(给 file_id) | `Fail` | 需二次取文件 |
| fal(参照) | `POST queue.fal.run/{model}` | `GET .../status` | `IN_QUEUE` | `IN_PROGRESS` | `COMPLETED` | error 字段 | 取消 202 |

可归纳为**四段式状态机:排队 → 运行 → 成功/失败(+可选 canceled)**;各家命名大小写/用词不同但语义一一对应。特例只有两个:Runway 的 `THROTTLED`(= 因并发超限未真正排队,语义等同 pending)与 DashScope 的 `UNKNOWN`(= 任务过期/不存在,24h)。

**共性 2:提交端点都是「REST 资源集合」或「领域动作」**——方舟/DashScope 用 `tasks` 资源,可灵/Vidu/Runway 用领域端点(text2video/img2video),MiniMax 用 `video_generation` 动作 + 查询参数。产物在成功态的响应体里,几乎都是**临时 URL,24h 左右失效**(万相 24h、Vidu 24h、方舟 cancelled 态 24h 删除);MiniMax 是唯一「file_id 两跳」的。

**共性 3:回调普遍是「可选优化」而非唯一通道。** 可灵、Vidu、fal、Replicate 明确支持 callback_url/webhook,且回调体结构 ≈ 查询响应体;方舟/DashScope/Runway 以轮询为主流用法。网关设计应**以轮询为可靠基线、回调为加速手段**,并按厂商 QPS 限制(如 DashScope 任务查询 20 QPS)做节流。

**共性 4:计费单位三种并存,且普遍「仅成功计费」。** 按秒(Kling ¥0.8~1/s、Veo $0.1~0.4/s、万相 ¥0.2~0.7/s、Vidu 折算 ¥0.25/s)、按千 token(Seedance ¥0.003~0.015/千token,视频 token 数由分辨率×时长决定)、按条/credit(MiniMax ~$0.28/6s 条、Runway $0.01/credit×12/s)。计费模型(04)必须同时建模 `token / image / video-second / per-item` 四种 unit。

**共性 5:鉴权三种形态**——静态 Bearer key(多数)、`Authorization: Token`(Vidu)、本地签发短时效 JWT(可灵,网关需缓存并自动重签)。适配层需要一个「凭证提供者」接口而不是裸 key 字符串。

## 三、对网关契约(03)/计费(04)的设计提示

1. **聊天统一面**:8 家里 7 家可直接以 OpenAI base_url 透传(仅前缀不同),Anthropic 需要独立适配为「原生 /v1/messages 直通」或做协议翻译;建议网关对外只暴露 OpenAI 风格 `/v1/chat/completions`,内部对 Anthropic 做转换(或同时暴露 Anthropic 风格入口,成本低——因为 Kimi/GLM/DeepSeek 都有现成 Anthropic 兼容端点可对齐)。
2. **生图统一面**:对 gpt-image/Seedream 用同步 OpenAI images 契约;对万相/fal 在适配层内「提交+轮询+等待」封装成同步响应(图片生成秒级~十秒级,可接受);或统一走我们自己的任务接口。
3. **生视频统一面(核心自定义契约)**:对齐共性状态机设计 `POST /v1/videos/generations → {task_id}`、`GET /v1/videos/tasks/{id}`(可选 callback_url),网关内部状态机收敛为 `pending/running/succeeded/failed/canceled` 五态,把 THROTTLED 归并到 pending、UNKNOWN/expired 归并到 failed(带原因码);**成功后立即转存产物到自有对象存储**,不依赖上游 24h 临时 URL。
4. **计费模型**:usage 采集分两类——聊天从响应 usage 字段(input/output/cache 三档,国内各家都有 cache hit 价)取数;媒体类按「张/秒/条」在适配层按上游价目表折算记账;错峰价(DeepSeek)、阶梯价(qwen-plus 按输入长度分档)、积分制(Vidu/Runway/Kling)统一折算为「网关内部计费分」。

## 四、主要来源(官方一手文档)

- OpenAI:https://developers.openai.com/api/docs/pricing ;https://community.openai.com/t/gpt-image-1-collected-pricing-information-and-why-responses-is-undocumented/1275254(gpt-image token 计费)
- Anthropic:https://platform.claude.com/docs/en/api/overview ;https://platform.claude.com/docs/en/manage-claude/authentication
- Gemini:https://ai.google.dev/gemini-api/docs/openai ;https://ai.google.dev/gemini-api/docs/pricing ;https://ai.google.dev/gemini-api/docs/gemini-3 ;https://ai.google.dev/gemini-api/docs/video
- DeepSeek:https://api-docs.deepseek.com/quick_start/pricing
- 阿里百炼:https://help.aliyun.com/zh/model-studio/compatibility-of-openai-with-dashscope ;https://help.aliyun.com/zh/model-studio/model-pricing ;https://help.aliyun.com/zh/model-studio/text-to-image-v2-api-reference ;https://help.aliyun.com/zh/model-studio/video-generation ;https://help.aliyun.com/zh/model-studio/manage-asynchronous-tasks
- 智谱:https://docs.bigmodel.cn/cn/guide/develop/http/introduction ;https://bigmodel.cn/pricing
- Kimi:https://platform.kimi.com/docs/api/overview.md ;https://platform.kimi.com/docs/pricing/chat-k3.md
- 火山方舟:https://docs.volcengine.com/docs/82379/1330626(兼容 OpenAI SDK);/docs/82379/1298459(Base URL 及鉴权);/docs/82379/1824121(Seedream 生图);/docs/82379/1520757 与 /docs/82379/1521309(视频任务创建/查询);/docs/82379/1544106(模型价格)
- 可灵:https://klingai.com/document-api/api/get-started/authentication ;https://klingai.com/document-api/pricing/base/video
- Vidu:https://platform.vidu.cn/docs/image-to-video ;https://platform.vidu.cn/docs/pricing
- fal:https://fal.ai/docs/documentation/model-apis/inference/queue ;https://fal.ai/pricing
- Replicate:https://replicate.com/docs/reference/http
- Runway:https://docs.dev.runwayml.com/api/ ;https://docs.dev.runwayml.com/usage/tiers/
- MiniMax:https://platform.minimax.cn/docs/api-reference/video-generation-query ;https://www.minimax.io/news/video-generation-api
