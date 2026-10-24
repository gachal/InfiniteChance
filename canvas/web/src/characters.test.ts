import { describe, expect, it } from 'vitest'

import type { CharacterImageRecord } from '@infinitechance/api'

import { orderedCharacterImages, toggleCharacterChip } from './characters'

function img(id: number, label: string): CharacterImageRecord {
  return { id, character_id: 1, variant_id: 1, asset_id: id, label, sort: 0 }
}

describe('orderedCharacterImages', () => {
  it('puts view-label images ahead while keeping each group in place (52 号票)', () => {
    // 变体画廊展示序:视图类(三视图/多视图)置前,组内保持 sort+id 原序
    // —— 与 51 号票详情抽屉同一口径。
    const images = [img(1, '表情集'), img(2, '三视图'), img(3, '其他'), img(4, '多视图')]
    expect(orderedCharacterImages(images).map((i) => i.id)).toEqual([2, 4, 1, 3])
  })

  it('keeps an all-view or all-misc list unchanged', () => {
    const views = [img(1, '三视图'), img(2, '多视图')]
    expect(orderedCharacterImages(views)).toEqual(views)
    const misc = [img(1, '色板'), img(2, '姿态')]
    expect(orderedCharacterImages(misc)).toEqual(misc)
  })

  it('does not mutate the input array', () => {
    const images = [img(1, '表情集'), img(2, '三视图')]
    orderedCharacterImages(images)
    expect(images.map((i) => i.id)).toEqual([1, 2])
  })
})

describe('toggleCharacterChip', () => {
  it('appends a new selection at the end (选择序即注入序)', () => {
    expect(toggleCharacterChip([], 11)).toEqual([11])
    expect(toggleCharacterChip([11], 12)).toEqual([11, 12])
  })

  it('removes an already-selected character idempotently', () => {
    expect(toggleCharacterChip([11, 12, 13], 12)).toEqual([11, 13])
    expect(toggleCharacterChip([11], 11)).toEqual([])
  })

  it('does not mutate the input array', () => {
    const ids = [11]
    toggleCharacterChip(ids, 12)
    expect(ids).toEqual([11])
  })
})
