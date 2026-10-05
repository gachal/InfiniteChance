import { describe, expect, it } from 'vitest'

import {
  PREVIZ_FOV_RANGE,
  PREVIZ_SCALE_RANGE,
  appendCamera,
  appendObject,
  clampFov,
  clampScale,
  defaultPrevizScene,
  nextEntityId,
  nextCameraName,
  nextObjectName,
  normalizeRotation,
  removeCamera,
  spawnPosition,
} from './previz'

describe('defaultPrevizScene', () => {
  it('seeds one humanoid and one camera, with the camera active', () => {
    const scene = defaultPrevizScene()
    expect(scene.objects).toHaveLength(1)
    expect(scene.objects[0].kind).toBe('humanoid')
    expect(scene.cameras).toHaveLength(1)
    expect(scene.activeCameraId).toBe(scene.cameras[0].id)
    // 机位齐眼高、注视场景内部。
    expect(scene.cameras[0].position.y).toBeGreaterThan(0)
    expect(scene.cameras[0].fov).toBeGreaterThan(0)
  })
})

describe('nextEntityId / nextObjectName / nextCameraName', () => {
  it('allocates beyond the current maximum, never reusing deleted numbers', () => {
    const existing = [{ id: 'obj-1' }, { id: 'obj-5' }]
    expect(nextEntityId('obj', existing)).toBe('obj-6')
    expect(nextEntityId('cam', [])).toBe('cam-1')
  })

  it('ignores malformed ids instead of crashing', () => {
    expect(nextEntityId('obj', [{ id: 'obj-x' }, { id: '' }])).toBe('obj-1')
  })

  it('counts object names per kind', () => {
    const scene = defaultPrevizScene()
    expect(nextObjectName('humanoid', scene.objects)).toBe('人形 2')
    expect(nextObjectName('box', scene.objects)).toBe('盒 1')
  })

  it('numbers names beyond the max so deleted middles never duplicate', () => {
    // 与 id 的不复用策略一致:盒 1/2/3 删 2 再加,新名是 盒 4 而非 盒 2。
    const objects = [
      { id: 'obj-1', kind: 'box' as const, name: '盒 1', position: { x: 0, y: 0, z: 0 }, rotation: 0, scale: 1 },
      { id: 'obj-2', kind: 'box' as const, name: '盒 2', position: { x: 0, y: 0, z: 0 }, rotation: 0, scale: 1 },
      { id: 'obj-3', kind: 'box' as const, name: '盒 3', position: { x: 0, y: 0, z: 0 }, rotation: 0, scale: 1 },
    ]
    const withoutMiddle = objects.filter((o) => o.id !== 'obj-2')
    expect(nextObjectName('box', withoutMiddle)).toBe('盒 4')
  })

  it('numbers cameras beyond the max as well', () => {
    const cameras = [
      { id: 'cam-1', name: '机位 1', position: { x: 0, y: 1.6, z: 0 }, target: { x: 0, y: 1, z: 0 }, fov: 45 },
      { id: 'cam-4', name: '机位 4', position: { x: 0, y: 1.6, z: 0 }, target: { x: 0, y: 1, z: 0 }, fov: 45 },
    ]
    expect(nextCameraName(cameras)).toBe('机位 5')
  })

  it('numbers cameras sequentially', () => {
    const scene = defaultPrevizScene()
    expect(nextCameraName(scene.cameras)).toBe('机位 2')
  })
})

describe('appendObject / appendCamera', () => {
  it('appends an object without mutating the input list', () => {
    const scene = defaultPrevizScene()
    const { object, objects } = appendObject(scene.objects, 'box')
    expect(scene.objects).toHaveLength(1)
    expect(objects).toHaveLength(2)
    expect(object.kind).toBe('box')
    expect(object.name).toBe('盒 1')
    expect(object.scale).toBe(1)
    // 落点不在原点(默认人形所在),y 落地。
    expect(object.position).not.toEqual({ x: 0, y: 0, z: 0 })
    expect(object.position.y).toBe(0)
  })

  it('appends a camera at eye height looking at the origin', () => {
    const scene = defaultPrevizScene()
    const { camera, cameras } = appendCamera(scene.cameras)
    expect(cameras).toHaveLength(2)
    expect(camera.name).toBe('机位 2')
    expect(camera.position.y).toBe(1.6)
    expect(camera.target).toEqual({ x: 0, y: 1, z: 0 })
  })
})

describe('removeCamera', () => {
  it('keeps at least one camera: refuses when only one remains', () => {
    const scene = defaultPrevizScene()
    expect(removeCamera(scene.cameras, scene.cameras[0].id, scene.activeCameraId)).toBeNull()
  })

  it('removes a camera and re-seats the active one when it was removed', () => {
    const scene = defaultPrevizScene()
    const second = appendCamera(scene.cameras)
    const result = removeCamera(second.cameras, second.camera.id, scene.activeCameraId)
    expect(result).not.toBeNull()
    expect(result!.cameras).toHaveLength(1)
    expect(result!.activeCameraId).toBe(scene.activeCameraId)

    const removedActive = removeCamera(second.cameras, scene.cameras[0].id, scene.cameras[0].id)
    expect(removedActive!.activeCameraId).toBe(second.camera.id)
  })
})

describe('spawnPosition', () => {
  it('spreads the first ring away from the origin and lands on the ground', () => {
    for (let i = 0; i < 5; i++) {
      const p = spawnPosition(i)
      expect(p.y).toBe(0)
      // 半径 2 的环,坐标保留两位小数后允许微小收缩。
      expect(Math.hypot(p.x, p.z)).toBeGreaterThanOrEqual(1.9)
    }
  })

  it('walks outward on the second ring', () => {
    expect(Math.hypot(spawnPosition(7).x, spawnPosition(7).z)).toBeGreaterThan(
      Math.hypot(spawnPosition(2).x, spawnPosition(2).z),
    )
  })
})

describe('clampFov / clampScale / normalizeRotation', () => {
  it('clamps fov into the configured range', () => {
    expect(clampFov(45)).toBe(45)
    expect(clampFov(5)).toBe(PREVIZ_FOV_RANGE.min)
    expect(clampFov(999)).toBe(PREVIZ_FOV_RANGE.max)
    expect(clampFov(Number.NaN)).toBe(PREVIZ_FOV_RANGE.min)
  })

  it('clamps scale and falls back to 1 for garbage', () => {
    expect(clampScale(1.5)).toBe(1.5)
    expect(clampScale(0.01)).toBe(PREVIZ_SCALE_RANGE.min)
    expect(clampScale(50)).toBe(PREVIZ_SCALE_RANGE.max)
    expect(clampScale(Number.NaN)).toBe(1)
    expect(clampScale(-2)).toBe(1)
  })

  it('normalizes rotation into [-180, 180)', () => {
    expect(normalizeRotation(0)).toBe(0)
    expect(normalizeRotation(270)).toBe(-90)
    expect(normalizeRotation(-270)).toBe(90)
    expect(normalizeRotation(360)).toBe(0)
    // 540° 的短弧表示是 -180(半开区间的左端)。
    expect(normalizeRotation(540)).toBe(-180)
    expect(normalizeRotation(Number.NaN)).toBe(0)
  })
})
