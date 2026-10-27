<script setup lang="ts">
// 关卡卡(55 号票,第九种节点类型「规划」组):文档型工件 —— 名称 + 目标
// + 节拍数 + 画面缩略为卡面,「展开」就地展开全部字段编辑再收起(**卡内
// 直接编辑不开编辑层**,53 号票定案)。节拍增删 + 上移/下移按钮(序号显示
// 时生成 1..N 不持久,排序不做拖拽);画面槽两入口 = AssetPicker(scope=ip
// 默认,经编辑器右缘面板)+ 本机上传(挂画布 IP),**不提供落图**(概念
// 参考非生成锚点)。不渲染把手 —— 关卡卡不参与连线(文档卡无来源/投递
// 语义,与角色引用节点的「不连线」同族);素材被删落「素材不可用」占位
// (既有契约)。
import { computed, ref, watch } from 'vue'

import type { LevelNodeData } from '../../graph'
import {
  appendBeat,
  canAddLevelAsset,
  moveBeat,
  removeBeat,
} from '../../level'
import { frameContentURL } from '../../storyboard'
import { useThumbFallback } from '../../thumbnail'

const props = defineProps<{
  id: string
  type: string
  data: LevelNodeData
  /** 本机上传在途(编辑器级,任一在途即禁用上传入口)。 */
  uploading?: boolean
}>()

const emit = defineEmits<{
  /** 字段补丁(名称/目标/详情/节拍数组/画面槽数组):编辑器
   * updateNodeData 浅合并落盘,逐键上抛,自动保存免费获得。 */
  change: [patch: Partial<LevelNodeData>]
  /** 本机上传一张画面参考(编辑器入库后按素材 id 回填画面槽)。 */
  upload: [file: File]
  /** 打开素材挑选面板(编辑器右缘,scope=ip 默认)。 */
  'open-picker': []
}>()

// ---- 展开收起(卡内直接编辑的总开关,组件内 UI 态,不入节点 data)----

const expanded = ref(false)

// ---- 名称 / 目标 / 详细描述 ----

function onNameInput(event: Event): void {
  emit('change', { name: (event.target as HTMLInputElement).value })
}

function onGoalInput(event: Event): void {
  const value = (event.target as HTMLInputElement).value
  emit('change', value === '' ? { goal: undefined } : { goal: value })
}

function onDetailInput(event: Event): void {
  const value = (event.target as HTMLTextAreaElement).value
  emit('change', value === '' ? { detail: undefined } : { detail: value })
}

// ---- 节拍(增删移走 level.ts 纯函数,数组换新上抛)----

function onBeatInput(index: number, event: Event): void {
  const value = (event.target as HTMLInputElement).value
  emit('change', { beats: props.data.beats.map((b, i) => (i === index ? value : b)) })
}

function onAddBeat(): void {
  emit('change', { beats: appendBeat(props.data.beats) })
}

function onRemoveBeat(index: number): void {
  emit('change', { beats: removeBeat(props.data.beats, index) })
}

function onMoveBeat(index: number, delta: -1 | 1): void {
  emit('change', { beats: moveBeat(props.data.beats, index, delta) })
}

// ---- 画面槽(素材引用列表,上限 9;两入口 + 逐张移除)----

const slotsFull = computed(() => !canAddLevelAsset(props.data.asset_ids))

// 缩略加载失败簿(素材被删 = 「素材不可用」占位契约):asset_ids 换新
// (增删/撤销重放)即整簿复位重试。显示地址走两阶段回落(49 号票)。
const thumbs = useThumbFallback()
const brokenIds = ref(new Set<number>())
watch(
  () => props.data.asset_ids,
  () => {
    brokenIds.value = new Set()
  },
)

function markBroken(assetId: number): void {
  brokenIds.value = new Set([...brokenIds.value, assetId])
}

function onRemoveAsset(index: number): void {
  emit('change', { asset_ids: props.data.asset_ids.filter((_, i) => i !== index) })
}

// 本机上传:文件选择即上抛,入库与回填在编辑器侧(挂画布 IP,键落
// uploads/ 档);满员时入口禁用,编辑器侧同款预检兜底。
const uploadInput = ref<HTMLInputElement | null>(null)

function onUploadChange(event: Event): void {
  const input = event.target as HTMLInputElement
  const file = input.files?.[0]
  input.value = '' // 复位,同一文件下次选择仍触发 change。
  if (file) {
    emit('upload', file)
  }
}
</script>

