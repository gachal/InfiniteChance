/**
 * 画布右键上下文菜单的零依赖条目定义(76 号票落、77 号票二级化):菜单
 * = 两级结构 —— 一级是分区入口(ContextMenuSection 的 label,纯文字),
 * hover 展开二级飞出条目(items),内容即数据 —— 条目集、文案、命令、
 * 配色 tone、悬停提示全部住在这里,渲染组件(CanvasContextMenu.vue)只
 * import 常量与类型、不含任何条目字面量(循 canvasHelp.ts 的「内容模块」
 * 范式,改条目只动本文件)。canvasContextMenu.test.ts 按票钉住两层结构
 * 与条目集。
 *
 * 口径(76·定案 3 + 77·定案 1/2):条目纯文字沿用 NODE_TYPE_LABEL(唯一
 * 例外 audio 的菜单语义是「配乐」,音效是 audio 节点带 source:'sfx' 的
 * 第二入口、翻唱是带 source:'cover' 的第三入口,78 号票),不带「+」
 * 前缀、不引入图标库;彩色 tone 语言完整保留在飞出内条目上,一级分区
 * 入口中性灰。面板组保持既有 toggle/打开语义,不做开启态标记 —— 组件
 * 也不感知面板开关状态。
 */

import { NODE_TYPE_LABEL, type AudioNodeData, type CanvasNodeType } from './graph'

/** 菜单命令:视图按 kind 分发(落节点 / 进画框 / 开面板 / 上传 / 帮助),
 * 本模块只描述意图不实现行为。 */
export type ContextMenuCommand =
  | { kind: 'add-node'; nodeType: CanvasNodeType; dataPatch?: Partial<AudioNodeData> }
  | { kind: 'group-draw' }
  | { kind: 'toggle-asset' }
  | { kind: 'toggle-records' }
  | { kind: 'upload' }
  | { kind: 'help' }

/** 条目配色 tone 的合法值(76 号票评审收字面量联合:拼错编译期报错,
 * 不再是裸 string)。组件按 tone 拼成 `tone-${tone}` 类名取 CSS 色对,
 * 值集与 CanvasContextMenu.vue 的 .tone-* 样式类一一对应。 */
export type ContextMenuItemTone =
  | 'accent'
  | 'success'
  | 'amber'
  | 'pink'
  | 'teal'
  | 'teal-light'
  | 'violet'
  | 'violet-strong'
  | 'orange'
  | 'sky'
  | 'red'
  | 'gold'
  | 'neutral'

export interface ContextMenuItem {
  /** 全局唯一 id(组件 :key 与可读性)。 */
  id: string
  /** 纯文字文案,不带「+」前缀。 */
  label: string
  /** 悬停提示:仅音效一条随迁原工具栏,其余按「落一个<菜单文案>节点」
   * 生成(76 号票新造,非历史随迁)。 */
  title?: string
  /** 配色 tone:沿用原工具栏按钮的彩色语言,组件按 tone 取 CSS 色对。 */
  tone: ContextMenuItemTone
  command: ContextMenuCommand
}

/** 分区 id 字面量联合(组件 hover 态按它记录当前展开的分区)。 */
export type ContextMenuSectionId = 'create' | 'plan' | 'organize' | 'panels'

/** 一级分区入口(77 号票二级化):label 即一级菜单行文案,hover 展开
 * items 飞出;顺序即一级渲染序,测试按本常量钉住两层结构。 */
export interface ContextMenuSection {
  id: ContextMenuSectionId
  label: string
  /** 二级飞出条目(76 号票条目集原样随迁,只是从平铺改为飞出展示)。 */
  items: ContextMenuItem[]
}

/** audio 的菜单语义是「配乐」(NODE_TYPE_LABEL 的「音频」是节点卡头的
 * 中性称谓,58 号票起的 ADD_LABELS 例外原样随迁)。 */
const AUDIO_LABEL = '配乐'

