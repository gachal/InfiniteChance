<script setup lang="ts">
// 全景节点(81 号票,第十四种节点类型「创作」组):参考场景图生成 360°
// 球面环绕视图(腾讯原生全景通路,公开名 hunyuan-world-panorama)。
// · 创作态:内联表单(ADR 0002 显式例外,全参数固定进对话框等于全禁用
//   控件;音频系同族先例)—— 参考图 chip 必填恰好 1(本机上传 + 素材库
//   挑选双入口;chip 旁注记「须为场景图,人像/物体会被上游拒绝」)+
//   模型下拉(/image-models 全量,缺省 hunyuan-world-panorama,不按模型
//   特判 —— 选非全景名由上游 400 透出)+ 生成按钮;**无描述框、无尺寸/
//   比例/透明选项**(上游固定 8K 2:1,原生不吃提示词)。选中不唤起生成
//   对话框(编辑器 composerNode 只认 image/video)。
// · 在途/失败:任务徽章(排队/生成中)+ 失败原因 + 原地重试(编辑器链)。
// · 产物态:2:1 缩略 + 360° 角标(卡面不跑 3D 渲染,40 号票纪律);整卡
//   点击进全屏球面查看层(表单区除外);宽高比偏离 2:1 超 2% 时卡面提示
//   行,照常可看可重生成 —— 重新生成 = 原地替换,旧产物留素材库。
// · 参考图 chip 加载出宽高比 ≈2:1(±2%)时出「直接环视」—— 不生成、
//   不花钱直接进查看层。
// 不渲染把手与 + 按钮,不作任何连线的源与目标(EDGE_MATRIX.panorama 全空)。
import { computed, ref, watch } from 'vue'

import type { CanvasTask, ModelPriceSummary } from '@infinitechance/api'

import type { PanoramaNodeData } from '../../graph'
import { PANO_DEFAULT_MODEL, isPanoRatio, panoRatioDeviation } from '../../panorama'
import { assetContentURL, useThumbFallback } from '../../thumbnail'
import { fmtCNY } from '../../voice'

const props = defineProps<{
  id: string
  type: string
  data: PanoramaNodeData
  /** 绑定到本节点的生成任务(编辑器轮询保持最新;null = 无任务)。 */
  task?: CanvasTask | null
  /** 重试请求在途时禁用重试按钮。 */
  retrying: boolean
  /** /image-models 全量目录:模型下拉(不按全景名过滤,选非全景名由
   * 上游 400 透出)与按次预估共用。 */
  imageModels: string[]
  imagePrices: Record<string, ModelPriceSummary>
  /** 全景提交请求在途(编辑器级,禁提交按钮)。 */
  submitting?: boolean
  /** 参考图上传在途(编辑器级,禁上传按钮)。 */
  refUploading?: boolean
}>()

const emit = defineEmits<{
  retry: []
  /** 创作态表单事实变更(逐键上抛,编辑器 updateNodeData 浅合并落盘)。 */
  'form-change': [patch: Partial<PanoramaNodeData>]
  /** 创作态提交(以当前 data 为准;校验在节点侧,编辑器链再兜一层)。 */
  submit: []
  /** 参考图:本机上传(kind=image 经 /api/assets/upload 入库)。 */
  'ref-file': [file: File]
  /** 参考图:素材库挑选(AssetPicker image 过滤)。 */
  'pick-ref': []
  /** 进查看层(产物态整卡点击 / 创作态参考图「直接环视」):素材 id,
   * 查看层贴图按它拼 /api/assets/{id}/content 同源地址。 */
  view: [assetId: number]
}>()

const status = computed(() => props.task?.status)
// 在途判定:排队/生成中禁新提交(失败/取消是终态,卡上另有重试,也可
// 直接改表单再发新任务 —— 编辑器提交链同款守卫)。
const taskActive = computed(() => status.value === 'queued' || status.value === 'running')
// 创作态 = 没有产物、没有任务(右键菜单「全景」落下的空节点)。
const creating = computed(() => !(props.data.url ?? '') && !status.value)

