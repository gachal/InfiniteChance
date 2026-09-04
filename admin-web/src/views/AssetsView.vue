<script setup lang="ts">
// 素材管理(14 号票):画布管理区的素材库页 —— 按类型/来源画布过滤、
// 预览、下载、删除。素材挂在 canvas/server(对象文件在画布服务的存储卷
// 上),这里的列表与删除走 canvasClient;删除素材后引用它的画布节点会
// 显示占位而非报错。
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'

import { ApiError, type AssetRecord, type CanvasSummary } from '@infinitechance/api'

import { useAuth } from '../auth'
import AdminShell from '../components/AdminShell.vue'

const { canvasClient } = useAuth()

const assets = ref<AssetRecord[]>([])
const canvases = ref<CanvasSummary[]>([])
const kindFilter = ref<'' | 'image' | 'video'>('')
const canvasFilter = ref<number | ''>('')
const loading = ref(false)
const error = ref('')
const deletingId = ref<number | null>(null)

// 预览灯箱:点缩略图放大,视频可播放;Esc 或点背景关闭。
const preview = ref<AssetRecord | null>(null)

const pageSize = 30

const canvasName = computed(() => {
  const map = new Map(canvases.value.map((c) => [c.id, c.name]))
  return (id: number) => map.get(id) ?? ''
})

