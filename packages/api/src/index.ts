/**
 * Shared request layer for both frontends (admin-web, canvas/web).
 * Framework-agnostic: apps inject their own fetch when testing.
 */

export type DepStatus = 'up' | 'down'

export interface DepCheck {
  status: DepStatus
  error?: string
}

export interface HealthReport {
  service: string
  status: 'ok' | 'degraded'
  checks: Record<string, DepCheck>
}

/** A session as answered by /auth/init and /auth/login. */
export interface SessionInfo {
  token: string
  expires_at: string
  username: string
}

/** GET /auth/status — has the first admin been created? */
export interface AuthStatus {
  initialized: boolean
}

/** GET /auth/me — the identity behind the presented token. */
export interface AdminIdentity {
  username: string
  expires_at: string
}

/** Channel types this build can relay and probe (25 号票起三种):
 * openai = OpenAI 兼容 Bearer;tencent-vod = 腾讯云 VOD AIGC(TC3 凭据在
 * config);volcengine-ark = 火山方舟 Seedance(单 Bearer key、MVP 仅
 * videos 能力)。 */
export const CHANNEL_TYPES = ['openai', 'tencent-vod', 'volcengine-ark'] as const
export type ChannelType = (typeof CHANNEL_TYPES)[number]

/** A vendor channel as answered by the admin gateway APIs. The vendor
 * secret never crosses the wire — only has_key and its last-4 hint. */

export interface Channel {
  id: number
  name: string
  type: string
  base_url: string
  has_key: boolean
  key_hint?: string
  /** Type-specific settings. Secret-key entries (name contains secret/key)
   * come back blanked; config_hints carries their tail hints. */
  config?: Record<string, string>
  config_hints?: Record<string, string>
  capabilities: string[]
  model_map: Record<string, string>
  priority: number
  weight: number
  enabled: boolean
  created_at: string
  updated_at: string
}

/** Channel config sent to create/update. api_key empty on update keeps the
 * stored secret; on create it is required. */
export interface ChannelInput {
  name: string
  type: string
  base_url: string
  api_key?: string
  /** Secret-key entries empty on update keep the stored value. */
  config?: Record<string, string>
  /** Empty array defaults to chat-only; images/videos must be explicit. */
  capabilities?: string[]
  model_map: Record<string, string>
  priority: number
  weight: number
  enabled: boolean
}

/** One-click connectivity probe verdict. */
export interface ChannelTestResult {
  ok: boolean
  latency_ms: number
  detail?: string
  error?: string
}

export type ApiKeyStatus = 'active' | 'revoked' | 'expired'

/** An issued gateway key. The full sk- value exists only on the create
 * response (CreatedApiKey); every other view shows the prefix. */
export interface ApiKeyRecord {
  id: number
  name: string
  prefix: string
  quota_usd: number
  status: ApiKeyStatus
  expires_at: string | null
  revoked_at: string | null
  created_at: string
  updated_at: string
}

export interface CreatedApiKey extends ApiKeyRecord {
  /** 完整 key 值,仅创建响应返回这一次。 */
  key: string
}

export interface ApiKeyInput {
  name: string
  /** RFC3339;缺省 = 永不过期。 */
  expires_at?: string
  initial_quota_usd?: number
}

/** One quota ledger row: what changed, the balance right after, and why. */
export interface QuotaEntry {
  id: number
  delta_usd: number
  balance_usd: number
  reason: string
  created_at: string
}

// ---- 网关管理:技能(挂 /admin/prompt-templates,需 JWT 会话)----

/** 技能目标徽章(29 号票):纯展示与 `/` 浮层筛选,后端零行为、投递不
 * 校验;any = 无目标之分(缺省)。 */
export type SkillTarget = 'image' | 'video' | 'any'

/** 一条技能(原「提示词模板」,29 号票更名;表名与 API 路径不动):
 * template 内含 {topic} 占位符,Agent 会话首条指令以主题替换;画布侧动作
 * 每次即时读库,增删改立即生效(11 号票)。description 是技能卡副标题
 * (可空),target 是目标徽章。 */
export interface PromptTemplate {
  id: number
  name: string
  description: string
  template: string
  target: SkillTarget
  enabled: boolean
  created_at: string
  updated_at: string
}

export interface PromptTemplateInput {
  name: string
  description?: string
  template: string
  /** 缺省 any。 */
  target?: SkillTarget
  /** 缺省视为启用;显式 false 停用。 */
  enabled?: boolean
}