// ---- 参考图 chip(创作事实:恰好 1 条)----

const refAssetId = computed(() => props.data.ref_asset_id ?? 0)

const refThumbs = useThumbFallback()
const refBroken = ref(false)
watch(
  () => props.data.ref_asset_id,
  () => {
    refBroken.value = false
    refDims.value = null
  },
)

/** 参考图宽高比 ≈2:1 判定(±2%):chip 缩略图加载后量自然尺寸 ——
 * 命中即出「直接环视」(参考图本来就是全景时不花钱直接看)。 */
const refDims = ref<{ w: number; h: number } | null>(null)
const refIsPano = computed(
  () => refDims.value !== null && isPanoRatio(refDims.value.w, refDims.value.h),
)

function onRefImgLoad(e: Event): void {
  const img = e.target as HTMLImageElement
  refDims.value = { w: img.naturalWidth, h: img.naturalHeight }
}

function clearRef(): void {
  emit('form-change', { ref_asset_id: 0 })
}

// 本机上传的隐藏 file input(cover 参考曲同款链)。
const refInput = ref<HTMLInputElement | null>(null)

function pickRefFile(): void {
  refInput.value?.click()
}

function onRefInputChange(e: Event): void {
  const input = e.target as HTMLInputElement
  const file = input.files?.[0]
  input.value = '' // 复位,同一文件下次选择仍触发 change。
  if (file && !props.refUploading) {
    emit('ref-file', file)
  }
}

// ---- 模型下拉(/image-models 全量,缺省 hunyuan-world-panorama)----

// 缺省物化(cover 创作态同款):创作态且节点尚未选模型时落一次 —— 目录
// 里有全景公开名用它,没有落目录首项(下拉不出现悬空值);目录到齐后
// 选中值不在册(渠道撤了该模型)回落首项;产物态不动产物配对元数据。
watch(
  () => [creating.value, props.imageModels] as const,
  ([isCreating, models]) => {
    if (!isCreating || models.length === 0) {
      return
    }
    const current = props.data.model ?? ''
    if (current === '') {
      emit('form-change', {
        model: models.includes(PANO_DEFAULT_MODEL) ? PANO_DEFAULT_MODEL : models[0]!,
      })
    } else if (!models.includes(current)) {
      emit('form-change', { model: models[0]! })
    }
  },
  { immediate: true },
)

const selectedPrice = computed(() => {
  const m = props.data.model
  return m ? props.imagePrices[m] : undefined
})

const estimate = computed(() => {
  const p = selectedPrice.value
  if (p?.unit === 'call' && p.cny_per_call != null) {
    return `一次 ${fmtCNY(p.cny_per_call)}(固定 8K 2:1)`
  }
  return '按次计费,以管理端配价为准'
})

const canSubmit = computed(
  () => refAssetId.value > 0 && (props.data.model ?? '') !== '' && !taskActive.value,
)

function submitForm(): void {
  if (!canSubmit.value || props.submitting) {
    return
  }
  emit('submit')
}

// ---- 产物态(2:1 缩略 + 校验提示;整卡点击进查看层)----

const thumbs = useThumbFallback()
const loadFailed = ref(false)
watch(
  () => props.data.url,
  () => {
    loadFailed.value = false
    productDims.value = null
  },
)

// 产物态 = 已有产物且未失联(重新生成的落点仍是本节点,原地替换)。
const hasProduct = computed(() => !!(props.data.url ?? '') && !loadFailed.value)

/** 产物校验提示(票·定案 9):缩略加载后量自然尺寸,偏离 2:1 超 2% 出
 * 提示行 —— 照常可看可重生成,不拦任何动作。 */
