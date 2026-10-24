/**
 * 关卡卡的纯逻辑(55 号票):初始 data、节拍的增删移、画面槽的 9 上限
 * 守卫。组件只做渲染与交互,这里集中可单测的判定 —— 与 storyboard.ts /
 * name.ts 同族(整图 JSON 随节点 data,自动保存免费获得);关卡卡是文档
 * 型工件,卡内直接编辑不开编辑层(53 号票定案),纯函数直接服务卡面。
 */
import type { LevelNodeData } from './graph'
import { moveItem, removeAt } from './list'

/** 新关卡的初始 data:名称「新关卡」(添加即命名,票面缺省)+ 空节拍 +
 * 空画面槽;goal/detail 可空不落键(ネーム页标题「不落键」同纪律)。 */
export function defaultLevel(): LevelNodeData {
  return { name: '新关卡', beats: [], asset_ids: [] }
}

/** 追加一个空节拍(拍文本后填):返回新数组,不改入参(数组换新纯函数
 * 纪律)。 */
export function appendBeat(beats: string[]): string[] {
  return [...beats, '']
}

/** 按下标删除节拍:无「最后不可删」守卫 —— 关卡是画布工作件而非库实体,
 * 节拍可删到空(与絵コンテ cut / ネーム页同 53/54 号票定案);机制在
 * list.ts removeAt(三域共享)。 */
export function removeBeat(beats: string[], index: number): string[] {
  return removeAt(beats, index)
}

/** 上移/下移一拍(delta = -1 | 1);越界或空表原样返回(卡内编辑排序用
 * 按钮不做拖拽,40 号票手势冲突教训;序号显示时生成 1..N,删插自动重排,
 * 不持久);机制在 list.ts moveItem(同上共享)。 */
export function moveBeat(beats: string[], index: number, delta: -1 | 1): string[] {
  return moveItem(beats, index, delta)
}

/** 画面槽上限(55 号票):素材引用列表最多 9 条,与参考图口径一致
 * (composer.ts MAX_COMPOSER_REFS 同值;概念图/地图草图是参考而非生成
 * 载荷,取同预算纯为口径统一)。 */
export const MAX_LEVEL_ASSETS = 9

/** 画面槽还能不能收一条(上传与素材挑选共用的前置预检):满员时入口
 * 禁用、两路拒绝都以此为准,不传字节不留孤儿素材行。 */
export function canAddLevelAsset(assetIds: number[]): boolean {
  return assetIds.length < MAX_LEVEL_ASSETS
}

/** 画面槽点选切换:已在槽中 → 摘下;不在且未满 → 追加;满员整条拒收
 * (返回原数组,appendComposerRef 同款纪律,调用方按引用不等识别拒收
 * 并提示)。纯函数,调用方拿返回值替换状态。 */
export function toggleLevelAsset(assetIds: number[], assetId: number): number[] {
  if (assetIds.includes(assetId)) {
    return assetIds.filter((id) => id !== assetId)
  }
  if (!canAddLevelAsset(assetIds)) {
    return assetIds
  }
  return [...assetIds, assetId]
}
