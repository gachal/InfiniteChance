/**
 * 剧本节点的纯逻辑(64 号票):初始 data、30 万字上限校验、卡面折叠摘要。
 * 组件只做渲染与交互,这里集中可单测的判定 —— 与 storyboard.ts / name.ts /
 * level.ts 同族(整图 JSON 随节点 data,自动保存免费获得)。一个类型三种
 * 用途(分集大纲/每集剧本/原文),用途区分靠 title;场次用 markdown 场头
 * 约定(如「【第N场|内|日|地点】」),不做结构化场次编辑 UI(票面定案)。
 */
import type { ScriptNodeData } from './graph'

/** 新剧本节点的初始 data:title 空(展示层回落「剧本」)+ 空正文。 */
export function defaultScript(): ScriptNodeData {
  return { title: '', text: '' }
}

/** 正文上限 30 万字(64 号票):按 UTF-16 code unit 计(JS string.length
 * 原生口径)。校验是产品口径不是存储护栏 —— 30 万 CJK 字 ≈ 0.9MB,加
 * JSON 序列化仍远低于后端整图 4MiB 上限;大文本 autosave 全量提交是
 * 64 号票已接受的代价,不在此优化。 */
export const MAX_SCRIPT_CHARS = 300_000

/** 正文字数(UTF-16 code unit,与 MAX_SCRIPT_CHARS 同口径)。 */
export function scriptCharCount(text: string): number {
  return text.length
}

/** 正文是否在上限内(前端校验唯一判据,超出提示分段粘贴)。 */
export function isScriptTextWithinLimit(text: string): boolean {
  return text.length <= MAX_SCRIPT_CHARS
}

/** 超出上限的字数(未超为 0):卡面/编辑层的超限提示共用。 */
export function scriptExcessChars(text: string): number {
  return Math.max(0, text.length - MAX_SCRIPT_CHARS)
}

/** 超限提示文案(空串 = 未超):「分段粘贴」是票面给的用户出路 —— 不做
 * 硬截断(打断 IME 组词/粘贴流),输入保持可用、由提示引导。 */
export function scriptLimitNotice(text: string): string {
  const excess = scriptExcessChars(text)
  if (excess === 0) {
    return ''
  }
  return `正文超出 ${MAX_SCRIPT_CHARS} 字上限 ${excess} 字,请分段粘贴`
}

/** 卡面摘要行的截断宽度:340px 卡宽下一行的舒适余量。 */
export const SCRIPT_SUMMARY_CHARS = 40

/** 卡面折叠摘要行:正文首个非空行(trim 后),超长截断加省略号;
 * 无正文返回空串(占位由组件渲染)。 */
export function scriptSummaryLine(text: string): string {
  const line = text
    .split('\n')
    .map((l) => l.trim())
    .find((l) => l !== '')
  if (line === undefined) {
    return ''
  }
  return line.length > SCRIPT_SUMMARY_CHARS
    ? line.slice(0, SCRIPT_SUMMARY_CHARS) + '…'
    : line
}

/** 卡面标题:title 空白回落「剧本」(三种用途靠 title 区分,新节点未
 * 命名时用类型名兜底)。 */
export function scriptCardTitle(title: string | undefined): string {
  const trimmed = title?.trim() ?? ''
  return trimmed === '' ? '剧本' : trimmed
}

/** 字数展示:不足 1 万显原数,达到 1 万起以「万」计(卡面与编辑层共用)。 */
export function scriptCharCountLabel(text: string): string {
  const count = scriptCharCount(text)
  if (count < 10_000) {
    return `${count} 字`
  }
  return `${(count / 10_000).toFixed(1)} 万字`
}
