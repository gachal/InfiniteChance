# CONTEXT.md — 领域术语表

实现过程中随用随补;术语定义冲突时以 [spec.md](.scratch/infinite-chance/spec.md) 及较新的决策票为准。

## 领域术语

- **渠道(channel)**:一条到上游 AI 厂商的接入配置——厂商类型、BaseURL、密钥、模型名映射、能力、优先级与权重、启用状态。网关代表调用方通过渠道发起上游请求;渠道有独立的熔断与恢复状态(见「调度与熔断」)。03 号票定案:渠道类型目前仅 `openai`(OpenAI 兼容上游,含 OpenAI/DeepSeek/Kimi/GLM/Qwen 兼容层/方舟等);BaseURL 含版本路径(如 `https://api.openai.com/v1`);厂商密钥明文存库(需可重放签名上游请求),管理 API 只写不读、响应仅带 `has_key` 与尾 4 位提示;一键连通测试 = 用存储密钥 GET `{base_url}/models`,2xx 即成功并回显模型数。07 号票定案:渠道带 `capabilities` 能力集(`chat`/`images`,JSON 列),调度只把聊天请求落到 `chat` 能力渠道、生图请求落到 `images` 能力渠道,互不串道;缺省(历史行/未声明)视为仅 `chat`,**生图能力必须显式开启**;响应里的 capabilities 恒回显生效集(nil 行显示 `["chat"]`)。08 号票追加 `videos` 能力:视频异步任务只落 `videos` 能力渠道,同一显式开启原则。
- **调度与熔断(scheduling & circuit breaker)**:06 号票定案。同一公开模型挂多渠道时,候选按优先级分层、自高向低依次尝试(故障转移),同层内按权重加权随机分流,权重 0 按 1 计(全零退化均匀)。上游**临时失败**——连接失败、上游 429/5xx、2xx 但响应体不可解析——在请求尚未被计费前自动换道重试,预扣原封带往下一渠道;客户端侧的上游 4xx(参数错误、厂商密钥失效等)与客户端主动断开不换道也不记熔断账,原样透传。计费边界:预扣一次,只有决定成败的那次尝试结算或退款;流式请求只在流打开前允许换道(流一开,响应头已发)。每渠道独立的熔断器是内存态三态电路(不落库、网关重启即清零):closed 下连续失败达阈值(默认 3 次,只计临时失败)转 open、一概不选;冷却期(默认 30s)过后转 half-open,只放一个探测请求(单飞),成功闭环、失败重开并重置冷却。全部候选都在熔断中时按 503 `model_unavailable` 拒绝(预扣原退、不留用量行);若真实尝试过部分候选、其余被熔断跳过,则按普通上游失败收尾(留痕、向客户端报真实失败原因)。`/v1/models` 目录不受熔断影响。换道的历史记在用量日志的 `upstream_error` 摘要列(成功行也可能非空 = 带病完成)。流打开后的中断不换道;流式请求的熔断成败在流收尾时一次记账(打开流本身不记成功):完整交付或已报用量记成功,客户端主动断开不记渠道的账,上游流中途失败且尚无可计用量记一次临时失败。
- **API key**:网关发放给调用方的凭据(`sk-` + 40 位 base64url,共 43 字符)。仅以 SHA-256 十六进制哈希存储(密钥是高熵随机值而非口令,中转路径每请求一次查询,故不用 bcrypt);对外只露前缀 `sk-` + 8 位随机字符,完整值仅在创建响应出现一次;支持过期时间与吊销(吊销幂等,保留首次吊销时间)。
- **额度(quota)**:挂在一个 API key 上的可用余额,库内单位为**微美元(micro-USD,1 美元 = 1e6)**整数,管理 API 边界转换为美元。扣减时序为「按估算预扣 → 完成后多退少补 → 失败退款」;并发安全靠数据库条件更新(`WHERE quota >= ?`)保证不超扣。每次变动落 `api_key_quota_log` 流水(变动量、变动后余额快照、原因),初始额度与充值已入账,04 号票的计费追加预扣(`estimate`)/结算差额(`settle`)/失败退款(`refund`)条目。
- **倍率(ratio)**:聊天类计费的价格系数——上游 token 成本乘以倍率得到对 key 的扣费。生图/生视频不用倍率,按「次」的 USD 单价乘尺寸/时长系数(见双轨计价)。04 号票定案:倍率以 `ratio_micros` 整数存(1e6 = ×1.0),扣费 = ⌈(输入 token × 输入单价 + 输出 token × 输出单价) × 倍率⌉,单位微美元、向上取整不低估。
- **计价(model price)**:公开模型的价格行,表 `model_prices`(public_model 主键、unit、config JSON,Go struct 校验)。双轨命名:**token 轨**(`unit=token`,config 含输入/输出微美元每百万 token 与 `ratio_micros`,04 号票落地)与**次轨**(`unit=call`/`unit=second`,按次 USD 单价乘尺寸系数;`unit=call` 07 号票落地:config 含 `usd_per_call_micros` 与 `size_factor_micros` 尺寸→系数表,1e6 = ×1.0,未配置的尺寸恒 ×1.0,扣费 = ⌈单价 × 系数⌉ × 实交张数;`unit=second` 08 号票落地:同一算法,单价按秒、系数按分辨率,扣费 = ⌈每秒单价 × 分辨率系数⌉ × 秒数)。admin API 人类单位:`input/output_usd_per_mtokens`+`ratio` 与 `usd_per_call`+`size_factors`,unit 决定哪组字段生效。未配置价格的模型一律拒绝服务(`model_not_priced`),不做静默兜底倍率;请求打到计价轨道以外的模型(聊天打非 token 轨、生图打非 call 轨、视频打非 second 轨)同样 `model_not_priced`。
- **生图转发(images relay)**:07 号票定案。`POST /v1/images/generations`(JSON 体)与 `POST /v1/images/edits`(multipart 表单)同步转发到 OpenAI 兼容上游的对应端点,只落 `images` 能力渠道;响应体原样透传(URL 或 b64_json),`model` 字段仅在厂商回带时才回写公开名。计费走与聊天轨一致的「预扣(单价 × 尺寸系数 × n,n 缺省 1,超出 1..100 直接 400 拒绝)→ 按实交 `data` 张数结算(多退少补;差额为零不落 settle 流水)→ 失败全额退款」;换道/熔断/留痕语义与缓冲聊天共用,2xx 但一张图都没交付按可换道失败处理。edits 的 multipart 按渠道重建(仅 `model` 文本字段换成上游名),文件分节字节与 header 原样保留。用量日志 `unit=call`,token 列恒 0;价格快照额外记 `request{size,n}`,按次扣费缺请求事实无法重算。
- **用量日志(usage log)**:请求级流水:渠道、模型、计费单位下的量(token/张/秒/条)、耗时、状态、扣费金额、倍率快照;失败请求同样留痕并含上游错误摘要。对账与审计的唯一依据。04 号票定案:表 `usage_logs`,`price_snapshot` JSON 列存请求时的价格快照(改价不影响历史对账),渠道名快照成文本存列(渠道删除后留痕仍在);状态 `success` / `upstream_error`(连接失败、非 2xx、取消都归并此态,摘要列区分)。
- **任务(task)**:异步生成作业(生视频为主;画布侧的编排任务见「画布任务」)。对外状态机收敛为五态:`queued / running / succeeded / failed / canceled`;上游的节流态归并 queued,未知态归并 failed。08 号票定案(网关侧「视频异步任务契约」):`POST /v1/videos/generations` 提交返回 `task_id`(网关生成,`vt_` 前缀)、`GET /v1/videos/tasks/{id}` 轮询、`POST /v1/videos/tasks/{id}/cancel` 取消,错误统一 OpenAI error object,任务归属发放 key(跨 key 查询/取消一律 404 `task_not_found`,不泄露存在性)。渠道在厂商受理提交那一刻钉死(提交前的失败照常换道,轮询/取消只打钉住的渠道);轮询是代理语义——每次轮询实时查上游并做归并,任务行记录最近一次上游原始状态,终态后轮询/取消只答账本事实、不再打扰上游。轮询本身的暂时失败(连接失败、非 2xx、响应体不可解析)不改状态不动账。计费「仅成功计费」:提交按「每秒单价 × 分辨率系数 × 秒数」预扣(seconds 缺省 5,1..100 之外 400 拒绝),成功定格为实扣(差额为零不落 settle 流水),失败/取消全额退款;上游报成功却无产物 URL 按失败处理。终态迁移落一条用量日志(`unit=second`,取消归并 `upstream_error`、摘要列区分 canceled by client/upstream),提交失败与同步轨同形(退款 + `upstream_error` 留痕)。上游契约(与对外同形的 OpenAI 风格任务面,`{base_url}/videos/generations`、`/videos/tasks/{id}`、`/videos/tasks/{id}/cancel`)由 adaptor 定义,真实厂商接入时在 adaptor 内转换。
- **素材(asset)**:生成产物(图片/视频)在素材库中的持久实体,独立于画布存在;画布节点通过引用持有素材,支持跨画布复用与下载。
- **节点(node)**:画布上的最小单元(提示词/图片/视频),位置与连线存在画布整图 JSON 里,不入库成行;AI 动作以节点为输入、生成结果落为新节点。
- **画布任务(canvas task)**:canvas/server 编排的生成作业——绑定节点与素材,带并发、重试与断线恢复;与网关的对接一律走服务级 key + 网关异步契约。

