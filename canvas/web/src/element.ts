/**
 * 元素(71 号票)的纯函数层:显示名解析、AssetPicker 带 element 点选的
 * 合并、绑定行构建与发送拼装插入。元素 = 资产库实体(角色/场景/道具)在
 * 一次生成里的出场引用,一元素一图;名称不存快照,一律按 entityId 从 IP
 * 目录实时取(52 号票精神),实体或变体已删的 chip 退化无名参考图 —— 角标
 * 隐藏、绑定行跳过。UI 装配(chip 角标、toast 文案、目录缓存、同图取消的
 * 选中态回退)归编辑器层,这里只出可单测的判定。
 */
import type { CharacterRecord, PropRecord, SceneRecord } from '@infinitechance/api'

import {
  appendComposerRef,
  appendVideoRef,
  isRelayableRef,
  type ComposerElement,
  type ComposerRef,
  type VideoComposerRef,
} from './composer'

/** IP 目录三类的最小视图:直接用 @infinitechance/api 的目录记录(type-only
 * import,测试可自造轻量对象,运行时零依赖)。 */
export interface ElementCatalog {
  characters: CharacterRecord[]
  scenes: SceneRecord[]
  props: PropRecord[]
}

/** 实体键:角色按 entityId + variantId 区分同一角色的不同造型(两个变体
 * 同时在条是两个元素);场景/道具按 entityId。三类 id 空间独立,键带类型
 * 前缀防互撞。 */
export function elementEntityKey(el: ComposerElement): string {
  return `${el.type}:${el.entityId}:${el.variantId ?? ''}`
}

/** 逐条解析元素显示名(与 refs 同序同长):无 element、实体已删、变体已
 * 删 → null(chip 退化无名,不进绑定行);同一角色多个变体同时在条 →
 * 名称带「·变体名」(小蓝·战斗服),只一个变体不带;场景/道具直接 name;
 * 角色缺 variantId(防御路径,正常挑选必带)回落角色名。补名计数只数
 * 「出得了名」的 chip —— 变体已删退化无名的不占位,不逼剩下的那个补后缀。 */
export function resolveElementNames(
  refs: readonly (ComposerRef | VideoComposerRef)[],
  catalog: ElementCatalog,
): (string | null)[] {
  type Resolved = { name: string; characterId?: number; variantName?: string } | null
  const resolved: Resolved[] = refs.map((r): Resolved => {
    const el = r.element
    if (!el) {
      return null
    }
    if (el.type === 'character') {
      const c = catalog.characters.find((x) => x.id === el.entityId)
      if (!c) {
        return null
      }
      if (el.variantId === undefined) {
        return { name: c.name }
      }
      const v = c.variants.find((x) => x.id === el.variantId)
      return v ? { name: c.name, characterId: c.id, variantName: v.name } : null
    }
    if (el.type === 'scene') {
      const s = catalog.scenes.find((x) => x.id === el.entityId)
      return s ? { name: s.name } : null
    }
    const p = catalog.props.find((x) => x.id === el.entityId)
    return p ? { name: p.name } : null
  })
  const namedVariantCounts = new Map<number, number>()
  for (const entry of resolved) {
    if (entry?.characterId !== undefined) {
      namedVariantCounts.set(entry.characterId, (namedVariantCounts.get(entry.characterId) ?? 0) + 1)
    }
  }
  return resolved.map((entry) => {
    if (!entry) {
      return null
    }
    if (entry.characterId !== undefined && entry.variantName !== undefined) {
      return (namedVariantCounts.get(entry.characterId) ?? 0) >= 2
        ? `${entry.name}·${entry.variantName}`
        : entry.name
    }
    return entry.name
  })
}

// ---- 元素 pick 合并(71 号票 B3)----

export type ElementPickAction = 'added' | 'replaced' | 'removed' | 'rejected'

/** 合并结果:refs 是调用方应整体替换上去的新条目数组(与 append 系同款
 * 拒收式 —— rejected 时返回原数组引用);action 给 wiring 层出 toast 用;
 * index = 受影响条目在原列表中的下标(added = 追加落位,replaced/removed =
 * 命中位;rejected 缺省)。 */
export interface ElementPickResult<T> {
  refs: T[]
  action: ElementPickAction
  index?: number
}

/** AssetPicker 带 element 的一次点选载荷:素材行地址 + 实体语义。 */
export interface ElementPick {
  url: string
  asset_id?: number
  element: ComposerElement
}

/** 合并核心(两模式共用):同实体已在条 → 同图取消(removed)/异图原位
 * 替换(replaced,保持一元素一图);新实体 → 复用 append 系既有守卫
 * (地址形状、URL 去重、角色上限、合计上限、图片条上限),超限整条拒收。
 * 同一地址已被其他条目占用(无名图或别的实体)同样拒收 —— 一张图一条。 */
function mergeElementPick<T extends ComposerRef | VideoComposerRef>(
  list: T[],
  pick: ElementPick,
  makeEntry: (p: ElementPick) => T,
  append: (list: T[], entry: T) => T[],
): ElementPickResult<T> {
  if (!isRelayableRef(pick.url)) {
    return { refs: list, action: 'rejected' }
  }
  const key = elementEntityKey(pick.element)
  const hit = list.findIndex((r) => r.element !== undefined && elementEntityKey(r.element) === key)
  if (hit >= 0) {
    if (list[hit].url === pick.url) {
      return { refs: list.filter((_, i) => i !== hit), action: 'removed', index: hit }
    }
    if (list.some((r) => r.url === pick.url)) {
      return { refs: list, action: 'rejected' }
    }
    return {
      refs: list.map((r, i) => (i === hit ? makeEntry(pick) : r)),
      action: 'replaced',
      index: hit,
    }
  }
  if (list.some((r) => r.url === pick.url)) {
    return { refs: list, action: 'rejected' }
  }
  const merged = append(list, makeEntry(pick))
  if (merged === list) {
    return { refs: list, action: 'rejected' }
  }
  return { refs: merged, action: 'added', index: list.length }
}

/** 图片模式合并:新条目 = {url, asset_id, element}。 */
export function mergeImageElementPick(
  list: ComposerRef[],
  pick: ElementPick,
): ElementPickResult<ComposerRef> {
  return mergeElementPick(
    list,
    pick,
    (p) => ({ url: p.url, asset_id: p.asset_id, element: p.element }),
    appendComposerRef,
  )
}

/** 视频模式合并:元素恒 reference_image(71 号票,不走 44 号票默认角色
 * 规则、不占首帧位),替换时同款归一。 */
export function mergeVideoElementPick(
  list: VideoComposerRef[],
  pick: ElementPick,
): ElementPickResult<VideoComposerRef> {
  return mergeElementPick(
    list,
    pick,
    (p) => ({
      url: p.url,
      kind: 'image',
      role: 'reference_image',
      asset_id: p.asset_id,
      element: p.element,
    }),
    appendVideoRef,
  )
}
