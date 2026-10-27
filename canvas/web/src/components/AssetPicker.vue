<script setup lang="ts">
// 通用素材选择器(47 号票,32 号票 AgentMediaPicker 泛化):Agent 节点
// 「素材库」与生成对话框(图片/视频模式)共用一块右缘面板,复用既有
// GET /assets 列表 API,按最近使用排序(服务端解引用刷新,47 号票口径)。
// kind 白名单收窄可选面(视频模式含音频,Agent 附件仍限图/视频)、缩略
// 点选、可多选 —— 点选挂到调用方(ready 引用,上限由调用方按同款纪律
// 拒收),再点一次取消。50 号票加 IP 归属过滤:ipId 提供时 scope 在
// 「本 IP + 全局」(缺省)与「全部」间切换。51 号票加「角色」视图:
// 角色 → 变体两级分组列设定图,点选进参考条/附件条与普通素材同款 chip 流
// (合成 AssetRecord:设定图引用 = 既有 asset_id + 内容寻址,服务端零改动;
// chip 不带角色标注 —— MVP 它就是素材)。
import { computed, onMounted, ref } from 'vue'

import { ApiError, type AssetRecord, type CharacterRecord } from '@infinitechance/api'

import { useAuth } from '../auth'
import { useThumbFallback } from '../thumbnail'
import { useAssetListQuery } from '../composables/useAssetList'
import { characterImageContentURL, characterImageThumbURL, isViewLabel } from '../characters'

/** 素材种类词表(与素材域 kind 同形)。 */
export type PickerKind = 'image' | 'video' | 'audio'

const props = defineProps<{
  /** 面板标题(Agent 用「选择参考素材」,对话框用「挑选参考素材」)。 */
  title: string
  /** kind 白名单:只列这些种类;>1 种时顶部出过滤档。 */
  kindWhitelist: PickerKind[]
  /** 已选素材 id,驱动已选高亮。 */
  selectedAssetIds: number[]
  /** 面板说明行;空 = 不显示。 */
  hint?: string
  /** 「本 IP」归属(50 号票);缺省 = 无 IP 维度,恒全库(也不出角色视图
   * —— 角色是 IP 级实体,51 号票)。 */
  ipId?: number
}>()

const emit = defineEmits<{
  toggle: [asset: AssetRecord]
  close: []
}>()

const { client } = useAuth()

const assets = ref<AssetRecord[]>([])
const kind = ref<'' | PickerKind>('')
const loading = ref(false)
const error = ref('')

// ---- 角色视图(51 号票):'asset' = 素材库平铺(缺省,行为不变),
// 'character' = 角色 → 变体两级分组的设定图。 ----
const view = ref<'asset' | 'character'>('asset')
const characters = ref<CharacterRecord[]>([])
const charactersLoading = ref(false)
const charactersError = ref('')

/** 角色视图只在带 IP 归属且白名单含图片时出现(设定图全是图片)。 */
const showViewSwitch = computed(() => props.ipId !== undefined && props.kindWhitelist.includes('image'))

// 归属视图、列表参数与分页窗口与 AssetPanel 同一套(评审修复提取至
// useAssetListQuery,单一来源)。
const { scope, listParams, hasMore } = useAssetListQuery({
  ipId: () => props.ipId,
  kind,
  count: () => assets.value.length,
})

const showScopeToggle = computed(() => props.ipId !== undefined && view.value === 'asset')

/** 过滤档只在多白名单时出现(单一种类没有可切的);「全部」= 白名单全体。 */
const kindTabs = computed<('' | PickerKind)[]>(() =>
  props.kindWhitelist.length > 1 ? ['', ...props.kindWhitelist] : [''],
)

/** 白名单外的种类就地过滤,不出现不可选的卡片。 */
const visibleAssets = computed(() => assets.value.filter((a) => props.kindWhitelist.includes(a.kind)))

const emptyText = computed(() =>
  props.kindWhitelist.includes('audio')
    ? '素材库还没有可选的素材。'
    : '素材库还没有可用的图片/视频。',
)

async function refresh(): Promise<void> {
  if (view.value === 'character') {
    await refreshCharacters()
    return
  }
  loading.value = true
  error.value = ''
  try {
    assets.value = await client.listAssets(listParams(0))
  } catch (e) {
    error.value = e instanceof ApiError ? e.message : '无法加载素材,请稍后再试'
  } finally {
    loading.value = false
  }
}

async function refreshCharacters(): Promise<void> {
  if (props.ipId === undefined) {
    return
  }
  charactersLoading.value = true
  charactersError.value = ''
  try {
    characters.value = await client.listIPCharacters(props.ipId)
  } catch (e) {
    charactersError.value = e instanceof ApiError ? e.message : '无法加载角色,请稍后再试'
  } finally {
    charactersLoading.value = false
  }
}

