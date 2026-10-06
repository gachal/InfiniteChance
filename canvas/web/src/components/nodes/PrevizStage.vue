<script setup lang="ts">
// 预演台 3D 视口(39 号票;40 号票起只在全屏编辑层 PrevizEditorLayer
// 挂载,画布卡片不再跑 three.js):three.js 场景的渲染与编辑交互。地面
// + 固定默认光 + 内置代理(盒/柱/板/人形素体)+ 机位 gizmo(带注视点
// 标记);编辑 = 自由轨道导航(OrbitControls)+ 点选实体 + 平移 gizmo
// (TransformControls,恒 translate 档;旋转/缩放走编辑层右侧检查器的
// 滑杆)。captureFrame() 按当前机位出 16:9 静帧 PNG —— 隐藏网格/gizmo
// 等辅助元素后离屏重渲,零计费、不进 canvas_tasks。尺寸随容器自适应
// (ResizeObserver),固定高 220px 的卡片用法已随 40 号票移除;容器上
// 的 .stop 保留以防组件将来回到画布内使用。
import { onBeforeUnmount, onMounted, ref, watch } from 'vue'
import * as THREE from 'three'
import { OrbitControls } from 'three/examples/jsm/controls/OrbitControls.js'
import { TransformControls } from 'three/examples/jsm/controls/TransformControls.js'

import type { PrevizCamera, PrevizObject } from '../../graph'
import { clampScale, normalizeRotation } from '../../previz'

const props = defineProps<{
  objects: PrevizObject[]
  cameras: PrevizCamera[]
  /** 渲染与「选中即活跃」语义共用的当前机位。 */
  activeCameraId?: string
  /** 被 gizmo 附着的实体 id(对象 id、机位 id 或 `机位id:target`)。 */
  selectedId?: string
}>()

const emit = defineEmits<{
  /** 点选(含点空白取消);id 语义同 selectedId。 */
  select: [id: string | null]
  'object-change': [object: PrevizObject]
  'camera-change': [camera: PrevizCamera]
}>()

const containerEl = ref<HTMLDivElement | null>(null)

defineExpose({ captureFrame })

// ---- three.js 场景对象(mount 时建,unmount 时释放)----

let renderer: THREE.WebGLRenderer
let scene: THREE.Scene
let orbitCamera: THREE.PerspectiveCamera
let orbit: OrbitControls
let transform: TransformControls
let selectionHelper: THREE.BoxHelper
let raf = 0
let resizeObserver: ResizeObserver | undefined
/** WebGL 上下文建立失败(策略/驱动禁用等):视口与渲染优雅降级为不可用。 */
let webglFailed = false

const OBJECT_COLORS: Record<PrevizObject['kind'], number> = {
  box: 0x5b6b8c,
  cylinder: 0x5c8a72,
  board: 0x8c7a5b,
  humanoid: 0xd8dce6,
}

const EDITOR_HOME_POSITION = new THREE.Vector3(5.5, 4.5, 7)
const EDITOR_HOME_TARGET = new THREE.Vector3(0, 1, 0)
/** 静帧渲染分辨率(16:9)。 */
const RENDER_W = 1280
const RENDER_H = 720

const objectGroups = new Map<string, THREE.Group>()
const cameraGizmos = new Map<string, THREE.Group>()
const targetMarkers = new Map<string, THREE.Mesh>()

// 辅助元素(网格/操纵杆/选中框):编辑时可见,渲染静帧时整体隐藏 ——
// 渲染帧只含地面、代理与光;机位 gizmo 与注视点标记独立管理(在
// cameraGizmos/targetMarkers 里),渲染时按 visible 开关。
const helpersGroup = new THREE.Group()

function objectMaterial(kind: PrevizObject['kind']): THREE.MeshStandardMaterial {
  return new THREE.MeshStandardMaterial({ color: OBJECT_COLORS[kind], roughness: 0.85, metalness: 0.05 })
}

function applyShadow(root: THREE.Object3D): void {
  root.traverse((child) => {
    if (child instanceof THREE.Mesh) {
      child.castShadow = true
      child.receiveShadow = true
    }
  })
}

