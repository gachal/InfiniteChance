import { describe, expect, it, vi } from 'vitest'

import { ApiClient, ApiError, UnauthorizedError, type HealthReport, type SessionInfo } from './index'

const healthy: HealthReport = {
  service: 'gateway',
  status: 'ok',
  checks: {
    mysql: { status: 'up' },
    redis: { status: 'up' },
  },
}

const session: SessionInfo = {
  token: 'jwt-token-value',
  expires_at: '2026-09-09T12:00:00Z',
  username: 'admin',
}

function stubFetch(status: number, body: unknown) {
  return vi.fn().mockResolvedValue({ status, json: () => Promise.resolve(body) })
}

function clientWith(fetchImpl: ReturnType<typeof stubFetch>): ApiClient {
  return new ApiClient({ fetch: fetchImpl as unknown as typeof fetch })
}

describe('ApiClient.health', () => {
  it('requests /healthz under the base path and returns the parsed report', async () => {
    const fetchImpl = stubFetch(200, healthy)
    const client = new ApiClient({ base: '/api', fetch: fetchImpl as unknown as typeof fetch })

    await expect(client.health()).resolves.toEqual(healthy)
    expect(fetchImpl).toHaveBeenCalledWith('/api/healthz', expect.anything())
  })

  it('strips trailing slashes from the base path', async () => {
    const fetchImpl = stubFetch(200, healthy)
    const client = new ApiClient({ base: '/api/', fetch: fetchImpl as unknown as typeof fetch })

    await client.health()
    expect(fetchImpl).toHaveBeenCalledWith('/api/healthz', expect.anything())
  })

  it('resolves with the degraded report when the backend answers 503', async () => {
    const degraded: HealthReport = {
      service: 'canvas',
      status: 'degraded',
      checks: { mysql: { status: 'down', error: 'connection refused' }, redis: { status: 'up' } },
    }
    const client = clientWith(stubFetch(503, degraded))

    await expect(client.health()).resolves.toEqual(degraded)
  })

  it('rejects on unexpected HTTP statuses', async () => {
    const client = clientWith(stubFetch(404, { message: 'not found' }))

    await expect(client.health()).rejects.toThrow('HTTP 404')
  })

  it('calls the injected fetch in a way that satisfies native binding requirements', async () => {
    // 浏览器的原生 window.fetch 脱离 window 作为 this 调用时会抛 Illegal invocation。
    const nativeLikeFetch = vi.fn(function (this: unknown) {
      if (this === undefined) {
        throw new TypeError("Failed to execute 'fetch' on 'Window': Illegal invocation")
      }
      return Promise.resolve({ status: 200, json: () => Promise.resolve(healthy) })
    })
    const client = new ApiClient({ fetch: nativeLikeFetch as unknown as typeof fetch })

    await expect(client.health()).resolves.toEqual(healthy)
  })
})

describe('ApiClient auth', () => {
  it('authStatus requests /auth/status and reports initialization state', async () => {
    const fetchImpl = stubFetch(200, { initialized: false })
    const client = new ApiClient({ base: '/api', fetch: fetchImpl as unknown as typeof fetch })

    await expect(client.authStatus()).resolves.toEqual({ initialized: false })
    const [url, init] = fetchImpl.mock.calls[0]
    expect(url).toBe('/api/auth/status')
    expect((init as RequestInit).method).toBe('GET')
  })

  it('login posts JSON credentials to /auth/login and returns the session', async () => {
    const fetchImpl = stubFetch(200, session)
    const client = new ApiClient({ base: '/api', fetch: fetchImpl as unknown as typeof fetch })

    await expect(client.login('admin', 's3cret-password')).resolves.toEqual(session)
    const [url, init] = fetchImpl.mock.calls[0]
    expect(url).toBe('/api/auth/login')
    expect((init as RequestInit).method).toBe('POST')
    expect((init as RequestInit).body).toBe(JSON.stringify({ username: 'admin', password: 's3cret-password' }))
  })

  it('initAdmin posts JSON credentials to /auth/init and returns the session', async () => {
    const fetchImpl = stubFetch(201, session)
    const client = new ApiClient({ base: '/api', fetch: fetchImpl as unknown as typeof fetch })

    await expect(client.initAdmin('admin', 's3cret-password')).resolves.toEqual(session)
    const [url, init] = fetchImpl.mock.calls[0]
    expect(url).toBe('/api/auth/init')
    expect((init as RequestInit).method).toBe('POST')
  })

  it('me attaches the bearer token from getToken', async () => {
    const fetchImpl = stubFetch(200, { username: 'admin', expires_at: session.expires_at })
    const client = new ApiClient({
      base: '/api',
      fetch: fetchImpl as unknown as typeof fetch,
      getToken: () => session.token,
    })

    await client.me()
    const [, init] = fetchImpl.mock.calls[0]
    expect((init as RequestInit).headers).toBeInstanceOf(Headers)
    expect(((init as RequestInit).headers as Headers).get('Authorization')).toBe(`Bearer ${session.token}`)
  })

  it('sends no Authorization header when getToken returns null', async () => {
    const fetchImpl = stubFetch(200, { username: 'admin', expires_at: session.expires_at })
    const client = new ApiClient({
      base: '/api',
      fetch: fetchImpl as unknown as typeof fetch,
      getToken: () => null,
    })

    await client.me()
    const [, init] = fetchImpl.mock.calls[0]
    expect(Array.from(((init as RequestInit).headers as Headers).keys())).not.toContain('authorization')
  })

  it('rejects 401 with UnauthorizedError carrying the backend code and message', async () => {
    const fetchImpl = stubFetch(401, { error: { code: 'invalid_credentials', message: '用户名或密码错误' } })
    const client = new ApiClient({ base: '/api', fetch: fetchImpl as unknown as typeof fetch })

    const err = await client.login('admin', 'wrong-password').catch((e: unknown) => e)
    expect(err).toBeInstanceOf(UnauthorizedError)
    expect((err as UnauthorizedError).code).toBe('invalid_credentials')
    expect((err as UnauthorizedError).message).toBe('用户名或密码错误')
  })

  it('rejects other error statuses with ApiError carrying the status', async () => {
    const fetchImpl = stubFetch(409, { error: { code: 'already_initialized', message: '管理员账号已存在,请直接登录' } })
    const client = new ApiClient({ base: '/api', fetch: fetchImpl as unknown as typeof fetch })

    const err = await client.initAdmin('admin', 's3cret-password').catch((e: unknown) => e)
    expect(err).toBeInstanceOf(ApiError)
    expect(err).not.toBeInstanceOf(UnauthorizedError)
    expect((err as ApiError).status).toBe(409)
    expect((err as ApiError).code).toBe('already_initialized')
  })
})
