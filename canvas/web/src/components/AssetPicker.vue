<script setup lang="ts">
// 通用素材选择器(47 号票,32 号票 AgentMediaPicker 泛化):Agent 节点
// 「素材库」与生成对话框(图片/视频模式)共用一块右缘面板,复用既有
// GET /assets 列表 API,按最近使用排序(服务端解引用刷新,47 号票口径)。
// kind 白名单收窄可选面(视频模式含音频,Agent 附件仍限图/视频)、缩略
// 点选、可多选 —— 点选挂到调用方(ready 引用,上限由调用方按同款纪律
// 拒收),再点一次取消。
import { computed, onMounted, ref } from 'vue'

import { ApiError, type AssetRecord } from '@infinitechance/api'

import { useAuth } from '../auth'
import { useThumbFallback } from '../thumbnail'

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

// 分页窗口:一页 30 条,「加载更多」向后翻页(与素材面板同款)。
const pageSize = 30

const hasMore = () => assets.value.length >= pageSize && assets.value.length % pageSize === 0

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
  loading.value = true
  error.value = ''
  try {
    assets.value = await client.listAssets({
      kind: kind.value === '' ? undefined : kind.value,
      sort: 'last_used',
      limit: pageSize,
      offset: 0,
    })
  } catch (e) {
    error.value = e instanceof ApiError ? e.message : '无法加载素材,请稍后再试'
  } finally {
    loading.value = false
  }
}

async function loadMore(): Promise<void> {
  if (loading.value) {
    return
  }
  loading.value = true
  error.value = ''
  try {
    const more = await client.listAssets({
      kind: kind.value === '' ? undefined : kind.value,
      sort: 'last_used',
      limit: pageSize,
      offset: assets.value.length,
    })
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
  </aside>
</template>

<style scoped>
.asset-picker {
  position: absolute;
  top: 0;
  right: 0;
  bottom: 0;
  width: 340px;
  display: flex;
  flex-direction: column;
  gap: 10px;
  padding: 14px;
  background: rgba(13, 21, 36, 0.96);
  border-left: 1px solid rgba(255, 255, 255, 0.1);
  z-index: 6;
  overflow-y: auto;
}

.panel-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
}

.panel-head h3 {
  margin: 0;
  font-size: 15px;
}

.head-actions button {
  border: 1px solid rgba(255, 255, 255, 0.14);
  background: transparent;
  color: #aab1c5;
  border-radius: 8px;
  padding: 4px 10px;
  font-size: 12px;
  cursor: pointer;
}

.head-actions button:hover {
  border-color: rgba(255, 255, 255, 0.32);
  color: inherit;
}

.panel-hint {
  margin: 0;
  color: #8b91a7;
  font-size: 12px;
}

.kind-filter {
  display: flex;
  gap: 4px;
}

.kind-filter button {
  border: 1px solid rgba(255, 255, 255, 0.14);
  background: transparent;
  color: #aab1c5;
  border-radius: 8px;
  padding: 4px 10px;
  font-size: 12px;
  cursor: pointer;
}

.kind-filter button.active {
  background: rgba(76, 110, 245, 0.25);
  color: inherit;
  border-color: rgba(76, 110, 245, 0.6);
}

.panel-error,
.panel-empty {
  margin: 0;
  color: #8b91a7;
  font-size: 13px;
}

.panel-error {
  color: #ff8f8f;
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
  background: rgba(255, 255, 255, 0.04);
  border: 1px solid rgba(255, 255, 255, 0.08);
  border-radius: 10px;
  cursor: pointer;
}

.asset-card:hover {
  border-color: rgba(122, 162, 247, 0.5);
}

.asset-card.selected {
  border-color: rgba(122, 162, 247, 0.8);
  background: rgba(122, 162, 247, 0.12);
}

.thumb {
  width: 100%;
  height: 84px;
  object-fit: cover;
  border-radius: 8px;
  background: rgba(255, 255, 255, 0.05);
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
  font-size: 10px;
  color: #9fb4f9;
}

.meta .prompt {
  font-size: 12px;
  color: #8b91a7;
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
  border-radius: 999px;
  background: #7aa2f7;
  color: #10152a;
  font-size: 13px;
  font-weight: 700;
}

.more {
  border: 1px solid rgba(255, 255, 255, 0.16);
  background: transparent;
  color: #aab1c5;
  border-radius: 8px;
  padding: 7px;
  font-size: 12px;
  cursor: pointer;
}

.more:disabled {
  opacity: 0.5;
  cursor: not-allowed;
}
</style>
