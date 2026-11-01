/**
 * 直通预填的装配层纯函数(71 号票 Wave-2):cut→视频(A2)与角色卡「生成登场
 * 视频」(D1)两条直通把 cutVideo / sheet / presets 的判定折成视频草稿(参考条
 * + 提示词)与轻提示,编辑器只做落节点、写草稿与出 toast。两条直通都只预
 * 填,发送仍人工确认(花钱操作总案 Q5;63 号票:花钱操作永不进 Agent 工具
 * 循环)。
 */
import type { VideoComposerRef } from './composer'
import type { CutVideoPrefill } from './cutVideo'
import { frameContentURL } from './storyboard'

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
