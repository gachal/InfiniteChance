<script setup lang="ts">
// 音色节点(57 号票,第十种节点类型「创作」组):创建表单内联卡上(不进
// 生成对话框、不进编辑层)—— 设计(名称 + 音色描述 + 试听文本 + 模型下拉)
// 与克隆(样本 chip + 试听文本 + 模型下拉)二选一,提交 = kind=voice 的
// canvas_task 绑本节点;卡面按台账行呈现状态徽章 + 试听 + 失败原地重试 +
// 「合成台词」入口 + 「删除音色」(二次确认在编辑器侧)。表单事实随节点
// data 整图 JSON 持久化(每次编辑 emit form-change,updateNodeData 落盘,
// 自动保存免费获得)。只渲染右把手(voice → audio 合成来源边)。
import { computed, ref } from 'vue'
import { Handle, Position } from '@vue-flow/core'

import type { CanvasTask, ModelPriceSummary, VoiceRecord } from '@infinitechance/api'

import type { VoiceNodeData } from '../../graph'
import type { ConnectSide, ConnectState } from '../../composables/useConnection'
import {
  MAX_PREVIEW_TEXT_RUNES,
  MIN_PREVIEW_TEXT_RUNES,
  estimateSpeechCNY,
  fmtCNY,
  validateSampleFile,
  validateSpeechForm,
  validateVoiceForm,
  voiceStatusBadge,
} from '../../voice'
import { assetContentURL } from '../../thumbnail'
import NodePorts from './NodePorts.vue'

const props = defineProps<{
  id: string
  type: string
  data: VoiceNodeData
  /** 音色台账目录(编辑器拉取;按 data.ledger_id 找绑定行)。 */
  voices: VoiceRecord[]
  /** /audio-models 目录:字符轨合成模型与价格(创建表单下拉 + 台词预估)。 */
  audioModels: string[]
  audioPrices: Record<string, ModelPriceSummary>
  /** voice-enrollment 的按次价(创建一次收一次;image-models 目录的投影,
   * 未配价时创建预估区退位为字数提示)。 */
  enrollmentPrice?: ModelPriceSummary
  /** 绑定到本节点的创建任务(编辑器轮询保持最新;null = 无任务)。 */
  task?: CanvasTask | null
  /** 重试请求在途时禁用重试按钮。 */
  retrying: boolean
  /** 删除音色请求在途(编辑器级)。 */
  deleting: boolean
  /** 连接态展示态(30 号票):origin/valid/dimmed,缺省 = 正常渲染。 */
  connectState?: ConnectState
}>()

const emit = defineEmits<{
  /** 表单事实变更(逐键上抛,编辑器 updateNodeData 浅合并落盘)。 */
  'form-change': [patch: Partial<VoiceNodeData>]
  /** 提交音色创建(以当前 data 为准;校验在节点侧,提示本地展示)。 */
  submit: []
  retry: []
  /** 右 + 连下游(voice → audio,台词合成来源)。 */
  'connect-start': [side: ConnectSide]
  /** 克隆样本:本机上传(编辑器 uploadAsset kind=audio)与素材库挑选。 */
  'sample-file': [file: File]
  'pick-sample': []
  /** 台词合成入口:编辑器先落右侧音频节点 + voice→audio 边再提交任务。 */
  speech: [payload: { text: string; instruction: string }]
  /** 删除音色(台账行 + 上游;节点保留为可再编辑的表单卡)。 */
  'delete-voice': []
}>()

// ---- 卡面状态派生 ----

const taskStatus = computed(() => props.task?.status)
const inFlight = computed(() => taskStatus.value === 'queued' || taskStatus.value === 'running')

const ledger = computed<VoiceRecord | null>(() => {
  if (!props.data.ledger_id) {
    return null
  }
  return props.voices.find((v) => v.id === props.data.ledger_id) ?? null
})

const badge = computed(() => voiceStatusBadge(ledger.value?.status ?? props.data.status))

// ---- 创建表单(受控:所有编辑 emit form-change)----

const formError = ref('')
const uploadingSample = ref(false)
const sampleInput = ref<HTMLInputElement | null>(null)

function pickSampleFile(): void {
  sampleInput.value?.click()
}

