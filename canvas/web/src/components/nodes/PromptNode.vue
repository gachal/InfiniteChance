<script setup lang="ts">
// 提示词节点:自由文本输入 + 文生图动作入口(10 号票)+ 提示词生成
// 动作入口(11 号票)。内容随整图 JSON 自动保存;动作上抛给编辑器
// (生成结果写回本节点或落为新提示词节点)。不直接改 props:文本变更
// 上抛给编辑器,由 updateNodeData 应用。
import { computed, ref, watch } from 'vue'
import { Handle, Position } from '@vue-flow/core'

import type { PromptTemplateOption } from '@infinitechance/api'

import type { PromptNodeData } from '../../graph'

const props = defineProps<{
  id: string
  type: string
  data: PromptNodeData
  /** 可用的按次计价生图模型(编辑器从 /image-models 拉取)。 */
  models: string[]
  /** 一次只允许一个在途提交(编辑器级状态,防止连点开多个任务)。 */
  generating: boolean
  /** 提示词模板目录(仅启用中的模板,编辑器从 /prompt-templates 拉取)。 */
  templates: PromptTemplateOption[]
  /** 可用的 token 轨聊天模型(编辑器从 /prompt-models 拉取)。 */
  chatModels: string[]
  /** 提示词生成的在途标记(编辑器级状态)。 */
  promptGenerating: boolean
}>()

const emit = defineEmits<{
  'text-change': [value: string]
  generate: [payload: { model: string }]
  'generate-prompt': [payload: { template_id: number; topic: string; model: string }]
}>()

const model = ref('')
watch(
  () => props.models,
  (list) => {
    if (!model.value && list.length > 0) {
      model.value = list[0]
    }
  },
  { immediate: true },
)

const canGenerate = computed(
  () => props.data.text.trim().length > 0 && model.value !== '' && !props.generating,
)

const templateId = ref<number | null>(null)
watch(
  () => props.templates,
  (list) => {
    // 目录刷新后原选中可能已被删除/停用:跟随目录收回默认项。
    if (list.some((t) => t.id === templateId.value)) {
      return
    }
    templateId.value = list.length > 0 ? list[0].id : null
  },
  { immediate: true },
)

const chatModel = ref('')
watch(
  () => props.chatModels,
  (list) => {
    if (!list.includes(chatModel.value)) {
      chatModel.value = list.length > 0 ? list[0] : ''
    }
  },
  { immediate: true },
)

const topic = ref('')

const canGeneratePrompt = computed(
  () =>
    templateId.value !== null &&
    chatModel.value !== '' &&
    topic.value.trim().length > 0 &&
    !props.promptGenerating,
)

function submitGeneratePrompt(): void {
  if (!canGeneratePrompt.value || templateId.value === null) {
    return
  }
  emit('generate-prompt', {
    template_id: templateId.value,
    topic: topic.value.trim(),
    model: chatModel.value,
  })
}
</script>

<template>
  <div class="node prompt">
    <header>提示词</header>
    <textarea
      :value="data.text"
      placeholder="写下提示词…"
      rows="4"
      @input="emit('text-change', ($event.target as HTMLTextAreaElement).value)"
    />
    <div
      v-if="models.length > 0"
      class="generate-row"
    >
      <select
        v-model="model"
        title="生图模型"
      >
        <option
          v-for="m in models"
          :key="m"
          :value="m"
        >
          {{ m }}
        </option>
      </select>
      <button
        class="generate"
        type="button"
        :disabled="!canGenerate"
        @click="emit('generate', { model })"
      >
        {{ generating ? '提交中…' : '生成' }}
      </button>
    </div>
    <p
      v-else
      class="no-models"
    >
      暂无可用生图模型
    </p>

    <div
      v-if="templates.length > 0 && chatModels.length > 0"
      class="prompt-gen"
    >
      <div class="generate-row">
        <select
          v-model="templateId"
          title="提示词模板"
        >
          <option
            v-for="t in templates"
            :key="t.id"
            :value="t.id"
          >
            {{ t.name }}
          </option>
        </select>
        <select
          v-model="chatModel"
          title="聊天模型"
        >
          <option
            v-for="m in chatModels"
            :key="m"
            :value="m"
          >
            {{ m }}
          </option>
        </select>
      </div>
      <div class="generate-row">
        <input
          v-model="topic"
          type="text"
          placeholder="输入主题或参考文本…"
          maxlength="4000"
          @keydown.enter.prevent="submitGeneratePrompt"
        >
        <button
          class="generate outline"
          type="button"
          :disabled="!canGeneratePrompt"
          title="按模板生成提示词"
          @click="submitGeneratePrompt"
        >
          {{ promptGenerating ? '生成中…' : '生成提示词' }}
        </button>
      </div>
    </div>
    <p
      v-else-if="templates.length === 0"
      class="no-models"
    >
      暂无提示词模板,可在管理后台维护
    </p>
    <p
      v-else
      class="no-models"
    >
      暂无可用聊天模型
    </p>
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
  border: 1px solid rgba(122, 162, 247, 0.45);
  border-radius: 12px;
  padding: 10px 12px;
  font-size: 13px;
}

.node header {
  font-weight: 600;
  color: #7aa2f7;
  margin-bottom: 8px;
}

textarea {
  width: 100%;
  resize: vertical;
  background: rgba(255, 255, 255, 0.05);
  border: 1px solid rgba(255, 255, 255, 0.12);
  border-radius: 8px;
  padding: 8px;
  color: inherit;
  font: inherit;
  line-height: 1.5;
}

textarea:focus {
  outline: 2px solid rgba(122, 162, 247, 0.6);
  outline-offset: 1px;
  border-color: transparent;
}

.generate-row {
  display: flex;
  gap: 6px;
  margin-top: 8px;
}

.generate-row select {
  flex: 1;
  min-width: 0;
  background: rgba(255, 255, 255, 0.05);
  border: 1px solid rgba(255, 255, 255, 0.12);
  border-radius: 8px;
  padding: 6px;
  color: inherit;
  font-size: 12px;
}

.generate {
  flex-shrink: 0;
  border: none;
  border-radius: 8px;
  padding: 6px 12px;
  font-size: 12px;
  font-weight: 600;
  cursor: pointer;
  background: #7aa2f7;
  color: #10152a;
}

.generate:disabled {
  opacity: 0.45;
  cursor: not-allowed;
}

.no-models {
  margin: 8px 0 0;
  font-size: 11px;
  color: #8b91a7;
}

.prompt-gen {
  margin-top: 8px;
  display: grid;
  gap: 6px;
}

.generate-row input {
  flex: 1;
  min-width: 0;
  background: rgba(255, 255, 255, 0.05);
  border: 1px solid rgba(255, 255, 255, 0.12);
  border-radius: 8px;
  padding: 6px;
  color: inherit;
  font-size: 12px;
}

.generate-row input:focus {
  outline: 2px solid rgba(122, 162, 247, 0.6);
  outline-offset: 1px;
  border-color: transparent;
}

/* 提示词生成与文生图是两个动作:描边样式区分,主蓝留给生图。 */
.generate.outline {
  background: transparent;
  border: 1px solid rgba(122, 162, 247, 0.55);
  color: #7aa2f7;
}

</style>