/** 条目 title 覆盖表:仅音效一条随迁自原工具栏按钮(58 号票积累的语义
 * 说明,原样保留);键是条目 id(音效/翻唱不是节点类型,是 audio 节点的
 * 第二、第三入口)。其余条目的 title 由 nodeItem 按菜单文案生成,不走
 * 这张表。 */
const ADD_TITLES: Record<string, string> = {
  sfx: '落一个音效创作节点(≤30 秒短音频,按秒计费)',
  cover: '落一个翻唱创作节点(以参考原曲为基底,换语言重新演唱)',
}

/** 落节点条目工厂入参(76 号票评审收对象参数:调用点按名传参,相邻两
 * 个 boolean/同型位置参数不再有传错位的可能)。 */
interface NodeItemInput {
  /** 落哪种节点;音效第二入口同落 audio,靠 data.source 区分意图。 */
  type: CanvasNodeType
  tone: ContextMenuItemTone
  /** 初始创作意图(61 号票:音效入口带 source:'sfx')。 */
  data?: Partial<AudioNodeData>
  /** 菜单文案覆盖;缺省按类型取 NODE_TYPE_LABEL(audio 例外「配乐」)。 */
  label?: string
}

/** 落节点条目工厂:文案 = NODE_TYPE_LABEL(audio 例外「配乐」,音效第二
 * 入口显式传「音效」);title 与文案同源生成 —— audio 条目实际落的也是
 * audio 节点,但菜单语义是「配乐」,title 从文案不从类型(名实一致,
 * 不写「落一个音频节点」);带 data 的入口 id 取意图标记本身。 */
function nodeItem({ type, tone, data, label }: NodeItemInput): ContextMenuItem {
  const id = data?.source ? String(data.source) : type
  const text = label ?? (type === 'audio' ? AUDIO_LABEL : NODE_TYPE_LABEL[type])
  return {
    id,
    label: text,
    title: ADD_TITLES[type] ?? `落一个${text}节点`,
    tone,
    command: data ? { kind: 'add-node', nodeType: type, dataPatch: data } : { kind: 'add-node', nodeType: type },
  }
}

/**
 * 菜单四段(票·定案 3):创作 9 钮 / 规划 4 钮 / 组织分组 / 面板 4 入口
 * (78 号票起创作段 8 → 9:翻唱是与配乐/音效并列的第三音频入口)。
 * 顺序即渲染序;测试按本常量钉住条目集。
 */
export const CONTEXT_MENU_SECTIONS: ContextMenuSection[] = [
  {
    id: 'create',
    label: '创作',
    items: [
      // 53 号票序:agent/image/video/previz + 52/57/60/61 号票四音频与
      // 角色入口(配乐在音色后、音效压轴,原工具栏顺序原样随迁);
      // 78 号票的翻唱是第三音频入口,随音效之后。
      nodeItem({ type: 'agent', tone: 'accent' }),
      nodeItem({ type: 'image', tone: 'success' }),
      nodeItem({ type: 'video', tone: 'amber' }),
      nodeItem({ type: 'previz', tone: 'pink' }),
      nodeItem({ type: 'character', tone: 'teal' }),
      nodeItem({ type: 'voice', tone: 'teal-light' }),
      nodeItem({ type: 'audio', tone: 'violet' }),
      nodeItem({ type: 'audio', tone: 'violet-strong', data: { source: 'sfx' }, label: '音效' }),
      nodeItem({ type: 'audio', tone: 'gold', data: { source: 'cover' }, label: '翻唱' }),
    ],
  },
  {
    id: 'plan',
    label: '规划',
    items: [
      nodeItem({ type: 'storyboard', tone: 'orange' }),
      nodeItem({ type: 'name', tone: 'sky' }),
      nodeItem({ type: 'level', tone: 'red' }),
      nodeItem({ type: 'script', tone: 'gold' }),
    ],
  },
  {
    id: 'organize',
    label: '组织',
    items: [
      {
        // 72 号票:画布分组是组织容器不是内容节点,不走节点添加,
        // 单独命令进画框模式(劫持拖画矩形、单次生效)。
        id: 'group',
        label: '分组',
        title: '画一个分组框:点击后在画布拖出矩形,松手落空分组;框选节点按 Ctrl/Cmd+G 直接成组',
        tone: 'neutral',
        command: { kind: 'group-draw' },
      },
    ],
  },
  {
    id: 'panels',
    label: '面板',
    items: [
      {
        id: 'asset',
        label: '素材库',
        title: '打开素材库:浏览历史素材并插入画布',
        tone: 'neutral',
        command: { kind: 'toggle-asset' },
      },
      {
        id: 'records',
        label: '生成记录',
        title: '打开生成记录:本画布的任务流水(最近 200 条)',
        tone: 'orange',
        command: { kind: 'toggle-records' },
      },
      {
        id: 'upload',
        label: '上传',
        title: '上传本机图片/视频进素材库,并作为节点插入画布',
        tone: 'teal-light',
        command: { kind: 'upload' },
      },
      {
        id: 'help',
        label: '帮助',
        title: '画布帮助:节点与运行总览、剧本流水线全链(?)',
        tone: 'neutral',
        command: { kind: 'help' },
      },
    ],
  },
]

