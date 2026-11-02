<script setup lang="ts">
// 分组节点卡(72 号票,第十三种节点类型):组织装饰非内容节点 —— 半透明
// 填充 + 描边 + 标题栏。不渲染把手/NodePorts(不作任何连线端点);标题栏
// 常驻左上,可发起整组拖动(vue-flow 原生节点拖拽)也承担双击改名 ——
// 两者共存由缺省 nodeDragThreshold: 1 保证(原位点击/双击位移不足阈值,
// d3-drag 不触发)。标题栏因此不挂 nodrag(挂了会被 noDragClassName 过滤,
// 标题栏拖不动,72 号票评审 C5);真正交互的子元素(改名输入、色板、解组
// 钮、resize 手柄)保留 nodrag。选中时标题栏右侧浮出色板 + 解组;四角
// resize 手柄只改用户设定尺寸(成员不动)。层级压底与选中提升的免疫在
// 全局 style.css(.vue-flow__node-group)。
import { computed, ref } from 'vue'

import type { GroupNodeData } from '../../graph'
import {
  GROUP_DEFAULT_TITLE,
  GROUP_PALETTE,
  GROUP_RESIZE_CORNERS,
  groupColorOrDefault,
  type GroupColorToken,
  type GroupResizeCorner,
} from '../../canvasGroup'

const props = defineProps<{
  id: string
  data: GroupNodeData
  /** 当前渲染尺寸(编辑器按 max(用户尺寸, 成员包围盒+内边距) 算好传入;
   * 组件不持尺寸逻辑)。 */
  width: number
  height: number
  /** 选中态(vue-flow NodeProps.selected 透传):浮出操作钮与选中描边。 */
  selected?: boolean
  /** 拖动中「松手将加入」的实时高亮(编辑器按拖动节点中心判定)。 */
  willJoin?: boolean
}>()

const emit = defineEmits<{
  titleChange: [title: string]
  colorChange: [color: GroupColorToken]
  /** resize 手柄交互:mousedown 起、mousemove 报位移、mouseup 收尾;
   * 尺寸/位置换算在编辑器侧(需要节点 position)。 */
  resizeStart: [corner: GroupResizeCorner]
  resizeMove: [dx: number, dy: number]
  resizeEnd: []
  ungroup: []
}>()

const title = computed(() => (props.data.title?.trim() || GROUP_DEFAULT_TITLE))
const color = computed(() => groupColorOrDefault(props.data.color))

// ---- 双击内联改名(票 E3)----

const renaming = ref(false)
const renameDraft = ref('')

function startRename(): void {
  renameDraft.value = title.value
  renaming.value = true
}

function commitRename(): void {
  if (!renaming.value) {
    return
  }
  renaming.value = false
  const next = renameDraft.value.trim()
  if (next && next !== title.value) {
    emit('titleChange', next)
  }
}

function cancelRename(): void {
  renaming.value = false
}

// ---- 四角 resize(票 D4)----

interface ResizeGesture {
  startX: number
  startY: number
}

let gesture: ResizeGesture | null = null

function onResizeMove(event: MouseEvent): void {
  if (!gesture) {
    return
  }
  emit('resizeMove', event.clientX - gesture.startX, event.clientY - gesture.startY)
}

function onResizeUp(): void {
  gesture = null
  window.removeEventListener('mousemove', onResizeMove)
  window.removeEventListener('mouseup', onResizeUp)
  emit('resizeEnd')
}

function startResize(corner: GroupResizeCorner, event: MouseEvent): void {
  gesture = { startX: event.clientX, startY: event.clientY }
  emit('resizeStart', corner)
  window.addEventListener('mousemove', onResizeMove)
  window.addEventListener('mouseup', onResizeUp)
}
</script>

<template>
  <div
    class="group-frame"
    :class="[`tone-${color}`, { 'will-join': willJoin }]"
    :style="{ width: `${width}px`, height: `${height}px` }"
  >
    <!-- 标题栏不挂 nodrag:拖它 = 拖组(票 C5);双击改名与拖动共存由
         nodeDragThreshold: 1 保证(原位双击不触发拖拽)。 -->
    <div class="group-titlebar">
      <template v-if="!renaming">
        <span
          class="group-title"
          :title="`${title}(双击改名)`"
          @dblclick.stop.prevent="startRename"
        >{{ title }}</span>
        <!-- 选中浮出操作钮(票 E3):色板 + 解组;改名中收起,别和输入框
             打架。nodrag + mousedown.stop 双保险:交互钮不发起组拖动。 -->
        <span
          v-if="selected"
          class="group-actions"
        >
          <button
            v-for="c in GROUP_PALETTE"
            :key="c"
            class="group-swatch nodrag"
            :class="[`tone-${c}`, { current: c === color }]"
            type="button"
            :title="`组色:${c}`"
            :aria-label="`组色:${c}`"
            @mousedown.stop
            @click.stop="emit('colorChange', c)"
          />
          <button
            class="group-ungroup nodrag"
            type="button"
            title="解组:删除组框,成员原地保留"
            @mousedown.stop
            @click.stop="emit('ungroup')"
          >
            解组
          </button>
        </span>
      </template>
      <input
        v-else
        v-model="renameDraft"
        class="group-rename nodrag"
        type="text"
        aria-label="分组名称"
        @mousedown.stop
        @click.stop
        @dblclick.stop
        @keydown.enter.stop.prevent="commitRename"
        @keydown.esc.stop.prevent="cancelRename"
        @blur="commitRename"
      >
    </div>
    <span
      v-for="c in GROUP_RESIZE_CORNERS"
      :key="c"
      class="group-resize nodrag"
      :class="`group-resize-${c}`"
      @mousedown.stop.prevent="startResize(c, $event)"
    />
  </div>
