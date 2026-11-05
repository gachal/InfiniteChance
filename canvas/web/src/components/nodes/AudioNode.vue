<script setup lang="ts">
// 音频节点(57 号票,第十一种节点类型;60 号票增配乐创作态;62 号票增
// 人声歌曲开关;61 号票增音效创作态;78 号票增翻唱创作态;79 号票增同曲
// 换词创作态;80 号票同曲换词加「旋律保持强度」滑杆)——
// · 创作态(右键菜单「配乐」/「音效」/「翻唱」/「同曲换词」落的空节点):
//   内联表单(配乐 = 「人声歌曲」开关 + 开关开才显示的歌词多行框 + 模型
//   下拉 + 风格描述 + 按次预估价 + 提交;音效 = 按 source==='sfx' 分叉的
//   模型下拉(¥/秒)+ 描述 + 秒数 1–30 + 按秒预估;翻唱 = 按
//   source==='cover' 分叉的参考音频 chip(必填恰好 1 条,上传 + 素材库
//   挑选双入口)+ 演绎描述 + 歌词选填(带「英→中」翻译按钮)+ 模型下拉
//   + 按次预估;同曲换词 = 按 source==='relyric' 分叉的参考曲 chip(必填
//   恰好 1 条)+ 风格描述选填 + 新歌词必填(带翻译按钮)+ 旋律保持强度
//   滑杆(0.3–0.9 步进 0.05 缺省 0.55,越高越贴原曲旋律)+ 模型下拉 +
//   按次预估),与音色节点同款范式,不进生成对话框;提交 = kind=music /
//   kind=sfx 的 canvas_task 绑本节点,表单事实随整图 JSON 持久化。
// · 在途/失败:状态徽章(排队/生成中)+ 失败原因 + 原地重试。
// · 产物:内联 <audio controls> + 元数据区(27 号票产物-元数据配对,
//   台词摘录或配乐/歌曲/音效/翻唱/同曲换词描述按 source 判别)。素材面板/
//   生成记录插入行(无任务)与台词合成产物走同款产物卡面。
// 只渲染左把手(voice → audio 合成来源边;音频生成不作任何边的源),无
// 灯箱。
import { computed, ref, watch } from 'vue'
import { Handle, Position } from '@vue-flow/core'

import type { CanvasTask, ModelPriceSummary } from '@infinitechance/api'

import type { AudioNodeData } from '../../graph'
import type { ConnectSide, ConnectState } from '../../composables/useConnection'
import { estimateMusicCNY, fmtCNY, MAX_LYRICS_CHARS, MAX_MUSIC_PROMPT_RUNES, RELYRIC_STRENGTH_DEFAULT, RELYRIC_STRENGTH_MAX, RELYRIC_STRENGTH_MIN, RELYRIC_STRENGTH_STEP, validateCoverForm, validateMusicForm, validateRelyricForm, type CoverFormInput, type MusicFormInput, type RelyricFormInput } from '../../music'
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
  /** /music-models 目录:按次配乐模型与价格(配乐/翻唱/同曲换词创作态
   * 表单下拉 + 预估;翻唱与同曲换词列全量目录、不做 cover/relyric 过滤)。 */
  musicModels: string[]
  musicPrices: Record<string, ModelPriceSummary>
  /** 配乐提交请求在途(编辑器级,禁提交按钮)。 */
  musicSubmitting?: boolean
  /** /sfx-models 目录:按秒音效模型与价格(音效创作态表单下拉 + 预估)。 */
  sfxModels: string[]
  sfxPrices: Record<string, ModelPriceSummary>
  /** 音效提交请求在途(编辑器级,禁提交按钮)。 */
  sfxSubmitting?: boolean
  /** 翻唱提交请求在途(编辑器级,禁提交按钮)。 */
  coverSubmitting?: boolean
  /** 翻唱参考音频上传在途(编辑器级,禁上传按钮)。 */
  coverRefUploading?: boolean
  /** 同曲换词提交请求在途(编辑器级,禁提交按钮;79 号票克隆 cover 旗标)。 */
  relyricSubmitting?: boolean
  /** 同曲换词参考曲上传在途(编辑器级,禁上传按钮)。 */
  relyricRefUploading?: boolean
  /** 「英→中」翻译请求在途(编辑器级,禁翻译按钮;翻唱与同曲换词共用)。 */
  translating?: boolean
  /** 连接态展示态(30 号票):origin/valid/dimmed,缺省 = 正常渲染。 */
  connectState?: ConnectState
}>()