<template>
  <div
    class="node level"
    title="关卡卡:卡内直接编辑"
  >
    <!-- 关卡名可编辑(缺省「新关卡」):输入框点击不触发节点拖拽。 -->
    <input
      class="level-name"
      :value="data.name"
      placeholder="新关卡"
      maxlength="128"
      title="关卡名"
      aria-label="关卡名"
      @click.stop
      @input="onNameInput"
    >

    <!-- 汇总行:节拍数 + 画面数。 -->
    <div class="summary">
      {{ data.beats.length }} 拍 · {{ data.asset_ids.length }} 图
    </div>

    <!-- 卡面:一句话目标(有则展示)。 -->
    <p
      v-if="data.goal"
      class="goal"
      :title="data.goal"
    >
      {{ data.goal }}
    </p>

    <!-- 卡面:画面缩略条(空槽无占位 —— asset_ids 只含已引用素材)。 -->
    <div
      v-if="data.asset_ids.length > 0"
      class="strip"
    >
      <span
        v-for="(assetId, i) in data.asset_ids"
        :key="assetId"
        class="thumb"
        :class="{
          missing: brokenIds.has(assetId),
          filled: !brokenIds.has(assetId),
        }"
        :title="brokenIds.has(assetId) ? '素材不可用' : `画面参考 ${i + 1}`"
      >
        <img
          v-if="!brokenIds.has(assetId)"
          :src="thumbs.displayURL(frameContentURL(assetId))"
          alt="画面参考缩略"
          loading="lazy"
          @error="markBroken(assetId)"
        >
        <span
          v-else
          class="thumb-text bad"
        >不可用</span>
      </span>
    </div>
    <div
      v-else
      class="placeholder"
    >
      <span>无画面参考</span>
      <small>展开后可从素材库挑选或本机上传(最多 9 张)</small>
    </div>

    <!-- 展开/收起:就地展开全部字段编辑,再点收起(不开编辑层)。 -->
    <button
      class="toggle"
      type="button"
      @click.stop="expanded = !expanded"
    >
      {{ expanded ? '收起' : '展开' }}
    </button>

    <!-- 展开态:目标 / 详细描述 / 节拍列表 / 画面槽管理。 -->
    <template v-if="expanded">
      <label class="field">
        <span class="field-label">一句话目标</span>
        <input
          class="field-input"
          :value="data.goal"
          placeholder="这个关卡要让玩家体验什么?"
          @click.stop
          @input="onGoalInput"
        >
      </label>

      <label class="field">
        <span class="field-label">详细描述</span>
        <textarea
          class="field-input detail"
          :value="data.detail"
          placeholder="地形机制、敌人布置、通关条件……"
          rows="4"
          @click.stop
          @input="onDetailInput"
        />
      </label>

      <!-- 流程节拍:有序列表,每拍一行;增删 + 上移/下移按钮(不拖拽),
           序号显示时生成 1..N 不持久。 -->
      <div class="field">
        <span class="field-label">流程节拍({{ data.beats.length }})</span>
        <ul class="beats">
          <li
            v-for="(beat, i) in data.beats"
            :key="i"
            class="beat-row"
          >
            <span class="beat-no">{{ i + 1 }}</span>
            <input
              class="beat-text"
              :value="beat"
              :aria-label="`节拍 ${i + 1}`"
              placeholder="这一拍发生什么"
              @click.stop
              @input="onBeatInput(i, $event)"
            >
            <span class="beat-actions">
              <button
                type="button"
                :disabled="i === 0"
                title="上移"
                aria-label="上移"
                @click.stop="onMoveBeat(i, -1)"
              >↑</button>
              <button
                type="button"
                :disabled="i === data.beats.length - 1"
                title="下移"
                aria-label="下移"
                @click.stop="onMoveBeat(i, 1)"
              >↓</button>
              <button
                type="button"
                title="删除节拍"
                aria-label="删除节拍"
                @click.stop="onRemoveBeat(i)"
              >×</button>
            </span>
          </li>
        </ul>
        <button
          class="add-beat"
          type="button"
          @click.stop="onAddBeat"
        >
          + 节拍
        </button>
      </div>

      <!-- 画面槽:缩略 + 逐张移除;两入口(素材库挑选 / 本机上传),
           不提供落图(概念参考非生成锚点);满员两入口禁用。 -->
      <div class="field">
        <span class="field-label">画面参考({{ data.asset_ids.length }}/9)</span>
        <div
          v-if="data.asset_ids.length > 0"
          class="slots"
        >
          <span
            v-for="(assetId, i) in data.asset_ids"
            :key="assetId"
            class="slot"
            :class="{ missing: brokenIds.has(assetId) }"
            :title="brokenIds.has(assetId) ? '素材不可用' : `画面参考 ${i + 1}`"
          >
            <img
              v-if="!brokenIds.has(assetId)"
              :src="thumbs.displayURL(frameContentURL(assetId))"
              alt="画面参考"
              loading="lazy"
              @error="markBroken(assetId)"
            >
            <span
              v-else
              class="thumb-text bad"
            >不可用</span>
            <button
              class="slot-remove"
              type="button"
              title="移除画面参考"
              :aria-label="`移除画面参考 ${i + 1}`"
              @click.stop="onRemoveAsset(i)"
            >×</button>
          </span>
        </div>
        <p
          v-else
          class="slots-empty"
        >
          还没有画面参考
        </p>
        <div class="slot-entries">
          <button
            class="entry"
            type="button"
            :disabled="slotsFull"
            :title="slotsFull ? '画面槽已满(最多 9 张)' : '从素材库挑选画面参考'"
            @click.stop="emit('open-picker')"
          >
            素材库
          </button>
          <button
            class="entry"
            type="button"
            :disabled="slotsFull || uploading"
            :title="slotsFull ? '画面槽已满(最多 9 张)' : uploading ? '上传中…' : '本机上传一张图'"
            @click.stop="uploadInput?.click()"
          >
            {{ uploading ? '上传中…' : '本机上传' }}
          </button>
          <input
            ref="uploadInput"
            class="upload-input"
            type="file"
            accept="image/*"
            @change="onUploadChange"
          >
        </div>
      </div>
    </template>
  </div>
