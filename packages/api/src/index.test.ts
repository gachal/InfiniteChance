import { describe, expect, it, vi } from 'vitest'

import {
  ApiClient,
  ApiError,
  UnauthorizedError,
  type CanvasDetail,
  type CanvasGraph,
  type CanvasSummary,
  type Channel,
  type ApiKeyRecord,
  type CreatedApiKey,
  type GeneratePromptInput,
  type HealthReport,
  type PromptTemplate,
  type PromptTemplateInput,
  type PromptTemplateOption,
  type QuotaEntry,
  type SaveGraphResult,
  type SessionInfo,
} from './index'

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

function clientWithBase(fetchImpl: ReturnType<typeof stubFetch>): ApiClient {
  return new ApiClient({ base: '/api', fetch: fetchImpl as unknown as typeof fetch })
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

  it('maps non-JSON error bodies (proxies, empty pages) to a generic ApiError', async () => {
    const fetchImpl = vi.fn().mockResolvedValue({
      status: 502,
      json: () => Promise.reject(new SyntaxError('Unexpected token < in JSON')),
    })
    const client = new ApiClient({ base: '/api', fetch: fetchImpl as unknown as typeof fetch })

    const err = await client.login('admin', 's3cret-password').catch((e: unknown) => e)
    expect(err).toBeInstanceOf(ApiError)
    expect((err as ApiError).status).toBe(502)
    expect((err as ApiError).code).toBe('error')
    expect((err as ApiError).message).toBe('请求失败 (HTTP 502)')
  })

  it('resolves 204 responses to undefined without parsing a body', async () => {
    const fetchImpl = vi.fn().mockResolvedValue({ status: 204, json: () => Promise.resolve(null) })
    const client = clientWithBase(fetchImpl as never)

    await expect(client.deleteChannel(7)).resolves.toBeUndefined()
    const [url, init] = fetchImpl.mock.calls[0]
    expect(url).toBe('/api/admin/channels/7')
    expect((init as RequestInit).method).toBe('DELETE')
  })
})

describe('ApiClient channels', () => {
  const channel: Channel = {
    id: 1,
    name: 'openai-main',
    type: 'openai',
    base_url: 'https://api.openai.com/v1',
    has_key: true,
    key_hint: '…9876',
    model_map: { 'gpt-4o': 'gpt-4o-2024-11-20' },
    priority: 10,
    weight: 1,
    enabled: true,
    created_at: '2026-09-02T12:00:00Z',
    updated_at: '2026-09-02T12:00:00Z',
  }

  it('listChannels unwraps the channels envelope and attaches the token', async () => {
    const fetchImpl = stubFetch(200, { channels: [channel] })
    const client = new ApiClient({
      base: '/api',
      fetch: fetchImpl as unknown as typeof fetch,
      getToken: () => session.token,
    })

    await expect(client.listChannels()).resolves.toEqual([channel])
    const [url, init] = fetchImpl.mock.calls[0]
    expect(url).toBe('/api/admin/channels')
    expect(((init as RequestInit).headers as Headers).get('Authorization')).toBe(
      `Bearer ${session.token}`,
    )
  })

  it('createChannel posts the full config including the vendor secret', async () => {
    const fetchImpl = stubFetch(201, channel)
    const client = clientWithBase(fetchImpl)

    const input = {
      name: 'openai-main',
      type: 'openai',
      base_url: 'https://api.openai.com/v1',
      api_key: 'sk-vendor-secret',
      model_map: { 'gpt-4o': 'gpt-4o' },
      priority: 10,
      weight: 1,
      enabled: true,
    }
    await expect(client.createChannel(input)).resolves.toEqual(channel)
    const [url, init] = fetchImpl.mock.calls[0]
    expect(url).toBe('/api/admin/channels')
    expect((init as RequestInit).method).toBe('POST')
    expect((init as RequestInit).body).toBe(JSON.stringify(input))
  })

  it('updateChannel PUTs to the id path (blank api_key keeps stored secret)', async () => {
    const fetchImpl = stubFetch(200, channel)
    const client = clientWithBase(fetchImpl)

    await client.updateChannel(3, { ...channel, api_key: '', id: undefined } as never)
    const [url, init] = fetchImpl.mock.calls[0]
    expect(url).toBe('/api/admin/channels/3')
    expect((init as RequestInit).method).toBe('PUT')
  })

  it('testChannel POSTs and returns the probe verdict (200 even on failure)', async () => {
    const verdict = { ok: false, latency_ms: 87, error: '上游返回 HTTP 401' }
    const fetchImpl = stubFetch(200, verdict)
    const client = clientWithBase(fetchImpl)

    await expect(client.testChannel(3)).resolves.toEqual(verdict)
    const [url, init] = fetchImpl.mock.calls[0]
    expect(url).toBe('/api/admin/channels/3/test')
    expect((init as RequestInit).method).toBe('POST')
  })
})

