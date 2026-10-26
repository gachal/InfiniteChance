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
/* 横幅收纳列(38 号票评审):多条横幅同列堆叠,视觉上是一个系统。 */
.alerts {
  display: flex;
  flex-direction: column;
}

.generate-error {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 14px;
  padding: 12px 16px;
  background: rgba(224, 49, 49, 0.12);
  border-bottom: 1px solid rgba(224, 49, 49, 0.35);
}

.generate-error p {
  margin: 0;
  font-size: 14px;
}

.generate-error button {
  border: 1px solid var(--line-16);
  border-radius: var(--r-md);
  padding: 8px 14px;
  font-size: 13px;
  cursor: pointer;
  background: transparent;
  color: var(--text-secondary);
  white-space: nowrap;
}

.conflict-banner {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 16px;
  padding: 12px 16px;
  background: rgba(224, 49, 49, 0.16);
  border-bottom: 1px solid rgba(224, 49, 49, 0.45);
}

.conflict-banner p {
  margin: 0;
  font-size: 14px;
}

.conflict-actions {
  display: flex;
  gap: 8px;
  white-space: nowrap;
}

.conflict-actions button,
.load-error button {
  border: none;
  border-radius: var(--r-md);
  padding: 8px 14px;
  font-size: 13px;
  cursor: pointer;
}

.load-error {
  display: flex;
  align-items: center;
  gap: 14px;
  padding: 12px 16px;
  background: rgba(224, 49, 49, 0.12);
  border-bottom: 1px solid rgba(224, 49, 49, 0.35);
}

.load-error p {
  margin: 0;
  font-size: 14px;
}

.primary {
  background: #3b5bdb;
  color: #fff;
  font-weight: 600;
}

.danger {
  background: #e03131;
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