## 工程术语

- **健康检查(/healthz)**:两个 Go 服务共有的探针端点,并发探测 MySQL 与 Redis(各自 2s 超时),全部可达返回 200 `ok`,任一不可达返回 503 `degraded` 并附逐项状态与错误摘要。
- **服务级 key**:canvas/server 调用网关所用的 API key,用量经请求元数据归属到画布来源;画布前端不持有任何厂商或网关密钥。
- **管理员账号(admin account)**:全库唯一的登录账号,由首次访问的初始化引导(`POST /auth/init`)创建;密码仅以 bcrypt 哈希存于 `admin_accounts` 表,初始化完成后引导不再出现。
- **JWT 会话**:管理员登录后由网关签发的 HS256 令牌(7 天有效),承载用户名与签发/过期时间;签名密钥经 `JWT_SECRET` 环境变量在网关与画布间共享,画布据此校验网关签发的令牌。唯一管理员由 `admin_accounts` 固定主键(id=1)保证,不依赖隔离级别。生产环境可设 `JWT_SECRET_REQUIRED=true`,密钥缺失时拒绝启动。无刷新机制,过期即重登;管理端 API 错误统一为 `{"error":{"code","message"}}`。
- **管理端路由面**:网关把管理 API 挂在 `/admin` 下(`admin/channels`、`admin/keys`),一律先过 JWT 会话中间件;中转面 `/v1`(04 号票起)挂 `apikey.RequireKey`,两者互不混用。
- **中转面错误(API key 路径)**:被 `/v1` 拒绝的请求(缺 key、未知、已吊销、已过期)统一回答 OpenAI error object `{"error":{"message","type","param":null,"code"}}`,401 + `invalid_request_error`;code 区分原因:`missing_api_key` / `invalid_api_key` / `key_revoked` / `key_expired`,客户端按 code 分支。04 号票起中转面全部错误同形:额度不足 429 + `insufficient_quota`(预扣被拒,余额不动)、模型无渠道 404 + `model_not_found`、候选渠道全部熔断中 503 + `model_unavailable`(预扣原退)、未配价/参数缺失 400、上游失败透传上游状态码 + code/type 均 `upstream_error`(连接失败、客户端中途断开、2xx 但响应体不可解析都归并 502)。
- **流式透传(SSE,05 号票定案)**:`stream:true` 的聊天请求逐帧透传,不重组内容;每帧 data 负载语义保真 —— 仅 `model` 字段回写公开名(JSON 重编码不保键序与空白),不可解析的负载原样转发。网关向上游强制注入 `stream_options.include_usage=true`(结算需要真实用量),由此产生的**用量专用块**(choices 为空、带 usage)仅用于记账,**客户端自己没点 include_usage 时被吞掉**,点了则照发。断开语义:客户端中途断开 → 上游连接随请求 context 取消而清理;已收到用量则照常按实结算(成功),未收到则全额退款并落 `upstream_error` 留痕(摘要列写明 client disconnected);流完整走完但上游始终未报用量 → 少记不虚记,退款、成功流零扣费留痕。

## 待补

- 素材库(14 号票)与视频产物的对象存储转存:上游产物 URL 临时(约 24h),网关任务行只存厂商原 URL;「成功后立即转存自有存储」为后续票的决策点。