// ---- 画布(canvas/server,挂 /canvases,需 JWT 会话)----

/** 整图 JSON 文档:节点与连线。节点类型与数据形状由编辑器
 * (canvas/web + vue-flow)定义,请求层保持宽松,只保证整图可序列化往返。 */
export interface CanvasGraph {
  nodes: unknown[]
  edges: unknown[]
}

/** 列表项:不带图(整图文档可能很大,列表页只需要名字)。 */
export interface CanvasSummary {
  id: number
  name: string
  version: number
  created_at: string
  updated_at: string
}

/** 画布详情:含整图 JSON。 */
export interface CanvasDetail extends CanvasSummary {
  graph: CanvasGraph
}

/** 自动保存成功后的版本指针,下一次保存必须带上它。 */
export interface SaveGraphResult {
  version: number
  updated_at: string
}

/** 画布生成任务状态:排队 → 生成中 → 成功/失败/已取消;失败可在节点上
 * 原地重试(同一任务回队);取消只属于视频任务(12 号票,同步生图没有
 * 可撤销的提交)。 */
export type CanvasTaskStatus = 'queued' | 'running' | 'succeeded' | 'failed' | 'canceled'

/** 服务端编排的生成任务。node_id 绑定到编辑器里展示结果的节点;
 * image_url / video_url 是产物地址(成功时与素材行同值),asset_id 是
 * 素材库引用;seconds 只属于视频任务(0 = 自动,不传维持厂商缺省),
 * ratio 是视频任务的显式宽高比串(空 = 不传)。 */
export interface CanvasTask {
  id: string
  canvas_id: number
  node_id: string
  kind: string
  prompt: string
  model: string
  size: string
  ratio: string
  seconds: number
  status: CanvasTaskStatus
  attempts: number
  error: string
  asset_id: number
  image_url: string
  video_url: string
  created_at: string
  updated_at: string
}

/** 视频任务的结构化参考(24 号票):url 接受厂商 http(s) 地址或素材内容
 * 寻址路径(服务端解出真实地址并校验素材种类与角色相符);kind/role 的
 * 合法组合 —— 首帧/尾帧/参考图必须 image、参考视频必须 video、参考音频
 * 必须 audio,数量上限首帧/尾帧/参考视频/参考音频各 1、参考图 4。 */
export interface CanvasVideoRef {
  url: string
  kind: 'image' | 'video' | 'audio'
  role: 'first_frame' | 'last_frame' | 'reference_image' | 'reference_video' | 'reference_audio'
}

/** 提交生成任务的请求体。kind=image 为文生图/图生图(image_urls 带参考
 * 图列表,≤4 条:厂商 http(s) 地址或素材内容寻址路径,服务端逐条解引用,
 * 21 号票);kind=video 为视频生成(24 号票对话框范式):video_refs 带全
 * 模态参考(空/缺省 = 文生视频;12 号票的单串 image_url 仍兼容),
 * seconds 可选(缺省 = 自动,厂商缺省时长),size 为分辨率档位串
 * (480p/720p/1080p),ratio 为显式宽高比。 */
export interface CreateCanvasTaskInput {
  node_id: string
  kind: 'image' | 'video'
  prompt: string
  model: string
  size?: string
  ratio?: string
  image_url?: string
  image_urls?: string[]
  video_refs?: CanvasVideoRef[]
  seconds?: number
}

/** 画布侧技能目录项(仅启用中的技能;29 号票带描述与目标徽章)。 */
export interface PromptTemplateOption {
  id: number
  name: string
  description: string
  target: SkillTarget
}

/** Agent 节点多轮对话的一轮(29 号票):role+content 随节点 data 持久化
 * (整图 JSON),20 轮上限超出截断最旧。 */
export interface AgentChatMessage {
  role: 'user' | 'assistant'
  content: string
}

/** 生成提示词的请求体(29 号票 Agent 会话):template_id 可选 —— 缺省/
 * 0 = 未选技能,服务端用内置通用「提示词书写」指令作首条;topic 为本轮
 * 输入(主题或修改意见);history 为此前的对话轮次(不含本轮,不含首条
 * 指令 —— 那由服务端按技能即时渲染);model 是 token 轨聊天模型;
 * node_id 可选,用于用量归因。 */
export interface GeneratePromptInput {
  node_id?: string
  template_id?: number
  topic: string
  model: string
  history?: AgentChatMessage[]
}

/** 生成提示词的响应:文本由编辑器写入 Agent 节点文本区并沿连线投递。 */
export interface GeneratePromptResult {
  text: string
}

