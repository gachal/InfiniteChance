import { describe, expect, it } from 'vitest'

import { EDGE_MATRIX, canConnect } from './connection'

describe('canConnect', () => {
  it('admits the seven semantic edge kinds', () => {
    // 七种语义:投递(agent→媒体)、反推(视频→agent)、分析(媒体→分析)、
    // 迭代(媒体→媒体)、渲染帧落点(预演台→图片,39 号票)、分镜画面落点
    // (絵コンテ→图片,53 号票)、格画面落点(ネーム→图片,54 号票);
    // 絵コンテ/ネーム两条在下方各票专属用例里单独断言。
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

  it('keeps the character node out of the edge matrix entirely (52 号票)', () => {
    // 角色引用节点 = 钉在画布上的设定参照卡:不作任何连线的源与目标
    // (组件也不渲染把手),「本镜头主角」类语义显式不做。
    expect(EDGE_MATRIX.character).toEqual([])
    expect(canConnect('character', 'image')).toBe(false)
    expect(canConnect('image', 'character')).toBe(false)
    expect(canConnect('agent', 'character')).toBe(false)
    expect(canConnect('character', 'character')).toBe(false)
    for (const source of Object.keys(EDGE_MATRIX) as (keyof typeof EDGE_MATRIX)[]) {
      expect(EDGE_MATRIX[source]).not.toContain('character')
    }
  })

  it('lets previz feed image nodes only, and never receive an edge', () => {
    expect(EDGE_MATRIX.previz).toEqual(['image'])
    for (const source of Object.keys(EDGE_MATRIX) as (keyof typeof EDGE_MATRIX)[]) {
      expect(EDGE_MATRIX[source]).not.toContain('previz')
    }
  })

  it('lets storyboard feed image and video nodes, and never receive an edge (53/67 号票)', () => {
    // 絵コンテ落图 = 来源语义(预演台 → 图片同族):cut 画面落成图片节点
    // 并连线;67 号票增补 cut→视频直通来源边(storyboard → video)。作任何
    // 连线的目标都非法 —— 画面槽的回填走素材引用,不走边。
    expect(EDGE_MATRIX.storyboard).toEqual(['image', 'video'])
    expect(canConnect('storyboard', 'image')).toBe(true)
    expect(canConnect('storyboard', 'video')).toBe(true)
    expect(canConnect('storyboard', 'analysis')).toBe(false)
    expect(canConnect('storyboard', 'agent')).toBe(false)
    expect(canConnect('storyboard', 'storyboard')).toBe(false)
    for (const source of Object.keys(EDGE_MATRIX) as (keyof typeof EDGE_MATRIX)[]) {
      expect(EDGE_MATRIX[source]).not.toContain('storyboard')
    }
  })

  it('lets name feed image nodes only, and never receive an edge (54 号票)', () => {
    // ネーム落图与絵コンテ同族(来源语义):格画面落成图片节点并连线;
    // 回填走素材引用不走边,作任何连线的目标都非法。
    expect(EDGE_MATRIX.name).toEqual(['image'])
    expect(canConnect('name', 'image')).toBe(true)
    expect(canConnect('name', 'video')).toBe(false)
    expect(canConnect('name', 'analysis')).toBe(false)
    expect(canConnect('name', 'agent')).toBe(false)
    expect(canConnect('name', 'name')).toBe(false)
    expect(canConnect('name', 'storyboard')).toBe(false)
    for (const source of Object.keys(EDGE_MATRIX) as (keyof typeof EDGE_MATRIX)[]) {
      expect(EDGE_MATRIX[source]).not.toContain('name')
    }
  })

  it('keeps the level node out of the edge matrix entirely (55 号票)', () => {
    // 关卡卡 = 文档型工件(55 号票):卡内直接编辑,无来源/投递语义,
    // 与角色引用节点的「不连线」同族 —— 不作任何连线的源与目标,组件
    // 也不渲染把手;画面参考走素材引用不走边。
    expect(EDGE_MATRIX.level).toEqual([])
    expect(canConnect('level', 'image')).toBe(false)
    expect(canConnect('level', 'agent')).toBe(false)
    expect(canConnect('level', 'level')).toBe(false)
    expect(canConnect('agent', 'level')).toBe(false)
    for (const source of Object.keys(EDGE_MATRIX) as (keyof typeof EDGE_MATRIX)[]) {
      expect(EDGE_MATRIX[source]).not.toContain('level')
    }
  })
})

describe('voice → audio(57 号票)', () => {
  it('admits only the synthesis-source edge', () => {
    // 音色→音频是台词合成的产出边(絵コンテ→图片同族来源语义);音频
    // 不作任何连线的源,音色不作任何连线的目标。
    expect(canConnect('voice', 'audio')).toBe(true)
    expect(canConnect('voice', 'image')).toBe(false)
    expect(canConnect('voice', 'video')).toBe(false)
    expect(canConnect('voice', 'voice')).toBe(false)
    expect(canConnect('audio', 'voice')).toBe(false)
    expect(canConnect('audio', 'image')).toBe(false)
    expect(canConnect('audio', 'audio')).toBe(false)
    expect(canConnect('image', 'audio')).toBe(false)
    expect(canConnect('voice', 'character')).toBe(false)
  })

  it('keeps the matrix total over the twelve node types', () => {
    // 矩阵键齐全(缺键会在 canConnect 里静默拒连):12 种节点各有条目
    // (64 号票起 script 入册)。
    expect(Object.keys(EDGE_MATRIX).sort()).toEqual(
      ['agent', 'analysis', 'audio', 'character', 'image', 'level', 'name', 'previz', 'script', 'storyboard', 'video', 'voice'].sort(),
    )
  })
})

describe('script 节点连线(64 号票)', () => {
  it('keeps the script node out of the edge matrix entirely', () => {
    // 剧本 = 文本载体卡:不作任何连线的源与目标(照角色卡/关卡卡先例),
    // 内容经 Agent 工具读写不靠边(66 号票 read_source 分段消费);组件
    // 也不渲染把手。Record 全键类型缺键即编译失败,此处钉运行时形状。
    expect(EDGE_MATRIX.script).toEqual([])
    expect(canConnect('script', 'image')).toBe(false)
    expect(canConnect('script', 'agent')).toBe(false)
    expect(canConnect('script', 'script')).toBe(false)
    expect(canConnect('agent', 'script')).toBe(false)
    expect(canConnect('image', 'script')).toBe(false)
    for (const source of Object.keys(EDGE_MATRIX) as (keyof typeof EDGE_MATRIX)[]) {
      expect(EDGE_MATRIX[source]).not.toContain('script')
    }
  })
})
