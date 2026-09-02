import { describe, expect, it, vi } from 'vitest'

import { ApiClient, type HealthReport } from './index'

const healthy: HealthReport = {
  service: 'gateway',
  status: 'ok',
  checks: {
    mysql: { status: 'up' },
    redis: { status: 'up' },
  },
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
    expect(fetchImpl).toHaveBeenCalledWith('/api/healthz')
  })

  it('strips trailing slashes from the base path', async () => {
    const fetchImpl = stubFetch(200, healthy)
    const client = new ApiClient({ base: '/api/', fetch: fetchImpl as unknown as typeof fetch })

    await client.health()
    expect(fetchImpl).toHaveBeenCalledWith('/api/healthz')
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
})
