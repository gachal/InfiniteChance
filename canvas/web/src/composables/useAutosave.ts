/**
 * 画布自动保存:防抖提交 + 版本号乐观锁的客户端状态机(09 号票)。
 *
 * - markDirty:图发生变化(拖动、连线、改内容),重置防抖计时。
 * - 计时到期后用当前版本调用 save,响应里的新版本成为下一次的期望版本。
 * - 409 version_conflict:另一个窗口先保存了 → 进入 conflict 态并停摆,
 *   由用户在编辑器里选择「加载服务器版本」或「以我的版本覆盖」后恢复;
 *   盲目自动重试只会反复撞锁,所以这里必须停下来。
 * - 其他失败(网络/5xx):保留脏标记,按退避自动重试。
 */
import { readonly, ref } from 'vue'

import { ApiError } from '@infinitechance/api'

export type AutosaveState = 'idle' | 'dirty' | 'saving' | 'saved' | 'error' | 'conflict'

export interface AutosaveOptions {
  /** 防抖毫秒数。 */
  delay?: number
  /** 执行保存:入参是当前已知版本,返回服务器确认的新版本。 */
  save: (expectedVersion: number) => Promise<{ version: number }>
}

const DEFAULT_DELAY = 800
/** 网络类失败的自动重试间隔 = delay 的倍数,避免高频空转。 */
const RETRY_MULTIPLIER = 8

export function useAutosave(options: AutosaveOptions) {
  const delay = options.delay ?? DEFAULT_DELAY

  const state = ref<AutosaveState>('idle')
  const version = ref(0)

  let timer: ReturnType<typeof setTimeout> | null = null
  let inFlight = false
  let pendingAfterFlight = false
  /** 等待在途保存结束的调用方(flush)。 */
  let flightWaiters: Array<() => void> = []

  function clearTimer(): void {
    if (timer !== null) {
      clearTimeout(timer)
      timer = null
    }
  }

  function armTimer(withDelay: number): void {
    clearTimer()
    timer = setTimeout(() => {
      timer = null
      void persist()
    }, withDelay)
  }

  async function persist(): Promise<void> {
    inFlight = true
    state.value = 'saving'
    try {
      const result = await options.save(version.value)
      version.value = result.version
      state.value = 'saved'
    } catch (e) {
      if (e instanceof ApiError && e.status === 409 && e.code === 'version_conflict') {
        state.value = 'conflict'
      } else {
        // 保持 error 态直到重试成功:期间 UI 能看见失败原因。
        armTimer(delay * RETRY_MULTIPLIER)
        state.value = 'error'
      }
    } finally {
      inFlight = false
      if (pendingAfterFlight && state.value !== 'conflict' && state.value !== 'error') {
        pendingAfterFlight = false
        armTimer(delay)
        state.value = 'dirty'
      }
      const waiters = flightWaiters
      flightWaiters = []
      for (const resolve of waiters) {
        resolve()
      }
    }
  }

  /**
   * 图发生了变化:安排一次防抖保存。两类空操作:
   * - conflict 态:必须先由用户解决冲突,盲目重试只会反复撞锁;
   * - 尚无已确认版本(version < 1):编辑器还没加载完成,
   *   水合触发的变更事件不算编辑。
   */
  function markDirty(): void {
    if (state.value === 'conflict' || version.value < 1) {
      return
    }
    if (inFlight) {
      pendingAfterFlight = true
      return
    }
    armTimer(delay)
    state.value = 'dirty'
  }

  /** 以服务器版本为权威:取消挂起的保存并回到 idle。加载/冲突重载后调用。 */
  function setVersion(confirmed: number): void {
    clearTimer()
    pendingAfterFlight = false
    version.value = confirmed
    state.value = 'idle'
  }

  /**
   * 立即保存未落库的变更(跳过防抖),返回保存是否已确认。生成动作在
   * 提交任务前调用:结果节点必须先于任务持久化,浏览器随后关闭也不丢。
   * conflict 态(或尚未加载完成)不做保存并返回 false。
   */
  async function flush(): Promise<boolean> {
    if (version.value < 1) {
      return false
    }
    clearTimer()
    await whenIdle()
    clearTimer() // 在途保存的收尾可能又排了防抖,一并取消
    // await 之后状态可能已被在途保存改写,重新读取而不是沿用前面的收窄。
    const current: AutosaveState = state.value
    if (current === 'conflict' || version.value < 1) {
      return false
    }
    if (current === 'dirty' || current === 'error' || pendingAfterFlight) {
      pendingAfterFlight = false
      await persist()
    }
    return state.value === 'saved'
  }

  /** 在途保存存在时等它结束;否则立即返回。 */
  function whenIdle(): Promise<void> {
    if (!inFlight) {
      return Promise.resolve()
    }
    return new Promise((resolve) => {
      flightWaiters.push(resolve)
    })
  }

  return {
    state: readonly(state),
    version: readonly(version),
    markDirty,
    setVersion,
    flush,
  }
}
