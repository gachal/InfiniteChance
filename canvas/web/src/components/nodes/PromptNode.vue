<script setup lang="ts">
// 提示词节点:自由文本输入,内容随整图 JSON 自动保存。
// 不直接改 props:文本变更上抛给编辑器,由 updateNodeData 应用。
import { Handle, Position } from '@vue-flow/core'

import type { PromptNodeData } from '../../graph'

defineProps<{
  id: string
  type: string
  data: PromptNodeData
}>()

const emit = defineEmits<{ 'text-change': [value: string] }>()
</script>

<template>
  <div class="node prompt">
    <header>提示词</header>
    <textarea
      :value="data.text"
      placeholder="写下提示词…"
      rows="4"
      @input="emit('text-change', ($event.target as HTMLTextAreaElement).value)"
    />
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
  width: 220px;
  background: rgba(20, 26, 43, 0.92);
  border: 1px solid rgba(122, 162, 247, 0.45);
  border-radius: 12px;
  padding: 10px 12px;
  font-size: 13px;
}

.node header {
  font-weight: 600;
  color: #7aa2f7;
  margin-bottom: 8px;
}

textarea {
  width: 100%;
  resize: vertical;
  background: rgba(255, 255, 255, 0.05);
  border: 1px solid rgba(255, 255, 255, 0.12);
  border-radius: 8px;
  padding: 8px;
  color: inherit;
  font: inherit;
  line-height: 1.5;
}

textarea:focus {
  outline: 2px solid rgba(122, 162, 247, 0.6);
  outline-offset: 1px;
  border-color: transparent;
}
</style>
