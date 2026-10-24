import { describe, expect, it } from 'vitest'

import {
  appendBeat,
  canAddLevelAsset,
  defaultLevel,
  MAX_LEVEL_ASSETS,
  moveBeat,
  removeBeat,
  toggleLevelAsset,
} from './level'

describe('defaultLevel', () => {
  it('seeds a level with the default name and empty beats/assets', () => {
    // 添加即「新关卡」+ 空节拍(55 号票);goal/detail 是可空字段,空时不
    // 落键(页标题「不落键」同纪律)。
    const data = defaultLevel()
    expect(data.name).toBe('新关卡')
    expect(data.beats).toEqual([])
    expect(data.asset_ids).toEqual([])
    expect('goal' in data).toBe(false)
    expect('detail' in data).toBe(false)
  })

  it('does not share the beats/assets arrays between levels', () => {
    // 数组换新纯函数纪律:两个新关卡的节拍与画面槽各自独立。
    const a = defaultLevel()
    const b = defaultLevel()
    expect(a.beats).not.toBe(b.beats)
    expect(a.asset_ids).not.toBe(b.asset_ids)
  })
})

describe('appendBeat / removeBeat / moveBeat', () => {
  it('appends an empty beat and returns a new array', () => {
    const seed = ['开场', '遭遇战']
    const next = appendBeat(seed)
    expect(next).toEqual(['开场', '遭遇战', ''])
    expect(seed).toEqual(['开场', '遭遇战'])
  })

  it('removes a beat by index and allows emptying the list', () => {
    // 节拍可删到空:关卡是画布工作件而非库实体,无「最后不可删」守卫
    // (与絵コンテ cut / ネーム页同 53/54 号票定案)。
    const seed = ['开场', 'BOSS', '结算']
    expect(removeBeat(seed, 1)).toEqual(['开场', '结算'])
    const once = removeBeat(seed, 0)
    const twice = removeBeat(once, 0)
    expect(twice).toEqual(['结算'])
    expect(removeBeat(twice, 0)).toEqual([])
    expect(seed).toHaveLength(3)
  })

  it('moves a beat within bounds and no-ops at the edges', () => {
    // 上移/下移按钮排序(40 号票手势纪律:不做拖拽);序号显示时生成
    // 1..N,删插自动重排,不持久。
    const seed = ['一', '二', '三']
    expect(moveBeat(seed, 0, -1)).toBe(seed)
    expect(moveBeat(seed, 2, 1)).toBe(seed)
    expect(moveBeat(seed, 9, 1)).toBe(seed)
    expect(moveBeat(seed, 1, -1)).toEqual(['二', '一', '三'])
    expect(moveBeat(seed, 1, 1)).toEqual(['一', '三', '二'])
    expect(seed).toEqual(['一', '二', '三'])
  })
})

describe('canAddLevelAsset / toggleLevelAsset(画面槽 9 上限守卫)', () => {
  it('caps the asset slot list at 9, same budget as composer refs', () => {
    // 上限 9 与参考图口径一致(55 号票);MAX_LEVEL_ASSETS 是唯一事实源。
    expect(MAX_LEVEL_ASSETS).toBe(9)
    expect(canAddLevelAsset([])).toBe(true)
    expect(canAddLevelAsset([1, 2, 3, 4, 5, 6, 7, 8])).toBe(true)
    expect(canAddLevelAsset([1, 2, 3, 4, 5, 6, 7, 8, 9])).toBe(false)
  })

  it('toggles an asset id in and out without mutating the input', () => {
    // 已在槽中 → 摘下;不在 → 追加(素材挑选点选/再点取消同款 toggle)。
    const seed = [3, 7]
    expect(toggleLevelAsset(seed, 7)).toEqual([3])
    expect(toggleLevelAsset(seed, 9)).toEqual([3, 7, 9])
    expect(seed).toEqual([3, 7])
    // 重复添加已存在的 id 是摘下,不产生重复引用。
    expect(toggleLevelAsset([5], 5)).toEqual([])
  })

  it('rejects the whole add when the slots are full (returns the same array)', () => {
    // 满员整条拒收(返回原数组,appendComposerRef 同款纪律):调用方按
    // 引用不等识别拒收并提示;摘除不受上限约束。
    const full = [1, 2, 3, 4, 5, 6, 7, 8, 9]
    expect(toggleLevelAsset(full, 10)).toBe(full)
    expect(toggleLevelAsset(full, 9)).toEqual([1, 2, 3, 4, 5, 6, 7, 8])
  })
})
