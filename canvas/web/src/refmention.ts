/**
 * 提示词 @ 引用(59 号票)的纯逻辑:各输入上下文「可 @ 条目」的派生、
 * chip 编号角标与引用 token 的构造。@ 的对象口径按上下文三态:
 *
 *  - 生成对话框图片模式:参考条全部条目,@图1..@图N(N = 参考条顺序);
 *  - 生成对话框视频模式:仅「参考图」角色,@图1..@图N(编号与网关
 *    references 数组序对齐;首帧/尾帧走单串 image/last_image 不占序,
 *    参考视频/参考音频上游无编号语义,均不可 @);
 *  - Agent 输入框:当前输入的图片与视频附件,图片 @图1..@图N、视频
 *    @视频1(各自插入序;会话历史媒体与未就绪附件不进)。
 *
 * token 是纯文本 —— 22 号票「顺序即参考序」的前端对应物,上游按提示词
 * 自由文本理解;悬空(参考被移除后编号漂移)不校验不警告,与手写「图N」
 * 的既有语义一致,同一参考可重复引用。
 */
import { VIDEO_ROLE_LABEL, type ComposerRef, type VideoComposerRef } from './composer'
import type { PendingAgentMedia } from './agentMedia'

/** 浮层里的一条可 @ 条目:token 是插入文本,kind 决定缩略(图)或徽章
 * (视频)展示,url 供缩略显示(走调用方的 useThumbFallback 派生)。 */
export interface MentionCandidate {
  token: string
  kind: 'image' | 'video'
  url: string
  /** 视频模式浮层的角色标签(恒「参考图」);其余上下文缺省。 */
  roleLabel?: string
}

/** 图片模式:参考条全部条目按序编号。 */
export function composerRefMentions(refs: ComposerRef[]): MentionCandidate[] {
  return refs.map((r, i) => ({ token: `@图${i + 1}`, kind: 'image' as const, url: r.url }))
}

/** 视频模式:仅「参考图」角色按其在参考图序列中的顺序编号 —— 编号即
 * references 数组下标,与上游契约严格对齐(44 号票「首帧切参考图」场景
 * 下编号不漂移)。 */
export function videoRefMentions(refs: VideoComposerRef[]): MentionCandidate[] {
  const out: MentionCandidate[] = []
  let n = 0
  for (const r of refs) {
    if (r.role !== 'reference_image') {
      continue
    }
    n += 1
    out.push({
      token: `@图${n}`,
      kind: 'image',
      url: r.url,
      roleLabel: VIDEO_ROLE_LABEL.reference_image,
    })
  }
  return out
}

/** Agent 输入框:待发附件按各自插入序编号(图片 @图N、视频 @视频N);
 * 上传中/失败的 chip 没有可引用素材,不进浮层也不编号。 */
export function agentAttachmentMentions(list: PendingAgentMedia[]): MentionCandidate[] {
  const out: MentionCandidate[] = []
  let img = 0
  let vid = 0
  for (const m of list) {
    if (m.state !== 'ready' || !m.ref) {
      continue
    }
    if (m.kind === 'image') {
      img += 1
      out.push({ token: `@图${img}`, kind: 'image', url: m.ref })
    } else {
      vid += 1
      out.push({ token: `@视频${vid}`, kind: 'video', url: m.ref })
    }
  }
  return out
}

/** 视频模式 chip 角标:仅「参考图」角色显示其在参考图序列中的序号
 * (其余角色 @ 不到,不产黑号);index 越界返回空串。 */
export function videoRefBadge(refs: VideoComposerRef[], index: number): string {
  const r = refs[index]
  if (!r || r.role !== 'reference_image') {
    return ''
  }
  let n = 0
  for (let i = 0; i <= index; i += 1) {
    if (refs[i].role === 'reference_image') {
      n += 1
    }
  }
  return String(n)
}

/** Agent 附件 chip 角标:就绪图片显示图片序号、就绪视频显示「视频N」;
 * 上传中/失败不编。找不到该 id 返回空串。 */
export function agentAttachmentBadge(list: PendingAgentMedia[], id: number): string {
  const m = list.find((x) => x.id === id)
  if (!m || m.state !== 'ready') {
    return ''
  }
  let n = 0
  for (const x of list) {
    if (x.state !== 'ready' || x.kind !== m.kind) {
      continue
    }
    n += 1
    if (x.id === id) {
      return m.kind === 'image' ? String(n) : `视频${n}`
    }
  }
  return ''
}

/** 触发判定:光标前一字符是 @ 则返回该字符下标,否则 -1(浮层的开与
 * 保活共用这一条判定 —— 光标离开 @ 位即收)。 */
export function mentionTriggerIndex(text: string, caret: number): number {
  return caret > 0 && text[caret - 1] === '@' ? caret - 1 : -1
}

/** 把触发字符(@)替换为 token:返回新文本与插入后的光标位置(token
 * 之后)。未选中就关闭时调用方不动文本,@ 留作普通字符。 */
export function insertMentionToken(
  text: string,
  triggerIndex: number,
  token: string,
): { text: string; cursor: number } {
  const next = text.slice(0, triggerIndex) + token + text.slice(triggerIndex + 1)
  return { text: next, cursor: triggerIndex + token.length }
}