function part(geo: THREE.BufferGeometry, mat: THREE.Material, x: number, y: number, z: number): THREE.Mesh {
  const mesh = new THREE.Mesh(geo, mat)
  mesh.position.set(x, y, z)
  return mesh
}

/** 内置人形素体(39 号票 Q4):站姿、真人比例(总高 ≈1.7 米),零外部
 * 资产 —— 头/颈/躯干/髋/双腿/双臂的几何拼装,组原点在脚底地面点。 */
function buildHumanoid(): THREE.Group {
  const mat = objectMaterial('humanoid')
  const g = new THREE.Group()
  g.add(
    part(new THREE.SphereGeometry(0.115, 20, 16), mat, 0, 1.575, 0),
    part(new THREE.CylinderGeometry(0.04, 0.045, 0.1, 12), mat, 0, 1.42, 0),
    part(new THREE.BoxGeometry(0.38, 0.54, 0.2), mat, 0, 1.13, 0),
    part(new THREE.BoxGeometry(0.32, 0.14, 0.2), mat, 0, 0.8, 0),
    part(new THREE.BoxGeometry(0.14, 0.8, 0.16), mat, -0.1, 0.4, 0),
    part(new THREE.BoxGeometry(0.14, 0.8, 0.16), mat, 0.1, 0.4, 0),
    part(new THREE.BoxGeometry(0.09, 0.52, 0.13), mat, -0.26, 1.11, 0),
    part(new THREE.BoxGeometry(0.09, 0.52, 0.13), mat, 0.26, 1.11, 0),
  )
  return g
}

function buildObjectGroup(kind: PrevizObject['kind']): THREE.Group {
  const g = kind === 'humanoid' ? buildHumanoid() : new THREE.Group()
  if (kind === 'box') {
    g.add(part(new THREE.BoxGeometry(1, 1, 1), objectMaterial(kind), 0, 0.5, 0))
  } else if (kind === 'cylinder') {
    g.add(part(new THREE.CylinderGeometry(0.35, 0.35, 1.8, 24), objectMaterial(kind), 0, 0.9, 0))
  } else if (kind === 'board') {
    g.add(part(new THREE.BoxGeometry(2.2, 2.4, 0.12), objectMaterial(kind), 0, 1.2, 0))
  }
  applyShadow(g)
  return g
}

/** 机位 gizmo:机身小盒 + 前向视锥线框(相机看 -Z,lookAt 后 -Z 指向
 * 注视点)。 */
function buildCameraGizmo(): THREE.Group {
  const g = new THREE.Group()
  const bodyMat = new THREE.MeshStandardMaterial({ color: 0xf472b6, roughness: 0.6 })
  g.add(part(new THREE.BoxGeometry(0.24, 0.16, 0.3), bodyMat, 0, 0, 0))
  const nearZ = -0.15
  const farZ = -0.95
  const nearHalf = { w: 0.1, h: 0.06 }
  const farHalf = { w: 0.34, h: 0.24 }
  const corners: Array<[number, number]> = [
    [-1, -1],
    [1, -1],
    [1, 1],
    [-1, 1],
  ]
  const pts: THREE.Vector3[] = []
  const farCorners: THREE.Vector3[] = []
  for (const [sx, sy] of corners) {
    pts.push(
      new THREE.Vector3(sx * nearHalf.w, sy * nearHalf.h, nearZ),
      new THREE.Vector3(sx * farHalf.w, sy * farHalf.h, farZ),
    )
    farCorners.push(new THREE.Vector3(sx * farHalf.w, sy * farHalf.h, farZ))
  }
  for (let i = 0; i < 4; i++) {
    pts.push(farCorners[i], farCorners[(i + 1) % 4])
  }
  g.add(
    new THREE.LineSegments(
      new THREE.BufferGeometry().setFromPoints(pts),
      new THREE.LineBasicMaterial({ color: 0xf472b6 }),
    ),
  )
  return g
}

function buildTargetMarker(): THREE.Mesh {
  return new THREE.Mesh(
    new THREE.OctahedronGeometry(0.09),
    new THREE.MeshStandardMaterial({ color: 0xfbbf24, roughness: 0.5 }),
  )
}