const previewRunes = computed(() => [...(props.data.preview_text ?? '').trim()].length)
const promptRunes = computed(() => [...(props.data.voice_prompt ?? '').trim()].length)

const selectedModelPrice = computed(() => {
  const m = props.data.target_model
  return m ? props.audioPrices[m] : undefined
})

const createEstimate = computed(() => {
  const p = props.enrollmentPrice
  if (p?.unit === 'call' && p.cny_per_call != null) {
    return `创建一次 ${fmtCNY(p.cny_per_call)}(试听可回放)`
  }
  return `按次计费,以管理端配价为准`
})

const canSubmit = computed(
  () =>
    validateVoiceForm({
      mode: props.data.mode,
      name: props.data.name,
      target_model: props.data.target_model ?? '',
      voice_prompt: props.data.voice_prompt,
      preview_text: props.data.preview_text ?? '',
      sample_asset_id: props.data.sample_asset_id,
    }) === null,
)

function setMode(mode: 'design' | 'clone'): void {
  formError.value = ''
  emit('form-change', { mode })
}

function onSampleFile(file: File): void {
  const invalid = validateSampleFile(file)
  if (invalid) {
    formError.value = invalid
    return
  }
  formError.value = ''
  sampleName.value = file.name
  uploadingSample.value = true
  emit('sample-file', file)
  // 上传的成败由编辑器回写 sample_asset_id / 轻提示;这里按时限复位
  // 在途位,失败时按钮不永久禁用(10MB 上限内上传普遍秒级)。
  window.setTimeout(() => {
    uploadingSample.value = false
  }, 15_000)
}

/** 上传完成由编辑器回写 data.sample_asset_id;上传失败同样经编辑器提示,
 * 这里只对 chip 显示做本地兜底。 */
function clearSample(): void {
  sampleName.value = ''
  emit('form-change', { sample_asset_id: 0 })
}

function submitForm(): void {
  const invalid = validateVoiceForm({
    mode: props.data.mode,
    name: props.data.name,
    target_model: props.data.target_model ?? '',
    voice_prompt: props.data.voice_prompt,
    preview_text: props.data.preview_text ?? '',
    sample_asset_id: props.data.sample_asset_id,
  })
  if (invalid) {
    formError.value = invalid
    return
  }
  formError.value = ''
  emit('submit')
}

/** 样本 chip 的临时显示名:本机上传在途/刚完成时用文件名,素材库挑选
 * 与重开画布后退化为素材 id。 */
const sampleName = ref('')

// ---- 台词合成(台账卡上的内联表单)----

const speechOpen = ref(false)
const speechText = ref('')
const speechInstruction = ref('')
const speechError = ref('')

const speechChars = computed(() => [...speechText.value].length)
const speechEstimate = computed(() => {
  const model = ledger.value?.target_model ?? props.data.target_model ?? ''
  return estimateSpeechCNY(speechText.value, speechInstruction.value, props.audioPrices[model])
})

const canSpeech = computed(
  () => validateSpeechForm(speechText.value, speechInstruction.value) === null,
)

function toggleSpeech(): void {
  speechOpen.value = !speechOpen.value
  speechError.value = ''
}

function submitSpeech(): void {
  const invalid = validateSpeechForm(speechText.value, speechInstruction.value)
  if (invalid) {
    speechError.value = invalid
    return
  }
  speechError.value = ''
  emit('speech', { text: speechText.value, instruction: speechInstruction.value.trim() })
  speechOpen.value = false
  speechText.value = ''
  speechInstruction.value = ''
}

// ---- 试听 ----

const previewURL = computed(() =>
  ledger.value && ledger.value.preview_asset_id > 0
    ? assetContentURL(ledger.value.preview_asset_id)
    : '',
)
const previewBroken = ref(false)
</script>

