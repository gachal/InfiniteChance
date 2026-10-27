<script setup lang="ts">
// 絵コンテ全屏编辑层(53 号票,预演台编辑层 40 号票范式):编辑器级单
// 实例浮层(Teleport 到 body,ADR 0002 同族),全屏遮挡时画布不卸载,
// cut 改动整包走 scene-change → updateNodeData → autosave 落盘链。
// 垂直 cut 列表:序号(显示时生成 1..N,不持久)+ 画面槽(共享组件
// StoryboardFrameSlot:素材库/上传/落图三入口)+ 时长输入(失焦夹紧)+
// 台词/音效多行 + 运镜 chip 组(前端白名单多选)+ 一行备注 + 上移/下移/
// 删除;底部「+ cut」。cut 可删到空(板是画布工作件非库实体);排序用
// 按钮不做拖拽(40 号票编辑层手势冲突教训)。Esc/返回按钮双路等价,
// Esc 刻意走元素级(层上可再开编辑器级灯箱,两层同听 window 会一次全关)。
// 素材挑选面板(AssetPicker)挂在层右缘:点选即设为本 cut 画面,再点取消。
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'

import type { AssetRecord } from '@infinitechance/api'

import type { StoryboardCut, StoryboardNodeData } from '../graph'
import {
  CAMERA_MOVE_OPTIONS,
  appendCut,
  clampSecondsInput,
  moveCut,
  removeCut,
  toggleCameraMove,
} from '../storyboard'
import { followMove, followRemove } from '../list'
import AssetPicker from './AssetPicker.vue'
import StoryboardFrameSlot from './StoryboardFrameSlot.vue'

const props = defineProps<{
  data: StoryboardNodeData
  /** 「本 IP」归属(素材挑选面板的 scope=ip 缺省;编辑器传画布 IP)。 */
  ipId?: number
  /** 本机上传在途(编辑器级状态),画面槽上传按钮随之禁用。 */
  uploading: boolean
}>()

const emit = defineEmits<{
  /** cut/板名整包变更,编辑器 updateNodeData 落盘(数组换新)。 */
  'scene-change': [patch: { title: string; cuts: StoryboardCut[] }]
  /** 画面槽本机上传:编辑器入库后按素材 id 回填本 cut。 */
  upload: [cutIndex: number, file: File]
  /** 落图节点:cut 画面落为图片节点并连线(编辑器处理)。 */
  drop: [cutIndex: number]
  /** 关闭编辑层,返回画布(原位不丢)。 */
  close: []
}>()

/** cut 数据统一出口:整包上抛(数组换新,纯函数纪律),由编辑器落盘。 */
function emitScene(patch: Partial<{ title: string; cuts: StoryboardCut[] }> = {}): void {
  emit('scene-change', {
    title: patch.title ?? props.data.title ?? '',
    cuts: patch.cuts ?? props.data.cuts,
  })
}

function patchCut(index: number, patch: Partial<StoryboardCut>): void {
  emitScene({ cuts: props.data.cuts.map((c, i) => (i === index ? { ...c, ...patch } : c)) })
}

function onLinesInput(index: number, value: string): void {
  patchCut(index, { lines: value })
}

function onSecondsBlur(index: number, raw: string): void {
  // 时长失焦夹紧:空/非法回到未填(汇总时跳过),负数/越界/小数夹紧取整。
  patchCut(index, { seconds: clampSecondsInput(raw) })
}

function onNoteInput(index: number, value: string): void {
  patchCut(index, { note: value })
}

function onCameraChip(index: number, move: string): void {
  const cut = props.data.cuts[index]
  if (cut) {
    patchCut(index, toggleCameraMove(cut, move))
  }
}

function addCut(): void {
  emitScene({ cuts: appendCut(props.data.cuts) })
}