function setView(v: 'asset' | 'character'): void {
  view.value = v
  void refresh()
}

async function loadMore(): Promise<void> {
  if (loading.value) {
    return
  }
  loading.value = true
  error.value = ''
  try {
    const more = await client.listAssets(listParams(assets.value.length))
    assets.value.push(...more)
  } catch (e) {
    error.value = e instanceof ApiError ? e.message : '无法加载更多素材,请稍后再试'
  } finally {
    loading.value = false
  }
}

function setKind(k: '' | PickerKind): void {
  kind.value = k
  void refresh()
}

function setScope(s: 'ip' | 'all'): void {
  scope.value = s
  void refresh()
}

function kindLabel(k: PickerKind): string {
  return k === 'image' ? '图片' : k === 'video' ? '视频' : '音频'
}

// 图片条目缩略显示(49 号票):onerror 回落 content_url;点选/取消链路
// 只传 AssetRecord,不受显示面影响。
const thumbs = useThumbFallback()

const isSelected = (a: AssetRecord): boolean => props.selectedAssetIds.includes(a.id)

function onPick(a: AssetRecord): void {
  if (!props.kindWhitelist.includes(a.kind)) {
    return
  }
  emit('toggle', a)
}

/** 角色设定图 → 合成 AssetRecord:设定图引用 = 既有 asset_id + 内容寻址
 * (51 号票,服务端零改动);chip 不带角色标注 —— 载荷与普通素材同形。
 * 角色视图只列图片,kind 恒 image。 */
function characterAsset(assetId: number): AssetRecord {
  return {
    id: assetId,
    kind: 'image',
    canvas_id: 0,
    canvas_name: '',
    task_id: '',
    model: '',
    prompt: '',
    url: '',
    content_type: 'image',
    size_bytes: 0,
    content_url: characterImageContentURL(assetId),
    thumbnail_url: characterImageThumbURL(assetId),
    ip_id: props.ipId ?? 0,
    ip_name: '',
    created_at: '',
    last_used_at: '',
  }
}

/** 角色视图的已选高亮按素材 id(同一素材锚进多个变体去重展示)。 */
const isCharacterAssetSelected = (assetId: number): boolean => props.selectedAssetIds.includes(assetId)

function onPickCharacterAsset(assetId: number): void {
  onPick(characterAsset(assetId))
}

/** 变体内展示序:视图类置前(51 号票主资产口径),组内保持 sort+id。 */
function orderedVariantImages(v: CharacterRecord['variants'][number]) {
  return [...v.images.filter((i) => isViewLabel(i.label)), ...v.images.filter((i) => !isViewLabel(i.label))]
}

/** 角色视图的缩略两阶段回落簿:缩略失败回原图,原图也失败落「素材不可用」
 * 占位(素材行被删的图行保留在树里,展示面按可达性降级)。 */
const brokenCharacterAssets = ref(new Set<number>())

function onCharacterThumbError(assetId: number): void {
  if (!thumbs.onThumbError(characterImageContentURL(assetId))) {
    brokenCharacterAssets.value = new Set([...brokenCharacterAssets.value, assetId])
  }
}

onMounted(refresh)
</script>

