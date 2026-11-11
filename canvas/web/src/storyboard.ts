/**
 * 絵コンテ(分镜板)的纯逻辑(53 号票):cut 的数据形状、增删移、运镜
 * 多选切换、时长汇总。组件只做渲染与交互,这里集中可单测的判定 —— 与
 * previz.ts 同族(整图 JSON 随节点 data,自动保存免费获得);54 号票
 * ネーム复用编辑层骨架与画面槽交互,数据结构各自定义、不强行归一。
 * 73 号票:cut→视频/图片两条直通的 prompt 拼装按 Seedance 2.0 官方公式
 * 重排段序(画面风格→全片基调→画面描述→…→画质约束尾段),并新增
 * 全片基调段(brief)与画质约束尾段(QUALITY_TAIL)。
 */
import type { StoryboardCut, StoryboardNodeData } from './graph'
import { prependStyleSegment } from './ipStyle'
import { moveItem, removeAt } from './list'
import { prependPaletteSegment } from './palette'
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

/** cut 出场角色的变体绑定映射(83 号票,角色 id → 变体 id)的纯操作:
 * variantId ≤ 0 = 清除该角色绑定(下拉回「默认变体」档);清到空整键
 * 不落(undefined,缺省回落默认变体,数据不留空壳);换新对象不改入参。
 * 已删变体的悬空键**不在此处清洗** —— 保留才能在编辑层显示回落态,
 * 用户在下拉换档时自然覆写(sheet.resolveCutVariant 负责解析回落)。 */
export function setCutVariantBinding(
  map: Record<number, number> | undefined,
  characterId: number,
  variantId: number,
): Record<number, number> | undefined {
  const next: Record<number, number> = { ...(map ?? {}) }
  if (variantId <= 0) {
    delete next[characterId]
  } else {
    next[characterId] = variantId
  }
  return Object.keys(next).length === 0 ? undefined : next
}

/** 变体绑定簿的孤儿收回(83 号票,目录刷新的悬空收回链):角色已不在
 * 出场清单(character_ids,keptCharacterIds 即收回后的清单)的绑定条目
 * 删掉 —— 角色标注被收回时其绑定随之无主,不把孤儿键留在整图 JSON;
 * 清到空整键不落(undefined)。无孤儿时返回原引用(编辑层不产生无谓
 * 落盘)。注意与「变体已删但角色仍在场」不同:那种绑定键**保留**,编辑
 * 展示回落态(用户换档自然覆写),不在此清洗。 */
export function pruneCutVariantOrphans(
  map: Record<number, number> | undefined,
  keptCharacterIds: number[],
): Record<number, number> | undefined {
  if (!map) {
    return undefined
  }
  const kept = new Set(keptCharacterIds)
  const next: Record<number, number> = {}
  for (const [k, v] of Object.entries(map)) {
    if (kept.has(Number(k))) {
      next[Number(k)] = v
    }
  }
  const count = Object.keys(next).length
  return count === Object.keys(map).length ? map : count === 0 ? undefined : next
}

// ---- 直通预填拼装(67 号票;73 号票按 Seedance 2.0 官方公式重排) ----

/** 全片基调段标签(73 号票定案 B):半角冒号,拼在 brief trim 后文本前,
 * brief 多行原样。基调段拼在画面风格段之后、画面描述之前。 */
const BRIEF_SEGMENT_LABEL = '全片基调:'

/** 画质约束尾段(73 号票定案 D.4):单行恒拼在提示词最尾(硬拼无开关);
 * 内容逐字钉死 —— 半角逗号/分号,末句为无字幕/无 Logo/无水印约束
 * (官方约束词;要不要背景音乐交给 lines 的音乐`()`段,尾段不写)。 */
export const QUALITY_TAIL =
  '高清,细节丰富,电影质感,色彩自然;保持无字幕,不要生成Logo和水印'

/** 直通拼装的 section 标记清单(84 号票 B2 防叠显式化,评审修复轮):
 * palette.prependPaletteSegment/stripPaletteSegment 据此识别「配色段之后
 * 的 section 行」—— palette 不反向 import 本文件(会循环依赖),段边界
 * 由拼装面声明、清单与上面各段标记同源维护,漂移由 storyboard.test 的
 * 段序钉子与 palette.test 的边界用例两侧对拍。 */
