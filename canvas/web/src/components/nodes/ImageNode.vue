<script setup lang="ts">
// 图片节点:生成产物落位之前显示占位;url 出现后直接呈现图片。
import { Handle, Position } from '@vue-flow/core'

import type { MediaNodeData } from '../../graph'

defineProps<{
  id: string
  type: string
  data: MediaNodeData
}>()
</script>

<template>
  <div class="node media image">
    <header>图片</header>
    <img
      v-if="data.url"
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
</style>
