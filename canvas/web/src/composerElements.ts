/**
 * 元素参考的装配层纯函数(71 号票 Wave-2):把 element.ts 的判定接进编辑器
 * 与生成对话框时的状态折算、提示判定与文案。element.ts 只认「一条参考条
 * 列表」,而编辑器的生效参考条是双源计算值 —— 选中节点产物 chip(未拆下
 * 时)+ 上传草稿(uploadedRefs / uploadedVideoRefs,经 append 系守卫收编)。
 * 合并必须对生效条做(上限与去重要算上产物 chip),结果再落回草稿;这里
 * 集中这道折算与对话框的几条判定,组件只做状态接线和展示(坑 4 / 坑 12)。
 */
import {
  isReferenceRole,
  isRelayableRef,
  MAX_COMPOSER_REFS,
  VIDEO_REFS_TOTAL_CAP,
  type ComposerRef,
  type VideoComposerRef,
} from './composer'
import {
  buildElementLine,
  elementEntityKey,
  insertElementLine,
  type ElementPickAction,
  type ElementPickResult,
} from './element'
import { styledSendPrompt, styleSegment } from './ipStyle'
import { fillPreset, presetSlotsFromElements, type GameplayPreset } from './presets'
import type { MentionCandidate } from './refmention'

/** 把对「生效参考条」算出的元素合并结果落回上传草稿。选中产物 chip 是
 * 计算值(从不带 element,合并不会命中它),只参与守卫,不写进草稿;命中
 * 条目按实体键 + 地址在草稿里定位 —— 草稿里可能有被产物 chip 去重挡住的
 * 隐藏条目,按「下标 - 1」偏移折算会换错条。rejected 原样返回草稿引用。 */
export function applyElementPickToDraft<T extends ComposerRef | VideoComposerRef>(
  draft: T[],
  visible: readonly T[],
  result: ElementPickResult<T>,
): T[] {
  if (result.action === 'rejected' || result.index === undefined) {
    return draft
  }
  if (result.action === 'added') {
    return [...draft, result.refs[result.index]]
  }
  const hit = visible[result.index]
  const el = hit?.element
  if (!el) {
    return draft
  }
  const key = elementEntityKey(el)
  const at = draft.findIndex(
    (r) => r.url === hit.url && r.element !== undefined && elementEntityKey(r.element) === key,
  )
  if (at < 0) {
    return draft
  }
  if (result.action === 'removed') {
    return draft.filter((_, i) => i !== at)
  }
  return draft.map((r, i) => (i === at ? result.refs[result.index!] : r))
}

/** 元素点选被拒收时的原因文案(编辑器经 generateError 横幅报出,与素材库
 * 挑选的满员预检同一出口)。merge 的拒收原因三类:地址不可进网关契约、
 * 同一张图已在条上(无名图或别的实体占着 —— 一张图一条)、上限。视频模式
 * 元素恒 reference_image(不走 44 号票默认角色),满员文案与编辑器
 * videoRefCapBlocked 的既有字面量一致。 */
export function elementPickRejectNotice(
  mode: 'image' | 'video',
  visible: readonly (ComposerRef | VideoComposerRef)[],
  url: string,
): string {
  if (!isRelayableRef(url)) {
    return '该图地址不能作为参考'
  }
  if (visible.some((r) => r.url === url)) {
    return '该图已在参考条上'
  }
  if (mode === 'image') {
    return `参考图已满员(最多 ${MAX_COMPOSER_REFS} 张)`
  }
  const refs = visible as readonly VideoComposerRef[]
  const totalFull = refs.filter((r) => isReferenceRole(r.role)).length >= VIDEO_REFS_TOTAL_CAP
  return totalFull
    ? '参考条已满员(参考图/参考视频/参考音频合计最多 9 条)'
    : '图片参考已满员(首帧/尾帧各 1,参考图最多 9)'
}