/** 视频反推提示词的请求体(13 号票):video_url 是视频节点持有的地址
 * (厂商 http(s) 地址,或素材内容寻址路径 /api/assets/{id}/content),
 * model 是 token 轨聊天模型;node_id 可选,用于用量归因。 */
export interface ReversePromptInput {
  node_id?: string
  video_url: string
  model: string
}

/** 视频反推提示词的响应:文本由编辑器落为新的 Agent 节点(29 号票)。 */
export interface ReversePromptResult {
  text: string
}

/** 画布分析(17 号票)的请求体:media_url 与反推同一套引用规则(厂商
 * http(s) 地址,或素材内容寻址路径;data URI 服务端拒绝),media_kind
 * 声明输入是视频还是图片,model 是 token 轨聊天模型;node_id 是承接
 * 结果的分析节点 id,用于用量归因。 */
export interface AnalyzeInput {
  node_id?: string
  media_url: string
  media_kind: 'video' | 'image'
  model: string
}

/** 画布分析的响应:结构化分镜 markdown,由编辑器写入分析节点。 */
export interface AnalyzeResult {
  text: string
}

// ---- 素材库(挂 /assets,canvas/server;列表/删除需 JWT 会话,14 号票)----

/** 一条素材:生成产物在素材库中的引用。content_url 恒为内容寻址路径
 * (预览与跨画布复用统一走它),url 是原始厂商地址(或历史 data: URI),
 * 仅排障时关心。canvas_name 来自来源画布,画布已删时为空。 */
export interface AssetRecord {
  id: number
  kind: 'image' | 'video' | 'audio'
  canvas_id: number
  canvas_name: string
  task_id: string
  model: string
  prompt: string
  url: string
  content_type: string
  size_bytes: number
  content_url: string
  created_at: string
}

/** 素材列表的过滤与分页:全部缺省 = 不过滤,后端默认一页 50 条。 */
export interface ListAssetsParams {
  kind?: 'image' | 'video' | 'audio'
  canvas_id?: number
  limit?: number
  offset?: number
}

// ---- 网关管理:存储设置(挂 /admin/settings/storage,需 JWT 会话,19 号票)----

/** 存储驱动:local = 本地卷(缺省),oss = 阿里云 OSS,cos = 腾讯云 COS
 * (23 号票,两者都是原生 SDK)。 */
export type StorageDriver = 'local' | 'oss' | 'cos'

/** GET 回答的 OSS 连接:密钥永不跨线,只有 has_* 与尾 4 位提示。 */
export interface OSSStorageConfig {
  endpoint: string
  bucket: string
  public_base_url: string
  has_access_key: boolean
  access_key_hint?: string
  has_secret: boolean
  secret_hint?: string
}

/** GET 回答的 COS 连接:字段名沿用腾讯控制台词汇,密钥同款只写不读。 */
export interface COSStorageConfig {
  endpoint: string
  bucket: string
  public_base_url: string
  has_secret_id: boolean
  secret_id_hint?: string
  has_secret_key: boolean
  secret_key_hint?: string
}

/** 存储设置(19/23 号票):驱动、relay_persist 与两家云连接,画布侧按请求
 * 读表即时生效。updated_at 为空 = 行从未保存过(零配置起步)。 */
export interface StorageSettings {
  driver: StorageDriver
  relay_persist: boolean
  oss: OSSStorageConfig
  cos: COSStorageConfig
  updated_at: string
}

/** PUT 的请求体:空密钥 = 保留已存密钥;不带某驱动的块 = 该连接原样;
 * relay_persist 缺省 = 沿用已存开关。 */
export interface StorageSettingsInput {
  driver: StorageDriver
  relay_persist?: boolean
  oss?: {
    endpoint: string
    bucket: string
    public_base_url: string
    access_key: string
    secret_key: string
  }
  cos?: {
    endpoint: string
    bucket: string
    public_base_url: string
    secret_id: string
    secret_key: string
  }
}

// ---- 网关管理:用量审计(挂 /admin/usage,需 JWT 会话,15 号票)----

/** 一条请求级用量行的状态:成功,或离开网关后的失败(上游错误摘要列区分
 * 具体原因,连接失败/非 2xx/取消/换道史都归并 upstream_error)。 */
export type UsageStatus = 'success' | 'upstream_error'

/** 按次/按秒轨的计费事实(尺寸与张数/秒数),来自请求时的价格快照;
 * token 轨的数量在 prompt/completion_tokens 列,此字段为 null。 */
