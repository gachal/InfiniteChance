<script setup lang="ts">
// Agent 节点(29 号票,提示词节点的升级取代):纯提示词生产者 —— 上方
// 文本区是当前提示词草稿(生成落点,可手编),底部单框输入即对话行:
// 打 `/` 唤起技能浮层,选中技能成可删 chip,继续输入即主题/修改意见,
// 回车提交;多轮历史存节点 data(换技能 = 开新会话)。生成成功由编辑器
// 沿连线自动投递,手改后可点「投递」重推。内联文生图入口随本票移除 ——
// 图片/视频生成统一走媒体节点 + 生成对话框。不直接改 props:文本变更
// 与技能变更上抛给编辑器,由 updateNodeData 应用。
import { computed, ref, watch } from 'vue'
import { Handle, Position } from '@vue-flow/core'

import type { PromptTemplateOption, SkillTarget } from '@infinitechance/api'

import {
  type AgentChatMessage,
  type AgentNodeData,
  agentRoundCount,
} from '../../graph'

const props = defineProps<{
  id: string
  type: string
  data: AgentNodeData
  /** 技能目录(仅启用中的技能,编辑器从 /prompt-templates 拉取)。 */
  skills: PromptTemplateOption[]
  /** 可用的 token 轨聊天模型(编辑器从 /prompt-models 拉取)。 */
  chatModels: string[]
  /** 提示词生成的在途标记(编辑器级状态)。 */
  promptGenerating: boolean
  /** 本节点下游连线上是否存在媒体节点(决定「投递」是否可用)。 */
  hasDownstream: boolean
}>()

const emit = defineEmits<{
  'text-change': [value: string]
  send: [payload: { template_id?: number; topic: string; model: string; history: AgentChatMessage[] }]
  'skill-change': [skillId: number | null]
  deliver: []
}>()

const TARGET_LABEL: Record<SkillTarget, string> = {
  any: '通用',
  image: '图',
  video: '视频',
}

// 聊天模型:目录刷新后跟随收敛到可用项(与旧节点同款纪律)。
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

// 当前技能 chip:按目录解析 skill_id;目录刷新后原选中被删/停用时收回
// 并清会话(沿用旧 templateId watch 语义),已投递文本不受影响。
const activeSkill = computed(
  () => props.skills.find((s) => s.id === props.data.skill_id) ?? null,
)
watch(
  () => props.skills,
  (list) => {
    if (props.data.skill_id != null && !list.some((s) => s.id === props.data.skill_id)) {
      emit('skill-change', null)
    }
  },
)

// ---- 输入行与技能浮层 ----

const input = ref('')
const overlayOpen = ref(false)
const highlight = ref(0)
const targetFilter = ref<'all' | 'image' | 'video'>('all')

// 打 `/` 唤起浮层:`/` 后的文本即搜索词(名称/描述),清掉即收起。
watch(input, (value) => {
  if (value.startsWith('/')) {
    overlayOpen.value = true
    highlight.value = 0
  } else {
    overlayOpen.value = false
  }
})

const filteredSkills = computed(() => {
  const query = input.value.slice(1).trim().toLowerCase()
  return props.skills.filter((s) => {
    if (targetFilter.value === 'image' && s.target === 'video') {
      return false
    }
    if (targetFilter.value === 'video' && s.target === 'image') {
      return false
    }
    if (query === '') {
      return true
    }
    return s.name.toLowerCase().includes(query) || s.description.toLowerCase().includes(query)
  })
})

watch(
  () => filteredSkills.value.length,
  (len) => {
    if (highlight.value >= len) {
      highlight.value = Math.max(len - 1, 0)
    }
  },
)

function chooseSkill(skill: PromptTemplateOption): void {
  if (props.data.skill_id !== skill.id) {
    // 换技能 = 开新会话:清历史由编辑器在应用 skill-change 时一并处理。
    emit('skill-change', skill.id)
  }
  input.value = ''
}

function removeSkill(): void {
  emit('skill-change', null)
}

function onInputKeydown(e: KeyboardEvent): void {
  // 中文输入法的选词回车/方向键不属于节点交互,交给 IME。
  if (e.isComposing) {
    return
  }
  if (overlayOpen.value) {
    const len = filteredSkills.value.length
    if (e.key === 'ArrowDown' && len > 0) {
      e.preventDefault()
      highlight.value = (highlight.value + 1) % len
      return
    }
    if (e.key === 'ArrowUp' && len > 0) {
      e.preventDefault()
      highlight.value = (highlight.value - 1 + len) % len
      return
    }
    if (e.key === 'Enter') {
      e.preventDefault()
      const picked = filteredSkills.value[highlight.value] ?? filteredSkills.value[0]
      if (picked) {
        chooseSkill(picked)
      }
      return
    }
    if (e.key === 'Escape') {
      e.preventDefault()
      input.value = ''
      return
    }
    return
  }
  if (e.key === 'Enter') {
    e.preventDefault()
    submit()
  }
}