function removeCutAt(index: number): void {
  // 无「最后不可删」守卫:cut 可删到空(与角色变体守卫相反,53 号票定案)。
  emitScene({ cuts: removeCut(props.data.cuts, index) })
  // 面板下标跟随(与ネーム层同语义,list.ts 纯函数):删的正是面板行 →
  // 关面板;删更早的 cut → 面板行前移一位,不静默改绑下一 cut(评审修复)。
  pickerFor.value = followRemove(pickerFor.value, index)
}

function moveCutAt(index: number, delta: -1 | 1): void {
  const moved = moveCut(props.data.cuts, index, delta)
  emitScene({ cuts: moved })
  // 未动(越界交换)面板不动;动了则被移行跟到新位、被换的相邻行同步换位
  // (list.ts followMove,评审修复补齐相邻行跟随)。
  const p = pickerFor.value
  if (moved !== props.data.cuts && p !== null) {
    pickerFor.value = followMove(p, index, delta)
  }
}

// ---- 素材挑选面板(层右缘,AssetPicker 复用)----

/** 正在挑画面的 cut 下标(null = 面板关着)。 */
const pickerFor = ref<number | null>(null)

function openPicker(index: number): void {
  pickerFor.value = pickerFor.value === index ? null : index
}

/** 已选高亮 = 当前 cut 的画面素材;点选即设置(再点取消),无专用
 * 「设为分镜画面」动作(53 号票零新交互)。 */
const pickerSelectedIds = computed<number[]>(() => {
  const i = pickerFor.value
  if (i === null) {
    return []
  }
  const assetId = props.data.cuts[i]?.asset_id ?? 0
  return assetId > 0 ? [assetId] : []
})

function onPickerToggle(a: AssetRecord): void {
  const i = pickerFor.value
  if (i === null) {
    return
  }
  const current = props.data.cuts[i]?.asset_id ?? 0
  patchCut(i, { asset_id: current === a.id ? 0 : a.id })
}

// ---- 打开时收焦点、Esc 关闭并归还(PrevizEditorLayer 同款)----

const rootEl = ref<HTMLDivElement | null>(null)
let opener: HTMLElement | null = null

function onKeydown(event: KeyboardEvent): void {
  if (event.key === 'Escape') {
    // 层内素材挑选面板开着时先收面板(右缘面板 Esc 纪律同款),再关层。
    if (pickerFor.value !== null) {
      pickerFor.value = null
      return
    }
    emit('close')
  }
}

onMounted(() => {
  opener = (document.activeElement as HTMLElement | null) ?? null
  rootEl.value?.focus()
})
onBeforeUnmount(() => {
  opener?.focus?.()
})
</script>