const productDims = ref<{ w: number; h: number } | null>(null)
const productDeviation = computed(() =>
  productDims.value === null ? 0 : panoRatioDeviation(productDims.value.w, productDims.value.h),
)

function onProductImgLoad(e: Event): void {
  const img = e.target as HTMLImageElement
  productDims.value = { w: img.naturalWidth, h: img.naturalHeight }
}

function onProductImgError(): void {
  if (!thumbs.onThumbError(props.data.url ?? '')) {
    loadFailed.value = true
  }
}

const productDisplayURL = computed(() => thumbs.displayURL(props.data.url ?? ''))
const refDisplayURL = computed(() => refThumbs.displayURL(assetContentURL(refAssetId.value)))

/** 整卡点击:仅产物态进查看层(创作态整卡即表单;素材行被删不可看)。
 * 表单区与下载钮各自 @click.stop。 */
function onCardClick(): void {
  if (hasProduct.value && (props.data.asset_id ?? 0) > 0) {
    emit('view', props.data.asset_id!)
  }
}
</script>

<template>
  <div
    class="node media panorama"
    :data-task="status ?? 'none'"
    @click="onCardClick"
  >
    <header>
      全景
      <span
        class="badge-360"
        title="360° 球面环绕视图"
      >360°</span>
    </header>

    <div
      v-if="status === 'queued' || status === 'running'"
      class="placeholder working"
    >
      <span class="pill">{{ status === 'queued' ? '排队中…' : '生成中…' }}</span>
      <small>全景任务在服务端进行(约 1–5 分钟),关闭页面不丢失</small>
    </div>
    <div
      v-else-if="status === 'failed'"
      class="failed"
      role="alert"
      @click.stop
    >
      <p class="error">
        {{ task?.error || '生成失败' }}
      </p>
      <button
        class="retry"
        type="button"
        :disabled="retrying"
        @click.stop="emit('retry')"
      >
        {{ retrying ? '重试中…' : '重试' }}
      </button>
    </div>
    <div
      v-else-if="data.url && !loadFailed"
      class="pano-frame"
      title="点击进入 360° 环绕查看"
    >
      <img
        :src="productDisplayURL"
        alt="全景产物"
        @load="onProductImgLoad"
        @error="onProductImgError"
      >
      <span class="badge-360 overlay">360°</span>
      <a
        class="download"
        :href="data.url"
        download
        title="下载图片"
        aria-label="下载图片"
        @click.stop
      >⬇</a>
    </div>
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
      <span>全景占位</span>
      <small>配一张参考场景图开始生成</small>
    </div>

    <!-- 产物校验提示(定案 9):偏离 2:1 超 2% 才出,照常可看可重生成。 -->
    <p
      v-if="hasProduct && productDeviation > 2"
      class="ratio-hint"
      role="note"
    >
      产物宽高比 {{ (productDims!.w / productDims!.h).toFixed(2) }}:1,偏离 2:1 超 2%,可照常查看与重新生成
    </p>

    <!-- 创作态表单(ADR 0002 显式例外,产物态保留作重新生成入口):
         参考图 chip 必填恰好 1 + 模型下拉 + 生成;无描述框、无尺寸选项。 -->
    <div
      class="pano-form"
      @click.stop
    >
      <div class="pano-ref">
        <template v-if="refAssetId > 0">
          <span
            v-if="!refBroken"
            class="chip"
            title="已选参考场景图(生成以它延续场景与视点)"
          >
            <img
              :src="refDisplayURL"
              alt="参考图缩略"
              @load="onRefImgLoad"
              @error="refBroken = true"
            >
            参考图 · 素材 #{{ refAssetId }}
            <button
              class="chip-x"
              type="button"
              aria-label="移除参考图"
              @click="clearRef"
            >×</button>
          </span>
          <span
            v-else
            class="chip broken"
          >
            参考素材不可用
            <button
              class="chip-x"
              type="button"
              aria-label="移除参考图"
              @click="clearRef"
            >×</button>
          </span>
          <!-- 直接环视:参考图本来就是全景(≈2:1)时不生成、不花钱直接看。 -->
          <button
            v-if="refIsPano && !refBroken"
            class="action direct-view"
            type="button"
            title="参考图已是 2:1 全景,直接进球面查看(不生成)"
            @click="emit('view', refAssetId)"
          >
            直接环视
          </button>
        </template>
        <template v-else>
          <button
            class="action"
            type="button"
            :disabled="refUploading"
            @click="pickRefFile"
          >
            {{ refUploading ? '上传中…' : '本机上传' }}
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
            class="ref-file"
            type="file"
            accept="image/*"
            aria-label="参考场景图文件"
            @change="onRefInputChange"
          >
        </template>
      </div>
      <p class="ref-note">
        须为场景图,人像/物体会被上游拒绝
      </p>
      <select
        class="field"
        :value="data.model ?? ''"
        aria-label="全景模型"
        @change="emit('form-change', { model: ($event.target as HTMLSelectElement).value })"
      >
        <option
          value=""
          disabled
        >
          选择模型
        </option>
        <option
          v-for="m in imageModels"
          :key="m"
          :value="m"
        >
          {{ m }}<template v-if="imagePrices[m]?.cny_per_call != null">
            · {{ fmtCNY(imagePrices[m].cny_per_call!) }}/次
          </template>
        </option>
      </select>
      <div class="form-foot">
        <span
          class="estimate"
          :title="selectedPrice ? undefined : '模型目录未带价,以管理端配价为准'"
        >
          {{ estimate }}
        </span>
        <button
          class="action primary"
          type="button"
          :disabled="!canSubmit || submitting"
          @click="submitForm"
        >
          {{ submitting ? '提交中…' : hasProduct ? '重新生成' : '生成全景' }}
        </button>
      </div>
    </div>
  </div>