// ---- 提交与投递 ----

const history = computed(() => props.data.messages ?? [])
const rounds = computed(() => agentRoundCount(history.value))

const canSend = computed(
  () => input.value.trim().length > 0 && chatModel.value !== '' && !props.promptGenerating,
)

// 主题为空的裸回车不提交:submit 由 canSend 挡下,无副作用。
function submit(): void {
  if (!canSend.value) {
    return
  }
  emit('send', {
    ...(props.data.skill_id != null ? { template_id: props.data.skill_id } : {}),
    topic: input.value.trim(),
    model: chatModel.value,
    history: history.value,
  })
  input.value = ''
}

const canDeliver = computed(() => props.data.text.trim().length > 0 && props.hasDownstream)

const deliveredFlash = ref(false)
let deliveredTimer: number | undefined

function onDeliver(): void {
  if (!canDeliver.value) {
    return
  }
  emit('deliver')
  deliveredFlash.value = true
  window.clearTimeout(deliveredTimer)
  deliveredTimer = window.setTimeout(() => {
    deliveredFlash.value = false
  }, 1500)
}
</script>

<template>
  <div class="node agent">
    <header>Agent</header>
    <textarea
      :value="data.text"
      placeholder="提示词草稿 —— 生成结果落在这里,可手编"
      rows="5"
      @input="emit('text-change', ($event.target as HTMLTextAreaElement).value)"
    />

    <div class="chat">
      <div
        v-if="activeSkill"
        class="chip-row"
      >
        <span class="chip">
          <span
            class="chip-target"
            :data-target="activeSkill.target"
          >{{ TARGET_LABEL[activeSkill.target] }}</span>
          <span class="chip-name">{{ activeSkill.name }}</span>
          <button
            class="chip-x"
            type="button"
            title="移除技能(开启新会话)"
            @click="removeSkill"
          >
            ×
          </button>
        </span>
      </div>

      <div class="input-row">
        <input
          v-model="input"
          type="text"
          placeholder="输入主题,或打 / 选技能…"
          maxlength="4000"
          @keydown="onInputKeydown"
        >
      </div>

      <div class="action-row">
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
        <button
          class="deliver"
          type="button"
          :disabled="!canDeliver"
          title="把当前提示词写进所有下游连线的图片/视频节点"
          @click="onDeliver"
        >
          {{ deliveredFlash ? '已投递' : '投递' }}
        </button>
        <button
          class="generate"
          type="button"
          :disabled="!canSend"
          title="按当前技能与输入生成/修改提示词"
          @click="submit"
        >
          {{ promptGenerating ? '生成中…' : '生成' }}
        </button>
      </div>

      <p
        v-if="rounds > 0"
        class="rounds"
      >
        已对话 {{ rounds }} 轮 · 继续输入即修改意见 · 换技能开新会话
      </p>
      <p
        v-else-if="chatModels.length === 0"
        class="no-models"
      >
        暂无可用聊天模型
      </p>

      <div
        v-if="overlayOpen"
        class="skill-overlay"
      >
        <div class="overlay-filter">
          <button
            type="button"
            :class="{ on: targetFilter === 'all' }"
            @mousedown.prevent="targetFilter = 'all'"
          >
            全部
          </button>
          <button
            type="button"
            :class="{ on: targetFilter === 'image' }"
            @mousedown.prevent="targetFilter = 'image'"
          >
            图
          </button>
          <button
            type="button"
            :class="{ on: targetFilter === 'video' }"
            @mousedown.prevent="targetFilter = 'video'"
          >
            视频
          </button>
        </div>
        <ul class="skill-list">
          <li
            v-for="(s, i) in filteredSkills"
            :key="s.id"
            :class="{ on: i === highlight }"
            @mousedown.prevent="chooseSkill(s)"
          >
            <span class="skill-line">
              <span class="skill-name">{{ s.name }}</span>
              <span
                class="badge"
                :data-target="s.target"
              >{{ TARGET_LABEL[s.target] }}</span>
            </span>
            <span
              v-if="s.description"
              class="skill-desc"
            >{{ s.description }}</span>
          </li>
          <li
            v-if="filteredSkills.length === 0"
            class="empty"
          >
            没有匹配的技能,可在管理后台维护
          </li>
        </ul>
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
  width: 260px;
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

.chat {
  position: relative;
  margin-top: 8px;
  display: grid;
  gap: 6px;
}

.chip-row {
  display: flex;
}

