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
  defaultCharacterVariant,
  latestVariantImage,
  pickCutCharacterRefs,
  pickVariantAssetId,
  resolveCutVariant,
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

  it('有风格:风格段拼最前单独成行(69 号票同形,73 号票标记换形);风格全空白不拼', () => {
    const got = buildSheetPrompt('姿态', { name: '阿银', variantName: '默认' }, ' 赛博朋克冷色调 ')
    expect(got.startsWith('画面风格:赛博朋克冷色调\n')).toBe(true)
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
      { characterId: 1, assetId: 109, variantId: 21 },
      { characterId: 2, assetId: 203, variantId: 22 },
      { characterId: 3, assetId: 303, variantId: 23 },
    ])
    expect(dropped).toBe(0)
  })

  it('变体名非「默认」时回落首个变体', () => {
    const c = character(1, [{ ...variant(30, [{ id: 2, asset_id: 302, label: '三视图' }]), name: '冬装' }])
    const { picks } = pickCutCharacterRefs([c], [1], 9)
    expect(picks).toEqual([{ characterId: 1, assetId: 302, variantId: 30 }])
  })

  it('无图角色静默跳过;超上限按序截断并计 dropped', () => {
    const chars = [
      character(1, [variant(21, [{ id: 1, asset_id: 101, label: '三视图' }])]),
      character(2, [variant(22, [])]), // 无图:跳过
      character(3, [variant(23, [{ id: 3, asset_id: 103, label: '三视图' }])]),
    ]
    const { picks, dropped } = pickCutCharacterRefs(chars, [1, 2, 3], 2)
    expect(picks).toEqual([
      { characterId: 1, assetId: 101, variantId: 21 },
      { characterId: 3, assetId: 103, variantId: 23 },
    ])
    expect(dropped).toBe(0)
    // 上限 1:角色 3 被截掉,dropped = 1。
    const capped = pickCutCharacterRefs(chars, [1, 2, 3], 1)
    expect(capped.picks).toEqual([{ characterId: 1, assetId: 101, variantId: 21 }])
    expect(capped.dropped).toBe(1)
  })

  it('不存在的角色 id 跳过(目录缺行的窄窗)', () => {
    const { picks, dropped } = pickCutCharacterRefs([], [404], 9)
    expect(picks).toEqual([])
    expect(dropped).toBe(0)
  })
})

// ---- 71 号票 D1:按变体取图共享函数(cut 注入与角色卡登场直通两处共用)----

describe('defaultCharacterVariant(cut 注入的「默认变体」规则)', () => {
  it('优先名为「默认」的变体,无则回落首个;无变体返回 undefined', () => {
    const d = variant(21, [])
    const w = { ...variant(22, []), name: '冬装' }
    expect(defaultCharacterVariant(character(1, [w, d]))).toBe(d)
    expect(defaultCharacterVariant(character(1, [w]))).toBe(w)
    expect(defaultCharacterVariant(character(1, []))).toBeUndefined()
  })
})

describe('pickVariantAssetId(按变体取图:视图类最近一张 > 变体任意图 > 封面投影)', () => {
  it('视图类(label ∈ 五类型)优先取最近一张,即便非视图类的图更新', () => {
    const v = variant(21, [
      { id: 9, asset_id: 109, label: '其他' }, // 最新但非视图类
      { id: 7, asset_id: 107, label: '三视图' },
      { id: 5, asset_id: 105, label: '色板' },
    ])
    expect(pickVariantAssetId(character(1, [v], 999), v)).toBe(107)
  })

  it('变体无视图类图时取变体内任意最近一张', () => {
    const v = variant(22, [
      { id: 3, asset_id: 203, label: '其他' },
      { id: 8, asset_id: 208, label: '其他' },
    ])
    expect(pickVariantAssetId(character(2, [v], 999), v)).toBe(208)
  })

  it('变体空图回落角色封面投影(cover_asset_id)', () => {
    const v = variant(23, [])
    expect(pickVariantAssetId(character(3, [v], 303), v)).toBe(303)
  })

  it('变体缺省(角色无任何变体)只走封面投影', () => {
    expect(pickVariantAssetId(character(4, [], 404), undefined)).toBe(404)
  })

  it('变体与封面全无图返回 0(调用方静默跳过)', () => {
    const v = variant(25, [])
    expect(pickVariantAssetId(character(5, [v], 0), v)).toBe(0)
    expect(pickVariantAssetId(character(6, [], 0), undefined)).toBe(0)
  })

  it('一致性:pickCutCharacterRefs 逐角色取图 = 共享函数按默认变体取图', () => {
    const chars = [
      // 视图类最近(7→107)而非最新(9→109「其他」)
      character(1, [variant(21, [
        { id: 9, asset_id: 109, label: '其他' },
        { id: 7, asset_id: 107, label: '三视图' },
      ])]),
      // 非视图类任意图
      character(2, [variant(22, [{ id: 3, asset_id: 203, label: '其他' }])]),
      // 变体空图 → 封面投影
      character(3, [variant(23, [])], 303),
      // 无变体:走封面投影
      character(4, [], 404),
      // 全无图 → 跳过
      character(5, [variant(25, [])]),
      // 变体名非「默认」回落首个
      character(6, [{ ...variant(26, [{ id: 1, asset_id: 601, label: '姿态' }]), name: '夏装' }]),
    ]
    const ids = [1, 2, 3, 4, 5, 6]
    const { picks } = pickCutCharacterRefs(chars, ids, 9)
    // 独立已知值:直通路径(角色卡登场)按同一规则应得同一组图。
    expect(picks).toEqual([
      { characterId: 1, assetId: 107, variantId: 21 },
      { characterId: 2, assetId: 203, variantId: 22 },
      { characterId: 3, assetId: 303, variantId: 23 },
      { characterId: 4, assetId: 404 },
      { characterId: 6, assetId: 601, variantId: 26 },
    ])
    const viaShared = ids
      .map((id) => chars.find((c) => c.id === id)!)
      .map((c) => {
        const variant = defaultCharacterVariant(c)
        return {
          characterId: c.id,
          assetId: pickVariantAssetId(c, variant),
          ...(variant === undefined ? {} : { variantId: variant.id }),
        }
      })
      .filter((p) => p.assetId !== 0)
    expect(viaShared).toEqual(picks)
  })
})

