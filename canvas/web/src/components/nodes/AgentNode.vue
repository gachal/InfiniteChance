<script setup lang="ts">
// Agent 节点(29 号票,提示词节点的升级取代;32 号票升级为可携媒体的
// 多模态提示词工作台):上方文本区是当前提示词草稿(生成落点,可手编),
// 底部单框输入即对话行:打 `/` 唤起技能浮层,选中技能成可删 chip,继续
// 输入即主题/修改意见,回车提交;多轮历史存节点 data(换技能 = 开新会话)。
// 32 号票:输入区旁媒体附件双入口(本机上传 / 从素材库选),待发附件
// chips 显示上传中/失败态、逐个可移除,发送只带就绪引用;「历史」按钮开
// 会话历史面板、「新会话」显式清历史。附件清单是编辑器持有的 UI 态,经
// props 下发、事件上抛;生成成功由编辑器沿连线自动投递。不直接改 props:
// 文本变更与技能变更上抛给编辑器,由 updateNodeData 应用。
import { computed, nextTick, ref, watch } from 'vue'
import { Handle, Position } from '@vue-flow/core'

import type { AgentChatMedia, PromptTemplateOption, SkillTarget } from '@infinitechance/api'

import {
  type AgentChatMessage,
  type AgentNodeData,
  agentRoundCount,
} from '../../graph'
import {
  type PendingAgentMedia,
  isAgentMediaUploading,
  readyAgentMediaRefs,
} from '../../agentMedia'
import type { ConnectSide, ConnectState } from '../../composables/useConnection'
import NodePorts from './NodePorts.vue'

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
  /** 待发媒体附件(编辑器持有的 UI 态:上传中/失败 chip 留在输入区)。 */
  attachments: PendingAgentMedia[]
  /** 会话历史面板是否正绑定本节点(「历史」按钮高亮)。 */
  historyOpen: boolean
  /** 素材选择面板是否正绑定本节点(「素材库」按钮高亮)。 */
  pickerOpen: boolean
  /** 连接态展示态(30 号票):origin/valid/dimmed,缺省 = 正常渲染。 */
  connectState?: ConnectState
}>()

