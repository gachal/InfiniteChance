/**
 * ネーム(分镜稿)的纯逻辑(54 号票):页/格的数据形状、增删移、大小格
 * 切换、页格计数汇总。组件只做渲染与交互,这里集中可单测的判定 —— 与
 * storyboard.ts 同族(整图 JSON 随节点 data,自动保存免费获得);页为
 * 一级单位、页内格子垂直流,与絵コンテ的线性 cut 流心智不同,数据结构
 * 各自定义不强行归一(53 号票定案)。
 */
import type { NameNodeData, NamePage, NamePanel } from './graph'
import { moveItem, removeAt } from './list'

/** 新格的空形状:画面空(asset_id = 0)、台词空、大格起步(コマ割り以
 * 大格推进为主,小格是节奏调剂,切档走 togglePanelSize)。 */
export function emptyNamePanel(): NamePanel {
  return { asset_id: 0, lines: '', size: 'large' }
}

/** 新页的空形状:标题不落键(可空,展示层回落「第 N 页」)+ 1 个空格
 * (新页不是白板,进去就有格子可画)。 */
export function emptyNamePage(): NamePage {
  return { panels: [emptyNamePanel()] }
}

/** 新ネーム板的初始 data:板名空(展示层回落「ネーム」)+ 1 页 1 格
 * (添加即可编辑,预演台「添加即可见」先例)。 */
export function defaultName(): NameNodeData {
  return { title: '', pages: [emptyNamePage()] }
}

/** 追加一个空页:返回新数组,不改入参(数组换新纯函数纪律)。 */
export function appendPage(pages: NamePage[]): NamePage[] {
  return [...pages, emptyNamePage()]
}

/** 按下标删除页:**无「最后不可删」守卫** —— 板是画布工作件而非库
 * 实体,页可删到空(板可空页,与絵コンテ cut 同 53 号票定案);机制在
 * list.ts removeAt(与絵コンテ cut/关卡节拍共享)。 */
export function removePage(pages: NamePage[], index: number): NamePage[] {
  return removeAt(pages, index)
}

/** 上移/下移一页(delta = -1 | 1);越界或空表原样返回(编辑层排序用
 * 按钮不做拖拽,40 号票手势冲突教训;页序号显示时生成 1..N,删插自动
 * 重排,不持久);机制在 list.ts moveItem(同上共享)。 */
export function movePage(pages: NamePage[], index: number, delta: -1 | 1): NamePage[] {
  return moveItem(pages, index, delta)
}

/** 追加一个空格(追加到页尾):返回新数组,不改入参。 */
export function appendPanel(panels: NamePanel[]): NamePanel[] {
  return [...panels, emptyNamePanel()]
}

/** 按下标删除格:无「最后不可删」守卫 —— 格可删到空,空页保留在板上
 * (删页走页级删除,两级语义分开);机制在 list.ts removeAt。 */
export function removePanel(panels: NamePanel[], index: number): NamePanel[] {
  return removeAt(panels, index)
}

/** 上移/下移一格(delta = -1 | 1);越界或空表原样返回;机制在
 * list.ts moveItem。 */
export function movePanel(panels: NamePanel[], index: number, delta: -1 | 1): NamePanel[] {
  return moveItem(panels, index, delta)
}

/** 大小格切换:large ↔ small 两档换新对象不改入参(コマ割り节奏的
 * 轻量表达,不做真二维排版 —— 拖拽定格子框另票)。 */
export function togglePanelSize(panel: NamePanel): NamePanel {
  return { ...panel, size: panel.size === 'small' ? 'large' : 'small' }
}

/** 板内格子总数(板卡汇总「N 页 · M 格」的 M):各页格子数求和,空页
 * 计 0 不计负。 */
export function countPanels(pages: NamePage[]): number {
  return pages.reduce((sum, p) => sum + p.panels.length, 0)
}
