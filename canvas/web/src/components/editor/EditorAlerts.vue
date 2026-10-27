<script setup lang="ts">
// 画布横幅收纳列(38 号票评审,58 号票阶段一自 CanvasEditorView 抽出):
// 生成失败 / 版本冲突 / 加载失败 / 画布缺失四类横幅同列堆叠;
// 全部动作上抛,视图持有对应的业务状态与处理器。
defineProps<{
  generateError: string
  loadError: string
  missing: boolean
  autosaveState: string
  resolvingConflict: boolean
}>()

const emit = defineEmits<{
  dismissGenerate: []
  reloadServer: []
  overwriteServer: []
  retry: []
  back: []
}>()
</script>

<template>
  <!-- 横幅统一收纳(38 号票评审):多条同时出现时聚成一列,不再逐条
       把画布往下推散。 -->
  <div class="alerts">
    <div
      v-if="generateError"
      class="generate-error"
      role="alert"
    >
      <p>{{ generateError }}</p>
      <button
        class="ghost"
        type="button"
        @click="emit('dismissGenerate')"
      >
        知道了
      </button>
    </div>

    <div
      v-if="autosaveState === 'conflict'"
      class="conflict-banner"
      role="alert"
    >
      <p>
        画布已在其他窗口被修改,本地更改尚未保存。
        可加载服务器版本(放弃本地更改),或以当前内容覆盖服务器。
      </p>
      <span class="conflict-actions">
        <button
          class="primary"
          type="button"
          :disabled="resolvingConflict"
          @click="emit('reloadServer')"
        >
          加载服务器版本
        </button>
        <button
          class="danger"
          type="button"
          :disabled="resolvingConflict"
          @click="emit('overwriteServer')"
        >
          以我的版本覆盖
        </button>
      </span>
    </div>

    <div
      v-if="loadError"
      class="load-error"
      role="alert"
    >
      <p>{{ loadError }}</p>
      <button
        class="ghost"
        type="button"
        @click="emit('retry')"
      >
        重试
      </button>
    </div>

    <div
      v-if="missing"
      class="load-error"
    >
      <p>画布不存在或已被删除。</p>
      <button
        class="ghost"
        type="button"
        @click="emit('back')"
      >
        返回列表
      </button>
    </div>
  </div>
</template>

<style scoped>
/* 横幅收纳列(38 号票评审):多条横幅同列堆叠,视觉上是一个系统。
   阶段二:横幅出现用基础过渡包的下滑入场。 */
.alerts {
  display: flex;
  flex-direction: column;
}

.alerts > div {
  animation: alert-in var(--dur-slow) var(--ease);
}

@keyframes alert-in {
  from {
    opacity: 0;
    transform: translateY(-6px);
  }
  to {
    opacity: 1;
    transform: translateY(0);
  }
}

.generate-error {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 14px;
  padding: 12px 20px;
  background: rgba(var(--danger-rgb), 0.12);
  border-bottom: 1px solid rgba(var(--danger-rgb), 0.35);
}

.generate-error p {
  margin: 0;
  font-size: var(--fs-body);
}

.generate-error button {
  border: 1px solid var(--line-control);
  border-radius: var(--r-md);
  padding: 7px 14px;
  font-size: var(--fs-body);
  cursor: pointer;
  background: transparent;
  color: var(--text-secondary);
  white-space: nowrap;
  transition:
    border-color var(--dur-fast) var(--ease),
    background-color var(--dur-fast) var(--ease),
    color var(--dur-fast) var(--ease);
}

.conflict-banner {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 16px;
  padding: 12px 20px;
  background: rgba(var(--danger-rgb), 0.16);
  border-bottom: 1px solid rgba(var(--danger-rgb), 0.45);
}

.conflict-banner p {
  margin: 0;
  font-size: var(--fs-body);
}

.conflict-actions {
  display: flex;
  gap: var(--sp-sm);
  white-space: nowrap;
}

.conflict-actions button,
.load-error button {
  border: none;
  border-radius: var(--r-md);
  padding: 7px 14px;
  font-size: var(--fs-body);
  cursor: pointer;
  transition:
    background-color var(--dur-fast) var(--ease),
    border-color var(--dur-fast) var(--ease);
}

.load-error {
  display: flex;
  align-items: center;
  gap: 14px;
  padding: 12px 20px;
  background: rgba(var(--danger-rgb), 0.12);
  border-bottom: 1px solid rgba(var(--danger-rgb), 0.35);
}

.load-error p {
  margin: 0;
  font-size: var(--fs-body);
}

.primary {
  background: var(--accent);
  color: var(--accent-ink);
  font-weight: 700;
}

.primary:hover {
  background: var(--accent-soft);
}

/* 危险操作 = 失败绯文字而非红底(DESIGN.md):描边玻璃 + 红字,
   hover 才升起红雾,避免横幅区变成一堵红墙。 */
.danger {
  background: transparent;
  border: 1px solid rgba(var(--danger-rgb), 0.5);
  color: var(--danger);
  font-weight: 600;
}

.danger:hover {
  background: rgba(var(--danger-rgb), 0.12);
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