describe('ApiClient keys', () => {
  const key: ApiKeyRecord = {
    id: 5,
    name: 'canvas-service',
    prefix: 'sk-abcd1234',
    quota_usd: 12.5,
    status: 'active',
    expires_at: null,
    revoked_at: null,
    created_at: '2026-09-02T12:00:00Z',
    updated_at: '2026-09-02T12:00:00Z',
  }

  it('createKey returns the full sk- value exactly once', async () => {
    const created: CreatedApiKey = { ...key, key: 'sk-full-value-only-once' }
    const fetchImpl = stubFetch(201, created)
    const client = clientWithBase(fetchImpl)

    await expect(
      client.createKey({ name: 'canvas-service', initial_quota_usd: 10 }),
    ).resolves.toEqual(created)
    const [url, init] = fetchImpl.mock.calls[0]
    expect(url).toBe('/api/admin/keys')
    expect((init as RequestInit).body).toBe(
      JSON.stringify({ name: 'canvas-service', initial_quota_usd: 10 }),
    )
  })

  it('listKeys unwraps the keys envelope (prefix only, never the full value)', async () => {
    const fetchImpl = stubFetch(200, { keys: [key] })
    const client = clientWithBase(fetchImpl)

    await expect(client.listKeys()).resolves.toEqual([key])
    expect(fetchImpl.mock.calls[0][0]).toBe('/api/admin/keys')
  })

  it('revokeKey POSTs to the revoke path', async () => {
    const revoked = { ...key, status: 'revoked' as const }
    const fetchImpl = stubFetch(200, revoked)
    const client = clientWithBase(fetchImpl)

    await expect(client.revokeKey(5)).resolves.toEqual(revoked)
    const [url, init] = fetchImpl.mock.calls[0]
    expect(url).toBe('/api/admin/keys/5/revoke')
    expect((init as RequestInit).method).toBe('POST')
  })

  it('topUpKey posts amount_usd and returns the refreshed balance', async () => {
    const topped = { ...key, quota_usd: 15 }
    const fetchImpl = stubFetch(200, topped)
    const client = clientWithBase(fetchImpl)

    await expect(client.topUpKey(5, 2.5)).resolves.toEqual(topped)
    const [url, init] = fetchImpl.mock.calls[0]
    expect(url).toBe('/api/admin/keys/5/topup')
    expect((init as RequestInit).body).toBe(JSON.stringify({ amount_usd: 2.5 }))
  })

  it('keyQuotaLog unwraps the ledger entries, newest first', async () => {
    const entries: QuotaEntry[] = [
      { id: 2, delta_usd: 2.5, balance_usd: 12.5, reason: 'manual_topup', created_at: '2026-09-02T13:00:00Z' },
      { id: 1, delta_usd: 10, balance_usd: 10, reason: 'initial', created_at: '2026-09-02T12:00:00Z' },
    ]
    const fetchImpl = stubFetch(200, { entries })
    const client = clientWithBase(fetchImpl)

    await expect(client.keyQuotaLog(5)).resolves.toEqual(entries)
    expect(fetchImpl.mock.calls[0][0]).toBe('/api/admin/keys/5/quota-log')
  })
})

