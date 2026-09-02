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
    const payload: unknown = await res.json()

    // 与 Response.ok 等价;自建 fetch 替身只需提供 status 与 json。
    const successful = res.status >= 200 && res.status < 300
    if (successful || allow.includes(res.status)) {
      return payload as T
    }
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
