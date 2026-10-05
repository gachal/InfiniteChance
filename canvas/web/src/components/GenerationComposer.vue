<script setup lang="ts">
// 生成对话框(21 号票图片模式;24 号票视频模式):画布生成的统一对话框,
// 单实例悬浮层(范式见 ADR 0002)。组件只做渲染与事件:草稿(提示词/
// 模型/尺寸/时长/参考条)由编辑器按模式独立持有(切换不清空),发送语义
// = 提交画布任务。视频模式 = 文生视频/图生视频/首尾帧/多参考的组合形态,
// 参考组合自然成模式,无显式模式 tab;选中视频节点时模式锁定视频(视频
// 产物不能当生图参考),底栏模式下拉的「图片生成」禁选。
import { computed, ref } from 'vue'

import type { ModelPriceSummary } from '@infinitechance/api'

import {
  MAX_COMPOSER_REFS,
  RATIO_PRESETS,
  RESOLUTION_PRESETS,
  composeSize,
  durationRangeFor,
  rolesForKind,
  VIDEO_RESOLUTION_PRESETS,
  VIDEO_ROLE_CAPS,
  VIDEO_ROLE_LABEL,
  videoPlaceholder,
  type ComposerRef,
  type VideoComposerRef,
  type VideoRefRole,
} from '../composer'

const props = defineProps<{
  /** 当前模式:图片生成或视频生成(编辑器持有,切换不清空两套草稿)。 */
  mode: 'image' | 'video'
  /** 模式锁定(选中视频节点):模式下拉的图片生成禁选。 */
  modeLocked: boolean
  /** 可用的按次计价生图模型(编辑器从 /image-models 拉取)。 */
  imageModels: string[]
  /** 可用的按秒计价视频模型(编辑器从 /video-models 拉取)。 */
  videoModels: string[]
  /** 当前模式的模型价格摘要(38 号票):key = 模型名,人类人民币单位;
   * 旧目录无 prices 时为空对象,价格展示与预估随之静默退位。 */
  prices: Record<string, ModelPriceSummary>
  /** 目录不可用原因(38 号票评审:目录拉取失败或未配价时,对话框保留
   * 但整体禁用并说明原因,不再静默蒸发);空 = 正常可用。 */
  disabledReason: string
  /** 图片模式的生效参考图列表。 */
  refs: ComposerRef[]
  /** 视频模式的全模态参考条(角色在 chip 上切换)。 */
  videoRefs: VideoComposerRef[]
  /** 当前模式的草稿:提示词/模型/比例/分辨率(视频分辨率为档位串)。 */
  prompt: string
  model: string
  ratio: string
  resolution: string
  /** 图片模式草稿:透明背景开关(37 号票,所有生图模型显示;视频模式
   * 不出现 —— 视频透明显式 out of scope)。 */
  transparent: boolean
  /** 视频模式草稿:时长档('' = 自动,不传 seconds)。 */
  duration: string
  /** 提交在途(编辑器级状态,防连点)。 */
  generating: boolean
  /** 参考上传在途。 */
  uploading: boolean
}>()

const emit = defineEmits<{
  'update:mode': [value: 'image' | 'video']
  'update:prompt': [value: string]
  'update:model': [value: string]
  'update:ratio': [value: string]
  'update:resolution': [value: string]
  'update:transparent': [value: boolean]
  'update:duration': [value: string]
  'remove-ref': [index: number]
  'set-ref-role': [index: number, role: VideoRefRole]
  upload: [file: File]
  /** 目录禁用态里的「重试」:编辑器重新拉取模型目录。 */
  'retry-catalogs': []
  send: []
}>()

const fileInput = ref<HTMLInputElement | null>(null)

const isVideo = computed(() => props.mode === 'video')
const activeModels = computed(() => (isVideo.value ? props.videoModels : props.imageModels))

/** 时长区间随所选模型收敛(官方校准:seedance 2.0 = 4–15,2.5 = 4–30;
 * 未知模型保守 4–15)。留空 = 自动(不传 seconds)。 */