<template>
  <div
    class="node voice"
    :class="connectState ? `connect-${connectState}` : undefined"
    :data-state="ledger ? (ledger.status === 'OK' ? 'ok' : badge.tone) : 'form'"
  >
    <header>
      音色
      <span
        v-if="ledger || inFlight || taskStatus === 'failed'"
        class="badge"
        :class="inFlight ? 'deploying' : badge.tone"
      >{{ inFlight ? '部署中' : badge.label }}</span>
    </header>

    <!-- 创建任务在途:表单冻结为状态卡(关闭页面不丢,worker 在服务端跑)。 -->
    <div
      v-if="inFlight"
      class="placeholder working"
    >
      <span class="pill">{{ taskStatus === 'queued' ? '排队中…' : '上游部署中…' }}</span>
      <small>{{ data.name || '音色' }} · {{ data.target_model }}</small>
    </div>

    <!-- 失败:原因 + 原地重试(带句柄续查,不重复创建)。 -->
    <div
      v-else-if="taskStatus === 'failed'"
      class="failed"
      role="alert"
    >
      <p class="error">
        {{ task?.error || '音色创建失败' }}
      </p>
      <button
        class="action"
        type="button"
        :disabled="retrying"
        @click="emit('retry')"
      >
        {{ retrying ? '重试中…' : '重试' }}
      </button>
    </div>

    <!-- 台账卡:已建成(或目录同步到行)的状态面。 -->
    <template v-else-if="ledger">
      <div class="ledger-name">
        {{ ledger.name }}
      </div>
      <div class="meta">
        {{ ledger.target_model }}
        <template v-if="ledger.source_mode === 'clone'">
          · 克隆
        </template>
      </div>
      <audio
        v-if="previewURL && !previewBroken"
        controls
        preload="none"
        :src="previewURL"
        @error="previewBroken = true"
      />
      <div
        v-else-if="ledger.status === 'OK'"
        class="preview-fallback"
      >
        暂无试听
      </div>

      <!-- 台词合成入口(消费闭环):台词 + 可选演绎指令 + 字符预估价。 -->
      <div
        v-if="ledger.status === 'OK'"
        class="speech"
      >
        <button
          class="action"
          type="button"
          @click="toggleSpeech"
        >
          {{ speechOpen ? '收起' : '合成台词' }}
        </button>
        <div
          v-if="speechOpen"
          class="speech-form"
        >
          <textarea
            v-model="speechText"
            rows="3"
            :maxlength="600"
            placeholder="输入台词(≤600 字符,按字符计费)"
            aria-label="台词"
          />
          <input
            v-model="speechInstruction"
            :maxlength="500"
            placeholder="演绎指令(可选,如:用河南话,带笑意)"
            aria-label="演绎指令"
          >
          <div class="form-foot">
            <span class="estimate">
              {{ speechChars }} 字符<template v-if="speechEstimate !== null"> · 约 {{ fmtCNY(speechEstimate) }}</template>
            </span>
            <button
              class="action primary"
              type="button"
              :disabled="!canSpeech"
              title="先落右侧音频节点与连线,再提交合成任务"
              @click="submitSpeech"
            >
              生成
            </button>
          </div>
        </div>
      </div>

      <div class="card-foot">
        <button
          class="action danger"
          type="button"
          :disabled="deleting"
          :title="ledger.status === 'OK' ? '从上游与台账删除该音色' : '删除该音色'"
          @click="emit('delete-voice')"
        >
          {{ deleting ? '删除中…' : '删除音色' }}
        </button>
      </div>
    </template>

    <!-- 创建表单(内联卡上,57 号票定案)。 -->
    <template v-else>
      <div
        class="mode-row"
        role="tablist"
        aria-label="创建模式"
      >
        <button
          :class="{ on: data.mode === 'design' }"
          type="button"
          role="tab"
          :aria-selected="data.mode === 'design'"
          @click="setMode('design')"
        >
          设计
        </button>
        <button
          :class="{ on: data.mode === 'clone' }"
          type="button"
          role="tab"
          :aria-selected="data.mode === 'clone'"
          @click="setMode('clone')"
        >
          克隆
        </button>
      </div>

      <input
        class="field"
        :value="data.name"
        :maxlength="255"
        placeholder="音色名"
        aria-label="音色名"
        @input="emit('form-change', { name: ($event.target as HTMLInputElement).value })"
      >

      <template v-if="data.mode === 'design'">
        <textarea
          class="field"
          rows="3"
          :value="data.voice_prompt"
          :maxlength="500"
          placeholder="音色描述:音色、语气、语速……(仅中英,≤500 字)"
          aria-label="音色描述"
          @input="emit('form-change', { voice_prompt: ($event.target as HTMLTextAreaElement).value })"
        />
        <div class="rune-count">
          {{ promptRunes }}/500
        </div>
      </template>

      <template v-else>
        <div class="sample">
          <template v-if="data.sample_asset_id">
            <span
              class="chip"
              title="已选声音样本"
            >
              {{ sampleName || `素材 #${data.sample_asset_id}` }}
              <button
                class="chip-x"
                type="button"
                aria-label="移除样本"
                @click="clearSample"
              >×</button>
            </span>
          </template>
          <template v-else>
            <button
              class="action"
              type="button"
              :disabled="uploadingSample"
              @click="pickSampleFile"
            >
              {{ uploadingSample ? '上传中…' : '本机上传' }}
            </button>
            <button
              class="action"
              type="button"
              @click="emit('pick-sample')"
            >
              素材库挑选
            </button>
            <input
              class="sample-file"
              type="file"
              accept="audio/*"
              aria-label="声音样本文件"
              @change="(e) => { const f = (e.target as HTMLInputElement).files?.[0]; if (f) onSampleFile(f); (e.target as HTMLInputElement).value = '' }"
            >
          </template>
        </div>
        <p class="hint">
          10–20 秒清晰人声,WAV/MP3/M4A,≤10MB
        </p>
      </template>

      <textarea
        class="field"
        rows="2"
        :value="data.preview_text"
        :maxlength="200"
        :placeholder="`试听文本(${MIN_PREVIEW_TEXT_RUNES}–${MAX_PREVIEW_TEXT_RUNES} 字)`"
        aria-label="试听文本"
        @input="emit('form-change', { preview_text: ($event.target as HTMLTextAreaElement).value })"
      />
      <div class="rune-count">
        {{ previewRunes }}/{{ MAX_PREVIEW_TEXT_RUNES }}
      </div>

      <select
        class="field"
        :value="data.target_model ?? ''"
        aria-label="合成模型"
        @change="emit('form-change', { target_model: ($event.target as HTMLSelectElement).value })"
      >
        <option
          value=""
          disabled
        >
          选择合成模型
        </option>
        <option
          v-for="m in audioModels"
          :key="m"
          :value="m"
        >
          {{ m }}<template
            v-if="audioPrices[m]?.cny_per_10k_chars != null"
          >
            · {{ fmtCNY(audioPrices[m].cny_per_10k_chars) }}/万字符
          </template>
        </option>
      </select>

      <p
        v-if="formError"
        class="form-error"
        role="alert"
      >
        {{ formError }}
      </p>

      <div class="form-foot">
        <span
          class="estimate"
          :title="selectedModelPrice ? undefined : '模型目录未带价,以管理端配价为准'"
        >
          {{ createEstimate }}
        </span>
        <button
          class="action primary"
          type="button"
          :disabled="!canSubmit"
          @click="submitForm"
        >
          创建音色
        </button>
      </div>
    </template>

    <NodePorts
      :sides="['right']"
      @start="emit('connect-start', $event)"
    />
    <Handle
      type="source"
      :position="Position.Right"
    />
  </div>