/** 元素点选的轻提示(71 号票 B3):只给「同实体替换」出 toast,名字用
 * 实时解析的显示名(含「·变体名」);added 由 chip 出现即反馈、removed 沿用
 * 既有取消无提示,一律空串。名字解析不出(目录竞态)用泛称兜底。 */
export function elementPickNotice(action: ElementPickAction, name: string | null): string {
  if (action !== 'replaced') {
    return ''
  }
  return name ? `已替换「${name}」的参考图` : '已替换该元素的参考图'
}

/** 发送拼装(71 号票 B5,图片/视频两路 onComposerSend 共用):先按「带 IP
 * 风格」开关拼风格段(69 号票 styledSendPrompt),再把绑定行插在风格段那一
 * 行之后(无风格段则置最前,已含同一行不叠加 —— element.insertElementLine)。
 * names 与 refs 同序(编辑器经 resolveElementNames 实时解析);无元素时与
 * styledSendPrompt 逐字节一致。发出的全文随任务行与结果节点 prompt 落库,
 * 可审计。多行风格兜底:IP 风格在编辑框里可以换行(styleSegment 只 trim 两端),
 * 而 insertElementLine 以首个换行认风格段行尾,会把绑定行插进风格文本中间 ——
 * 风格段本身多行且全稿以「整段风格 + 换行」开头时,这里按整段长度切开再插。 */
export function composeSendPrompt(
  prompt: string,
  withStyle: boolean,
  style: string | null | undefined,
  refs: (ComposerRef | VideoComposerRef)[],
  names: readonly (string | null)[],
  mode: 'image' | 'video',
): string {
  const styled = styledSendPrompt(prompt, withStyle, style)
  const line = buildElementLine(refs, names, mode)
  const segment = styleSegment(style)
  if (line !== '' && segment.includes('\n') && styled.startsWith(`${segment}\n`) && !styled.includes(line)) {
    return `${segment}\n${line}${styled.slice(segment.length)}`
  }
  return insertElementLine(styled, line)
}

/** 首帧/尾帧与参考互斥提示的显隐(71 号票 A3):方舟 Seedance 官方「图生
 * 视频-首帧、首尾帧、全模态参考为 3 种互斥场景,不可混用」,MiniMax H3 原生
 * 与百炼同口径 —— 参考条上首帧或尾帧与任一 reference_* 同时在场即出提示
 * 行。只提示不拦发送(上游裁决原则不变)。 */
export function hasFrameReferenceConflict(refs: readonly VideoComposerRef[]): boolean {
  const hasFrame = refs.some((r) => r.role === 'first_frame' || r.role === 'last_frame')
  return hasFrame && refs.some((r) => isReferenceRole(r.role))
}

/** @ 浮层选中后插进提示词的文本(71 号票 B6):有名元素插纯名字 —— 不带
 * @、不随参考序移位,名字与图的对应由发送时的绑定行承担;无名图(含实体
 * 已删退化的元素 chip)维持 59 号票的 @图N。 */
export function mentionInsertText(c: MentionCandidate): string {
  return c.elementName ?? c.token
}

/** 「玩法」下拉选中后写进提示词的文本(71 号票 C3):按参考条元素 chip 序
 * 填名(第 1/2 个角色、第 1 个场景;names 为编辑器实时解析的显示名),必需
 * 槽缺失留可见占位、可选场景从句缺场景整段省略(presets.fillPreset)。 */
export function presetPromptFor(
  preset: GameplayPreset,
  refs: readonly (ComposerRef | VideoComposerRef)[],
  names: readonly (string | null)[],
): string {
  return fillPreset(preset.template, presetSlotsFromElements(refs, names))
}

/** 写入前是否要确认替换(71 号票 C4):提示词空(含全空白)直接写入;与将写
 * 入的模板逐字相同也不必确认(无损);其余非空 = 会覆盖用户手写,需确认。 */
export function presetNeedsConfirm(current: string, next: string): boolean {
  return current.trim() !== '' && current !== next
}