const durationRange = computed(() => durationRangeFor(props.model))

/** 失焦时把输入夹紧回区间(整数):留空保持自动,越界/小数就地归位。 */
function onDurationBlur(e: Event): void {
  const raw = (e.target as HTMLInputElement).value.trim()
  if (raw === '') {
    return
  }
  const n = Number(raw)
  if (Number.isInteger(n) && n >= durationRange.value.min && n <= durationRange.value.max) {
    return
  }
  const clamped = Math.min(Math.max(Math.round(n) || durationRange.value.min, durationRange.value.min), durationRange.value.max)
  emit('update:duration', String(clamped))
}

const canSend = computed(
  () =>
    props.prompt.trim().length > 0 &&
    props.model !== '' &&
    !props.generating &&
    props.disabledReason === '',
)

const refsFull = computed(() => props.refs.length >= MAX_COMPOSER_REFS)

// ---- 成本可见性(38 号票):下拉带价、发送前预估 ----

/** 人民币金额的紧凑展示:整数不带小数、最多三位小数、去尾零。 */
function fmtCNY(n: number): string {
  const s = n.toFixed(3).replace(/\.?0+$/, '')
  return `¥${s}`
}

/** 模型下拉 option 的展示名:带价时缀上人类可读单价,选中即见成本。 */
function modelOptionLabel(m: string): string {
  const p = props.prices[m]
  if (!p) {
    return m
  }
  if (p.unit === 'call' && p.cny_per_call != null) {
    return `${m} · ${fmtCNY(p.cny_per_call)}/张`
  }
  if (p.unit === 'second' && p.cny_per_call != null) {
    return `${m} · ${fmtCNY(p.cny_per_call)}/秒`
  }
  if (p.unit === 'token' && p.output_cny_per_mtokens != null) {
    return `${m} · 约 ${fmtCNY(p.output_cny_per_mtokens)}/百万 tok`
  }
  return m
}

/** 发送前预估(仅展示,向上取整对齐服务端预扣口径;旧目录无 prices 或
 * 未知轨道时空串退位)。second 轨按 分辨率系数 × 秒数;时长自动时只报
 * 单价;视频 token 轨按 秒折算率 × 秒数 × 输出单价 × 倍率;call 轨按
 * 尺寸系数,不乘参考图数(参考加价各厂商不同,不虚报)。 */
const estimate = computed<string>(() => {
  const p = props.prices[props.model]
  if (!p) {
    return ''
  }
  if (p.unit === 'call' && p.cny_per_call != null) {
    const size = composeSize(props.ratio, props.resolution)
    const factor = size ? (p.size_factors?.[size] ?? 1) : 1
    return `本次预估 ${fmtCNY(Math.ceil(p.cny_per_call * factor * 100) / 100)}`
  }
  if (p.unit === 'second' && p.cny_per_call != null) {
    const factor = props.resolution ? (p.size_factors?.[props.resolution] ?? 1) : 1
    const per = p.cny_per_call * factor
    const secs = Number(props.duration)
    if (props.duration !== '' && Number.isFinite(secs) && secs > 0) {
      return `本次预估 ${fmtCNY(Math.ceil(per * secs * 100) / 100)}(${fmtCNY(per)}/秒 × ${secs} 秒)`
    }
    return `${fmtCNY(per)}/秒,时长自动,按实结算`
  }
  if (p.unit === 'token' && p.output_cny_per_mtokens != null) {
    const secs = Number(props.duration)
    if (props.duration === '' || !Number.isFinite(secs) || secs <= 0) {
      return ''
    }
    const rate = props.resolution
      ? (p.size_tokens_per_second?.[props.resolution] ?? p.default_tokens_per_second ?? 0)
      : (p.default_tokens_per_second ?? 0)
    if (rate <= 0) {
      return ''
    }
    const tokens = Math.ceil(rate * secs)
    const cost = (tokens / 1_000_000) * p.output_cny_per_mtokens * (p.ratio ?? 1)
    return `本次预估 约 ${fmtCNY(Math.ceil(cost * 100) / 100)}(${tokens} tok)`
  }
  return ''
})

