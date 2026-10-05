/**
 * 预演台场景的纯逻辑(39 号票):默认场景、对象/机位的增删与编号、
 * 数值夹紧。组件只做渲染与交互,这里集中可单测的判定 —— 不依赖
 * three.js,测试保持在纯 DOM 之外。
 */
import type { PrevizCamera, PrevizObject, PrevizObjectKind } from '@infinitechance/api'

export type { PrevizCamera, PrevizObject, PrevizObjectKind }

/** 各类代理对象的展示名前缀(编号跟随种类各自累计)。 */
export const PREVIZ_KIND_LABEL: Record<PrevizObjectKind, string> = {
  box: '盒',
  cylinder: '柱',
  board: '板',
  humanoid: '人形',
}

/** 机位竖直视场角的范围(度),对应常用摄影镜头焦段;越界输入在入口
 * 夹紧。 */
export const PREVIZ_FOV_RANGE = { min: 20, max: 100 } as const

/** 代理对象等比缩放范围。 */
export const PREVIZ_SCALE_RANGE = { min: 0.2, max: 5 } as const

/** 一次完整预演:对象列表 + 机位列表(节点 data 的场景部分)。 */
export interface PrevizScene {
  objects: PrevizObject[]
  cameras: PrevizCamera[]
}

/** 新预演节点的初始场景:一个人形 + 一个机位(能立刻看到可渲染的
 * 东西),机位在右前上方注视原点。 */
export function defaultPrevizScene(): PrevizScene & { activeCameraId: string } {
  const objects: PrevizObject[] = [
    { id: 'obj-1', kind: 'humanoid', name: '人形 1', position: { x: 0, y: 0, z: 0 }, rotation: 0, scale: 1 },
  ]
  const cameras: PrevizCamera[] = [
    {
      id: 'cam-1',
      name: '机位 1',
      position: { x: 3.5, y: 1.6, z: 4.5 },
      target: { x: 0, y: 1, z: 0 },
      fov: 45,
    },
  ]
  return { objects, cameras, activeCameraId: cameras[0].id }
}

/** 场景内下一个可用编号的实体 id:取既有最大号 +1,删除中间的实体不复用
 * 旧号(id 只要求图内唯一,不复用可避免整图 JSON 里旧引用撞新实体)。 */
export function nextEntityId(prefix: 'obj' | 'cam', existing: { id: string }[]): string {
  let max = 0
  for (const item of existing) {
    const n = Number(item.id.slice(prefix.length + 1))
    if (Number.isInteger(n) && n > max) {
      max = n
    }
  }
  return `${prefix}-${max + 1}`
}

/** 新对象的展示名:按同类既有最大号 +1 —— 与 id 的不复用策略一致,
 * 删除中间项后再加不会产生重名(盒 1、盒 2 删 1 再加 = 盒 3 而非 盒 2)。 */
export function nextObjectName(kind: PrevizObjectKind, existing: PrevizObject[]): string {
  const prefix = PREVIZ_KIND_LABEL[kind]
  let max = 0
  for (const o of existing) {
    if (o.kind !== kind) {
      continue
    }
    const n = Number(o.name.slice(prefix.length + 1))
    if (Number.isInteger(n) && n > max) {
      max = n
    }
  }
  return `${prefix} ${max + 1}`
}

/** 新机位的展示名:按最大号 +1,同 nextObjectName 的不复用策略。 */
export function nextCameraName(existing: PrevizCamera[]): string {
  let max = 0
  for (const c of existing) {
    const n = Number(c.name.slice('机位 '.length))
    if (Number.isInteger(n) && n > max) {
      max = n
    }
  }
  return `机位 ${max + 1}`
}

/** 新对象的落点:绕原点的稀疏环(每 72° 一个位,两圈半径递增),连加
 * 多个不错叠、不压在默认人形上。 */
export function spawnPosition(index: number): { x: number; y: number; z: number } {
  const angle = (index % 5) * ((Math.PI * 2) / 5)
  const radius = 2 + Math.floor(index / 5) * 1.5
  return {
    x: round2(Math.sin(angle) * radius),
    y: 0,
    z: round2(Math.cos(angle) * radius),
  }
}

/** 追加一个代理对象:返回新对象与更新后的列表(纯函数,不改入参)。 */
export function appendObject(
  objects: PrevizObject[],
  kind: PrevizObjectKind,
): { object: PrevizObject; objects: PrevizObject[] } {
  const object: PrevizObject = {
    id: nextEntityId('obj', objects),
    kind,
    name: nextObjectName(kind, objects),
    position: spawnPosition(objects.length),
    rotation: 0,
    scale: 1,
  }
  return { object, objects: [...objects, object] }
}

/** 追加一个机位:落在生成环外侧、齐眼高,注视原点;返回新机位与列表。 */
export function appendCamera(cameras: PrevizCamera[]): { camera: PrevizCamera; cameras: PrevizCamera[] } {
  const camera: PrevizCamera = {
    id: nextEntityId('cam', cameras),
    name: nextCameraName(cameras),
    position: spawnPosition(cameras.length + 1),
    target: { x: 0, y: 1, z: 0 },
    fov: 45,
  }
  camera.position.y = 1.6
  return { camera, cameras: [...cameras, camera] }
}

/** 删除一个机位;至少保留一个(没有机位就没有渲染出口)。删的是当前
 * 机位时,活跃位顺延到剩余列表的第一个。不可删返回 null。 */
export function removeCamera(
  cameras: PrevizCamera[],
  id: string,
  activeCameraId: string,
): { cameras: PrevizCamera[]; activeCameraId: string } | null {
  if (cameras.length <= 1) {
    return null
  }
  const next = cameras.filter((c) => c.id !== id)
  return {
    cameras: next,
    activeCameraId: activeCameraId === id ? next[0].id : activeCameraId,
  }
}

/** FOV 输入夹紧:非有限数或越界都收到最近端。 */
export function clampFov(fov: number): number {
  if (!Number.isFinite(fov)) {
    return PREVIZ_FOV_RANGE.min
  }
  return round1(Math.min(PREVIZ_FOV_RANGE.max, Math.max(PREVIZ_FOV_RANGE.min, fov)))
}

/** 缩放输入夹紧:非有限数或非正数回落 1。 */
export function clampScale(scale: number): number {
  if (!Number.isFinite(scale) || scale <= 0) {
    return 1
  }
  return round2(Math.min(PREVIZ_SCALE_RANGE.max, Math.max(PREVIZ_SCALE_RANGE.min, scale)))
}

/** 旋转角归一到 [-180, 180):gizmo 拖出的多圈角度收进半开区间,
 * JSON 里永远是短弧表示。 */
export function normalizeRotation(degrees: number): number {
  if (!Number.isFinite(degrees)) {
    return 0
  }
  let d = degrees % 360
  if (d >= 180) {
    d -= 360
  }
  if (d < -180) {
    d += 360
  }
  return round1(d)
}

function round1(n: number): number {
  return Math.round(n * 10) / 10
}

function round2(n: number): number {
  return Math.round(n * 100) / 100
}
