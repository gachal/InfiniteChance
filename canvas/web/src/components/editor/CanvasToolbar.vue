<script setup lang="ts">
// 画布底部工具栏(58 号票阶段一自 CanvasEditorView 抽出):左侧快捷键
// 提示,右侧添加按钮分「创作/规划」两组 + 素材库/生成记录/上传容器动作。
// 全部动作上抛;上传文件选择器内聚在本组件,change 事件原样上抛。
import { ref } from 'vue'
import { NODE_TYPE_LABEL, type AudioNodeData, type CanvasNodeType } from '../../graph'

defineProps<{
  assetPanelOpen: boolean
  recordsPanelOpen: boolean
  uploading: boolean
}>()

const emit = defineEmits<{
  /** 落一个新节点;dataPatch 允许入口携带初始创作意图(61 号票:音效入
   * 口落 audio 节点并带 source:'sfx',编辑器按它浅合并 initialData)。 */
  add: [type: CanvasNodeType, dataPatch?: Partial<AudioNodeData>]
  toggleAsset: []
  toggleRecords: []
  uploadChange: [event: Event]
}>()

// 53 号票:添加按钮分「创作/规划」两组(组间分隔线纯展示分组)。
// 60 号票:audio 以「配乐」入创作组(唯一的工具栏锚点,落创作态空音频
// 节点;台词合成产物与面板插入仍不在此列)。61 号票:「音效」是 audio
// 节点的第二个创作入口 —— 同落 audio 节点,但初始 data 带 source:'sfx'
// (创作意图标记,AudioNode 创作态按它分叉表单);配乐入口行为不变。
const CREATE_NODE_TYPES: CanvasNodeType[] = ['agent', 'image', 'video', 'previz', 'character', 'voice', 'audio']
const PLAN_NODE_TYPES: CanvasNodeType[] = ['storyboard', 'name', 'level']

/** 添加按钮的文案:audio 的工具栏语义是「配乐」(NODE_TYPE_LABEL 的
 * 「音频」是节点卡头的中性称谓,两者不混用)。 */
const ADD_LABELS: Partial<Record<CanvasNodeType, string>> = { audio: '配乐' }

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
        + {{ ADD_LABELS[t] ?? NODE_TYPE_LABEL[t] }}
      </button>
      <button
        class="add-sfx"
        type="button"
        title="落一个音效创作节点(≤30 秒短音频,按秒计费)"
        @click="emit('add', 'audio', { source: 'sfx' })"
      >
        + 音效
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
  gap: var(--sp-lg);
  padding: 10px 20px;
  border-top: 1px solid var(--line-section);
  background: rgba(var(--surface-panel-rgb), 0.9);
}

/* 提示行可让位:窄窗(1440 档)下单行省略,把横向空间让给按钮组。 */
.hint {
  color: var(--text-muted);
  font-size: var(--fs-body);
  flex: 1;
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.add-group {
  display: flex;
  gap: var(--sp-sm);
  flex-shrink: 0;
}

/* 阶段二质感:色底按钮配 1px 白系细描边(hover 提到可点击档),
   遵循「描边即轮廓规则」——不用 box-shadow 充当轮廓。 */
.add-group button {
  border: 1px solid var(--line-section);
  border-radius: var(--r-md);
  padding: 8px 14px;
  font-size: var(--fs-label);
  font-weight: 600;
  cursor: pointer;
  white-space: nowrap;
  transition:
    filter var(--dur-fast) var(--ease),
    border-color var(--dur-fast) var(--ease);
}

.add-group button:hover {
  filter: brightness(1.18);
  border-color: var(--line-action);
}

.add-group button:active {
  filter: brightness(0.92);
}

/* 53 号票:创作/规划两组的组名与组间分隔线(纯展示分组)。 */
.group-label {
  align-self: center;
  color: var(--text-faint);
  font-size: var(--fs-label);
  font-weight: 600;
  white-space: nowrap;
}

.group-sep {
  align-self: stretch;
  margin: 4px 2px;
  width: 1px;
  background: var(--line-control);
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

/* 57 号票:音色入「创作」组,水绿与节点描边/小地图同源。 */
.add-voice {
  background: rgba(var(--teal-light-rgb), 0.14);
  color: var(--teal-light);
}

/* 60 号票:配乐(audio 节点)入「创作」组,紫与节点描边同源。 */
.add-audio {
  background: rgba(var(--violet-rgb), 0.14);
  color: var(--violet);
}

/* 61 号票:音效同落 audio 节点,同紫色系(同为音频家族的创作入口),
   底色稍提一档与「配乐」区分。 */
.add-sfx {
  background: rgba(var(--violet-rgb), 0.24);
  color: var(--violet);
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
  background: rgba(var(--estimate-ink-rgb), 0.14);
  color: var(--estimate-ink);
}

.add-records {
  background: rgba(var(--orange-rgb), 0.14);
  color: var(--orange);
}

.add-upload {
  background: rgba(var(--teal-light-rgb), 0.14);
  color: var(--teal-light);
}

.add-upload:disabled {
  cursor: default;
  opacity: 0.4;
}

.upload-input {
  display: none;
}
</style>