<template>
  <Teleport to="body">
    <div
      ref="rootEl"
      class="layer"
      tabindex="-1"
      @keydown.esc="onKeydown"
    >
      <header class="bar">
        <button
          class="back"
          type="button"
          title="关闭分镜编辑,返回画布(Esc)"
          @click="emit('close')"
        >
          ← 返回画布
        </button>
        <span class="title">
          分镜<span v-if="data.title"> · {{ data.title }}</span>
        </span>
        <span class="meta">{{ data.cuts.length }} cut</span>
      </header>

      <div class="body">
        <div class="cuts">
          <p
            v-if="data.cuts.length === 0"
            class="empty"
          >
            空板 —— cut 可删到空;按下方「+ cut」继续分镜。
          </p>

          <section
            v-for="(cut, i) in data.cuts"
            :key="i"
            class="cut"
          >
            <div class="cut-no">
              {{ i + 1 }}
            </div>

            <div class="cut-main">
              <div class="cut-frame">
                <StoryboardFrameSlot
                  :asset-id="cut.asset_id"
                  :uploading="uploading"
                  @pick="openPicker(i)"
                  @upload="emit('upload', i, $event)"
                  @drop="emit('drop', i)"
                />
              </div>

              <div class="cut-fields">
                <div class="field-row">
                  <label class="field seconds">
                    <span>时长(秒)</span>
                    <input
                      type="text"
                      inputmode="decimal"
                      :value="cut.seconds ?? ''"
                      placeholder="未定"
                      title="失焦夹紧:空 = 未定(汇总跳过),负数/越界/小数取整夹紧"
                      @change="onSecondsBlur(i, ($event.target as HTMLInputElement).value)"
                    >
                  </label>
                  <span class="field note">
                    <span>备注</span>
                    <input
                      type="text"
                      :value="cut.note"
                      placeholder="一行备注(可选)"
                      @input="onNoteInput(i, ($event.target as HTMLInputElement).value)"
                    >
                  </span>
                </div>

                <label class="field lines">
                  <span>台词 / 音效</span>
                  <textarea
                    :value="cut.lines"
                    rows="3"
                    placeholder="台词、音效、音乐注记(多行)"
                    @input="onLinesInput(i, ($event.target as HTMLTextAreaElement).value)"
                  />
                </label>

                <div class="field camera">
                  <span>运镜</span>
                  <div class="chips">
                    <button
                      v-for="m in CAMERA_MOVE_OPTIONS"
                      :key="m"
                      type="button"
                      class="chip"
                      :class="{ on: cut.camera_moves.includes(m) }"
                      :title="cut.camera_moves.includes(m) ? `移除「${m}」` : `加「${m}」`"
                      @click="onCameraChip(i, m)"
                    >
                      {{ m }}
                    </button>
                  </div>
                </div>
              </div>
            </div>

            <div class="cut-actions">
              <button
                type="button"
                :disabled="i === 0"
                title="上移本 cut(序号自动重排)"
                @click="moveCutAt(i, -1)"
              >
                ↑ 上移
              </button>
              <button
                type="button"
                :disabled="i === data.cuts.length - 1"
                title="下移本 cut(序号自动重排)"
                @click="moveCutAt(i, 1)"
              >
                ↓ 下移
              </button>
              <button
                type="button"
                class="danger"
                title="删除本 cut(板可删到空;素材行不动)"
                @click="removeCutAt(i)"
              >
                删除
              </button>
            </div>
          </section>

          <button
            class="add-cut"
            type="button"
            title="追加一个空 cut"
            @click="addCut"
          >
            + cut
          </button>
        </div>

        <!-- 素材挑选面板挂层右缘(AssetPicker 复用,scope=ip 缺省):
             点选即设为正在编辑的 cut 画面,再点取消。 -->
        <AssetPicker
          v-if="pickerFor !== null"
          :key="`storyboard-picker-${pickerFor}`"
          class="layer-picker"
          title="选为分镜画面"
          :kind-whitelist="['image']"
          hint="点选设为本 cut 画面,再点一次取消;生成产物即素材行,按最近使用排序就近可得。"
          :selected-asset-ids="pickerSelectedIds"
          :ip-id="ipId"
          @toggle="onPickerToggle"
          @close="pickerFor = null"
        />
      </div>
    </div>
  </Teleport>
</template>

<style scoped>
.layer {
  position: fixed;
  inset: 0;
  z-index: var(--z-editor-layer);
  display: flex;
  flex-direction: column;
  background: var(--bg-layer);
  color: var(--text-bright);
  font-size: var(--fs-body);
  animation: layer-in var(--dur) var(--ease);
}

.layer:focus {
  outline: none;
}

/* ---- 顶栏 ---- */

.bar {
  display: flex;
  align-items: center;
  gap: 12px;
  padding: 10px 14px;
  border-bottom: 1px solid var(--line-section);
  background: rgba(var(--surface-card-rgb), 0.95);
}

.bar .title {
  font-weight: 600;
  color: var(--orange);
}

.bar .meta {
  color: var(--text-muted);
  font-size: var(--fs-label);
}

.bar button {
  border: 1px solid var(--line-control);
  border-radius: var(--r-md);
  padding: 6px 12px;
  font-size: var(--fs-label);
  cursor: pointer;
  background: var(--wash-soft);
  color: var(--text-bright);
  white-space: nowrap;
}

