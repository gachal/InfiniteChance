<script setup lang="ts">
// 全景球面查看层(81 号票):产物态整卡点击 / 「直接环视」进入的全屏浮层
// (编辑层范式,Teleport 到 body 单实例,z 90 族 = 编辑层档)。等距柱状
// (2:1)贴图贴内翻球面(three.js 0.186.1 + OrbitControls,预演台 39/40
// 号票已带依赖):拖动环视 + 滚轮缩放 + 阻尼,**全俯仰**(预演台的地平线
// 钳制不照搬 —— 球面要能看到天顶与天底)。贴图源 = /api/assets/{id}/content
// 同源代理(26 号票读兜底),无 CORS 问题,截图导出不受画布污染。
// 查看层截图(票·定案 8):取景比例五档(1:1/16:9/9:16/4:3/3:4,缺省
// 16:9)、长边固定 1920 —— 按目标比例临时改渲染尺寸离屏渲一帧导出 JPEG
// 上抛,入库与 toast 在宿主侧(编辑器挂画布 IP,素材抽屉挂实体 IP);
// 按钮 + C 键(与灯箱截帧 S 键分层不冲突),连续截不关层。
// 8K(7680×3840)贴图性能:超出设备 maxTextureSize 时查看器侧降采样
// (实现裁量,验证点 2),不超则原图直贴。
import { onBeforeUnmount, onMounted, ref } from 'vue'
import * as THREE from 'three'
import { OrbitControls } from 'three/examples/jsm/controls/OrbitControls.js'

import { PANO_CAPTURE_DEFAULT_RATIO, PANO_CAPTURE_RATIOS, captureSize } from '../panorama'
import { assetContentURL } from '../thumbnail'

const props = defineProps<{
  /** 贴图素材行 id:贴图源 = /api/assets/{id}/content(同源代理)。 */
  assetId: number
}>()

const emit = defineEmits<{
  close: []
  /** 截图成功:当前取景 JPEG blob(长边 1920)+ 所选比例,入库由宿主处理。 */
  capture: [blob: Blob]
  /** 截图失败:面向用户的报错文案,宿主 toast 展示(浮层自身不持提示面)。 */
  'capture-error': [message: string]
}>()

const rootEl = ref<HTMLDivElement | null>(null)
const containerEl = ref<HTMLDivElement | null>(null)
let opener: HTMLElement | null = null

// 贴图加载态:loading(球面未就绪)→ ready / error(素材被删或解码失败)。
const texStatus = ref<'loading' | 'ready' | 'error'>('loading')

// 截图取景比例(缺省 16:9);比例档复用 panorama.PANO_CAPTURE_RATIOS。
const captureRatio = ref(PANO_CAPTURE_DEFAULT_RATIO)
const capturing = ref(false)

// ---- three.js 场景对象(mount 时建,unmount 时释放;PrevizStage 同款
// 生命周期纪律)----

let renderer: THREE.WebGLRenderer
let scene: THREE.Scene
let camera: THREE.PerspectiveCamera
let controls: OrbitControls
let sphere: THREE.Mesh
let texture: THREE.Texture | null = null
let raf = 0
let resizeObserver: ResizeObserver | undefined
let webglFailed = false

function setupScene(): void {
  scene = new THREE.Scene()
  camera = new THREE.PerspectiveCamera(75, 1, 0.1, 1100)
  camera.position.set(0, 0, 0.01)

  const container = containerEl.value
  if (!container) {
    return
  }
  try {
    renderer = new THREE.WebGLRenderer({ antialias: true, preserveDrawingBuffer: true })
  } catch {
    // WebGL 被禁/驱动失败:查看层优雅降级为不可用,不炸整个编辑器。
    webglFailed = true
    texStatus.value = 'error'
    return
  }
  renderer.setPixelRatio(Math.min(window.devicePixelRatio, 2))
  container.replaceChildren(renderer.domElement)

  // 内翻球面:几何 x 轴取反,纹理贴内侧,相机在球心向外看。
  const geometry = new THREE.SphereGeometry(500, 64, 48)
  geometry.scale(-1, 1, 1)
  sphere = new THREE.Mesh(geometry, new THREE.MeshBasicMaterial({ color: 0x10151f }))
  scene.add(sphere)

  controls = new OrbitControls(camera, renderer.domElement)
  controls.enableDamping = true
  controls.dampingFactor = 0.08
  controls.enablePan = false
  controls.rotateSpeed = -0.35
  controls.zoomSpeed = 0.6
  // 全俯仰:极角不设地平线钳制(0..π 默认区间),天顶天底都可达;
  // 缩放 = 相机离球心距离,钳在球内。
  controls.minDistance = 0.1
  controls.maxDistance = 200

  resizeToContainer()
  loop()
}

function resizeToContainer(): void {
  const el = containerEl.value
  if (!el || webglFailed || typeof renderer === 'undefined') {
    return
  }
  const w = el.clientWidth || window.innerWidth
  const h = el.clientHeight || window.innerHeight
  renderer.setSize(w, h, false)
  camera.aspect = w / h
  camera.updateProjectionMatrix()
}

