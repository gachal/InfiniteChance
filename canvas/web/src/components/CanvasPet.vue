<script setup lang="ts">
// 画布桌宠(92 号票):画布助手(91 号票)在画布上的化身壳 —— 纯 UI 形态
// 实体,不承载会话/工具语义。行为:
//   - 按下位移阈值区分点击与拖动(petGestureExceedsDrag,6px;阈值钉在
//     canvasPet.ts,拖动手感留浏览器点验);
//   - 单击 = 唤出助手面板(activate 上抛,视图同步右缘互斥组);拖动仅
//     改位置不触发点击;
//   - 位置与收起态按画布存 localStorage(canvasPet.ts;本组件只管位置,
//     收起由视图 v-if 控制 —— 收起 = 不渲染,非仅隐藏);
//   - 右键无菜单;与 vue-flow 无交互(不进节点层,z 档 --z-pet);
//   - 形象:配置了待机动图走 <img> 循环播放(GIF/WebP/APNG 自带动帧),
//     未配置走内置 CSS 缺省形象(呼吸/眨眼,零素材依赖);配置了互动
//     动图时点击瞬间短暂播放(播放时机 = 票面实现期裁量,接缝即此);
//     拖起/按下的微动效走 CSS(放大/阴影),不引入 Lottie。
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'

import type { PetConfigInfo } from '@infinitechance/api'

import {
  clampPetPosition,
  loadPetPosition,
  petGestureExceedsDrag,
  petObjectURL,
  PET_SIZE,
  savePetPosition,
  type PetBounds,
  type PetPoint,
} from '../canvasPet'

const props = defineProps<{
  canvasId: number
  /** pet 配置(canvas GET /pet/config;null = 未加载/未配置 → CSS 缺省形象)。 */
  config: PetConfigInfo | null
}>()

const emit = defineEmits<{
  /** 单击(位移未超阈值)唤出助手面板。 */
  activate: []
}>()

const el = ref<HTMLElement | null>(null)
const bounds = ref<PetBounds>({ width: 0, height: 0 })
const position = ref<PetPoint>({ x: 0, y: 0 })
/** 手势三态:idle 未按下;pressed 按下未超阈值;dragging 已超阈值在拖。 */
const gesture = ref<'idle' | 'pressed' | 'dragging'>('idle')

const idleSrc = computed(() => (props.config?.idle_image ? petObjectURL(props.config.idle_image) : ''))
const interactiveSrc = computed(() => (props.config?.interactive_image ? petObjectURL(props.config.interactive_image) : ''))
const petName = computed(() => (props.config?.name ?? '').trim() || '桌宠')

// ---- 互动动图:点击瞬间短播一帧窗口(到位图的 MVP 形态;待机图恒是
// 底座,超时回待机)。 ----
const INTERACTIVE_BURST_MS = 1600
const playingInteractive = ref(false)
let interactiveTimer: number | null = null

function playInteractiveBurst(): void {
  if (!interactiveSrc.value) {
    return
  }
  playingInteractive.value = true
  if (interactiveTimer !== null) {
    clearTimeout(interactiveTimer)
  }
  interactiveTimer = window.setTimeout(() => {
    playingInteractive.value = false
    interactiveTimer = null
  }, INTERACTIVE_BURST_MS)
}

// ---- 指针手势(pointer capture:按住拖出桌宠框也持续收到 move/up)----

let pressX = 0
let pressY = 0
let origin: PetPoint = { x: 0, y: 0 }

function onPointerDown(e: PointerEvent): void {
  if (e.button !== 0) {
    return // 右键无菜单、中键不响应(仅左键触发点击/拖动)
  }
  gesture.value = 'pressed'
  pressX = e.clientX
  pressY = e.clientY
  origin = { ...position.value }
  el.value?.setPointerCapture(e.pointerId)
}

function onPointerMove(e: PointerEvent): void {
  if (gesture.value === 'idle') {
    return
  }
  const dx = e.clientX - pressX
  const dy = e.clientY - pressY
  if (gesture.value === 'pressed' && petGestureExceedsDrag(dx, dy)) {
    gesture.value = 'dragging'
  }
  if (gesture.value === 'dragging') {
    // 拖动仅改位置(不触发点击);位置夹在画布区内。
    position.value = clampPetPosition({ x: origin.x + dx, y: origin.y + dy }, bounds.value)
  }
}

function onPointerUp(e: PointerEvent): void {
  if (gesture.value === 'idle') {
    return
  }
  const wasDragging = gesture.value === 'dragging'
  gesture.value = 'idle'
  if (el.value?.hasPointerCapture?.(e.pointerId)) {
    el.value.releasePointerCapture(e.pointerId)
  }
  if (wasDragging) {
    savePetPosition(props.canvasId, position.value)
    return
  }
  playInteractiveBurst()
  emit('activate')
}

/** 右键无菜单(票面定案)。 */
function onContextMenu(e: MouseEvent): void {
  e.preventDefault()
}

// ---- 画布区尺寸:贴着 .canvas-wrap(position:relative 宿主)量,窗口
// 缩放/面板开合改变可用区域时把桌宠夹回区内。 ----
let observer: ResizeObserver | null = null