export interface UsageRequestFacts {
  size: string
  n: number
}

/** 一条请求级用量日志:计费单位下的量、耗时、状态、扣费与画布来源标记。
 * source 是调用方自报的审计注记,画布侧形如 canvas=<id> task=<ct_…> node=…,
 * 空 = 直连流量;失败行的 upstream_error 带上游错误/换道摘要。 */
export interface UsageLogRecord {
  id: number
  key_id: number
  channel_id: number
  channel_name: string
  public_model: string
  upstream_model: string
  unit: 'token' | 'call' | 'second'
  prompt_tokens: number
  completion_tokens: number
  request: UsageRequestFacts | null
  duration_ms: number
  status: UsageStatus
  charge_usd: number
  upstream_error: string
  source: string
  created_at: string
}

/** 用量日志列表的过滤与分页:全部缺省 = 不过滤,后端默认一页 50 条(上限
 * 500)。from/to 是 RFC3339;from 含端点、to 不含;model 按公开模型精确
 * 匹配。列表与汇总共用这套过滤,数字才可对账。 */
export interface UsageAuditParams {
  from?: string
  to?: string
  key_id?: number
  channel_id?: number
  model?: string
  status?: UsageStatus
  source?: 'canvas' | 'direct'
}

export interface ListUsageLogsParams extends UsageAuditParams {
  limit?: number
  offset?: number
}

export interface UsageLogPage {
  logs: UsageLogRecord[]
  /** 过滤后的总行数(不是本页行数),供分页器使用。 */
  total: number
}

/** 汇总桶:按 by 维度各带自己的键,数字永远是同一过滤下明细的和。 */
export interface UsageDayBucket {
  day: string
  requests: number
  errors: number
  charge_usd: number
}

export interface UsageModelBucket {
  model: string
  requests: number
  errors: number
  charge_usd: number
}

export interface UsageChannelBucket {
  channel_id: number
  channel_name: string
  requests: number
  errors: number
  charge_usd: number
}

export type UsageBucket = UsageDayBucket | UsageModelBucket | UsageChannelBucket

export interface UsageSummaryParams extends UsageAuditParams {
  by: 'day' | 'model' | 'channel'
}

interface ErrorPayload {
  error?: { code?: string; message?: string }
}

/** Error with the admin APIs' standard {"error":{code,message}} body. */
export class ApiError extends Error {
  readonly status: number
  readonly code: string

  constructor(status: number, code: string, message: string) {
    super(message)
    this.name = 'ApiError'
    this.status = status
    this.code = code
  }
}

/** 401 from the backend: not signed in, bad credentials, or expired session. */
export class UnauthorizedError extends ApiError {
  constructor(code: string, message: string) {
    super(401, code, message)
    this.name = 'UnauthorizedError'
  }
}

export interface ApiClientOptions {
  /** Base path all requests go under; the dev server proxies it to a backend. */
  base?: string
  fetch?: typeof fetch
  /** Returns the bearer token to attach to requests, or null when signed out. */
  getToken?: () => string | null
}

interface RequestOptions {
  method?: string
  body?: unknown
  /** Non-2xx statuses to resolve with instead of throwing (e.g. 503 health). */
  allow?: number[]
}

export class ApiClient {
  private readonly base: string
  private readonly fetchImpl: typeof fetch
  private readonly getToken?: () => string | null

  constructor({ base = '/api', fetch: fetchImpl = globalThis.fetch, getToken }: ApiClientOptions = {}) {
    this.base = base.replace(/\/+$/, '')
    // 绑定到全局再调用:浏览器原生 fetch 脱离 window 作为 this 会抛 Illegal invocation。
    this.fetchImpl = fetchImpl.bind(globalThis)
    this.getToken = getToken
  }

  /**
   * Fetches /healthz from the backend. Resolves with the parsed report even
   * when a dependency is down (the backend answers 503 with a full report);
   * rejects only on network errors or unexpected HTTP statuses.
   */
  async health(): Promise<HealthReport> {
    return this.request<HealthReport>('/healthz', { allow: [503] })
  }

  /** Has the first admin been created? Drives the init-vs-login split. */
  authStatus(): Promise<AuthStatus> {
    return this.request<AuthStatus>('/auth/status')
  }

  /** Creates the first admin account and returns its session. */
  initAdmin(username: string, password: string): Promise<SessionInfo> {
    return this.request<SessionInfo>('/auth/init', { method: 'POST', body: { username, password } })
  }