const emit = defineEmits<{
  retry: []
  /** 创作态表单事实变更(逐键上抛,编辑器 updateNodeData 浅合并落盘)。 */
  'form-change': [patch: Partial<AudioNodeData>]
  /** 创作态提交(以当前 data 为准;校验在节点侧,提示本地展示)。 */
  submit: []
  /** 翻唱参考音频:本机上传(kind=audio 经 /api/assets/upload 入库)。 */
  'ref-file': [file: File]
  /** 翻唱参考音频:素材库挑选(AssetPicker audio 过滤)。 */
  'pick-ref': []
  /** 「英→中」翻译(歌词框非空才可用;结果由编辑器回填替换歌词框)。 */
  translate: []
  /** 左 + 接上游(voice → audio 合成来源)。 */
  'connect-start': [side: ConnectSide]
}>()

const status = computed(() => props.task?.status)
// 创作态 = 没有产物、没有任务,右键菜单「配乐/音效/翻唱」条目落下的空节点。
const creating = computed(
  () => !(props.data.asset_id > 0) && !status.value,
)
// 音效创作态分叉(61 号票):工具栏「音效」入口落 source:'sfx'。
const sfxMode = computed(() => props.data.source === 'sfx')
// 翻唱创作态分叉(78 号票):「翻唱」入口落 source:'cover'(创作意图
// 标记兼产物来源,composer 对账不覆盖)。
const coverMode = computed(() => props.data.source === 'cover')
// 同曲换词创作态分叉(79 号票):「同曲换词」入口落 source:'relyric'
// (创作意图标记兼产物来源,composer 对账不覆盖)。
const relyricMode = computed(() => props.data.source === 'relyric')

/** 元数据摘录(卡面一行):配乐/歌曲/音效/翻唱/同曲换词显描述,台词显
 * 摘录,完整内容进 MediaPrompt 配对区(cover 是 78 号票漏加、79 号票随
 * 迁补齐;漏加会让产物卡回退台词摘录形态)。 */
