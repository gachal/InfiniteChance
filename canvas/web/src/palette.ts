/**
 * 角色配色段(84 号票 B2/B3):cut 直通(画面 + 视频无画面)把出场角色
 * 绑定变体的配色拼进提示词。固定标记段「角色配色:」—— 单角色单行
 * (风格段单行形态),多角色 = 标记行 + 逐角色一行「<角色名>:<配色文本>」;
 * 无配色角色跳过不占行(票面 B2)。
 *
 * 数据源 = cut.character_ids × 83 号票绑定变体解析(resolveCutVariant:
 * 绑定优先 → 默认变体回落),与参考图挑选(pickCutCharacterRefs)同源
 * 解析、但互不依赖 —— 配色注入与参考图注入是两件事,无图角色的配色照拼。
 * palette 是变体级自由文本(≤500 runes 在服务端拒绝),不做格式解析。
 *
 * 段位:配色行进 storyboard.buildCut*Prompt 的 sections 首位,风格段由
 * prependStyleSegment 顶到最前 —— 最终序 = 画面风格 → 角色配色 → 全片
 * 基调 → …;发送链里绑定行(「出场元素:」)经 insertElementLine 插在
 * 风格段之后、配色段之前,即票面「配色段插在绑定行之后」。
 *
 * 防叠显式化(84 号票 B2 评审修复轮):票面「prompt 已含同段不叠段
 * (照 ipStyle.ts 防叠)」落地为拼装口 stripPaletteSegment /
 * prependPaletteSegment —— ipStyle.prependStyleSegment 是「已含同段不
 * 重复拼」(includes 检查,风格文本不随重拼变),配色段是「剥旧拼新」
 * (重拼时变体配色可能已改,includes 跳过会留旧值,必须剥)。storyboard
 * 的 buildCut*Prompt 出口统一经 prependPaletteSegment 拼段,「同段不叠」
 * 成为拼装函数自身的性质而非调用方纪律。
 *
 * 服务端对照:canvas 侧 BuildSheetPrompt(internal/canvastask/iptask.go,
 * 84 号票 B1 配色行)是请求体事实的全新拼装、不消费前端预填文本,无旧
 * 段可叠 —— 不需要等价物;两侧语义以本注释钉住。
 */
import type { CharacterRecord } from '@infinitechance/api'

import { resolveCutVariant } from './sheet'

/** 配色段固定标记(不用【】,避开厂商字幕渲染风险;单行形态:
 * 「角色配色:小蓝:发色:#1A2E;…」)。 */
export const PALETTE_SEGMENT_MARK = '角色配色:'

/** 一个出场角色的配色条目(名字实时取目录,不存快照 —— 52 号票哲学)。 */
export interface PaletteEntry {
  name: string
  palette: string
}

/** 逐出场角色解析配色(83 号票绑定变体优先 → 默认变体回落):角色不在
 * 目录、无任何变体、或解析到的变体配色空白 → 跳过不占行。characterVariants
 * 即 cut.character_variants 绑定映射,可缺省(存量画布路径回落默认变体)。 */
export function buildPaletteEntries(
  characters: CharacterRecord[],
  characterIds: number[],
  characterVariants?: Record<number, number>,
): PaletteEntry[] {
  const entries: PaletteEntry[] = []
  for (const id of characterIds) {
    const c = characters.find((x) => x.id === id)
    if (!c) {
      continue
    }
    const { variant } = resolveCutVariant(c, characterVariants?.[id])
    const palette = (variant?.palette ?? '').trim()
    if (palette === '') {
      continue
    }
    entries.push({ name: c.name, palette })
  }
  return entries
}

/** 配色行构建:单角色单行(风格段单行形态);多角色 = 标记行 + 逐角色
 * 一行;无条目 = 空串(调用方整段跳过)。段不设总截断:单角色 ≤500 runes
 * 已在服务端限死,行数随出场角色数(票面 B4,单用户自用形态可接受)。 */
export function buildPaletteLine(entries: readonly PaletteEntry[]): string {
  if (entries.length === 0) {
    return ''
  }
  if (entries.length === 1) {
    return `${PALETTE_SEGMENT_MARK}${entries[0].name}:${entries[0].palette}`
  }
  const lines = entries.map((e) => `${e.name}:${e.palette}`)
  return `${PALETTE_SEGMENT_MARK}\n${lines.join('\n')}`
}

/**
 * 剥掉文本里已存在的全部「角色配色」段(84 号票 B2 防叠显式化,评审修复
 * 轮):段形态与 buildPaletteLine 逐字对应 ——
 * - 单角色:一行「角色配色:<名>:<配色>」,整行剥;
 * - 多角色:纯标记行「角色配色:」+ 紧随的连续内容行,整块剥;内容行的
 *   终止 = 空行(终止并保留)/ 另一个段标记行(转入新段继续剥)/
 *   sectionMarks 命中行(终止并保留 —— 拼装产物里段后必接下一 section)。
 *
 * sectionMarks 由拼装面传入(storyboard 的 section 标记清单):palette 不
 * 反向 import 拼装面(会循环依赖),段边界由拼装面声明,两侧字面量测试
 * 各自钉住(storyboard.test 的段序钉子 + palette.test 的清单用例)。空清单
 * (缺省)时只按空行/段标记终止。空行保留,段外文本不动。纯函数,不改
 * 入参。
 */
export function stripPaletteSegment(text: string, sectionMarks: readonly string[] = []): string {
  const kept: string[] = []
  let inSegment = false
  for (const raw of text.split('\n')) {
    const line = raw.trim()
    if (line.startsWith(PALETTE_SEGMENT_MARK)) {
      // 段标记行必剥;标记后带内容 = 单角色形态,段即此一行。
      inSegment = line.slice(PALETTE_SEGMENT_MARK.length).trim() === ''
      continue
    }
    if (inSegment) {
      if (line === '') {
        inSegment = false
        kept.push(raw)
        continue
      }
      if (sectionMarks.some((m) => line.startsWith(m))) {
        inSegment = false
        kept.push(raw)
        continue
      }
      continue // 段内容行:剥
    }
    kept.push(raw)
  }
  return kept.join('\n')
}

/** 配色段拼进提示词前部(84 号票 B2;评审修复轮防叠显式化):line 空
 * (trim 后)= 无段,原样返回;否则先剥掉 body 里已存在的配色段
 * (stripPaletteSegment,「剥旧拼新」—— 重拼时变体配色可能已改,旧值
 * 让位;与 ipStyle.prependStyleSegment 的 includes 防叠同源纪律,选
 * 「剥」不选「跳」的原因见 strip 注释),再把 line 拼到最前 —— 即票面
 * 「配色段 sections 首位」(风格段随后由 prependStyleSegment 顶到最前)。
 * body 由 storyboard 的 sections 全新拼装、契约上无段,剥段是拼装函数
 * 自身的防叠性质而非对调用方纪律的依赖。纯函数,不改入参。 */
export function prependPaletteSegment(
  body: string,
  line: string,
  sectionMarks: readonly string[] = [],
): string {
  if (line.trim() === '') {
    return body
  }
  const stripped = stripPaletteSegment(body, sectionMarks)
  return stripped === '' ? line : `${line}\n${stripped}`
}