/** 键盘发送(38 号票评审):Enter 发送、Shift+Enter 换行,Cmd/Ctrl+Enter
 * 同样发送;输入法组词中不触发(isComposing,31 号票同款纪律)。 */
function onPromptKeydown(e: KeyboardEvent): void {
  if (e.key !== 'Enter' || e.isComposing) {
    return
  }
  if (e.shiftKey && !(e.metaKey || e.ctrlKey)) {
    return // Shift+Enter = 换行,交给默认行为。
  }
  e.preventDefault()
  if (canSend.value) {
    emit('send')
  }
}

/** 视频参考条还有空位:任一角色未满即可再收(appendVideoRef 会按角色
 * 最终裁决,这里只决定加号按钮的可用态与提示)。 */
const videoRefsVacant = computed(() =>
  (Object.keys(VIDEO_ROLE_CAPS) as VideoRefRole[]).some(
    (role) => props.videoRefs.filter((r) => r.role === role).length < VIDEO_ROLE_CAPS[role],
  ),
)

const placeholder = computed(() =>
  isVideo.value ? videoPlaceholder(props.videoRefs) : '描述画面,或对选中图片说明要改什么…',
)

const uploadAccept = computed(() => (isVideo.value ? 'image/*,video/*,audio/*' : 'image/*'))

const uploadTitle = computed(() => {
  if (isVideo.value) {
    return videoRefsVacant.value ? '上传本机图片/视频/音频作为参考' : '各角色参考已满员'
  }
  return refsFull.value ? `最多 ${MAX_COMPOSER_REFS} 张参考图` : '上传本机图片作为参考图'
})

/** 参考上传:kind 由编辑器按文件 MIME/扩展名判断(图片模式下恒 image),
 * 服务端按魔数最终裁决。 */
function onFileChange(e: Event): void {
  const input = e.target as HTMLInputElement
  const file = input.files?.[0]
  input.value = '' // 复位,同一文件下次选择仍触发 change。
  if (!file) {
    return
  }
  emit('upload', file)
}
</script>