</template>

<style scoped>
.node {
  width: 340px;
  background: rgba(var(--surface-card-rgb), 0.92);
  border: 1px solid rgba(var(--teal-light-rgb), 0.4);
  border-radius: var(--r-xl);
  padding: 10px 12px;
  font-size: var(--fs-body);
  display: grid;
  gap: 8px;
}

header {
  font-size: var(--fs-label);
  font-weight: 600;
  color: var(--teal-light);
  display: flex;
  align-items: center;
  gap: 8px;
}

.badge {
  border-radius: var(--r-pill);
  padding: 1px 8px;
  font-size: var(--fs-micro);
  font-weight: 500;
}

.badge.deploying {
  background: rgba(var(--amber-rgb), 0.16);
  color: var(--amber);
}

.badge.ok {
  background: rgba(var(--success-rgb), 0.16);
  color: var(--success);
}

.badge.rejected {
  background: rgba(var(--red-signal-rgb), 0.16);
  color: var(--red-signal);
}

.ledger-name {
  font-size: var(--fs-body);
  font-weight: 600;
  color: inherit;
}

.meta {
  color: var(--text-muted);
  font-size: var(--fs-label);
}

audio {
  width: 100%;
  height: 34px;
  display: block;
}

.preview-fallback {
  color: var(--text-faint);
  font-size: var(--fs-label);
}

