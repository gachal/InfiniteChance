<script setup lang="ts">
// 音频节点(57 号票,第十一种节点类型;60 号票增配乐创作态;62 号票增
// 人声歌曲开关;61 号票增音效创作态)四种形态 ——
// · 创作态(工具栏「配乐」/「音效」落的空节点):内联表单(配乐 = 「人声
//   歌曲」开关 + 开关开才显示的歌词多行框 + 模型下拉 + 风格描述 + 按次
//   预估价 + 提交;音效 = 按 source==='sfx' 分叉的模型下拉(¥/秒)+ 描述 +
//   秒数 1–30 + 按秒预估),与音色节点同款范式,不进生成对话框;提交 =
//   kind=music / kind=sfx 的 canvas_task 绑本节点,表单事实随整图 JSON
//   持久化。
// · 在途/失败:状态徽章(排队/生成中)+ 失败原因 + 原地重试。
// · 产物:内联 <audio controls> + 元数据区(27 号票产物-元数据配对,
//   台词摘录或配乐/歌曲/音效描述按 source 判别)。素材面板/生成记录插入行
//   (无任务)与台词合成产物走同款产物卡面。
// 只渲染左把手(voice → audio 合成来源边;配乐不作任何边的源),无灯箱。
import { computed, ref, watch } from 'vue'
import { Handle, Position } from '@vue-flow/core'

import type { CanvasTask, ModelPriceSummary } from '@infinitechance/api'

import type { AudioNodeData } from '../../graph'
import type { ConnectSide, ConnectState } from '../../composables/useConnection'
import { estimateMusicCNY, fmtCNY, MAX_LYRICS_CHARS, MAX_MUSIC_PROMPT_RUNES, validateMusicForm, type MusicFormInput } from '../../music'
import { estimateSfxCNY, MAX_SFX_TEXT_RUNES, SFX_MAX_SECONDS, SFX_MIN_SECONDS, validateSfxForm, type SfxFormInput } from '../../sfx'
import { assetContentURL } from '../../thumbnail'
import NodePorts from './NodePorts.vue'
import MediaPrompt from './MediaPrompt.vue'

const props = defineProps<{
  id: string
  type: string
  data: AudioNodeData
  /** 绑定到本节点的生成任务(编辑器轮询保持最新;null = 面板插入行/
   * 未提交的创作态节点)。 */
  task?: CanvasTask | null
  /** 重试请求在途时禁用重试按钮。 */
  retrying: boolean
  /** /music-models 目录:按次配乐模型与价格(配乐创作态表单下拉 + 预估)。 */
  musicModels: string[]
  musicPrices: Record<string, ModelPriceSummary>
  /** 配乐提交请求在途(编辑器级,禁提交按钮)。 */
  musicSubmitting?: boolean
  /** /sfx-models 目录:按秒音效模型与价格(音效创作态表单下拉 + 预估)。 */
  sfxModels: string[]
  sfxPrices: Record<string, ModelPriceSummary>
  /** 音效提交请求在途(编辑器级,禁提交按钮)。 */
  sfxSubmitting?: boolean
  /** 连接态展示态(30 号票):origin/valid/dimmed,缺省 = 正常渲染。 */
  connectState?: ConnectState
}>()

const emit = defineEmits<{
  retry: []
  /** 创作态表单事实变更(逐键上抛,编辑器 updateNodeData 浅合并落盘)。 */
  'form-change': [patch: Partial<AudioNodeData>]
  /** 创作态提交(以当前 data 为准;校验在节点侧,提示本地展示)。 */
  submit: []
  /** 左 + 接上游(voice → audio 合成来源)。 */
  'connect-start': [side: ConnectSide]
}>()

const status = computed(() => props.task?.status)
// 创作态 = 没有产物、没有任务,工具栏「配乐/音效」落下的空节点。
const creating = computed(
  () => !(props.data.asset_id > 0) && !status.value,
)
// 音效创作态分叉(61 号票):工具栏「音效」入口落 source:'sfx'。
const sfxMode = computed(() => props.data.source === 'sfx')

/** 元数据摘录(卡面一行):配乐/歌曲/音效显描述,台词显摘录,完整内容进
 * MediaPrompt 配对区。 */
