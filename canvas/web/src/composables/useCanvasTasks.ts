/**
 * 画布生成任务的客户端状态(10 号票,12 号票追加取消):轮询任务列表、
 * 把任务状态同步到绑定节点、失败任务的原地重试、进行中视频任务的取消。
 * 任务由服务端 worker 编排 —— 浏览器关掉任务照跑,这里只负责「看」:
 * 有活动任务时轮询,全部到终态就停。
 */
import { computed, reactive } from 'vue'

import { ApiError, type CanvasTask } from '@infinitechance/api'

export interface CanvasTasksOptions {
  /** 拉取任务列表(通常就是 ApiClient.listCanvasTasks 的偏函数)。 */
  fetchTasks: () => Promise<CanvasTask[]>
  /** 重试失败任务,返回回队后的任务。 */
  retryTask: (taskId: string) => Promise<CanvasTask>
  /** 取消进行中的任务(网关侧同步取消并退预扣),返回取消后的任务。 */
  cancelTask: (taskId: string) => Promise<CanvasTask>
  /** 每次任务状态更新后回调(编辑器据此把产物写进节点数据)。 */
  onTask: (task: CanvasTask) => void
  /** 轮询间隔毫秒数(测试用)。 */
  intervalMs?: number
}

const DEFAULT_INTERVAL_MS = 1500

export function useCanvasTasks(options: CanvasTasksOptions) {
  const intervalMs = options.intervalMs ?? DEFAULT_INTERVAL_MS

  /** node_id → 最新任务快照:节点组件按它渲染排队/生成中/失败与重试。 */
  const byNode = reactive(new Map<string, CanvasTask>())

  let timer: ReturnType<typeof setTimeout> | null = null
  let polling = false
  /** stop 之后不得再排下一轮(编辑器已卸载)。 */
  let stopRequested = false

  const activeCount = computed(
    () =>
      [...byNode.values()].filter(
        (t) => t.status === 'queued' || t.status === 'running',
      ).length,
  )

  async function sync(): Promise<void> {
    const tasks = await options.fetchTasks()
    for (const task of tasks) {
      byNode.set(task.node_id, task)
      options.onTask(task)
    }
  }

  function schedule(): void {
    if (stopRequested || timer !== null) {
      return
    }
    timer = setTimeout(() => {
      timer = null
      void sync()
        .then(() => {
          if (stopRequested) {
            return
          }
          if (activeCount.value > 0) {
            schedule()
          } else {
            polling = false
          }
        })
        .catch(() => {
          // 单轮失败不打断轮询:下一轮继续拉(网络抖动不该让节点卡死)。
          if (!stopRequested) {
            polling = true
            schedule()
          }
        })
    }, intervalMs)
  }

  /** 拉一次任务并在有活动任务时保持轮询。编辑器加载与提交任务后调用。 */
  async function start(): Promise<void> {
    stopRequested = false
    await sync()
    if (activeCount.value > 0) {
      polling = true
      schedule()
    }
  }

  /** 组件卸载时停表。 */
  function stop(): void {
    stopRequested = true
    if (timer !== null) {
      clearTimeout(timer)
      timer = null
    }
    polling = false
  }

  /** 纳入跟踪并保持轮询(track 与 retry 的共同收尾)。 */
  function adopt(task: CanvasTask): void {
    byNode.set(task.node_id, task)
    options.onTask(task)
    polling = true
    schedule()
  }

  /** 提交任务后立即纳入跟踪:同步快照并保持轮询,不等下一轮拉取。 */
  function track(task: CanvasTask): void {
    adopt(task)
  }

  /** 失败任务的原地重试:同一任务回队,成功后恢复轮询。 */
  async function retry(taskId: string): Promise<void> {
    adopt(await options.retryTask(taskId))
  }

  /** 进行中任务的原地取消:服务端同步取消网关任务并退预扣,这里采纳
   * 回包(canceled 是终态,不再有活动任务时轮询自然停止)。 */
  async function cancel(taskId: string): Promise<void> {
    adopt(await options.cancelTask(taskId))
  }

  /** 409 = 任务不在失败态(多半已被重试),UI 据此静默刷新而非报错。 */
  function isRetryConflict(e: unknown): e is ApiError {
    return e instanceof ApiError && e.status === 409
  }

  return {
    byNode,
    activeCount,
    get polling() {
      return polling
    },
    start,
    stop,
    track,
    retry,
    cancel,
    isRetryConflict,
  }
}
