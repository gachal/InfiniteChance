/**
 * cut→视频预填纯逻辑测试(71 号票 A 节,修 70 号票已上线缺陷):首帧与
 * 全模态参考互斥(Seedance / MiniMax H3 三家官方口径)——cut 有画面作真
 * 首帧且不注入角色设定图(跳过标记),cut 无画面才按出场角色注入元素 chip
 * (role 恒 reference_image,超上限截断计数,无图静默跳过)。
 */
import { describe, expect, it } from 'vitest'

import type { CharacterRecord, CharacterVariantRecord } from '@infinitechance/api'

import { pickCutCharacterRefs } from './sheet'
import { buildCutVideoPrefill } from './cutVideo'

// ---- 测试脚手架(sheet.test.ts 同款轻量对象,不依赖 api 包运行时)----

function variant(id: number, images: { id: number; asset_id: number; label: string }[], palette = ''): CharacterVariantRecord {
  return {
    id,
    character_id: 1,
    name: '默认',
    note: '',
    palette,
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

const CHARS = [
  character(1, [variant(21, [
    { id: 9, asset_id: 109, label: '其他' }, // 最新但非视图类 → 不取
    { id: 7, asset_id: 107, label: '三视图' },
  ])]),
  character(2, [variant(22, [{ id: 3, asset_id: 203, label: '色板' }])]),
  character(3, [variant(23, [])], 303), // 变体空图 → 封面投影
]

describe('buildCutVideoPrefill(cut 有画面 = 真首帧,不注入角色设定图)', () => {
  it('cut 有画面且标了出场角色:首帧直通、参考条不注入、跳过标记为真', () => {
    const got = buildCutVideoPrefill(
      { asset_id: 55, character_ids: [1, 2] },
      CHARS,
      9,
    )
    expect(got).toEqual({
      firstFrameAssetId: 55,
      refs: [],
      dropped: 0,
      skippedInject: true,
      paletteLine: '',
    })
  })

  it('cut 有画面但无出场角色:不标记跳过(无事可说)', () => {
    expect(buildCutVideoPrefill({ asset_id: 55 }, CHARS, 9).skippedInject).toBe(false)
    expect(buildCutVideoPrefill({ asset_id: 55, character_ids: [] }, CHARS, 9).skippedInject).toBe(false)
  })

  it('首帧不占参考条(坑:refs 里永不出现首帧,走 firstFrameAssetId 单独返回)', () => {
    const got = buildCutVideoPrefill({ asset_id: 55, character_ids: [1] }, CHARS, 9)
    expect(got.refs.some((r) => r.asset_id === 55)).toBe(false)
    expect(got.refs).toHaveLength(0)
  })
})

describe('buildCutVideoPrefill(cut 无画面 = 按出场角色注入元素 chip)', () => {
  it('按 character_ids 序注入:role 恒 reference_image、kind image、元素 chip 形态', () => {
    const got = buildCutVideoPrefill(
      { asset_id: 0, character_ids: [1, 2] },
      CHARS,
      9,
    )
    expect(got.firstFrameAssetId).toBe(0)
    expect(got.skippedInject).toBe(false)
    expect(got.dropped).toBe(0)
    expect(got.refs).toEqual([
      {
        url: '/api/assets/107/content',
        kind: 'image',
        role: 'reference_image',
        asset_id: 107,
        element: { type: 'character', entityId: 1, variantId: 21 },
      },
      {
        url: '/api/assets/203/content',
        kind: 'image',
        role: 'reference_image',
        asset_id: 203,
        element: { type: 'character', entityId: 2, variantId: 22 },
      },
    ])
  })

  it('角色无变体时走封面投影,element 不带 variantId', () => {
    const got = buildCutVideoPrefill(
      { asset_id: 0, character_ids: [4] },
      [character(4, [], 404)],
      9,
    )
    expect(got.refs).toHaveLength(1)
    expect(got.refs[0].asset_id).toBe(404)
    expect(got.refs[0].element).toStrictEqual({ type: 'character', entityId: 4 })
  })

  it('与 pickCutCharacterRefs 两路取同一组图(D1 共享函数一致性)', () => {
    const ids = [1, 2, 3]
    const got = buildCutVideoPrefill({ asset_id: 0, character_ids: ids }, CHARS, 9)
    const { picks } = pickCutCharacterRefs(CHARS, ids, 9)
    expect(got.refs.map((r) => r.asset_id)).toEqual(picks.map((p) => p.assetId))
    expect(got.refs.map((r) => r.element?.entityId)).toEqual(picks.map((p) => p.characterId))
  })

  it('无图角色静默跳过(70 号票语义),不计入 dropped;不存在的角色 id 同样跳过', () => {
    const chars = [
      ...CHARS,
      character(5, [variant(25, [])]), // 全无图
    ]
    const got = buildCutVideoPrefill(
      { asset_id: 0, character_ids: [5, 404, 1] },
      chars,
      9,
    )
    expect(got.refs.map((r) => r.element?.entityId)).toEqual([1])
    expect(got.dropped).toBe(0)
  })

  it('超上限按序截断,dropped 计未带上数量(wiring 层 toast 用)', () => {
    const got = buildCutVideoPrefill(
      { asset_id: 0, character_ids: [1, 2, 3] },
      CHARS,
      2,
    )
    expect(got.refs).toHaveLength(2)
    expect(got.refs.map((r) => r.element?.entityId)).toEqual([1, 2])
    expect(got.dropped).toBe(1)
  })

  it('cut 无画面且角色全无图:refs 空、无首帧、无截断、无跳过标记', () => {
    const got = buildCutVideoPrefill(
      { asset_id: 0, character_ids: [5] },
      [character(5, [variant(25, [])])],
      9,
    )
    expect(got).toEqual({ firstFrameAssetId: 0, refs: [], dropped: 0, skippedInject: false, paletteLine: '' })
  })
})

describe('buildCutVideoPrefill 变体绑定(83 号票:绑定优先 → 默认变体回落)', () => {
  const def = variant(21, [{ id: 7, asset_id: 107, label: '三视图' }])
  const rain = { ...variant(22, [{ id: 8, asset_id: 208, label: '三视图' }]), name: '雨衣' }
  const chars = [character(1, [def, rain])]

  it('无画面 + 绑定:元素 chip 取绑定变体图,variantId = 绑定变体', () => {
    const got = buildCutVideoPrefill(
      { asset_id: 0, character_ids: [1], character_variants: { 1: 22 } },
      chars,
      9,
    )
    expect(got.refs).toEqual([
      {
        url: '/api/assets/208/content',
        kind: 'image',
        role: 'reference_image',
        asset_id: 208,
        element: { type: 'character', entityId: 1, variantId: 22 },
      },
    ])
  })

  it('绑定变体已删:回落默认变体(variantId = 默认变体 id)', () => {
    const got = buildCutVideoPrefill(
      { asset_id: 0, character_ids: [1], character_variants: { 1: 99 } },
      chars,
      9,
    )
    expect(got.refs[0].asset_id).toBe(107)
    expect(got.refs[0].element).toStrictEqual({ type: 'character', entityId: 1, variantId: 21 })
  })

  it('无字段(存量画布):行为与改动前逐字节一致', () => {
    const got = buildCutVideoPrefill(
      { asset_id: 0, character_ids: [1] },
      chars,
      9,
    )
    expect(got.refs[0].asset_id).toBe(107)
    expect(got.refs[0].element).toStrictEqual({ type: 'character', entityId: 1, variantId: 21 })
  })

  it('A4 硬约束不动:有画面 + 绑定,仍不注入(skippedInject 真、refs 空)', () => {
    const got = buildCutVideoPrefill(
      { asset_id: 55, character_ids: [1], character_variants: { 1: 22 } },
      chars,
      9,
    )
    expect(got).toEqual({ firstFrameAssetId: 55, refs: [], dropped: 0, skippedInject: true, paletteLine: '' })
  })
})

// ---- 84 号票 B3:角色配色段(无画面路径同拼,有画面路径恒空)----

describe('buildCutVideoPrefill 配色段(84 号票 B3)', () => {
  const HEX_BLUE = '发色:#1A2E;肤色:#FFE0D0'
  const HEX_RED = '发色:#C0392B;服装主色:#8E2A22'

  it('无画面:paletteLine 按绑定变体配色拼行(单角色单行),与参考 chip 同源解析', () => {
    const chars = [
      character(1, [variant(21, [{ id: 7, asset_id: 107, label: '三视图' }], HEX_BLUE)]),
    ]
    const got = buildCutVideoPrefill({ asset_id: 0, character_ids: [1] }, chars, 9)
    expect(got.paletteLine).toBe(`角色配色:角色1:${HEX_BLUE}`)
  })

  it('绑定变体优先:配色取绑定变体(参考图取图同源,绑定映射同份)', () => {
    const def = variant(21, [{ id: 7, asset_id: 107, label: '三视图' }], HEX_BLUE)
    const rain = { ...variant(22, [{ id: 8, asset_id: 208, label: '三视图' }], HEX_RED), name: '雨衣' }
    const chars = [character(1, [def, rain])]
    const got = buildCutVideoPrefill(
      { asset_id: 0, character_ids: [1], character_variants: { 1: 22 } },
      chars,
      9,
    )
    expect(got.refs[0].asset_id).toBe(208)
    expect(got.paletteLine).toBe(`角色配色:角色1:${HEX_RED}`)
  })

  it('多角色:标记行 + 逐角色一行;无配色角色跳过不占行', () => {
    const chars = [
      character(1, [variant(21, [{ id: 7, asset_id: 107, label: '三视图' }], HEX_BLUE)]),
      character(2, [variant(22, [{ id: 3, asset_id: 203, label: '三视图' }], '')]),
      character(3, [variant(23, [{ id: 4, asset_id: 204, label: '三视图' }], HEX_RED)]),
    ]
    const got = buildCutVideoPrefill({ asset_id: 0, character_ids: [1, 2, 3] }, chars, 9)
    expect(got.paletteLine).toBe(`角色配色:\n角色1:${HEX_BLUE}\n角色3:${HEX_RED}`)
  })

  it('角色全无配色:paletteLine 空串(整段跳过,行为回到 83 号票形态)', () => {
    const chars = [character(1, [variant(21, [{ id: 7, asset_id: 107, label: '三视图' }])])]
    const got = buildCutVideoPrefill({ asset_id: 0, character_ids: [1] }, chars, 9)
    expect(got.paletteLine).toBe('')
  })

  it('无图但绑定变体有配色:参考条跳过(refs 空)、配色行照拼(两件事互不依赖)', () => {
    const chars = [character(1, [variant(21, [], HEX_BLUE)])]
    const got = buildCutVideoPrefill({ asset_id: 0, character_ids: [1] }, chars, 9)
    expect(got.refs).toHaveLength(0)
    expect(got.paletteLine).toBe(`角色配色:角色1:${HEX_BLUE}`)
  })

  it('有画面:paletteLine 恒空(71 号票互斥不注入,84 不碰有画面路径)', () => {
    const chars = [
      character(1, [variant(21, [{ id: 7, asset_id: 107, label: '三视图' }], HEX_BLUE)]),
      character(2, [variant(22, [{ id: 3, asset_id: 203, label: '三视图' }], HEX_RED)]),
    ]
    const got = buildCutVideoPrefill(
      { asset_id: 55, character_ids: [1, 2], character_variants: { 1: 21, 2: 22 } },
      chars,
      9,
    )
    expect(got.skippedInject).toBe(true)
    expect(got.paletteLine).toBe('')
  })
})