</template>

<style scoped>
.node {
  width: 280px;
  background: rgba(var(--surface-card-rgb), 0.92);
  border: 1px solid rgba(103, 232, 249, 0.4);
  border-radius: var(--r-xl);
  padding: 10px 12px;
  font-size: var(--fs-body);
}

/* 产物态整卡可点(创作态整卡即表单,点击落空)。 */
.node:has(.pano-frame) {
  cursor: pointer;
}

.node header {
  font-weight: 600;
  color: #67e8f9;
  margin-bottom: 8px;
  display: flex;
  align-items: center;
  gap: 6px;
}

.node[data-task='queued'] {
  border-style: dashed;
}

.node[data-task='running'] {
  border-color: rgba(103, 232, 249, 0.85);
}

.node[data-task='failed'] {
  border-color: rgba(var(--danger-rgb), 0.6);
}

/* 360° 角标:卡头与产物缩略各一枚(卡面不跑 3D 渲染,40 号票纪律)。 */
.badge-360 {
  padding: 0 6px;
  border-radius: var(--r-sm);
  background: rgba(103, 232, 249, 0.16);
  color: #67e8f9;
  font-size: var(--fs-micro);
  font-weight: 600;
  letter-spacing: 0.5px;
}

.pano-frame {
  position: relative;
  cursor: pointer;
}

.pano-frame img {
  display: block;
  width: 100%;
  aspect-ratio: 2 / 1;
  object-fit: cover;
  border-radius: var(--r-lg);
}

.pano-frame .overlay {
  position: absolute;
  left: 8px;
  bottom: 8px;
  background: rgba(var(--surface-panel-rgb), 0.85);
}

/* 悬浮下载:产物右上角,hover 才出现(触屏常显)。 */
.pano-frame .download {
  position: absolute;
  top: 8px;
  right: 8px;
  width: 28px;
  height: 28px;
  display: grid;
  place-items: center;
  border: none;
  border-radius: var(--r-md);
  background: rgba(var(--surface-panel-rgb), 0.85);
  color: var(--text-bright);
  font-size: var(--fs-body);
  text-decoration: none;
  opacity: 0;
  transition: opacity var(--dur-fast) var(--ease);
}

