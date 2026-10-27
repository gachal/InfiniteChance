<script setup lang="ts">
// ネーム板卡(54 号票,絵コンテ板卡 53 号票范式):板名(可编辑,缺省
// 「分镜稿」占位)+ 汇总(N 页 · M 格)+ 页首格缩略条(每页取第 1 格
// 的画面;空格显页序号);整卡点击进全屏编辑层(NameEditorLayer),页/格
// 增删移与画面三入口都在编辑层。手势零拦截:拖拽位移 >5px 不算点击
// (40 号票教训,与絵コンテ/预演台同款阈值过滤)。只渲染右 +
// (name → 图片来源边,storyboard → 图片同族,54 号票)。
import { ref, watch } from 'vue'
import { Handle, Position } from '@vue-flow/core'

import type { NameNodeData } from '../../graph'
import { countPanels } from '../../name'
import { frameContentURL } from '../../storyboard'
import type { ConnectSide, ConnectState } from '../../composables/useConnection'
import { useThumbFallback } from '../../thumbnail'
import NodePorts from './NodePorts.vue'

const props = defineProps<{
  id: string
  type: string
  data: NameNodeData
  /** 连接态展示态(30 号票):origin/valid/dimmed,缺省 = 正常渲染。 */
  connectState?: ConnectState
}>()

const emit = defineEmits<{
  /** 整卡点击进入全屏编辑层。 */
  edit: []
  /** 板名编辑(卡上输入框,逐键上抛,编辑器 updateNodeData 落盘)。 */
  'title-change': [title: string]
  /** 右 + 连下游(name → 图片,落图语义,storyboard 同族)。 */
  'connect-start': [side: ConnectSide]
}>()

// 点卡片进编辑层,拖拽节点不算:按下/抬起位移阈值过滤(StoryboardNode 同款)。
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

// 缩略条加载失败簿(素材被删 = 「素材不可用」占位契约):pages 数组换新
// (编辑层改动/撤销重放)即整簿复位重试。显示地址走两阶段回落(49 号票):
// 缩略失败回原图,原图也失败才落占位。
const thumbs = useThumbFallback()
const brokenIds = ref(new Set<number>())
watch(
  () => props.data.pages,
  () => {
    brokenIds.value = new Set()
  },
)

function markBroken(assetId: number): void {
  brokenIds.value = new Set([...brokenIds.value, assetId])
}

/** 页首格画面:每页取第 1 格的素材引用(0 = 空格,页序号占位);空页无
 * 格同样落页序号。缩略条只给节奏概览,完整页内容进编辑层看。 */
function firstPanelAsset(page: { panels: { asset_id: number }[] }): number {
  return page.panels[0]?.asset_id ?? 0
}

/** 缩略格悬停文案(嵌套三元收进函数,模板里只留一次调用)。 */
function thumbTitle(assetId: number, pageIndex: number, broken: boolean): string {
  if (assetId > 0) {
    return broken ? '素材不可用' : `第 ${pageIndex + 1} 页首格画面`
  }
  return `第 ${pageIndex + 1} 页(首格空)`
}
</script>