async function refresh(): Promise<void> {
  loading.value = true
  error.value = ''
  try {
    assets.value = await canvasClient.listAssets({
      kind: kindFilter.value === '' ? undefined : kindFilter.value,
      canvas_id: canvasFilter.value === '' ? undefined : canvasFilter.value,
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
    const more = await canvasClient.listAssets({
      kind: kindFilter.value === '' ? undefined : kindFilter.value,
      canvas_id: canvasFilter.value === '' ? undefined : canvasFilter.value,
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

const hasMore = () => assets.value.length >= pageSize && assets.value.length % pageSize === 0

function setKind(k: '' | 'image' | 'video'): void {
  kindFilter.value = k
  void refresh()
}

function setCanvasId(v: string): void {
  canvasFilter.value = v === '' ? '' : Number(v)
  void refresh()
}

// 管理页的媒体地址走 canvas-api 代理;下载用同一内容路由的 attachment 形态。
function contentURL(a: AssetRecord): string {
  return `/canvas-api/assets/${a.id}/content`
}

function downloadURL(a: AssetRecord): string {
  return `${contentURL(a)}?download=1`
}

async function remove(a: AssetRecord): Promise<void> {
  if (!window.confirm(`确定删除这条${a.kind === 'image' ? '图片' : '视频'}素材?对象文件会被一并清除,引用它的画布节点将显示占位。`)) {
    return
  }
  deletingId.value = a.id
  error.value = ''
  try {
    await canvasClient.deleteAsset(a.id)
    assets.value = assets.value.filter((x) => x.id !== a.id)
    if (preview.value?.id === a.id) {
      preview.value = null
    }
  } catch (e) {
    error.value = e instanceof ApiError ? e.message : '删除失败,请稍后再试'
  } finally {
    deletingId.value = null
  }
}

function formatSize(bytes: number): string {
  if (bytes <= 0) {
    return '—'
  }
  if (bytes >= 1 << 20) {
    return `${(bytes / (1 << 20)).toFixed(1)} MB`
  }
  return `${Math.max(1, Math.round(bytes / 1024))} KB`
}

function formatTime(iso: string): string {
  return new Date(iso).toLocaleString('zh-CN', { hour12: false })
}

function onKeydown(e: KeyboardEvent): void {
  if (e.key === 'Escape') {
    preview.value = null
  }
}

onMounted(() => {
  void refresh()
  void canvasClient
    .listCanvases()
    .then((list) => {
      canvases.value = list
    })
    .catch(() => {
      /* 来源画布下拉拉不到就保持空,不影响列表与过滤 */
    })
  window.addEventListener('keydown', onKeydown)
})

onBeforeUnmount(() => {
  window.removeEventListener('keydown', onKeydown)
})
</script>

<template>
  <AdminShell>
    <div class="toolbar">
      <h2>素材管理</h2>
      <div class="filters">
        <div class="kind-filter">
          <button
            v-for="k in (['', 'image', 'video'] as const)"
            :key="k"
            type="button"
            :class="{ active: kindFilter === k }"
            @click="setKind(k)"
          >
            {{ k === '' ? '全部类型' : k === 'image' ? '图片' : '视频' }}
          </button>
        </div>
        <select
          class="canvas-filter"
          :value="canvasFilter === '' ? '' : String(canvasFilter)"
          aria-label="按来源画布过滤"
          @change="setCanvasId(($event.target as HTMLSelectElement).value)"
        >
          <option value="">
            全部画布
          </option>
          <option
            v-for="c in canvases"
            :key="c.id"
            :value="String(c.id)"
          >
            {{ c.name }}
          </option>
        </select>
        <button
          type="button"
          class="ghost"
          :disabled="loading"
          @click="refresh"
        >
          刷新
        </button>
      </div>
    </div>

    <p
      v-if="error"
      class="error"
      role="alert"
    >
      {{ error }}
    </p>

    <p
      v-if="loading && assets.length === 0"
      class="muted"
    >
      正在加载素材…
    </p>
    <p
      v-else-if="assets.length === 0"
      class="muted"
    >
      没有符合条件的素材。画布上生成的图片和视频会自动进入素材库。
    </p>

    <div class="grid">
      <section
        v-for="a in assets"
        :key="a.id"
        class="card asset"
      >
        <button
          type="button"
          class="thumb-btn"
          title="点击预览"
          @click="preview = a"
        >
          <img
            v-if="a.kind === 'image'"
            :src="contentURL(a)"
            :alt="`素材 ${a.id}`"
            loading="lazy"
          >
          <video
            v-else
            :src="contentURL(a)"
            muted
            preload="metadata"
          />
        </button>
        <dl class="fields">
          <div>
            <dt>类型</dt>
            <dd>{{ a.kind === 'image' ? '图片' : '视频' }}</dd>
          </div>
          <div>
            <dt>来源画布</dt>
            <dd>{{ canvasName(a.canvas_id) || `画布 #${a.canvas_id}` }}</dd>
          </div>
          <div>
            <dt>模型</dt>
            <dd>{{ a.model || '—' }}</dd>
          </div>
          <div>
            <dt>大小</dt>
            <dd>{{ formatSize(a.size_bytes) }}</dd>
          </div>
          <div class="wide">
            <dt>提示词</dt>
            <dd class="prompt">
              {{ a.prompt || '—' }}
            </dd>
          </div>
          <div>
            <dt>生成时间</dt>
            <dd>{{ formatTime(a.created_at) }}</dd>
          </div>
        </dl>
        <div class="row-actions">
          <a
            class="ghost"
            :href="downloadURL(a)"
            :download="`asset-${a.id}-${a.kind}`"
          >下载</a>
          <button
            type="button"
            class="danger"
            :disabled="deletingId === a.id"
            @click="remove(a)"
          >
            {{ deletingId === a.id ? '删除中…' : '删除' }}
          </button>
        </div>
      </section>
    </div>

    <button
      v-if="hasMore()"
      type="button"
      class="ghost more"
      :disabled="loading"
      @click="loadMore"
    >
      {{ loading ? '加载中…' : '加载更多' }}
    </button>

    <div
      v-if="preview"
      class="lightbox"
      role="dialog"
      aria-label="素材预览"
      @click.self="preview = null"
    >
      <figure>
        <img
          v-if="preview.kind === 'image'"
          :src="contentURL(preview)"
          :alt="`素材 ${preview.id}`"
        >
        <video
          v-else
          :src="contentURL(preview)"
          controls
          autoplay
        />
        <figcaption>
          {{ preview.kind === 'image' ? '图片' : '视频' }} #{{ preview.id }}
          · {{ canvasName(preview.canvas_id) || `画布 #${preview.canvas_id}` }}
          <a
            :href="downloadURL(preview)"
            :download="`asset-${preview.id}-${preview.kind}`"
          >下载</a>
          <button
            type="button"
            class="danger"
            :disabled="deletingId === preview.id"
            @click="remove(preview)"
          >
            删除
          </button>
          <button
            type="button"
            class="ghost"
            @click="preview = null"
          >
            关闭
          </button>
        </figcaption>
      </figure>
    </div>
  </AdminShell>
</template>

<style scoped src="../components/admin-ui.css"></style>

<style scoped>
.filters {
  display: flex;
  align-items: center;
  gap: 10px;
  flex-wrap: wrap;
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
  padding: 6px 12px;
  font-size: 13px;
  cursor: pointer;
}

.kind-filter button.active {
  background: rgba(76, 110, 245, 0.25);
  color: inherit;
  border-color: rgba(76, 110, 245, 0.6);
}

.canvas-filter {
  background: rgba(255, 255, 255, 0.05);
  border: 1px solid rgba(255, 255, 255, 0.12);
  border-radius: 8px;
  padding: 7px 10px;
  color: inherit;
  font-size: 13px;
}

.grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(260px, 1fr));
  gap: 14px;
  margin-top: 14px;
}

.asset {
  display: grid;
  gap: 10px;
}

.thumb-btn {
  padding: 0;
  border: none;
  background: rgba(255, 255, 255, 0.04);
  border-radius: 10px;
  overflow: hidden;
  cursor: zoom-in;
}

.thumb-btn img,
.thumb-btn video {
  display: block;
  width: 100%;
  height: 160px;
  object-fit: cover;
}

.prompt {
  white-space: pre-wrap;
  word-break: break-word;
  display: -webkit-box;
  -webkit-line-clamp: 3;
  -webkit-box-orient: vertical;
  overflow: hidden;
}

.row-actions {
  display: flex;
  gap: 8px;
}

.row-actions a {
  text-decoration: none;
  text-align: center;
  padding: 6px 12px;
  border-radius: 8px;
  font-size: 13px;
}

.more {
  margin-top: 14px;
}

.lightbox {
  position: fixed;
  inset: 0;
  z-index: 50;
  display: grid;
  place-items: center;
  background: rgba(0, 0, 0, 0.75);
  padding: 32px;
}

.lightbox figure {
  margin: 0;
  max-width: min(880px, 92vw);
  display: grid;
  gap: 10px;
}

.lightbox img,
.lightbox video {
  display: block;
  max-width: 100%;
  max-height: 70vh;
  margin: 0 auto;
  border-radius: 12px;
}

.lightbox figcaption {
  display: flex;
  align-items: center;
  gap: 10px;
  justify-content: center;
  color: #d5d9e6;
  font-size: 14px;
}

.lightbox a {
  color: #a5b4fc;
}
</style>
