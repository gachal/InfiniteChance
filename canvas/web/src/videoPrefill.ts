/**
 * 直通预填的装配层纯函数(71 号票 Wave-2):cut→视频(A2)与角色卡「生成登场
 * 视频」(D1)两条直通把 cutVideo / sheet / presets 的判定折成视频草稿(参考条
 * + 提示词)与轻提示,编辑器只做落节点、写草稿与出 toast。两条直通都只预
 * 填,发送仍人工确认(花钱操作总案 Q5;63 号票:花钱操作永不进 Agent 工具
 * 循环)。
 */
import type { CharacterRecord, CharacterVariantRecord } from '@infinitechance/api'

import type { ComposerElement, VideoComposerRef } from './composer'
import type { CutVideoPrefill } from './cutVideo'
import { prependStyleSegment } from './ipStyle'
import { fillPreset, GAMEPLAY_PRESET_TEMPLATES, GAMEPLAY_PRESETS } from './presets'
import { pickVariantAssetId } from './sheet'
import { frameContentURL } from './storyboard'
import { assetContentURL } from './thumbnail'

/** cut→视频预填的参考条(整条替换上传草稿):有画面 = 画面作真首帧(走
 * image 单串,不进 references、不占 9 额度),此时 refs 恒空 —— 首帧与全模态
 * 参考互斥(Seedance / MiniMax H3 三家必拒混用);无画面 = 角色设定图元素
 * chip(恒参考图)。 */
export function cutPrefillVideoRefs(prefill: CutVideoPrefill): VideoComposerRef[] {
  const frame: VideoComposerRef[] =
    prefill.firstFrameAssetId > 0
      ? [
          {
            url: frameContentURL(prefill.firstFrameAssetId),
            kind: 'image',
            role: 'first_frame',
            asset_id: prefill.firstFrameAssetId,
          },
        ]
      : []
  return [...frame, ...prefill.refs]
}

/** cut→视频预填的轻提示:因首帧跳过注入时说明原因(优先),否则报超上限
 * 未带上的角色数;都没有为空串。 */
export function cutPrefillNotice(prefill: CutVideoPrefill): string {
  if (prefill.skippedInject) {
    return 'cut 已有画面作首帧,出场角色设定图未注入(首帧与参考不能混用)'
  }
  if (prefill.dropped > 0) {
    return `${prefill.dropped} 个角色设定图未带上(参考上限 9)`
  }
  return ''
}

/** 角色卡「生成登场视频」的视频草稿预填。 */
export interface EntrancePrefill {
  /** 整条替换的参考条:该角色当前变体的元素 chip(恒参考图);取不到图为空。 */
  refs: VideoComposerRef[]
  /** 提示词:「单角色登场」玩法预设填该角色名,IP 风格段拼最前。 */
  prompt: string
  /** 轻提示:变体与角色都无图时建议先生成设定图;否则空串。 */
  notice: string
}

/** 「单角色登场」模板(按 key 取,定稿顺序变动不受影响)。 */
const SOLO_ENTRANCE_TEMPLATE =
  GAMEPLAY_PRESETS.find((p) => p.key === 'solo-entrance')?.template ?? GAMEPLAY_PRESET_TEMPLATES[0]

/** 角色卡「生成登场视频」直通预填(71 号票 D1):取图走 sheet.pickVariantAssetId
 * (该变体最新视图类 > 该变体任意设定图 > 角色封面投影 —— 与 cut 出场角色注入
 * 共用,同角色同变体两路同图);元素 = 该角色 + 卡片当前变体(变体缺省的防御
 * 路径不带 variantId);提示词 = prependStyleSegment(单角色登场模板填名, 风格)
 * (69 号票直通先例:风格段拼最前),场景缺失按 fillPreset 规则省略从句。
 * 时长/比例/分辨率不在预填范围。 */
export function buildEntrancePrefill(
  character: Pick<CharacterRecord, 'id' | 'name' | 'cover_asset_id'>,
  variant: Pick<CharacterVariantRecord, 'id' | 'images'> | undefined,
  style: string | null | undefined,
): EntrancePrefill {
  const assetId = pickVariantAssetId(character, variant)
  const element: ComposerElement = variant
    ? { type: 'character', entityId: character.id, variantId: variant.id }
    : { type: 'character', entityId: character.id }
  const refs: VideoComposerRef[] =
    assetId > 0
      ? [{ url: assetContentURL(assetId), kind: 'image', role: 'reference_image', asset_id: assetId, element }]
      : []
  return {
    refs,
    prompt: prependStyleSegment(fillPreset(SOLO_ENTRANCE_TEMPLATE, { role1: character.name }), style),
    notice: assetId > 0 ? '' : '该造型还没有设定图,建议先生成设定图',
  }
}
