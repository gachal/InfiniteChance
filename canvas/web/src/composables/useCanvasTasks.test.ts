import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest'

import { ApiError, type CanvasTask } from '@infinitechance/api'

import { useCanvasTasks } from '../composables/useCanvasTasks'

const INTERVAL = 100

function task(overrides: Partial<CanvasTask>): CanvasTask {
  return {
    id: 'ct_abc',
    canvas_id: 7,
    node_id: 'image-1-1',
    kind: 'image',
    prompt: 'p',
    model: 'img-m',
    size: '',
    seconds: 0,
    status: 'queued',
    attempts: 1,
    error: '',
    asset_id: 0,
    image_url: '',
    video_url: '',
    created_at: '2026-09-03T12:00:00Z',
    updated_at: '2026-09-03T12:00:00Z',
    ...overrides,
  }
}

/** 不产生副作用的重试/取消替身(用例自身不断言它们的用例共用)。 */
const noopOp = () => Promise.resolve(task({}))

beforeEach(() => {
  vi.useFakeTimers()
})

afterEach(() => {
  vi.useRealTimers()
})

describe('useCanvasTasks', () => {
  it('start 同步一次,没有活动任务就不轮询', async () => {
    const fetchTasks = vi.fn().mockResolvedValue([
      task({ status: 'succeeded', image_url: 'https://img.example/a.png' }),
    ])
    const onTask = vi.fn()
    const tasks = useCanvasTasks({
      fetchTasks,
      retryTask: noopOp,
      cancelTask: noopOp,
      onTask,
      intervalMs: INTERVAL,
    })

    await tasks.start()
    await vi.advanceTimersByTimeAsync(INTERVAL * 5)

    expect(fetchTasks).toHaveBeenCalledTimes(1)
    expect(onTask).toHaveBeenCalledTimes(1)
    expect(tasks.polling).toBe(false)
    expect(tasks.byNode.get('image-1-1')?.status).toBe('succeeded')
  })

  it('有活动任务时按间隔轮询,全部到终态后停止', async () => {
    const statuses: CanvasTask['status'][] = ['queued', 'running', 'succeeded']
    let call = 0
    const fetchTasks = vi.fn().mockImplementation(() => {
      const status = statuses[Math.min(call, statuses.length - 1)]
      call++
      return Promise.resolve([task({ status, image_url: status === 'succeeded' ? 'u' : '' })])
    })
    const onTask = vi.fn()
    const tasks = useCanvasTasks({
      fetchTasks,
      retryTask: noopOp,
      cancelTask: noopOp,
      onTask,
      intervalMs: INTERVAL,
    })

    await tasks.start()
    expect(tasks.polling).toBe(true)

    await vi.advanceTimersByTimeAsync(INTERVAL) // 第 2 轮:running
    expect(fetchTasks).toHaveBeenCalledTimes(2)
    expect(tasks.polling).toBe(true)

    await vi.advanceTimersByTimeAsync(INTERVAL) // 第 3 轮:succeeded → 停
    expect(fetchTasks).toHaveBeenCalledTimes(3)
    await vi.advanceTimersByTimeAsync(INTERVAL * 3)
    expect(fetchTasks).toHaveBeenCalledTimes(3)
    expect(tasks.polling).toBe(false)
  })

  it('retry 回队任务并恢复轮询', async () => {
    const fetchTasks = vi
      .fn()
      .mockResolvedValueOnce([task({ status: 'failed', error: 'boom' })])
      .mockResolvedValue([task({ status: 'running' })])
    const retryTask = vi
      .fn()
      .mockResolvedValue(task({ status: 'queued', error: '', attempts: 2 }))
    const onTask = vi.fn()
    const tasks = useCanvasTasks({
      fetchTasks,
      retryTask,
      cancelTask: noopOp,
      onTask,
      intervalMs: INTERVAL,
    })

    await tasks.start()
    expect(tasks.polling).toBe(false)

    await tasks.retry('ct_abc')
    expect(retryTask).toHaveBeenCalledWith('ct_abc')
    expect(tasks.byNode.get('image-1-1')?.status).toBe('queued')
    expect(tasks.polling).toBe(true)

    await vi.advanceTimersByTimeAsync(INTERVAL)
    expect(fetchTasks).toHaveBeenCalledTimes(2)
  })

  it('stop 之后不再拉取', async () => {
    const fetchTasks = vi.fn().mockResolvedValue([task({})])
    const tasks = useCanvasTasks({
      fetchTasks,
      retryTask: noopOp,
      cancelTask: noopOp,
      onTask: vi.fn(),
      intervalMs: INTERVAL,
    })

    await tasks.start()
    tasks.stop()
    await vi.advanceTimersByTimeAsync(INTERVAL * 5)

    expect(fetchTasks).toHaveBeenCalledTimes(1)
    expect(tasks.polling).toBe(false)
  })

  it('单轮拉取失败不打断轮询', async () => {
    const fetchTasks = vi
      .fn()
      .mockResolvedValueOnce([task({ status: 'running' })])
      .mockRejectedValueOnce(new Error('network down'))
      .mockResolvedValueOnce([task({ status: 'succeeded', image_url: 'u' })])
    const tasks = useCanvasTasks({
      fetchTasks,
      retryTask: noopOp,
      cancelTask: noopOp,
      onTask: vi.fn(),
      intervalMs: INTERVAL,
    })

    await tasks.start()
    await vi.advanceTimersByTimeAsync(INTERVAL) // 失败的一轮
    expect(tasks.polling).toBe(true)
    await vi.advanceTimersByTimeAsync(INTERVAL) // 恢复的一轮
    expect(fetchTasks).toHaveBeenCalledTimes(3)
    await vi.advanceTimersByTimeAsync(INTERVAL * 3)
    expect(tasks.polling).toBe(false)
  })

  it('isRetryConflict 识别 409 冲突', () => {
    const tasks = useCanvasTasks({
      fetchTasks: vi.fn(),
      retryTask: noopOp,
      cancelTask: noopOp,
      onTask: vi.fn(),
    })
    expect(
      tasks.isRetryConflict(new ApiError(409, 'task_not_retryable', '只有失败的任务可以重试')),
    ).toBe(true)
    expect(tasks.isRetryConflict(new ApiError(500, 'internal_error', 'x'))).toBe(false)
    expect(tasks.isRetryConflict(new Error('x'))).toBe(false)
  })
})

describe('useCanvasTasks 取消(12 号票)', () => {
  it('cancel 采纳取消后的任务,终态后轮询停止', async () => {
    const cancelTask = vi
      .fn()
      .mockResolvedValue(task({ kind: 'video', status: 'canceled', attempts: 1 }))
    const onTask = vi.fn()
    const fetchTasks = vi
      .fn()
      .mockResolvedValueOnce([task({ kind: 'video', status: 'running' })])
      .mockResolvedValue([task({ kind: 'video', status: 'canceled' })])
    const tasks = useCanvasTasks({
      fetchTasks,
      retryTask: noopOp,
      cancelTask,
      onTask,
      intervalMs: INTERVAL,
    })

    await tasks.start()
    expect(tasks.polling).toBe(true)

    await tasks.cancel('ct_abc')
    expect(cancelTask).toHaveBeenCalledWith('ct_abc')
    expect(tasks.byNode.get('image-1-1')?.status).toBe('canceled')

    // 取消是终态:下一轮同步发现再无活动任务,轮询就此停止。
    await vi.advanceTimersByTimeAsync(INTERVAL)
    expect(tasks.polling).toBe(false)
    await vi.advanceTimersByTimeAsync(INTERVAL * 3)
    expect(tasks.polling).toBe(false)
  })
})
