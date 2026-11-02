/**
 * 提示词 @ 引用(59 号票)的纯逻辑:各输入上下文「可 @ 条目」的派生、
 * chip 编号角标与引用 token 的构造。@ 的对象口径按上下文三态:
 *
 *  - 生成对话框图片模式:参考条全部条目,@图片1..@图片N(N = 参考条顺序);
 *  - 生成对话框视频模式:仅「参考图」角色,@图片1..@图片N(编号与网关
 *    references 数组序对齐;首帧/尾帧走单串 image/last_image 不占序,
 *    参考视频/参考音频上游无编号语义,均不可 @);
 *  - Agent 输入框:当前输入的图片与视频附件,图片 @图片1..@图片N、视频
 *    @视频1(各自插入序;会话历史媒体与未就绪附件不进)。
 *
 * token 是纯文本 —— 22 号票「顺序即参考序」的前端对应物,上游按提示词
 * 自由文本理解;悬空(参考被移除后编号漂移)不校验不警告,与手写
 * 「图片N」的既有语义一致,同一参考可重复引用(73 号票:「图N」指代
 * 换形「图片N」,对齐 Seedance 官方文档)。
 */
import { VIDEO_ROLE_LABEL, type ComposerElement, type ComposerRef, type VideoComposerRef } from './composer'
import type { PendingAgentMedia } from './agentMedia'

/** 浮层里的一条可 @ 条目:token 是插入文本,kind 决定缩略(图)或徽章
 * (视频)展示,url 供缩略显示(走调用方的 useThumbFallback 派生)。 */
export interface MentionCandidate {
  token: string
  kind: 'image' | 'video'
  url: string
  /** 视频模式浮层的角色标签(恒「参考图」);其余上下文缺省。 */
  roleLabel?: string
  /** 元素 chip 的显示名与类型(71 号票):有名元素在浮层条目上显示名称与
   * 类型小标;实体/变体已删退化无名的 chip 与普通参考图两者皆缺省。 */
  elementName?: string
  elementType?: ComposerElement['type']
}

/** 元素 chip 的显示字段拼装:名字解析成功才带(名称 + 类型成对出现,
 * 退化无名 = 与普通参考图同形)。 */
function withElementDisplay(
  candidate: MentionCandidate,
  name: string | null | undefined,
  el?: ComposerElement,
): MentionCandidate {
  if (!el || name == null || name === '') {
    return candidate
  }
  return { ...candidate, elementName: name, elementType: el.type }
}

/** 「图片N」编号的「图片」字面:绑定行、chip 角标、@ 浮层 token 共用这
 * 一个常量 —— Seedance 官方文档指代写作「图片N」,73 号票定案 D.3 由
 * 旧值「图」换形收编(71 号票 B5 留的口子由本票落定);将来真 key 实测
 * 再有差异时只改这里。测试钉住了各处的一致性,不会被静默改漂。 */
export const REF_INDEX_LABEL = '图片'

/** 各上下文的「图片N」结构化编号(不从 token 反解):图片模式 = 参考条全部
 * 条目按序 1..N;视频模式 = 仅 reference_image 角色按其在参考图序列中的顺序
 * 编号(即网关 references 数组序),其余角色 null 不占号。返回数组与 refs
 * 同序同长;@ 浮层 token 与绑定行构建(element.buildElementLine)都从这里
 * 取号,编号口径只有一份实现。 */
export function refIndexNumbers(
  refs: readonly (ComposerRef | VideoComposerRef)[],
  mode: 'image' | 'video',
): (number | null)[] {
  let n = 0
  return refs.map((r) => {
    if (mode === 'video' && (r as VideoComposerRef).role !== 'reference_image') {
      return null
    }
    n += 1
    return n
  })
}

/** 图片模式:参考条全部条目按序编号。names(可选)与 refs 同序,是编辑器
 * 经 element.resolveElementNames 解析好的显示名;缺省时行为与旧口径逐字节
 * 一致。 */
export function composerRefMentions(
  refs: ComposerRef[],
  names?: readonly (string | null)[],
): MentionCandidate[] {
  const numbers = refIndexNumbers(refs, 'image')
  return refs.map((r, i) =>
    withElementDisplay(
      { token: `@${REF_INDEX_LABEL}${numbers[i]}`, kind: 'image' as const, url: r.url },
      names?.[i],
      r.element,
    ),
  )
}

/** 视频模式:仅「参考图」角色按其在参考图序列中的顺序编号 —— 编号即
 * references 数组下标,与上游契约严格对齐(44 号票「首帧切参考图」场景
 * 下编号不漂移)。names 与 refs 同序(按条目下标对位,不按参考图序)。 */
export function videoRefMentions(
  refs: VideoComposerRef[],
  names?: readonly (string | null)[],
): MentionCandidate[] {
  const numbers = refIndexNumbers(refs, 'video')
  const out: MentionCandidate[] = []
  for (const [i, r] of refs.entries()) {
    if (r.role !== 'reference_image') {
      continue
    }
    out.push(
      withElementDisplay(
        {
          token: `@${REF_INDEX_LABEL}${numbers[i]}`,
          kind: 'image',
          url: r.url,
          roleLabel: VIDEO_ROLE_LABEL.reference_image,
        },
        names?.[i],
        r.element,
      ),
    )
  }
  return out
}

/** Agent 输入框:待发附件按各自插入序编号(图片 @图片N、视频 @视频N);
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
      out.push({ token: `@${REF_INDEX_LABEL}${img}`, kind: 'image', url: m.ref })
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