<template>
  <aside
    class="asset-picker"
    :aria-label="title"
  >
    <header class="panel-head">
      <h3>{{ title }}</h3>
      <div class="head-actions">
        <button
          class="close"
          type="button"
          @click="emit('close')"
        >
          关闭
        </button>
      </div>
    </header>
    <p
      v-if="hint"
      class="panel-hint"
    >
      {{ hint }}
    </p>

    <!-- 视图切换(51 号票):素材库平铺(缺省,47 号票行为不变)与
         角色 → 变体两级分组的设定图;只带 IP 归属且白名单含图片时出现。 -->
    <div
      v-if="showViewSwitch"
      class="view-switch"
    >
      <button
        type="button"
        :class="{ active: view === 'asset' }"
        @click="setView('asset')"
      >
        素材
      </button>
      <button
        type="button"
        :class="{ active: view === 'character' }"
        @click="setView('character')"
      >
        角色
      </button>
    </div>

    <template v-if="view === 'asset'">
      <!-- IP 归属视图切换(50 号票):缺省「本 IP + 全局」,可切全库。 -->
      <div
        v-if="showScopeToggle"
        class="scope-filter"
      >
        <button
          type="button"
          :class="{ active: scope === 'ip' }"
          @click="setScope('ip')"
        >
          本 IP + 全局
        </button>
        <button
          type="button"
          :class="{ active: scope === 'all' }"
          @click="setScope('all')"
        >
          全部
        </button>
      </div>

      <div
        v-if="kindTabs.length > 1"
        class="kind-filter"
      >
        <button
          v-for="k in kindTabs"
          :key="k"
          type="button"
          :class="{ active: kind === k }"
          @click="setKind(k)"
        >
          {{ k === '' ? '全部' : kindLabel(k) }}
        </button>
      </div>

      <p
        v-if="error"
        class="panel-error"
        role="alert"
      >
        {{ error }}
      </p>
      <p
        v-else-if="loading && assets.length === 0"
        class="panel-empty"
      >
        加载素材…
      </p>
      <p
        v-else-if="visibleAssets.length === 0"
        class="panel-empty"
      >
        {{ emptyText }}
      </p>

      <ul class="asset-list">
        <li
          v-for="a in visibleAssets"
          :key="a.id"
          class="asset-card"
          :class="{ selected: isSelected(a) }"
          :title="isSelected(a) ? '取消选择' : '选为参考'"
          @click="onPick(a)"
        >
          <img
            v-if="a.kind === 'image'"
            :src="thumbs.displayURL(a.content_url, a.thumbnail_url)"
            class="thumb"
            loading="lazy"
            alt="素材缩略图"
            @error="thumbs.onThumbError(a.content_url)"
          >
          <!-- 音频没有缩略图:给试听条占缩略位(素材面板同款);点击不触发
               卡片的选择/取消。 -->
          <audio
            v-else-if="a.kind === 'audio'"
            :src="a.content_url"
            class="audio-thumb"
            controls
            preload="none"
            @click.stop
          />
          <video
            v-else
            :src="a.content_url"
            class="thumb"
            muted
            preload="metadata"
          />
          <div class="meta">
            <span class="kind-tag">{{ kindLabel(a.kind) }}</span>
            <span
              class="prompt"
              :title="a.prompt"
            >{{ a.prompt || '(无提示词)' }}</span>
          </div>
          <span
            v-if="isSelected(a)"
            class="picked"
          >✓</span>
        </li>
      </ul>

      <button
        v-if="hasMore()"
        type="button"
        class="more"
        :disabled="loading"
        @click="loadMore"
      >
        {{ loading ? '加载中…' : '加载更多' }}
      </button>
    </template>

    <!-- 角色视图(51 号票):角色 → 变体两级分组列设定图,点选与普通素材
         同款 toggle(chip 不带角色标注,载荷 = asset_id + 内容寻址)。 -->
    <template v-else>
      <p
        v-if="charactersError"
        class="panel-error"
        role="alert"
      >
        {{ charactersError }}
      </p>
      <p
        v-else-if="charactersLoading"
        class="panel-empty"
      >
        加载角色…
      </p>
      <p
        v-else-if="characters.length === 0"
        class="panel-empty"
      >
        本 IP 还没有角色 —— 先在 IP 详情页创建。
      </p>
      <template v-else>
        <section
          v-for="c in characters"
          :key="c.id"
          class="character-group"
        >
          <h4 class="character-name">
            {{ c.name }}
            <span
              v-if="c.alias"
              class="character-alias"
            >{{ c.alias }}</span>
          </h4>
          <div
            v-for="v in c.variants"
            :key="v.id"
            class="variant-group"
          >
            <p class="variant-name">
              {{ v.name }}
              <span
                v-if="v.note"
                class="variant-note"
              >{{ v.note }}</span>
            </p>
            <ul
              v-if="v.images.length > 0"
              class="asset-list"
            >
              <li
                v-for="img in orderedVariantImages(v)"
                :key="img.id"
                class="asset-card"
                :class="{ selected: isCharacterAssetSelected(img.asset_id) }"
                :title="isCharacterAssetSelected(img.asset_id) ? '取消选择' : '选为参考'"
                @click="onPickCharacterAsset(img.asset_id)"
              >
                <span
                  v-if="brokenCharacterAssets.has(img.asset_id)"
                  class="character-unavailable"
                >
                  素材不可用
                </span>
                <img
                  v-else
                  :src="thumbs.displayURL(characterImageContentURL(img.asset_id), characterImageThumbURL(img.asset_id))"
                  class="thumb"
                  loading="lazy"
                  alt="设定图"
                  @error="onCharacterThumbError(img.asset_id)"
                >
                <div class="meta">
                  <span
                    class="kind-tag"
                    :class="{ 'view-label': isViewLabel(img.label) }"
                  >{{ img.label || '设定图' }}</span>
                </div>
                <span
                  v-if="isCharacterAssetSelected(img.asset_id)"
                  class="picked"
                >✓</span>
              </li>
            </ul>
            <p
              v-else
              class="variant-empty"
            >
              该变体还没有设定图
            </p>
          </div>
        </section>
      </template>
    </template>
  </aside>
