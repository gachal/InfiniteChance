<script setup lang="ts">
// 资产库抽屉(51 号票角色详情抽屉的 65 号票泛化):三类实体(角色/场景/
// 道具)共用一块详情抽屉 —— 字段编辑(name/alias/description/detail 纯文本
// pre-wrap,不解析 markdown)+ 设定图画廊(缩略复用 thumb 302、label 按
// 类别白名单、上传/从库选/删除/排序,主资产置前)。角色分支保留音色绑定
// (57 号票)与造型变体条;场景/道具无造型变体,画廊直接挂实体(变体语义
// 用 label 表达)。素材行被删的图行落「素材不可用」占位可手动移除。数据
// 自取自管(get* 拉全树),每次变更 emit('changed', kind, record) 让父级
// 卡墙同步刷新。
import { computed, onMounted, onUnmounted, ref } from 'vue'

import {
  ApiError,
  type AssetRecord,
  type CanvasTask,
  type CharacterImageRecord,
  type CharacterRecord,
  type CharacterVariantRecord,
  type ModelPriceSummary,
  type PropRecord,
  type SceneRecord,
  type VoiceRecord,
} from '@infinitechance/api'

import { useAuth } from '../auth'
import { useThumbFallback, assetContentURL } from '../thumbnail'
import type { AssetKind } from '../assetlib'
import { imageLabelsForKind, isPrimaryLabel, orderedImages } from '../assetlib'
import { characterImageContentURL, characterImageThumbURL, isViewLabel } from '../characters'
import { composeSize, RATIO_PRESETS, RESOLUTION_PRESETS } from '../composer'
import { SHEET_TYPES, latestVariantImage } from '../sheet'
import { voiceStatusBadge } from '../voice'
import AssetPicker from './AssetPicker.vue'

/** 画廊图行的最小视图形状:角色/场景/道具三族的图行记录都满足。 */
interface GalleryImage {
  id: number
  asset_id: number
  label: string
  sort: number
}

const props = defineProps<{
  /** 实体类别:决定加载端点、label 白名单与画廊层级。 */
  kind: AssetKind
  /** 实体 id(角色/场景/道具共用一个槽位)。 */
  assetId: number
  /** 上传与「从库选」的 IP 归属(设定图入库挂该 IP)。 */
  ipId: number
}>()

const emit = defineEmits<{
  changed: [kind: AssetKind, record: CharacterRecord | SceneRecord | PropRecord]
  close: []
}>()

const { client, clearSession } = useAuth()

const entityName = computed(() =>
  props.kind === 'character' ? '角色' : props.kind === 'scene' ? '场景' : '道具',
)

const character = ref<CharacterRecord | null>(null)
const scene = ref<SceneRecord | null>(null)
const prop = ref<PropRecord | null>(null)
const loading = ref(true)
const loadError = ref('')
const actionError = ref('')

// 字段编辑草稿(打开即装,保存才提交)。
const nameDraft = ref('')
const aliasDraft = ref('')
const descriptionDraft = ref('')
const detailDraft = ref('')
const savingFields = ref(false)

// 变体条状态(仅角色):选中变体、行内改名/描述草稿(仅一行处于编辑态)。
const activeVariantId = ref<number>(0)
const editingVariantId = ref<number>(0)
const variantNameDraft = ref('')
const variantNoteDraft = ref('')
const newVariantName = ref('')
const newVariantNote = ref('')
const creatingVariant = ref(false)

// 画廊状态:标签下拉直接改、上下移排序、本机上传、从库选。
const pickingOpen = ref(false)
const fileInput = ref<HTMLInputElement | null>(null)
const uploading = ref(false)
// 展示面回落簿:缩略失败先回原图,原图也失败才落「素材不可用」。
const thumbs = useThumbFallback()
const brokenAssetIds = ref(new Set<number>())

// 音色绑定(57 号票,仅角色):voices 全量 picker(状态徽章 + 试听)+
// 解除;选中即 patchCharacter 提交(0 = 解除)。voices 全局作用域(可跨
// IP 复用),目录自取自管(角色数据同款纪律)。
const voices = ref<VoiceRecord[]>([])
const voicesLoaded = ref(false)
const brokenVoicePreviews = ref(new Set<number>())

async function refreshVoices(): Promise<void> {
  try {
    voices.value = await client.listVoices()
  } catch {
    /* 拉不到就保持空,picker 内置空态提示 */
  } finally {
    voicesLoaded.value = true
  }
}

const boundVoice = computed<VoiceRecord | null>(() => {
  const id = character.value?.voice_id ?? 0
  return id > 0 ? voices.value.find((v) => v.id === id) ?? null : null
})

async function bindVoice(voiceId: number): Promise<void> {
  const c = character.value
  if (!c || c.voice_id === voiceId) {
    return
  }
  actionError.value = ''
  try {
    await client.patchCharacter(c.id, { voice_id: voiceId })
    await refresh()
  } catch (e) {
    fail(e, '音色绑定失败,请稍后再试')
  }
}

const activeVariant = computed<CharacterVariantRecord | null>(() => {
  const c = character.value
  if (!c) {
    return null
  }
  return c.variants.find((v) => v.id === activeVariantId.value) ?? c.variants[0] ?? null
})

/** 画廊展示序:主资产置前(角色 = 视图类三视图/多视图;场景 = 全景;
 * 道具 = 多视图),两组内保持 sort+id。 */
const galleryImages = computed<GalleryImage[]>(() => {
  if (props.kind === 'character') {
    const images: CharacterImageRecord[] = activeVariant.value?.images ?? []
    return [...images.filter((i) => isViewLabel(i.label)), ...images.filter((i) => !isViewLabel(i.label))]
  }
  if (props.kind === 'scene') {
    return orderedImages('scene', scene.value?.images ?? [])
  }
  return orderedImages('prop', prop.value?.images ?? [])
})

const variantLacksViews = computed(() => {
  if (props.kind !== 'character') {
    return false
  }
  const images = activeVariant.value?.images ?? []
  return images.length > 0 && !images.some((i) => isViewLabel(i.label))
})

/** 场景/道具的「缺主资产」弱提示:有图但全无全景/多视图(空画廊走空态
 * 文案,不与提示并出)。 */