  /** Verifies credentials and returns a fresh session. */
  login(username: string, password: string): Promise<SessionInfo> {
    return this.request<SessionInfo>('/auth/login', { method: 'POST', body: { username, password } })
  }

  /** Asks the backend who the current bearer token belongs to. */
  me(): Promise<AdminIdentity> {
    return this.request<AdminIdentity>('/auth/me')
  }

  // ---- 网关管理:渠道(挂 /admin/channels,需 JWT 会话)----

  async listChannels(): Promise<Channel[]> {
    const body = await this.request<{ channels: Channel[] }>('/admin/channels')
    return body.channels
  }

  createChannel(input: ChannelInput): Promise<Channel> {
    return this.request<Channel>('/admin/channels', { method: 'POST', body: input })
  }

  /** 更新渠道;input.api_key 留空 = 保留已存密钥。 */
  updateChannel(id: number, input: ChannelInput): Promise<Channel> {
    return this.request<Channel>(`/admin/channels/${id}`, { method: 'PUT', body: input })
  }

  async deleteChannel(id: number): Promise<void> {
    await this.request<void>(`/admin/channels/${id}`, { method: 'DELETE' })
  }

  /** 一键连通测试;探测结论(含失败)总是以 200 返回。 */
  testChannel(id: number): Promise<ChannelTestResult> {
    return this.request<ChannelTestResult>(`/admin/channels/${id}/test`, { method: 'POST' })
  }

  // ---- 网关管理:API key(挂 /admin/keys,需 JWT 会话)----

  async listKeys(): Promise<ApiKeyRecord[]> {
    const body = await this.request<{ keys: ApiKeyRecord[] }>('/admin/keys')
    return body.keys
  }

  /** 创建 key;完整值仅在本响应出现一次。 */
  createKey(input: ApiKeyInput): Promise<CreatedApiKey> {
    return this.request<CreatedApiKey>('/admin/keys', { method: 'POST', body: input })
  }

  revokeKey(id: number): Promise<ApiKeyRecord> {
    return this.request<ApiKeyRecord>(`/admin/keys/${id}/revoke`, { method: 'POST' })
  }

  /** 手工充值;返回更新后的 key(新余额即时可见)。 */
  topUpKey(id: number, amountUsd: number): Promise<ApiKeyRecord> {
    return this.request<ApiKeyRecord>(`/admin/keys/${id}/topup`, {
      method: 'POST',
      body: { amount_usd: amountUsd },
    })
  }

  async keyQuotaLog(id: number): Promise<QuotaEntry[]> {
    const body = await this.request<{ entries: QuotaEntry[] }>(`/admin/keys/${id}/quota-log`)
    return body.entries
  }

  // ---- 网关管理:提示词模板(挂 /admin/prompt-templates,需 JWT 会话)----

  async listPromptTemplates(): Promise<PromptTemplate[]> {
    const body = await this.request<{ templates: PromptTemplate[] }>('/admin/prompt-templates')
    return body.templates
  }

  createPromptTemplate(input: PromptTemplateInput): Promise<PromptTemplate> {
    return this.request<PromptTemplate>('/admin/prompt-templates', { method: 'POST', body: input })
  }

  updatePromptTemplate(id: number, input: PromptTemplateInput): Promise<PromptTemplate> {
    return this.request<PromptTemplate>(`/admin/prompt-templates/${id}`, {
      method: 'PUT',
      body: input,
    })
  }

  async deletePromptTemplate(id: number): Promise<void> {
    await this.request<void>(`/admin/prompt-templates/${id}`, { method: 'DELETE' })
  }

  // ---- 画布(挂 /canvases,需 JWT 会话)----

  async listCanvases(): Promise<CanvasSummary[]> {
    const body = await this.request<{ canvases: CanvasSummary[] }>('/canvases')
    return body.canvases
  }

  createCanvas(name: string): Promise<CanvasDetail> {
    return this.request<CanvasDetail>('/canvases', { method: 'POST', body: { name } })
  }

  getCanvas(id: number): Promise<CanvasDetail> {
    return this.request<CanvasDetail>(`/canvases/${id}`)
  }

  renameCanvas(id: number, name: string): Promise<CanvasDetail> {
    return this.request<CanvasDetail>(`/canvases/${id}`, { method: 'PATCH', body: { name } })
  }

  async deleteCanvas(id: number): Promise<void> {
    await this.request<void>(`/canvases/${id}`, { method: 'DELETE' })
  }