.pano-frame:hover .download,
.pano-frame .download:focus-visible {
  opacity: 1;
}

/* 产物校验提示行(偏离 2:1 超 2%):琥珀系弱提示,不拦动作。 */
.ratio-hint {
  margin: 8px 0 0;
  padding: 6px 8px;
  border: 1px solid rgba(250, 204, 21, 0.4);
  border-radius: var(--r-md);
  color: var(--text-muted);
  font-size: var(--fs-micro);
}

.placeholder {
  display: grid;
  gap: 4px;
  justify-items: center;
  padding: 26px 8px;
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

.working .pill {
  color: #67e8f9;
}

.node[data-task='running'] .pill {
  animation: pulse 1.4s ease-in-out infinite;
}

.failed {
  display: grid;
  gap: 8px;
  justify-items: start;
  padding: 10px;
  border: 1px solid rgba(var(--danger-rgb), 0.35);
  border-radius: var(--r-md);
}

.failed .error {
  margin: 0;
  font-size: var(--fs-label);
  color: var(--danger);
  word-break: break-all;
}

.retry {
  border: none;
  border-radius: var(--r-md);
  padding: 6px 14px;
  font-size: var(--fs-label);
  font-weight: 600;
  cursor: pointer;
  background: var(--wash-fill);
  color: inherit;
}

.retry:disabled {
  opacity: 0.5;
  cursor: not-allowed;
}

/* 创作态表单(产物态保留作重新生成入口)。 */
.pano-form {
  display: grid;
  gap: 8px;
  margin-top: 8px;
}

.pano-ref {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
  align-items: center;
}

.chip {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  padding: 3px 8px;
  border: 1px solid var(--line-control);
  border-radius: var(--r-md);
  background: var(--wash-soft);
  font-size: var(--fs-micro);
}

.chip img {
  width: 42px;
  height: 21px;
  object-fit: cover;
  border-radius: var(--r-sm);
}

.chip.broken {
  color: var(--danger);
  border-color: rgba(var(--danger-rgb), 0.4);
}

.chip-x {
  border: none;
  background: transparent;
  color: var(--text-muted);
  font-size: var(--fs-body);
  line-height: 1;
  cursor: pointer;
  padding: 0 2px;
}

.chip-x:hover {
  color: var(--danger);
}

.ref-note {
  margin: -2px 0 0;
  font-size: var(--fs-micro);
  color: var(--text-muted);
}

.action {
  border: 1px solid var(--line-control);
  border-radius: var(--r-md);
  padding: 6px 12px;
  font-size: var(--fs-label);
  font-weight: 600;
  cursor: pointer;
  background: transparent;
  color: inherit;
}

.action:disabled {
  opacity: 0.5;
  cursor: not-allowed;
}

.action:hover:not(:disabled) {
  border-color: rgba(103, 232, 249, 0.6);
}

/* 直接环视:青色实底,与生成按钮区分(一个不花钱、一个花钱)。 */
.action.direct-view {
  border-color: rgba(103, 232, 249, 0.55);
  color: #67e8f9;
}

.action.primary {
  background: rgba(103, 232, 249, 0.18);
  border-color: rgba(103, 232, 249, 0.55);
}

.field {
  width: 100%;
  background: var(--wash-soft);
  border: 1px solid var(--line-control);
  border-radius: var(--r-md);
  padding: 6px;
  color: inherit;
  font-size: var(--fs-label);
}

.form-foot {
  display: flex;
  justify-content: space-between;
  align-items: center;
  gap: 8px;
}

.estimate {
  font-size: var(--fs-micro);
  color: var(--text-muted);
}

.ref-file {
  display: none;
}
</style>
