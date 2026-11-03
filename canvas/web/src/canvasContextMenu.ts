/**
 * 画布右键上下文菜单(76 号票)的零依赖条目定义:菜单 = 四段分组 →
 * 条目数组,内容即数据 —— 条目集、文案、命令、配色 tone、悬停提示全部
 * 住在这里,渲染组件(CanvasContextMenu.vue)只 import 常量与类型、
 * 不含任何条目字面量(循 canvasHelp.ts 的「内容模块」范式,改条目只动
 * 本文件)。canvasContextMenu.test.ts 按票钉住条目集与分组结构。
 *
 * 口径(票·定案 3):条目纯文字沿用 NODE_TYPE_LABEL(唯一例外 audio 的
 * 菜单语义是「配乐」,音效是 audio 节点带 source:'sfx' 的第二入口),
 * 不带「+」前缀、不引入图标库;配色沿用原工具栏按钮的彩色语言(tone
 * 字段承载,组件按 tone 取 CSS 色对)。面板组保持既有 toggle/打开语义,
 * 不做开启态标记 —— 组件也不感知面板开关状态。
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

export interface ContextMenuItem {
  /** 全局唯一 id(组件 :key 与可读性)。 */
  id: string
  /** 纯文字文案,不带「+」前缀。 */
  label: string
  /** 悬停提示,沿用原工具栏按钮 title 的语义说明。 */
  title?: string
  /** 配色 tone:沿用原工具栏按钮的彩色语言,组件按 tone 取 CSS 色对。 */
  tone: string
  command: ContextMenuCommand
}

export interface ContextMenuSection {
  id: 'create' | 'plan' | 'organize' | 'panels'
  label: string
  items: ContextMenuItem[]
}

/** audio 的菜单语义是「配乐」(NODE_TYPE_LABEL 的「音频」是节点卡头的
 * 中性称谓,58 号票起的 ADD_LABELS 例外原样随迁)。 */
const AUDIO_LABEL = '配乐'

/** 原工具栏按钮的 title 提示(58 号票积累的语义说明原样随迁);键是
 * 条目 id(音效不是节点类型,是 audio 节点的第二入口)。 */
const ADD_TITLES: Record<string, string> = {
  sfx: '落一个音效创作节点(≤30 秒短音频,按秒计费)',
}

/** 落节点条目工厂:文案 = NODE_TYPE_LABEL(audio 例外「配乐」,音效第二
 * 入口显式传「音效」);带 dataPatch 的入口 id 取意图标记本身。 */
function nodeItem(
  nodeType: CanvasNodeType,
  tone: string,
  dataPatch?: Partial<AudioNodeData>,
  label?: string,
): ContextMenuItem {
  const id = dataPatch?.source ? String(dataPatch.source) : nodeType
  return {
    id,
    label: label ?? (nodeType === 'audio' ? AUDIO_LABEL : NODE_TYPE_LABEL[nodeType]),
    title: ADD_TITLES[nodeType] ?? `落一个${NODE_TYPE_LABEL[nodeType]}节点`,
    tone,
    command: dataPatch ? { kind: 'add-node', nodeType, dataPatch } : { kind: 'add-node', nodeType },
  }
}

/**
 * 菜单四段(票·定案 3):创作 8 钮 / 规划 4 钮 / 组织分组 / 面板 4 入口。
 * 顺序即渲染序;测试按本常量钉住条目集。
 */
export const CONTEXT_MENU_SECTIONS: ContextMenuSection[] = [
  {
    id: 'create',
    label: '创作',
    items: [
      // 53 号票序:agent/image/video/previz + 52/57/60/61 号票四音频与
      // 角色入口(配乐在音色后、音效压轴,原工具栏顺序原样随迁)。
      nodeItem('agent', 'accent'),
      nodeItem('image', 'success'),
      nodeItem('video', 'amber'),
      nodeItem('previz', 'pink'),
      nodeItem('character', 'teal'),
      nodeItem('voice', 'teal-light'),
      nodeItem('audio', 'violet'),
      nodeItem('audio', 'violet-strong', { source: 'sfx' }, '音效'),
    ],
  },
  {
    id: 'plan',
    label: '规划',
    items: [
      nodeItem('storyboard', 'orange'),
      nodeItem('name', 'sky'),
      nodeItem('level', 'red'),
      nodeItem('script', 'gold'),
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
    x: Math.max(margin, Math.min(point.x, viewport.width - margin - size.width)),
    y: Math.max(margin, Math.min(point.y, viewport.height - margin - size.height)),
  }
}