</template>

<style scoped>
.node {
  width: 340px;
  background: rgba(var(--surface-card-rgb), 0.92);
  border: 1px solid rgba(var(--red-signal-rgb), 0.4);
  border-radius: var(--r-xl);
  padding: 10px 12px;
  font-size: var(--fs-body);
}

.level-name {
  width: 100%;
  box-sizing: border-box;
  background: transparent;
  border: none;
  border-bottom: 1px solid transparent;
  border-radius: var(--r-sm);
  padding: 2px 4px;
  color: var(--red-signal);
  font-size: var(--fs-body);
  font-weight: 600;
}

.level-name::placeholder {
  color: var(--red-signal);
  opacity: 0.9;
}

.level-name:hover {
  border-bottom-color: rgba(var(--red-signal-rgb), 0.35);
}

.level-name:focus {
  outline: none;
  background: var(--wash-soft);
  border-bottom-color: rgba(var(--red-signal-rgb), 0.6);
  color: inherit;
}

.summary {
  margin: 6px 0 8px;
  color: var(--text-muted);
  font-size: var(--fs-label);
}

.goal {
  margin: 0 0 8px;
  color: var(--text-bright);
  font-size: var(--fs-label);
  overflow: hidden;
  display: -webkit-box;
  -webkit-line-clamp: 2;
  -webkit-box-orient: vertical;
}

.strip {
  display: flex;
  gap: 6px;
  overflow-x: auto;
  padding-bottom: 4px;
}

.thumb {
  flex: 0 0 auto;
  display: grid;
  place-items: center;
  width: 56px;
  height: 40px;
  border-radius: var(--r-sm);
  border: 1px dashed var(--line-action);
  background: var(--wash-subtle);
  overflow: hidden;
}

.thumb.filled {
  border-style: solid;
  border-color: var(--line-section);
}

.thumb.missing {
  border-color: rgba(var(--danger-rgb), 0.4);
}

.thumb img {
  width: 100%;
  height: 100%;
  object-fit: cover;
  display: block;
}

.thumb-text {
  font-size: var(--fs-micro);
  color: var(--text-faint);
}

.thumb-text.bad {
  font-size: var(--fs-micro);
  color: var(--danger);
}

:global(.vue-flow__node.selected) .thumb.filled,
:global(.vue-flow__node.selected) .slot {
  outline: 2px solid rgba(var(--accent-rgb), 0.8);
  outline-offset: 1px;
}

.placeholder {
  display: grid;
  gap: 4px;
  justify-items: center;
  padding: 18px 8px;
  border: 1px dashed var(--line-action);
  border-radius: var(--r-md);
  color: var(--text-muted);
}

.placeholder span {
  font-weight: 600;
}

.placeholder small {
  font-size: var(--fs-micro);
}

