/**
 * cut→视频预填的纯逻辑(71 号票 A 节,修 70 号票 C 节已上线缺陷):参考条
 * 与首帧的预填判定,提示词拼装不在这里(仍由 storyboard.buildCutVideoPrompt
 * 负责)。
 *
 * 事实:方舟 Seedance 官方「图生视频-首帧、首尾帧、全模态参考生视频为 3 种
 * 互斥场景,不可混用」,MiniMax H3 原生与百炼同口径。70 号票把 cut 画面作
 * first_frame、角色设定图作 reference_image 一起预填,三家视频渠道必拒。
 * 修正:cut 有画面 = 画面作真首帧、不注入角色设定图(有出场角色时置跳过
 * 标记,wiring 层据此 toast);cut 无画面 = 按 character_ids 序注入角色设定
 * 图,以 B 节元素 chip 形态(element.type=character、变体 = 默认变体)进参考
 * 条,role 恒 reference_image。首帧不走 references 数组(不占序、不占 9 额度),
 * 单独以 firstFrameAssetId 返回,refs 里永不含首帧。
 */
import type { CharacterRecord } from '@infinitechance/api'

import type { StoryboardCut } from './graph'
import { defaultCharacterVariant, pickVariantAssetId } from './sheet'
import { assetContentURL } from './thumbnail'

/** 元素 chip 标注(71 号票 B2 钉死的形状,与 composer.ts 将加的 element
 * 字段同形):资产库实体在一次生成里的出场引用;名称不存快照,渲染与发送时
 * 按 entityId 从 IP 目录实时取。本模块只产 character 一类。 */
export interface CutRefElement {
  type: 'character' | 'scene' | 'prop'
  entityId: number
  variantId?: number
}

/** cut 预填注入的一条角色参考:结构兼容 composer.ts 的 VideoComposerRef
 * (url/kind/role/asset_id 同名同义),外加 element 元素标注。此处就地定义
 * 而不 import composer.ts —— 该文件正由另一分支加 element 字段,避免跨分支
 * 依赖;wiring 层可原样塞进 uploadedVideoRefs。 */
export interface CutVideoRef {
  url: string
  kind: 'image'
  role: 'reference_image'
  asset_id: number
  element: CutRefElement
}

/** cut→视频预填结果。 */
export interface CutVideoPrefill {
  /** cut 画面素材 id,0 = 无首帧(纯文生视频或多图参考)。wiring 层自行构造
   * first_frame 条目(走 image 单串,不进 references 数组)。 */
  firstFrameAssetId: number
  /** 注入参考条的角色元素 chip(role 恒 reference_image);有首帧时恒空。 */
  refs: CutVideoRef[]
  /** 超上限未带上的角色数(toast「N 个角色设定图未带上(参考上限 9)」)。 */
  dropped: number
  /** cut 有画面且标了出场角色 —— 因首帧/参考互斥未注入设定图(toast「cut
   * 已有画面作首帧,出场角色设定图未注入(首帧与参考不能混用)」)。 */
  skippedInject: boolean
}

/** cut→视频预填(71 号票 A2/A4)。只消费 cut 的 asset_id(0 = 无画面)与
 * character_ids;cap 是 reference_* 合计上限(VIDEO_REFS_TOTAL_CAP = 9),首帧
 * 不计入。取图走 sheet.ts 的「按变体取图」共享函数(默认变体,与角色卡登场
 * 直通同一规则);角色/变体无图静默跳过(70 号票语义),不计入 dropped。 */
export function buildCutVideoPrefill(
  cut: Pick<StoryboardCut, 'asset_id' | 'character_ids'>,
  characters: CharacterRecord[],
  cap: number,
): CutVideoPrefill {
  const characterIds = cut.character_ids ?? []
  if (cut.asset_id > 0) {
    // 有画面 = 真首帧;设定图一律不注入(互斥),出场角色非空时让 wiring 说明。
    return {
      firstFrameAssetId: cut.asset_id,
      refs: [],
      dropped: 0,
      skippedInject: characterIds.length > 0,
    }
  }
  const refs: CutVideoRef[] = []
  let dropped = 0
  for (const id of characterIds) {
    const c = characters.find((x) => x.id === id)
    if (!c) {
      continue
    }
    const variant = defaultCharacterVariant(c)
    const assetId = pickVariantAssetId(c, variant)
    if (assetId === 0) {
      continue
    }
    if (refs.length >= cap) {
      dropped += 1
      continue
    }
    refs.push({
      url: assetContentURL(assetId),
      kind: 'image',
      role: 'reference_image',
      asset_id: assetId,
      element: variant
        ? { type: 'character', entityId: id, variantId: variant.id }
        : { type: 'character', entityId: id },
    })
  }
  return { firstFrameAssetId: 0, refs, dropped, skippedInject: false }
}
