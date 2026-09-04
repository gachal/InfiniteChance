<script setup lang="ts">
// 视频节点:图生视频任务的展示面(12 号票)。任务在途时显示进度并可
// 取消(取消不计费),失败显示原因与原地重试(预扣已退回),取消留痕;
// 产物落位后内嵌播放,并提供视频反推提示词入口(13 号票:选中模型即
// 反推,动作上抛给编辑器,提示词落为新提示词节点)。
import { computed, ref, watch } from 'vue'
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
  /** 取消请求在途时禁用取消按钮。 */
  canceling: boolean
  /** 可用的 token 轨聊天模型(编辑器从 /prompt-models 拉取)。 */
  chatModels: string[]
  /** 视频反推的在途标记(编辑器级状态)。 */
  reverseGenerating: boolean
}>()

const emit = defineEmits<{
  retry: []
  cancel: []
  'reverse-prompt': [payload: { model: string }]
}>()

const status = computed(() => props.task?.status)

const reverseModel = ref('')
watch(
  () => props.chatModels,
  (list) => {
    if (!list.includes(reverseModel.value)) {
      reverseModel.value = list.length > 0 ? list[0] : ''
    }
  },
  { immediate: true },
)

// 产物落位才有可反推的视频;聊天模型目录为空时入口整个隐藏。
const canReverse = computed(
  () => !!props.data.url && reverseModel.value !== '' && !props.reverseGenerating,
)

// 产物加载失败(素材被删除、对象被清理):显示占位而非报错(14 号票)。
// url 变化(重新生成/插入新素材)时复位再试。
const loadFailed = ref(false)
watch(
  () => props.data.url,
  () => {
    loadFailed.value = false
  },
)

function submitReverse(): void {
  if (!canReverse.value) {
    return
  }
  emit('reverse-prompt', { model: reverseModel.value })
}
</script>

<template>
  <div
    class="node media video"
    :data-task="status ?? 'none'"
  >
    <header>视频</header>
    <div
      v-if="status === 'queued' || status === 'running'"
      class="placeholder working"
    >
      <span class="pill">{{ status === 'queued' ? '排队中…' : '生成中…' }}</span>
      <small>视频生成需要几分钟,关闭页面不丢失</small>
      <button
        class="cancel"
        type="button"
        :disabled="canceling"
        @click="emit('cancel')"
      >
        {{ canceling ? '取消中…' : '取消生成' }}
      </button>
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
    <div
      v-else-if="status === 'canceled'"
      class="canceled"
    >
      <span>已取消</span>
      <small>预扣额度已退回</small>
    </div>
    <video
      v-else-if="data.url && !loadFailed"
      :src="data.url"
      controls
      muted
      @error="loadFailed = true"
    />
    <div
      v-else-if="data.url && loadFailed"
      class="placeholder missing"
    >
      <span>素材不可用</span>
      <small>原素材可能已被删除</small>
    </div>
    <div
      v-else
      class="placeholder"
    >
      <span>视频占位</span>
      <small>生成结果将出现在这里</small>
    </div>
    <div
      v-if="data.url && chatModels.length > 0"
      class="reverse"
    >
      <div class="reverse-row">
        <select
          v-model="reverseModel"
          title="反推用的聊天模型"
        >
          <option
            v-for="m in chatModels"
            :key="m"
            :value="m"
          >
            {{ m }}
          </option>
        </select>
        <button
          class="reverse-btn"
          type="button"
          :disabled="!canReverse"
          title="分析视频并生成提示词节点"
          @click="submitReverse"
        >
          {{ reverseGenerating ? '反推中…' : '反推提示词' }}
        </button>
      </div>
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

.node[data-task='queued'] {
  border-style: dashed;
}

.node[data-task='running'] {
  border-color: rgba(250, 204, 21, 0.85);
}

.node[data-task='failed'] {
  border-color: rgba(255, 143, 143, 0.6);
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

.placeholder.missing {
  border-color: rgba(255, 143, 143, 0.4);
  color: #ff8f8f;
}

.working .pill {
  color: #facc15;
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

.cancel {
  margin-top: 6px;
  border: 1px solid rgba(255, 255, 255, 0.16);
  border-radius: 8px;
  padding: 5px 12px;
  font-size: 12px;
  cursor: pointer;
  background: transparent;
  color: #aab1c5;
}

.cancel:disabled {
  opacity: 0.5;
  cursor: not-allowed;
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

.canceled {
  display: grid;
  gap: 4px;
  justify-items: center;
  padding: 18px 8px;
  border: 1px dashed rgba(139, 145, 167, 0.45);
  border-radius: 8px;
  color: #8b91a7;
}

.canceled span {
  font-weight: 600;
}

.canceled small {
  font-size: 11px;
}

/* 反推提示词入口(13 号票):模型选择 + 触发按钮,描边样式与提示词
 * 节点的生成提示词动作一致(同为提示词生成类动作)。 */
.reverse {
  margin-top: 8px;
}

.reverse-row {
  display: flex;
  gap: 6px;
}

.reverse-row select {
  flex: 1;
  min-width: 0;
  background: rgba(255, 255, 255, 0.05);
  border: 1px solid rgba(255, 255, 255, 0.12);
  border-radius: 8px;
  padding: 6px;
  color: inherit;
  font-size: 12px;
}

.reverse-btn {
  flex-shrink: 0;
  background: transparent;
  border: 1px solid rgba(122, 162, 247, 0.55);
  border-radius: 8px;
  padding: 6px 12px;
  font-size: 12px;
  font-weight: 600;
  cursor: pointer;
  color: #7aa2f7;
}

.reverse-btn:disabled {
  opacity: 0.45;
  cursor: not-allowed;
}
</style>
