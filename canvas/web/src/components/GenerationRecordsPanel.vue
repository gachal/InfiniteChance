<script setup lang="ts">
// 生成记录面板(28 号票):画布内任务流水的可查面板 —— 数据零后端改动,
// 直接渲染编辑器喂进来的 GET /canvases/:id/tasks 快照(新到旧,上限 200,
// 顶部注明)。行 = 缩略图/kind 徽章/提示词(截断可展开)/模型/状态/时间,
// 失败行内联 error 摘要。面板保持只读,只有两个动作:点行定位画布节点
// (绑定节点已被删时静默无操作,由编辑器裁决)、成功行「插入画布」(复用
// 素材面板的内容寻址插入语义);素材行已删 → 缩略图加载失败翻成不可用占
// 位并收走插入按钮。不做面板内重试/取消(节点卡片已有)。
import { ref, reactive } from 'vue'

import type { CanvasTask } from '@infinitechance/api'

import {
  canInsertRecord,
  formatRecordTime,
  isAudioRecord,
  RECORDS_LIMIT,
  recordKindLabel,
  recordStatusLabel,
  recordThumbURL,
  truncatePrompt,
} from '../records'
import { useThumbFallback } from '../thumbnail'

const props = defineProps<{
  /** 当前画布的任务流水,新到旧(与任务轮询同拍刷新:编辑器经
   * useCanvasTasks 的 onTasks 整表回调喂进来)。 */
  tasks: CanvasTask[]
}>()

const emit = defineEmits<{
  /** 点行:请求编辑器平移定位并选中绑定节点。 */
  locate: [task: CanvasTask]
  /** 行内「插入画布」:复用素材面板的插入语义。 */
  insert: [task: CanvasTask]
}>()

/** 收起态提示词的截断长度(点击行内提示词区展开全文,再点收起)。 */
const PROMPT_LIMIT = 60
/** 失败原因摘要的截断长度(title 悬浮可见全文)。 */
const ERROR_LIMIT = 80

// 一次只展开一行(手风琴):记录面板要的是扫读,不是对照阅读。
const expandedId = ref('')

// 缩略图加载失败的任务行(素材被删/厂商临时地址过期):翻成不可用占位并
// 收走插入按钮。AssetPanel 同款信任 content 地址、按需降级的纪律。
// 49 号票起图片行缩略先行:缩略失败先回落原图,原图也失败才落「已失联」;
// 视频行不换(首帧 <video> 语义不动),asset_id=0 的厂商直连行本就派生
// 不出缩略地址,保持原图。
const unavailable = reactive(new Set<string>())
const thumbs = useThumbFallback()

function toggleExpand(task: CanvasTask): void {
  expandedId.value = expandedId.value === task.id ? '' : task.id
}

/** 成功行的缩略图地址;已判失联的行不再发请求(空串由模板落占位)。图片
 * 行走缩略派生,视频行与厂商直连行保持原地址。 */
function thumbURL(task: CanvasTask): string {
  if (unavailable.has(task.id)) {
    return ''
  }
  const original = recordThumbURL(task)
  return task.kind === 'video' ? original : thumbs.displayURL(original)
}

function onThumbError(task: CanvasTask): void {
  if (task.kind === 'video' || !thumbs.onThumbError(recordThumbURL(task))) {
    unavailable.add(task.id)
  }
}

/** 行内插入按钮是否出现:成功、有素材引用、且地址未失联。 */
function insertable(task: CanvasTask): boolean {
  return canInsertRecord(task) && !unavailable.has(task.id)
}

function statusClass(task: CanvasTask): string {
  return `st-${task.status}`
}
</script>

<template>
  <aside
    class="records-panel"
    aria-label="生成记录"
  >
    <header class="panel-head">
      <h3>生成记录</h3>
      <span class="limit-note">最近 {{ RECORDS_LIMIT }} 条</span>
    </header>

    <p
      v-if="tasks.length === 0"
      class="panel-empty"
    >
      还没有生成记录。提交第一个生成任务后会出现在这里。
    </p>

    <ul class="record-list">
      <li
        v-for="task in props.tasks"
        :key="task.id"
        class="record-card"
        @click="emit('locate', task)"
      >
        <!-- 缩略位:成功且地址可达走媒体预览;音频行(57 号票 tts)给
             内联试听条占缩略位,失联/失败/排队态照旧落徽章占位。 -->
        <audio
          v-if="isAudioRecord(task) && !unavailable.has(task.id)"
          :src="thumbURL(task)"
          class="thumb audio-line"
          controls
          preload="none"
          @error="onThumbError(task)"
          @click.stop
        />
        <img
          v-else-if="thumbURL(task) !== '' && task.kind !== 'video'"
          :src="thumbURL(task)"
          class="thumb"
          loading="lazy"
          alt="产物缩略图"
          @error="onThumbError(task)"
        >
        <video
          v-else-if="thumbURL(task) !== ''"
          :src="thumbURL(task)"
          class="thumb"
          muted
          preload="metadata"
          @error="onThumbError(task)"
        />
        <div
          v-else-if="unavailable.has(task.id) && task.status === 'succeeded'"
          class="thumb thumb-broken"
        >
          已失联
        </div>
        <div
          v-else
          class="thumb thumb-badge"
        >
          {{ recordKindLabel(task.kind) }}
        </div>

        <div class="meta">
          <span class="row-1">
            <span class="kind">{{ recordKindLabel(task.kind) }}</span>
            <span
              v-if="task.background === 'transparent'"
              class="bg-flag"
              title="透明背景(37 号票)"
            >透明</span>
            <span
              class="status"
              :class="statusClass(task)"
            >{{ recordStatusLabel(task.status) }}</span>
            <span class="time">{{ formatRecordTime(task.created_at) }}</span>
          </span>
          <button
            type="button"
            class="prompt"
            :title="task.prompt || '(无提示词)'"
            @click.stop="toggleExpand(task)"
          >
            {{ expandedId === task.id ? task.prompt.trim() || '(无提示词)' : truncatePrompt(task.prompt, PROMPT_LIMIT) }}
          </button>
          <span class="model">{{ task.model || '未记录模型' }}</span>
          <span
            v-if="task.status === 'failed' && task.error !== ''"
            class="error"
            :title="task.error"
          >失败原因:{{ truncatePrompt(task.error, ERROR_LIMIT) }}</span>
        </div>

        <div class="actions">
          <button
            v-if="insertable(task)"
            type="button"
            class="insert"
            title="再放一份到当前画布(内容寻址复用,不复制字节)"
            @click.stop="emit('insert', task)"
          >
            插入画布
          </button>
        </div>
      </li>
    </ul>
  </aside>