const lacksPrimary = computed(() => {
  if (props.kind === 'character') {
    return false
  }
  const images = props.kind === 'scene' ? (scene.value?.images ?? []) : (prop.value?.images ?? [])
  return images.length > 0 && !images.some((i) => isPrimaryLabel(props.kind, i.label))
})

/** 已锚进当前挂点的素材 id(从库选的已选高亮与幂等去重)。 */
const anchoredAssetIds = computed(() => galleryImages.value.map((i) => i.asset_id))

async function refresh(): Promise<void> {
  loadError.value = ''
  try {
    if (props.kind === 'character') {
      const fresh = await client.getCharacter(props.assetId)
      character.value = fresh
      if (!fresh.variants.some((v) => v.id === activeVariantId.value)) {
        activeVariantId.value = fresh.variants[0]?.id ?? 0
      }
      emit('changed', props.kind, fresh)
    } else if (props.kind === 'scene') {
      const fresh = await client.getScene(props.assetId)
      scene.value = fresh
      emit('changed', props.kind, fresh)
    } else {
      const fresh = await client.getProp(props.assetId)
      prop.value = fresh
      emit('changed', props.kind, fresh)
    }
    loadDrafts()
  } catch (e) {
    if (e instanceof ApiError && e.status === 401) {
      clearSession()
      return
    }
    loadError.value = e instanceof ApiError ? e.message : `无法加载${entityName.value},请稍后再试`
  } finally {
    loading.value = false
  }
}

function loadDrafts(): void {
  const fresh = character.value ?? scene.value ?? prop.value
  if (!fresh) {
    return
  }
  nameDraft.value = fresh.name
  aliasDraft.value = fresh.alias
  descriptionDraft.value = fresh.description
  detailDraft.value = fresh.detail
}

onMounted(() => {
  void refresh()
  if (props.kind === 'character') {
    void refreshVoices()
  }
})

function fail(e: unknown, fallback: string): void {
  actionError.value = e instanceof ApiError ? e.message : fallback
}

async function saveFields(): Promise<void> {
  const current = character.value ?? scene.value ?? prop.value
  if (!current || savingFields.value) {
    return
  }
  const name = nameDraft.value.trim()
  if (!name) {
    actionError.value = `${entityName.value}名不能为空`
    return
  }
  savingFields.value = true
  actionError.value = ''
  const body = {
    name,
    alias: aliasDraft.value.trim(),
    description: descriptionDraft.value.trim(),
    detail: detailDraft.value,
  }
  try {
    if (props.kind === 'character') {
      await client.patchCharacter(current.id, body)
    } else if (props.kind === 'scene') {
      await client.patchScene(current.id, body)
    } else {
      await client.patchProp(current.id, body)
    }
    await refresh()
  } catch (e) {
    fail(e, '保存失败,请稍后再试')
  } finally {
    savingFields.value = false
  }
}

// ---- 变体条(仅角色)----

async function addVariant(): Promise<void> {
  const name = newVariantName.value.trim()
  if (!name || !character.value || creatingVariant.value) {
    return
  }
  creatingVariant.value = true
  actionError.value = ''
  try {
    const created = await client.createCharacterVariant(character.value.id, {
      name,
      note: newVariantNote.value.trim(),
    })
    newVariantName.value = ''
    newVariantNote.value = ''
    activeVariantId.value = created.id
    await refresh()
  } catch (e) {
    fail(e, '新建变体失败,请稍后再试')
  } finally {
    creatingVariant.value = false
  }
}

function startVariantEdit(v: CharacterVariantRecord): void {
  editingVariantId.value = v.id
  variantNameDraft.value = v.name
  variantNoteDraft.value = v.note
  actionError.value = ''
}

async function commitVariantEdit(): Promise<void> {
  const id = editingVariantId.value
  const name = variantNameDraft.value.trim()
  if (id === 0 || !name) {
    editingVariantId.value = 0
    return
  }
  actionError.value = ''
  try {
    await client.patchCharacterVariant(id, { name, note: variantNoteDraft.value.trim() })
    editingVariantId.value = 0
    await refresh()
  } catch (e) {
    fail(e, '保存变体失败,请稍后再试')
  }
}

async function removeVariant(v: CharacterVariantRecord): Promise<void> {
  actionError.value = ''
  try {
    await client.deleteCharacterVariant(v.id)
    if (activeVariantId.value === v.id) {
      activeVariantId.value = 0
    }
    await refresh()
  } catch (e) {
    if (e instanceof ApiError && e.code === 'variant_last_one') {
      actionError.value = '最后一个变体不可删除,角色至少要有一个造型。'
    } else {
      fail(e, '删除变体失败,请稍后再试')
    }
  }
}

/** 变体上下移:展示序按 sort+id,交换后把受影响的变体 sort 按下标重写
 * (单管理员量级小,两发 PATCH 足够)。 */
async function moveVariant(v: CharacterVariantRecord, delta: -1 | 1): Promise<void> {
  const c = character.value
  if (!c) {
    return
  }
  const index = c.variants.findIndex((x) => x.id === v.id)
  const target = index + delta
  if (index < 0 || target < 0 || target >= c.variants.length) {
    return
  }
  const reordered = [...c.variants]
  ;[reordered[index], reordered[target]] = [reordered[target], reordered[index]]
  actionError.value = ''
  try {
    for (let i = 0; i < reordered.length; i++) {
      if (reordered[i].sort !== i) {
        await client.patchCharacterVariant(reordered[i].id, { sort: i })
      }
    }
    await refresh()
  } catch (e) {
    fail(e, '排序失败,请稍后再试')
  }
}

// ---- 设定图画廊(角色 = 选中变体;场景/道具 = 实体)----

async function setLabel(img: GalleryImage, label: string): Promise<void> {
  actionError.value = ''
  try {
    await patchImageByKind(img.id, { label })
    await refresh()
  } catch (e) {
    fail(e, '设置标签失败,请稍后再试')
  }
}

async function removeImage(img: GalleryImage): Promise<void> {
  actionError.value = ''
  try {
    await deleteImageByKind(img.id)
    await refresh()
  } catch (e) {
    fail(e, '删除失败,请稍后再试')
  }
}

