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

import type { VideoComposerRef } from './composer'
import { characterElement } from './element'
import type { StoryboardCut } from './graph'
import { buildPaletteEntries, buildPaletteLine } from './palette'
import { pickCutCharacterRefs } from './sheet'
import { assetContentURL } from './thumbnail'

/** cut→视频预填结果。refs 即 composer.ts 的 VideoComposerRef(元素 chip
 * 形态,element 恒 character;名称不存快照,渲染与发送时按 entityId 从 IP
 * 目录实时取),wiring 层可原样塞进 uploadedVideoRefs。 */
export interface CutVideoPrefill {
  /** cut 画面素材 id,0 = 无首帧(纯文生视频或多图参考)。wiring 层自行构造
   * first_frame 条目(走 image 单串,不进 references 数组)。 */
  firstFrameAssetId: number
  /** 注入参考条的角色元素 chip(role 恒 reference_image);有首帧时恒空。 */
  refs: VideoComposerRef[]
  /** 超上限未带上的角色数(toast「N 个角色设定图未带上(参考上限 9)」)。 */
  dropped: number
  /** cut 有画面且标了出场角色 —— 因首帧/参考互斥未注入设定图(toast「cut
   * 已有画面作首帧,出场角色设定图未注入(首帧与参考不能混用)」)。 */
  skippedInject: boolean
  /** 角色配色段(84 号票 B3):无画面路径按绑定变体拼(palette.
   * buildPaletteLine 产物,无配色角色 = 空串),wiring 层传给
   * buildCutVideoPrompt;有画面路径恒空串(71 号票互斥不注入,84 不碰)。 */
  paletteLine: string
}

/** cut→视频预填(71 号票 A2/A4;83 号票取图规则更新;84 号票配色段)。
 * 只消费 cut 的 asset_id(0 = 无画面)、character_ids 与 character_variants
 * (83 号票变体绑定映射,可缺省);cap 是 reference_* 合计上限
 * (VIDEO_REFS_TOTAL_CAP = 9),首帧不计入。角色挑选整体复用
 * sheet.pickCutCharacterRefs(**cut 绑定变体优先 → 默认变体回落**,83 号票 +
 * 无图静默跳过 + 上限截断计数,与 cut→图片直通同一实现;变体绑定只影响
 * 「取哪张参考」,不影响「注不注入」—— 有画面路径维持不注入,71 号票互斥
 * 硬约束);元素标注带命中变体的 variantId,角色无变体(防御路径)不带。
 * 配色段与参考图挑选同源解析(同一绑定映射、同一回落规则)但互不依赖 ——
 * 无图角色的配色照拼,参考条截断不影响配色行。 */
export function buildCutVideoPrefill(
  cut: Pick<StoryboardCut, 'asset_id' | 'character_ids' | 'character_variants'>,
  characters: CharacterRecord[],
  cap: number,
): CutVideoPrefill {
  const characterIds = cut.character_ids ?? []
  if (cut.asset_id > 0) {
    // 有画面 = 真首帧;设定图与配色段一律不注入(互斥,84 不碰有画面
    // 路径),出场角色非空时让 wiring 说明。
    return {
      firstFrameAssetId: cut.asset_id,
      refs: [],
      dropped: 0,
      skippedInject: characterIds.length > 0,
      paletteLine: '',
    }
  }
  const { picks, dropped } = pickCutCharacterRefs(characters, characterIds, cap, cut.character_variants)
  const refs: VideoComposerRef[] = picks.map((p) => ({
    url: assetContentURL(p.assetId),
    kind: 'image',
    role: 'reference_image',
    asset_id: p.assetId,
    element: characterElement(p.characterId, p.variantId),
  }))
  const paletteLine = buildPaletteLine(buildPaletteEntries(characters, characterIds, cut.character_variants))
  return { firstFrameAssetId: 0, refs, dropped, skippedInject: false, paletteLine }
}
