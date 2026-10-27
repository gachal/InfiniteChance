<script setup lang="ts">
// 素材面板(14 号票):画布内浏览历史素材并把素材插入当前画布 —— 插入的
// 节点持有素材的内容寻址引用(asset_id + content_url),跨画布复用同一
// 素材而非复制字节。列表/过滤由服务端 /assets 提供,按最近使用排序
// (47 号票;管理端素材页保持创建序),拉取失败不打扰画布。
// 50 号票加 IP 归属过滤:scope 在「本 IP + 全局」与「全部」间切换(缺省
// 本 IP + 全局);ipId 声明「本 IP」;uploadIpId 提供时面板出上传入口
// (上传固定带该 IP —— 画布内 = 画布的 IP,IP 详情页 = 该 IP);selectable
// 关掉插入按钮(纯浏览形态,IP 详情页素材区用)。
import { computed, onMounted, ref } from 'vue'

import { ApiError, type AssetRecord } from '@infinitechance/api'

import { useAuth } from '../auth'
import { useThumbFallback } from '../thumbnail'
import { useAssetListQuery, type AssetListKind } from '../composables/useAssetList'

const props = withDefaults(
  defineProps<{
    /** 「本 IP」归属的 IP id(50 号票);缺省 = 无 IP 维度,恒全部。 */
    ipId?: number
    /** 提供时面板出上传入口,上传固定带该 IP。 */
    uploadIpId?: number
    /** false = 纯浏览,不出「插入」按钮。 */
    selectable?: boolean
  }>(),
  { ipId: undefined, uploadIpId: undefined, selectable: true },
)

const emit = defineEmits<{
  insert: [asset: AssetRecord]
}>()

const { client } = useAuth()

const assets = ref<AssetRecord[]>([])
const kind = ref<AssetListKind>('')
const loading = ref(false)
const error = ref('')
const uploading = ref(false)
const uploadInput = ref<HTMLInputElement | null>(null)

// 归属视图、列表参数与分页窗口与 AssetPicker 同一套(评审修复提取至
// useAssetListQuery,单一来源)。
const { scope, listParams, hasMore } = useAssetListQuery({
  ipId: () => props.ipId,
  kind,
  count: () => assets.value.length,
})

const scopeChoices = computed(() =>
  props.ipId !== undefined ? ([{ v: 'ip', label: '本 IP + 全局' }, { v: 'all', label: '全部' }] as const) : ([{ v: 'all', label: '全部' }] as const),
)