function setupScene(): void {
  scene = new THREE.Scene()
  scene.background = new THREE.Color(0x1d2434)

  // 固定默认光(39 号票 Q3:光照不可调):半球环境光 + 带阴影的方向光。
  scene.add(new THREE.HemisphereLight(0xcdd7ee, 0x1a2030, 0.9))
  const sun = new THREE.DirectionalLight(0xffffff, 1.6)
  sun.position.set(6, 10, 4)
  sun.castShadow = true
  sun.shadow.mapSize.set(1024, 1024)
  sun.shadow.camera.left = -12
  sun.shadow.camera.right = 12
  sun.shadow.camera.top = 12
  sun.shadow.camera.bottom = -12
  scene.add(sun)

  // 地面:承接阴影的哑光地板(渲染帧里保留作空间锚)+ 编辑态网格
  // (网格属辅助元素,渲染时随 helpersGroup 一起隐藏)。
  const ground = new THREE.Mesh(
    new THREE.PlaneGeometry(80, 80),
    new THREE.MeshStandardMaterial({ color: 0x2a3140, roughness: 1 }),
  )
  ground.rotation.x = -Math.PI / 2
  ground.receiveShadow = true
  scene.add(ground)

  const grid = new THREE.GridHelper(40, 40, 0x4a566e, 0x333c50)
  grid.position.y = 0.002
  helpersGroup.add(grid)
  scene.add(helpersGroup)

  orbitCamera = new THREE.PerspectiveCamera(50, 1, 0.1, 300)
  orbitCamera.position.copy(EDITOR_HOME_POSITION)

  const container = containerEl.value!
  try {
    renderer = new THREE.WebGLRenderer({ antialias: true, preserveDrawingBuffer: true })
  } catch {
    // WebGL 被禁/驱动失败:场景留空、渲染按钮出不了帧,不炸整个节点。
    webglFailed = true
    return
  }
  renderer.setPixelRatio(Math.min(window.devicePixelRatio, 2))
  renderer.shadowMap.enabled = true
  renderer.shadowMap.type = THREE.PCFSoftShadowMap
  container.replaceChildren(renderer.domElement)

  orbit = new OrbitControls(orbitCamera, renderer.domElement)
  orbit.enableDamping = true
  orbit.dampingFactor = 0.12
  orbit.maxPolarAngle = Math.PI / 2 - 0.04
  orbit.target.copy(EDITOR_HOME_TARGET)

  transform = new TransformControls(orbitCamera, renderer.domElement)
  transform.setMode('translate')
  transform.addEventListener('dragging-changed', (event) => {
    orbit.enabled = !event.value
    if (!event.value && transform.object) {
      commitTransformedEntity(transform.object)
    }
  })
  transform.addEventListener('objectChange', liveUpdateDragged)
  helpersGroup.add(transform.getHelper())

  selectionHelper = new THREE.BoxHelper(new THREE.Object3D(), 0x7aa2f7)
  selectionHelper.visible = false
  helpersGroup.add(selectionHelper)

  resizeToContainer()
  // 数据 → 场景的 watch 带 immediate,但触发在 setup 阶段(renderer 未建,
  // 被 guard 拦下):挂载完成后必须显式补一次全量同步,初始场景才会出现。
  syncObjects(props.objects)
  syncCameras(props.cameras)
  loop()
}

function resizeToContainer(): void {
  const el = containerEl.value
  if (!el || webglFailed || typeof renderer === 'undefined') {
    return
  }
  const w = el.clientWidth || 300
  const h = el.clientHeight || 200
  renderer.setSize(w, h, false)
  orbitCamera.aspect = w / h
  orbitCamera.updateProjectionMatrix()
}

function loop(): void {
  raf = requestAnimationFrame(loop)
  orbit.update()
  if (selectionHelper.visible) {
    selectionHelper.update()
  }
  renderer.render(scene, orbitCamera)
}

// ---- 数据 → 场景同步(整图 JSON 是权威;拖拽中的帧跳过,drag end 落
// 数据后回灌同值)----