  /** 整图自动保存:expectedVersion 不匹配时后端回答 409 version_conflict
   * (乐观锁,两标签页后保存者收到冲突)。 */
  saveCanvasGraph(id: number, graph: CanvasGraph, expectedVersion: number): Promise<SaveGraphResult> {
    return this.request<SaveGraphResult>(`/canvases/${id}/graph`, {
      method: 'PUT',
      body: { graph, version: expectedVersion },
    })
  }

  // ---- 画布生成任务(挂 /canvases/:id/tasks,需 JWT 会话)----

  /** 提交生成任务(文生图 / 图生视频):任务行落库即返回(queued),
   * 服务端 worker 负责后续。 */
  createCanvasTask(canvasId: number, input: CreateCanvasTaskInput): Promise<CanvasTask> {
    return this.request<{ task: CanvasTask }>(`/canvases/${canvasId}/tasks`, {
      method: 'POST',
      body: input,
    }).then((body) => body.task)
  }

  /** 画布的近期任务(最新在前):编辑器加载时对账 + 轮询时同步。 */
  async listCanvasTasks(canvasId: number): Promise<CanvasTask[]> {
    const body = await this.request<{ tasks: CanvasTask[] }>(`/canvases/${canvasId}/tasks`)
    return body.tasks
  }

  /** 失败任务原地重试:同一任务回队,节点绑定不变。 */
  retryCanvasTask(canvasId: number, taskId: string): Promise<CanvasTask> {
    return this.request<{ task: CanvasTask }>(`/canvases/${canvasId}/tasks/${taskId}/retry`, {
      method: 'POST',
    }).then((body) => body.task)
  }

  /** 撤回进行中的任务(12 号票,图生视频):服务端同步取消网关任务并
   * 退回预扣;已终态的任务原样回放。 */
  cancelCanvasTask(canvasId: number, taskId: string): Promise<CanvasTask> {
    return this.request<{ task: CanvasTask }>(`/canvases/${canvasId}/tasks/${taskId}/cancel`, {
      method: 'POST',
    }).then((body) => body.task)
  }

  /** 可用于文生图的公开模型(按次计价的 call 轨模型,名字排序)。 */
  async listImageModels(): Promise<string[]> {
    const body = await this.request<{ models: string[] }>('/image-models')
    return body.models
  }

  /** 可用于图生视频的公开模型(按秒计价的 second 轨模型,名字排序)。 */
  async listVideoModels(): Promise<string[]> {
    const body = await this.request<{ models: string[] }>('/video-models')
    return body.models
  }

  /** 可用于 Agent 会话的技能目录(仅启用,画布侧每次即时读库)。 */
  async listPromptTemplateCatalog(): Promise<PromptTemplateOption[]> {
    const body = await this.request<{ templates: PromptTemplateOption[] }>('/prompt-templates')
    return body.templates
  }

  /** 可用于提示词生成的聊天模型(token 轨计价的公开模型,名字排序)。 */
  async listPromptModels(): Promise<string[]> {
    const body = await this.request<{ models: string[] }>('/prompt-models')
    return body.models
  }

  /** 生成提示词(Agent 会话,29 号票):canvas/server 把技能渲染文本作
   * 首条指令、拼历史与本轮输入经网关聊天接口生成,同步返回文本。 */
  generatePrompt(canvasId: number, input: GeneratePromptInput): Promise<GeneratePromptResult> {
    return this.request<GeneratePromptResult>(`/canvases/${canvasId}/generate-prompt`, {
      method: 'POST',
      body: input,
    })
  }

