<script setup lang="ts">
// 素材选择面板(32 号票):Agent 节点「素材库」按钮唤起的轻量选择面板,
// 复用既有 GET /assets 列表 API;按 kind 过滤(仅图片/视频,音频不可作
// 会话附件)、缩略点选、可多选 —— 点选即挂到该节点的待发附件(ready 引
// 用,上限由编辑器按同款纪律拒收),再点一次取消。生成对话框的素材库
// 挑选仍留「待补」,本面板不连带。
import { computed, onMounted, ref } from 'vue'

import { ApiError, type AssetRecord } from '@infinitechance/api'

import { useAuth } from '../auth'

const props = defineProps<{
  /** 已挂到该节点的素材 id(上传与库选两路一致),驱动已选高亮。 */
  selectedAssetIds: number[]
}>()

const emit = defineEmits<{
  toggle: [asset: AssetRecord]
  close: []
}>()

const { client } = useAuth()

const assets = ref<AssetRecord[]>([])
const kind = ref<'' | 'image' | 'video'>('')
const loading = ref(false)
const error = ref('')

// 分页窗口:一页 30 条,「加载更多」向后翻页(与素材面板同款)。
const pageSize = 30

const hasMore = () => assets.value.length >= pageSize && assets.value.length % pageSize === 0

// 音频不可作会话附件:列表就地过滤,不出现不可选的卡片。
const visibleAssets = computed(() => assets.value.filter((a) => a.kind !== 'audio'))

async function refresh(): Promise<void> {
  loading.value = true
  error.value = ''
  try {
    assets.value = await client.listAssets({
      kind: kind.value === '' ? undefined : kind.value,
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

function setKind(k: '' | 'image' | 'video'): void {
  kind.value = k
  void refresh()
}

function kindLabel(k: 'image' | 'video' | 'audio'): string {
  return k === 'image' ? '图片' : k === 'video' ? '视频' : '音频'
}

const isSelected = (a: AssetRecord): boolean => props.selectedAssetIds.includes(a.id)

function onPick(a: AssetRecord): void {
  if (a.kind !== 'image' && a.kind !== 'video') {
    return
  }
  emit('toggle', a)
}

onMounted(refresh)
</script>

<template>
  <aside
    class="agent-picker"
    aria-label="选择会话附件素材"
  >
    <header class="panel-head">
      <h3>选择参考素材</h3>
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
    <p class="panel-hint">
      点选挂为会话附件,再点取消;单条消息 ≤4 图 + ≤1 视频。
    </p>

    <div class="kind-filter">
      <button
        v-for="k in (['', 'image', 'video'] as const)"
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
      素材库还没有可用的图片/视频。
    </p>

    <ul class="asset-list">
      <li
        v-for="a in visibleAssets"
        :key="a.id"
        class="asset-card"
        :class="{ selected: isSelected(a) }"
        :title="isSelected(a) ? '取消选择' : '选为会话附件'"
        @click="onPick(a)"
      >
        <img
          v-if="a.kind === 'image'"
          :src="a.content_url"
          class="thumb"
          loading="lazy"
          alt="素材缩略图"
        >
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
.agent-picker {
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
  font-size: 11px;
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