.bar button:hover {
  border-color: rgba(var(--orange-rgb), 0.6);
}

.bar .back:hover {
  border-color: rgba(var(--accent-rgb), 0.7);
}

/* ---- 主体:垂直 cut 列表 ---- */

.body {
  position: relative;
  flex: 1;
  display: flex;
  min-height: 0;
}

.cuts {
  flex: 1;
  min-width: 0;
  overflow-y: auto;
  padding: 16px 16px 28px;
  display: flex;
  flex-direction: column;
  gap: 14px;
  max-width: 900px;
  margin: 0 auto;
  width: 100%;
  box-sizing: border-box;
}

.empty {
  margin: 0;
  color: var(--text-muted);
}

.cut {
  display: grid;
  grid-template-columns: 36px 1fr;
  gap: 12px;
  border: 1px solid var(--line-control);
  border-radius: var(--r-xl);
  padding: 12px;
  background: var(--wash-subtle);
}

.cut-no {
  display: grid;
  place-items: start center;
  padding-top: 4px;
  font-size: 16px;
  font-weight: 700;
  color: var(--orange);
}

.cut-main {
  display: grid;
  grid-template-columns: 240px 1fr;
  gap: 14px;
  min-width: 0;
}

.cut-fields {
  display: grid;
  gap: 10px;
  align-content: start;
  min-width: 0;
}

.field {
  display: grid;
  gap: 4px;
  font-size: var(--fs-label);
  color: var(--text-muted);
}

.field input,
.field textarea {
  background: var(--wash-soft);
  border: 1px solid var(--line-control);
  border-radius: var(--r-md);
  padding: 6px 10px;
  color: inherit;
  font-size: var(--fs-body);
  width: 100%;
  box-sizing: border-box;
}

.field textarea {
  resize: vertical;
  white-space: pre-wrap;
}

.field input:focus,
.field textarea:focus {
  outline: none;
  border-color: rgba(var(--orange-rgb), 0.6);
}

.field-row {
  display: grid;
  grid-template-columns: 110px 1fr;
  gap: 10px;
}

.chips {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
}

.chip {
  border: 1px solid var(--line-control);
  border-radius: var(--r-pill);
  padding: 3px 12px;
  font-size: var(--fs-label);
  cursor: pointer;
  background: transparent;
  color: var(--text-secondary);
}

.chip:hover {
  border-color: rgba(var(--orange-rgb), 0.6);
}

.chip.on {
  background: rgba(var(--orange-rgb), 0.18);
  border-color: rgba(var(--orange-rgb), 0.65);
  color: var(--orange);
  font-weight: 600;
}

.cut-actions {
  grid-column: 2;
  display: flex;
  gap: 6px;
  justify-content: flex-end;
}

.cut-actions button {
  border: 1px solid var(--line-control);
  border-radius: var(--r-md);
  padding: 4px 10px;
  font-size: var(--fs-label);
  cursor: pointer;
  background: transparent;
  color: var(--text-secondary);
  white-space: nowrap;
}

.cut-actions button:hover:not(:disabled) {
  border-color: rgba(var(--orange-rgb), 0.6);
  color: inherit;
}

.cut-actions button:disabled {
  opacity: 0.4;
  cursor: not-allowed;
}

.cut-actions .danger {
  border-color: rgba(var(--danger-rgb), 0.4);
  color: var(--danger);
}

.add-cut {
  align-self: flex-start;
  border: 1px dashed rgba(var(--orange-rgb), 0.5);
  border-radius: var(--r-lg);
  padding: 8px 18px;
  font-size: var(--fs-body);
  font-weight: 600;
  cursor: pointer;
  background: transparent;
  color: var(--orange);
}

.add-cut:hover {
  background: rgba(var(--orange-rgb), 0.12);
}

/* 素材挑选面板:AssetPicker 原为编辑器右缘浮层,这里锚在层右缘。 */
.layer-picker {
  position: absolute;
}
</style>