const emit = defineEmits<{
  'text-change': [value: string]
  send: [payload: {
    template_id?: number
    topic: string
    model: string
    media: AgentChatMedia[]
    history: AgentChatMessage[]
  }]
  'skill-change': [skillId: number | null]
  deliver: []
  /** 本机文件选作附件(32 号票):编辑器上传入素材库后挂 chip。 */
  'attach-files': [files: File[]]
  /** 移除一条待发附件。 */
  'detach-attachment': [id: number]
  /** 开/关素材选择面板(绑定本节点)。 */
  'open-picker': []
  /** 开/关会话历史面板(绑定本节点)。 */
  'open-history': []
  /** 新会话:清历史与未发送附件(技能不变)。 */
  'new-session': []
  /** 左右 + 按钮点击(30 号票两击连线):side 决定本节点作 source 还是 target。 */
  'connect-start': [side: ConnectSide]
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
const inputEl = ref<HTMLTextAreaElement | null>(null)
const overlayOpen = ref(false)
const highlight = ref(0)
const targetFilter = ref<'all' | 'image' | 'video'>('all')

// 输入框高度(31 号票 + 追补):默认随内容自动调节,1 行起、约 5 行封顶
// 后内部滚动;用户拖动右下角 grip 后高度归用户所有 —— auto-grow 退位,
// 超出部分内部滚动,组件生命周期内有效(不入节点 data,与草稿同属 UI 态)。
const userResized = ref(false)
let resizeArmed = false

// 部分内核会给 textarea 派发原生 resize 事件,直接认定。
function onInputResize(): void {
  userResized.value = true
}

// resize 事件不齐时的兜底:按住右下角 18px 角区并移动即认定在拖 grip
// (只按下不动不误判,避免点角落定位光标关闭自动增高)。
function onInputPointerdown(e: PointerEvent): void {
  const el = inputEl.value
  if (!el) {
    return
  }
  const rect = el.getBoundingClientRect()
  resizeArmed = rect.right - e.clientX <= 18 && rect.bottom - e.clientY <= 18
}

function onInputPointermove(): void {
  if (resizeArmed) {
    userResized.value = true
    resizeArmed = false
  }
}

// 打 `/` 唤起浮层:`/` 后的文本即搜索词(名称/描述),清掉即收起。
watch(input, (value) => {
  if (value.startsWith('/')) {
    overlayOpen.value = true
    highlight.value = 0
  } else {
    overlayOpen.value = false
  }
})

// 输入框自动增高(31 号票):1 行起,约 5 行封顶后内部滚动;
// flush: post 保证读到的 scrollHeight 已含最新文本,提交清空后缩回一行;
// 用户拖过 grip(userResized)后不再代管高度。
watch(input, () => {
  void nextTick(() => {
    const el = inputEl.value
    if (!el || userResized.value) {
      return
    }
    el.style.height = 'auto'
    el.style.height = `${Math.min(el.scrollHeight, 110)}px`
  })
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
  // 多行输入(31 号票):Enter 提交,Shift+Enter 换行(交回默认行为)。
  if (e.key === 'Enter' && !e.shiftKey) {
    e.preventDefault()
    submit()
  }
}

// ---- 提交与投递 ----

const history = computed(() => props.data.messages ?? [])
const rounds = computed(() => agentRoundCount(history.value))

// 待发附件:发送只带就绪引用;还有上传在途时不可发送(引用没就绪);
// 有历史或未发送附件时「新会话」才有意义。
const attachments = computed(() => props.attachments)
const hasConversation = computed(() => history.value.length > 0 || attachments.value.length > 0)

const canSend = computed(
  () =>
    input.value.trim().length > 0 &&
    chatModel.value !== '' &&
    !props.promptGenerating &&
    !isAgentMediaUploading(attachments.value),
)

// 本机附件入口:文件选择后整体上抛,上传与上限校验在编辑器(与对话框
// 上传同址);input 复位让同一文件下次选择仍触发 change。
const fileInput = ref<HTMLInputElement | null>(null)

function onFilesPicked(e: Event): void {
  const input = e.target as HTMLInputElement
  const files = Array.from(input.files ?? [])
  input.value = ''
  if (files.length > 0) {
    emit('attach-files', files)
  }
}

// 主题为空的裸回车不提交:submit 由 canSend 挡下,无副作用。
function submit(): void {
  if (!canSend.value) {
    return
  }
  emit('send', {
    ...(props.data.skill_id != null ? { template_id: props.data.skill_id } : {}),
    topic: input.value.trim(),
    model: chatModel.value,
    media: readyAgentMediaRefs(attachments.value),
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
  <div
    class="node agent"
    :class="connectState ? `connect-${connectState}` : undefined"
  >
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

      <!-- 32 号票:待发附件 chips —— 图片缩略 / 视频徽章,上传中/失败
           态就地展示,逐个可移除;发送成功由编辑器清空进历史。 -->
      <div
        v-if="attachments.length > 0"
        class="chip-row media-chips"
      >
        <span
          v-for="m in attachments"
          :key="m.id"
          class="media-chip"
          :data-state="m.state"
          :title="m.state === 'failed' ? m.error || '上传失败' : m.kind === 'image' ? '图片附件' : '视频附件'"
        >
          <img
            v-if="m.state === 'ready' && m.kind === 'image'"
            :src="m.ref"
            alt="附件图片"
          >
          <span
            v-else
            class="media-badge"
          >{{ m.state === 'uploading' ? (m.kind === 'image' ? '图片上传中…' : '视频上传中…') : m.state === 'failed' ? '上传失败' : m.kind === 'image' ? '图片' : '▶ 视频' }}</span>
          <button
            class="chip-x"
            type="button"
            title="移除附件"
            @click="emit('detach-attachment', m.id)"
          >
            ×
          </button>
        </span>
      </div>

      <div class="input-row">
        <textarea
          ref="inputEl"
          v-model="input"
          rows="1"
          placeholder="输入主题,或打 / 选技能…(Shift+Enter 换行)"
          maxlength="4000"
          @keydown="onInputKeydown"
          @resize="onInputResize"
          @pointerdown="onInputPointerdown"
          @pointermove="onInputPointermove"
        />
      </div>

      <!-- 32 号票:附件双入口(本机上传 / 素材库选)+ 历史面板与新会话
           入口;上限 ≤4 图 + ≤1 视频,拒收提示由编辑器横幅给出。 -->
      <div class="tools-row">
        <button
          class="tool"
          type="button"
          title="上传本机图片/视频作参考输入(单条消息 ≤4 图 + ≤1 视频)"
          @click="fileInput?.click()"
        >
          附件
        </button>
        <button
          class="tool"
          type="button"
          :class="{ on: pickerOpen }"
          title="从素材库选图片/视频作参考输入"
          @click="emit('open-picker')"
        >
          素材库
        </button>
        <span class="tool-spacer" />
        <button
          class="tool"
          type="button"
          :class="{ on: historyOpen }"
          title="查看本节点的完整会话历史"
          @click="emit('open-history')"
        >
          历史
        </button>
        <button
          class="tool"
          type="button"
          :disabled="!hasConversation"
          title="清空对话历史与未发送附件,重新开始会话(技能不变)"
          @click="emit('new-session')"
        >
          新会话
        </button>
        <input
          ref="fileInput"
          class="file-input"
          type="file"
          accept="image/*,video/*"
          multiple
          @change="onFilesPicked"
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

    <!-- 30 号票:左右常驻 + 按钮,两击连线的唯一入口;Handle 已视觉
         隐藏,仅作边端点锚点。 -->
    <NodePorts
      :sides="['left', 'right']"
      @start="emit('connect-start', $event)"
    />

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

/* 32 号票:待发附件 chips —— 图片 chip 是缩略,视频/上传中/失败是徽章。 */
.media-chips {
  flex-wrap: wrap;
  gap: 4px;
}

.media-chip {
  display: inline-flex;
  align-items: center;
  gap: 3px;
  background: rgba(122, 162, 247, 0.12);
  border: 1px solid rgba(122, 162, 247, 0.35);
  border-radius: 8px;
  padding: 2px;
  font-size: 11px;
  color: #c4cdf3;
}

.media-chip[data-state='uploading'] {
  border-style: dashed;
  color: #8b91a7;
}

.media-chip[data-state='failed'] {
  border-color: rgba(224, 49, 49, 0.5);
  color: #ff8f8f;
}

.media-chip img {
  width: 30px;
  height: 30px;
  object-fit: cover;
  border-radius: 6px;
  display: block;
}

.media-badge {
  padding: 0 4px;
  white-space: nowrap;
}

/* 32 号票:附件双入口 + 历史/新会话工具行。 */
.tools-row {
  display: flex;
  align-items: center;
  gap: 4px;
}

.tool {
  border: 1px solid rgba(255, 255, 255, 0.12);
  background: transparent;
  color: #8b91a7;
  border-radius: 7px;
  padding: 3px 7px;
  font-size: 11px;
  cursor: pointer;
  white-space: nowrap;
}

.tool:hover {
  border-color: rgba(122, 162, 247, 0.5);
  color: #c4cdf3;
}

.tool.on {
  border-color: rgba(122, 162, 247, 0.7);
  color: #7aa2f7;
  background: rgba(122, 162, 247, 0.12);
}

.tool:disabled {
  opacity: 0.4;
  cursor: not-allowed;
}

.tool-spacer {
  flex: 1;
}

.file-input {
  display: none;
}

/* 对话输入是多行 textarea(31 号票):默认高度由脚本随内容调节,1 行起、
 * 约 5 行封顶后内部滚动;右下角 grip 可拖拽调高(追补),拖过后高度归
 * 用户,auto-grow 退位 —— CSS 不设 max-height,不挡用户拖大。 */
.input-row textarea {
  width: 100%;
  min-height: 32px;
  resize: vertical;
  overflow-y: auto;
  background: rgba(255, 255, 255, 0.05);
  border: 1px solid rgba(255, 255, 255, 0.12);
  border-radius: 8px;
  padding: 6px 8px;
  color: inherit;
  font-size: 12px;
  line-height: 1.5;
}

.input-row textarea:focus {
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