function loop(): void {
  raf = requestAnimationFrame(loop)
  controls.update()
  renderer.render(scene, camera)
}

// ---- 贴图加载:Image 解码后按设备上限降采样,再上 GPU ----

/** 8K 贴图的上采样上限:min(设备 maxTextureSize, 8192)。7680×3840 在
 * 桌面 GPU 直接可贴;超限设备(Wails WebView 老机型等)按比例缩到上限,
 * 显存降 4 倍量级,观感仍优于加载失败。 */
function textureLongEdgeCap(): number {
  if (webglFailed || typeof renderer === 'undefined') {
    return 4096
  }
  return Math.min(renderer.capabilities.maxTextureSize, 8192)
}

async function loadTexture(url: string): Promise<void> {
  texStatus.value = 'loading'
  try {
    const img = new Image()
    img.src = url
    await new Promise<void>((resolve, reject) => {
      img.onload = () => resolve()
      img.onerror = () => reject(new Error('texture load failed'))
    })
    if (webglFailed || typeof renderer === 'undefined') {
      return
    }
    let source: TexImageSource = img
    const cap = textureLongEdgeCap()
    if (img.naturalWidth > cap) {
      const scale = cap / img.naturalWidth
      const canvas = document.createElement('canvas')
      canvas.width = Math.round(img.naturalWidth * scale)
      canvas.height = Math.round(img.naturalHeight * scale)
      canvas.getContext('2d')!.drawImage(img, 0, 0, canvas.width, canvas.height)
      source = canvas
    }
    texture = new THREE.Texture(source)
    texture.colorSpace = THREE.SRGBColorSpace
    texture.needsUpdate = true
    // 换材质先弃旧(占位素色材质),防 GPU 侧材质对象泄漏。
    const prevMat = sphere.material as THREE.Material
    sphere.material = new THREE.MeshBasicMaterial({ map: texture })
    prevMat.dispose()
    texStatus.value = 'ready'
  } catch {
    texStatus.value = 'error'
  }
}

// ---- 截图:按取景比例离屏渲一帧,长边固定 1920,导出 JPEG ----

async function capture(): Promise<void> {
  if (webglFailed || capturing.value || texStatus.value !== 'ready') {
    return
  }
  capturing.value = true
  const { width, height } = captureSize(captureRatio.value)
  // 暂停交互渲染循环:toBlob 是异步的,RAF 若继续跑会在离屏缓冲上重绘
  // 交互视图(PrevizStage.captureFrame 同款纪律)。
  cancelAnimationFrame(raf)
  const prevSize = renderer.getSize(new THREE.Vector2())
  const prevRatio = renderer.getPixelRatio()
  try {
    renderer.setPixelRatio(1)
    renderer.setSize(width, height, false)
    camera.aspect = width / height
    camera.updateProjectionMatrix()
    renderer.render(scene, camera)
    const blob = await new Promise<Blob | null>((resolve) =>
      renderer.domElement.toBlob(resolve, 'image/jpeg', 0.92),
    )
    if (!blob) {
      emit('capture-error', '截图失败,请稍后再试')
      return
    }
    emit('capture', blob)
  } catch {
    emit('capture-error', '当前浏览器不支持导出截图')
  } finally {
    renderer.setPixelRatio(prevRatio)
    renderer.setSize(prevSize.x, prevSize.y, false)
    camera.aspect = prevSize.x / prevSize.y
    camera.updateProjectionMatrix()
    capturing.value = false
    loop()
  }
}

function onKeydown(event: KeyboardEvent): void {
  if (event.key === 'Escape') {
    emit('close')
    return
  }
  // C 截图:带修饰键不触发(不劫持 ⌘S/⌘C);与灯箱截帧的 S 键分层——
  // 两层同时在场时只会有一个键盘处理生效,字母错开从根上避免歧义。
  if (
    (event.key === 'c' || event.key === 'C') &&
    !event.ctrlKey && !event.metaKey && !event.altKey
  ) {
    event.preventDefault()
    void capture()
  }
}

onMounted(() => {
  opener = (document.activeElement as HTMLElement | null) ?? null
  rootEl.value?.focus()
  window.addEventListener('keydown', onKeydown)
  setupScene()
  void loadTexture(assetContentURL(props.assetId))
  const el = containerEl.value
  if (el) {
    resizeObserver = new ResizeObserver(resizeToContainer)
    resizeObserver.observe(el)
  }
})

onBeforeUnmount(() => {
  window.removeEventListener('keydown', onKeydown)
  resizeObserver?.disconnect()
  cancelAnimationFrame(raf)
  controls?.dispose()
  sphere?.geometry.dispose()
  const mat = sphere?.material as THREE.Material | undefined
  mat?.dispose()
  texture?.dispose()
  renderer?.dispose()
  // 主动释放上下文配额(PrevizStage 同款):查看层开关频繁时及时归还。
  renderer?.forceContextLoss()
  opener?.focus?.()
})
</script>

