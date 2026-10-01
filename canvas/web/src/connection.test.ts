import { describe, expect, it } from 'vitest'

import { EDGE_MATRIX, canConnect } from './connection'

describe('canConnect', () => {
  it('admits the four semantic edge kinds', () => {
    // 四种语义:投递(agent→媒体)、反推(视频→agent)、分析(媒体→分析)、
    // 迭代(媒体→媒体)。
    expect(canConnect('agent', 'image')).toBe(true)
    expect(canConnect('agent', 'video')).toBe(true)
    expect(canConnect('video', 'agent')).toBe(true)
    expect(canConnect('image', 'analysis')).toBe(true)
    expect(canConnect('video', 'analysis')).toBe(true)
    expect(canConnect('image', 'image')).toBe(true)
    expect(canConnect('image', 'video')).toBe(true)
    expect(canConnect('video', 'image')).toBe(true)
    expect(canConnect('video', 'video')).toBe(true)
  })

  it('rejects every other combination', () => {
    expect(canConnect('agent', 'agent')).toBe(false)
    expect(canConnect('agent', 'analysis')).toBe(false)
    expect(canConnect('analysis', 'agent')).toBe(false)
    expect(canConnect('analysis', 'image')).toBe(false)
    expect(canConnect('analysis', 'analysis')).toBe(false)
  })

  it('rejects unknown or missing types', () => {
    expect(canConnect(undefined, 'image')).toBe(false)
    expect(canConnect('agent', undefined)).toBe(false)
    expect(canConnect('mystery', 'image')).toBe(false)
  })

  it('keeps analysis out of the source seat entirely', () => {
    expect(EDGE_MATRIX.analysis).toEqual([])
  })
})
