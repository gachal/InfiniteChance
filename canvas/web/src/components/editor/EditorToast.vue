<script setup lang="ts">
// 轻提示的呈现层(38 号票评审,58 号票阶段一自视图抽出):文案由
// useEditorToast 驱动,样式锚定 .canvas-wrap 底部居中。
defineProps<{ text: string }>()
</script>

<template>
  <div
    v-if="text"
    class="toast"
    role="status"
  >
    {{ text }}
  </div>
</template>

<style scoped>
/* 38 号票评审:画布底部居中,自动消退,不抢焦点。z 取 120 高于
   灯箱(z-100,Teleport 到 body;.editor 不建层叠上下文,全局可比)——
   41 号票截帧等浮层期间的反馈要浮在遮罩之上可见。 */
.toast {
  position: absolute;
  left: 50%;
  bottom: 18px;
  transform: translateX(-50%);
  z-index: var(--z-toast);
  max-width: 560px;
  padding: 8px 16px;
  border-radius: var(--r-lg);
  background: rgba(var(--surface-card-rgb), 0.95);
  border: 1px solid rgba(var(--accent-rgb), 0.4);
  color: var(--text-bright);
  font-size: var(--fs-body);
  line-height: 1.5;
  box-shadow: var(--shadow-dialog);
  pointer-events: none;
  animation: toast-in var(--dur) var(--ease);
}

@keyframes toast-in {
  from {
    opacity: 0;
    transform: translateX(-50%) translateY(8px);
  }
  to {
    opacity: 1;
    transform: translateX(-50%) translateY(0);
  }
}
</style>
