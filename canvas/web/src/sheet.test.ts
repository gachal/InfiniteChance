/**
 * 角色设定图生成的纯逻辑测试(70 号票):五类型模板字面量钉子(与服务端
 * internal/canvastask/iptask.go 同组字面量,两侧各自钉住防漂移)、prompt
 * 拼装口径、B3 参考图默认、cut 出场角色参考挑选(C)。
 */
import { describe, expect, it } from 'vitest'

import type { CharacterRecord, CharacterVariantRecord } from '@infinitechance/api'

import {
  MAX_SHEET_PROMPT_RUNES,
  SHEET_TYPES,
  SHEET_TYPE_TEMPLATES,
  buildSheetPrompt,
  latestVariantImage,
  pickCutCharacterRefs,
} from './sheet'

describe('五类型模板字面量钉子(与服务端同字面量)', () => {
  it('五类型固定序,默认三视图', () => {
    expect(SHEET_TYPES).toEqual(['三视图', '多视图', '表情集', '色板', '姿态'])
  })

  it('模板文案与服务端 iptask.go 逐字一致', () => {
    expect(SHEET_TYPE_TEMPLATES['三视图']).toBe(
      '角色设定图·三视图:同一角色的全身站姿正面、侧面、背面三视图对照,白底,全身完整无裁切,各视角比例一致,标准设定图画风',
    )
    expect(SHEET_TYPE_TEMPLATES['多视图']).toBe(
      '角色设定图·多视图:同一角色的全身多角度视图对照(正面、四分之三、侧面、背面),白底,各视角比例一致,标准设定图画风',
    )
    expect(SHEET_TYPE_TEMPLATES['表情集']).toBe(
      '角色设定图·表情集:同一角色头部的多种表情对照(喜、怒、哀、惊等),白底,构图整齐,表情差异清晰,标准设定图画风',
    )
    expect(SHEET_TYPE_TEMPLATES['色板']).toBe(
      '角色设定图·色板:该角色的配色色板展示(发色、肤色、服装主色与辅色色块并列),白底,标注干净,标准设定图画风',
    )
    expect(SHEET_TYPE_TEMPLATES['姿态']).toBe(
      '角色设定图·姿态集:同一角色的多个典型动作与姿态对照,全身,白底,姿态自然清晰,标准设定图画风',
    )
  })
})

describe('buildSheetPrompt(与 Go BuildSheetPrompt 同形)', () => {
  it('模板 + 四件套 + 变体名/服装,空字段整行跳过', () => {
    const got = buildSheetPrompt('三视图', {
      name: '林小满',
      alias: '小满',
      description: '爱冒险的少女',
      detail: '十六岁,黑色短发',
      variantName: '冬季制服',
      variantNote: '深蓝色大衣配红色围巾',
    }, '')
    expect(got).toBe([
      '角色设定图·三视图:同一角色的全身站姿正面、侧面、背面三视图对照,白底,全身完整无裁切,各视角比例一致,标准设定图画风',
      '角色:林小满',
      '别名:小满',
      '简介:爱冒险的少女',
      '详细设定:十六岁,黑色短发',
      '造型:冬季制服',
      '服装:深蓝色大衣配红色围巾',
    ].join('\n'))
  })

  it('有风格:风格段拼最前单独成行(69 号票同形);风格全空白不拼', () => {
    const got = buildSheetPrompt('姿态', { name: '阿银', variantName: '默认' }, ' 赛博朋克冷色调 ')
    expect(got.startsWith('【画面风格】赛博朋克冷色调\n')).toBe(true)
    expect(buildSheetPrompt('姿态', { name: '阿银', variantName: '默认' }, '  ')).toBe(
      [
        '角色设定图·姿态集:同一角色的多个典型动作与姿态对照,全身,白底,姿态自然清晰,标准设定图画风',
        '角色:阿银',
        '造型:默认',
      ].join('\n'),
    )
  })

  it('总长截断 ≤2000 runes', () => {
    const got = buildSheetPrompt('色板', {
      name: '满'.repeat(2000),
      detail: '细'.repeat(2000),
      variantName: '默认',
    }, '')
    expect([...got].length).toBeLessThanOrEqual(MAX_SHEET_PROMPT_RUNES)
  })
})