/** 上下移:同画廊序交换,受影响的图按下标重写 sort(主资产置前只是展示
 * 序,sort 按画廊位次落库,刷新后顺序稳定)。 */
async function moveImage(img: GalleryImage, delta: -1 | 1): Promise<void> {
  const ordered = galleryImages.value
  const index = ordered.findIndex((x) => x.id === img.id)
  const target = index + delta
  if (index < 0 || target < 0 || target >= ordered.length) {
    return
  }
  const reordered = [...ordered]
  ;[reordered[index], reordered[target]] = [reordered[target], reordered[index]]
  actionError.value = ''
  try {
    for (let i = 0; i < reordered.length; i++) {
      if (reordered[i].sort !== i) {
        await patchImageByKind(reordered[i].id, { sort: i })
      }
    }
    await refresh()
  } catch (e) {
    fail(e, '排序失败,请稍后再试')
  }
}

/** 图行 PATCH 按类别分发(三族端点形状一致,只是路由不同)。 */
async function patchImageByKind(imageId: number, body: { label?: string; sort?: number }): Promise<void> {
  if (props.kind === 'character') {
    await client.patchCharacterImage(imageId, body)
  } else if (props.kind === 'scene') {
    await client.patchSceneImage(imageId, body)
  } else {
    await client.patchPropImage(imageId, body)
  }
}

/** 图行删除按类别分发(只删图行,素材行不动)。 */
async function deleteImageByKind(imageId: number): Promise<void> {
  if (props.kind === 'character') {
    await client.deleteCharacterImage(imageId)
  } else if (props.kind === 'scene') {
    await client.deleteSceneImage(imageId)
  } else {
    await client.deletePropImage(imageId)
  }
}

/** <img> 两阶段回落:缩略失败先回原图(onThumbError 已换 src),原图再
 * 失败才落「素材不可用」占位(素材行被删的图行保留,可手动移除)。 */
function onImageError(assetId: number): void {
  if (!thumbs.onThumbError(characterImageContentURL(assetId))) {
    brokenAssetIds.value = new Set([...brokenAssetIds.value, assetId])
  }
}

/** 锚一张上传/选中的素材:角色挂进当前变体,场景/道具直接挂实体。 */
async function anchorAsset(assetId: number): Promise<void> {
  if (props.kind === 'character') {
    if (!character.value || !activeVariant.value) {
      return
    }
    await client.createCharacterImage(character.value.id, {
      variant_id: activeVariant.value.id,
      asset_id: assetId,
    })
  } else if (props.kind === 'scene') {
    if (!scene.value) {
      return
    }
    await client.createSceneImage(scene.value.id, { asset_id: assetId })
  } else {
    if (!prop.value) {
      return
    }
    await client.createPropImage(prop.value.id, { asset_id: assetId })
  }
}

async function onUploadPicked(file: File): Promise<void> {
  if (uploading.value) {
    return
  }
  uploading.value = true
  actionError.value = ''
  try {
    const asset = await client.uploadAsset(file, 'image', props.ipId)
    await anchorAsset(asset.id)
    await refresh()
  } catch (e) {
    fail(e, '上传失败,请稍后再试')
  } finally {
    uploading.value = false
    if (fileInput.value) {
      fileInput.value.value = ''
    }
  }
}

function onFileChange(event: Event): void {
  const input = event.target as HTMLInputElement
  const file = input.files?.[0]
  if (file) {
    void onUploadPicked(file)
  }
}

/** 从库选:点选即锚进当前挂点(已在库里的幂等跳过),面板保持打开可连选;
 * chip 流不涉及 —— 这里是设定图锚,不是参考条。 */
async function onPickAsset(asset: AssetRecord): Promise<void> {
  if (asset.kind !== 'image' || anchoredAssetIds.value.includes(asset.id)) {
    return
  }
  actionError.value = ''
  try {
    await anchorAsset(asset.id)
    await refresh()
  } catch (e) {
    fail(e, '添加设定图失败,请稍后再试')
  }
}

function variantLabel(v: CharacterVariantRecord): string {
  const n = v.images.length
  return n > 0 ? `${v.name}(${n} 图)` : v.name
}

function imageAltLabel(kind: AssetKind): string {
  return kind === 'character' ? '设定图' : kind === 'scene' ? '场景设定图' : '道具设定图'
}

/** 画廊标题:角色 = 选中变体的设定图;场景/道具 = 实体级设定图。 */
const galleryTitle = computed(() => {
  if (props.kind === 'character') {
    return activeVariant.value ? `「${activeVariant.value.name}」的设定图` : '设定图'
  }
  return props.kind === 'scene' ? '场景设定图' : '道具设定图'
})

// ---- 设定图生成对话框(70 号票入口二,仅角色)----
// 轻量生成面:选变体 + 选类型 + 选模型/尺寸,B3 参考默认可移除;prompt
// 不在这里编辑 —— 由服务端按五类型模板拼装(POST /ips/:id/tasks),提交
// 后对话框内轮询单查,成功自动锚进对应变体。模型/尺寸/价格预估/参考条
// 复用 composer 的纯逻辑(composeSize 等),不搬 composer 组件(它是画布
// 锚定单实例);对话框关闭/抽屉关闭即停轮询,任务照常服务端续跑,产物
// 仍是素材,可从画廊「从库选」手动锚兜底。

const sheetOpen = ref(false)
const sheetVariantId = ref(0)
const sheetType = ref<string>(SHEET_TYPES[0])
const sheetModel = ref('')
const sheetRatio = ref('')
const sheetResolution = ref('')
// B3 参考默认:所选变体下已存设定图时默认带最近一张(0 = 无/已移除);
// 轻量面只做「移除」,不加挂(添加走画廊本机上传/从库选)。
const sheetRefAssetId = ref(0)
const imageModels = ref<string[]>([])
const imagePrices = ref<Record<string, ModelPriceSummary>>({})
const sheetModelsFailed = ref(false)
const sheetSubmitting = ref(false)
const sheetTask = ref<CanvasTask | null>(null)
const sheetAnchoring = ref(false)
const sheetDone = ref(false)
const sheetError = ref('')
let sheetPollTimer: number | undefined