</template>

<style scoped>
.records-panel {
  position: absolute;
  top: 0;
  right: 0;
  bottom: 0;
  width: 320px;
  display: flex;
  flex-direction: column;
  gap: 10px;
  padding: 14px;
  background: rgba(var(--surface-panel-rgb), 0.96);
  border-left: 1px solid var(--line-10);
  z-index: var(--z-side-panel);
  overflow-y: auto;
}

.panel-head {
  display: flex;
  align-items: baseline;
  justify-content: space-between;
  gap: 8px;
}

.panel-head h3 {
  margin: 0;
  font-size: 15px;
}

.limit-note {
  color: #8f97ad;
  font-size: 12px;
}

.panel-empty {
  margin: 0;
  color: var(--text-muted);
  font-size: 13px;
}

.record-list {
  list-style: none;
  margin: 0;
  padding: 0;
  display: grid;
  gap: 10px;
}

.record-card {
  display: grid;
  grid-template-columns: 72px 1fr;
  gap: 8px;
  padding: 8px;
  background: var(--wash-04);
  border: 1px solid var(--line-08);
  border-radius: var(--r-lg);
  cursor: pointer;
}

.record-card:hover {
  border-color: var(--line-20);
}

.thumb {
  grid-row: span 2;
  width: 72px;
  height: 72px;
  object-fit: cover;
  border-radius: var(--r-md);
  background: var(--wash-05);
}

/* 失败/取消/排队/生成中的行没有产物可看:kind 徽章占位。 */
.audio-line {
  height: 32px;
}

.thumb-badge {
  display: grid;
  place-items: center;
  font-size: 14px;
  font-weight: 600;
  color: var(--text-muted);
  background: var(--wash-05);
}

/* 成功但素材已删/地址过期的行:失联占位,不再尝试加载。 */
.thumb-broken {
  display: grid;
  place-items: center;
  font-size: 12px;
  color: #8f97ad;
  background: var(--wash-03);
}

.meta {
  display: grid;
  gap: 3px;
  min-width: 0;
}

.row-1 {
  display: flex;
  align-items: center;
  gap: 6px;
  font-size: 12px;
}

.kind {
  padding: 1px 6px;
  border-radius: var(--r-sm);
  background: var(--wash-08);
  color: var(--text-secondary);
}

/* 透明背景徽章(37 号票):与 kind 徽章同族的浅色小标。 */
.bg-flag {
  padding: 1px 6px;
  border-radius: var(--r-sm);
  background: rgba(var(--accent-rgb), 0.18);
  color: var(--indigo-soft);
}

.status {
  font-weight: 600;
}

.status.st-succeeded {
  color: var(--success);
}

.status.st-failed {
  color: var(--danger);
}

.status.st-canceled {
  color: var(--text-muted);
}

.status.st-queued,
.status.st-running {
  color: var(--accent);
}

.time {
  margin-left: auto;
  color: #8f97ad;
}

.prompt {
  border: none;
  background: transparent;
  color: inherit;
  padding: 0;
  text-align: left;
  font-size: 12px;
  cursor: pointer;
  overflow: hidden;
  display: -webkit-box;
  -webkit-line-clamp: 2;
  -webkit-box-orient: vertical;
  white-space: pre-wrap;
  word-break: break-all;
}

.model {
  font-size: 12px;
  color: var(--text-muted);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.error {
  font-size: 12px;
  color: var(--danger);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.actions {
  grid-column: 1 / -1;
  display: flex;
  gap: 6px;
  min-height: 26px;
  align-items: center;
}

.insert {
  border: none;
  border-radius: var(--r-md);
  padding: 5px 12px;
  font-size: 12px;
  cursor: pointer;
  background: rgba(76, 110, 245, 0.25);
  color: var(--indigo-soft);
  font-weight: 600;
}
</style>