.toggle {
  margin-top: 8px;
  border: 1px solid var(--line-control);
  background: transparent;
  color: var(--text-secondary);
  border-radius: var(--r-md);
  padding: 3px 12px;
  font-size: var(--fs-label);
  cursor: pointer;
}

.toggle:hover {
  border-color: rgba(var(--red-signal-rgb), 0.5);
  color: inherit;
}

/* 展开态的字段区:分隔线拉开卡面与编辑区。 */
.field {
  display: grid;
  gap: 4px;
  margin-top: 10px;
  padding-top: 8px;
  border-top: 1px solid var(--line-section);
}

.field-label {
  color: var(--text-faint);
  font-size: var(--fs-micro);
  font-weight: 600;
}

.field-input {
  width: 100%;
  box-sizing: border-box;
  background: var(--wash-soft);
  border: 1px solid var(--line-control);
  border-radius: var(--r-sm);
  padding: 5px 8px;
  color: inherit;
  font-size: var(--fs-label);
}

.field-input:focus {
  outline: none;
  border-color: rgba(var(--red-signal-rgb), 0.5);
}

.field-input.detail {
  resize: vertical;
  min-height: 64px;
  font-family: inherit;
  line-height: 1.5;
}

.beats {
  list-style: none;
  margin: 0;
  padding: 0;
  display: grid;
  gap: 4px;
}

.beat-row {
  display: flex;
  align-items: center;
  gap: 4px;
}

.beat-no {
  flex: 0 0 18px;
  text-align: center;
  color: var(--text-faint);
  font-size: var(--fs-micro);
}

.beat-text {
  flex: 1;
  min-width: 0;
  background: var(--wash-soft);
  border: 1px solid var(--line-control);
  border-radius: var(--r-sm);
  padding: 4px 6px;
  color: inherit;
  font-size: var(--fs-label);
}

.beat-text:focus {
  outline: none;
  border-color: rgba(var(--red-signal-rgb), 0.5);
}

.beat-actions {
  display: flex;
  gap: 2px;
}

.beat-actions button {
  border: 1px solid var(--line-control);
  background: transparent;
  color: var(--text-secondary);
  border-radius: 5px;
  width: 20px;
  height: 20px;
  padding: 0;
  font-size: var(--fs-micro);
  line-height: 1;
  cursor: pointer;
}

.beat-actions button:hover:not(:disabled) {
  border-color: rgba(var(--red-signal-rgb), 0.5);
  color: inherit;
}

.beat-actions button:disabled {
  opacity: 0.35;
  cursor: default;
}

.add-beat {
  justify-self: start;
  border: 1px dashed var(--line-action);
  background: transparent;
  color: var(--text-secondary);
  border-radius: var(--r-sm);
  padding: 3px 10px;
  font-size: var(--fs-label);
  cursor: pointer;
}

.add-beat:hover {
  border-color: rgba(var(--red-signal-rgb), 0.5);
  color: inherit;
}

.slots {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
}

.slot {
  position: relative;
  display: grid;
  place-items: center;
  width: 64px;
  height: 46px;
  border-radius: var(--r-sm);
  border: 1px solid var(--line-control);
  background: var(--wash-subtle);
  overflow: hidden;
}

.slot.missing {
  border-color: rgba(var(--danger-rgb), 0.4);
}

.slot img {
  width: 100%;
  height: 100%;
  object-fit: cover;
  display: block;
}

.slot-remove {
  position: absolute;
  top: 1px;
  right: 1px;
  width: 16px;
  height: 16px;
  border: none;
  border-radius: var(--r-sm);
  background: rgba(var(--surface-panel-rgb), 0.85);
  color: var(--danger);
  font-size: var(--fs-micro);
  line-height: 1;
  padding: 0;
  cursor: pointer;
}

.slot-remove:hover {
  background: rgba(var(--danger-rgb), 0.6);
  color: var(--text-primary);
}

.slots-empty {
  margin: 0;
  color: var(--text-faint);
  font-size: var(--fs-label);
}

.slot-entries {
  display: flex;
  gap: 6px;
}

.slot-entries .entry {
  border: 1px solid var(--line-control);
  background: transparent;
  color: var(--text-secondary);
  border-radius: var(--r-sm);
  padding: 3px 10px;
  font-size: var(--fs-label);
  cursor: pointer;
}

.slot-entries .entry:hover:not(:disabled) {
  border-color: rgba(var(--red-signal-rgb), 0.5);
  color: inherit;
}

.slot-entries .entry:disabled {
  opacity: 0.45;
  cursor: default;
}

.upload-input {
  display: none;
}
</style>