function sheetVariantOf(): CharacterVariantRecord | null {
  return character.value?.variants.find((v) => v.id === sheetVariantId.value) ?? null
}

/** 打开对话框:类型/比例/分辨率回默认档,变体 = 当前选中,B3 参考随所选
 * 变体重取;模型目录拉取(失败置位,对话框内说明)。 */
function openSheetDialog(): void {
  sheetOpen.value = true
  sheetError.value = ''
  sheetTask.value = null
  sheetDone.value = false
  sheetType.value = SHEET_TYPES[0]
  sheetRatio.value = ''
  sheetResolution.value = ''
  sheetVariantId.value = activeVariant.value?.id ?? 0
  resetSheetRef()
  void loadSheetModels()
}

/** B3:参考默认 = 所选变体最近一张设定图(id 最大)。 */
function resetSheetRef(): void {
  const v = sheetVariantOf()
  sheetRefAssetId.value = latestVariantImage(v ?? { images: [] })?.asset_id ?? 0
}

function onSheetVariantChange(): void {
  resetSheetRef()
}

async function loadSheetModels(): Promise<void> {
  sheetModelsFailed.value = false
  try {
    const catalog = await client.listImageModels()
    imageModels.value = catalog.models
    imagePrices.value = catalog.prices
    if (!catalog.models.includes(sheetModel.value)) {
      sheetModel.value = catalog.models[0] ?? ''
    }
  } catch {
    sheetModelsFailed.value = true
  }
}

/** 比例 × 分辨率 → size 串(composer 同一组合表;双自动 = 不传)。 */
const sheetSize = computed(() => composeSize(sheetRatio.value, sheetResolution.value) || undefined)

/** 人民币金额的紧凑展示(与 GenerationComposer 同规)。 */
function fmtCNY(n: number): string {
  return `¥${n.toFixed(3).replace(/\.?0+$/, '')}`
}

/** 发送前预估(38 号票口径):按次轨单价 × 1 张;无价不显示。 */
const sheetEstimate = computed(() => {
  const p = imagePrices.value[sheetModel.value]
  if (p?.unit === 'call' && p.cny_per_call != null) {
    return `预估 ${fmtCNY(p.cny_per_call)} / 张`
  }
  return ''
})

/** 提交无画布任务:prompt 服务端拼装;gateway 未配置(503)与校验拒绝
 * (400)原文透出。 */
async function submitSheet(): Promise<void> {
  const c = character.value
  if (!c || !sheetModel.value || sheetSubmitting.value) {
    return
  }
  sheetSubmitting.value = true
  sheetError.value = ''
  sheetDone.value = false
  try {
    const task = await client.createIPTask(props.ipId, {
      character_id: c.id,
      variant_id: sheetVariantId.value,
      sheet_type: sheetType.value,
      model: sheetModel.value,
      ...(sheetSize.value ? { size: sheetSize.value } : {}),
      ...(sheetRefAssetId.value > 0
        ? { image_urls: [characterImageContentURL(sheetRefAssetId.value)] }
        : {}),
    })
    sheetTask.value = task
    pollSheet(task.id)
  } catch (e) {
    fail(e, '提交失败,请稍后再试')
  } finally {
    sheetSubmitting.value = false
  }
}

/** 对话框内轮询(2s):终态即停;succeeded 带产物素材行时自动锚。暂态
 * 轮询失败静默下一拍再试;抽屉关闭(组件卸载)即停 —— 任务服务端照跑,
 * 产物素材在库可手动锚。 */
function pollSheet(taskId: string): void {
  window.clearInterval(sheetPollTimer)
  sheetPollTimer = window.setInterval(() => {
    void (async () => {
      try {
        const t = await client.getIPTask(props.ipId, taskId)
        sheetTask.value = t
        if (t.status !== 'queued' && t.status !== 'running') {
          window.clearInterval(sheetPollTimer)
          sheetPollTimer = undefined
          sheetDone.value = true
          if (t.status === 'succeeded' && t.asset_id > 0) {
            await anchorSheet(t.asset_id)
          }
        }
      } catch {
        /* 暂态失败下一拍再试 */
      }
    })()
  }, 2000)
}

/** 自动锚:label = 所选类型落 character_images(同 label 多张并存);
 * 失败不拦产物 —— 素材在库,画廊「从库选」可手动锚。 */
async function anchorSheet(assetId: number): Promise<void> {
  if (!character.value) {
    return
  }
  sheetAnchoring.value = true
  try {
    await client.createCharacterImage(character.value.id, {
      variant_id: sheetVariantId.value,
      asset_id: assetId,
      label: sheetType.value,
    })
    await refresh()
  } catch (e) {
    actionError.value = e instanceof ApiError ? e.message : '自动锚定失败,可稍后手动锚'
  } finally {
    sheetAnchoring.value = false
  }
}

onUnmounted(() => {
  window.clearInterval(sheetPollTimer)
})
</script>

