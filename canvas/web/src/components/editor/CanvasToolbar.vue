<script setup lang="ts">
// 画布底部工具栏(58 号票阶段一自 CanvasEditorView 抽出):左侧快捷键
// 提示,右侧添加按钮分「创作/规划」两组 + 素材库/生成记录/上传容器动作。
// 全部动作上抛;上传文件选择器内聚在本组件,change 事件原样上抛。
import { ref } from 'vue'
import { NODE_TYPE_LABEL, type CanvasNodeType } from '../../graph'

defineProps<{
  assetPanelOpen: boolean
  recordsPanelOpen: boolean
  uploading: boolean
}>()

const emit = defineEmits<{
  add: [type: CanvasNodeType]
  toggleAsset: []
  toggleRecords: []
  uploadChange: [event: Event]
}>()

// 53 号票:添加按钮分「创作/规划」两组(组间分隔线纯展示分组)。
const CREATE_NODE_TYPES: CanvasNodeType[] = ['agent', 'image', 'video', 'previz', 'character', 'voice']
const PLAN_NODE_TYPES: CanvasNodeType[] = ['storyboard', 'name', 'level']

const uploadInput = ref<HTMLInputElement | null>(null)
</script>

<template>
  <footer class="toolbar">
    <span class="hint">+ 号连线(左接上游、右连下游);F 回全景;⌘Z 撤销;Enter 在对话框直接发送。</span>
    <span class="add-group">
      <span class="group-label">创作</span>
      <button
        v-for="t in CREATE_NODE_TYPES"
        :key="t"
        :class="`add-${t}`"
        type="button"
        @click="emit('add', t)"
      >
        + {{ NODE_TYPE_LABEL[t] }}
      </button>
      <span
        class="group-sep"
        aria-hidden="true"
      />
      <span class="group-label">规划</span>
      <button
        v-for="t in PLAN_NODE_TYPES"
        :key="t"
        :class="`add-${t}`"
        type="button"
        @click="emit('add', t)"
      >
        + {{ NODE_TYPE_LABEL[t] }}
      </button>
      <span
        class="group-sep"
        aria-hidden="true"
      />
      <button
        class="add-asset"
        type="button"
        :title="assetPanelOpen ? '关闭素材库面板' : '打开素材库:浏览历史素材并插入画布'"
        @click="emit('toggleAsset')"
      >
        {{ assetPanelOpen ? '收起素材库' : '素材库' }}
      </button>
      <button
        class="add-records"
        type="button"
        :title="recordsPanelOpen ? '关闭生成记录面板' : '打开生成记录:本画布的任务流水(最近 200 条)'"
        @click="emit('toggleRecords')"
      >
        {{ recordsPanelOpen ? '收起记录' : '生成记录' }}
      </button>
      <button
        class="add-upload"
        type="button"
        :disabled="uploading"
        :title="uploading ? '上传中…' : '上传本机图片/视频进素材库,并作为节点插入画布'"
        @click="uploadInput?.click()"
      >
        {{ uploading ? '上传中…' : '上传' }}
      </button>
      <input
        ref="uploadInput"
        class="upload-input"
        type="file"
        accept="image/*,video/*"
        @change="emit('uploadChange', $event)"
      >
    </span>
  </footer>
</template>

<style scoped>
.toolbar {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 16px;
  padding: 10px 16px;
  border-top: 1px solid var(--line-08);
  background: rgba(var(--surface-panel-rgb), 0.9);
}

.hint {
  color: var(--text-muted);
  font-size: 13px;
}

.add-group {
  display: flex;
  gap: 8px;
}

.add-group button {
  border: none;
  border-radius: var(--r-lg);
  padding: 9px 16px;
  font-size: 14px;
  font-weight: 600;
  cursor: pointer;
}

/* 53 号票:创作/规划两组的组名与组间分隔线(纯展示分组)。 */
.group-label {
  align-self: center;
  color: var(--text-faint);
  font-size: 12px;
  font-weight: 600;
  white-space: nowrap;
}

.group-sep {
  align-self: stretch;
  margin: 4px 2px;
  width: 1px;
  background: var(--wash-14);
}

.add-agent {
  background: rgba(var(--accent-rgb), 0.2);
  color: var(--accent);
}

.add-image {
  background: rgba(var(--success-rgb), 0.16);
  color: var(--success);
}

.add-video {
  background: rgba(var(--amber-rgb), 0.14);
  color: var(--amber);
}

.add-previz {
  background: rgba(var(--pink-rgb), 0.16);
  color: var(--pink);
}

/* 52 号票:角色引用节点入添加组,青色与节点描边/小地图同源。 */
.add-character {
  background: rgba(var(--teal-rgb), 0.16);
  color: var(--teal);
}

/* 57 号票:音色入「创作」组,水绿与节点描边/小地图同源(audio 不进组,
   只作台词合成产物与面板插入出现)。 */
.add-voice {
  background: rgba(var(--teal-light-rgb), 0.14);
  color: var(--teal-light);
}

/* 53 号票:絵コンテ入「规划」组,橙色与节点描边/小地图同源。 */
.add-storyboard {
  background: rgba(var(--orange-rgb), 0.16);
  color: var(--orange);
}

/* 54 号票:ネーム入「规划」组,天蓝与节点描边/小地图同源。 */
.add-name {
  background: rgba(var(--sky-rgb), 0.16);
  color: var(--sky);
}

/* 55 号票:关卡入「规划」组,红色与节点描边/小地图同源。 */
.add-level {
  background: rgba(var(--red-signal-rgb), 0.16);
  color: var(--red-signal);
}

/* 素材库按钮改中性蓝灰(38 号票评审:旧紫色与分析节点撞色,素材不是
   分析;类型色留给节点,容器类动作用中性色)。 */
.add-asset {
  background: rgba(168, 182, 216, 0.14);
  color: #b6c2d8;
}

.add-records {
  background: rgba(var(--orange-rgb), 0.14);
  color: #fdba74;
}

.add-upload {
  background: rgba(var(--teal-light-rgb), 0.14);
  color: var(--teal-light);
}

.add-upload:disabled {
  cursor: default;
  opacity: 0.6;
}

.upload-input {
  display: none;
}
</style>
