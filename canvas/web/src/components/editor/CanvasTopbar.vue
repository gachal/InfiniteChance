<script setup lang="ts">
// 画布顶栏(58 号票阶段一自 CanvasEditorView 抽出):面包屑「IP 名 / 画布名」
// + 画布改名(表单态内聚在本组件)+ 自动保存状态 + 在途任务计数。
// 改名提交上抛给视图调 API;返回上抛由视图路由。
import { computed, ref } from 'vue'

const props = defineProps<{
  ipId: number
  ipName: string
  canvasName: string
  saveState: string
  taskActiveCount: number
}>()

const emit = defineEmits<{
  back: []
  rename: [draft: string]
}>()

const renaming = ref(false)
const renameDraft = ref('')

const saveLabel = computed(() => {
  switch (props.saveState) {
    case 'dirty':
      return '有未保存的更改'
    case 'saving':
      return '保存中…'
    case 'saved':
      return '已保存'
    case 'error':
      return '保存失败,将自动重试'
    case 'conflict':
      return '版本冲突'
    default:
      return '所有更改已保存'
  }
})

function commitRename(): void {
  renaming.value = false
  emit('rename', renameDraft.value)
}

/** Escape 取消改名:全局键盘纪律(58 号票前为视图内联状态),由视图的
 * onGlobalKeydown 调用;返回是否发生了取消。 */
function cancelRenaming(): boolean {
  if (!renaming.value) {
    return false
  }
  renaming.value = false
  return true
}

defineExpose({ cancelRenaming })
</script>

<template>
  <header class="topbar">
    <!-- 50 号票面包屑:「IP 名 / 画布名」,IP 段点击回详情页;53 号票 A 段
         撤类型徽章 —— 画布回归通用,类型不再展示。 -->
    <button
      class="ghost breadcrumb-ip"
      type="button"
      :title="ipId > 0 ? `回到「${ipName || 'IP'}」详情` : '回到 IP 列表'"
      @click="emit('back')"
    >
      ← {{ ipId > 0 && ipName ? ipName : 'IP 列表' }}
    </button>
    <span
      v-if="ipId > 0 && ipName"
      class="breadcrumb-sep"
    >/</span>

    <template v-if="renaming">
      <form
        class="rename-form"
        @submit.prevent="commitRename"
      >
        <input
          v-model="renameDraft"
          type="text"
          maxlength="128"
          autofocus
        >
        <button
          class="primary"
          type="submit"
        >
          保存
        </button>
        <button
          class="ghost"
          type="button"
          @click="renaming = false"
        >
          取消
        </button>
      </form>
    </template>
    <template v-else>
      <button
        class="title"
        type="button"
        title="点击重命名"
        @click="renaming = true; renameDraft = canvasName"
      >
        {{ canvasName || '未命名画布' }}
      </button>
    </template>

    <span
      class="save-state"
      :data-state="saveState"
    >
      {{ saveLabel }}
    </span>

    <span
      v-if="taskActiveCount > 0"
      class="task-state"
    >
      生成中 {{ taskActiveCount }} 个任务…
    </span>
  </header>
</template>

<style scoped>
.breadcrumb-sep {
  color: #4c5468;
  font-size: 14px;
  margin: 0 -4px;
}

.topbar {
  display: flex;
  align-items: center;
  gap: 14px;
  padding: 10px 16px;
  border-bottom: 1px solid var(--line-08);
  background: rgba(var(--surface-panel-rgb), 0.9);
}

.topbar button {
  border: none;
  border-radius: var(--r-md);
  padding: 8px 12px;
  font-size: 14px;
  cursor: pointer;
}

.title {
  background: transparent;
  color: inherit;
  font-size: 16px;
  font-weight: 600;
}

.title:hover {
  background: var(--wash-06);
}

.rename-form {
  display: flex;
  gap: 8px;
  flex: 1;
}

.rename-form input {
  flex: 1;
  max-width: 320px;
  background: var(--wash-05);
  border: 1px solid var(--line-12);
  border-radius: var(--r-md);
  padding: 8px 10px;
  color: inherit;
  font-size: 14px;
}

.save-state {
  margin-left: auto;
  color: var(--text-muted);
  font-size: 13px;
}

/* 已保存态用中性蓝灰而不是绿(38 号票评审:绿色与图片节点/在途任务
   撞色,状态色不该占用类型色)。 */
.save-state[data-state='saved'] {
  color: #a8b6d8;
}

.save-state[data-state='error'],
.save-state[data-state='conflict'] {
  color: var(--danger);
}

/* 在途任务 = 活动态,用交互蓝,不再与图片节点的绿色共用。 */
.task-state {
  color: var(--accent);
  font-size: 13px;
  white-space: nowrap;
}

.primary {
  background: #3b5bdb;
  color: #fff;
  font-weight: 600;
}

.ghost {
  background: transparent;
  border: 1px solid var(--line-16);
  color: var(--text-secondary);
}

.ghost:hover {
  border-color: var(--line-32);
  color: inherit;
}
</style>
