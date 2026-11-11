/**
 * 角色配色段纯逻辑测试(84 号票 B2/B3):固定标记段、单/多角色两种形态、
 * 无配色跳过、绑定变体解析与参考图挑选同源(resolveCutVariant:绑定优先
 * → 默认变体回落)。
 */
import { describe, expect, it } from 'vitest'

import type { CharacterRecord, CharacterVariantRecord } from '@infinitechance/api'

import {
  PALETTE_SEGMENT_MARK,
  buildPaletteEntries,
  buildPaletteLine,
  prependPaletteSegment,
  stripPaletteSegment,
} from './palette'

// ---- 测试脚手架(sheet.test.ts 同款轻量对象,不依赖 api 包运行时)----

function variant(id: number, palette = ''): CharacterVariantRecord {
  return {
    id,
    character_id: 1,
    name: '默认',
    note: '',
    palette,
    sort: 0,
    images: [],
    created_at: '',
    updated_at: '',
  }
}

function character(id: number, name: string, variants: CharacterVariantRecord[]): CharacterRecord {
  return {
    id,
    ip_id: 1,
    name,
    alias: '',
    description: '',
    detail: '',
    cover_asset_id: 0,
    voice_id: 0,
    variants,
    created_at: '',
    updated_at: '',
  }
}

const HEX_BLUE = '发色:#1A2E;肤色:#FFE0D0;瞳色:#4A90D9;线色:#2B2B2B'
const HEX_RED = '发色:#C0392B;服装主色:#8E2A22'

describe('buildPaletteEntries(84 号票:绑定变体的配色解析)', () => {
  it('按 character_ids 序解析绑定变体配色;无配色角色跳过不占行', () => {
    const chars = [
      character(1, '小蓝', [variant(11, HEX_BLUE)]),
      character(2, '阿红', [variant(12, '')]), // 无配色:跳过
      character(3, '小雪', [variant(13, HEX_RED)]),
    ]
    expect(buildPaletteEntries(chars, [1, 2, 3])).toEqual([
      { name: '小蓝', palette: HEX_BLUE },
      { name: '小雪', palette: HEX_RED },
    ])
  })

  it('绑定变体优先:取绑定变体的配色而非默认变体', () => {
    const def = variant(11, HEX_BLUE)
    const rain = { ...variant(12, HEX_RED), name: '雨衣' }
    const chars = [character(1, '小蓝', [def, rain])]
    expect(buildPaletteEntries(chars, [1], { 1: 12 })).toEqual([
      { name: '小蓝', palette: HEX_RED },
    ])
  })

  it('绑定变体已删:回落默认变体配色(resolveCutVariant 同源)', () => {
    const def = variant(11, HEX_BLUE)
    const chars = [character(1, '小蓝', [def])]
    expect(buildPaletteEntries(chars, [1], { 1: 99 })).toEqual([
      { name: '小蓝', palette: HEX_BLUE },
    ])
  })

  it('缺映射/无字段(存量画布):默认变体配色,行为与旧契约一致', () => {
    const def = variant(11, HEX_BLUE)
    const w = { ...variant(12, HEX_RED), name: '冬装' }
    const chars = [character(1, '小蓝', [w, def])]
    const want = [{ name: '小蓝', palette: HEX_BLUE }]
    expect(buildPaletteEntries(chars, [1])).toEqual(want)
    expect(buildPaletteEntries(chars, [1], undefined)).toEqual(want)
    expect(buildPaletteEntries(chars, [1], {})).toEqual(want)
    expect(buildPaletteEntries(chars, [1], { 2: 12 })).toEqual(want)
  })

  it('角色不在目录、角色无任何变体、配色全空白:各自跳过', () => {
    const chars = [character(1, '小蓝', [])]
    expect(buildPaletteEntries(chars, [404, 1])).toEqual([])
    expect(buildPaletteEntries([character(2, '阿红', [variant(21, '  \n  ')])], [2])).toEqual([])
  })
})