function syncObjects(objects: PrevizObject[]): void {
  if (typeof renderer === 'undefined') {
    return
  }
  const keep = new Set(objects.map((o) => o.id))
  for (const [id, group] of objectGroups) {
    if (!keep.has(id)) {
      scene.remove(group)
      disposeTree(group)
      objectGroups.delete(id)
    }
  }
  for (const obj of objects) {
    let group = objectGroups.get(obj.id)
    if (!group) {
      group = buildObjectGroup(obj.kind)
      group.userData.entityId = obj.id
      scene.add(group)
      objectGroups.set(obj.id, group)
    }
    group.position.set(obj.position.x, obj.position.y, obj.position.z)
    group.rotation.set(0, THREE.MathUtils.degToRad(obj.rotation), 0)
    group.scale.setScalar(obj.scale)
  }
  reconcileAttachment()
}

function syncCameras(cameras: PrevizCamera[]): void {
  if (typeof renderer === 'undefined') {
    return
  }
  const keep = new Set(cameras.map((c) => c.id))
  for (const [id, gizmo] of cameraGizmos) {
    if (!keep.has(id)) {
      scene.remove(gizmo)
      disposeTree(gizmo)
      cameraGizmos.delete(id)
      const marker = targetMarkers.get(id)
      if (marker) {
        scene.remove(marker)
        marker.geometry.dispose()
        targetMarkers.delete(id)
      }
    }
  }
  for (const cam of cameras) {
    let gizmo = cameraGizmos.get(cam.id)
    let marker = targetMarkers.get(cam.id)
    if (!gizmo) {
      gizmo = buildCameraGizmo()
      gizmo.userData.entityId = cam.id
      scene.add(gizmo)
      cameraGizmos.set(cam.id, gizmo)
    }
    if (!marker) {
      marker = buildTargetMarker()
      marker.userData.entityId = `${cam.id}:target`
      marker.userData.cameraId = cam.id
      scene.add(marker)
      targetMarkers.set(cam.id, marker)
    }
    gizmo.position.set(cam.position.x, cam.position.y, cam.position.z)
    gizmo.lookAt(cam.target.x, cam.target.y, cam.target.z)
    marker.position.set(cam.target.x, cam.target.y, cam.target.z)
  }
  reconcileAttachment()
}

/** 撤销/重做与外部删除会把实体从图里整包换掉,而 selectedId 是卡片内
 * UI 态不随之清空 —— 附着实体已不存在时在这里兜底收回 gizmo 与选中框。 */
function reconcileAttachment(): void {
  if (typeof transform === 'undefined' || !transform.object) {
    return
  }
  const id = transform.object.userData.entityId as string | undefined
  if (!id) {
    return
  }
  const exists = id.endsWith(':target')
    ? targetMarkers.has(id.slice(0, -':target'.length))
    : objectGroups.has(id) || cameraGizmos.has(id)
  if (!exists) {
    transform.detach()
    selectionHelper.visible = false
  }
}

watch(() => props.objects, (list) => syncObjects(list), { deep: true, immediate: true })
watch(() => props.cameras, (list) => syncCameras(list), { deep: true, immediate: true })

// ---- 选中与 gizmo 附着 ----

function entityGroupFor(id: string | undefined): THREE.Object3D | null {
  if (!id) {
    return null
  }
  if (id.endsWith(':target')) {
    return targetMarkers.get(id.slice(0, -':target'.length)) ?? null
  }
  return objectGroups.get(id) ?? cameraGizmos.get(id) ?? null
}

watch(
  () => props.selectedId,
  (id) => {
    if (typeof transform === 'undefined') {
      return
    }
    const target = entityGroupFor(id)
    if (target) {
      transform.attach(target)
      selectionHelper.setFromObject(target)
      // 注视点标记很小,选中框反而碍事:gizmo 平移杆已表达选中。
      selectionHelper.visible = !(id?.endsWith(':target') ?? false)
    } else {
      transform.detach()
      selectionHelper.visible = false
    }
  },
)

/** 拖拽中的实时反馈:机位注视点移动时机位 gizmo 跟着转头。 */
function liveUpdateDragged(): void {
  const dragging = transform.object
  if (!dragging) {
    return
  }
  const cameraId = dragging.userData.cameraId as string | undefined
  if (cameraId) {
    cameraGizmos.get(cameraId)?.lookAt(dragging.position.x, dragging.position.y, dragging.position.z)
  }
  if (selectionHelper.visible) {
    selectionHelper.update()
  }
}

