# 25 火山方舟 Seedance 渠道(/v1/videos 契约扩展 + 视频 token 轨)

Type: grilling
Status: approved（2026-09-30 grilling 定案,待实现;实施顺序先于 24 号票或与其并行）

## Question

火山引擎方舟(Ark)的 Seedance 2.5 / 2.0 生视频怎么接入?渠道类型、鉴权、异步任务翻译怎么落?`/v1/videos` 契约如何表达多参考(图/视频/音频)与首尾帧?厂商按 token 计费且任务实报用量,视频计费轨怎么改(token 轨与存量 second 轨如何共存)?

## Answer

1. **渠道类型 `volcengine-ark`**:`SupportedTypes` 追加;**单 API Key** 存渠道既有 `apikey` 列(Bearer 头,无重签名,不新增 config 列凭据;BaseURL 必填,含版本路径,如 `https://ark.cn-beijing.volcengine.com/api/v3`——换 region 即换 BaseURL,不新增配置项)。能力面 MVP **仅 `videos`**:Chat/ChatStream/Images* 落 tencent-vod 同款显式不支持桩(调度不串道,声明什么能力才有什么流量;将来接豆包对话/Seedream 生图时在 adaptor 内把桩换成透传即可)。一键连通测试 = GET 查询不存在的任务 ID:业务级「任务不存在」类错误 = 凭据与连通全通,401/鉴权类 = 凭据失败(tencent-vod 假任务探测同款套路,具体错误码实现期用真 key 验证)。

2. **adaptor(Videos\* 三方法翻译 Ark 任务面)**:Submit = `POST {base}/contents/generations/tasks`;Query = `GET {base}/contents/generations/tasks/{id}`;Cancel = Ark 取消端点(方法与路径以官方文档为准,实现期验证;网关「取消对外不扣费、厂商失败只留日志」语义不动)。参数映射:`seconds → duration`(数值直传,不校验枚举)、`size → resolution`(档位串直传)、`ratio → ratio`、参考结构 → `content[]` 分节 + role(text 分节承载提示词;首帧/尾帧/参考的角色分节形态 = 验证点,视频/音频参考的 item 结构一并验证)。状态归并:Ark 的 queued/running/succeeded/failed/cancelled 对 `videotask.MergeStatus` 复核补齐(未知态归并 failed 的既有规则不动)。成功产物从响应 content 解 `video_url`;usage 取响应 `usage.completion_tokens`(官方计费对账依据)。

3. **`/v1/videos` 契约扩展**:提交体在 `model/prompt/seconds/size/image` 之外新增 **`last_image`**(单串,尾帧)、**`references`**(数组,元素 `{url, kind}`,kind ∈ image|video|audio,≤9 条)、**`ratio`**(显式宽高比,22 号票生图同款哲学:不校验枚举交上游裁)。约束:`image`/`last_image`/`references` 条目一律 http(s) 地址,`data:` 内联 400(12 号票契约),URL 长度上限对齐既有 `maxImageURLRunes`;`image`(单串首帧)与既有请求**逐字节兼容**。openai 形渠道对新参数**全量透传不剥离**(请求体仅重写 model,新字段自然携带;上游不识别 4xx 透回——22 号票先例)。直连调用方与 canvas worker(24 号票)共用此契约。

4. **视频 token 计费轨(本票核心)**:`model_prices` 允许视频模型配 **token 轨**(`unit=token`,config 复用 input/output 每 Mtoken 微美元字段;视频实际消耗体现在 output——厂商实报 `completion_tokens`;另加可选估算表 `size_tokens_per_second`,键 = size 档位串如 `"720p"`,值 = 该档每秒 token 数,取值按火山公布的折算填;再加标量缺省 `default_tokens_per_second` 兜底)。计价校验从「视频须 second 轨」放宽为「**视频按模型所配轨道结算:second 或 token 皆收**,其余轨道仍 `model_not_priced`」。token 轨账务时序:**提交预扣** = ⌈每 Mtoken 单价 × 估算 token⌉,估算 = `size_tokens_per_second[size]`(size 未传或档位未配 → 落 `default_tokens_per_second`)× seconds;**成功终态结算** = 按厂商实报 `completion_tokens` 多退少补(差额为零不落 settle 流水,与生图同款);**厂商未报 usage 时回退按预扣估算定格实结**,用量摘要留痕 `usage missing, billed estimate`(任务已跑完不退款,也不虚记);失败/取消全额退款语义不变。**存量 second 轨视频模型零影响**:账务按所配轨道分叉,second 轨「定格实扣」照旧。用量日志 token 轨视频行 `unit=token`、completion_tokens 列记视频 token 数,price_snapshot 记请求事实(size/seconds)+ 实报 tokens,审计视图照既有「token 轨读 token 列」路径出数。

5. **公开模型与配价**:`seedance-2.5`、`seedance-2.0` 两个公开模型(一档一名,20 号票先例);上游名 = 方舟带日期后缀 ID(形如 `doubao-seedance-2-5-…`),由渠道模型映射配置,精确后缀以方舟控制台为准(验证点)。配价前调用照常 `model_not_priced`;价格数字由管理员按火山牌价折算配置,票不锁数。

6. **出范围**:VOD AIGC 生视频(20 号票待补,另立票)、`/v1/videos` 直连产物转存(23 号票遗留,待另立票)、Seedance 首尾帧之外的花式编辑能力、素材库挑选作为参考来源(21 号票遗留)。

7. **产出面**:`internal/channel`(类型常量/Normalize/连通测试分流)、`internal/relay`(ark adaptor 新文件 + `adaptorFor` 分发、`video.go` 契约解析与计费轨道分叉、`pricing` 估算表字段与校验)、`internal/videotask`(状态归并复核;如需在行上记实报 tokens 则双方言加列)、admin-web 渠道表单加类型选项、`packages/api` 类型、`docs/api.md`(视频直连示例更新);CONTEXT.md「渠道」「任务」「计价」词条随实现回写。测试:adaptor 单测(翻译/状态归并/usage 解析)、契约校验(last_image/references/ratio 四例)、token 轨账务(预扣估算/实结多退少补/无 usage 回退/失败退款)、second 轨回归、双方言迁移,go test 全绿。
