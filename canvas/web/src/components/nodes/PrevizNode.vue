<script setup lang="ts">
// 预演台节点卡片(39 号票定节点;40 号票收拢为帧卡):卡片不再跑
// three.js —— 只展示最新渲染帧(uploads/ 档素材引用),整卡点击进入
// 全屏编辑层(PrevizEditorLayer)排练;「落画布」把当前帧复制为图片
// 节点并连线,喂生成链一律经图片节点,对话框不认本节点。手势零拦截:
// 滚轮/拖拽全部还给画布(40 号票:视口吞手势的冲突根源随实时视口移除)。
import { ref, watch } from 'vue'
import { Handle, Position } from '@vue-flow/core'

import type { PrevizNodeData } from '../../graph'
import type { ConnectSide, ConnectState } from '../../composables/useConnection'
import NodePorts from './NodePorts.vue'

const props = defineProps<{
  id: string
  type: string
  data: PrevizNodeData
  /** 连接态展示态(30 号票):origin/valid/dimmed,缺省 = 正常渲染。 */
  connectState?: ConnectState
}>()

const emit = defineEmits<{
  /** 整卡点击进入全屏编辑层(40 号票)。 */
  edit: []
  /** 落画布:把最新渲染帧落为新图片节点并连线(编辑器处理)。 */
  drop: []
  /** 右 + 连下游(39 号票:预演台不作任何连线的目标,无左 +)。 */
  'connect-start': [side: ConnectSide]
}>()

// 点卡片进编辑层,但拖拽节点不算 —— 浏览器 click 不看位移,按住卡片把
// 节点拖走再松手也会触发 click,这里按按下/抬起的位移阈值手动过滤;
// pointerdown 与 mousedown 都记(pointer 事件缺失的合成/自动化路径由
// mousedown 兜底),click 取最近一次按下位置算位移。
let pointerDown: { x: number; y: number } | null = null

function onPointerDown(event: { clientX: number; clientY: number }): void {
  pointerDown = { x: event.clientX, y: event.clientY }
}

function onCardClick(event: MouseEvent): void {
  const down = pointerDown
  pointerDown = null
  if (!down || Math.hypot(event.clientX - down.x, event.clientY - down.y) > 5) {
    return
  }
  emit('edit')
}

// ---- 渲染帧加载失败占位(素材被删等同款纪律,14 号票)----

const latestFailed = ref(false)
watch(
  () => props.data.latest?.url,
  () => {
    latestFailed.value = false
  },
)
</script>

<template>
  <div
    class="node previz"
    :class="connectState ? `connect-${connectState}` : undefined"
    title="点击进入排练"
    @pointerdown="onPointerDown"
    @mousedown="onPointerDown"
    @click="onCardClick"
  >
    <header>预演台</header>

    <!-- 最新渲染帧(uploads/ 档素材引用):整卡点击进编辑层,悬浮下载。 -->
    <div
      v-if="data.latest && !latestFailed"
      class="media-frame"
    >
      <img
        :src="data.latest.url"
        alt="预演台最新渲染帧"
        @error="latestFailed = true"
      >
      <a
        class="download"
        :href="data.latest.url"
        download
        title="下载渲染帧"
        aria-label="下载渲染帧"
        @click.stop
      >⬇</a>
    </div>
    <div
      v-else-if="data.latest && latestFailed"
      class="placeholder missing"
    >
      <span>素材不可用</span>
      <small>原素材可能已被删除,点击仍可进入排练</small>
    </div>
    <div
      v-else
      class="placeholder"
    >
      <span>空场景待排练</span>
      <small>点击进入排练:摆台、机位、渲染</small>
    </div>

    <button
      class="drop"
      type="button"
      :disabled="!data.latest?.url || latestFailed"
      title="把最新渲染帧落为新图片节点并连线,可喂生成对话框参考条/首帧"
      @click.stop="emit('drop')"
    >
      落画布
    </button>

    <!-- 39 号票:预演台不作任何连线的目标,只渲染右 + 连下游。 -->
    <NodePorts
      :sides="['right']"
      @start="emit('connect-start', $event)"
    />

    <Handle
      type="source"
      :position="Position.Right"
    />
  </div>
</template>

<style scoped>
.node {
  width: 340px;
  background: rgba(20, 26, 43, 0.92);
  border: 1px solid rgba(244, 114, 182, 0.4);
  border-radius: 12px;
  padding: 10px 12px;
  font-size: 13px;
  cursor: pointer;
}

.node header {
  font-weight: 600;
  color: #f472b6;
  margin-bottom: 8px;
}

:global(.vue-flow__node.selected) .media-frame {
  outline: 2px solid rgba(122, 162, 247, 0.8);
  outline-offset: 2px;
}

.media-frame {
  position: relative;
  cursor: pointer;
}

.media-frame img {
  display: block;
  width: 100%;
  border-radius: 10px;
}

.media-frame .download {
  position: absolute;
  top: 8px;
  right: 8px;
  width: 28px;
  height: 28px;
  display: grid;
  place-items: center;
  border: none;
  border-radius: 8px;
  background: rgba(13, 18, 32, 0.85);
  color: #dfe3ee;
  font-size: 14px;
  text-decoration: none;
  opacity: 0;
  transition: opacity 0.15s ease;
}

.media-frame:hover .download,
.media-frame .download:focus-visible {
  opacity: 1;
}

@media (hover: none) {
  .media-frame .download {
    opacity: 1;
  }
}

.placeholder {
  display: grid;
  gap: 4px;
  justify-items: center;
  padding: 34px 8px;
  border: 1px dashed rgba(255, 255, 255, 0.22);
  border-radius: 8px;
  color: #8b91a7;
}

.placeholder span {
  font-weight: 600;
}

.placeholder small {
  font-size: 11px;
}

.placeholder.missing {
  border-color: rgba(255, 143, 143, 0.4);
  color: #ff8f8f;
}

.drop {
  width: 100%;
  margin-top: 8px;
  border: 1px solid rgba(122, 162, 247, 0.55);
  border-radius: 8px;
  padding: 7px 0;
  font-size: 12px;
  font-weight: 600;
  cursor: pointer;
  background: rgba(122, 162, 247, 0.12);
  color: #7aa2f7;
}

.drop:disabled {
  opacity: 0.45;
  cursor: not-allowed;
}
</style>