</template>

<style scoped>
/* 色板 token 名 → rgb 三元组(与 style.css 的 --*-rgb 同源;半透明填充
   与描边全部走 rgba(var(--tone-rgb), α) 一个变量)。 */
.tone-sky {
  --tone-rgb: var(--sky-rgb);
}

.tone-teal {
  --tone-rgb: var(--teal-rgb);
}

.tone-violet {
  --tone-rgb: var(--violet-rgb);
}

.tone-pink {
  --tone-rgb: var(--pink-rgb);
}

.tone-orange {
  --tone-rgb: var(--orange-rgb);
}

.tone-gold {
  --tone-rgb: var(--gold-rgb);
}

.tone-success {
  --tone-rgb: var(--success-rgb);
}

.tone-red-signal {
  --tone-rgb: var(--red-signal-rgb);
}

.group-frame {
  position: relative;
  width: 100%;
  height: 100%;
  border: 1px solid rgba(var(--tone-rgb), 0.45);
  border-radius: var(--r-xl);
  background: rgba(var(--tone-rgb), 0.1);
  /* 框体即拖拽面:点空白处拖动 = 整组移动(vue-flow 原生节点拖拽)。 */
  cursor: move;
  transition:
    background var(--dur-fast) var(--ease),
    border-color var(--dur-fast) var(--ease),
    box-shadow var(--dur-fast) var(--ease);
}

/* 拖动中「松手将加入」实时高亮(票 C2):描边提亮 + 底纹加深。 */
.group-frame.will-join {
  border-color: rgba(var(--tone-rgb), 0.95);
  background: rgba(var(--tone-rgb), 0.18);
  box-shadow: 0 0 0 2px rgba(var(--tone-rgb), 0.35);
}

/* 选中描边(与各节点卡同款 :global 选择器纪律)。 */
:global(.vue-flow__node.selected) .group-frame {
  border-color: rgba(var(--tone-rgb), 0.9);
  box-shadow: 0 0 0 2px rgba(var(--tone-rgb), 0.3);
}

/* 标题栏:常驻左上小牌。不挂 nodrag —— 拖它 = 拖组(vue-flow 原生节点
   拖拽,nodeDragThreshold: 1 让原位双击改名与拖动共存)。 */
.group-titlebar {
  position: absolute;
  top: -1px;
  left: -1px;
  display: inline-flex;
  align-items: center;
  gap: 6px;
  max-width: calc(100% + 2px);
  padding: 3px 8px;
  border: 1px solid rgba(var(--tone-rgb), 0.45);
  border-radius: var(--r-xl) var(--r-sm) var(--r-sm) var(--r-xl);
  background: rgba(var(--surface-card-rgb), 0.92);
  cursor: move;
}

.group-title {
  color: rgba(var(--tone-rgb), 1);
  font-size: var(--fs-label);
  font-weight: 600;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
  max-width: 160px;
}

.group-rename {
  width: 140px;
  padding: 1px 4px;
  border: 1px solid rgba(var(--tone-rgb), 0.6);
  border-radius: var(--r-sm);
  background: rgba(var(--surface-raised-rgb), 0.95);
  color: var(--text-primary);
  font-size: var(--fs-label);
  cursor: text;
}

.group-rename:focus {
  outline: none;
  border-color: rgba(var(--accent-rgb), 0.85);
}

/* 操作钮随选中浮出:色板圆点 + 解组。 */
.group-actions {
  display: inline-flex;
  align-items: center;
  gap: 4px;
}

.group-swatch {
  width: 14px;
  height: 14px;
  padding: 0;
  border: 1px solid rgba(255, 255, 255, 0.35);
  border-radius: var(--r-pill);
  background: rgba(var(--tone-rgb), 0.5);
  cursor: pointer;
}

.group-swatch:hover {
  border-color: rgba(255, 255, 255, 0.8);
}

.group-swatch.current {
  box-shadow: 0 0 0 2px rgba(var(--tone-rgb), 0.45);
}

.group-ungroup {
  padding: 1px 8px;
  border: 1px solid var(--line-control);
  border-radius: var(--r-sm);
  background: rgba(var(--surface-raised-rgb), 0.95);
  color: var(--text-secondary);
  font-size: var(--fs-label);
  cursor: pointer;
}

.group-ungroup:hover {
  border-color: rgba(var(--danger-rgb), 0.7);
  color: var(--danger);
}

/* 四角 resize 手柄(nodrag 类防触发节点拖拽,mousedown.stop 同款纪律)。 */
.group-resize {
  position: absolute;
  width: 10px;
  height: 10px;
  border: 1px solid rgba(var(--tone-rgb), 0.7);
  border-radius: 2px;
  background: rgba(var(--surface-card-rgb), 0.92);
  opacity: 0;
  transition: opacity var(--dur-fast) var(--ease);
}

.group-frame:hover .group-resize,
:global(.vue-flow__node.selected) .group-resize {
  opacity: 1;
}

.group-resize-nw {
  top: -5px;
  left: -5px;
  cursor: nwse-resize;
}

.group-resize-ne {
  top: -5px;
  right: -5px;
  cursor: nesw-resize;
}

.group-resize-sw {
  bottom: -5px;
  left: -5px;
  cursor: nesw-resize;
}

.group-resize-se {
  bottom: -5px;
  right: -5px;
  cursor: nwse-resize;
}
</style>