/** gizmo 松手:把组变换折回数据模型(位置保留、对象 y 抬升非负、机位
 * 高度 0.1 起步;旋转只取 Y 轴短弧;缩放取三轴平均收等比),上抛给节点
 * 写回 data。 */
function commitTransformedEntity(group: THREE.Object3D): void {
  const entityId = group.userData.entityId as string | undefined
  if (!entityId) {
    return
  }
  const round = (n: number) => Math.round(n * 100) / 100
  if (entityId.endsWith(':target')) {
    const cam = props.cameras.find((c) => c.id === group.userData.cameraId)
    if (cam) {
      emit('camera-change', {
        ...cam,
        target: { x: round(group.position.x), y: round(group.position.y), z: round(group.position.z) },
      })
    }
    return
  }
  const obj = props.objects.find((o) => o.id === entityId)
  if (obj) {
    emit('object-change', {
      ...obj,
      position: {
        x: round(group.position.x),
        y: Math.max(0, round(group.position.y)),
        z: round(group.position.z),
      },
      rotation: normalizeRotation(THREE.MathUtils.radToDeg(group.rotation.y)),
      scale: clampScale((group.scale.x + group.scale.y + group.scale.z) / 3),
    })
    return
  }
  const cam = props.cameras.find((c) => c.id === entityId)
  if (cam) {
    emit('camera-change', {
      ...cam,
      position: {
        x: round(group.position.x),
        y: Math.max(0.1, round(group.position.y)),
        z: round(group.position.z),
      },
    })
  }
}

// ---- 点选:按下/抬起位移小于阈值视为点击,raycast 命中实体上报 ----

let pointerDown: { x: number; y: number } | null = null

function onPointerDown(event: PointerEvent): void {
  pointerDown = { x: event.clientX, y: event.clientY }
}

function onPointerUp(event: PointerEvent): void {
  const down = pointerDown
  pointerDown = null
  if (!down) {
    return
  }
  // 拖拽(轨道导航或 gizmo)不算点选;悬停在 gizmo 轴上时把点击让给它。
  if (Math.hypot(event.clientX - down.x, event.clientY - down.y) > 5 || transform.axis) {
    return
  }
  const rect = renderer.domElement.getBoundingClientRect()
  const ndc = new THREE.Vector2(
    ((event.clientX - rect.left) / rect.width) * 2 - 1,
    -((event.clientY - rect.top) / rect.height) * 2 + 1,
  )
  const raycaster = new THREE.Raycaster()
  raycaster.setFromCamera(ndc, orbitCamera)
  const targets: THREE.Object3D[] = [
    ...objectGroups.values(),
    ...cameraGizmos.values(),
    ...targetMarkers.values(),
  ]
  const hits = raycaster.intersectObjects(targets, true)
  const hit = hits.find((h) => h.object.visible)
  if (!hit) {
    emit('select', null)
    return
  }
  let node: THREE.Object3D | null = hit.object
  while (node && !node.userData.entityId) {
    node = node.parent
  }
  emit('select', (node?.userData.entityId as string | undefined) ?? null)
}

// ---- 静帧渲染:隐藏辅助元素,按当前机位 16:9 离屏出 PNG ----

async function captureFrame(): Promise<Blob | null> {
  if (webglFailed || typeof renderer === 'undefined') {
    return null
  }
  const camData = props.cameras.find((c) => c.id === props.activeCameraId) ?? props.cameras[0]
  if (!camData) {
    return null
  }
  const frameCamera = new THREE.PerspectiveCamera(camData.fov, RENDER_W / RENDER_H, 0.1, 300)
  frameCamera.position.set(camData.position.x, camData.position.y, camData.position.z)
  frameCamera.lookAt(camData.target.x, camData.target.y, camData.target.z)

  const prevSize = renderer.getSize(new THREE.Vector2())
  const prevRatio = renderer.getPixelRatio()
  const prevHelpersVisible = helpersGroup.visible
  // 暂停交互渲染循环:toBlob 是异步的,RAF 若继续跑会在 1280x720 缓冲上
  // 重绘交互视图,网格/gizmo 会肉眼可见地闪几帧。
  cancelAnimationFrame(raf)
  try {
    helpersGroup.visible = false
    for (const marker of targetMarkers.values()) {
      marker.visible = false
    }
    renderer.setPixelRatio(1)
    renderer.setSize(RENDER_W, RENDER_H, false)
    renderer.render(scene, frameCamera)
    return await new Promise<Blob | null>((resolve) => renderer.domElement.toBlob(resolve, 'image/png'))
  } finally {
    for (const marker of targetMarkers.values()) {
      marker.visible = true
    }
    helpersGroup.visible = prevHelpersVisible
    renderer.setPixelRatio(prevRatio)
    renderer.setSize(prevSize.x, prevSize.y, false)
    loop()
  }
}