<template>
  <aside
    class="character-drawer"
    :aria-label="`${entityName}详情`"
  >
    <header class="drawer-head">
      <h3>{{ (character ?? scene ?? prop)?.name ?? entityName }}</h3>
      <button
        class="close"
        type="button"
        @click="emit('close')"
      >
        关闭
      </button>
    </header>

    <p
      v-if="loadError"
      class="feedback error"
      role="alert"
    >
      {{ loadError }}
    </p>
    <p
      v-else-if="loading"
      class="feedback"
    >
      加载中…
    </p>

    <template v-if="character ?? scene ?? prop">
      <!-- 字段编辑:detail 纯文本 pre-wrap,不解析 markdown(51 号票口径)。 -->
      <section class="fields">
        <label class="field">
          名称
          <input
            v-model="nameDraft"
            type="text"
            maxlength="255"
          >
        </label>
        <label class="field">
          别名
          <input
            v-model="aliasDraft"
            type="text"
            maxlength="255"
            placeholder="可选"
          >
        </label>
        <label class="field">
          简介(一句话)
          <input
            v-model="descriptionDraft"
            type="text"
            maxlength="1024"
            placeholder="可选"
          >
        </label>
        <label class="field">
          详细设定(纯文本)
          <textarea
            v-model="detailDraft"
            rows="5"
            maxlength="20000"
            placeholder="可选"
          />
        </label>
        <div class="form-actions">
          <button
            type="button"
            class="primary"
            :disabled="savingFields || !nameDraft.trim()"
            @click="saveFields"
          >
            {{ savingFields ? '保存中…' : '保存资料' }}
          </button>
        </div>
      </section>

      <!-- 音色(57 号票,仅角色;场景/道具无音色 —— 65 号票定案 6)。 -->
      <section
        v-if="character"
        class="voices"
      >
        <h4>音色</h4>
        <p
          v-if="voicesLoaded && voices.length === 0"
          class="hint"
        >
          还没有可用音色 —— 到画布添加「音色」节点创建(需先在管理端为
          voice-enrollment 与合成模型配价)。
        </p>
        <select
          v-else
          :value="character.voice_id > 0 ? String(character.voice_id) : ''"
          aria-label="绑定音色"
          @change="bindVoice(Number(($event.target as HTMLSelectElement).value) || 0)"
        >
          <option value="">
            — 不绑定 —
          </option>
          <option
            v-for="v in voices"
            :key="v.id"
            :value="v.id"
          >
            {{ v.name }}({{ voiceStatusBadge(v.status).label }})
          </option>
        </select>
        <div
          v-if="boundVoice"
          class="voice-line"
        >
          <span
            class="voice-badge"
            :class="voiceStatusBadge(boundVoice.status).tone"
          >{{ voiceStatusBadge(boundVoice.status).label }}</span>
          <span class="voice-name">{{ boundVoice.target_model }}</span>
          <audio
            v-if="boundVoice.preview_asset_id > 0 && !brokenVoicePreviews.has(boundVoice.id)"
            controls
            preload="none"
            :src="assetContentURL(boundVoice.preview_asset_id)"
            @error="brokenVoicePreviews = new Set([...brokenVoicePreviews, boundVoice.id])"
          />
          <p
            v-else
            class="hint"
          >
            暂无试听
          </p>
        </div>
      </section>

      <p
        v-if="actionError"
        class="feedback error"
        role="alert"
      >
        {{ actionError }}
      </p>

      <!-- 变体条(仅角色):名称/服装描述/增删改名/排序;最后一个不可删
           (服务端 409 variant_last_one,文案就地给出)。场景/道具无变体。 -->
      <section
        v-if="character"
        class="variants"
      >
        <h4>造型变体</h4>
        <ul class="variant-list">
          <li
            v-for="(v, i) in character.variants"
            :key="v.id"
            :class="{ active: v.id === activeVariant?.id }"
          >
            <template v-if="editingVariantId === v.id">
              <form
                class="variant-edit"
                @submit.prevent="commitVariantEdit"
              >
                <input
                  v-model="variantNameDraft"
                  type="text"
                  maxlength="255"
                  placeholder="变体名"
                >
                <input
                  v-model="variantNoteDraft"
                  type="text"
                  maxlength="1024"
                  placeholder="服装描述(可选)"
                >
                <button
                  type="submit"
                  class="mini primary"
                >
                  保存
                </button>
                <button
                  type="button"
                  class="mini"
                  @click="editingVariantId = 0"
                >
                  取消
                </button>
              </form>
            </template>
            <template v-else>
              <button
                type="button"
                class="variant-open"
                @click="activeVariantId = v.id"
              >
                <span class="variant-name">{{ variantLabel(v) }}</span>
                <span
                  v-if="v.note"
                  class="variant-note"
                >{{ v.note }}</span>
              </button>
              <span class="variant-actions">
                <button
                  type="button"
                  class="mini"
                  :disabled="i === 0"
                  title="上移"
                  @click="moveVariant(v, -1)"
                >
                  ↑
                </button>
                <button
                  type="button"
                  class="mini"
                  :disabled="i === character.variants.length - 1"
                  title="下移"
                  @click="moveVariant(v, 1)"
                >
                  ↓
                </button>
                <button
                  type="button"
                  class="mini"
                  @click="startVariantEdit(v)"
                >
                  改名
                </button>
                <button
                  type="button"
                  class="mini danger-text"
                  @click="removeVariant(v)"
                >
                  删除
                </button>
              </span>
            </template>
          </li>
        </ul>
        <form
          class="variant-new"
          @submit.prevent="addVariant"
        >
          <input
            v-model="newVariantName"
            type="text"
            maxlength="255"
            placeholder="新变体名(如「校服」)"
          >
          <input
            v-model="newVariantNote"
            type="text"
            maxlength="1024"
            placeholder="服装描述(可选)"
          >
          <button
            type="submit"
            class="mini primary"
            :disabled="creatingVariant || !newVariantName.trim()"
          >
            {{ creatingVariant ? '创建中…' : '+ 新建变体' }}
          </button>
        </form>
      </section>

      <!-- 设定图画廊:角色 = 选中变体,场景/道具 = 实体级;主资产置前;
           缩略走 thumb 302;素材行被删的图行落「素材不可用」占位可移除。 -->
      <section class="gallery">
        <h4>
          {{ galleryTitle }}
          <span class="count">{{ galleryImages.length }}</span>
        </h4>
        <p
          v-if="variantLacksViews"
          class="feedback hint"
        >
          该造型还没有三视图/多视图 —— 视图类是主资产,建议补齐。
        </p>
        <p
          v-else-if="lacksPrimary"
          class="feedback hint"
        >
          还没有{{ kind === 'scene' ? '全景' : '多视图' }}设定图 —— 主资产,建议优先补齐。
        </p>
        <div class="gallery-actions">
          <button
            v-if="character"
            type="button"
            class="mini primary"
            title="生成设定图(三视图/多视图/表情集/色板/姿态,服务端拼装提示词)"
            @click="openSheetDialog"
          >
            生成设定图
          </button>
          <button
            type="button"
            class="mini"
            :disabled="uploading"
            @click="fileInput?.click()"
          >
            {{ uploading ? '上传中…' : '本机上传' }}
          </button>
          <button
            type="button"
            class="mini"
            @click="pickingOpen = !pickingOpen"
          >
            {{ pickingOpen ? '收起素材库' : '从素材库选' }}
          </button>
          <input
            ref="fileInput"
            type="file"
            accept="image/*"
            class="hidden-input"
            @change="onFileChange"
          >
        </div>
        <p
          v-if="galleryImages.length === 0"
          class="feedback"
        >
          {{ character && activeVariant ? '该变体还没有设定图。' : '还没有设定图。' }}
        </p>
        <ul class="gallery-grid">
          <li
            v-for="img in galleryImages"
            :key="img.id"
            class="gallery-card"
          >
            <span
              v-if="brokenAssetIds.has(img.asset_id)"
              class="unavailable"
            >
              素材不可用
              <button
                type="button"
                class="mini"
                @click="removeImage(img)"
              >
                移除
              </button>
            </span>
            <template v-else>
              <img
                :src="thumbs.displayURL(characterImageContentURL(img.asset_id), characterImageThumbURL(img.asset_id))"
                class="gallery-thumb"
                loading="lazy"
                :alt="imageAltLabel(kind)"
                @error="onImageError(img.asset_id)"
              >
              <span
                v-if="img.label"
                class="label-badge"
                :class="{ view: isPrimaryLabel(kind, img.label) }"
              >{{ img.label }}</span>
            </template>
            <div class="gallery-meta">
              <select
                class="label-select"
                :value="img.label"
                aria-label="设定图标签"
                @change="setLabel(img, ($event.target as HTMLSelectElement).value)"
              >
                <option value="">
                  无标签
                </option>
                <option
                  v-for="l in imageLabelsForKind(kind)"
                  :key="l"
                  :value="l"
                >
                  {{ l }}
                </option>
              </select>
              <span class="gallery-actions-inline">
                <button
                  type="button"
                  class="mini"
                  :disabled="img.id === galleryImages[0]?.id"
                  title="上移"
                  @click="moveImage(img, -1)"
                >
                  ↑
                </button>
                <button
                  type="button"
                  class="mini"
                  :disabled="img.id === galleryImages[galleryImages.length - 1]?.id"
                  title="下移"
                  @click="moveImage(img, 1)"
                >
                  ↓
                </button>
                <button
                  type="button"
                  class="mini danger-text"
                  title="删除设定图(素材行不动)"
                  @click="removeImage(img)"
                >
                  删除
                </button>
              </span>
            </div>
          </li>
        </ul>
      </section>

      <!-- 从库选:挂本 IP + 全局,仅图片;面板覆盖在抽屉右缘。 -->
      <AssetPicker
        v-if="pickingOpen"
        title="选择设定图素材"
        :kind-whitelist="['image']"
        hint="点选锚进当前挂点,可连选;素材入库后仍归素材库管理。"
        :selected-asset-ids="anchoredAssetIds"
        :ip-id="ipId"
        @toggle="onPickAsset"
        @close="pickingOpen = false"
      />

      <!-- 生成设定图对话框(70 号票入口二,仅角色):选变体 + 类型 +
           模型/尺寸,B3 参考默认可移除;提交走无画布任务,对话框内轮询,
           成功自动锚。人工确认才发送(花钱操作总案 Q5)。 -->
      <div
        v-if="sheetOpen"
        class="sheet-overlay"
        role="dialog"
        aria-label="生成设定图"
        @click.self="sheetOpen = false"
      >
        <div class="sheet-dialog">
          <header class="sheet-head">
            <h4>生成设定图</h4>
            <button
              type="button"
              class="mini"
              title="关闭(任务照常在服务端续跑,产物在素材库可见)"
              @click="sheetOpen = false"
            >
              关闭
            </button>
          </header>

          <label class="sheet-field">
            造型变体
            <select
              v-model.number="sheetVariantId"
              aria-label="选择造型变体"
              @change="onSheetVariantChange"
            >
              <option
                v-for="v in character?.variants ?? []"
                :key="v.id"
                :value="v.id"
              >
                {{ v.name }}
              </option>
            </select>
          </label>

          <label class="sheet-field">
            类型
            <select
              v-model="sheetType"
              aria-label="设定图类型"
            >
              <option
                v-for="t in SHEET_TYPES"
                :key="t"
                :value="t"
              >
                {{ t }}
              </option>
            </select>
          </label>

          <label class="sheet-field">
            模型
            <select
              v-model="sheetModel"
              :disabled="sheetModelsFailed || imageModels.length === 0"
              aria-label="生图模型"
            >
              <option
                v-for="m in imageModels"
                :key="m"
                :value="m"
              >
                {{ m }}
              </option>
            </select>
          </label>
          <p
            v-if="sheetModelsFailed"
            class="sheet-hint"
            role="alert"
          >
            生图模型目录拉取失败(网关或画布服务不可达),生成暂不可用。
          </p>
          <p
            v-else-if="imageModels.length === 0"
            class="sheet-hint"
          >
            没有已配价的生图模型:请在管理台配置按次价格。
          </p>

          <div class="sheet-field-row">
            <label class="sheet-field">
              比例
              <select
                v-model="sheetRatio"
                aria-label="画面比例"
              >
                <option
                  v-for="p in RATIO_PRESETS"
                  :key="p.value"
                  :value="p.value"
                >
                  {{ p.label }}
                </option>
              </select>
            </label>
            <label class="sheet-field">
              分辨率
              <select
                v-model="sheetResolution"
                aria-label="分辨率档位"
              >
                <option
                  v-for="p in RESOLUTION_PRESETS"
                  :key="p.value"
                  :value="p.value"
                >
                  {{ p.label }}
                </option>
              </select>
            </label>
          </div>

          <!-- B3 参考默认:所选变体最近一张设定图,可移除(移除 = 干净
               重 roll);轻量面不做加挂,添加走画廊上传/从库选。 -->
          <div class="sheet-field">
            <span class="sheet-label-text">参考图</span>
            <div
              v-if="sheetRefAssetId > 0"
              class="sheet-ref"
            >
              <img
                :src="thumbs.displayURL(characterImageContentURL(sheetRefAssetId), characterImageThumbURL(sheetRefAssetId))"
                alt="参考图"
                @error="onImageError(sheetRefAssetId)"
              >
              <button
                type="button"
                class="mini"
                title="移除参考图(发送即文生图重 roll)"
                @click="sheetRefAssetId = 0"
              >
                ×
              </button>
            </div>
            <span
              v-else
              class="sheet-hint"
            >不带参考(文生图重 roll)</span>
          </div>

          <p
            v-if="sheetEstimate"
            class="sheet-hint"
          >
            {{ sheetEstimate }}
          </p>

          <!-- 进度面:queued/running 转圈文案,终态落结果;刷新丢映射不
               影响任务本身,产物素材在库。 -->
          <p
            v-if="sheetTask && !sheetDone"
            class="sheet-hint"
            role="status"
          >
            生成中…({{ sheetTask.status === 'queued' ? '排队中' : '生成中' }}),可关闭对话框,任务照常续跑。
          </p>
          <p
            v-if="sheetDone && sheetTask?.status === 'succeeded'"
            class="sheet-hint ok"
            role="status"
          >
            {{ sheetAnchoring ? '已生成,正在锚进角色库…' : '已生成并锚进「' + (sheetVariantOf()?.name ?? '') + '」(' + sheetType + ')。' }}
          </p>
          <p
            v-if="sheetDone && sheetTask && sheetTask.status !== 'succeeded'"
            class="sheet-hint"
            role="alert"
          >
            任务{{ sheetTask.status === 'canceled' ? '已取消' : '失败' }}{{ sheetTask.error ? ':' + sheetTask.error : '' }}(可重新提交)
          </p>
          <p
            v-if="sheetError"
            class="sheet-hint"
            role="alert"
          >
            {{ sheetError }}
          </p>

          <div class="sheet-actions">
            <button
              type="button"
              class="primary"
              :disabled="sheetSubmitting || !sheetModel || sheetModelsFailed || imageModels.length === 0"
              title="提交生成任务(服务端拼装提示词,人工确认后计费)"
              @click="submitSheet"
            >
              {{ sheetSubmitting ? '提交中…' : '生成' }}
            </button>
          </div>
        </div>
      </div>
    </template>
  </aside>
