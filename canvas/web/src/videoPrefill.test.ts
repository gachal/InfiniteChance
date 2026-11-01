// 直通预填(71 号票 A2 / D1)的装配层纯函数测试:cut→视频预填与角色卡「生成
// 登场视频」把 cutVideo / sheet / presets 的判定折成视频草稿(参考条 + 提示词)
// 与轻提示。cut 路径经真实的 buildCutVideoPrefill 产出,测试的是装配缝。
import { describe, expect, it } from 'vitest'

import type { CharacterImageRecord, CharacterRecord, CharacterVariantRecord } from '@infinitechance/api'

import { buildCutVideoPrefill } from './cutVideo'
import { buildEntrancePrefill, cutPrefillNotice, cutPrefillVideoRefs } from './videoPrefill'

const image = (id: number, assetId: number, label: string): CharacterImageRecord => ({
  id,
  character_id: 1,
  variant_id: 1,
  asset_id: assetId,
  label,
  sort: 0,
})

const variant = (id: number, name: string, images: CharacterImageRecord[] = []): CharacterVariantRecord => ({
  id,
  character_id: 1,
  name,
  note: '',
  sort: 0,
  images,
  created_at: '',
  updated_at: '',
})

const character = (
  id: number,
  name: string,
  variants: CharacterVariantRecord[],
  cover = 0,
): CharacterRecord => ({
  id,
  ip_id: 1,
  name,
  alias: '',
  description: '',
  detail: '',
  cover_asset_id: cover,
  voice_id: 0,
  variants,
  created_at: '',
  updated_at: '',
})

const blue = character(7, '小蓝', [variant(70, '默认', [image(1, 501, '三视图')])])
const red = character(8, '阿红', [variant(80, '默认', [image(2, 502, '三视图')])])

describe('cut→视频预填折算(71 号票 A2,修 70 号票首帧/参考互斥缺陷)', () => {
  it('cut 有画面:画面作真首帧、不注入角色;有出场角色时提示未注入原因', () => {
    const prefill = buildCutVideoPrefill({ asset_id: 900, character_ids: [7, 8] }, [blue, red], 9)
    expect(cutPrefillVideoRefs(prefill)).toEqual([
      { url: '/api/assets/900/content', kind: 'image', role: 'first_frame', asset_id: 900 },
    ])
    expect(cutPrefillNotice(prefill)).toBe('cut 已有画面作首帧,出场角色设定图未注入(首帧与参考不能混用)')
  })

  it('cut 有画面但没标出场角色:只带首帧,不出提示', () => {
    const prefill = buildCutVideoPrefill({ asset_id: 900 }, [blue], 9)
    expect(cutPrefillVideoRefs(prefill)).toEqual([
      { url: '/api/assets/900/content', kind: 'image', role: 'first_frame', asset_id: 900 },
    ])
    expect(cutPrefillNotice(prefill)).toBe('')
  })

  it('cut 无画面:出场角色设定图以元素 chip 注入(恒参考图、默认变体),无首帧', () => {
    const prefill = buildCutVideoPrefill({ asset_id: 0, character_ids: [7, 8] }, [blue, red], 9)
    expect(cutPrefillVideoRefs(prefill)).toEqual([
      {
        url: '/api/assets/501/content',
        kind: 'image',
        role: 'reference_image',
        asset_id: 501,
        element: { type: 'character', entityId: 7, variantId: 70 },
      },
      {
        url: '/api/assets/502/content',
        kind: 'image',
        role: 'reference_image',
        asset_id: 502,
        element: { type: 'character', entityId: 8, variantId: 80 },
      },
    ])
    expect(cutPrefillNotice(prefill)).toBe('')
  })

  it('cut 无画面且角色超上限:截断计数进提示', () => {
    const prefill = buildCutVideoPrefill({ asset_id: 0, character_ids: [7, 8] }, [blue, red], 1)
    expect(cutPrefillVideoRefs(prefill)).toHaveLength(1)
    expect(cutPrefillNotice(prefill)).toBe('1 个角色设定图未带上(参考上限 9)')
  })

  it('cut 无画面无角色:纯文生视频,参考条清空', () => {
    const prefill = buildCutVideoPrefill({ asset_id: 0 }, [blue], 9)
    expect(cutPrefillVideoRefs(prefill)).toEqual([])
    expect(cutPrefillNotice(prefill)).toBe('')
  })
})

describe('角色卡「生成登场视频」预填(71 号票 D1)', () => {
  const ENTRANCE_BLUE =
    '小蓝登场:空镜开场,小蓝从画面深处缓步走出,低角度仰拍缓慢推近,风吹动衣摆与发丝;小蓝站定,转身直视镜头,摆出标志性姿势定格。节奏先慢后快,电影感光影,小蓝的样貌与服装和参考保持一致。'

  it('参考条 = 该角色当前变体的元素 chip(视图类最新一张);提示词 = 单角色登场模板填名,无场景省略从句', () => {
    const battle = variant(71, '战斗服', [image(3, 601, '其他'), image(4, 602, '三视图'), image(5, 603, '多视图')])
    const c = character(7, '小蓝', [variant(70, '默认', [image(1, 501, '三视图')]), battle])
    const prefill = buildEntrancePrefill(c, battle, '')
    expect(prefill.refs).toEqual([
      {
        url: '/api/assets/603/content',
        kind: 'image',
        role: 'reference_image',
        asset_id: 603,
        element: { type: 'character', entityId: 7, variantId: 71 },
      },
    ])
    expect(prefill.prompt).toBe(ENTRANCE_BLUE)
    expect(prefill.notice).toBe('')
  })

  it('IP 已定画面风格:风格段拼最前(69 号票直通先例)', () => {
    const prefill = buildEntrancePrefill(blue, blue.variants[0], '水墨')
    expect(prefill.prompt).toBe(`【画面风格】水墨\n${ENTRANCE_BLUE}`)
  })

  it('与 cut 注入同一取图规则:默认变体两路取到同一张图、同一元素', () => {
    const entrance = buildEntrancePrefill(blue, blue.variants[0], '')
    const cut = buildCutVideoPrefill({ asset_id: 0, character_ids: [7] }, [blue], 9)
    expect(entrance.refs).toEqual(cut.refs)
  })

  it('变体无设定图时回落角色封面投影', () => {
    const c = character(7, '小蓝', [variant(70, '默认')], 777)
    expect(buildEntrancePrefill(c, c.variants[0], '').refs[0]?.asset_id).toBe(777)
  })

  it('变体与角色都无图:不带 chip,提示先生成设定图;提示词照常填名', () => {
    const c = character(7, '小蓝', [variant(70, '默认')])
    const prefill = buildEntrancePrefill(c, c.variants[0], '')
    expect(prefill.refs).toEqual([])
    expect(prefill.notice).toBe('该造型还没有设定图,建议先生成设定图')
    expect(prefill.prompt).toBe(ENTRANCE_BLUE)
  })

  it('变体缺省(防御路径):元素不带 variantId,只走封面投影', () => {
    const c = character(7, '小蓝', [], 777)
    expect(buildEntrancePrefill(c, undefined, '').refs).toEqual([
      {
        url: '/api/assets/777/content',
        kind: 'image',
        role: 'reference_image',
        asset_id: 777,
        element: { type: 'character', entityId: 7 },
      },
    ])
  })
})