async function refresh(): Promise<void> {
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

function setKind(k: '' | 'image' | 'video' | 'audio'): void {
  kind.value = k
  void refresh()
}

function setScope(s: 'ip' | 'all'): void {
  scope.value = s
  void refresh()
}

/** 面板内上传(50 号票):固定带面板声明的 IP(缺省 = 全局,历史口径)。 */
async function onUploadChange(e: Event): Promise<void> {
  const input = e.target as HTMLInputElement
  const file = input.files?.[0]
  input.value = ''
  if (!file || uploading.value) {
    return
  }
  uploading.value = true
  error.value = ''
  try {
    const a = await client.uploadAsset(
      file,
      file.type.startsWith('video/') ? 'video' : file.type.startsWith('audio/') ? 'audio' : 'image',
      props.uploadIpId,
    )
    // 刷新取最新序;上传成功的行在最前(最近使用 = 创建时刻)。
    await refresh()
    if (props.selectable) {
      // 上传成功即可插入(与列表插入同语义);音频也落画布 —— 57 号票
      // 推翻 24 号票「音频只入库不插」(音频节点出现后插入有载体)。
      emit('insert', a)
    }
  } catch (e) {
    error.value = e instanceof ApiError ? e.message : '上传失败,请稍后再试'
  } finally {
    uploading.value = false
  }
}

function kindLabel(k: 'image' | 'video' | 'audio'): string {
  return k === 'image' ? '图片' : k === 'video' ? '视频' : '音频'
}

function downloadURL(a: AssetRecord): string {
  return `${a.content_url}?download=1`
}

// 图片缩略走 thumbnail_url(49 号票):缩略失败回落原图地址,下载/插入
// 链路不动。
const thumbs = useThumbFallback()

onMounted(refresh)
</script>

<template>
  <aside
    class="asset-panel"
    aria-label="素材库"
  >
    <header class="panel-head">
      <h3>素材库</h3>
      <div class="head-filters">
        <div
          v-if="ipId !== undefined"
          class="scope-filter"
        >
          <button
            v-for="c in scopeChoices"
            :key="c.v"
            type="button"
            :class="{ active: scope === c.v }"
            @click="setScope(c.v)"
          >
            {{ c.label }}
          </button>
        </div>
        <div class="kind-filter">
          <button
            v-for="k in (['', 'image', 'video', 'audio'] as const)"
            :key="k"
            type="button"
            :class="{ active: kind === k }"
            @click="setKind(k)"
          >
            {{ k === '' ? '全部' : kindLabel(k) }}
          </button>
        </div>
      </div>
    </header>

    <div
      v-if="uploadIpId !== undefined"
      class="upload-row"
    >
      <button
        type="button"
        class="upload"
        :disabled="uploading"
        @click="uploadInput?.click()"
      >
        {{ uploading ? '上传中…' : '上传本机图片/视频' }}
      </button>
      <input
        ref="uploadInput"
        type="file"
        accept="image/*,video/*,audio/*"
        class="sr-input"
        @change="onUploadChange"
      >
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
          :src="thumbs.displayURL(a.content_url, a.thumbnail_url)"
          class="thumb"
          loading="lazy"
          alt="素材缩略图"
          @error="thumbs.onThumbError(a.content_url)"
        >
        <video
          v-else-if="a.kind === 'video'"
          :src="a.content_url"
          class="thumb"
          muted
          preload="metadata"
        />
        <!-- 音频没有缩略图:面板内直接给 <audio> 试听;插入画布落音频
             节点(57 号票翻案 —— 音频节点出现后插入有载体)。 -->
        <audio
          v-else
          :src="a.content_url"
          class="audio-thumb"
          controls
          preload="none"
        />
        <div class="meta">
          <span class="model">{{ a.model || '未记录模型' }}</span>
          <span
            class="prompt"
            :title="a.prompt"
          >{{ a.prompt || '(无提示词)' }}</span>
          <span class="origin">
            {{ kindLabel(a.kind) }}
            <template v-if="a.canvas_name">· 来自「{{ a.canvas_name }}」</template>
            <template v-if="a.ip_id === 0">· 全局</template>
            <template v-else-if="a.ip_name">· {{ a.ip_name }}</template>
          </span>
        </div>
        <div
          v-if="selectable"
          class="actions"
        >
          <button
            type="button"
            class="insert"
            :title="a.kind === 'audio' ? '插入当前画布(落音频节点,57 号票)' : '插入当前画布(跨画布复用同一素材)'"
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

.head-filters {
  display: grid;
  gap: 6px;
  justify-items: end;
}

.scope-filter,
.kind-filter {
  display: flex;
  gap: 4px;
}

.scope-filter button,
.kind-filter button,
.upload {
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

.scope-filter button:hover,
.kind-filter button:hover,
.upload:hover {
  border-color: var(--line-action);
  color: var(--text-primary);
}

.scope-filter button.active,
.kind-filter button.active {
  background: rgba(var(--accent-rgb), 0.25);
  color: inherit;
  border-color: rgba(var(--accent-rgb), 0.6);
}

.upload-row {
  display: flex;
}

.upload {
  flex: 1;
  padding: 7px;
}

.upload:disabled {
  opacity: 0.4;
  cursor: default;
}

/* 面板内上传的隐藏文件输入(按钮代点)。 */
.sr-input {
  position: absolute;
  width: 1px;
  height: 1px;
  opacity: 0;
  pointer-events: none;
}

.panel-error {
  margin: 0;
  color: var(--danger);
  font-size: var(--fs-body);
}

.panel-empty {
  margin: 0;
  color: var(--text-muted);
  font-size: var(--fs-body);
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
  gap: var(--sp-sm);
  padding: var(--sp-sm);
  background: var(--wash-subtle);
  border: 1px solid var(--line-section);
  border-radius: var(--r-lg);
  transition: border-color var(--dur-fast) var(--ease);
}

.asset-card:hover {
  border-color: var(--line-action);
}

.thumb {
  grid-row: span 2;
  width: 72px;
  height: 72px;
  object-fit: cover;
  border-radius: var(--r-md);
  background: var(--wash-soft);
}

/* 音频卡片:试听条占缩略位,下方照常给元信息。 */
.audio-thumb {
  grid-row: span 2;
  width: 100%;
  height: 36px;
  align-self: center;
}

.meta {
  display: grid;
  gap: 2px;
  min-width: 0;
}

.meta .model {
  font-size: var(--fs-label);
  font-weight: 600;
}

.meta .prompt {
  font-size: var(--fs-label);
  color: var(--text-muted);
  overflow: hidden;
  display: -webkit-box;
  -webkit-line-clamp: 2;
  -webkit-box-orient: vertical;
}

.meta .origin {
  font-size: var(--fs-label);
  color: var(--text-muted);
}

.actions {
  grid-column: 1 / -1;
  display: flex;
  gap: 6px;
}

.actions button,
.actions a {
  border: none;
  border-radius: var(--r-md);
  padding: 5px 12px;
  font-size: var(--fs-label);
  cursor: pointer;
  text-decoration: none;
  text-align: center;
}

.insert {
  background: rgba(var(--accent-rgb), 0.25);
  color: var(--accent-soft);
  font-weight: 600;
}

.download {
  background: transparent;
  border: 1px solid var(--line-control);
  color: var(--text-secondary);
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
