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
//     未配置走内置 CSS 缺省形象(呼吸/眨眼/左看右看,零素材依赖);配置了互动
//     动图时点击瞬间短暂播放(播放时机 = 票面实现期裁量,接缝即此);
//     拖起/按下的微动效走 CSS(放大/阴影),不引入 Lottie;
//   - 常动守卫(DESIGN.md 动效协议,照 AuthLayout 86 号票范式):缺省
//     形象的循环动画只在 prefers-reduced-motion: no-preference 下开启
//     (reduce 静止首帧);暂停盘按 2026-11-19 用户裁决移除(DESIGN.md
//     「常动件必配暂停」已加桌宠豁免注记)。
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
  // 先量边界再回读:bounds 还是零值时,缺省位会兜回左上、已存位置会被
  // 零边界夹成原点(旧版桌宠恒落左上、位置记忆从不生效的根因)。
  measure()
  position.value = loadPetPosition(props.canvasId, bounds.value)
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

/* ---- CSS 缺省形象(即梦式绿毛球):呼吸 + 眨眼 + 左看右看,零素材依赖。
   常动守卫(DESIGN.md 动效协议,AuthLayout 86 号票范式):循环动画只在
   prefers-reduced-motion: no-preference 下声明(reduce 静止显示首帧)。 ---- */
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
}

.pet-eye.left {
  left: 28%;
}

.pet-eye.right {
  right: 28%;
}

@media (prefers-reduced-motion: no-preference) {
  /* 同元素多动画逗号叠加:breathe/blink 动 transform,tilt/look 动
     rotate/translate 独立属性 —— 同属性多动画后者整体覆盖前者,错开
     属性才真能叠加(93 号票);不用 animation-delay(简写会重置
     delay,86 号票实证),look/tilt 同周期天然同相。 */
  .pet-default {
    animation:
      pet-breathe 2.8s var(--ease) infinite,
      pet-tilt 6s ease-in-out infinite;
  }

  .pet-eye {
    animation:
      pet-blink 4.4s step-end infinite,
      pet-look 6s ease-in-out infinite;
  }
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

/* 左看右看(93 号票):节奏「中 → 左停 → 回中 → 右停 → 回中」。
   look 平移量 80% 取眼自身宽(眼宽 = 脸宽 10%)→ 实际平移脸宽 8%
   (72px 形象上约 5.8px,票面裁量区间 6–12% 取中);tilt 与 look
   同周期 6s 同相,身体 ±2° 倾斜给张望感。段间各走 ease-in-out,
   平台段(14–42% / 64–92%)两侧关键帧同值 = 停留。 */
@keyframes pet-look {
  0%,
  50%,
  100% {
    translate: 0 0;
  }
  14%,
  42% {
    translate: -80% 0;
  }
  64%,
  92% {
    translate: 80% 0;
  }
}

@keyframes pet-tilt {
  0%,
  50%,
  100% {
    rotate: 0deg;
  }
  14%,
  42% {
    rotate: -2deg;
  }
  64%,
  92% {
    rotate: 2deg;
  }
}
</style>