const SECTION_MARKS = [
  BRIEF_SEGMENT_LABEL,
  '画面描述:',
  '景别:',
  '运镜:',
  '台词/音效:',
  QUALITY_TAIL,
]

/** cut → 视频提示词拼装(67 号票 cut→视频直通;73 号票段序按官方公式
 * 重排):画面风格段 → 全片基调段 → 画面描述 → 景别 → 运镜 → 台词/音效 →
 * 画质约束尾段。基调段(brief)与尾段作为 sections 成员参与序位 —— 基调
 * 最前、尾段最后,最后照旧经 ipStyle.prependStyleSegment 把风格段顶到
 * 最前(风格段不在 sections 里,「带 IP 风格」开关语义不变)。
 *
 * - brief trim 非空恒拼,**不受 composer「带 IP 风格」开关管**(73 号票
 *   定案 B.4:开关只管 ips.style 段,brief 是 cut 板数据本身)。
 * - 尾段恒拼(硬拼无开关):因此全空 cut + 无 brief + 无风格的输出不再
 *   是空串而是尾段一行(旧「全空返回空串」行为按 73 号票定案 D.1/D.4
 *   调整);有风格则风格段照旧在最前。
 * - 画面描述取 note(53 号票备注字段;66 号票 save_storyboard 产物的
 *   note 即画面内容描述,信息密度最高,排基调段之后的首位;台词/音效段
 *   标签、运镜顿号连接、cut.seconds 不进 prompt 照旧(秒数参照由导演写
 *   进 note,73 号票定案 D.7)。
 * - 84 号票 B3:角色配色段(paletteLine,palette.buildPaletteLine 产物)
 *   非空拼在 sections 之前(段位 = 首位)—— 风格段最后由
 *   prependStyleSegment 顶到最前,最终序 = 画面风格 → 角色配色 → 全片
 *   基调 → …;发送链的绑定行(「出场元素:」)经 insertElementLine 插
 *   风格段之后、配色段之前,即票面「配色段插在绑定行之后」。有画面路径
 *   不传(71 号票互斥不注入)。拼装经 palette.prependPaletteSegment
 *   (84-B2 防叠显式化):拼前剥掉 body 里已存在的段,「prompt 已含同段
 *   不叠段」是拼装函数自身的性质而非调用方纪律(与 ipStyle 防叠的同构
 *   关系与服务端对照见 palette.stripPaletteSegment 注释)。
 * 纯函数便于单测;参考图注入不进提示词照旧(70 号票,配色段是 84 另加)。 */
export function buildCutVideoPrompt(cut: StoryboardCut, brief = '', style = '', paletteLine = ''): string {
  const sections: string[] = []
  const briefText = brief.trim()
  if (briefText !== '') {
    sections.push(`${BRIEF_SEGMENT_LABEL}${briefText}`)
  }
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
  sections.push(QUALITY_TAIL)
  return prependStyleSegment(prependPaletteSegment(sections.join('\n'), paletteLine, SECTION_MARKS), style)
}

/** cut → 图片提示词拼装(分镜画面直通生成;73 号票定案 D.5 生图直通
 * 同吃):画面风格段 → 角色配色段(84 号票 B2,非空才拼)→ 全片基调段
 * → 画面描述 → 景别 → 画质约束尾段 —— 分镜画面是视频首帧种子,同需基调
 * 一致与画面干净,两直通口径一致;不拼运镜/台词照旧(那是视频语汇)。
 * 基调段/尾段/风格段/配色段的拼装口径与 buildCutVideoPrompt 逐条一致
 * (含尾段恒拼、brief 不受开关管、配色段经 prependPaletteSegment 防叠
 * 拼装)。纯函数便于单测。 */
export function buildCutFramePrompt(cut: StoryboardCut, brief = '', style = '', paletteLine = ''): string {
  const sections: string[] = []
  const briefText = brief.trim()
  if (briefText !== '') {
    sections.push(`${BRIEF_SEGMENT_LABEL}${briefText}`)
  }
  const note = (cut.note ?? '').trim()
  if (note !== '') {
    sections.push(`画面描述:${note}`)
  }
  if (cut.shot) {
    sections.push(`景别:${cut.shot}`)
  }
  sections.push(QUALITY_TAIL)
  return prependStyleSegment(prependPaletteSegment(sections.join('\n'), paletteLine, SECTION_MARKS), style)
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