.mode-row {
  display: flex;
  gap: 6px;
}

.mode-row button {
  flex: 1;
  border: 1px solid var(--line-control);
  background: transparent;
  color: var(--text-muted);
  border-radius: var(--r-md);
  padding: 5px 0;
  cursor: pointer;
  font-size: var(--fs-label);
}

.mode-row button.on {
  border-color: rgba(var(--teal-light-rgb), 0.6);
  background: rgba(var(--teal-light-rgb), 0.12);
  color: var(--teal-light);
}

.field {
  width: 100%;
  box-sizing: border-box;
  background: var(--wash-soft);
  border: 1px solid var(--line-control);
  border-radius: var(--r-md);
  padding: 7px 9px;
  color: inherit;
  font-size: var(--fs-body);
  font-family: inherit;
  resize: vertical;
}

.field:focus {
  outline: none;
  border-color: rgba(var(--teal-light-rgb), 0.6);
}

.field::placeholder {
  color: var(--text-faint);
}

select.field option {
  background: rgba(var(--surface-card-rgb), 1);
}

.rune-count {
  margin-top: -4px;
  text-align: right;
  color: var(--text-faint);
  font-size: var(--fs-micro);
}

.sample {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
  align-items: center;
}

.sample-file {
  display: none;
}

.chip {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  border-radius: var(--r-pill);
  background: rgba(var(--teal-light-rgb), 0.12);
  color: var(--teal-light);
  padding: 3px 10px;
  font-size: var(--fs-label);
  max-width: 100%;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.chip-x {
  border: none;
  background: transparent;
  color: inherit;
  cursor: pointer;
  padding: 0 2px;
  font-size: var(--fs-body);
}

.hint {
  margin: -2px 0 0;
  color: var(--text-faint);
  font-size: var(--fs-micro);
}

.action {
  border: none;
  border-radius: var(--r-md);
  background: var(--wash-fill);
  color: var(--estimate-ink);
  padding: 6px 12px;
  cursor: pointer;
  font-size: var(--fs-label);
}

.action:disabled {
  opacity: 0.55;
  cursor: default;
}

.action.primary {
  background: rgba(var(--teal-light-rgb), 0.18);
  color: var(--teal-light);
}

.action.danger {
  background: rgba(var(--red-signal-rgb), 0.12);
  color: var(--red-signal);
}

.form-foot {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
}

.estimate {
  color: var(--text-muted);
  font-size: var(--fs-micro);
}

.form-error {
  margin: 0;
  color: var(--danger);
  font-size: var(--fs-label);
}

.speech-form {
  display: grid;
  gap: 6px;
}

.speech-form textarea,
.speech-form input {
  width: 100%;
  box-sizing: border-box;
  background: var(--wash-soft);
  border: 1px solid var(--line-control);
  border-radius: var(--r-md);
  padding: 7px 9px;
  color: inherit;
  font-size: var(--fs-label);
  font-family: inherit;
  resize: vertical;
}

.speech-form textarea:focus,
.speech-form input:focus {
  outline: none;
  border-color: rgba(var(--teal-light-rgb), 0.6);
}

.card-foot {
  display: flex;
  justify-content: flex-end;
}

.placeholder {
  display: grid;
  gap: 4px;
  justify-items: center;
  padding: 20px 8px;
  border: 1px dashed var(--line-action);
  border-radius: var(--r-md);
  color: var(--text-muted);
}

.placeholder small {
  font-size: var(--fs-micro);
}

.pill {
  border-radius: var(--r-pill);
  background: rgba(var(--amber-rgb), 0.14);
  color: var(--amber);
  padding: 3px 10px;
  font-size: var(--fs-label);
}

.failed {
  display: grid;
  gap: 6px;
}

.error {
  margin: 0;
  color: var(--danger);
  font-size: var(--fs-label);
  word-break: break-word;
  display: -webkit-box;
  -webkit-line-clamp: 3;
  -webkit-box-orient: vertical;
  overflow: hidden;
}
</style>