<template>
  <section
    class="composer"
    :class="{ disabled: disabledReason !== '' }"
    aria-label="生成对话框"
  >
    <!-- 目录不可用禁态(38 号票评审):对话框保留、说明原因、可重试,
         不再静默蒸发。 -->
    <div
      v-if="disabledReason"
      class="catalog-error"
      role="alert"
    >
      <span>{{ disabledReason }}</span>
      <button
        type="button"
        @click="emit('retry-catalogs')"
      >
        重试
      </button>
    </div>
    <!-- 图片模式:参考图缩略条 -->
    <div
      v-if="!isVideo"
      class="ref-strip"
    >
      <div
        v-for="(r, i) in refs"
        :key="r.url"
        class="ref-chip"
      >
        <img
          :src="r.url"
          alt="参考图"
        >
        <button
          class="ref-remove"
          type="button"
          title="移除参考图(发送即文生图,连线仍保留)"
          aria-label="移除参考图"
          @click="emit('remove-ref', i)"
        >
          ×
        </button>
      </div>
      <button
        class="ref-add"
        type="button"
        :disabled="uploading || refsFull || disabledReason !== ''"
        :title="uploadTitle"
        aria-label="上传参考图"
        @click="fileInput?.click()"
      >
        {{ uploading ? '…' : '+' }}
      </button>
      <input
        ref="fileInput"
        type="file"
        accept="image/*"
        class="ref-file"
        @change="onFileChange"
      >
    </div>

    <!-- 视频模式:全模态参考条,角色标在 chip 上(图缩略,视频/音频图标
         占位,不做波形与抽帧);图片 chip 的角色可切换。 -->
    <div
      v-else
      class="ref-strip"
    >
      <div
        v-for="(r, i) in videoRefs"
        :key="r.url"
        class="vref-chip"
        :data-kind="r.kind"
      >
        <img
          v-if="r.kind === 'image'"
          :src="r.url"
          alt="参考图"
        >
        <span
          v-else
          class="vref-icon"
        >{{ r.kind === 'video' ? '▶' : '♪' }}</span>
        <select
          v-if="r.kind === 'image'"
          class="vref-role"
          :value="r.role"
          :title="`角色(${VIDEO_ROLE_LABEL[r.role]})`"
          @change="emit('set-ref-role', i, ($event.target as HTMLSelectElement).value as VideoRefRole)"
        >
          <option
            v-for="role in rolesForKind('image')"
            :key="role"
            :value="role"
          >
            {{ VIDEO_ROLE_LABEL[role] }}
          </option>
        </select>
        <span
          v-else
          class="vref-role fixed"
        >{{ VIDEO_ROLE_LABEL[r.role] }}</span>
        <button
          class="ref-remove"
          type="button"
          title="移除该参考"
          @click="emit('remove-ref', i)"
        >
          ×
        </button>
      </div>
      <button
        class="ref-add"
        type="button"
        :disabled="uploading || !videoRefsVacant"
        :title="uploadTitle"
        @click="fileInput?.click()"
      >
        {{ uploading ? '…' : '+' }}
      </button>
      <input
        ref="fileInput"
        type="file"
        :accept="uploadAccept"
        class="ref-file"
        @change="onFileChange"
      >
    </div>

    <textarea
      :value="prompt"
      :placeholder="placeholder"
      :disabled="disabledReason !== ''"
      rows="3"
      aria-label="生成提示词"
      @input="emit('update:prompt', ($event.target as HTMLTextAreaElement).value)"
      @keydown="onPromptKeydown"
    />

    <!-- 成本预估(38 号票):有价才显示,随参数联动。 -->
    <p
      v-if="estimate"
      class="estimate"
    >
      {{ estimate }}
    </p>

    <div class="controls">
      <select
        :value="mode"
        class="mode"
        :disabled="disabledReason !== ''"
        title="生成模式(选中视频节点时锁定视频生成)"
        @change="emit('update:mode', ($event.target as HTMLSelectElement).value as 'image' | 'video')"
      >
        <option
          value="image"
          :disabled="modeLocked"
        >
          图片生成
        </option>
        <option value="video">
          视频生成
        </option>
      </select>
      <input
        v-if="isVideo"
        :value="duration"
        type="number"
        class="duration"
        :min="durationRange.min"
        :max="durationRange.max"
        step="1"
        placeholder="自动"
        :disabled="disabledReason !== ''"
        :title="`时长(秒):留空 = 自动由模型裁决;本模型支持 ${durationRange.min}–${durationRange.max} 秒`"
        @input="emit('update:duration', ($event.target as HTMLInputElement).value)"
        @blur="onDurationBlur"
      >
      <select
        :value="ratio"
        class="ratio"
        :disabled="disabledReason !== ''"
        title="画面比例(自动 = 由模型缺省裁决)"
        @change="emit('update:ratio', ($event.target as HTMLSelectElement).value)"
      >
        <option
          v-for="p in RATIO_PRESETS"
          :key="p.value"
          :value="p.value"
        >
          {{ p.label }}
        </option>
      </select>
      <select
        v-if="isVideo"
        :value="resolution"
        class="resolution"
        :disabled="disabledReason !== ''"
        title="分辨率档位(自动 = 由模型缺省裁决;档位串直传上游)"
        @change="emit('update:resolution', ($event.target as HTMLSelectElement).value)"
      >
        <option
          v-for="p in VIDEO_RESOLUTION_PRESETS"
          :key="p.value"
          :value="p.value"
        >
          {{ p.label }}
        </option>
      </select>
      <select
        v-else
        :value="resolution"
        class="resolution"
        :disabled="disabledReason !== ''"
        title="分辨率档位(自动 = 由模型缺省裁决;单选分辨率按 1:1 兜底)"
        @change="emit('update:resolution', ($event.target as HTMLSelectElement).value)"
      >
        <option
          v-for="p in RESOLUTION_PRESETS"
          :key="p.value"
          :value="p.value"
        >
          {{ p.label }}
        </option>
      </select>
      <!-- 透明背景开关(37 号票):仅图片模式、所有生图模型显示,不按
           模型特判 —— 上游不认时 4xx 原样透出、节点可见可重试。 -->
      <label
        v-if="!isVideo"
        class="transparent"
        title="透明背景(产物为带 alpha 通道的 PNG;上游不支持时错误原样透出)"
      >
        <input
          type="checkbox"
          :checked="transparent"
          :disabled="disabledReason !== ''"
          @change="emit('update:transparent', ($event.target as HTMLInputElement).checked)"
        >
        透明背景
      </label>
      <select
        :value="model"
        class="model"
        :disabled="disabledReason !== ''"
        :title="isVideo ? '视频模型' : '生图模型'"
        @change="emit('update:model', ($event.target as HTMLSelectElement).value)"
      >
        <option
          v-for="m in activeModels"
          :key="m"
          :value="m"
        >
          {{ modelOptionLabel(m) }}
        </option>
      </select>
      <button
        class="send"
        type="button"
        :disabled="!canSend"
        :title="canSend ? '生成(Enter 发送,Shift+Enter 换行)' : '先写提示词并选模型'"
        aria-label="生成"
        @click="emit('send')"
      >
        {{ generating ? '…' : '↑' }}
      </button>
    </div>
  </section>
