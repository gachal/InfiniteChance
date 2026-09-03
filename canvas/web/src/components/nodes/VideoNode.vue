<script setup lang="ts">
// 视频节点:生成产物落位之前显示占位;url 出现后内嵌播放。
import { Handle, Position } from '@vue-flow/core'

import type { MediaNodeData } from '../../graph'

defineProps<{
  id: string
  type: string
  data: MediaNodeData
}>()
</script>

<template>
  <div class="node media video">
    <header>视频</header>
    <video
      v-if="data.url"
      :src="data.url"
      controls
      muted
    />
    <div
      v-else
      class="placeholder"
    >
      <span>视频占位</span>
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
  width: 220px;
  background: rgba(20, 26, 43, 0.92);
  border: 1px solid rgba(250, 204, 21, 0.4);
  border-radius: 12px;
  padding: 10px 12px;
  font-size: 13px;
}

.node header {
  font-weight: 600;
  color: #facc15;
  margin-bottom: 8px;
}

video {
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
