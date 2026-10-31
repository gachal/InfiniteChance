/**
 * 絵コンテ(分镜板)的纯逻辑(53 号票):cut 的数据形状、增删移、运镜
 * 多选切换、时长汇总。组件只做渲染与交互,这里集中可单测的判定 —— 与
 * previz.ts 同族(整图 JSON 随节点 data,自动保存免费获得);54 号票
 * ネーム复用编辑层骨架与画面槽交互,数据结构各自定义、不强行归一。
 */
import type { StoryboardCut, StoryboardNodeData } from './graph'
import { moveItem, removeAt } from './list'
import { assetContentURL } from './thumbnail'

/** 运镜白名单(53 号票):前端持有,后端存串不校验(51 号 label 同哲学);
 * 将来管理端可配显式另票。初始集:推/拉/摇/移/跟/固定/手持/变焦。 */
export const CAMERA_MOVE_OPTIONS = ['推', '拉', '摇', '移', '跟', '固定', '手持', '变焦'] as const

export type CameraMove = (typeof CAMERA_MOVE_OPTIONS)[number]

/** 新 cut 的空形状:画面空(asset_id = 0)、时长未填、台词空、无运镜、
 * 无备注。 */
export function emptyStoryboardCut(): StoryboardCut {
  return { asset_id: 0, lines: '', camera_moves: [] }
}

/** 新分镜板的初始 data:板名空(展示层回落「分镜」)+ 1 个空 cut
 * (添加即可编辑,预演台「添加即可见」先例)。 */
export function defaultStoryboard(): StoryboardNodeData {
  return { title: '', cuts: [emptyStoryboardCut()] }
}

/** 追加一个空 cut:返回新数组,不改入参(数组换新纯函数纪律)。 */
export function appendCut(cuts: StoryboardCut[]): StoryboardCut[] {
  return [...cuts, emptyStoryboardCut()]
}

/** 按下标删除 cut:**无「最后不可删」守卫** —— 板是画布工作件而非库
 * 实体,cut 可删到空(与角色变体的守卫相反,53 号票定案);机制在
 * list.ts removeAt(与ネーム页格/关卡节拍共享)。 */
export function removeCut(cuts: StoryboardCut[], index: number): StoryboardCut[] {
  return removeAt(cuts, index)
}

/** 上移/下移一个 cut(delta = -1 | 1);越界或空表原样返回(编辑层排序
 * 用按钮不做拖拽,40 号票手势冲突教训;序号显示时生成 1..N,删插自动
 * 重排,不持久);机制在 list.ts moveItem(同上共享)。 */
export function moveCut(cuts: StoryboardCut[], index: number, delta: -1 | 1): StoryboardCut[] {
  return moveItem(cuts, index, delta)
}

/** 运镜 chip 多选切换:选中 → 移除,未选 → 追加;换新对象不改入参。 */
export function toggleCameraMove(cut: StoryboardCut, move: string): StoryboardCut {
  const selected = cut.camera_moves.includes(move)
  return {
    ...cut,
    camera_moves: selected
      ? cut.camera_moves.filter((m) => m !== move)
      : [...cut.camera_moves, move],
  }
}

/** 景别白名单(67 号票):前端持有,后端存串不校验(运镜同哲学);
 * 单选 —— 一个镜头一个景别,与运镜多选区分(票面定案 2)。 */
export const SHOT_SIZE_OPTIONS = ['远', '全', '中', '近', '特写'] as const

export type ShotSize = (typeof SHOT_SIZE_OPTIONS)[number]

/** 景别 chip 单选切换(67 号票):点已选 = 清除(可回到未定,键不落),
 * 点别档 = 替换;换新对象不改入参(与 toggleCameraMove 同款纯函数纪律)。 */
export function setShotSize(cut: StoryboardCut, shot: string): StoryboardCut {
  if (cut.shot === shot) {
    const next = { ...cut }
    delete next.shot
    return next
  }
  return { ...cut, shot }
}

/** cut → 视频提示词拼装(67 号票 cut→视频直通):画面描述/景别/运镜/
 * 台词音效按序拼成带标签的小节,空段落跳过,全空返回空串(对话框提示词
 * 留白,占位文案引导)。画面描述取 note(53 号票备注字段;66 号票
 * save_storyboard 产物的 note 即画面内容描述,工具产物与人工 cut 同规);
 * 它是对画面内容的自然语言描述、信息密度最高,排最前。纯函数便于单测;
 * 出场角色不进提示词(「出场角色→生成注入」二期,67 号票 Out of Scope)。 */
export function buildCutVideoPrompt(cut: StoryboardCut): string {
  const sections: string[] = []
  const note = (cut.note ?? '').trim()
  if (note !== '') {
    sections.push(`画面描述:${note}`)
  }
  if (cut.shot) {
    sections.push(`景别:${cut.shot}`)
  }
  if (cut.camera_moves.length > 0) {
    sections.push(`运镜:${cut.camera_moves.join('、')}`)
  }
  const lines = cut.lines.trim()
  if (lines !== '') {
    sections.push(`台词/音效:${lines}`)
  }
  return sections.join('\n')
}

/** cut → 图片提示词拼装(分镜画面直通生成):画面描述排最前、景别作构图
 * 指引随后;空段落跳过,全空返回空串(对话框提示词留白,占位文案引导)。
 * 不拼运镜/台词——那是视频语汇,图片画面只吃内容与构图;出场角色不进
 * 提示词(「出场角色→生成注入」二期)。纯函数便于单测。 */
export function buildCutFramePrompt(cut: StoryboardCut): string {
  const sections: string[] = []
  const note = (cut.note ?? '').trim()
  if (note !== '') {
    sections.push(`画面描述:${note}`)
  }
  if (cut.shot) {
    sections.push(`景别:${cut.shot}`)
  }
  return sections.join('\n')
}

/** 单个 cut 时长的夹紧上限(秒):一个分镜 cut 超过一小时没有意义。
 * 导出供 agentTools.ts 的草案定形复用(66 号票 save_storyboard 同口径)。 */
export const SECONDS_MAX = 3600

/** 时长输入的失焦夹紧:空串/非数字 → 未填(undefined,汇总时跳过);
 * 负数夹到 0、超上限夹到 3600、小数取整。 */
export function clampSecondsInput(raw: string): number | undefined {
  const trimmed = raw.trim()
  if (trimmed === '') {
    return undefined
  }
  const value = Number(trimmed)
  if (!Number.isFinite(value)) {
    return undefined
  }
  return Math.min(Math.max(Math.round(value), 0), SECONDS_MAX)
}

/** 板内时长合计:只求非空 cut 的和(时长可空是「还没定」,不是 0)。 */
export function totalSeconds(cuts: StoryboardCut[]): number {
  return cuts.reduce((sum, c) => sum + (c.seconds ?? 0), 0)
}

/** 合计的展示格式 M:SS(秒两位补零,分钟不补):65s → 1:05。 */
export function formatDuration(seconds: number): string {
  const m = Math.floor(seconds / 60)
  const s = seconds % 60
  return `${m}:${String(s).padStart(2, '0')}`
}

/** cut 画面的内容寻址地址(展示层按素材 id 拼装,服务端解出真实地址;
 * 素材行被删时 404,展示面落「素材不可用」占位);构造统一走
 * thumbnail.ts assetContentURL(与角色设定图同一体,评审合并)。 */
export function frameContentURL(assetId: number): string {
  return assetContentURL(assetId)
}