// ---- B3 参考图默认与 cut 注入挑选的测试脚手架 ----

function variant(id: number, images: { id: number; asset_id: number; label: string }[]): CharacterVariantRecord {
  return {
    id,
    character_id: 1,
    name: '默认',
    note: '',
    sort: 0,
    images: images.map((im) => ({ id: im.id, character_id: 1, variant_id: id, asset_id: im.asset_id, label: im.label, sort: 0 })),
    created_at: '',
    updated_at: '',
  }
}

function character(id: number, variants: CharacterVariantRecord[], cover = 0): CharacterRecord {
  return {
    id,
    ip_id: 1,
    name: `角色${id}`,
    alias: '',
    description: '',
    detail: '',
    cover_asset_id: cover,
    voice_id: 0,
    variants,
    created_at: '',
    updated_at: '',
  }
}

describe('latestVariantImage(B3 参考图默认)', () => {
  it('取变体内最近一张(id 最大)', () => {
    const v = variant(21, [
      { id: 5, asset_id: 105, label: '三视图' },
      { id: 9, asset_id: 109, label: '色板' },
    ])
    expect(latestVariantImage(v)?.asset_id).toBe(109)
  })

  it('空变体返回 null', () => {
    expect(latestVariantImage(variant(21, []))).toBeNull()
  })
})

describe('pickCutCharacterRefs(cut 出场角色注入)', () => {
  it('按 character_ids 序挑选;默认变体优先视图类 > 任意图 > 封面投影', () => {
    const chars = [
      // 角色 1:默认变体两张都在视图五类(色板/三视图)→ 取最新(id 9)
      character(1, [variant(21, [
        { id: 9, asset_id: 109, label: '色板' },
        { id: 7, asset_id: 107, label: '三视图' },
      ])]),
      // 角色 2:默认变体只有非五类 label?色板也在五类 —— 用「其他」表达任意图
      character(2, [variant(22, [{ id: 3, asset_id: 203, label: '其他' }])]),
      // 角色 3:默认变体空图,但封面投影有图
      character(3, [variant(23, [])], 303),
    ]
    const { picks, dropped } = pickCutCharacterRefs(chars, [1, 2, 3], 9)
    expect(picks).toEqual([
      { characterId: 1, assetId: 109 },
      { characterId: 2, assetId: 203 },
      { characterId: 3, assetId: 303 },
    ])
    expect(dropped).toBe(0)
  })

  it('变体名非「默认」时回落首个变体', () => {
    const c = character(1, [{ ...variant(30, [{ id: 2, asset_id: 302, label: '三视图' }]), name: '冬装' }])
    const { picks } = pickCutCharacterRefs([c], [1], 9)
    expect(picks).toEqual([{ characterId: 1, assetId: 302 }])
  })

  it('无图角色静默跳过;超上限按序截断并计 dropped', () => {
    const chars = [
      character(1, [variant(21, [{ id: 1, asset_id: 101, label: '三视图' }])]),
      character(2, [variant(22, [])]), // 无图:跳过
      character(3, [variant(23, [{ id: 3, asset_id: 103, label: '三视图' }])]),
    ]
    const { picks, dropped } = pickCutCharacterRefs(chars, [1, 2, 3], 2)
    expect(picks).toEqual([
      { characterId: 1, assetId: 101 },
      { characterId: 3, assetId: 103 },
    ])
    expect(dropped).toBe(0)
    // 上限 1:角色 3 被截掉,dropped = 1。
    const capped = pickCutCharacterRefs(chars, [1, 2, 3], 1)
    expect(capped.picks).toEqual([{ characterId: 1, assetId: 101 }])
    expect(capped.dropped).toBe(1)
  })

  it('不存在的角色 id 跳过(目录缺行的窄窗)', () => {
    const { picks, dropped } = pickCutCharacterRefs([], [404], 9)
    expect(picks).toEqual([])
    expect(dropped).toBe(0)
  })
})
