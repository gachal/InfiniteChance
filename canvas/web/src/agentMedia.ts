/**
 * Agent 会话的媒体附件(32 号票):发送前的待发附件纯函数 —— 上限纪律与
 * 引用收集。待发附件是 UI 态(不入节点 data / 整图 JSON):选中/挑选即
 * 上传,上传中/失败的 chip 留在输入区逐个可移除,发送时只带上传成功的
 * 引用,发送成功即清空进历史,换技能/开新会话连同未发送附件一并清。
 */
import type { AgentChatMedia } from '@infinitechance/api'

/** 单条消息附件上限(32 号票):≤4 图 + ≤1 视频,混合允许 —— 图片对齐
 * 21 号票图生图 image_urls,视频对齐 24 号票参考视频(聊天轨视频分节
 * 计价贵,1 条封顶是成本护栏);与服务端 normalizeTurnMedia 同宽。 */
export const AGENT_MEDIA_CAPS = { image: 4, video: 1 } as const

export type AgentMediaKind = keyof typeof AGENT_MEDIA_CAPS

/** 一条待发附件:上传成功(ready)后才持有素材内容寻址引用;assetId 是
 * 素材库 id,上传与库选两路一致,素材选择面板按它显示已选态。 */
export interface PendingAgentMedia {
  id: number
  kind: AgentMediaKind
  state: 'uploading' | 'ready' | 'failed'
  /** 素材内容寻址路径(ready 态必有)。 */
  ref?: string
  assetId?: number
  /** 上传失败原因(chip 上直接展示)。 */
  error?: string
}

let seq = 0

/** 下一附件 id(UI 态序号,仅用于列表 key 与移除定位)。 */
export function nextAgentMediaId(): number {
  seq += 1
  return seq
}

/** 该种类还有没有空位:第 5 张图 / 第 2 个视频在两个入口都直接拒收
 * (不传字节不占位,与服务端超限整单拒绝的前置纪律一致)。 */
export function hasAgentMediaSlot(list: PendingAgentMedia[], kind: AgentMediaKind): boolean {
  return list.filter((m) => m.kind === kind).length < AGENT_MEDIA_CAPS[kind]
}

/** 发送时只传引用:上传成功的待发附件 → 服务端 media 字段形状(内容
 * 寻址路径 + kind,与历史轮引用形状一致)。 */
export function readyAgentMediaRefs(list: PendingAgentMedia[]): AgentChatMedia[] {
  return list
    .filter((m): m is PendingAgentMedia & { ref: string } => m.state === 'ready' && !!m.ref)
    .map((m) => ({ ref: m.ref, kind: m.kind }))
}

/** 附件还在上传中时不可发送:引用没就绪,发出去的媒体不完整。 */
export function isAgentMediaUploading(list: PendingAgentMedia[]): boolean {
  return list.some((m) => m.state === 'uploading')
}
