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
 * 风格段之后、配色段之前,即票面「配色段插在绑定行之后」。直通预填是
 * 覆盖写草稿,同段不叠由拼装语义保证(照 ipStyle 防叠纪律的等价面)。
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
