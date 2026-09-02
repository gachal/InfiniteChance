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

export interface ApiClientOptions {
  /** Base path all requests go under; the dev server proxies it to a backend. */
  base?: string
  fetch?: typeof fetch
}

export class ApiClient {
  private readonly base: string
  private readonly fetchImpl: typeof fetch

  constructor({ base = '/api', fetch: fetchImpl = globalThis.fetch }: ApiClientOptions = {}) {
    this.base = base.replace(/\/+$/, '')
    // 绑定到全局再调用:浏览器原生 fetch 脱离 window 作为 this 会抛 Illegal invocation。
    this.fetchImpl = fetchImpl.bind(globalThis)
  }

  /**
   * Fetches /healthz from the backend. Resolves with the parsed report even
   * when a dependency is down (the backend answers 503 with a full report);
   * rejects only on network errors or unexpected HTTP statuses.
   */
  async health(): Promise<HealthReport> {
    const res = await this.fetchImpl(`${this.base}/healthz`)
    if (res.status !== 200 && res.status !== 503) {
      throw new Error(`health check failed with HTTP ${res.status}`)
    }
    return (await res.json()) as HealthReport
  }
}
