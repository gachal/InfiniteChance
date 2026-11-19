import { afterEach, describe, expect, it, vi } from 'vitest'

import {
  clampPetPosition,
  defaultPetPosition,
  loadPetCollapsed,
  loadPetPosition,
  petChipPage,
  petCollapsedKey,
  petDefaultModel,
  petGestureExceedsDrag,
  PET_CHIP_PAGE_SIZE,
  PET_COLLAPSED_KEY_PREFIX,
  PET_DRAG_THRESHOLD_PX,
  PET_POSITION_KEY_PREFIX,
  petObjectURL,
  petPositionKey,
  savePetCollapsed,
  savePetPosition,
  type PetPoint,
} from './canvasPet'

describe('canvasPet 手势与位置(92 号票)', () => {
  it('petGestureExceedsDrag separates click from drag by the threshold', () => {
    // 阈值内 = 点击(含正好压线:超过才算拖)。
    expect(petGestureExceedsDrag(0, 0)).toBe(false)
    expect(petGestureExceedsDrag(3, 4)).toBe(false) // 距离 5 < 6
    expect(petGestureExceedsDrag(PET_DRAG_THRESHOLD_PX, 0)).toBe(false)
    // 超阈值 = 拖动。
    expect(petGestureExceedsDrag(4, 5)).toBe(true) // 距离 ≈ 6.4 > 6
    expect(petGestureExceedsDrag(0, -10)).toBe(true)
    // 自定义阈值(组件可不传,测试钉缺省)。
    expect(petGestureExceedsDrag(10, 0, 12)).toBe(false)
  })

  it('defaultPetPosition parks the pet at the bottom-right with margin', () => {
    expect(defaultPetPosition({ width: 1000, height: 800 })).toEqual({
      x: 1000 - 72 - 24,
      y: 800 - 72 - 24,
    })
    // 极小画布:不小于内边距,不出负位。
    expect(defaultPetPosition({ width: 10, height: 10 })).toEqual({ x: 24, y: 24 })
  })

  it('clampPetPosition keeps the pet inside the canvas bounds', () => {
    const bounds = { width: 500, height: 400 }
    expect(clampPetPosition({ x: -20, y: -1 }, bounds)).toEqual({ x: 0, y: 0 })
    expect(clampPetPosition({ x: 9999, y: 9999 }, bounds)).toEqual({ x: 500 - 72, y: 400 - 72 })
    expect(clampPetPosition({ x: 100, y: 50 }, bounds)).toEqual({ x: 100, y: 50 })
    // 画布比桌宠还小:夹到原点。
    expect(clampPetPosition({ x: 30, y: 30 }, { width: 40, height: 40 })).toEqual({ x: 0, y: 0 })
  })

  it('petObjectURL builds the unauthenticated canvas object path with encoded key', () => {
    expect(petObjectURL('uploads/20261119/a.gif')).toBe('/api/pet/object?key=uploads%2F20261119%2Fa.gif')
  })
})