</template>

<style scoped>
.asset-picker {
  position: absolute;
  top: 0;
  right: 0;
  bottom: 0;
  width: var(--w-panel);
  display: flex;
  flex-direction: column;
  gap: var(--sp-md);
  padding: var(--sp-lg);
  background: rgba(var(--surface-panel-rgb), 0.96);
  border-left: 1px solid var(--line-control);
  box-shadow: var(--shadow-slide);
  z-index: var(--z-side-panel);
  overflow-y: auto;
  animation: panel-in var(--dur-slow) var(--ease);
}

.panel-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: var(--sp-sm);
  padding-bottom: 10px;
  border-bottom: 1px solid var(--line-section);
}

.panel-head h3 {
  margin: 0;
  font-size: var(--fs-title);
}

.head-actions button {
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

.head-actions button:hover {
  border-color: var(--line-action);
  color: inherit;
}

.panel-hint {
  margin: 0;
  color: var(--text-muted);
  font-size: var(--fs-label);
}

.kind-filter,
.scope-filter {
  display: flex;
  gap: 4px;
}

.scope-filter button,
.kind-filter button,
.view-switch button {
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

.scope-filter button.active,
.kind-filter button.active,
.view-switch button.active {
  background: rgba(var(--accent-rgb), 0.25);
  color: inherit;
  border-color: rgba(var(--accent-rgb), 0.6);
}

/* 角色视图(51 号票):角色 → 变体两级分组。 */
.character-group {
  display: grid;
  gap: 8px;
  padding-bottom: 10px;
  border-bottom: 1px solid var(--line-section);
}

.character-name {
  margin: 0;
  font-size: var(--fs-body);
}

.character-alias {
  color: var(--text-muted);
  font-size: var(--fs-label);
  font-weight: 400;
  margin-left: 6px;
}

.variant-group .variant-name {
  margin: 0 0 6px;
  color: var(--text-secondary);
  font-size: var(--fs-label);
}

.variant-group .variant-note {
  color: var(--text-faint);
  margin-left: 6px;
}

.variant-empty {
  margin: 0;
  color: var(--text-faint);
  font-size: var(--fs-label);
}

.character-unavailable {
  display: grid;
  place-items: center;
  height: 84px;
  color: var(--text-muted);
  font-size: var(--fs-label);
  border: 1px dashed var(--line-control);
  border-radius: var(--r-md);
}

.kind-tag.view-label {
  color: var(--gold);
}

.panel-error,
.panel-empty {
  margin: 0;
  color: var(--text-muted);
  font-size: var(--fs-body);
}

.panel-error {
  color: var(--danger);
}

.asset-list {
  list-style: none;
  margin: 0;
  padding: 0;
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 8px;
}

.asset-card {
  position: relative;
  display: grid;
  gap: 4px;
  padding: 6px;
  background: var(--wash-subtle);
  border: 1px solid var(--line-section);
  border-radius: var(--r-lg);
  cursor: pointer;
}

.asset-card:hover {
  border-color: rgba(var(--accent-rgb), 0.5);
}

.asset-card.selected {
  border-color: rgba(var(--accent-rgb), 0.8);
  background: rgba(var(--accent-rgb), 0.12);
}

.thumb {
  width: 100%;
  height: 84px;
  object-fit: cover;
  border-radius: var(--r-md);
  background: var(--wash-soft);
  display: block;
}

/* 音频卡片:试听条占缩略位。 */
.audio-thumb {
  width: 100%;
  height: 36px;
  align-self: center;
}

.meta {
  display: grid;
  gap: 2px;
  min-width: 0;
}

.kind-tag {
  font-size: var(--fs-micro);
  color: var(--accent-soft);
}

.meta .prompt {
  font-size: var(--fs-label);
  color: var(--text-muted);
  overflow: hidden;
  display: -webkit-box;
  -webkit-line-clamp: 2;
  -webkit-box-orient: vertical;
}

.picked {
  position: absolute;
  top: 10px;
  right: 10px;
  width: 22px;
  height: 22px;
  display: grid;
  place-items: center;
  border-radius: var(--r-pill);
  background: var(--accent);
  color: var(--accent-ink);
  font-size: var(--fs-body);
  font-weight: 700;
}

.more {
  border: 1px solid var(--line-control);
  background: transparent;
  color: var(--text-secondary);
  border-radius: var(--r-md);
  padding: 7px;
  font-size: var(--fs-label);
  cursor: pointer;
}

.more:disabled {
  opacity: 0.4;
  cursor: not-allowed;
}
</style>
