<script setup lang="ts">
// 剧本卡(64 号票,第十二种节点类型「规划」组):文本载体卡的折叠摘要
// ——标题(空回落「剧本」)+ 摘要行(正文首个非空行,截断)+ 字数;
// 整卡点击进全屏编辑层(ScriptEditorLayer,40 号票浮层范式家族),卡上
// 不开任何直接编辑(一集剧本数千字,卡范式装不下,票面定案)。手势零
// 拦截:拖拽位移 >5px 不算点击(40 号票教训,与絵コンテ同款阈值过滤)。
// 不渲染把手(不作任何连线的源与目标,角色卡/关卡卡先例 —— 内容经
// Agent 工具读写不靠边,66 号票 read_source 分段消费)。
import { computed } from 'vue'

import type { ScriptNodeData } from '../../graph'
import {
  isScriptTextWithinLimit,
  scriptCardTitle,
  scriptCharCountLabel,
  scriptExcessChars,
  scriptSummaryLine,
} from '../../script'

const props = defineProps<{
  id: string
  type: string
  data: ScriptNodeData
}>()

const emit = defineEmits<{
  /** 整卡点击进入全屏编辑层。 */
  edit: []
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

// 折叠摘要三件套全部走 script.ts 纯函数(可单测,组件不持逻辑)。
const title = computed(() => scriptCardTitle(props.data.title))
const summary = computed(() => scriptSummaryLine(props.data.text))
const countLabel = computed(() => scriptCharCountLabel(props.data.text))
const overLimit = computed(() => !isScriptTextWithinLimit(props.data.text))
const excess = computed(() => scriptExcessChars(props.data.text))
</script>

<template>
  <div
    class="node script"
    title="点击进入剧本编辑"
    @pointerdown="onPointerDown"
    @mousedown="onPointerDown"
    @click="onCardClick"
  >
    <div class="card-title">
      {{ title }}
    </div>

    <!-- 摘要行:正文首个非空行;空正文落「空剧本」占位。 -->
    <div
      class="summary"
      :class="{ empty: summary === '' }"
    >
      {{ summary === '' ? '空剧本' : summary }}
    </div>

    <!-- 字数行:达 1 万以「万」计;超 30 万字上限追加分段粘贴提示。 -->
    <div
      class="meta"
      :class="{ over: overLimit }"
    >
      <span>{{ countLabel }}</span>
      <span
        v-if="overLimit"
        class="limit"
      >超出上限 {{ excess }} 字 · 请分段粘贴</span>
    </div>
  </div>
</template>

<style scoped>
.node {
  width: 340px;
  background: rgba(var(--surface-card-rgb), 0.92);
  border: 1px solid rgba(var(--gold-rgb), 0.4);
  border-radius: var(--r-xl);
  padding: 10px 12px;
  font-size: var(--fs-body);
  cursor: pointer;
}

.card-title {
  color: var(--gold);
  font-size: var(--fs-body);
  font-weight: 600;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.summary {
  margin: 6px 0 8px;
  color: var(--text-secondary);
  font-size: var(--fs-label);
  display: -webkit-box;
  -webkit-line-clamp: 2;
  -webkit-box-orient: vertical;
  overflow: hidden;
  word-break: break-all;
}

.summary.empty {
  color: var(--text-faint);
}

.meta {
  display: flex;
  align-items: baseline;
  gap: 8px;
  color: var(--text-muted);
  font-size: var(--fs-micro);
}

/* 超 30 万字上限:字数行整行提为警示色(前端校验的可见出口,编辑层
   有同款横幅,两处共用 script.ts 判据)。 */
.meta.over {
  color: var(--danger);
}

.meta .limit {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
</style>