<template>
  <Teleport to="body">
    <div
      ref="rootEl"
      class="pano-viewer"
      tabindex="-1"
      role="dialog"
      aria-label="360° 全景查看"
    >
      <!-- 顶部:返回 + 360° 角标(查看层带角标,票·定案 2)。 -->
      <div class="viewer-top">
        <button
          class="viewer-btn"
          type="button"
          title="返回(Esc)"
          @click="emit('close')"
        >
          ← 返回
        </button>
        <span class="viewer-badge">360°</span>
      </div>
      <button
        class="viewer-btn close"
        type="button"
        title="关闭查看"
        aria-label="关闭查看"
        @click="emit('close')"
      >
        ✕
      </button>

      <!-- 球面视口:贴图就绪前/失败时的占位居中。 -->
      <div
        ref="containerEl"
        class="viewer-stage"
      >
        <div
          v-if="texStatus !== 'ready'"
          class="viewer-state"
        >
          <template v-if="texStatus === 'loading'">
            <span>正在加载全景贴图…</span>
            <small>8K 贴图首次加载可能需要数秒</small>
          </template>
          <template v-else>
            <span>全景素材不可用</span>
            <small>原素材可能已被删除</small>
          </template>
        </div>
      </div>

      <!-- 底部:取景比例 + 截图 + 操作提示。 -->
      <div class="viewer-bar">
        <span class="viewer-hint">拖动环视 · 滚轮缩放 · 可看天顶天底</span>
        <label class="viewer-ratio">
          取景比例
          <select
            v-model="captureRatio"
            aria-label="截图取景比例"
          >
            <option
              v-for="r in PANO_CAPTURE_RATIOS"
              :key="r.value"
              :value="r.value"
            >
              {{ r.label }}
            </option>
          </select>
        </label>
        <button
          class="viewer-btn capture"
          type="button"
          :disabled="capturing || texStatus !== 'ready'"
          title="截图入素材库(C);仅入库,不落画布节点"
          @click="capture"
        >
          {{ capturing ? '截图中…' : '📷 截图' }}
        </button>
      </div>
    </div>
  </Teleport>
</template>

<style scoped>
.pano-viewer {
  position: fixed;
  inset: 0;
  /* z 90 族 = 编辑层档(编辑层范式,样式表 z 档位表;帮助 z95/灯箱 z100
     仍在它之上,Esc 栈序由编辑器全局链裁决)。 */
  z-index: var(--z-editor-layer);
  display: flex;
  flex-direction: column;
  background: #0b0f18;
  outline: none;
  animation: pano-viewer-in var(--dur) var(--ease);
}

@keyframes pano-viewer-in {
  from {
    opacity: 0;
  }
  to {
    opacity: 1;
  }
}

.viewer-top {
  position: absolute;
  top: 16px;
  left: 16px;
  z-index: var(--z-previz-inner);
  display: flex;
  align-items: center;
  gap: 8px;
}

.viewer-badge {
  padding: 2px 8px;
  border-radius: var(--r-sm);
  background: rgba(103, 232, 249, 0.16);
  color: #67e8f9;
  font-size: var(--fs-label);
  font-weight: 600;
  letter-spacing: 0.5px;
}

.viewer-btn {
  border: 1px solid var(--line-control);
  border-radius: var(--r-lg);
  padding: 6px 12px;
  background: rgba(var(--surface-panel-rgb), 0.85);
  color: var(--text-bright);
  font-size: var(--fs-label);
  font-weight: 600;
  cursor: pointer;
}

.viewer-btn:hover {
  background: rgba(var(--surface-raised-rgb), 0.9);
}

.viewer-btn.close {
  position: absolute;
  top: 16px;
  right: 16px;
  z-index: var(--z-previz-inner);
}

.viewer-btn.capture:disabled {
  opacity: 0.5;
  cursor: not-allowed;
}

.viewer-stage {
  position: relative;
  flex: 1;
  min-height: 0;
  touch-action: none;
  cursor: grab;
}

.viewer-stage:active {
  cursor: grabbing;
}

.viewer-stage :deep(canvas) {
  display: block;
  width: 100% !important;
  height: 100% !important;
}

.viewer-state {
  position: absolute;
  inset: 0;
  display: grid;
  place-content: center;
  gap: 6px;
  text-align: center;
  color: var(--text-muted);
  pointer-events: none;
}

.viewer-state span {
  font-weight: 600;
}

.viewer-state small {
  font-size: var(--fs-micro);
}

.viewer-bar {
  display: flex;
  align-items: center;
  gap: 16px;
  padding: 10px 20px;
  background: rgba(var(--surface-panel-rgb), 0.9);
}

.viewer-hint {
  flex: 1;
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  color: var(--text-muted);
  font-size: var(--fs-label);
}

.viewer-ratio {
  display: flex;
  align-items: center;
  gap: 6px;
  color: var(--text-muted);
  font-size: var(--fs-label);
}

.viewer-ratio select {
  background: var(--wash-soft);
  border: 1px solid var(--line-control);
  border-radius: var(--r-md);
  padding: 5px 8px;
  color: inherit;
  font-size: var(--fs-label);
}
</style>
