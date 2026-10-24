/**
 * 角色库展示层的共享口径(51 号票):label 白名单、视图类判定与设定图的
 * 内容寻址派生。服务端存 label 字符串不校验枚举,白名单只是前端写入口;
 * 视图类(三视图/多视图)是主资产,画廊与封面排序置前(服务端 cover_asset_id
 * 投影同规则,两侧以同一集合为准)。
 */

import type { CharacterImageRecord } from '@infinitechance/api'

/** 设定图标签的六类白名单(新建/改标签时的唯一入口)。 */
export const CHARACTER_IMAGE_LABELS = ['三视图', '多视图', '表情集', '色板', '姿态', '其他'] as const

/** 视图类 label 集合:主资产,展示面排序置前。与服务端 character.ViewLabels
 * 同集(两处各持一份,后端不校验枚举,此处是唯一写入口)。 */
export function isViewLabel(label: string): boolean {
  return label === '三视图' || label === '多视图'
}

/** 设定图 → 内容寻址路径(服务端 302 thumb 端点的显示面派生,49 号票
 * 同款规则;素材行被删时两地址都会 404,展示面落「素材不可用」占位)。 */
export function characterImageContentURL(assetId: number): string {
  return `/api/assets/${assetId}/content`
}

export function characterImageThumbURL(assetId: number): string {
  return `/api/assets/${assetId}/thumb`
}

/** 展示序:视图类置前,两组内保持 sort+id 原序(51 号票主资产口径;
 * 角色引用节点的变体画廊与详情抽屉共用同一规则)。 */
export function orderedCharacterImages(images: CharacterImageRecord[]): CharacterImageRecord[] {
  return [...images.filter((i) => isViewLabel(i.label)), ...images.filter((i) => !isViewLabel(i.label))]
}

/** Agent 角色 chip 的纯切换(52 号票):点选幂等加入(追加在选择序末尾)、
 * 再点移除;返回新数组不改动入参(与 appendAgentTurn 同款纪律)。 */
export function toggleCharacterChip(ids: number[], characterId: number): number[] {
  if (ids.includes(characterId)) {
    return ids.filter((id) => id !== characterId)
  }
  return [...ids, characterId]
}