const musicish = computed(() =>
  props.data.source === 'music' || props.data.source === 'song' || props.data.source === 'sfx',
)
const metaLine = computed(() => musicish.value ? (props.data.prompt ?? '') : (props.data.text ?? ''))
const excerpt = computed(() => {
  const text = metaLine.value
  return text.length > 42 ? `${text.slice(0, 42)}…` : text
})
const excerptTitle = computed(() => musicish.value ? props.data.prompt : props.data.text)

// 内容寻址地址按素材 id 拼装;asset_id 变化(重新生成/换插)即复位重试。
const src = computed(() => (props.data.asset_id > 0 ? assetContentURL(props.data.asset_id) : ''))
const loadFailed = ref(false)
watch(
  () => props.data.asset_id,
  () => {
    loadFailed.value = false
  },
)

function onAudioError(): void {
  loadFailed.value = true
}

// ---- 创作态表单(受控:所有编辑 emit form-change)----

const formError = ref('')
const promptRunes = computed(() => [...(props.data.prompt ?? '').trim()].length)

// 「人声歌曲」开关(62 号票):默认关 = 纯音乐(60 号票行为不变),开 =
// 人声歌曲(instrumental=false,歌词选填)。source:'song' 兼作意图标记:
// 提交失败重开时开关回到 on;关掉时清标记(JSON 序列化丢 undefined 键)。
const songMode = ref(props.data.source === 'song' || (props.data.lyrics ?? '').trim() !== '')

function onSongToggle(on: boolean): void {
  songMode.value = on
  emit('form-change', on ? { source: 'song' } : { source: undefined })
}

const lyricsRunes = computed(() => [...(props.data.lyrics ?? '')].length)

const selectedModelPrice = computed(() => {
  const m = props.data.model
  return m ? props.musicPrices[m] : undefined
})

const createEstimate = computed(() => {
  const cny = estimateMusicCNY(selectedModelPrice.value)
  if (cny !== null) {
    return `一次 ${fmtCNY(cny)}(整曲)`
  }
  return '按次计费,以管理端配价为准'
})

// 表单输入:开关关 = 纯音乐(instrumental=true,歌词不随表单,即便节点
// 上留着旧歌词也不送 —— 互斥校验在两侧都成立)。
const formInput = computed<MusicFormInput>(() => ({
  prompt: props.data.prompt ?? '',
  model: props.data.model ?? '',
  instrumental: !songMode.value,
  lyrics: songMode.value ? props.data.lyrics ?? '' : '',
}))

const canSubmit = computed(() =>
  sfxMode.value
    ? validateSfxForm(sfxFormInput.value) === null
    : validateMusicForm(formInput.value) === null,
)

// ---- 音效创作态(61 号票):描述 + 模型(¥/秒)+ 秒数 1–30 ----

const selectedSfxPrice = computed(() => {
  const m = props.data.model
  return m ? props.sfxPrices[m] : undefined
})

const sfxSeconds = computed(() => props.data.seconds ?? 0)

const sfxFormInput = computed<SfxFormInput>(() => ({
  text: props.data.prompt ?? '',
  model: props.data.model ?? '',
  seconds: sfxSeconds.value,
}))

const sfxEstimate = computed(() => {
  const cny = estimateSfxCNY(selectedSfxPrice.value, sfxSeconds.value)
  if (cny !== null) {
    return `约 ${fmtCNY(cny)}(${sfxSeconds.value} 秒)`
  }
  return '按秒计费,以管理端配价为准'
})

function submitForm(): void {
  const invalid = sfxMode.value
    ? validateSfxForm(sfxFormInput.value)
    : validateMusicForm(formInput.value)
  if (invalid) {
    formError.value = invalid
    return
  }
  formError.value = ''
  emit('submit')
}
</script>

