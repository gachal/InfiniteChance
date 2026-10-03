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
  RECORDS_LIMIT,
  recordKindLabel,
  recordStatusLabel,
  recordThumbURL,
  truncatePrompt,
} from '../records'

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
const unavailable = reactive(new Set<string>())

function toggleExpand(task: CanvasTask): void {
  expandedId.value = expandedId.value === task.id ? '' : task.id
}

/** 成功行的缩略图地址;已判失联的行不再发请求(空串由模板落占位)。 */
function thumbURL(task: CanvasTask): string {
  return unavailable.has(task.id) ? '' : recordThumbURL(task)
}

function onThumbError(task: CanvasTask): void {
  unavailable.add(task.id)
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
        <!-- 缩略位:成功且地址可达走媒体预览;失败/取消/排队/生成中与失联行
             落 kind 徽章占位。 -->
        <img
          v-if="thumbURL(task) !== '' && task.kind !== 'video'"
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
  background: rgba(13, 21, 36, 0.96);
  border-left: 1px solid rgba(255, 255, 255, 0.1);
  z-index: 6;
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
  color: #5b627a;
  font-size: 11px;
}

.panel-empty {
  margin: 0;
  color: #8b91a7;
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
  background: rgba(255, 255, 255, 0.04);
  border: 1px solid rgba(255, 255, 255, 0.08);
  border-radius: 10px;
  cursor: pointer;
}

.record-card:hover {
  border-color: rgba(255, 255, 255, 0.2);
}

.thumb {
  grid-row: span 2;
  width: 72px;
  height: 72px;
  object-fit: cover;
  border-radius: 8px;
  background: rgba(255, 255, 255, 0.05);
}

/* 失败/取消/排队/生成中的行没有产物可看:kind 徽章占位。 */
.thumb-badge {
  display: grid;
  place-items: center;
  font-size: 14px;
  font-weight: 600;
  color: #8b91a7;
  background: rgba(255, 255, 255, 0.05);
}

/* 成功但素材已删/地址过期的行:失联占位,不再尝试加载。 */
.thumb-broken {
  display: grid;
  place-items: center;
  font-size: 11px;
  color: #5b627a;
  background: rgba(255, 255, 255, 0.03);
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
  font-size: 11px;
}

.kind {
  padding: 1px 6px;
  border-radius: 6px;
  background: rgba(255, 255, 255, 0.08);
  color: #aab1c5;
}

/* 透明背景徽章(37 号票):与 kind 徽章同族的浅色小标。 */
.bg-flag {
  padding: 1px 6px;
  border-radius: 6px;
  background: rgba(122, 162, 247, 0.18);
  color: #a5b4fc;
}

.status {
  font-weight: 600;
}

.status.st-succeeded {
  color: #4ade80;
}

.status.st-failed {
  color: #ff8f8f;
}

.status.st-canceled {
  color: #8b91a7;
}

.status.st-queued,
.status.st-running {
  color: #7aa2f7;
}

.time {
  margin-left: auto;
  color: #5b627a;
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
  font-size: 11px;
  color: #8b91a7;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.error {
  font-size: 11px;
  color: #ff8f8f;
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
  border-radius: 8px;
  padding: 5px 12px;
  font-size: 12px;
  cursor: pointer;
  background: rgba(76, 110, 245, 0.25);
  color: #a5b4fc;
  font-weight: 600;
}
</style>
