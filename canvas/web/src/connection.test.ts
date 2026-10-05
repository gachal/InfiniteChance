import { describe, expect, it } from 'vitest'

import { EDGE_MATRIX, canConnect } from './connection'

describe('canConnect', () => {
  it('admits the five semantic edge kinds', () => {
    // 五种语义:投递(agent→媒体)、反推(视频→agent)、分析(媒体→分析)、
    // 迭代(媒体→媒体)、渲染帧落点(预演台→图片,39 号票)。
    expect(canConnect('agent', 'image')).toBe(true)
    expect(canConnect('agent', 'video')).toBe(true)
    expect(canConnect('video', 'agent')).toBe(true)
    expect(canConnect('image', 'analysis')).toBe(true)
    expect(canConnect('video', 'analysis')).toBe(true)
    expect(canConnect('image', 'image')).toBe(true)
    expect(canConnect('image', 'video')).toBe(true)
    expect(canConnect('video', 'image')).toBe(true)
    expect(canConnect('video', 'video')).toBe(true)
    expect(canConnect('previz', 'image')).toBe(true)
  })

  it('rejects every other combination', () => {
    expect(canConnect('agent', 'agent')).toBe(false)
    expect(canConnect('agent', 'analysis')).toBe(false)
    expect(canConnect('analysis', 'agent')).toBe(false)
    expect(canConnect('analysis', 'image')).toBe(false)
    expect(canConnect('analysis', 'analysis')).toBe(false)
    // 预演台只落图片节点:→video 与一切指向预演台的连线都非法(39 号票,
    // 预演台不作任何连线的目标)。
    expect(canConnect('previz', 'video')).toBe(false)
    expect(canConnect('previz', 'previz')).toBe(false)
    expect(canConnect('previz', 'analysis')).toBe(false)
    expect(canConnect('previz', 'agent')).toBe(false)
    expect(canConnect('agent', 'previz')).toBe(false)
    expect(canConnect('image', 'previz')).toBe(false)
    expect(canConnect('video', 'previz')).toBe(false)
    expect(canConnect('analysis', 'previz')).toBe(false)
  })

  it('rejects unknown or missing types', () => {
    expect(canConnect(undefined, 'image')).toBe(false)
    expect(canConnect('agent', undefined)).toBe(false)
    expect(canConnect('mystery', 'image')).toBe(false)
  })

  it('keeps analysis out of the source seat entirely', () => {
    expect(EDGE_MATRIX.analysis).toEqual([])
  })

  it('lets previz feed image nodes only, and never receive an edge', () => {
    expect(EDGE_MATRIX.previz).toEqual(['image'])
    for (const source of Object.keys(EDGE_MATRIX) as (keyof typeof EDGE_MATRIX)[]) {
      expect(EDGE_MATRIX[source]).not.toContain('previz')
    }
  })
})