describe('canvasPet localStorage 存取(按画布隔离)', () => {
  const mem = new Map<string, string>()

  afterEach(() => {
    mem.clear()
    vi.unstubAllGlobals()
  })

  function stubStorage(): void {
    vi.stubGlobal('localStorage', {
      getItem: (k: string) => mem.get(k) ?? null,
      setItem: (k: string, v: string) => void mem.set(k, v),
      removeItem: (k: string) => void mem.delete(k),
    })
  }

  it('position keys carry the canvas id', () => {
    expect(petPositionKey(7)).toBe(`${PET_POSITION_KEY_PREFIX}7`)
    expect(petCollapsedKey(7)).toBe(`${PET_COLLAPSED_KEY_PREFIX}7`)
  })

  it('savePetPosition/loadPetPosition round-trips per canvas with clamping', () => {
    stubStorage()
    savePetPosition(7, { x: 120, y: 80 } satisfies PetPoint)
    savePetPosition(9, { x: 300, y: 10 } satisfies PetPoint)

    expect(loadPetPosition(7, { width: 1000, height: 800 })).toEqual({ x: 120, y: 80 })
    expect(loadPetPosition(9, { width: 1000, height: 800 })).toEqual({ x: 300, y: 10 })
    // 互不串台(键含画布 id)。
    expect(loadPetPosition(8, { width: 1000, height: 800 })).toEqual({ x: 1000 - 72 - 24, y: 800 - 72 - 24 })
    // 读出越界值夹回画布内。
    savePetPosition(10, { x: -5, y: 99999 } satisfies PetPoint)
    expect(loadPetPosition(10, { width: 500, height: 400 })).toEqual({ x: 0, y: 400 - 72 })
  })

  it('loadPetPosition falls back to the default on missing/corrupt values', () => {
    stubStorage()
    expect(loadPetPosition(3, { width: 1000, height: 800 })).toEqual({ x: 1000 - 72 - 24, y: 800 - 72 - 24 })
    mem.set(petPositionKey(3), '{not json')
    expect(loadPetPosition(3, { width: 1000, height: 800 })).toEqual({ x: 1000 - 72 - 24, y: 800 - 72 - 24 })
    mem.set(petPositionKey(3), '{"x":"bad"}')
    expect(loadPetPosition(3, { width: 1000, height: 800 })).toEqual({ x: 1000 - 72 - 24, y: 800 - 72 - 24 })
  })

  it('storage failures degrade silently (privacy mode)', () => {
    vi.stubGlobal('localStorage', {
      getItem: () => {
        throw new Error('denied')
      },
      setItem: () => {
        throw new Error('denied')
      },
      removeItem: () => {
        throw new Error('denied')
      },
    })
    expect(() => savePetPosition(1, { x: 1, y: 1 })).not.toThrow()
    expect(loadPetPosition(1, { width: 1000, height: 800 })).toEqual({ x: 1000 - 72 - 24, y: 800 - 72 - 24 })
    expect(loadPetCollapsed(1)).toBe(false)
    expect(() => savePetCollapsed(1, true)).not.toThrow()
  })

  it('savePetCollapsed/loadPetCollapsed round-trips the collapsed flag', () => {
    stubStorage()
    expect(loadPetCollapsed(5)).toBe(false)
    savePetCollapsed(5, true)
    expect(loadPetCollapsed(5)).toBe(true)
    expect(mem.get(petCollapsedKey(5))).toBe('1')
    savePetCollapsed(5, false)
    expect(loadPetCollapsed(5)).toBe(false)
    expect(mem.has(petCollapsedKey(5))).toBe(false)
  })
})

describe('canvasPet chips 轮换(面板「换一批」)', () => {
  const chips = ['a', 'b', 'c', 'd', 'e', 'f']

  it('petChipPage slices rotated pages of the configured size', () => {
    expect(PET_CHIP_PAGE_SIZE).toBe(4)
    expect(petChipPage(chips, 0)).toEqual(['a', 'b', 'c', 'd'])
    expect(petChipPage(chips, 1)).toEqual(['e', 'f', 'a', 'b'])
    expect(petChipPage(chips, 2)).toEqual(['c', 'd', 'e', 'f'])
    // 轮换是循环的:页码超出长度回绕。
    expect(petChipPage(chips, 6)).toEqual(['a', 'b', 'c', 'd'])
    // 负页码同样按公式回绕(左移 -4 ≡ 左移 2;防御,组件只增页)。
    expect(petChipPage(chips, -1)).toEqual(['c', 'd', 'e', 'f'])
  })

  it('petChipPage keeps small sets unchanged and handles empty input', () => {
    expect(petChipPage(['x', 'y'], 3)).toEqual(['x', 'y'])
    expect(petChipPage([], 0)).toEqual([])
    // 不改入参(照 appendAssistantTurn 纯函数纪律):轮换后再取第 0 批
    // 仍是原序。
    expect(petChipPage(chips, 1)).toEqual(['e', 'f', 'a', 'b'])
    expect(petChipPage(chips, 0)).toEqual(['a', 'b', 'c', 'd'])
    expect(chips).toEqual(['a', 'b', 'c', 'd', 'e', 'f'])
  })

  it('petDefaultModel prefers the configured model when in catalog, else the first entry', () => {
    const catalog = ['chat-a', 'chat-b']
    expect(petDefaultModel('chat-b', catalog)).toBe('chat-b')
    expect(petDefaultModel('not-in-catalog', catalog)).toBe('chat-a')
    expect(petDefaultModel('', catalog)).toBe('chat-a')
    expect(petDefaultModel('chat-a', [])).toBe('')
  })
})