</template>

<style scoped>
.character-drawer {
  position: fixed;
  top: 0;
  right: 0;
  bottom: 0;
  width: var(--w-drawer);
  display: flex;
  flex-direction: column;
  gap: var(--sp-md);
  padding: var(--sp-lg);
  background: rgba(var(--surface-panel-rgb), 0.98);
  border-left: 1px solid var(--line-control);
  box-shadow: var(--shadow-slide);
  z-index: var(--z-drawer);
  overflow-y: auto;
  animation: drawer-in var(--dur-slow) var(--ease);
}

@keyframes drawer-in {
  from {
    opacity: 0;
    transform: translateX(24px);
  }
  to {
    opacity: 1;
    transform: translateX(0);
  }
}

.drawer-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: var(--sp-sm);
  padding-bottom: 10px;
  border-bottom: 1px solid var(--line-section);
}

.drawer-head h3 {
  margin: 0;
  font-size: var(--fs-title);
}

.close {
  border: 1px solid var(--line-control);
  background: transparent;
  color: var(--text-secondary);
  border-radius: var(--r-md);
  padding: 4px 10px;
  font-size: var(--fs-label);
  cursor: pointer;
  transition:
    border-color var(--dur-fast) var(--ease),
    background-color var(--dur-fast) var(--ease),
    color var(--dur-fast) var(--ease);
}

