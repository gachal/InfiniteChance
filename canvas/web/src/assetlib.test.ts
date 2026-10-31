// 资产库展示层口径的钉死断言(65 号票):白名单是前端唯一写入口,与服务
// 端 scene.PrimaryLabels / prop.PrimaryLabels 以同一集合为准 —— 白名单
// 漂移会让封面投影与展示序两侧口径分裂,故逐值钉死。
import { describe, expect, it } from 'vitest'

import {
  PROP_IMAGE_LABELS,
  SCENE_IMAGE_LABELS,
  imageLabelsForKind,
  isPrimaryLabel,
  orderedImages,
} from './assetlib'

describe('assetlib labels', () => {
  it('scene whitelist is pinned to the ticketed five labels', () => {
    expect([...SCENE_IMAGE_LABELS]).toEqual(['全景', '日景', '夜景', '局部', '其他'])
  })

  it('prop whitelist is pinned to the ticketed four labels', () => {
    expect([...PROP_IMAGE_LABELS]).toEqual(['多视图', '细节', '使用态', '其他'])
  })

  it('returns the whitelist of the requested kind', () => {
    expect(imageLabelsForKind('scene')).toBe(SCENE_IMAGE_LABELS)
    expect(imageLabelsForKind('prop')).toBe(PROP_IMAGE_LABELS)
    expect(imageLabelsForKind('character')).toHaveLength(6)
  })
})

describe('isPrimaryLabel', () => {
  it('scene primary is 全景 only', () => {
    expect(isPrimaryLabel('scene', '全景')).toBe(true)
    expect(isPrimaryLabel('scene', '日景')).toBe(false)
    expect(isPrimaryLabel('scene', '')).toBe(false)
  })

  it('prop primary is 多视图 only', () => {
    expect(isPrimaryLabel('prop', '多视图')).toBe(true)
    expect(isPrimaryLabel('prop', '细节')).toBe(false)
    // 注意:多视图对角色也是视图类,但按传入类别判定 —— 类别不串。
    expect(isPrimaryLabel('prop', '三视图')).toBe(false)
  })

  it('character primary stays the view-label pair', () => {
    expect(isPrimaryLabel('character', '三视图')).toBe(true)
    expect(isPrimaryLabel('character', '多视图')).toBe(true)
    expect(isPrimaryLabel('character', '色板')).toBe(false)
  })
})

describe('orderedImages', () => {
  const img = (id: number, label: string) => ({ id, label })

  it('puts primary labels first, keeping sort+id order inside both groups', () => {
    const images = [img(1, '日景'), img(2, '全景'), img(3, '局部'), img(4, '夜景')]
    expect(orderedImages<{ id: number; label: string }>('scene', images)).toEqual([
      img(2, '全景'),
      img(1, '日景'),
      img(3, '局部'),
      img(4, '夜景'),
    ])
  })

  it('keeps original order when nothing is primary', () => {
    const images = [img(1, '细节'), img(2, '使用态')]
    expect(orderedImages('prop', images)).toEqual([img(1, '细节'), img(2, '使用态')])
  })

  it('answers an empty array for empty input', () => {
    expect(orderedImages('scene', [])).toEqual([])
  })
})
