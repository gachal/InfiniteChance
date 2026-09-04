<script setup lang="ts">
// 素材面板(14 号票):画布内浏览历史素材并把素材插入当前画布 —— 插入的
// 节点持有素材的内容寻址引用(asset_id + content_url),跨画布复用同一
// 素材而非复制字节。列表/过滤由服务端 /assets 提供,拉取失败不打扰画布。
import { onMounted, ref } from 'vue'

import { ApiError, type AssetRecord } from '@infinitechance/api'

import { useAuth } from '../auth'

const emit = defineEmits<{
  insert: [asset: AssetRecord]
}>()

const { client } = useAuth()

const assets = ref<AssetRecord[]>([])
const kind = ref<'' | 'image' | 'video'>('')
const loading = ref(false)
const error = ref('')

// 分页窗口:一页 30 条,「加载更多」向后翻页;上一页拉满才可能出现下一页。
const pageSize = 30

const hasMore = () => assets.value.length >= pageSize && assets.value.length % pageSize === 0

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

function downloadURL(a: AssetRecord): string {
  return `${a.content_url}?download=1`
}

onMounted(refresh)
</script>

<template>
  <aside
    class="asset-panel"
    aria-label="素材库"
  >
    <header class="panel-head">
      <h3>素材库</h3>
      <div class="kind-filter">
        <button
          v-for="k in (['', 'image', 'video'] as const)"
          :key="k"
          type="button"
          :class="{ active: kind === k }"
          @click="setKind(k)"
        >
          {{ k === '' ? '全部' : k === 'image' ? '图片' : '视频' }}
        </button>
      </div>
    </header>

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
      v-else-if="assets.length === 0"
      class="panel-empty"
    >
      素材库还是空的。生成图片或视频后会自动沉淀在这里。
    </p>

    <ul class="asset-list">
      <li
        v-for="a in assets"
        :key="a.id"
        class="asset-card"
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
          <span class="model">{{ a.model || '未记录模型' }}</span>
          <span
            class="prompt"
            :title="a.prompt"
          >{{ a.prompt || '(无提示词)' }}</span>
          <span class="origin">
            {{ a.kind === 'image' ? '图片' : '视频' }}
            <template v-if="a.canvas_name">· 来自「{{ a.canvas_name }}」</template>
          </span>
        </div>
        <div class="actions">
          <button
            type="button"
            class="insert"
            title="插入当前画布(跨画布复用同一素材)"
            @click="emit('insert', a)"
          >
            插入
          </button>
          <a
            class="download"
            :href="downloadURL(a)"
            :download="`asset-${a.id}-${a.kind}`"
          >下载</a>
        </div>
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
.asset-panel {
  position: absolute;
  top: 0;
  right: 0;
  bottom: 0;
  width: 320px;
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

.panel-error {
  margin: 0;
  color: #ff8f8f;
  font-size: 13px;
}

.panel-empty {
  margin: 0;
  color: #8b91a7;
  font-size: 13px;
}

.asset-list {
  list-style: none;
  margin: 0;
  padding: 0;
  display: grid;
  gap: 10px;
}

.asset-card {
  display: grid;
  grid-template-columns: 72px 1fr;
  gap: 8px;
  padding: 8px;
  background: rgba(255, 255, 255, 0.04);
  border: 1px solid rgba(255, 255, 255, 0.08);
  border-radius: 10px;
}

.thumb {
  grid-row: span 2;
  width: 72px;
  height: 72px;
  object-fit: cover;
  border-radius: 8px;
  background: rgba(255, 255, 255, 0.05);
}

.meta {
  display: grid;
  gap: 2px;
  min-width: 0;
}

.meta .model {
  font-size: 12px;
  font-weight: 600;
}

.meta .prompt {
  font-size: 11px;
  color: #8b91a7;
  overflow: hidden;
  display: -webkit-box;
  -webkit-line-clamp: 2;
  -webkit-box-orient: vertical;
}

.meta .origin {
  font-size: 11px;
  color: #5b627a;
}

.actions {
  grid-column: 1 / -1;
  display: flex;
  gap: 6px;
}

.actions button,
.actions a {
  border: none;
  border-radius: 8px;
  padding: 5px 12px;
  font-size: 12px;
  cursor: pointer;
  text-decoration: none;
  text-align: center;
}

.insert {
  background: rgba(76, 110, 245, 0.25);
  color: #a5b4fc;
  font-weight: 600;
}

.download {
  background: transparent;
  border: 1px solid rgba(255, 255, 255, 0.16);
  color: #aab1c5;
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
