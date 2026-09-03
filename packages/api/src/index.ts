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
