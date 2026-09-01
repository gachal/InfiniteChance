# 02 开源网关参考调研(one-api/new-api)

Type: research
Status: resolved
Blocked by:

## Question

one-api、new-api 等开源 LLM 网关在以下方面是怎么做的?产出可借鉴要点与应避开的坑清单,侂数据模型(05)与计费模型(04)参考:

- 渠道(channel)适配层架构:如何抽象「OpenAI 兼容/厂商原生」两类上游,模型名映射怎么做
- 数据模型:channel、token(key)、usage log、quota 的表结构与字段
- 计费:倍率(group/model ratio)、预扣与退款、按次计费的生图怎么处理
- OpenAI 兼容层:流式 SSE 转发、错误格式归一化的实现方式
- 有哪些已知的设计债或社区抱怨(如渠道健康检查、并发扣减精度问题)

## Answer

调研基于两个仓库的源码(main 分支,2026-09 拉取):one-api(songquanpeng/one-api)与 new-api(QuantumNous/new-api,one-api 的活跃超集 fork)。结论按主题分「可借鉴 / 应避开」,来源以文件路径标注。

### 核心抽象总览

one-api 系的请求主链路:`auth 中间件(验 key/额度) → distributor 中间件(定模型→选渠道→注入 ctx) → relay controller(算倍率→预扣→适配器转换→转发→流式回写→goroutine 结算+记日志)`。渠道与协议是**两层编号**:ChannelType(运营层,如 Azure/OpenRouter/Doubao)映射到 APIType(协议族,如 OpenAI/Gemini/Anthropic);OpenAI 兼容上游全部复用同一个 openai Adaptor,在 Adaptor 内部按 ChannelType 分支处理 URL/头部差异。见 `relay/channeltype/define.go`、`relay/adaptor.go(GetAdaptor)`。

---

## 可借鉴

### 1. 渠道适配层架构

- **Adaptor 接口**(one-api `relay/adaptor/interface.go`):`Init / GetRequestURL / SetupRequestHeader / ConvertRequest / DoRequest / DoResponse(返回 usage) / GetModelList / GetChannelName`。转换进、usage 出,计费与协议解耦;`DoResponse` 返回 `*model.Usage` 是计费数据唯一入口,这个边界划得很好。new-api 同形接口扩展了 `ConvertClaudeRequest / ConvertGeminiRequest / ConvertOpenAIResponsesRequest`(`relay/channel/adapter.go`)。
- **Meta 请求上下文**(one-api `relay/meta/relay_meta.go`):把 ChannelType/ChannelId/TokenId/UserId/Group/ModelMapping/OriginModelName/ActualModelName/BaseURL/APIKey/IsStream 等收拢成一个 struct,避免 gin ctx 到处取 key。OriginModelName(计费/日志)与 ActualModelName(上游)分离是显式设计。
- **模型名映射**:渠道级 JSON 字符串字段 `ModelMapping`(map[请求名]上游名,`model/channel.go GetModelMapping()`),distributor 存入 ctx,relay controller 在构造上游请求前 `getMappedModelName` 应用(`relay/controller/text.go`);映射只影响上游调用,计费与日志用 OriginModelName;失败重试时保留原始模型名重新选渠道。
- **ability 路由表**(one-api `model/ability.go`):`(group, model, channel_id, enabled, priority)` 复合主键的扁平表,选渠道 = 查表 + 「最高 priority 层内随机」,失败重试传 `ignoreFirstPriority` 降级到低优先级层。new-api 同表增加 weight,官方 FAQ:同 priority 内按 weight 加权(`https://doc.newapi.pro/support/faq/`)。
- **new-api 的异步任务计费钩子**(`relay/channel/adapter.go TaskAdaptor`):`EstimateBilling(按请求参数估 ratio) → AdjustBillingOnSubmit(按上游受理实参调差额) → AdjustBillingOnComplete(轮询到终态时多退少补)`,是生视频/MJ 这类异步按次业务的成熟形态,值得我们生视频链路直接参考。
- new-api 的多 key 渠道:`ChannelInfo{IsMultiKey, MultiKeyStatusList, MultiKeyPollingIndex, MultiKeyMode}` 一个渠道挂多把 key、key 级禁用与轮询(`model/channel.go`),自用多 key 轮询可以抄这个模型。

