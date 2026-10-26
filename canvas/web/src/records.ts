/**
 * 生成记录面板的纯逻辑(28 号票):任务流水行的视图判定 —— 缩略图地址、
 * 可否插入、状态/种类文案、提示词截断、时间格式化。组件只做渲染与事件,
 * 这里集中可单测的判定。地址规则沿用 composer.ts 的 taskUrlFor(素材内容
 * 寻址优先,厂商地址回落)。
 */
import type { CanvasTask } from '@infinitechance/api'

import { taskUrlFor } from './composer'

/** 面板数据零后端改动(28 号票定案):任务端点无分页、上限 200,面板顶部
 * 的截断提示与这里保持同一个数字。 */
export const RECORDS_LIMIT = 200

/** 行缩略图地址:成功行走素材内容寻址(无素材行回落厂商临时地址),非成功
 * 态由渲染层显示 kind 图标徽章。 */
export function recordThumbURL(task: CanvasTask): string {
  return taskUrlFor(task)
}

/** 行内「插入画布」是否出现:成功且有素材引用(插入语义 = asset_id + 内容
 * 寻址,复用素材面板;素材行已被删的行由渲染层的加载失败态收走按钮)。
 * voice 行除外(57 号票):成功行的素材是试听音频,产物本体在台账 ——
 * 插入画布的入口不出现(再建走节点表单)。 */
export function canInsertRecord(task: CanvasTask): boolean {
  return task.status === 'succeeded' && task.asset_id > 0 && task.kind !== 'voice'
}

/** 任务状态的中文文案(记录面板行内;节点卡片有自己的态文案)。 */
export function recordStatusLabel(status: CanvasTask['status']): string {
  switch (status) {
    case 'queued':
      return '排队中'
    case 'running':
      return '生成中'
    case 'succeeded':
      return '已成功'
    case 'failed':
      return '失败'
    case 'canceled':
      return '已取消'
  }
}

/** 任务种类的徽章文案(57 号票四元:音色闭环的 voice/tts 加入)。 */
export function recordKindLabel(kind: string): string {
  switch (kind) {
    case 'video':
      return '视频'
    case 'voice':
      return '音色'
    case 'tts':
      return '台词'
    default:
      return '图片'
  }
}

/** 音频行(57 号票):kind=tts 的成功行有 kind=audio 素材产物,行内直接
 * 给 <audio> 试听(素材面板同款;voice 行的产物在台账试听,不走这里)。 */
export function isAudioRecord(task: CanvasTask): boolean {
  return task.kind === 'tts' && task.status === 'succeeded' && task.asset_id > 0
}

/** 提示词单行截断:超上限裁到上限并以省略号结尾;空提示词给占位。全文由
 * 行内展开显示,这里只管收起态的展示串。 */
export function truncatePrompt(prompt: string, limit: number): string {
  const text = prompt.trim()
  if (text === '') {
    return '(无提示词)'
  }
  if (text.length <= limit) {
    return text
  }
  return `${text.slice(0, limit - 1)}…`
}

/** 面板行的时间列:月-日 时:分(记录面板只看先后,不强调年份);解析失败
 * 原样返回,坏数据不至于把整列渲染成 Invalid Date。 */
export function formatRecordTime(iso: string): string {
  if (iso === '') {
    return iso
  }
  const d = new Date(iso)
  if (Number.isNaN(d.getTime())) {
    return iso
  }
  const pad = (n: number) => String(n).padStart(2, '0')
  return `${pad(d.getMonth() + 1)}-${pad(d.getDate())} ${pad(d.getHours())}:${pad(d.getMinutes())}`
}