// ---- 83 号票:cut→变体精确绑定(绑定优先 → 默认变体回落)----

describe('resolveCutVariant(83 号票绑定解析)', () => {
  it('绑定存在且可解析:取绑定变体,非回落态', () => {
    const d = variant(21, [])
    const rain = { ...variant(22, []), name: '雨衣' }
    const c = character(1, [d, rain])
    expect(resolveCutVariant(c, 22)).toEqual({ variant: rain, fellBack: false })
  })

  it('缺省绑定(undefined)即缺映射:默认变体,非回落态', () => {
    const d = variant(21, [])
    const w = { ...variant(22, []), name: '冬装' }
    const c = character(1, [w, d])
    expect(resolveCutVariant(c, undefined)).toEqual({ variant: d, fellBack: false })
  })

  it('绑定变体已删:回落默认变体,fellBack 真(回落态显示依据)', () => {
    const d = variant(21, [])
    const c = character(1, [d])
    expect(resolveCutVariant(c, 99)).toEqual({ variant: d, fellBack: true })
  })

  it('绑定变体已删且角色已无任何变体:回落 undefined,fellBack 真', () => {
    const c = character(1, [])
    expect(resolveCutVariant(c, 99)).toEqual({ variant: undefined, fellBack: true })
  })
})

describe('pickCutCharacterRefs 变体绑定(83 号票:绑定优先 → 默认变体回落)', () => {
  const d = variant(21, [{ id: 7, asset_id: 107, label: '三视图' }])
  const rain = { ...variant(22, [{ id: 8, asset_id: 208, label: '三视图' }]), name: '雨衣' }
  const chars = [character(1, [d, rain])]

  it('cut 绑定变体优先:取绑定变体的设定图与 variantId', () => {
    const { picks, dropped } = pickCutCharacterRefs(chars, [1], 9, { 1: 22 })
    expect(picks).toEqual([{ characterId: 1, assetId: 208, variantId: 22 }])
    expect(dropped).toBe(0)
  })

  it('无字段/空映射/缺该角色的映射:三种情况都回落默认变体,与旧契约一致', () => {
    const expected = [{ characterId: 1, assetId: 107, variantId: 21 }]
    expect(pickCutCharacterRefs(chars, [1], 9).picks).toEqual(expected)
    expect(pickCutCharacterRefs(chars, [1], 9, undefined).picks).toEqual(expected)
    expect(pickCutCharacterRefs(chars, [1], 9, {}).picks).toEqual(expected)
    expect(pickCutCharacterRefs(chars, [1], 9, { 2: 22 }).picks).toEqual(expected)
  })

  it('绑定变体已删:回落默认变体(variantId = 默认变体 id)', () => {
    const { picks } = pickCutCharacterRefs(chars, [1], 9, { 1: 99 })
    expect(picks).toEqual([{ characterId: 1, assetId: 107, variantId: 21 }])
  })

  it('绑定生效但绑定变体无图:取图链照旧回落封面投影(只换变体,不改取图优先级)', () => {
    const rainNoImg = { ...variant(22, []), name: '雨衣' }
    const cs = [character(1, [d, rainNoImg], 909)]
    const { picks } = pickCutCharacterRefs(cs, [1], 9, { 1: 22 })
    expect(picks).toEqual([{ characterId: 1, assetId: 909, variantId: 22 }])
  })

  it('存量画布行为不变:同输入不带映射的输出与映射缺省路径一致', () => {
    const withMap = pickCutCharacterRefs(chars, [1], 9, { 1: 21 })
    const noMap = pickCutCharacterRefs(chars, [1], 9)
    expect(withMap).toEqual(noMap)
  })
})
