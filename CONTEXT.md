# CONTEXT.md — 领域术语表

实现过程中随用随补;术语定义冲突时以 [spec.md](.scratch/infinite-chance/spec.md) 及较新的决策票为准。

## 领域术语

- **渠道(channel)**:一条到上游 AI 厂商的接入配置——厂商类型、BaseURL、密钥、模型名映射、优先级与权重、启用状态。网关代表调用方通过渠道发起上游请求;渠道有独立的熔断与恢复状态。03 号票定案:渠道类型目前仅 `openai`(OpenAI 兼容上游,含 OpenAI/DeepSeek/Kimi/GLM/Qwen 兼容层/方舟等);BaseURL 含版本路径(如 `https://api.openai.com/v1`);厂商密钥明文存库(需可重放签名上游请求),管理 API 只写不读、响应仅带 `has_key` 与尾 4 位提示;一键连通测试 = 用存储密钥 GET `{base_url}/models`,2xx 即成功并回显模型数。
- **API key**:网关发放给调用方的凭据(`sk-` + 40 位 base64url,共 43 字符)。仅以 SHA-256 十六进制哈希存储(密钥是高熵随机值而非口令,中转路径每请求一次查询,故不用 bcrypt);对外只露前缀 `sk-` + 8 位随机字符,完整值仅在创建响应出现一次;支持过期时间与吊销(吊销幂等,保留首次吊销时间)。
- **额度(quota)**:挂在一个 API key 上的可用余额,库内单位为**微美元(micro-USD,1 美元 = 1e6)**整数,管理 API 边界转换为美元。扣减时序为「按估算预扣 → 完成后多退少补 → 失败退款」;并发安全靠数据库条件更新(`WHERE quota >= ?`)保证不超扣。每次变动落 `api_key_quota_log` 流水(变动量、变动后余额快照、原因),初始额度与充值已入账,04 号票的计费追加预扣(`estimate`)/结算差额(`settle`)/失败退款(`refund`)条目。
- **倍率(ratio)**:聊天类计费的价格系数——上游 token 成本乘以倍率得到对 key 的扣费。生图/生视频不用倍率,按「次」的 USD 单价乘尺寸/时长系数(见双轨计价)。04 号票定案:倍率以 `ratio_micros` 整数存(1e6 = ×1.0),扣费 = ⌈(输入 token × 输入单价 + 输出 token × 输出单价) × 倍率⌉,单位微美元、向上取整不低估。
- **计价(model price)**:公开模型的价格行,表 `model_prices`(public_model 主键、unit、config JSON,Go struct 校验)。双轨命名:**token 轨**(`unit=token`,config 含输入/输出微美元每百万 token 与 `ratio_micros`,04 号票落地)与**次轨**(`unit=call`,按次 USD 单价乘尺寸/时长系数,07 号票落地;当前 admin API 明确拒绝)。未配置价格的模型一律拒绝服务(`model_not_priced`),不做静默兜底倍率。
- **用量日志(usage log)**:请求级流水:渠道、模型、计费单位下的量(token/张/秒/条)、耗时、状态、扣费金额、倍率快照;失败请求同样留痕并含上游错误摘要。对账与审计的唯一依据。04 号票定案:表 `usage_logs`,`price_snapshot` JSON 列存请求时的价格快照(改价不影响历史对账),渠道名快照成文本存列(渠道删除后留痕仍在);状态 `success` / `upstream_error`(连接失败、非 2xx、取消都归并此态,摘要列区分)。
- **任务(task)**:异步生成作业(生视频为主;画布侧的编排任务见「画布任务」)。对外状态机收敛为五态:`queued / running / succeeded / failed / canceled`;上游的节流态归并 queued,未知态归并 failed。
- **素材(asset)**:生成产物(图片/视频)在素材库中的持久实体,独立于画布存在;画布节点通过引用持有素材,支持跨画布复用与下载。
- **节点(node)**:画布上的最小单元(提示词/图片/视频),位置与连线存在画布整图 JSON 里,不入库成行;AI 动作以节点为输入、生成结果落为新节点。
- **画布任务(canvas task)**:canvas/server 编排的生成作业——绑定节点与素材,带并发、重试与断线恢复;与网关的对接一律走服务级 key + 网关异步契约。

## 工程术语

- **健康检查(/healthz)**:两个 Go 服务共有的探针端点,并发探测 MySQL 与 Redis(各自 2s 超时),全部可达返回 200 `ok`,任一不可达返回 503 `degraded` 并附逐项状态与错误摘要。
- **服务级 key**:canvas/server 调用网关所用的 API key,用量经请求元数据归属到画布来源;画布前端不持有任何厂商或网关密钥。
- **管理员账号(admin account)**:全库唯一的登录账号,由首次访问的初始化引导(`POST /auth/init`)创建;密码仅以 bcrypt 哈希存于 `admin_accounts` 表,初始化完成后引导不再出现。
- **JWT 会话**:管理员登录后由网关签发的 HS256 令牌(7 天有效),承载用户名与签发/过期时间;签名密钥经 `JWT_SECRET` 环境变量在网关与画布间共享,画布据此校验网关签发的令牌。唯一管理员由 `admin_accounts` 固定主键(id=1)保证,不依赖隔离级别。生产环境可设 `JWT_SECRET_REQUIRED=true`,密钥缺失时拒绝启动。无刷新机制,过期即重登;管理端 API 错误统一为 `{"error":{"code","message"}}`。
- **管理端路由面**:网关把管理 API 挂在 `/admin` 下(`admin/channels`、`admin/keys`),一律先过 JWT 会话中间件;中转面 `/v1`(04 号票起)挂 `apikey.RequireKey`,两者互不混用。
- **中转面错误(API key 路径)**:被 `/v1` 拒绝的请求(缺 key、未知、已吊销、已过期)统一回答 OpenAI error object `{"error":{"message","type","param":null,"code"}}`,401 + `invalid_request_error`;code 区分原因:`missing_api_key` / `invalid_api_key` / `key_revoked` / `key_expired`,客户端按 code 分支。04 号票起中转面全部错误同形:额度不足 429 + `insufficient_quota`(预扣被拒,余额不动)、模型无渠道 404 + `model_not_found`、未配价/参数缺失 400、上游失败透传上游状态码 + code/type 均 `upstream_error`(连接失败、客户端中途断开、2xx 但响应体不可解析都归并 502)。
- **流式透传(SSE,05 号票定案)**:`stream:true` 的聊天请求逐帧透传,不重组内容;每帧 data 负载语义保真 —— 仅 `model` 字段回写公开名(JSON 重编码不保键序与空白),不可解析的负载原样转发。网关向上游强制注入 `stream_options.include_usage=true`(结算需要真实用量),由此产生的**用量专用块**(choices 为空、带 usage)仅用于记账,**客户端自己没点 include_usage 时被吞掉**,点了则照发。断开语义:客户端中途断开 → 上游连接随请求 context 取消而清理;已收到用量则照常按实结算(成功),未收到则全额退款并落 `upstream_error` 留痕(摘要列写明 client disconnected);流完整走完但上游始终未报用量 → 少记不虚记,退款、成功流零扣费留痕。

## 待补

- 渠道熔断/半开的精确术语(06 号票定案后补)
- 按次轨(生图/生视频)的尺寸/时长系数字段命名(07 号票定案后补;双轨单位命名已定:token / call)