<template>
  <div
    class="node name"
    :class="connectState ? `connect-${connectState}` : undefined"
    title="点击进入分镜稿编辑"
    @pointerdown="onPointerDown"
    @mousedown="onPointerDown"
    @click="onCardClick"
  >
    <!-- 板名可编辑(缺省「分镜稿」):输入框点击不触发进层。 -->
    <input
      class="board-title"
      :value="data.title"
      placeholder="分镜稿"
      maxlength="128"
      title="板名"
      aria-label="板名"
      @click.stop
      @input="emit('title-change', ($event.target as HTMLInputElement).value)"
    >

    <!-- 汇总行:页数 + 格子总数(各页求和)。 -->
    <div class="summary">
      {{ data.pages.length }} 页 · {{ countPanels(data.pages) }} 格
    </div>

    <!-- 页首格缩略条:每页一格,空格/空页显页序号(显示时生成 1..N,不持久)。 -->
    <div
      v-if="data.pages.length > 0"
      class="strip"
    >
      <span
        v-for="(p, i) in data.pages"
        :key="i"
        class="thumb"
        :class="{
          missing: firstPanelAsset(p) > 0 && brokenIds.has(firstPanelAsset(p)),
          filled: firstPanelAsset(p) > 0 && !brokenIds.has(firstPanelAsset(p)),
        }"
        :title="thumbTitle(firstPanelAsset(p), i, brokenIds.has(firstPanelAsset(p)))"
      >
        <img
          v-if="firstPanelAsset(p) > 0 && !brokenIds.has(firstPanelAsset(p))"
          :src="thumbs.displayURL(frameContentURL(firstPanelAsset(p)))"
          alt="页首格画面缩略"
          loading="lazy"
          @error="markBroken(firstPanelAsset(p))"
        >
        <span
          v-else-if="firstPanelAsset(p) > 0"
          class="thumb-text bad"
        >不可用</span>
        <span
          v-else
          class="thumb-text"
        >{{ i + 1 }}</span>
      </span>
    </div>
    <div
      v-else
      class="placeholder"
    >
      <span>空板</span>
      <small>点击进入编辑:页可删到空,再按 + 页 续写</small>
    </div>

    <!-- 54 号票:ネーム不作任何连线的目标,只渲染右 + 连下游(落图)。 -->
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
  background: rgba(var(--surface-card-rgb), 0.92);
  border: 1px solid rgba(var(--sky-rgb), 0.4);
  border-radius: var(--r-xl);
  padding: 10px 12px;
  font-size: var(--fs-body);
  cursor: pointer;
}

.board-title {
  width: 100%;
  box-sizing: border-box;
  background: transparent;
  border: none;
  border-bottom: 1px solid transparent;
  border-radius: var(--r-sm);
  padding: 2px 4px;
  color: var(--sky);
  font-size: var(--fs-body);
  font-weight: 600;
}

.board-title::placeholder {
  color: var(--sky);
  opacity: 0.9;
}

.board-title:hover {
  border-bottom-color: rgba(var(--sky-rgb), 0.35);
}

.board-title:focus {
  outline: none;
  background: var(--wash-soft);
  border-bottom-color: rgba(var(--sky-rgb), 0.6);
  color: inherit;
}

.summary {
  margin: 6px 0 8px;
  color: var(--text-muted);
  font-size: var(--fs-label);
}

.strip {
  display: flex;
  gap: 6px;
  overflow-x: auto;
  padding-bottom: 4px;
}

.thumb {
  flex: 0 0 auto;
  display: grid;
  place-items: center;
  width: 56px;
  height: 40px;
  border-radius: var(--r-sm);
  border: 1px dashed var(--line-action);
  background: var(--wash-subtle);
  overflow: hidden;
}

.thumb.filled {
  border-style: solid;
  border-color: var(--line-section);
}

.thumb.missing {
  border-color: rgba(var(--danger-rgb), 0.4);
}

.thumb img {
  width: 100%;
  height: 100%;
  object-fit: cover;
  display: block;
}

.thumb-text {
  font-size: var(--fs-micro);
  color: var(--text-faint);
}

.thumb-text.bad {
  font-size: var(--fs-micro);
  color: var(--danger);
}

:global(.vue-flow__node.selected) .thumb.filled {
  outline: 2px solid rgba(var(--accent-rgb), 0.8);
  outline-offset: 1px;
}

.placeholder {
  display: grid;
  gap: 4px;
  justify-items: center;
  padding: 26px 8px;
  border: 1px dashed var(--line-action);
  border-radius: var(--r-md);
  color: var(--text-muted);
}

.placeholder span {
  font-weight: 600;
}

.placeholder small {
  font-size: var(--fs-micro);
}
</style>