  /** 生成提示词的流式变体(31 号票):POST /canvases/:id/generate-prompt/stream,
   * SSE 响应逐帧把增量交给 onDelta,`data: [DONE]` 收尾时返回累计全文;
   * `data: {"error":{code,message}}` 中途失败抛 ApiError。校验类失败仍是
   * 普通 JSON 错误响应(非 2xx → ApiError),流断在半途(无 DONE)同样抛错。 */
  async generatePromptStream(
    canvasId: number,
    input: GeneratePromptInput,
    onDelta: (delta: string) => void,
  ): Promise<string> {
    const headers = new Headers({ 'Content-Type': 'application/json' })
    const token = this.getToken?.()
    if (token) {
      headers.set('Authorization', `Bearer ${token}`)
    }
    const res = await this.fetchImpl(`${this.base}/canvases/${canvasId}/generate-prompt/stream`, {
      method: 'POST',
      headers,
      body: JSON.stringify(input),
    })
    if (res.status < 200 || res.status > 299) {
      // 校验失败发生在流打开之前:与 request() 同款错误形状。
      const payload: unknown = await res.json().catch(() => null)
      const { code, message } = errorInfo(payload, res.status)
      if (res.status === 401) {
        throw new UnauthorizedError(code, message)
      }
      throw new ApiError(res.status, code, message)
    }
    if (!res.body) {
      throw new ApiError(res.status, 'error', '响应没有内容流')
    }

    const reader = res.body.getReader()
    const decoder = new TextDecoder()
    let buffer = ''
    let text = ''
    let finished = false
    for (;;) {
      const { done, value } = await reader.read()
      if (done) {
        break
      }
      buffer += decoder.decode(value, { stream: true })
      let boundary = buffer.indexOf('\n\n')
      while (boundary >= 0) {
        const event = buffer.slice(0, boundary)
        buffer = buffer.slice(boundary + 2)
        boundary = buffer.indexOf('\n\n')

        const dataLine = event.split('\n').find((line) => line.startsWith('data:'))
        if (dataLine === undefined) {
          continue
        }
        const payload = dataLine.slice(5).replace(/^ /, '')
        if (payload === '[DONE]') {
          finished = true
          break
        }
        try {
          const parsed = JSON.parse(payload) as { delta?: string; error?: { code?: string; message?: string } }
          if (parsed.error) {
            throw new ApiError(502, parsed.error.code ?? 'upstream_error', parsed.error.message ?? '生成失败')
          }
          if (parsed.delta) {
            text += parsed.delta
            onDelta(parsed.delta)
          }
        } catch (e) {
          if (e instanceof ApiError) {
            throw e
          }
          // 不可解析的帧:后端只发合法 JSON,出现即跳过不打断。
        }
      }
      if (finished) {
        return text
      }
    }
    // 流断在半途(无 DONE):已交增量不作成功,由调用方决定去留。
    throw new ApiError(502, 'upstream_error', '生成流中断,未收到完成标记')
  }

  /** 视频反推提示词(13 号票):canvas/server 经网关多模态聊天接口分析
   * 视频,同步返回提示词文本;用量按 token 计费入网关用量日志。 */
  reversePrompt(canvasId: number, input: ReversePromptInput): Promise<ReversePromptResult> {
    return this.request<ReversePromptResult>(`/canvases/${canvasId}/reverse-prompt`, {
      method: 'POST',
      body: input,
    })
  }

  /** 画布分析(17 号票):canvas/server 经网关多模态聊天接口理解视频/
   * 图片,同步返回结构化分镜 markdown;文本由编辑器写入分析节点。 */
  analyzeMedia(canvasId: number, input: AnalyzeInput): Promise<AnalyzeResult> {
    return this.request<AnalyzeResult>(`/canvases/${canvasId}/analyze`, {
      method: 'POST',
      body: input,
    })
  }

  // ---- 素材库(挂 /assets,canvas/server;列表/删除需 JWT 会话,14 号票)----

  /** 素材库列表,最新在前:画布素材面板与管理端素材页共用。 */
  async listAssets(params: ListAssetsParams = {}): Promise<AssetRecord[]> {
    const query = new URLSearchParams()
    if (params.kind) {
      query.set('kind', params.kind)
    }
    if (params.canvas_id !== undefined) {
      query.set('canvas_id', String(params.canvas_id))
    }
    if (params.limit !== undefined) {
      query.set('limit', String(params.limit))
    }
    if (params.offset !== undefined) {
      query.set('offset', String(params.offset))
    }
    const qs = query.toString()
    const body = await this.request<{ assets: AssetRecord[] }>(`/assets${qs ? `?${qs}` : ''}`)
    return body.assets
  }

  /** 删除素材(对象文件与行一起消失);引用它的节点显示占位而非报错。 */
  async deleteAsset(id: number): Promise<void> {
    await this.request<void>(`/assets/${id}`, { method: 'DELETE' })
  }

  /** 上传素材(18 号票;24 号票追加音频):multipart 把本机图片/视频/
   * 音频送进素材库,响应即新素材行 —— content_url 可直接落媒体节点或进
   * 对话框参考条(与素材面板插入同语义)。kind 声明意图,服务端按魔数
   * 嗅探裁决真实类型。 */
  uploadAsset(file: File, kind: 'image' | 'video' | 'audio'): Promise<AssetRecord> {
    const form = new FormData()
    form.set('kind', kind)
    form.set('file', file)
    return this.request<{ asset: AssetRecord }>('/assets/upload', {
      method: 'POST',
      body: form,
    }).then((body) => body.asset)
  }

