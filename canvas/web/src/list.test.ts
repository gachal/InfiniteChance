import { describe, expect, it } from 'vitest'

import { followMove, followRemove, moveItem, removeAt } from './list'

describe('removeAt', () => {
  it('removes the item at the index and keeps the rest in order', () => {
    expect(removeAt(['a', 'b', 'c'], 1)).toEqual(['a', 'c'])
    expect(removeAt(['a', 'b', 'c'], 0)).toEqual(['b', 'c'])
    expect(removeAt(['a', 'b', 'c'], 2)).toEqual(['a', 'b'])
  })

  it('returns the same content for out-of-range indexes', () => {
    expect(removeAt(['a', 'b'], -1)).toEqual(['a', 'b'])
    expect(removeAt(['a', 'b'], 2)).toEqual(['a', 'b'])
    expect(removeAt([], 0)).toEqual([])
  })

  it('never mutates the input (数组换新纪律)', () => {
    const input = ['a', 'b', 'c']
    const next = removeAt(input, 1)
    expect(input).toEqual(['a', 'b', 'c'])
    expect(next).not.toBe(input)
  })
})

describe('moveItem', () => {
  it('swaps the item with its neighbor (delta = -1 | 1)', () => {
    expect(moveItem(['a', 'b', 'c'], 1, -1)).toEqual(['b', 'a', 'c'])
    expect(moveItem(['a', 'b', 'c'], 1, 1)).toEqual(['a', 'c', 'b'])
    expect(moveItem(['a', 'b', 'c'], 0, 1)).toEqual(['b', 'a', 'c'])
    expect(moveItem(['a', 'b', 'c'], 2, -1)).toEqual(['a', 'c', 'b'])
  })

  it('returns the original array untouched at the edges or on empty lists', () => {
    const items = ['a', 'b']
    expect(moveItem(items, 0, -1)).toBe(items)
    expect(moveItem(items, 1, 1)).toBe(items)
    expect(moveItem(items, -1, 1)).toBe(items)
    expect(moveItem(items, 2, -1)).toBe(items)
    const empty: string[] = []
    expect(moveItem(empty, 0, 1)).toBe(empty)
  })

  it('never mutates the input (数组换新纪律)', () => {
    const input = ['a', 'b', 'c']
    const next = moveItem(input, 0, 1)
    expect(input).toEqual(['a', 'b', 'c'])
    expect(next).not.toBe(input)
  })
})

describe('followRemove', () => {
  it('closes the picker when its own row is removed', () => {
    expect(followRemove(2, 2)).toBe(null)
  })

  it('decrements when a row before the picker is removed (评审修复)', () => {
    // 删更早的行:面板行前移一位,不能静默改绑下一行。
    expect(followRemove(5, 2)).toBe(4)
    expect(followRemove(1, 0)).toBe(0)
  })

  it('keeps the index when the removed row is after the picker', () => {
    expect(followRemove(1, 3)).toBe(1)
  })

  it('passes null through', () => {
    expect(followRemove(null, 0)).toBe(null)
  })
})

describe('followMove', () => {
  it('follows the moved row to its new position', () => {
    expect(followMove(2, 2, -1)).toBe(1)
    expect(followMove(2, 2, 1)).toBe(3)
  })

  it('swaps the index when the picker sits on the displaced neighbor (评审修复)', () => {
    // 被移行跳过面板所在行:两者换位,面板跟随自己的行而不是被移行。
    expect(followMove(1, 2, -1)).toBe(2)
    expect(followMove(3, 2, 1)).toBe(2)
  })

  it('keeps unrelated rows untouched', () => {
    expect(followMove(0, 2, -1)).toBe(0)
    expect(followMove(4, 2, 1)).toBe(4)
  })
})