<template>
  <div
    class="node audio"
    :class="connectState ? `connect-${connectState}` : undefined"
    :data-task="status ?? 'none'"
  >
    <header>音频</header>

    <!-- 创作态内联表单(60 号票:配乐的唯一生成入口,工具栏「配乐」落下;
         62 号票:「人声歌曲」开关 + 开关开才显示的歌词多行框;61 号票:
         source='sfx' 分叉音效表单 —— 模型(¥/秒)+ 描述 + 秒数 1–30)。 -->
    <template v-if="creating && sfxMode">
      <textarea
        class="field"
        rows="3"
        :value="data.prompt"
        :maxlength="MAX_SFX_TEXT_RUNES"
        placeholder="音效描述:拟声对象、情绪、环境……(≤2000 字)"
        aria-label="音效描述"
        @input="emit('form-change', { prompt: ($event.target as HTMLTextAreaElement).value })"
      />
      <div class="rune-count">
        {{ promptRunes }}/{{ MAX_SFX_TEXT_RUNES }}
      </div>
      <select
        class="field"
        :value="data.model ?? ''"
        aria-label="音效模型"
        @change="emit('form-change', { model: ($event.target as HTMLSelectElement).value })"
      >
        <option
          value=""
          disabled
        >
          选择音效模型
        </option>
        <option
          v-for="m in sfxModels"
          :key="m"
          :value="m"
        >
          {{ m }}<template v-if="sfxPrices[m]?.cny_per_call != null">
            · {{ fmtCNY(sfxPrices[m].cny_per_call!) }}/秒
          </template>
        </option>
      </select>
      <label class="seconds-row">
        <span>时长(秒)</span>
        <input
          class="field seconds-input"
          type="number"
          :min="SFX_MIN_SECONDS"
          :max="SFX_MAX_SECONDS"
          step="1"
          :value="data.seconds ?? ''"
          aria-label="音效时长(秒)"
          @input="emit('form-change', { seconds: Number(($event.target as HTMLInputElement).value) })"
        >
        <small>{{ SFX_MIN_SECONDS }}–{{ SFX_MAX_SECONDS }} 整数</small>
      </label>
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
          :title="selectedSfxPrice ? undefined : '模型目录未带价,以管理端配价为准'"
        >
          {{ sfxEstimate }}
        </span>
        <button
          class="action primary"
          type="button"
          :disabled="!canSubmit || sfxSubmitting"
          @click="submitForm"
        >
          {{ sfxSubmitting ? '提交中…' : '生成音效' }}
        </button>
      </div>
    </template>

    <template v-else-if="creating">
      <label class="song-toggle">
        <input
          type="checkbox"
          :checked="songMode"
          @change="onSongToggle(($event.target as HTMLInputElement).checked)"
        >
        <span>人声歌曲</span>
        <small>{{ songMode ? '带歌词人声;歌词选填,留空自动写词' : '纯音乐(默认)' }}</small>
      </label>
      <textarea
        v-if="songMode"
        class="field"
        rows="4"
        :value="data.lyrics"
        :maxlength="MAX_LYRICS_CHARS"
        placeholder="歌词(选填,≤3500 字;留空由模型自动写词)"
        aria-label="歌曲歌词"
        @input="emit('form-change', { lyrics: ($event.target as HTMLTextAreaElement).value })"
      />
      <div
        v-if="songMode"
        class="rune-count"
      >
        {{ lyricsRunes }}/{{ MAX_LYRICS_CHARS }}
      </div>
      <textarea
        class="field"
        rows="3"
        :value="data.prompt"
        :maxlength="MAX_MUSIC_PROMPT_RUNES"
        placeholder="风格描述:情绪、节奏、乐器、场景……(≤2000 字)"
        aria-label="配乐风格描述"
        @input="emit('form-change', { prompt: ($event.target as HTMLTextAreaElement).value })"
      />
      <div class="rune-count">
        {{ promptRunes }}/{{ MAX_MUSIC_PROMPT_RUNES }}
      </div>
      <select
        class="field"
        :value="data.model ?? ''"
        aria-label="配乐模型"
        @change="emit('form-change', { model: ($event.target as HTMLSelectElement).value })"
      >
        <option
          value=""
          disabled
        >
          选择配乐模型
        </option>
        <option
          v-for="m in musicModels"
          :key="m"
          :value="m"
        >
          {{ m }}<template v-if="musicPrices[m]?.cny_per_call != null">
            · {{ fmtCNY(musicPrices[m].cny_per_call!) }}/次
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
          :disabled="!canSubmit || musicSubmitting"
          @click="submitForm"
        >
          {{ musicSubmitting ? '提交中…' : (songMode ? '生成歌曲' : '生成配乐') }}
        </button>
      </div>
    </template>

    <div
      v-else-if="status === 'queued' || status === 'running'"
      class="placeholder working"
    >
      <span class="pill">{{ status === 'queued' ? '排队中…' : '生成中…' }}</span>
      <small>任务在服务端进行,关闭页面不丢失</small>
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
    <template v-else-if="src && !loadFailed">
      <audio
        controls
        preload="metadata"
        :src="src"
        @error="onAudioError"
      />
      <p
        v-if="excerpt"
        class="excerpt"
        :title="excerptTitle"
      >
        {{ excerpt }}
      </p>
      <MediaPrompt
        v-if="data.model"
        :prompt="musicish ? data.prompt ?? '' : data.text ?? ''"
        :model="data.model"
      />
    </template>
    <div
      v-else-if="src && loadFailed"
      class="placeholder missing"
    >
      <span>素材不可用</span>
      <small>原素材可能已被删除</small>
    </div>
    <div
      v-else
      class="placeholder"
    >
      <span>音频占位</span>
      <small>台词合成或配乐结果将出现在这里</small>
    </div>

    <!-- 57 号票:audio 只作为连线目标,只渲染左 +。 -->
    <NodePorts
      :sides="['left']"
      @start="emit('connect-start', $event)"
    />
    <Handle
      type="target"
      :position="Position.Left"
    />
  </div>
