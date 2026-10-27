<script setup lang="ts">
// 角色详情抽屉(51 号票):字段编辑(name/alias/description/detail 纯文本
// pre-wrap,不解析 markdown)+ 变体条(名称/服装描述/增删改名/排序)+
// 选中变体的设定图画廊(缩略复用 thumb 302、label 六类白名单、上传/从库
// 选/删除/排序,视图类置前)。变体缺视图类图时弱提示,不做硬约束;素材
// 行被删的图行落「素材不可用」占位可手动移除。数据自取自管(getCharacter
// 拉全树),每次变更 emit('changed') 让父级卡墙同步刷新。
import { computed, onMounted, ref } from 'vue'

import {
  ApiError,
  type AssetRecord,
  type CharacterImageRecord,
  type CharacterRecord,
  type CharacterVariantRecord,
  type VoiceRecord,
} from '@infinitechance/api'

import { useAuth } from '../auth'
import { useThumbFallback, assetContentURL } from '../thumbnail'
import { CHARACTER_IMAGE_LABELS, characterImageContentURL, characterImageThumbURL, isViewLabel } from '../characters'
import { voiceStatusBadge } from '../voice'
import AssetPicker from './AssetPicker.vue'

const props = defineProps<{
  characterId: number
  /** 上传与「从库选」的 IP 归属(设定图入库挂该 IP)。 */
  ipId: number
}>()

const emit = defineEmits<{
  changed: [character: CharacterRecord]
  close: []
}>()

const { client, clearSession } = useAuth()

const character = ref<CharacterRecord | null>(null)
const loading = ref(true)
const loadError = ref('')
const actionError = ref('')

// 字段编辑草稿(打开即装,保存才提交)。
const nameDraft = ref('')
const aliasDraft = ref('')
const descriptionDraft = ref('')
const detailDraft = ref('')
const savingFields = ref(false)

// 变体条状态:选中变体、行内改名/描述草稿(仅一行处于编辑态)。
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

// 音色绑定(57 号票,角色级):voices 全量 picker(状态徽章 + 试听)+
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

/** 画廊展示序:视图类置前,两组内保持 sort+id(51 号票主资产口径)。 */
const galleryImages = computed<CharacterImageRecord[]>(() => {
  const images = activeVariant.value?.images ?? []
  return [...images.filter((i) => isViewLabel(i.label)), ...images.filter((i) => !isViewLabel(i.label))]
})

const variantLacksViews = computed(() => {
  const images = activeVariant.value?.images ?? []
  return images.length > 0 && !images.some((i) => isViewLabel(i.label))
})

/** 已锚进当前变体的素材 id(从库选的已选高亮与幂等去重)。 */
const anchoredAssetIds = computed(() => (activeVariant.value?.images ?? []).map((i) => i.asset_id))

async function refresh(): Promise<void> {
  loadError.value = ''
  try {
    const fresh = await client.getCharacter(props.characterId)
    character.value = fresh
    nameDraft.value = fresh.name
    aliasDraft.value = fresh.alias
    descriptionDraft.value = fresh.description
    detailDraft.value = fresh.detail
    if (!fresh.variants.some((v) => v.id === activeVariantId.value)) {
      activeVariantId.value = fresh.variants[0]?.id ?? 0
    }
    emit('changed', fresh)
  } catch (e) {
    if (e instanceof ApiError && e.status === 401) {
      clearSession()
      return
    }
    loadError.value = e instanceof ApiError ? e.message : '无法加载角色,请稍后再试'
  } finally {
    loading.value = false
  }
}

onMounted(() => {
  void refresh()
  void refreshVoices()
})

function fail(e: unknown, fallback: string): void {
  actionError.value = e instanceof ApiError ? e.message : fallback
}

async function saveFields(): Promise<void> {
  if (!character.value || savingFields.value) {
    return
  }
  const name = nameDraft.value.trim()
  if (!name) {
    actionError.value = '角色名不能为空'
    return
  }
  savingFields.value = true
  actionError.value = ''
  try {
    await client.patchCharacter(character.value.id, {
      name,
      alias: aliasDraft.value.trim(),
      description: descriptionDraft.value.trim(),
      detail: detailDraft.value,
    })
    await refresh()
  } catch (e) {
    fail(e, '保存失败,请稍后再试')
  } finally {
    savingFields.value = false
  }
}

// ---- 变体条 ----

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

// ---- 设定图画廊 ----