</template>

<style scoped>
/* 即梦式深色面板:参考条 + 输入区 + 参数栏,悬浮在画布上。 */
.composer {
  position: absolute;
  z-index: 10;
  width: min(560px, calc(100% - 32px));
  display: grid;
  gap: 8px;
  padding: 12px;
  background: rgba(13, 18, 32, 0.96);
  border: 1px solid rgba(255, 255, 255, 0.14);
  border-radius: 16px;
  box-shadow: 0 12px 40px rgba(0, 0, 0, 0.45);
  box-sizing: border-box;
}

.ref-strip {
  display: flex;
  gap: 8px;
  align-items: flex-start;
  min-height: 48px;
  flex-wrap: wrap;
}

.ref-chip {
  position: relative;
  width: 48px;
  height: 48px;
  flex-shrink: 0;
}

.ref-chip img {
  width: 100%;
  height: 100%;
  object-fit: cover;
  border-radius: 10px;
  border: 1px solid rgba(255, 255, 255, 0.18);
  display: block;
}

/* 视频参考 chip:图片缩略或图标占位,角色标记贴在底部。 */
.vref-chip {
  position: relative;
  width: 64px;
  flex-shrink: 0;
  display: grid;
  gap: 2px;
  justify-items: stretch;
}

.vref-chip img {
  width: 64px;
  height: 48px;
  object-fit: cover;
  border-radius: 10px;
  border: 1px solid rgba(255, 255, 255, 0.18);
  display: block;
}

.vref-icon {
  width: 64px;
  height: 48px;
  display: grid;
  place-items: center;
  border-radius: 10px;
  border: 1px solid rgba(255, 255, 255, 0.18);
  background: rgba(255, 255, 255, 0.05);
  color: #aab1c5;
  font-size: 18px;
}

.vref-role {
  width: 100%;
  background: rgba(255, 255, 255, 0.07);
  border: 1px solid rgba(255, 255, 255, 0.14);
  border-radius: 6px;
  color: inherit;
  font-size: 10px;
  padding: 1px 2px;
}

