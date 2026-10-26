import { describe, expect, it } from 'vitest'

import type { StoryboardCut } from './graph'
import {
  CAMERA_MOVE_OPTIONS,
  appendCut,
  clampSecondsInput,
  defaultStoryboard,
  emptyStoryboardCut,
  formatDuration,
  moveCut,
  removeCut,
  toggleCameraMove,
  totalSeconds,
} from './storyboard'

const cut = (over: Partial<StoryboardCut> = {}): StoryboardCut => ({
  ...emptyStoryboardCut(),
  ...over,
})

describe('CAMERA_MOVE_OPTIONS', () => {
  it('holds the initial whitelist of eight moves', () => {
    // 运镜白名单前端持有(53 号票):初始集八项,后端存串不校验。
    expect([...CAMERA_MOVE_OPTIONS]).toEqual(['推', '拉', '摇', '移', '跟', '固定', '手持', '变焦'])
  })
})

describe('defaultStoryboard / emptyStoryboardCut', () => {
  it('seeds a board with one empty cut', () => {
    // 添加即带 1 个空 cut(预演台「添加即可见」先例):画面空、时长未填、
    // 台词空、无运镜、无备注;板名缺省空串(展示层回落「分镜」)。
    const data = defaultStoryboard()
    expect(data.title).toBe('')
    expect(data.cuts).toHaveLength(1)
    expect(data.cuts[0]).toEqual({ asset_id: 0, lines: '', camera_moves: [] })
    expect(data.cuts[0].seconds).toBeUndefined()
    expect(data.cuts[0].note).toBeUndefined()
  })

  it('does not share the cuts array between boards', () => {
    expect(defaultStoryboard().cuts).not.toBe(defaultStoryboard().cuts)
  })
})

describe('appendCut / removeCut / moveCut', () => {
  it('appends an empty cut and returns a new array', () => {
    const seed = [cut({ lines: '开场' })]
    const next = appendCut(seed)
    expect(next).toHaveLength(2)
    expect(next[1]).toEqual({ asset_id: 0, lines: '', camera_moves: [] })
    expect(seed).toHaveLength(1)
  })

  it('removes a cut by index and allows emptying the board', () => {
    // 板是画布工作件非库实体:cut 可删到空(与角色变体「最后不可删」相反)。
    const seed = [cut({ lines: 'a' }), cut({ lines: 'b' })]
    expect(removeCut(seed, 0).map((c) => c.lines)).toEqual(['b'])
    const once = removeCut(seed, 0)
    expect(removeCut(once, 0)).toEqual([])
    expect(seed).toHaveLength(2)
  })

  it('moves a cut within bounds and no-ops at the edges', () => {
    const seed = [cut({ lines: '1' }), cut({ lines: '2' }), cut({ lines: '3' })]
    expect(moveCut(seed, 0, -1)).toBe(seed)
    expect(moveCut(seed, 2, 1)).toBe(seed)
    expect(moveCut(seed, 9, 1)).toBe(seed)
    expect(moveCut(seed, 1, -1).map((c) => c.lines)).toEqual(['2', '1', '3'])
    expect(moveCut(seed, 1, 1).map((c) => c.lines)).toEqual(['1', '3', '2'])
    expect(seed.map((c) => c.lines)).toEqual(['1', '2', '3'])
  })
})

describe('toggleCameraMove', () => {
  it('adds a move to an empty selection in order', () => {
    const next = toggleCameraMove(emptyStoryboardCut(), '推')
    expect(next.camera_moves).toEqual(['推'])
  })

  it('removes an already selected move without mutating the input', () => {
    const seed = cut({ camera_moves: ['推', '手持'] })
    const next = toggleCameraMove(seed, '推')
    expect(next.camera_moves).toEqual(['手持'])
    expect(seed.camera_moves).toEqual(['推', '手持'])
  })
})

describe('clampSecondsInput', () => {
  it('treats empty or non-numeric input as unset', () => {
    // 时长可空:清空/乱写失焦后回到未填,汇总时跳过。
    expect(clampSecondsInput('')).toBeUndefined()
    expect(clampSecondsInput('  ')).toBeUndefined()
    expect(clampSecondsInput('abc')).toBeUndefined()
  })

  it('clamps negatives to zero and overflows to one hour', () => {
    expect(clampSecondsInput('-5')).toBe(0)
    expect(clampSecondsInput('99999')).toBe(3600)
  })

  it('rounds fractional input to whole seconds', () => {
    expect(clampSecondsInput('12.6')).toBe(13)
    expect(clampSecondsInput('90')).toBe(90)
  })
})

describe('totalSeconds / formatDuration', () => {
  it('sums only the non-empty durations', () => {
    // 时长非空求和:未填的 cut 不计入,不是 0 也不是错误。
    const cuts = [cut({ seconds: 30 }), cut(), cut({ seconds: 95 })]
    expect(totalSeconds(cuts)).toBe(125)
    expect(totalSeconds([])).toBe(0)
  })

  it('formats M:SS with padded seconds', () => {
    expect(formatDuration(0)).toBe('0:00')
    expect(formatDuration(65)).toBe('1:05')
    expect(formatDuration(600)).toBe('10:00')
    expect(formatDuration(3600)).toBe('60:00')
  })
})