</template>

<style scoped>
.node {
  width: 300px;
  background: rgba(var(--surface-card-rgb), 0.92);
  border: 1px solid rgba(var(--violet-rgb), 0.45);
  border-radius: var(--r-xl);
  padding: 10px 12px;
  font-size: var(--fs-body);
  display: grid;
  gap: 8px;
}

header {
  font-size: var(--fs-label);
  font-weight: 600;
  color: var(--violet);
}

audio {
  width: 100%;
  height: 36px;
  display: block;
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
  border-color: rgba(var(--violet-rgb), 0.6);
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

/* 音效秒数行(61 号票):标签 + 数字输入 + 域提示一行排开。 */
.seconds-row {
  display: flex;
  align-items: center;
  gap: 8px;
  color: var(--text-muted);
  font-size: var(--fs-label);
}

.seconds-row span {
  font-weight: 600;
  color: var(--violet);
  white-space: nowrap;
}

.seconds-input {
  width: 84px;
  flex-shrink: 0;
}

.seconds-row small {
  color: var(--text-faint);
  font-size: var(--fs-micro);
}

/* 「人声歌曲」开关(62 号票):一行开关 + 随行小注。 */
.song-toggle {
  display: flex;
  align-items: baseline;
  gap: 6px;
  cursor: pointer;
  user-select: none;
}

.song-toggle input {
  align-self: center;
  accent-color: var(--violet);
}

.song-toggle span {
  font-size: var(--fs-label);
  font-weight: 600;
  color: var(--violet);
}

.song-toggle small {
  color: var(--text-faint);
  font-size: var(--fs-micro);
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
  background: rgba(var(--violet-rgb), 0.18);
  color: var(--violet);
}

.form-error {
  margin: 0;
  color: var(--danger);
  font-size: var(--fs-label);
}

.excerpt {
  margin: 8px 0 0;
  color: var(--text-muted);
  font-size: var(--fs-label);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.placeholder {
  display: grid;
  gap: 4px;
  justify-items: center;
  padding: 22px 8px;
  border: 1px dashed var(--line-action);
  border-radius: var(--r-md);
  color: var(--text-muted);
}

.placeholder span {
  font-weight: 600;
}

.placeholder small {
  font-size: var(--fs-micro);
}

.placeholder.missing {
  border-color: rgba(var(--danger-rgb), 0.4);
  color: var(--danger);
}

.pill {
  border-radius: var(--r-pill);
  background: rgba(var(--accent-rgb), 0.18);
  color: var(--accent);
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

.retry {
  justify-self: start;
  border: none;
  border-radius: var(--r-md);
  background: rgba(var(--accent-rgb), 0.2);
  color: var(--accent);
  padding: 6px 12px;
  cursor: pointer;
  font-size: var(--fs-label);
}

.retry:disabled {
  opacity: 0.6;
  cursor: default;
}
</style>