/** 编辑器视角回正(视口右上角小按钮)。 */
function resetView(): void {
  orbitCamera.position.copy(EDITOR_HOME_POSITION)
  orbit.target.copy(EDITOR_HOME_TARGET)
}

function disposeTree(root: THREE.Object3D): void {
  root.traverse((child) => {
    if (child instanceof THREE.Mesh || child instanceof THREE.LineSegments) {
      child.geometry.dispose()
      const mat = child.material as THREE.Material | THREE.Material[]
      if (Array.isArray(mat)) {
        mat.forEach((m) => m.dispose())
      } else {
        mat.dispose()
      }
    }
  })
}

onMounted(() => {
  setupScene()
  const el = containerEl.value!
  el.addEventListener('pointerdown', onPointerDown)
  el.addEventListener('pointerup', onPointerUp)
  resizeObserver = new ResizeObserver(resizeToContainer)
  resizeObserver.observe(el)
})

onBeforeUnmount(() => {
  cancelAnimationFrame(raf)
  resizeObserver?.disconnect()
  const el = containerEl.value
  if (el) {
    el.removeEventListener('pointerdown', onPointerDown)
    el.removeEventListener('pointerup', onPointerUp)
  }
  orbit?.dispose()
  transform?.dispose()
  for (const group of objectGroups.values()) {
    disposeTree(group)
  }
  for (const gizmo of cameraGizmos.values()) {
    disposeTree(gizmo)
  }
  renderer?.dispose()
  // 主动释放上下文配额:浏览器每页 WebGL 上下文有限,删除节点后及时归还。
  renderer?.forceContextLoss()
})
</script>

<template>
  <!-- 指针事件绝不 .stop:OrbitControls 拖拽期间把 pointermove/pointerup
       挂在 ownerDocument 上(为接住画布外松手),容器上 stopPropagation 会
       拦掉 pointerup,旋转状态卡死在「按住」态 —— 下一次鼠标移动就把相机
       按旧点到新点的位移一次性甩出去(40 号票修复的实际线上症状)。与
       vue-flow 的手势隔离用 nodrag/nowheel 类(vue-flow 官方逃生口),不用
       阻断冒泡;contextmenu/dblclick 无文档级监听,照旧拦截。 -->
  <div
    ref="containerEl"
    class="stage nodrag nowheel"
    @contextmenu.stop.prevent
    @dblclick.stop.prevent
  >
    <button
      class="reset-view"
      type="button"
      title="编辑视角回正"
      aria-label="编辑视角回正"
      @pointerdown.stop
      @click.stop="resetView"
    >
      ⌂
    </button>
  </div>
</template>

<style scoped>
.stage {
  position: relative;
  /* 尺寸交给宿主(编辑层的视口窗格),组件随容器自适应。 */
  width: 100%;
  height: 100%;
  border-radius: 10px;
  overflow: hidden;
  background: #1d2434;
  border: 1px solid rgba(244, 114, 182, 0.25);
  touch-action: none;
  cursor: grab;
}

.stage:active {
  cursor: grabbing;
}

.stage :deep(canvas) {
  display: block;
  width: 100% !important;
  height: 100% !important;
}

.reset-view {
  position: absolute;
  top: 6px;
  right: 6px;
  z-index: 2;
  width: 26px;
  height: 26px;
  display: grid;
  place-items: center;
  border: 1px solid rgba(255, 255, 255, 0.18);
  border-radius: 8px;
  background: rgba(13, 18, 32, 0.85);
  color: #dfe3ee;
  font-size: 14px;
  cursor: pointer;
}

.reset-view:hover {
  border-color: rgba(122, 162, 247, 0.85);
  color: #fff;
}
</style>
