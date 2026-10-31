/**
 * 资产库三类实体的展示层共享口径(65 号票):场景/道具的 label 白名单、
 * 主资产判定与展示序。服务端存 label 字符串不校验枚举,白名单只是前端
 * 写入口(角色库 characters.ts 同纪律);主资产(场景「全景」、道具
 * 「多视图」)是画廊与封面排序置前的约定集,与服务端 scene.PrimaryLabels、
 * prop.PrimaryLabels 同集(两处各持一份)。
 */

import { CHARACTER_IMAGE_LABELS, isViewLabel } from './characters'


/** 资产库实体类别(资产库抽屉与 AssetPicker 分组共用)。 */
export type AssetKind = 'character' | 'scene' | 'prop'

/** 场景设定图标签的白名单(新建/改标签时的唯一入口,票面定案:全景/
 * 日景/夜景/局部/其他)。 */
export const SCENE_IMAGE_LABELS = ['全景', '日景', '夜景', '局部', '其他'] as const

/** 道具设定图标签的白名单(票面定案:多视图/细节/使用态/其他)。 */
export const PROP_IMAGE_LABELS = ['多视图', '细节', '使用态', '其他'] as const

/** 各类别设定图标签的白名单(资产库抽屉按当前类别取)。 */
export function imageLabelsForKind(kind: AssetKind): readonly string[] {
  if (kind === 'scene') {
    return SCENE_IMAGE_LABELS
  }
  if (kind === 'prop') {
    return PROP_IMAGE_LABELS
  }
  return CHARACTER_IMAGE_LABELS
}

/** 主资产判定:场景「全景」是定场视图、道具「多视图」是设定主画,展示
 * 面排序置前(角色沿用视图类三视图/多视图口径)。 */
export function isPrimaryLabel(kind: AssetKind, label: string): boolean {
  if (kind === 'scene') {
    return label === '全景'
  }
  if (kind === 'prop') {
    return label === '多视图'
  }
  return isViewLabel(label)
}

/** 展示序:主资产置前,两组内保持 sort+id 原序(角色库 orderedCharacterImages
 * 同规则;场景/道具图行共用)。 */
export function orderedImages<K extends { label: string }>(kind: AssetKind, images: K[]): K[] {
  return [...images.filter((i) => isPrimaryLabel(kind, i.label)), ...images.filter((i) => !isPrimaryLabel(kind, i.label))]
}
