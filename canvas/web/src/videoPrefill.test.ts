// 直通预填(71 号票 A2 / D1)的装配层纯函数测试:cut→视频预填与角色卡「生成
// 登场视频」把 cutVideo / sheet / presets 的判定折成视频草稿(参考条 + 提示词)
// 与轻提示。cut 路径经真实的 buildCutVideoPrefill 产出,测试的是装配缝。
import { describe, expect, it } from 'vitest'

import type { CharacterImageRecord, CharacterRecord, CharacterVariantRecord } from '@infinitechance/api'

import { buildCutVideoPrefill } from './cutVideo'
import { cutPrefillNotice, cutPrefillVideoRefs } from './videoPrefill'

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