.chip {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  max-width: 100%;
  background: rgba(122, 162, 247, 0.14);
  border: 1px solid rgba(122, 162, 247, 0.4);
  border-radius: 999px;
  padding: 2px 4px 2px 8px;
  font-size: 11px;
  color: #c4cdf3;
}

.chip-target {
  flex-shrink: 0;
  border-radius: 999px;
  padding: 0 6px;
  font-size: 10px;
  background: rgba(122, 162, 247, 0.2);
  color: #9fb4f9;
}

.chip-target[data-target='image'] {
  background: rgba(74, 222, 128, 0.16);
  color: #4ade80;
}

.chip-target[data-target='video'] {
  background: rgba(250, 204, 21, 0.16);
  color: #facc15;
}

.chip-name {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.chip-x {
  flex-shrink: 0;
  border: none;
  background: transparent;
  color: #8b91a7;
  font-size: 13px;
  line-height: 1;
  cursor: pointer;
  padding: 2px 4px;
}

.chip-x:hover {
  color: #ff8f8f;
}

.input-row input {
  width: 100%;
  background: rgba(255, 255, 255, 0.05);
  border: 1px solid rgba(255, 255, 255, 0.12);
  border-radius: 8px;
  padding: 6px 8px;
  color: inherit;
  font-size: 12px;
}

.input-row input:focus {
  outline: 2px solid rgba(122, 162, 247, 0.6);
  outline-offset: 1px;
  border-color: transparent;
}

.action-row {
  display: flex;
  gap: 6px;
}

.action-row select {
  flex: 1;
  min-width: 0;
  background: rgba(255, 255, 255, 0.05);
  border: 1px solid rgba(255, 255, 255, 0.12);
  border-radius: 8px;
  padding: 6px;
  color: inherit;
  font-size: 12px;
}

.action-row button {
  flex-shrink: 0;
  border: none;
  border-radius: 8px;
  padding: 6px 10px;
  font-size: 12px;
  font-weight: 600;
  cursor: pointer;
}

.action-row button:disabled {
  opacity: 0.45;
  cursor: not-allowed;
}

.generate {
  background: #7aa2f7;
  color: #10152a;
}

/* 投递是次级动作:描边样式区分,主蓝留给生成。 */
.deliver {
  background: transparent;
  border: 1px solid rgba(122, 162, 247, 0.55) !important;
  color: #7aa2f7;
}

.rounds,
.no-models {
  margin: 0;
  font-size: 11px;
  color: #8b91a7;
}

/* 技能浮层:盖在输入行上方,即梦式斜杠命令。 */
.skill-overlay {
  position: absolute;
  left: 0;
  right: 0;
  bottom: calc(100% + 6px);
  background: rgba(13, 21, 36, 0.98);
  border: 1px solid rgba(122, 162, 247, 0.45);
  border-radius: 10px;
  box-shadow: 0 8px 28px rgba(0, 0, 0, 0.5);
  padding: 6px;
  z-index: 30;
}

.overlay-filter {
  display: flex;
  gap: 4px;
  padding: 2px 2px 6px;
}

.overlay-filter button {
  border: 1px solid rgba(255, 255, 255, 0.12);
  background: transparent;
  color: #8b91a7;
  border-radius: 999px;
  padding: 1px 8px;
  font-size: 11px;
  cursor: pointer;
}

.overlay-filter button.on {
  border-color: rgba(122, 162, 247, 0.6);
  color: #7aa2f7;
}

.skill-list {
  list-style: none;
  margin: 0;
  padding: 0;
  max-height: 180px;
  overflow-y: auto;
  display: grid;
  gap: 2px;
}

.skill-list li {
  display: grid;
  gap: 2px;
  border-radius: 8px;
  padding: 6px 8px;
  cursor: pointer;
}

.skill-list li.on {
  background: rgba(122, 162, 247, 0.16);
}

.skill-list li.empty {
  cursor: default;
  color: #8b91a7;
  font-size: 11px;
}

.skill-line {
  display: flex;
  align-items: center;
  gap: 6px;
  min-width: 0;
}

.skill-name {
  flex: 1;
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  font-size: 12px;
}

.skill-list .badge {
  flex-shrink: 0;
  border-radius: 999px;
  padding: 0 6px;
  font-size: 10px;
  background: rgba(122, 162, 247, 0.2);
  color: #9fb4f9;
}

.skill-list .badge[data-target='image'] {
  background: rgba(74, 222, 128, 0.16);
  color: #4ade80;
}

.skill-list .badge[data-target='video'] {
  background: rgba(250, 204, 21, 0.16);
  color: #facc15;
}

.skill-desc {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  font-size: 11px;
  color: #8b91a7;
}
</style>
