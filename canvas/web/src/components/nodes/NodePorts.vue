<script setup lang="ts">
// 节点左右常驻 + 按钮(30 号票):两击连线的唯一入口。左 + = 接上游
// (本节点为 target),右 + = 连下游(本节点为 source)。mousedown.stop
// 防止按钮点击触发节点拖拽;视觉样式在全局 style.css(.port),四个节点
// 组件共用。
import type { ConnectSide } from '../../composables/useConnection'

defineProps<{
  /** 渲染哪些按钮:分析节点不作任何连线的源,只传 ['left']。 */
  sides: ConnectSide[]
}>()

const emit = defineEmits<{
  start: [side: ConnectSide]
}>()
</script>

<template>
  <button
    v-for="side in sides"
    :key="side"
    class="port"
    :class="`port-${side}`"
    type="button"
    :title="side === 'left' ? '接上游:点击后再点来源节点' : '连下游:点击后再点目标节点'"
    :aria-label="side === 'left' ? '接上游连线' : '连下游连线'"
    @mousedown.stop
    @click.stop="emit('start', side)"
  >
    +
  </button>
</template>