.close:hover {
  border-color: var(--line-action);
  color: inherit;
}

.voices {
  display: grid;
  gap: 8px;
}

.voices select {
  background: var(--wash-soft);
  border: 1px solid var(--line-control);
  border-radius: var(--r-md);
  padding: 8px 10px;
  color: inherit;
  font-size: var(--fs-body);
}

.voices select option {
  background: rgba(var(--surface-card-rgb), 1);
}

.voice-line {
  display: grid;
  gap: 6px;
  padding: 8px 10px;
  border: 1px dashed rgba(var(--teal-light-rgb), 0.35);
  border-radius: var(--r-md);
}

.voice-line audio {
  width: 100%;
  height: 30px;
}

.voice-badge {
  justify-self: start;
  border-radius: var(--r-pill);
  padding: 1px 8px;
  font-size: var(--fs-micro);
  background: rgba(var(--teal-light-rgb), 0.12);
  color: var(--teal-light);
}

.voice-badge.ok {
  background: rgba(var(--success-rgb), 0.16);
  color: var(--success);
}

.voice-badge.deploying {
  background: rgba(var(--amber-rgb), 0.16);
  color: var(--amber);
}

.voice-badge.rejected {
  background: rgba(var(--red-signal-rgb), 0.16);
  color: var(--red-signal);
}

.voice-name {
  color: var(--text-muted);
  font-size: var(--fs-label);
}

.fields {
  display: grid;
  gap: 10px;
}

.field {
  display: grid;
  gap: 4px;
  font-size: var(--fs-label);
  color: var(--text-secondary);
}

.field input,
.field textarea,
.variant-new input,
.variant-edit input {
  background: var(--wash-soft);
  border: 1px solid var(--line-control);
  border-radius: var(--r-md);
  padding: 8px 10px;
  color: inherit;
  font-size: var(--fs-body);
  transition:
    border-color var(--dur-fast) var(--ease),
    box-shadow var(--dur-fast) var(--ease);
}

/* 工作灯焦点:与生成对话框输入同款(描边转交互蓝 + 极淡光晕)。 */
.field input:focus,
.field textarea:focus,
.variant-new input:focus,
.variant-edit input:focus {
  outline: none;
  border-color: rgba(var(--accent-rgb), 0.85);
  box-shadow: 0 0 0 3px rgba(var(--accent-rgb), 0.12);
}

.field textarea {
  resize: vertical;
  white-space: pre-wrap;
}

.form-actions {
  display: flex;
  justify-content: flex-end;
}

.primary {
  background: var(--accent);
  color: var(--accent-ink);
  font-weight: 700;
  border: none;
  border-radius: var(--r-md);
  padding: 7px 14px;
  font-size: var(--fs-body);
  cursor: pointer;
  transition: background-color var(--dur-fast) var(--ease);
}