const musicish = computed(() =>
  props.data.source === 'music' || props.data.source === 'song' || props.data.source === 'sfx'
    || props.data.source === 'cover' || props.data.source === 'relyric',
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

const canSubmit = computed(() => {
  if (coverMode.value) {
    return validateCoverForm(coverFormInput.value) === null
  }
  if (relyricMode.value) {
    return validateRelyricForm(relyricFormInput.value) === null
  }
  return sfxMode.value
    ? validateSfxForm(sfxFormInput.value) === null
    : validateMusicForm(formInput.value) === null
})

// ---- 翻唱创作态(78 号票):参考 chip + 演绎描述 + 歌词(可翻译)+ 模型 ----

const refAssetId = computed(() => props.data.ref_asset_id ?? 0)

const coverFormInput = computed<CoverFormInput>(() => ({
  refAssetId: refAssetId.value,
  prompt: props.data.prompt ?? '',
  model: props.data.model ?? '',
  lyrics: props.data.lyrics ?? '',
}))

const selectedCoverPrice = computed(() => {
  const m = props.data.model
  return m ? props.musicPrices[m] : undefined
})

const coverEstimate = computed(() => {
  const cny = estimateMusicCNY(selectedCoverPrice.value)
  if (cny !== null) {
    return `一次 ${fmtCNY(cny)}(整曲)`
  }
  return '按次计费,以管理端配价为准'
})

const lyricsRunesCover = computed(() => [...(props.data.lyrics ?? '')].length)
const lyricsEmpty = computed(() => (props.data.lyrics ?? '').trim() === '')

// cover/relyric 创作态的模型缺省 list[0](78 号票:目录全量不过滤,缺省
// 首项;79 号票同曲换词同款)—— 目录就位且节点尚未选模型时落一次,用户
// 改选后不再动。
watch(
  () => [(coverMode.value || relyricMode.value), props.musicModels] as const,
  ([isRefCreationMode, models]) => {
    if (isRefCreationMode && !(props.data.model ?? '') && models.length > 0) {
      emit('form-change', { model: models[0]! })
    }
  },
  { immediate: true },
)

function onRefFile(file: File): void {
  if (props.coverRefUploading) {
    return
  }
  emit('ref-file', file)
}

// 本机上传的隐藏 file input(57 号票克隆样本同款链)。
const refInput = ref<HTMLInputElement | null>(null)

function pickRefFile(): void {
  refInput.value?.click()
}

function clearRef(): void {
  emit('form-change', { ref_asset_id: 0 })
}

// ---- 同曲换词创作态(79 号票):参考曲 chip + 风格选填 + 新歌词必填
// (可翻译)+ 旋律保持强度滑杆(80 号票)+ 模型;参考 chip 与上传/挑选
// 链与 cover 同形(refAssetId/onRefFile/clearRef 按 ref_asset_id 操作,
// 不分 source)。 ----

const relyricFormInput = computed<RelyricFormInput>(() => ({
  refAssetId: refAssetId.value,
  prompt: props.data.prompt ?? '',
  model: props.data.model ?? '',
  lyrics: props.data.lyrics ?? '',
  strength: props.data.strength ?? RELYRIC_STRENGTH_DEFAULT,
}))

// 旋律保持强度(80 号票):滑杆绑定值以缺省常量兜底(节点未物化也显示
// 0.55,与提交 payload 的实际发送值一致);拖动即时回写节点 data ——
// 草稿保留纪律(27 号票起),等待期重开节点表单不弹回。range 输入的
// min/max/step 由元素自身钳制,handler 只防非有限值。
const relyricStrength = computed(() => props.data.strength ?? RELYRIC_STRENGTH_DEFAULT)

function onRelyricStrength(raw: string): void {
  const v = Number.parseFloat(raw)
  if (!Number.isFinite(v)) {
    return
  }
  emit('form-change', { strength: v })
}

// 缺省强度物化(80 号票):进入 relyric 创作态且节点尚未携带 strength 时
// 落缺省 0.55 —— 创作事实与实际发送同形(用户不动滑杆,节点也记录实际
// 送出的值);产物节点(非创作态)不物化,不虚构旧任务没送过的事实。
watch(
  () => creating.value && relyricMode.value,
  (isRelyricCreating) => {
    if (isRelyricCreating && !((props.data.strength ?? 0) > 0)) {
      emit('form-change', { strength: RELYRIC_STRENGTH_DEFAULT })
    }
  },
  { immediate: true },
)

const selectedRelyricPrice = computed(() => {
  const m = props.data.model
  return m ? props.musicPrices[m] : undefined
})

const relyricEstimate = computed(() => {
  const cny = estimateMusicCNY(selectedRelyricPrice.value)
  if (cny !== null) {
    return `一次 ${fmtCNY(cny)}(整曲)`
  }
  return '按次计费,以管理端配价为准'
})

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
  const invalid = coverMode.value
    ? validateCoverForm(coverFormInput.value)
    : relyricMode.value
      ? validateRelyricForm(relyricFormInput.value)
      : sfxMode.value
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

    <!-- 翻唱创作态(78 号票):参考音频 chip 必填恰好 1 条(本机上传 +
         素材库挑选双入口,57 号票克隆样本 chip 范式;不做大小/时长守卫),
         演绎描述必填、歌词选填(带「英→中」翻译按钮,空框禁用)、模型
         下拉列全量 /music-models 目录、按次预估。 -->
    <template v-else-if="creating && coverMode">
      <div class="sample">
        <template v-if="refAssetId > 0">
          <span
            class="chip"
            title="已选参考音频(翻唱以它为旋律/编曲基底)"
          >
            参考音频 · 素材 #{{ refAssetId }}
            <button
              class="chip-x"
              type="button"
              aria-label="移除参考音频"
              @click="clearRef"
            >×</button>
          </span>
        </template>
        <template v-else>
          <button
            class="action"
            type="button"
            :disabled="coverRefUploading"
            @click="pickRefFile"
          >
            {{ coverRefUploading ? '上传中…' : '本机上传' }}
          </button>
          <button
            class="action"
            type="button"
            @click="emit('pick-ref')"
          >
            素材库挑选
          </button>
          <input
            ref="refInput"
            class="sample-file"
            type="file"
            accept="audio/*"
            aria-label="参考音频文件"
            @change="(e) => { const f = (e.target as HTMLInputElement).files?.[0]; if (f) onRefFile(f); (e.target as HTMLInputElement).value = '' }"
          >
        </template>
      </div>
      <textarea
        class="field"
        rows="3"
        :value="data.prompt"
        :maxlength="MAX_MUSIC_PROMPT_RUNES"
        placeholder="演绎/风格描述:唱腔、编曲走向、情绪……(≤2000 字)"
        aria-label="翻唱演绎描述"
        @input="emit('form-change', { prompt: ($event.target as HTMLTextAreaElement).value })"
      />
      <div class="rune-count">
        {{ promptRunes }}/{{ MAX_MUSIC_PROMPT_RUNES }}
      </div>
      <div class="lyrics-head">
        <span class="lyrics-label">歌词(选填)</span>
        <button
          class="action translate"
          type="button"
          :disabled="lyricsEmpty || translating"
          :title="lyricsEmpty ? '先填入原歌词,再一键翻成中文' : '调翻译端点把歌词翻成中文,结果替换歌词框'"
          @click="emit('translate')"
        >
          {{ translating ? '翻译中…' : '英→中' }}
        </button>
      </div>
      <textarea
        class="field"
        rows="4"
        :value="data.lyrics"
        :maxlength="MAX_LYRICS_CHARS"
        placeholder="歌词(选填,≤3500 字;留空将由模型自动写词、非原曲歌词)"
        aria-label="翻唱歌词"
        @input="emit('form-change', { lyrics: ($event.target as HTMLTextAreaElement).value })"
      />
      <div class="rune-count">
        {{ lyricsRunesCover }}/{{ MAX_LYRICS_CHARS }}
      </div>
      <select
        class="field"
        :value="data.model ?? ''"
        aria-label="翻唱模型"
        @change="emit('form-change', { model: ($event.target as HTMLSelectElement).value })"
      >
        <option
          value=""
          disabled
        >
          选择翻唱模型
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
          :title="selectedCoverPrice ? undefined : '模型目录未带价,以管理端配价为准'"
        >
          {{ coverEstimate }}
        </span>
        <button
          class="action primary"
          type="button"
          :disabled="!canSubmit || coverSubmitting"
          @click="submitForm"
        >
          {{ coverSubmitting ? '提交中…' : '生成翻唱' }}
        </button>
      </div>
    </template>

    <!-- 同曲换词创作态(79 号票,cover 表单克隆分叉):参考曲 chip 必填
         恰好 1 条(本机上传 + 素材库挑选双入口,以参考原曲锁定旋律;不做
         大小/时长守卫,上游裁决 4xx 透出)、风格描述选填(空 = 不送)、
         新歌词必填(词是身份必需,无「自动写词」;带「英→中」翻译按钮)、
         模型下拉列全量 /music-models 目录、按次预估。 -->
    <template v-else-if="creating && relyricMode">
      <div class="sample">
        <template v-if="refAssetId > 0">
          <span
            class="chip"
            title="已选参考原曲(同曲换词以它锁定旋律,新歌词重唱)"
          >
            参考原曲 · 素材 #{{ refAssetId }}
            <button
              class="chip-x"
              type="button"
              aria-label="移除参考原曲"
              @click="clearRef"
            >×</button>
          </span>
        </template>
        <template v-else>
          <button
            class="action"
            type="button"
            :disabled="relyricRefUploading"
            @click="pickRefFile"
          >
            {{ relyricRefUploading ? '上传中…' : '本机上传' }}
          </button>
          <button
            class="action"
            type="button"
            @click="emit('pick-ref')"
          >
            素材库挑选
          </button>
          <input
            ref="refInput"
            class="sample-file"
            type="file"
            accept="audio/*"
            aria-label="参考原曲文件"
            @change="(e) => { const f = (e.target as HTMLInputElement).files?.[0]; if (f) onRefFile(f); (e.target as HTMLInputElement).value = '' }"
          >
        </template>
      </div>
      <textarea
        class="field"
        rows="3"
        :value="data.prompt"
        :maxlength="MAX_MUSIC_PROMPT_RUNES"
        placeholder="风格描述(选填):编曲走向、情绪、唱法……(≤2000 字;留空不送)"
        aria-label="同曲换词风格描述"
        @input="emit('form-change', { prompt: ($event.target as HTMLTextAreaElement).value })"
      />
      <div class="rune-count">
        {{ promptRunes }}/{{ MAX_MUSIC_PROMPT_RUNES }}
      </div>
      <div class="lyrics-head">
        <span class="lyrics-label">新歌词(必填)</span>
        <button
          class="action translate"
          type="button"
          :disabled="lyricsEmpty || translating"
          :title="lyricsEmpty ? '先填入原歌词,再一键翻成中文' : '调翻译端点把歌词翻成中文,结果替换歌词框'"
          @click="emit('translate')"
        >
          {{ translating ? '翻译中…' : '英→中' }}
        </button>
      </div>
      <textarea
        class="field"
        rows="4"
        :value="data.lyrics"
        :maxlength="MAX_LYRICS_CHARS"
        placeholder="新歌词(必填,≤3500 字;同曲换词按新词重唱原曲旋律,不留空)"
        aria-label="同曲换词新歌词"
        @input="emit('form-change', { lyrics: ($event.target as HTMLTextAreaElement).value })"
      />
      <div class="rune-count">
        {{ lyricsRunes }}/{{ MAX_LYRICS_CHARS }}
      </div>
      <!-- 旋律保持强度滑杆(80 号票定案 5):0.3–0.9 步进 0.05,越高越贴
           原曲旋律(重组自由度越低);旁边数值实时显示,拖动即时回写节点
           data(草稿保留纪律)。音效秒数行(61 号票)同款一行三段布局。 -->
      <label class="strength-row">
        <span>旋律保持强度</span>
        <input
          class="strength-input"
          type="range"
          :min="RELYRIC_STRENGTH_MIN"
          :max="RELYRIC_STRENGTH_MAX"
          :step="RELYRIC_STRENGTH_STEP"
          :value="relyricStrength"
          aria-label="旋律保持强度"
          @input="onRelyricStrength(($event.target as HTMLInputElement).value)"
        >
        <output class="strength-value">{{ relyricStrength.toFixed(2) }}</output>
      </label>
      <small class="strength-hint">越高越贴原曲旋律(重组自由度越低)</small>
      <select
        class="field"
        :value="data.model ?? ''"
        aria-label="同曲换词模型"
        @change="emit('form-change', { model: ($event.target as HTMLSelectElement).value })"
      >
        <option
          value=""
          disabled
        >
          选择同曲换词模型
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
          :title="selectedRelyricPrice ? undefined : '模型目录未带价,以管理端配价为准'"
        >
          {{ relyricEstimate }}
        </span>
        <button
          class="action primary"
          type="button"
          :disabled="!canSubmit || relyricSubmitting"
          @click="submitForm"
        >
          {{ relyricSubmitting ? '提交中…' : '生成同曲换词' }}
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

/* 旋律保持强度行(80 号票):标签 + 滑杆 + 实时数值一行排开(音效秒数
 * 行同款布局);滑杆 accent 走紫罗兰 token 与「人声歌曲」开关一致,
 * 数值 tabular-nums 对齐,语义小注单独一行落微字号。 */
.strength-row {
  display: flex;
  align-items: center;
  gap: 8px;
  color: var(--text-muted);
  font-size: var(--fs-label);
}

.strength-row span {
  font-weight: 600;
  color: var(--violet);
  white-space: nowrap;
}

.strength-input {
  flex: 1;
  min-width: 0;
  accent-color: var(--violet);
}

.strength-value {
  font-variant-numeric: tabular-nums;
  color: var(--violet);
  white-space: nowrap;
}

.strength-hint {
  margin-top: -4px;
  color: var(--text-faint);
  font-size: var(--fs-micro);
}

/* 翻唱参考 chip(78 号票,57 号票克隆样本 chip 同款范式):按钮行 /
 * chip 行 + 隐藏 file input。 */
.sample {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 6px;
}

.chip {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  border-radius: var(--r-pill);
  background: rgba(var(--violet-rgb), 0.14);
  color: var(--violet);
  padding: 4px 8px 4px 10px;
  font-size: var(--fs-micro);
  max-width: 100%;
  overflow: hidden;
}

.chip-x {
  border: none;
  background: none;
  color: inherit;
  cursor: pointer;
  font-size: var(--fs-label);
  line-height: 1;
  padding: 0 2px;
}

.sample-file {
  display: none;
}

/* 「英→中」翻译按钮行(78 号票):标签与按钮同行,按钮靠右。 */
.lyrics-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 6px;
  margin-top: -2px;
}

.lyrics-label {
  font-size: var(--fs-label);
  font-weight: 600;
  color: var(--violet);
}

.action.translate {
  background: var(--wash-fill);
  color: var(--estimate-ink);
  padding: 4px 10px;
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