### 2. 数据模型(GORM)

- **Channel**(one-api `model/channel.go`):`Id/Type/Key(text)/Status(1启用)/Weight/BaseURL/Models(逗号串)/Group(逗号串)/UsedQuota(int64)/ModelMapping(JSON)/Priority/Config(JSON)/SystemPrompt/TestTime/ResponseTime`。new-api 追加 `StatusCodeMapping / Tag / Setting / ParamOverride / HeaderOverride / OtherSettings / ChannelInfo(multi-key)`,以及 `TestModel`(测活用哪个模型)。
- **Token**(one-api `model/token.go`):`Key(char(48) uniqueIndex)/Status/ExpiredTime(int64,-1=永不过期)/RemainQuota/UnlimitedQuota/UsedQuota/Models(允许模型)/Subnet`;new-api 换成 `ModelLimitsEnabled/ModelLimits/AllowIps/Group/AutoGroups` + gorm 软删除。注意 token 与 user **都有**独立配额(RemainQuota 是 token 级限额,User.Quota 是钱包),两层扣减。
- **User 配额字段**:`Quota/UsedQuota/RequestCount/Group`(varchar(32))。
- **Log**(new-api `model/log.go` 更完整):`UserId/Type(枚举:topup/consume/manage/system/error/refund/login)/ModelName/TokenName(+TokenId)/Quota/PromptTokens/CompletionTokens/UseTime/IsStream/ChannelId/Group/Ip/RequestId/UpstreamRequestId/Other`。**`Other` 字段存 JSON 结算快照**(当时的 model_ratio/group_ratio/cache_ratio 等),配合 Content 里的「倍率:x × y × z」文本,回溯账目争议极其有用——强烈建议照抄。one-api 还支持 `LOG_SQL_DSN` 把日志表拆到独立库(`model/main.go InitLogDB`)。
- **倍率不放表里**:model_ratio/completion_ratio/group_ratio/model_price 全部以 JSON 字符串存 `options` 表,启动加载进内存 map(RWMutex 保护,`relay/billing/ratio/model.go、group.go`)。改价 = 改 option + 热更新内存,无 join,适合倍率这种「全局少改、读多」的数据。

### 3. 计费机制

- **额度单位**:两项目都是 `QuotaPerUnit = 500000` quota = $1(即 $0.002/1K tokens 的整型化),全程 int64 整数计费(`common/config/config.go`、new-api `common/constants.go`)。
- **三层倍率公式**(one-api `relay/controller/helper.go postConsumeQuota`):`quota = ceil((promptTokens + completionTokens × completionRatio) × modelRatio × groupRatio)`,最低记 1;倍率组合写入日志。
- **倍率查找回退链**(`relay/billing/ratio/model.go GetModelRatio`):`自定义[name(channelType)] → 内置默认[name(channelType)] → 自定义[name] → 内置[name] → 兜底 30`;completion ratio 对 gpt-4/claude/gemini/deepseek 等有前缀硬编码规则。new-api 把查找收敛为 `GetModelRatio(name) (ratio, ok, matchName)`,未配置时拒绝服务(除非开「自用模式」或用户同意未知倍率),并把按次价 `GetModelPrice` 独立成 map——**price 优先于 ratio,per-call 与 per-token 双轨**(`setting/ratio_setting/model_ratio.go`)。
- **预扣-结算-退款流程**(one-api `relay/controller/helper.go preConsumeQuota/postConsumeQuota` + `relay/billing/billing.go`):
  1. 预扣量 = `(PreConsumedQuota(500) + promptTokens + maxTokens) × modelRatio × groupRatio`;
  2. 余额远超预扣(userQuota > 100×预扣)则信任用户跳过预扣;
  3. 请求失败/上游错误 → `ReturnPreConsumedQuota` 异步退回;
  4. 成功 → goroutine 里 `postConsumeQuota`:实际 quota − 预扣 = delta,`PostConsumeTokenQuota(delta)` 正补负退,再记 Log、累加 User.UsedQuota/Channel.UsedQuota。
  new-api 同流程但预扣计算移入 `relay/helper/price.go ModelPriceHelper`:按次模型 `预扣 = modelPrice × QuotaPerUnit × groupRatio × otherRatios`;per-token 模型 `(max(promptTokens, 500) + maxTokens) × ratio`;倍率为 0 的免费模型直接不预扣。