function measure(): void {
  const host = el.value?.parentElement
  if (!host) {
    return
  }
  const rect = host.getBoundingClientRect()
  if (rect.width <= 0 || rect.height <= 0) {
    return
  }
  bounds.value = { width: rect.width, height: rect.height }
  position.value = clampPetPosition(position.value, bounds.value)
}

onMounted(() => {
  position.value = loadPetPosition(props.canvasId, bounds.value)
  measure()
  const host = el.value?.parentElement
  if (host) {
    observer = new ResizeObserver(() => measure())
    observer.observe(host)
  }
})

onBeforeUnmount(() => {
  observer?.disconnect()
  if (interactiveTimer !== null) {
    clearTimeout(interactiveTimer)
  }
})

// 画布切换(组件复用场景):位置按新画布重读。
watch(
  () => props.canvasId,
  () => {
    position.value = loadPetPosition(props.canvasId, bounds.value)
  },
)
</script>

<template>
  <div
    ref="el"
    class="canvas-pet"
    :class="{ 'is-pressed': gesture === 'pressed', 'is-dragging': gesture === 'dragging' }"
    :style="{ left: `${position.x}px`, top: `${position.y}px`, width: `${PET_SIZE}px`, height: `${PET_SIZE}px` }"
    :title="`${petName} · 点击唤出画布助手,按住拖动`"
    data-testid="canvas-pet"
    @pointerdown="onPointerDown"
    @pointermove="onPointerMove"
    @pointerup="onPointerUp"
    @pointercancel="onPointerUp"
    @contextmenu="onContextMenu"
  >
    <!-- 互动动图(点击瞬间短播)/ 待机动图(循环播放)/ CSS 缺省形象 -->
    <img
      v-if="playingInteractive && interactiveSrc"
      class="pet-image"
      :src="interactiveSrc"
      alt=""
      draggable="false"
    >
    <img
      v-else-if="idleSrc"
      class="pet-image"
      :src="idleSrc"
      alt=""
      draggable="false"
    >
    <div
      v-else
      class="pet-default"
      aria-hidden="true"
    >
      <span class="pet-eye left" />
      <span class="pet-eye right" />
      <span class="pet-cheek left" />
      <span class="pet-cheek right" />
      <span class="pet-mouth" />
    </div>
  </div>
</template>

<style scoped>
/* 画布区自由浮层(z 档 --z-pet,style.css 档位表 92 号票新档);不进
   vue-flow 节点层。touch-action:none 让指针事件不被浏览器手势接管。 */
.canvas-pet {
  position: absolute;
  z-index: var(--z-pet);
  cursor: grab;
  user-select: none;
  touch-action: none;
  transition:
    transform var(--dur-fast) var(--ease),
    filter var(--dur-fast) var(--ease);
}

.canvas-pet.is-pressed {
  cursor: grabbing;
  transform: scale(0.94);
}

.canvas-pet.is-dragging {
  cursor: grabbing;
  transform: scale(1.08) rotate(-3deg);
  filter: drop-shadow(0 10px 20px rgba(0, 0, 0, 0.3));
}

.pet-image {
  width: 100%;
  height: 100%;
  object-fit: contain;
  pointer-events: none;
}

/* ---- CSS 缺省形象(即梦式绿毛球):呼吸 + 眨眼,零素材依赖 ---- */
.pet-default {
  position: relative;
  width: 100%;
  height: 100%;
  border-radius: 52% 48% 50% 50% / 54% 56% 44% 46%;
  background: radial-gradient(circle at 34% 28%, #9ad99a 0%, #4db06e 48%, #2f8f58 100%);
  box-shadow:
    inset -6px -9px 14px rgba(0, 0, 0, 0.14),
    inset 4px 6px 10px rgba(255, 255, 255, 0.28),
    0 6px 16px rgba(0, 0, 0, 0.18);
  animation: pet-breathe 2.8s var(--ease) infinite;
  pointer-events: none;
}

.pet-eye {
  position: absolute;
  top: 40%;
  width: 10%;
  height: 16%;
  border-radius: 50%;
  background: #1d3324;
  transform-origin: center;
  animation: pet-blink 4.4s step-end infinite;
}

.pet-eye.left {
  left: 28%;
}

.pet-eye.right {
  right: 28%;
}

/* 腮红与嘴:形象的性格层,纯 CSS。 */
.pet-cheek {
  position: absolute;
  top: 55%;
  width: 12%;
  height: 8%;
  border-radius: 50%;
  background: rgba(255, 140, 140, 0.55);
  filter: blur(1px);
}

.pet-cheek.left {
  left: 18%;
}

.pet-cheek.right {
  right: 18%;
}

.pet-mouth {
  position: absolute;
  top: 58%;
  left: 44%;
  width: 12%;
  height: 6%;
  border-bottom: 2px solid #1d3324;
  border-radius: 0 0 50% 50%;
}

@keyframes pet-breathe {
  0%,
  100% {
    transform: scale(1);
  }
  50% {
    transform: scale(1.05) translateY(-2px);
  }
}

/* 眨眼:大部分时间睁着,偶发压扁(step-end 让闭合帧干脆)。 */
@keyframes pet-blink {
  0%,
  91%,
  95%,
  100% {
    transform: scaleY(1);
  }
  92%,
  94% {
    transform: scaleY(0.08);
  }
}
</style>
