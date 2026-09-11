<script setup lang="ts">
// 分析节点(17 号票):对来源媒体(视频/图片)的结构化理解,由来源节点
// 上的「分析」动作创建并连线。MVP 文本只读展示(pre-wrap + 复制);空
// 节点(分析在途,或上次分析失败)可原地重新分析 —— 模型默认沿用上次
// 生成用的(data.model)。删除走画布通用交互(选中 + Backspace,
// vue-flow 默认 deleteKeyCode)。
import { computed, ref, watch } from 'vue'
import { Handle, Position } from '@vue-flow/core'

import type { AnalysisNodeData } from '../../graph'

const props = defineProps<{
  id: string
  type: string
  data: AnalysisNodeData
  /** 可用的 token 轨聊天模型(编辑器从 /prompt-models 拉取)。 */
  chatModels: string[]
  /** 本节点的分析请求在途(编辑器级状态)。 */
  analyzing: boolean
}>()

const emit = defineEmits<{
  analyze: [payload: { model: string }]
}>()

const hasText = computed(() => props.data.text.trim().length > 0)

const model = ref('')
watch(
  () => props.chatModels,
  (list) => {
    if (list.includes(props.data.model ?? '')) {
      model.value = props.data.model as string
      return
    }
    if (!list.includes(model.value)) {
      model.value = list.length > 0 ? list[0] : ''
    }
  },
  { immediate: true },
)

const canAnalyze = computed(() => model.value !== '' && !props.analyzing)

function submitAnalyze(): void {
  if (!canAnalyze.value) {
    return
  }
  emit('analyze', { model: model.value })
}

// 复制成功是转瞬的反馈:两秒后回到「复制」。
const copied = ref(false)
let copiedTimer: ReturnType<typeof setTimeout> | undefined
async function copyText(): Promise<void> {
  try {
    await navigator.clipboard.writeText(props.data.text)
    copied.value = true
    clearTimeout(copiedTimer)
    copiedTimer = setTimeout(() => {
      copied.value = false
    }, 2000)
  } catch {
    /* 剪贴板不可用(非安全上下文等):静默,按钮原样 */
  }
}
</script>

<template>
  <div
    class="node analysis"
    :data-state="analyzing ? 'working' : hasText ? 'done' : 'empty'"
  >
    <header>分析</header>
    <div
      v-if="analyzing"
      class="placeholder working"
    >
      <span class="pill">分析中…</span>
      <small>分镜分析可能需要一两分钟</small>
    </div>
    <template v-else-if="hasText">
      <div class="report">
        {{ data.text }}
      </div>
      <div class="report-meta">
        <small>{{ data.model || '已完成' }}</small>
        <button
          class="copy"
          type="button"
          @click="copyText"
        >
          {{ copied ? '已复制' : '复制' }}
        </button>
      </div>
    </template>
    <div
      v-else
      class="placeholder empty"
    >
      <span>未得到分析结果</span>
      <small>上次分析未完成或已失败</small>
      <div
        v-if="chatModels.length > 0"
        class="redo-row"
      >
        <select
          v-model="model"
          title="分析用的聊天模型"
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
          class="redo"
          type="button"
          :disabled="!canAnalyze"
          title="用所选模型重新分析来源媒体"
          @click="submitAnalyze"
        >
          重新分析
        </button>
      </div>
      <small
        v-else
        class="no-models"
      >
        暂无可用聊天模型
      </small>
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
  width: 260px;
  background: rgba(20, 26, 43, 0.92);
  border: 1px solid rgba(165, 180, 252, 0.45);
  border-radius: 12px;
  padding: 10px 12px;
  font-size: 13px;
}

.node header {
  font-weight: 600;
  color: #a5b4fc;
  margin-bottom: 8px;
}

.node[data-state='working'] {
  border-style: dashed;
  border-color: rgba(165, 180, 252, 0.85);
}

/* 报告只读展示:pre-wrap 保留模型输出的 markdown 换行与缩进。 */
.report {
  max-height: 220px;
  overflow-y: auto;
  white-space: pre-wrap;
  word-break: break-word;
  line-height: 1.55;
  background: rgba(255, 255, 255, 0.04);
  border: 1px solid rgba(255, 255, 255, 0.1);
  border-radius: 8px;
  padding: 8px;
}

.report-meta {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
  margin-top: 6px;
}

.report-meta small {
  color: #8b91a7;
  font-size: 11px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.copy {
  flex-shrink: 0;
  border: 1px solid rgba(255, 255, 255, 0.16);
  border-radius: 8px;
  padding: 4px 10px;
  font-size: 12px;
  cursor: pointer;
  background: transparent;
  color: #aab1c5;
}

.copy:hover {
  border-color: rgba(255, 255, 255, 0.32);
  color: inherit;
}

.placeholder {
  display: grid;
  gap: 4px;
  justify-items: center;
  padding: 18px 8px;
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

.placeholder .pill {
  color: #a5b4fc;
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

.redo-row {
  display: flex;
  gap: 6px;
  margin-top: 6px;
  width: 100%;
}

.redo-row select {
  flex: 1;
  min-width: 0;
  background: rgba(255, 255, 255, 0.05);
  border: 1px solid rgba(255, 255, 255, 0.12);
  border-radius: 8px;
  padding: 6px;
  color: inherit;
  font-size: 12px;
}

.redo {
  flex-shrink: 0;
  background: transparent;
  border: 1px solid rgba(165, 180, 252, 0.55);
  border-radius: 8px;
  padding: 6px 12px;
  font-size: 12px;
  font-weight: 600;
  cursor: pointer;
  color: #a5b4fc;
}

.redo:disabled {
  opacity: 0.45;
  cursor: not-allowed;
}

.no-models {
  color: #8b91a7;
}
</style>
