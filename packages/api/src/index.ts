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

/** A vendor channel as answered by the admin gateway APIs. The vendor
 * secret never crosses the wire — only has_key and its last-4 hint. */
export interface Channel {
  id: number
  name: string
  type: string
  base_url: string
  has_key: boolean
  key_hint?: string
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

// ---- 网关管理:提示词模板(挂 /admin/prompt-templates,需 JWT 会话)----

/** 一条提示词模板:template 内含 {topic} 占位符,画布生成提示词时以
 * 输入的主题替换;画布侧动作每次即时读库,增删改立即生效(11 号票)。 */
export interface PromptTemplate {
  id: number
  name: string
  template: string
  enabled: boolean
  created_at: string
  updated_at: string
}

export interface PromptTemplateInput {
  name: string
  template: string
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
 * 素材库引用;seconds 只属于视频任务。 */
export interface CanvasTask {
  id: string
  canvas_id: number
  node_id: string
  kind: string
  prompt: string
  model: string
  size: string
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

/** 提交生成任务的请求体。kind=image 为文生图;kind=video 为图生视频,
 * 需要 image_url(参考图片)并可带 seconds(期望时长,缺省 5 秒)。 */
export interface CreateCanvasTaskInput {
  node_id: string
  kind: 'image' | 'video'
  prompt: string
  model: string
  size?: string
  image_url?: string
  seconds?: number
}

/** 画布侧提示词模板目录项(仅启用中的模板,只带 id 与名字)。 */
export interface PromptTemplateOption {
  id: number
  name: string
}

/** 生成提示词的请求体:template_id 选模板,topic 为输入的主题,
 * model 是 token 轨聊天模型;node_id 可选,用于用量归因。 */
export interface GeneratePromptInput {
  node_id?: string
  template_id: number
  topic: string
  model: string
}

/** 生成提示词的响应:文本由编辑器写入当前节点或新建提示词节点。 */
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

/** 视频反推提示词的响应:文本由编辑器落为新的提示词节点。 */
export interface ReversePromptResult {
  text: string
}

// ---- 素材库(挂 /assets,canvas/server;列表/删除需 JWT 会话,14 号票)----

/** 一条素材:生成产物在素材库中的引用。content_url 恒为内容寻址路径
 * (预览与跨画布复用统一走它),url 是原始厂商地址(或历史 data: URI),
 * 仅排障时关心。canvas_name 来自来源画布,画布已删时为空。 */
export interface AssetRecord {
  id: number
  kind: 'image' | 'video'
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
  kind?: 'image' | 'video'
  canvas_id?: number
  limit?: number
  offset?: number
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

  /** 可用于提示词生成的模板目录(仅启用,画布侧每次即时读库)。 */
  async listPromptTemplateCatalog(): Promise<PromptTemplateOption[]> {
    const body = await this.request<{ templates: PromptTemplateOption[] }>('/prompt-templates')
    return body.templates
  }

  /** 可用于提示词生成的聊天模型(token 轨计价的公开模型,名字排序)。 */
  async listPromptModels(): Promise<string[]> {
    const body = await this.request<{ models: string[] }>('/prompt-models')
    return body.models
  }

  /** 生成提示词:canvas/server 经网关聊天接口按模板渲染,同步返回文本。 */
  generatePrompt(canvasId: number, input: GeneratePromptInput): Promise<GeneratePromptResult> {
    return this.request<GeneratePromptResult>(`/canvases/${canvasId}/generate-prompt`, {
      method: 'POST',
      body: input,
    })
  }

  /** 视频反推提示词(13 号票):canvas/server 经网关多模态聊天接口分析
   * 视频,同步返回提示词文本;用量按 token 计费入网关用量日志。 */
  reversePrompt(canvasId: number, input: ReversePromptInput): Promise<ReversePromptResult> {
    return this.request<ReversePromptResult>(`/canvases/${canvasId}/reverse-prompt`, {
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

  private async request<T>(path: string, { method = 'GET', body, allow = [] }: RequestOptions = {}): Promise<T> {
    const headers = new Headers()
    if (body !== undefined) {
      headers.set('Content-Type', 'application/json')
    }
    const token = this.getToken?.()
    if (token) {
      headers.set('Authorization', `Bearer ${token}`)
    }

    const res = await this.fetchImpl(`${this.base}${path}`, {
      method,
      headers,
      body: body === undefined ? undefined : JSON.stringify(body),
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

function errorInfo(payload: unknown, status: number): { code: string; message: string } {
  const err = (payload as ErrorPayload | null)?.error
  return {
    code: err?.code ?? 'error',
    message: err?.message ?? `请求失败 (HTTP ${status})`,
  }
}
