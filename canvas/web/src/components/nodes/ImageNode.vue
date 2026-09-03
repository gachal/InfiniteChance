<script setup lang="ts">
// 图片节点:文生图任务的展示面(10 号票)。任务在途时显示排队/生成中,
// 失败显示原因与原地重试,产物落位后直接呈现图片。
import { computed } from 'vue'
import { Handle, Position } from '@vue-flow/core'

import type { CanvasTask } from '@infinitechance/api'

import type { MediaNodeData } from '../../graph'

const props = defineProps<{
  id: string
  type: string
  data: MediaNodeData
  /** 绑定到本节点的生成任务(编辑器轮询保持最新;null = 无任务)。 */
  task?: CanvasTask | null
  /** 重试请求在途时禁用重试按钮。 */
  retrying: boolean
}>()

const emit = defineEmits<{ retry: [] }>()

const status = computed(() => props.task?.status)
</script>

<template>
  <div
    class="node media image"
    :data-task="status ?? 'none'"
  >
    <header>图片</header>
    <div
      v-if="status === 'queued' || status === 'running'"
      class="placeholder working"
    >
      <span class="pill">{{ status === 'queued' ? '排队中…' : '生成中…' }}</span>
      <small>任务在服务端进行,关闭页面不丢失</small>
    </div>
    <div
      v-else-if="status === 'failed'"
      class="failed"
      role="alert"
    >
      <p class="error">
        {{ task?.error || '生成失败' }}
      </p>
      <button
        class="retry"
        type="button"
        :disabled="retrying"
        @click="emit('retry')"
      >
        {{ retrying ? '重试中…' : '重试' }}
      </button>
    </div>
    <img
      v-else-if="data.url"
      :src="data.url"
      alt="图片节点产物"
    >
    <div
      v-else
      class="placeholder"
    >
      <span>图片占位</span>
      <small>生成结果将出现在这里</small>
    </div>
    <Handle
      type="source"
      :position="Position.Right"
    />
    <Handle
      type="target"
      :position="Position.Left"
    />
  </div>
</template>

<style scoped>
.node {
  width: 200px;
  background: rgba(20, 26, 43, 0.92);
  border: 1px solid rgba(74, 222, 128, 0.4);
  border-radius: 12px;
  padding: 10px 12px;
  font-size: 13px;
}

.node header {
  font-weight: 600;
  color: #4ade80;
  margin-bottom: 8px;
}

.node[data-task='queued'] {
  border-style: dashed;
}

.node[data-task='running'] {
  border-color: rgba(74, 222, 128, 0.85);
}

.node[data-task='failed'] {
  border-color: rgba(255, 143, 143, 0.6);
}

img {
  display: block;
  width: 100%;
  border-radius: 8px;
}

.placeholder {
  display: grid;
  gap: 4px;
  justify-items: center;
  padding: 26px 8px;
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

.working .pill {
  color: #4ade80;
}

.node[data-task='running'] .pill {
  animation: pulse 1.4s ease-in-out infinite;
}

@keyframes pulse {
  0%,
  100% {
    opacity: 1;
  }
  50% {
    opacity: 0.45;
  }
}

.failed {
  display: grid;
  gap: 8px;
  justify-items: start;
  padding: 10px;
  border: 1px solid rgba(255, 143, 143, 0.35);
  border-radius: 8px;
}

.failed .error {
  margin: 0;
  font-size: 12px;
  color: #ff8f8f;
  word-break: break-all;
}

.retry {
  border: none;
  border-radius: 8px;
  padding: 6px 14px;
  font-size: 12px;
  font-weight: 600;
  cursor: pointer;
  background: rgba(255, 255, 255, 0.1);
  color: inherit;
}

.retry:disabled {
  opacity: 0.5;
  cursor: not-allowed;
}
</style>