describe('ApiClient canvases', () => {
  const summary: CanvasSummary = {
    id: 2,
    name: '第一张画布',
    version: 4,
    created_at: '2026-09-03T08:00:00Z',
    updated_at: '2026-09-03T09:30:00Z',
  }
  const graph: CanvasGraph = { nodes: [{ id: 'n1', type: 'prompt' }], edges: [] }

  it('listCanvases unwraps the canvases envelope and attaches the token', async () => {
    const fetchImpl = stubFetch(200, { canvases: [summary] })
    const client = new ApiClient({
      base: '/api',
      fetch: fetchImpl as unknown as typeof fetch,
      getToken: () => session.token,
    })

    await expect(client.listCanvases()).resolves.toEqual([summary])
    const [url, init] = fetchImpl.mock.calls[0]
    expect(url).toBe('/api/canvases')
    expect(((init as RequestInit).headers as Headers).get('Authorization')).toBe(
      `Bearer ${session.token}`,
    )
  })

  it('createCanvas POSTs the name and returns the created detail', async () => {
    const detail: CanvasDetail = { ...summary, graph: { nodes: [], edges: [] } }
    const fetchImpl = stubFetch(201, detail)
    const client = clientWithBase(fetchImpl)

    await expect(client.createCanvas('第一张画布')).resolves.toEqual(detail)
    const [url, init] = fetchImpl.mock.calls[0]
    expect(url).toBe('/api/canvases')
    expect((init as RequestInit).method).toBe('POST')
    expect((init as RequestInit).body).toBe(JSON.stringify({ name: '第一张画布' }))
  })

  it('getCanvas GETs the id path and returns detail with graph', async () => {
    const detail: CanvasDetail = { ...summary, graph }
    const fetchImpl = stubFetch(200, detail)
    const client = clientWithBase(fetchImpl)

    await expect(client.getCanvas(2)).resolves.toEqual(detail)
    expect(fetchImpl.mock.calls[0][0]).toBe('/api/canvases/2')
  })

  it('renameCanvas PATCHes {name} to the id path', async () => {
    const fetchImpl = stubFetch(200, summary)
    const client = clientWithBase(fetchImpl)

    await expect(client.renameCanvas(2, '新名字')).resolves.toEqual(summary)
    const [url, init] = fetchImpl.mock.calls[0]
    expect(url).toBe('/api/canvases/2')
    expect((init as RequestInit).method).toBe('PATCH')
    expect((init as RequestInit).body).toBe(JSON.stringify({ name: '新名字' }))
  })

  it('deleteCanvas DELETEs to the id path (204 → undefined)', async () => {
    const fetchImpl = vi.fn().mockResolvedValue({ status: 204, json: () => Promise.resolve(null) })
    const client = clientWithBase(fetchImpl as never)

    await expect(client.deleteCanvas(2)).resolves.toBeUndefined()
    const [url, init] = fetchImpl.mock.calls[0]
    expect(url).toBe('/api/canvases/2')
    expect((init as RequestInit).method).toBe('DELETE')
  })

  it('saveCanvasGraph PUTs {graph, version} and returns the next version', async () => {
    const result: SaveGraphResult = { version: 5, updated_at: '2026-09-03T09:31:00Z' }
    const fetchImpl = stubFetch(200, result)
    const client = clientWithBase(fetchImpl)

    await expect(client.saveCanvasGraph(2, graph, 4)).resolves.toEqual(result)
    const [url, init] = fetchImpl.mock.calls[0]
    expect(url).toBe('/api/canvases/2/graph')
    expect((init as RequestInit).method).toBe('PUT')
    expect((init as RequestInit).body).toBe(JSON.stringify({ graph, version: 4 }))
  })

  it('saveCanvasGraph surfaces 409 as ApiError with code version_conflict', async () => {
    const fetchImpl = stubFetch(409, { error: { code: 'version_conflict', message: '画布已在其他窗口被修改' } })
    const client = clientWithBase(fetchImpl)

    const err = await client.saveCanvasGraph(2, graph, 4).catch((e: unknown) => e)
    expect(err).toBeInstanceOf(ApiError)
    expect((err as ApiError).status).toBe(409)
    expect((err as ApiError).code).toBe('version_conflict')
  })
})

