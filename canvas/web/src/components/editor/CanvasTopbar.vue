<script setup lang="ts">
// 画布顶栏(58 号票阶段一自 CanvasEditorView 抽出):面包屑「IP 名 / 画布名」
// + 画布改名(表单态内聚在本组件)+ 自动保存状态 + 在途任务计数。
// 改名提交上抛给视图调 API;返回上抛由视图路由。91 号票 +「助手」按钮:
// 桌宠(92 号票)缺位时的画布助手面板入口,点击上抛由视图开关面板。
// 92 号票 +「桌宠」开关:收起/唤出桌宠(收起 = 不渲染;状态按画布存
// localStorage,视图持有 petCollapsed 并持久)。
import { computed, ref } from 'vue'

const props = defineProps<{
  ipId: number
  ipName: string
  canvasName: string
  saveState: string
  taskActiveCount: number
  /** 桌宠当前是否收起(按钮文案随之切换)。 */
  petCollapsed?: boolean
}>()

const emit = defineEmits<{
  back: []
  rename: [draft: string]
  assistant: []
  togglePet: []
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

    <button
      class="ghost assistant-toggle"
      type="button"
      title="画布助手:对话修改画布(建/改/删节点、连线、触发生成)"
      @click="emit('assistant')"
    >
      助手
    </button>

    <!-- 92 号票:桌宠收起/唤出开关(收起 = 不渲染;状态按画布记忆)。 -->
    <button
      class="ghost pet-toggle"
      type="button"
      :title="petCollapsed ? '唤出画布桌宠' : '收起画布桌宠'"
      @click="emit('togglePet')"
    >
      {{ petCollapsed ? '唤出桌宠' : '收起桌宠' }}
    </button>

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
  color: var(--text-faint);
  font-size: var(--fs-body);
  margin: 0 -4px;
}

.topbar {
  display: flex;
  align-items: center;
  gap: var(--sp-lg);
  padding: 10px 20px;
  border-bottom: 1px solid var(--line-section);
  background: rgba(var(--surface-panel-rgb), 0.9);
}

.topbar button {
  border: none;
  border-radius: var(--r-md);
  padding: 7px 12px;
  font-size: var(--fs-label);
  cursor: pointer;
  transition:
    background-color var(--dur-fast) var(--ease),
    border-color var(--dur-fast) var(--ease),
    color var(--dur-fast) var(--ease);
}

.title {
  background: transparent;
  color: inherit;
  font-size: var(--fs-title);
  font-weight: 600;
}

.title:hover {
  background: var(--wash-soft);
}

.rename-form {
  display: flex;
  gap: var(--sp-sm);
  flex: 1;
}

.rename-form input {
  flex: 1;
  max-width: 320px;
  background: var(--wash-soft);
  border: 1px solid var(--line-control);
  border-radius: var(--r-md);
  padding: 7px 10px;
  color: inherit;
  font-size: var(--fs-body);
  transition: border-color var(--dur-fast) var(--ease);
}

.rename-form input:focus {
  border-color: rgba(var(--accent-rgb), 0.85);
  outline: none;
}

.assistant-toggle {
  margin-left: auto;
  font-weight: 600;
}

/* 92 号票:桌宠开关紧随助手入口,同族弱按钮 —— 字重与相邻的
   assistant-toggle 同档 600(两者是顶栏上的一组开关,视觉成对;
   500 不在 DESIGN.md 字档 400/600/700 内,评审修复归档)。 */
.pet-toggle {
  font-weight: 600;
}

.save-state {
  margin-left: 0;
  color: var(--text-muted);
  font-size: var(--fs-body);
}

/* 已保存态用中性蓝灰而不是绿(38 号票评审:绿色与图片节点/在途任务
   撞色,状态色不该占用类型色)。 */
.save-state[data-state='saved'] {
  color: var(--estimate-ink);
}

.save-state[data-state='error'],
.save-state[data-state='conflict'] {
  color: var(--danger);
}

/* 在途任务 = 活动态,用交互蓝,不再与图片节点的绿色共用。 */
.task-state {
  color: var(--accent);
  font-size: var(--fs-body);
  white-space: nowrap;
}

.primary {
  background: var(--accent);
  color: var(--accent-ink);
  font-weight: 700;
}

.primary:hover {
  background: var(--accent-soft);
}

.ghost {
  background: transparent;
  border: 1px solid var(--line-control);
  color: var(--text-secondary);
}

.ghost:hover {
  border-color: var(--line-action);
  background: var(--wash-soft);
  color: var(--text-primary);
}
</style>