describe('buildPaletteLine(单角色单行 / 多角色标记行 + 逐角色一行)', () => {
  it('无条目 = 空串(调用方不拼段,行为不变)', () => {
    expect(buildPaletteLine([])).toBe('')
  })

  it('单角色:单行「角色配色:<角色名>:<配色文本>」(风格段单行形态)', () => {
    expect(buildPaletteLine([{ name: '小蓝', palette: HEX_BLUE }])).toBe(
      `角色配色:小蓝:${HEX_BLUE}`,
    )
  })

  it('多角色:标记行「角色配色:」+ 逐角色一行', () => {
    expect(
      buildPaletteLine([
        { name: '小蓝', palette: HEX_BLUE },
        { name: '阿红', palette: HEX_RED },
      ]),
    ).toBe(`角色配色:\n小蓝:${HEX_BLUE}\n阿红:${HEX_RED}`)
  })

  it('段标记逐字钉死(与注释、测试三方一致;不用【】避开字幕渲染)', () => {
    expect(PALETTE_SEGMENT_MARK).toBe('角色配色:')
  })
})

// ---- 84 号票 B2 防叠显式化(评审修复轮):拼装前剥掉已存在的段 ----
// 边界清单 = storyboard 拼装面的 section 标记(与 CUT_SECTION_MARKS 同源,
// 测试里按字面量复制;漂移由两侧字面量测试对拍)。

const SECTION_MARKS = [
  '全片基调:',
  '画面描述:',
  '景别:',
  '运镜:',
  '台词/音效:',
  '高清,细节丰富,电影质感,色彩自然;保持无字幕,不要生成Logo和水印',
]

describe('stripPaletteSegment(84-B2 防叠:剥掉已存在的配色段)', () => {
  const TAIL = SECTION_MARKS[5]

  it('已有段被剥:单角色单行段整行剥掉,段外文本保留', () => {
    const line = `角色配色:小蓝:${HEX_BLUE}`
    expect(stripPaletteSegment(`全片基调:夜\n${line}\n画面描述:巷口`, SECTION_MARKS)).toBe(
      '全片基调:夜\n画面描述:巷口',
    )
  })

  it('多行段全剥:纯标记行 + 逐角色行整块消失,后续 section 保留', () => {
    const line = `角色配色:\n小蓝:${HEX_BLUE}\n阿红:${HEX_RED}`
    expect(stripPaletteSegment(`${line}\n全片基调:夜\n${TAIL}`, SECTION_MARKS)).toBe(
      `全片基调:夜\n${TAIL}`,
    )
  })

  it('无段不动:原样返回(带清单与缺省清单一致)', () => {
    const text = `全片基调:夜\n画面描述:巷口\n${TAIL}`
    expect(stripPaletteSegment(text, SECTION_MARKS)).toBe(text)
    expect(stripPaletteSegment(text)).toBe(text)
  })

  it('叠段防御:两份段(单行 + 多行形态)全部剥掉', () => {
    const text = `角色配色:小蓝:${HEX_BLUE}\n角色配色:\n阿红:${HEX_RED}\n画面描述:巷口`
    expect(stripPaletteSegment(text, SECTION_MARKS)).toBe('画面描述:巷口')
  })

  it('纯段文本:剥净为空串(重拼前置形态)', () => {
    expect(stripPaletteSegment(`角色配色:\n小蓝:${HEX_BLUE}`, SECTION_MARKS)).toBe('')
  })
})

describe('prependPaletteSegment(84-B2 防叠拼装口:剥旧拼新)', () => {
  it('body 无段:line 拼最前(与旧 sections 首位 push 行为逐字节一致)', () => {
    const body = `全片基调:夜\n画面描述:巷口`
    const line = `角色配色:小蓝:${HEX_BLUE}`
    expect(prependPaletteSegment(body, line, SECTION_MARKS)).toBe(`${line}\n${body}`)
  })

  it('body 已含段:剥旧段、新段拼最前(重拼时配色可能已改,旧值让位)', () => {
    const oldLine = `角色配色:\n小蓝:${HEX_BLUE}`
    const newLine = `角色配色:小蓝:${HEX_RED}`
    expect(prependPaletteSegment(`${oldLine}\n全片基调:夜`, newLine, SECTION_MARKS)).toBe(
      `${newLine}\n全片基调:夜`,
    )
  })

  it('line 空:原样返回(整段跳过,行为不变)', () => {
    const body = `全片基调:夜\n画面描述:巷口`
    expect(prependPaletteSegment(body, '', SECTION_MARKS)).toBe(body)
    expect(prependPaletteSegment(body, '   ', SECTION_MARKS)).toBe(body)
  })
})