.primary:hover:not(:disabled) {
  background: var(--accent-soft);
}

.primary:disabled {
  opacity: 0.4;
  cursor: default;
}

.mini {
  background: transparent;
  border: 1px solid var(--line-control);
  color: var(--text-secondary);
  border-radius: var(--r-md);
  padding: 3px 8px;
  font-size: var(--fs-label);
  cursor: pointer;
  transition:
    border-color var(--dur-fast) var(--ease),
    background-color var(--dur-fast) var(--ease),
    color var(--dur-fast) var(--ease);
}

.mini:hover:not(:disabled) {
  border-color: var(--line-action);
  background: var(--wash-soft);
  color: var(--text-primary);
}

.mini:disabled {
  opacity: 0.4;
  cursor: default;
}

.mini.primary {
  background: var(--accent);
  border-color: var(--accent);
  color: var(--accent-ink);
}

.mini.primary:hover:not(:disabled) {
  background: var(--accent-soft);
  color: var(--accent-ink);
}

.danger-text {
  color: var(--danger);
}

.variants h4,
.gallery h4 {
  margin: 0 0 8px;
  font-size: var(--fs-body);
  display: flex;
  align-items: center;
  gap: 6px;
}

.gallery .count {
  color: var(--text-faint);
  font-size: var(--fs-label);
  font-weight: 400;
}

.variant-list {
  list-style: none;
  margin: 0 0 8px;
  padding: 0;
  display: grid;
  gap: 6px;
}

.variant-list li {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
  border: 1px solid var(--line-section);
  border-radius: var(--r-lg);
  padding: 6px 8px;
}

.variant-list li.active {
  border-color: rgba(var(--accent-rgb), 0.6);
  background: rgba(var(--accent-rgb), 0.12);
}

.variant-open {
  flex: 1;
  display: grid;
  gap: 2px;
  text-align: left;
  background: transparent;
  border: none;
  color: inherit;
  padding: 2px 4px;
  cursor: pointer;
  min-width: 0;
}

.variant-name {
  font-size: var(--fs-body);
  font-weight: 600;
}

.variant-note {
  color: var(--text-muted);
  font-size: var(--fs-label);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.variant-actions {
  display: flex;
  gap: 4px;
  flex-shrink: 0;
}

.variant-edit {
  flex: 1;
  display: grid;
  grid-template-columns: 1fr 1fr auto auto;
  gap: 6px;
}

.variant-new {
  display: grid;
  grid-template-columns: 1fr 1fr auto;
  gap: 6px;
}

.gallery-actions {
  display: flex;
  gap: 6px;
  margin-bottom: 8px;
}

.hidden-input {
  display: none;
}

.gallery-grid {
  list-style: none;
  margin: 0;
  padding: 0;
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 8px;
}

.gallery-card {
  position: relative;
  display: grid;
  gap: 4px;
  background: var(--wash-subtle);
  border: 1px solid var(--line-section);
  border-radius: var(--r-lg);
  padding: 6px;
}

.gallery-thumb {
  width: 100%;
  height: 110px;
  object-fit: cover;
  border-radius: var(--r-md);
  background: var(--wash-soft);
  display: block;
}

.label-badge {
  position: absolute;
  top: 10px;
  left: 10px;
  background: rgba(var(--surface-panel-rgb), 0.85);
  border: 1px solid var(--line-action);
  color: var(--text-bright);
  font-size: var(--fs-micro);
  border-radius: var(--r-pill);
  padding: 1px 8px;
}

.label-badge.view {
  border-color: rgba(var(--accent-rgb), 0.8);
  color: var(--accent-soft);
}

.unavailable {
  display: grid;
  place-items: center;
  gap: 6px;
  height: 110px;
  color: var(--text-muted);
  font-size: var(--fs-label);
  border: 1px dashed var(--line-control);
  border-radius: var(--r-md);
}

.gallery-meta {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 4px;
}

.label-select {
  background: var(--wash-soft);
  border: 1px solid var(--line-control);
  border-radius: 7px;
  color: var(--text-secondary);
  padding: 2px 4px;
  font-size: var(--fs-micro);
  max-width: 96px;
}

.gallery-actions-inline {
  display: flex;
  gap: 3px;
}

.feedback {
  margin: 0;
  color: var(--text-muted);
  font-size: var(--fs-body);
}

.feedback.error {
  color: var(--danger);
}

.feedback.hint {
  color: var(--gold);
  margin-bottom: 8px;
}

/* 生成设定图对话框(70 号票入口二):覆盖抽屉的轻量浮层。 */
.sheet-overlay {
  position: fixed;
  inset: 0;
  z-index: calc(var(--z-drawer) + 1);
  display: grid;
  place-items: center;
  background: rgba(0, 0, 0, 0.4);
}

.sheet-dialog {
  width: min(420px, calc(100vw - 48px));
  display: grid;
  gap: var(--sp-sm);
  padding: var(--sp-md);
  background: rgba(var(--surface-panel-rgb), 0.98);
  border: 1px solid var(--line-control);
  border-radius: var(--r-xl);
  box-shadow: var(--shadow-overlay);
}

.sheet-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
}

.sheet-head h4 {
  margin: 0;
}

.sheet-field {
  display: grid;
  gap: 4px;
  font-size: var(--fs-label);
  color: var(--text-muted);
}

.sheet-field select {
  background: var(--wash-soft);
  border: 1px solid var(--line-control);
  border-radius: var(--r-md);
  padding: 6px;
  color: inherit;
  font-size: var(--fs-body);
}

.sheet-field-row {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: var(--sp-sm);
}

.sheet-label-text {
  font-size: var(--fs-label);
  color: var(--text-muted);
}

.sheet-ref {
  display: flex;
  align-items: center;
  gap: var(--sp-sm);
}

.sheet-ref img {
  width: 64px;
  height: 64px;
  object-fit: cover;
  border-radius: var(--r-md);
  background: var(--wash-soft);
  display: block;
}

.sheet-hint {
  margin: 0;
  font-size: var(--fs-micro);
  color: var(--text-muted);
}

.sheet-hint.ok {
  color: var(--success);
}

.sheet-actions {
  display: flex;
  justify-content: flex-end;
}
</style>