- **按次计费(生图)**:one-api 用「倍率×尺寸系数×1000×n」模拟按次(`relay/controller/image.go`:imageCostRatio 来自 `ratio/image.go ImageSizeRatios[模型][尺寸]`,dall-e-3 hd 品质 ×2/×1.5,`quota = ratio × imageCostRatio × 1000 × n`);new-api 改成正经双轨:图像模型配 ModelPrice,请求后把 usage 强制为 1 token 走 `PostTextConsumeQuota`,金额 = `ModelPrice × QuotaPerUnit × groupRatio × ImagePriceRatio(尺寸/品质系数)`(`relay/image_handler.go` + `types/price_data.go`)。**结论:按次业务用「价」不用「倍率」,单价 USD 直配,倍率只做分组折扣。**
- **细粒度 token 计费**(new-api `service/text_quota.go calculateTextQuotaSummary`):cached tokens×cacheRatio、cache-creation 5m/1h 双档、image/audio token 各自 ratio、tool-call 附加费、otherRatios 通用乘子,全部用 `shopspring/decimal` 计算,最后 `QuotaFromDecimalChecked` 转整型并 clamp。给自家模型对账用了完整的计费快照,是我们做 cache 计费(如 Claude/Gemini)时的现成蓝本。
- **并发不超扣(new-api 的现代解法,`model/quota_reserve.go`)**:
  - 无 Redis:`UPDATE users SET quota = quota-? WHERE id=? AND quota >= ?`,以 `RowsAffected==1` 判定成功——条件更新即原子检查+扣减;
  - 有 Redis:Lua 脚本原子 `HGET Quota 检查 + HINCRBY 扣减`(带 user id/schema 版本防串号),缓存 miss 先水合再重试;Redis 故障降级 DB 条件更新;DB 落库失败时反向补偿缓存。token 同构(RemainQuota/UsedQuota 同步动)。
  - `PreConsumeTokenQuota`(new-api `service/quota.go`)注释明说:「原子预扣:检查与扣减在同一操作中完成,并发请求不可能同时通过检查后超扣」。
  - one-api 旧方案的 DB 侧也用了 `gorm.Expr("quota + ?")` 单语句自增(行级原子),但**检查与扣减分离**(见应避开#1)。

### 4. OpenAI 兼容层

- **请求透传优化**(one-api `relay/controller/text.go getRequestBody`):OpenAI 兼容上游 + 无模型映射 + 无强制 system prompt 时,直接把 `c.Request.Body` 原样转发,不做 unmarshal/marshal;否则解析成 `GeneralOpenAIRequest` 后由 adaptor `ConvertRequest` 重组。流式请求强制注入 `stream_options.include_usage=true` 以便拿到计费 usage。
- **SSE 流式:旁路解析 + 默认透传**(one-api `relay/adaptor/openai/main.go StreamHandler`):`bufio.Scanner` 按行扫,校验 `data: ` 前缀,JSON 解析只为累计 usage 和 responseText(估算兜底),**转发的是原始行字符串**;解析失败也原样转发;上游没发 `[DONE]` 则网关补发。new-api 同思路(`relay/channel/openai/relay-openai.go OaiStreamHandler` + `helper.StreamScannerHandler` 回调),额外:保留 last/second-last 事件——部分兼容网关把 usage 放在倒数第二个 chunk;可选 ForceFormat(解析→重序列化)与 reasoning→content 转换。**结论:不要整体重组 SSE;逐行透传 + 旁路取 usage,仅在需要改写时才反序列化重发。**
- **上游 usage 缺失兜底**:one-api 流式没拿到 usage 时用本地 tokenizer 按响应文本估 token(`ResponseText2Usage`);非流式响应 usage 字段为 0 时同样按消息文本补估。
- **错误归一化**(one-api `relay/controller/error.go`):定义 `GeneralErrorResponse{error, message, msg, err, error_msg, header.message, response.error.message}`,`ToMessage()` 逐级兜底提取错误文案;若是标准 OpenAI error 对象则整体采用,否则包进 `{message, type:"upstream_error", code, param:status}` 的 OpenAI error 信封返回。new-api(`service/error.go RelayErrorHandler`)同构并增加 `GeneralErrorResponse.TryToOpenAIError()` 兼容 Anthropic/Gemini 的 error 形状,以及 per-channel `StatusCodeMapping`(如把上游 503 映射为 529)+ `ResetStatusCode`。**这个「宽进窄出」的错误信封是廉价且够用的归一化方案。**
- **非 OpenAI 格式转换**:请求侧 `ConvertRequest` 整体转;响应侧 adaptor `DoResponse` 内逐 chunk 转成 OpenAI 事件再写客户端(gemini/claude 等 adaptor 各自实现),即「转换发生在 adaptor 内,主链路只见 OpenAI 形状」。

---

## 应避开

1. **one-api 的预扣存在 TOCTOU 竞态(最大坑,已被定性为安全漏洞)**:`preConsumeQuota` 先 `CacheGetUserQuota` 检查、再 `CacheDecreaseUserQuota`(Redis DECR)扣减,两步不原子;`model/token.go PreConsumeTokenQuota` 也是读 token→判断→扣的读改写;并发下可超卖(issue [#2440 "Quota consumption TOCTOU race"](https://github.com/songquanpeng/one-api/issues/2440)、[#399 高并发统计漂移](https://github.com/songquanpeng/one-api/issues/399)、[#2398 兑换码并发重复兑换](https://github.com/songquanpeng/one-api/issues/2398))。→ 我们必须一步到位:DB 条件更新 `WHERE quota >= ?` 或 Redis Lua(即 new-api `model/quota_reserve.go` 的做法),自用规模单 MySQL 条件更新就够。
2. **长 TTL 缓存导致的额度不一致**:one-api 用户/令牌额度进 Redis,TTL = `SYNC_FREQUENCY`(默认 600s),token 用尽后仍可继续用、充值后仍报不足([#204](https://github.com/songquanpeng/one-api/issues/204)、[#1236](https://github.com/songquanpeng/one-api/issues/1236));`BATCH_UPDATE_ENABLED` 的内存聚合(每 5s flush,`model/utils.go`)在宕机时丢增量。new-api 源码注释也承认「批量模式下入库,过期 DB 余额会放大并发超扣」,故其 Lua 预扣**以缓存余额为准**。→ 自用网关建议:扣减走 DB 条件更新为主,缓存只做读加速且短 TTL;不要默认开批量聚合。
3. **float64 全程计费的精度债**:one-api 从倍率乘到 quota 全是 float64;new-api 已整体迁到 decimal + 显式 clamp(`service/text_quota.go`)。→ 新网关从第一天就用整数 quota(×500000/$)或 decimal,别复制 one-api 的浮点路径。
4. **「信任用户跳过预扣」与按次扣费时机的坑**:one-api `userQuota > 100×预扣` 就不预扣(高倍率模型上等于无防护);生图把 `PostConsumeTokenQuota` 放在 defer 里,只要上游返回 200/201 就扣费,响应体解析失败也不退;结算在 goroutine 里,错误只打日志,账目异常无补偿队列。
5. **未知模型倍率静默兜底**:one-api 查不到倍率默认返回 30 并只打一条 SysError(可能严重多扣或少扣);new-api 改为拒绝服务或要求显式接受未知倍率。→ 自用也应「未配置价格即拒绝」,把兜底值做成显式配置。
6. **渠道测活/自动禁用的误禁与恢复难**:偶发一次 429/503 即禁用、需人工反复恢复([new-api #4100](https://github.com/QuantumNous/new-api/issues/4100)、[#4585](https://github.com/QuantumNous/new-api/issues/4585));恢复依赖定时全量测试——费钱([#5205](https://github.com/QuantumNous/new-api/issues/5205))、多 key 渠道测不活([#7040](https://github.com/QuantumNous/new-api/issues/7040)、[#3537](https://github.com/QuantumNous/new-api/issues/3537))、强制流式渠道测试请求不匹配导致连锁误禁([#3298](https://github.com/QuantumNous/new-api/issues/3298));社区长期呼吁冷却半开(half-open)机制([#5420](https://github.com/QuantumNous/new-api/issues/5420))。→ 自用网关从一开始就按熔断器做:连续 N 次失败才禁用 + 半开探测恢复,别只做定时全量测活。
7. **负载均衡语义混乱**:one-api 渠道表里有 `Weight` 字段但选渠道根本没用它(只有 priority 层内均匀随机,`model/ability.go、model/cache.go`);高并发负载分配长期被诟病([#281](https://github.com/songquanpeng/one-api/issues/281));new-api 也有重试不按优先级的 bug 报告([#7007](https://github.com/QuantumNous/new-api/issues/7007))与渠道亲和缓存「粘住」旧渠道的排障案例。→ priority(故障转移)+ weight(层内分流)语义要在代码里严格区分并测试。
8. **路由表与渠道表双写漂移**:one-api 的 `UpdateAbilities` 是「先删后插、非原子」,源码自嘲 "quick and dirty";channel cache 默认 10s~10min 全量同步,期间新渠道不可见、禁用渠道仍可能被选中,cache 与 DB 不一致时 distributor 直接报「数据库一致性已被破坏」。→ 用外键级联 + 同事务维护路由表,或干脆渠道少时每次请求内联选路。
9. **逗号串/JSON 串当关系用**:`Models`、`Group` 是逗号分隔字符串,`ModelMapping/Config/Setting/ParamOverride` 是 TEXT 里的 JSON——schema 无约束、脏数据只能靠运行时报错;`Log.Quota` 用 int 而 `User.Quota` 用 int64,类型不统一。→ 我们设计时:能力表规范化,扩展配置用单一 `config` JSON 列并定义 Go struct 校验,金额字段统一 int64。
10. **usage 日志表无限增长**:logs 与业务同库(可选独立 LOG_DSN)但无分区,只靠 `DeleteOldLog` 手动清;高频自用也要预留按月分区/归档策略。另外 key 明文存储(`char(48)`),多租户场景应存哈希。

---

### 对我们(InfiniteChance 网关)的直接建议

1. 计费主结构照 new-api 抄:`PriceData{UsePrice, ModelPrice, ModelRatio, CompletionRatio, GroupRatio, otherRatios, QuotaToPreConsume}` 一次算好贯穿请求生命周期;预扣一律原子 reserve(DB 条件更新或 Redis Lua)+ 失败退款 + 结算写 `Log.Other` 快照;按次业务(生图/生视频)只配 USD 单价 + 尺寸/时长等 ratio 乘子,异步任务用 Estimate→AdjustOnSubmit→AdjustOnComplete 三钩子。
2. 渠道层抽象保持 one-api 的窄接口(7 个方法,usage 从 DoResponse 出),OpenAI 兼容上游共用一个 adaptor + per-channel 小分支,不要为每个厂商开新协议类型;SSE 逐行透传 + 旁路 usage;错误统一 GeneralErrorResponse 宽解析 → OpenAI error 信封。

### 主要来源

- one-api:`relay/adaptor/interface.go`、`relay/adaptor.go`、`relay/meta/relay_meta.go`、`middleware/distributor.go`、`relay/controller/{text,image,error,helper}.go`、`relay/adaptor/openai/{adaptor,main}.go`、`relay/billing/billing.go`、`relay/billing/ratio/{model,group,image}.go`、`model/{channel,token,user,log,ability,cache,utils,main}.go`、`common/config/config.go`(github.com/songquanpeng/one-api,main 分支)
- new-api:`relay/channel/adapter.go`、`model/quota_reserve.go`、`service/{quota,text_quota,error}.go`、`relay/helper/price.go`、`types/price_data.go`、`setting/ratio_setting/model_ratio.go`、`relay/{compatible_handler,image_handler}.go`、`relay/channel/openai/relay-openai.go`、`model/{channel,log,token}.go`(github.com/QuantumNous/new-api,main 分支)
- Issues:one-api #2440/#2398/#204/#1236/#399/#499/#281;new-api #4211/#4100/#4585/#5420/#5205/#7040/#3537/#3298/#7007;定价/权重 FAQ:doc.newapi.pro/support/faq/