async function setLabel(img: CharacterImageRecord, label: string): Promise<void> {
  actionError.value = ''
  try {
    await client.patchCharacterImage(img.id, { label })
    await refresh()
  } catch (e) {
    fail(e, '设置标签失败,请稍后再试')
  }
}

async function removeImage(img: CharacterImageRecord): Promise<void> {
  actionError.value = ''
  try {
    await client.deleteCharacterImage(img.id)
    await refresh()
  } catch (e) {
    fail(e, '删除失败,请稍后再试')
  }
}

/** 上下移:同画廊序交换,受影响的图按下标重写 sort(视图类置前只是展示
 * 序,sort 按画廊位次落库,刷新后顺序稳定)。 */
async function moveImage(img: CharacterImageRecord, delta: -1 | 1): Promise<void> {
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
        await client.patchCharacterImage(reordered[i].id, { sort: i })
      }
    }
    await refresh()
  } catch (e) {
    fail(e, '排序失败,请稍后再试')
  }
}

/** <img> 两阶段回落:缩略失败先回原图(onThumbError 已换 src),原图再
 * 失败才落「素材不可用」占位(素材行被删的图行保留,可手动移除)。 */
function onImageError(assetId: number): void {
  if (!thumbs.onThumbError(characterImageContentURL(assetId))) {
    brokenAssetIds.value = new Set([...brokenAssetIds.value, assetId])
  }
}

async function onUploadPicked(file: File): Promise<void> {
  if (!character.value || !activeVariant.value || uploading.value) {
    return
  }
  uploading.value = true
  actionError.value = ''
  try {
    const asset = await client.uploadAsset(file, 'image', props.ipId)
    await client.createCharacterImage(character.value.id, {
      variant_id: activeVariant.value.id,
      asset_id: asset.id,
    })
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

/** 从库选:点选即锚进当前变体(已在库里的幂等跳过),面板保持打开可连选;
 * chip 流不涉及 —— 这里是设定图锚,不是参考条。 */
async function onPickAsset(asset: AssetRecord): Promise<void> {
  if (!character.value || !activeVariant.value || asset.kind !== 'image') {
    return
  }
  if (anchoredAssetIds.value.includes(asset.id)) {
    return
  }
  actionError.value = ''
  try {
    await client.createCharacterImage(character.value.id, {
      variant_id: activeVariant.value.id,
      asset_id: asset.id,
    })
    await refresh()
  } catch (e) {
    fail(e, '添加设定图失败,请稍后再试')
  }
}

function variantLabel(v: CharacterVariantRecord): string {
  const n = v.images.length
  return n > 0 ? `${v.name}(${n} 图)` : v.name
}
</script>

<template>
  <aside
    class="character-drawer"
    aria-label="角色详情"
  >
    <header class="drawer-head">
      <h3>{{ character?.name ?? '角色' }}</h3>
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

    <template v-if="character">
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

      <!-- 音色(57 号票,角色级绑定:声音跟人不跟衣服,变体级显式不做)。
           picker 列全量台账(状态徽章 + 试听),选中即提交,解除选占位。 -->
      <section class="voices">
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

      <!-- 变体条:名称/服装描述/增删改名/排序;最后一个不可删(服务端
           409 variant_last_one,文案就地给出)。 -->
      <section class="variants">
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

      <!-- 选中变体的设定图画廊:视图类置前;缩略走 thumb 302;素材行被删
           的图行落「素材不可用」占位可移除。 -->
      <section
        v-if="activeVariant"
        class="gallery"
      >
        <h4>
          「{{ activeVariant.name }}」的设定图
          <span class="count">{{ activeVariant.images.length }}</span>
        </h4>
        <p
          v-if="variantLacksViews"
          class="feedback hint"
        >
          该造型还没有三视图/多视图 —— 视图类是主资产,建议补齐。
        </p>
        <div class="gallery-actions">
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
          该变体还没有设定图。
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
                alt="设定图"
                @error="onImageError(img.asset_id)"
              >
              <span
                v-if="img.label"
                class="label-badge"
                :class="{ view: isViewLabel(img.label) }"
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
                  v-for="l in CHARACTER_IMAGE_LABELS"
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
        hint="点选锚进当前变体,可连选;素材入库后仍归素材库管理。"
        :selected-asset-ids="anchoredAssetIds"
        :ip-id="ipId"
        @toggle="onPickAsset"
        @close="pickingOpen = false"
      />
    </template>
  </aside>
</template>

<style scoped>
.character-drawer {
  position: fixed;
  top: 0;
  right: 0;
  bottom: 0;
  width: min(var(--w-drawer), calc(100vw - 40px));
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
</style>