describe('ApiClient prompt templates (admin)', () => {
  const template: PromptTemplate = {
    id: 3,
    name: '文生图-中文',
    template: '请为主题「{topic}」写一段英文文生图提示词,只输出提示词本身。',
    enabled: true,
    created_at: '2026-09-03T08:00:00Z',
    updated_at: '2026-09-03T08:00:00Z',
  }

  it('listPromptTemplates unwraps the templates envelope', async () => {
    const fetchImpl = stubFetch(200, { templates: [template] })
    const client = clientWithBase(fetchImpl)

    await expect(client.listPromptTemplates()).resolves.toEqual([template])
    expect(fetchImpl.mock.calls[0][0]).toBe('/api/admin/prompt-templates')
  })

  it('createPromptTemplate posts the input and returns the created row', async () => {
    const fetchImpl = stubFetch(201, template)
    const client = clientWithBase(fetchImpl)

    const input: PromptTemplateInput = { name: template.name, template: template.template }
    await expect(client.createPromptTemplate(input)).resolves.toEqual(template)
    const [url, init] = fetchImpl.mock.calls[0]
    expect(url).toBe('/api/admin/prompt-templates')
    expect((init as RequestInit).method).toBe('POST')
    expect((init as RequestInit).body).toBe(JSON.stringify(input))
  })

  it('updatePromptTemplate PUTs to the id path', async () => {
    const fetchImpl = stubFetch(200, { ...template, enabled: false })
    const client = clientWithBase(fetchImpl)

    await client.updatePromptTemplate(3, { name: template.name, template: template.template, enabled: false })
    const [url, init] = fetchImpl.mock.calls[0]
    expect(url).toBe('/api/admin/prompt-templates/3')
    expect((init as RequestInit).method).toBe('PUT')
  })

  it('deletePromptTemplate DELETEs to the id path (204 → undefined)', async () => {
    const fetchImpl = vi.fn().mockResolvedValue({ status: 204, json: () => Promise.resolve(null) })
    const client = clientWithBase(fetchImpl as never)

    await expect(client.deletePromptTemplate(3)).resolves.toBeUndefined()
    const [url, init] = fetchImpl.mock.calls[0]
    expect(url).toBe('/api/admin/prompt-templates/3')
    expect((init as RequestInit).method).toBe('DELETE')
  })
})

describe('ApiClient prompt generation (canvas)', () => {
  it('listPromptTemplateCatalog unwraps the catalog envelope', async () => {
    const options: PromptTemplateOption[] = [{ id: 3, name: '文生图-中文' }]
    const fetchImpl = stubFetch(200, { templates: options })
    const client = clientWithBase(fetchImpl)

    await expect(client.listPromptTemplateCatalog()).resolves.toEqual(options)
    expect(fetchImpl.mock.calls[0][0]).toBe('/api/prompt-templates')
  })

  it('listPromptModels unwraps the models envelope', async () => {
    const fetchImpl = stubFetch(200, { models: ['chat-a', 'chat-b'] })
    const client = clientWithBase(fetchImpl)

    await expect(client.listPromptModels()).resolves.toEqual(['chat-a', 'chat-b'])
    expect(fetchImpl.mock.calls[0][0]).toBe('/api/prompt-models')
  })

  it('generatePrompt POSTs to the canvas path and returns the text', async () => {
    const fetchImpl = stubFetch(200, { text: 'a neon city at dusk' })
    const client = clientWithBase(fetchImpl)

    const input: GeneratePromptInput = {
      node_id: 'prompt-1-1',
      template_id: 3,
      topic: '赛博朋克城市',
      model: 'chat-m',
    }
    await expect(client.generatePrompt(7, input)).resolves.toEqual({ text: 'a neon city at dusk' })
    const [url, init] = fetchImpl.mock.calls[0]
    expect(url).toBe('/api/canvases/7/generate-prompt')
    expect((init as RequestInit).method).toBe('POST')
    expect((init as RequestInit).body).toBe(JSON.stringify(input))
  })

  it('generatePrompt surfaces upstream errors as ApiError with the code', async () => {
    const fetchImpl = stubFetch(502, { error: { code: 'upstream_error', message: 'gateway 402: 余额不足' } })
    const client = clientWithBase(fetchImpl)

    const err = await client
      .generatePrompt(7, { template_id: 3, topic: '任意', model: 'chat-m' })
      .catch((e: unknown) => e)
    expect(err).toBeInstanceOf(ApiError)
    expect((err as ApiError).status).toBe(502)
    expect((err as ApiError).code).toBe('upstream_error')
  })
})