.vref-role.fixed {
  text-align: center;
  color: #8b91a7;
  font-size: 10px;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.ref-remove {
  position: absolute;
  top: -6px;
  right: -6px;
  width: 18px;
  height: 18px;
  border: none;
  border-radius: 50%;
  background: rgba(60, 64, 80, 0.95);
  color: #dfe3ee;
  font-size: 12px;
  line-height: 1;
  cursor: pointer;
  display: grid;
  place-items: center;
  padding: 0;
}

.ref-add {
  width: 48px;
  height: 48px;
  flex-shrink: 0;
  border: 1px dashed rgba(255, 255, 255, 0.3);
  border-radius: 10px;
  background: transparent;
  color: #aab1c5;
  font-size: 20px;
  cursor: pointer;
}

.ref-add:disabled {
  opacity: 0.4;
  cursor: not-allowed;
}

.ref-file {
  display: none;
}

/* 目录不可用禁态:整体降不透明度,交互面全部 :disabled。 */
.composer.disabled .ref-strip {
  opacity: 0.5;
}

.catalog-error {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 10px;
  padding: 8px 10px;
  border: 1px solid rgba(250, 204, 21, 0.4);
  border-radius: 10px;
  background: rgba(250, 204, 21, 0.08);
  font-size: 12px;
  color: #fde68a;
}

.catalog-error button {
  border: 1px solid rgba(253, 230, 138, 0.5);
  border-radius: 8px;
  background: transparent;
  color: #fde68a;
  padding: 4px 10px;
  font-size: 12px;
  cursor: pointer;
  white-space: nowrap;
}

.catalog-error button:hover {
  background: rgba(253, 230, 138, 0.12);
}

/* 成本预估行:安静的单行小字,参数变化即时刷新。 */
.estimate {
  margin: 0;
  font-size: 12px;
  color: #9fb3d9;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

textarea {
  width: 100%;
  resize: vertical;
  min-height: 64px;
  background: rgba(255, 255, 255, 0.05);
  border: 1px solid rgba(255, 255, 255, 0.12);
  border-radius: 10px;
  padding: 8px 10px;
  color: inherit;
  font: inherit;
  line-height: 1.5;
  box-sizing: border-box;
}

textarea:focus {
  outline: 2px solid rgba(122, 162, 247, 0.6);
  outline-offset: 1px;
  border-color: transparent;
}

.controls {
  display: flex;
  align-items: center;
  gap: 8px;
  flex-wrap: wrap;
}

.controls select {
  background: rgba(255, 255, 255, 0.05);
  border: 1px solid rgba(255, 255, 255, 0.12);
  border-radius: 8px;
  padding: 6px 8px;
  color: inherit;
  font-size: 12px;
  max-width: 150px;
}

.controls .mode,
.controls .ratio,
.controls .duration,
.controls .resolution {
  flex-shrink: 0;
  max-width: 110px;
}

/* 时长数字输入比下拉窄,单位(秒)缀在 placeholder 语义里。 */
.controls input.duration {
  width: 64px;
  background: rgba(255, 255, 255, 0.05);
  border: 1px solid rgba(255, 255, 255, 0.12);
  border-radius: 8px;
  padding: 6px 8px;
  color: inherit;
  font-size: 12px;
  box-sizing: border-box;
}

/* 透明背景开关:与下拉同排的小号复选项。 */
.controls label.transparent {
  display: flex;
  align-items: center;
  gap: 4px;
  flex-shrink: 0;
  font-size: 12px;
  color: #aab1c5;
  cursor: pointer;
  user-select: none;
  white-space: nowrap;
}

.controls label.transparent input {
  accent-color: #7aa2f7;
  margin: 0;
}

.controls .model {
  flex: 1;
  min-width: 120px;
}

.send {
  flex-shrink: 0;
  width: 36px;
  height: 36px;
  border: none;
  border-radius: 50%;
  background: #7aa2f7;
  color: #10152a;
  font-size: 16px;
  font-weight: 700;
  cursor: pointer;
  display: grid;
  place-items: center;
  padding: 0;
}

.send:disabled {
  opacity: 0.4;
  cursor: not-allowed;
}
</style>
