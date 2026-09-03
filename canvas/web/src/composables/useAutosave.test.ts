import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest'

import { ApiError } from '@infinitechance/api'

import { useAutosave } from '../composables/useAutosave'

const DELAY = 500

function conflictError(): ApiError {
  return new ApiError(409, 'version_conflict', '画布已在其他窗口被修改')
}

beforeEach(() => {
  vi.useFakeTimers()
})

afterEach(() => {
  vi.useRealTimers()
})

describe('useAutosave', () => {
  it('debounces markDirty and saves once with the current version', async () => {
    const save = vi.fn().mockResolvedValue({ version: 2 })
    const autosave = useAutosave({ delay: DELAY, save })
    autosave.setVersion(1)

    autosave.markDirty()
    autosave.markDirty()
    autosave.markDirty()

    // 防抖窗口内不保存。
    await vi.advanceTimersByTimeAsync(DELAY - 1)
    expect(save).not.toHaveBeenCalled()

    await vi.advanceTimersByTimeAsync(1)
    expect(save).toHaveBeenCalledTimes(1)
    expect(save).toHaveBeenCalledWith(1)
    expect(autosave.state.value).toBe('saved')
    expect(autosave.version.value).toBe(2)
  })

  it('uses the version returned by the previous save for the next one', async () => {
    const save = vi
      .fn()
      .mockResolvedValueOnce({ version: 2 })
      .mockResolvedValueOnce({ version: 3 })
    const autosave = useAutosave({ delay: DELAY, save })
    autosave.setVersion(1)

    autosave.markDirty()
    await vi.advanceTimersByTimeAsync(DELAY)
    autosave.markDirty()
    await vi.advanceTimersByTimeAsync(DELAY)

    expect(save).toHaveBeenNthCalledWith(1, 1)
    expect(save).toHaveBeenNthCalledWith(2, 2)
    expect(autosave.version.value).toBe(3)
  })

  it('marks dirty while a save is in flight and saves again afterwards', async () => {
    let release!: (value: { version: number }) => void
    const save = vi.fn().mockImplementation(
      () =>
        new Promise<{ version: number }>((resolve) => {
          release = resolve
        }),
    )
    const autosave = useAutosave({ delay: DELAY, save })
    autosave.setVersion(1)

    autosave.markDirty()
    await vi.advanceTimersByTimeAsync(DELAY)
    expect(autosave.state.value).toBe('saving')

    // 保存进行中的编辑:完成后必须再排一轮,不能丢。
    autosave.markDirty()
    release({ version: 2 })
    await vi.advanceTimersByTimeAsync(0)
    expect(autosave.state.value).toBe('dirty')

    await vi.advanceTimersByTimeAsync(DELAY)
    expect(save).toHaveBeenCalledTimes(2)
    expect(save).toHaveBeenLastCalledWith(2)
  })

  it('enters conflict on 409 version_conflict and stops autosaving', async () => {
    const save = vi.fn().mockRejectedValue(conflictError())
    const autosave = useAutosave({ delay: DELAY, save })
    autosave.setVersion(7)

    autosave.markDirty()
    await vi.advanceTimersByTimeAsync(DELAY)
    expect(autosave.state.value).toBe('conflict')
    expect(save).toHaveBeenCalledTimes(1)

    // 冲突后编辑不再触发保存:必须先由用户解决冲突(重载/覆盖)。
    autosave.markDirty()
    await vi.advanceTimersByTimeAsync(DELAY * 10)
    expect(save).toHaveBeenCalledTimes(1)
  })

  it('retries automatically after a non-conflict failure', async () => {
    const save = vi
      .fn()
      .mockRejectedValueOnce(new Error('network down'))
      .mockResolvedValueOnce({ version: 2 })
    const autosave = useAutosave({ delay: DELAY, save })
    autosave.setVersion(1)

    autosave.markDirty()
    await vi.advanceTimersByTimeAsync(DELAY)
    expect(autosave.state.value).toBe('error')

    // 网络类失败按退避自动重试,版本不变。
    await vi.advanceTimersByTimeAsync(DELAY * 8)
    expect(save).toHaveBeenCalledTimes(2)
    expect(save).toHaveBeenLastCalledWith(1)
    expect(autosave.state.value).toBe('saved')
  })

  it('setVersion cancels pending saves and returns to idle', async () => {
    const save = vi.fn().mockResolvedValue({ version: 9 })
    const autosave = useAutosave({ delay: DELAY, save })

    autosave.markDirty()
    autosave.setVersion(3)
    await vi.advanceTimersByTimeAsync(DELAY * 10)

    expect(save).not.toHaveBeenCalled()
    expect(autosave.state.value).toBe('idle')
    expect(autosave.version.value).toBe(3)
  })
})