/** 菜单外缘与视口的安全边距(px)。 */
const VIEWPORT_MARGIN = 8

/** 单轴钳制(私有):值钳进 [margin, 视口该侧边距内上界],比视口还大
 * 时钳到 margin(不溢出不出负)。clampMenuPosition 两轴与
 * clampFlyoutPosition 纵向共用。 */
function clampAxis(value: number, size: number, viewportSize: number, margin: number): number {
  return Math.max(margin, Math.min(value, viewportSize - margin - size))
}

/**
 * 贴边翻转钳制(票·定案 5):右键点默认即菜单左上角;贴底向上弹、贴右
 * 向左收(min/max 钳制,两轴独立),贴左上钳到安全边距;菜单比视口还
 * 大时钳到边距(不溢出不出负)。纯函数,组件挂载后量实际尺寸再调它。
 */
export function clampMenuPosition(
  point: { x: number; y: number },
  size: { width: number; height: number },
  viewport: { width: number; height: number },
  margin = VIEWPORT_MARGIN,
): { x: number; y: number } {
  return {
    x: clampAxis(point.x, size.width, viewport.width, margin),
    y: clampAxis(point.y, size.height, viewport.height, margin),
  }
}

/** 飞出定位锚(77 号票定案 4):x/y = 飞出左上角的期望落点(一级菜单
 * 右缘、顶部与分区入口对齐);flipX = 贴视口右缘翻左时飞出右缘贴靠的
 * x(一级菜单左缘)。 */
export interface FlyoutAnchor {
  x: number
  y: number
  flipX: number
}

/**
 * 飞出贴边钳制(77 号票定案 4):飞出默认贴一级菜单右缘(x/y 即期望
 * 落点,顶部与分区入口对齐);期望落点会越过视口右缘时整体翻到菜单
 * 左侧(右缘贴 flipX),翻左仍放不下钳到 margin(超窄视口退化,不出
 * 负);纵向不管在哪侧都钳进视口(顶部对齐入口后按飞出自身高度钳底,
 * 贴顶钳到 margin,比视口还高钳到 margin)。纯函数,组件在飞出挂载/
 * 分区切换后量实际尺寸再调它 —— 思路与 clampMenuPosition 一致。
 */
export function clampFlyoutPosition(
  anchor: FlyoutAnchor,
  size: { width: number; height: number },
  viewport: { width: number; height: number },
  margin = VIEWPORT_MARGIN,
): { x: number; y: number } {
  const overflowsRight = anchor.x + size.width > viewport.width - margin
  return {
    x: overflowsRight ? Math.max(margin, anchor.flipX - size.width) : Math.max(margin, anchor.x),
    y: clampAxis(anchor.y, size.height, viewport.height, margin),
  }
}