  // ---- 网关管理:用量审计(挂 /admin/usage,需 JWT 会话,15 号票)----

  /** 请求级用量日志,最新在前;过滤后的 total 随页返回,驱动分页。 */
  async listUsageLogs(params: ListUsageLogsParams = {}): Promise<UsageLogPage> {
    const qs = usageAuditQuery(params)
    const body = await this.request<UsageLogPage>(`/admin/usage/logs${qs ? `?${qs}` : ''}`)
    return body
  }

  /** 按天/模型/渠道汇总;与列表同一套过滤,数字与明细对账一致。 */
  async usageSummary(params: UsageSummaryParams): Promise<UsageBucket[]> {
    const by = `by=${encodeURIComponent(params.by)}`
    const rest = usageAuditQuery(params)
    const body = await this.request<{ buckets: UsageBucket[] }>(
      `/admin/usage/summary?${rest ? `${by}&${rest}` : by}`,
    )
    return body.buckets
  }

  // ---- 网关管理:存储设置(挂 /admin/settings/storage,需 JWT 会话)----

  /** 存储设置;缺行时后端回答 local 缺省(密钥仅 has_* + 尾 4 位提示)。 */
  async getStorageSettings(): Promise<StorageSettings> {
    const body = await this.request<{ storage: StorageSettings }>('/admin/settings/storage')
    return body.storage
  }

  /** 更新存储设置;空 access_key/secret_key 保留已存密钥,即时生效。 */
  async updateStorageSettings(input: StorageSettingsInput): Promise<StorageSettings> {
    const body = await this.request<{ storage: StorageSettings }>('/admin/settings/storage', {
      method: 'PUT',
      body: input,
    })
    return body.storage
  }

  private async request<T>(path: string, { method = 'GET', body, allow = [] }: RequestOptions = {}): Promise<T> {
    // multipart 表单(上传素材)交给 fetch 自带的多部分编码:手工设
    // Content-Type 反而会丢掉浏览器生成的 boundary。
    const isForm = typeof FormData !== 'undefined' && body instanceof FormData
    const headers = new Headers()
    if (body !== undefined && !isForm) {
      headers.set('Content-Type', 'application/json')
    }
    const token = this.getToken?.()
    if (token) {
      headers.set('Authorization', `Bearer ${token}`)
    }

    // multipart 表单原样交给 fetch,JSON 体序列化后出发。
    const outgoingBody = body === undefined ? undefined : isForm ? (body as FormData) : JSON.stringify(body)
    const res = await this.fetchImpl(`${this.base}${path}`, {
      method,
      headers,
      body: outgoingBody,
    })
    if (res.status === 204) {
      // 删除等操作无响应体;json() 会对空体抛错,直接返回。
      return undefined as T
    }
    // 与 Response.ok 等价;自建 fetch 替身只需提供 status 与 json。
    const successful = res.status >= 200 && res.status < 300
    if (successful || allow.includes(res.status)) {
      return (await res.json()) as T
    }
    // 错误响应体允许缺失或不可解析(如代理返回的 HTML/空体),退化为通用错误。
    const payload: unknown = await res.json().catch(() => null)
    const { code, message } = errorInfo(payload, res.status)
    if (res.status === 401) {
      throw new UnauthorizedError(code, message)
    }
    throw new ApiError(res.status, code, message)
  }
}

/** 用量审计的公共查询串:列表与汇总共用同一套过滤,保证两处数字对得上。 */
function usageAuditQuery(params: ListUsageLogsParams): string {
  const query = new URLSearchParams()
  if (params.from) {
    query.set('from', params.from)
  }
  if (params.to) {
    query.set('to', params.to)
  }
  if (params.key_id !== undefined) {
    query.set('key_id', String(params.key_id))
  }
  if (params.channel_id !== undefined) {
    query.set('channel_id', String(params.channel_id))
  }
  if (params.model) {
    query.set('model', params.model)
  }
  if (params.status) {
    query.set('status', params.status)
  }
  if (params.source) {
    query.set('source', params.source)
  }
  if (params.limit !== undefined) {
    query.set('limit', String(params.limit))
  }
  if (params.offset !== undefined) {
    query.set('offset', String(params.offset))
  }
  return query.toString()
}

function errorInfo(payload: unknown, status: number): { code: string; message: string } {
  const err = (payload as ErrorPayload | null)?.error
  return {
    code: err?.code ?? 'error',
    message: err?.message ?? `请求失败 (HTTP ${status})`,
  }
}
