<script setup lang="ts">
// ネーム全屏编辑层(54 号票,絵コンテ编辑层 53 号票范式、预演台 40 号票
// 浮层范式同族):编辑器级单实例浮层(Teleport 到 body,ADR 0002 同族),
// 全屏遮挡时画布不卸载,页/格改动整包走 scene-change → updateNodeData →
// autosave 落盘链。两级结构:页列表(序号 + 页标题 + 上移/下移/删除)→
// 页内格子垂直流(序号 + 画面槽(共享组件 StoryboardFrameSlot:素材库/
// 上传/落图三入口)+ 台词多行 + 大/小格切换 + 上移/下移/删除);页尾
// 「+ 格」、板尾「+ 页」。格可删到空(空页保留)、页可删到空(板可空页
// —— 画布工作件非库实体);排序按钮不拖拽(40 号票编辑层手势冲突教训);
// 序号显示时生成 1..N 不持久。Esc/返回按钮双路等价,Esc 刻意走元素级
// (层上可再开编辑器级灯箱,两层同听 window 会一次全关)。素材挑选面板
// (AssetPicker)挂层右缘:点选即设为本格画面,再点取消。
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'

import type { AssetRecord } from '@infinitechance/api'

import type { NameNodeData, NamePage, NamePanel } from '../graph'
import {
  appendPage,
  appendPanel,
  countPanels,
  movePage,
  movePanel,
  removePage,
  removePanel,
} from '../name'
import { followMove, followRemove } from '../list'
import AssetPicker from './AssetPicker.vue'
import StoryboardFrameSlot from './StoryboardFrameSlot.vue'

const props = defineProps<{
  data: NameNodeData
  /** 「本 IP」归属(素材挑选面板的 scope=ip 缺省;编辑器传画布 IP)。 */
  ipId?: number
  /** 本机上传在途(编辑器级状态),画面槽上传按钮随之禁用。 */
  uploading: boolean
}>()

const emit = defineEmits<{
  /** 页/格/板名整包变更,编辑器 updateNodeData 落盘(数组换新)。 */
  'scene-change': [patch: { title: string; pages: NamePage[] }]
  /** 画面槽本机上传:编辑器入库后按素材 id 回填本格。 */
  upload: [pageIndex: number, panelIndex: number, file: File]
  /** 落图节点:格画面落为图片节点并连线(编辑器处理)。 */
  drop: [pageIndex: number, panelIndex: number]
  /** 关闭编辑层,返回画布(原位不丢)。 */
  close: []
}>()

/** 页/格数据统一出口:整包上抛(数组换新,纯函数纪律),由编辑器落盘。 */
function emitScene(patch: Partial<{ title: string; pages: NamePage[] }> = {}): void {
  emit('scene-change', {
    title: patch.title ?? props.data.title ?? '',
    pages: patch.pages ?? props.data.pages,
  })
}

function patchPage(index: number, patch: Partial<NamePage>): void {
  emitScene({ pages: props.data.pages.map((p, i) => (i === index ? { ...p, ...patch } : p)) })
}

function patchPanel(pageIndex: number, panelIndex: number, patch: Partial<NamePanel>): void {
  patchPage(pageIndex, {
    panels: props.data.pages[pageIndex].panels.map((n, i) =>
      i === panelIndex ? { ...n, ...patch } : n,
    ),
  })
}

function onPageTitleInput(index: number, value: string): void {
  patchPage(index, { title: value })
}

function onLinesInput(pageIndex: number, panelIndex: number, value: string): void {
  patchPanel(pageIndex, panelIndex, { lines: value })
}

// ---- 页级操作(增/删/移;纯函数在 name.ts)----

function addPage(): void {
  emitScene({ pages: appendPage(props.data.pages) })
}

function removePageAt(index: number): void {
  emitScene({ pages: removePage(props.data.pages, index) })
  // 面板下标跟随(list.ts 纯函数,絵コンテ层同语义):删的正是面板所在页
  // → 关面板;删更早的页 → 面板页号前移一格位不丢。
  const p = pickerFor.value
  if (p !== null) {
    const page = followRemove(p.page, index)
    pickerFor.value = page === null ? null : { page, panel: p.panel }
  }
}

function movePageAt(index: number, delta: -1 | 1): void {
  const moved = movePage(props.data.pages, index, delta)
  emitScene({ pages: moved })
  // 未动(越界交换)面板不动;动了则被移页跟到新位、被换的相邻页同步换位
  // (list.ts followMove,评审修复补齐相邻页跟随)。
  if (moved !== props.data.pages) {
    const p = pickerFor.value
    if (p !== null) {
      pickerFor.value = { page: followMove(p.page, index, delta), panel: p.panel }
    }
  }
}

// ---- 格级操作(增/删/移/大小切;纯函数在 name.ts)----

function addPanel(pageIndex: number): void {
  patchPage(pageIndex, { panels: appendPanel(props.data.pages[pageIndex].panels) })
}

function removePanelAt(pageIndex: number, panelIndex: number): void {
  patchPage(pageIndex, { panels: removePanel(props.data.pages[pageIndex].panels, panelIndex) })
  // 面板下标跟随(list.ts 纯函数,语义同页级):删的正是面板格 → 关面板;
  // 删同页更早的格 → 面板格号前移一位。
  const p = pickerFor.value
  if (p !== null && p.page === pageIndex) {
    const panel = followRemove(p.panel, panelIndex)
    pickerFor.value = panel === null ? null : { page: pageIndex, panel }
  }
}

function movePanelAt(pageIndex: number, panelIndex: number, delta: -1 | 1): void {
  const panels = props.data.pages[pageIndex].panels
  const moved = movePanel(panels, panelIndex, delta)
  patchPage(pageIndex, { panels: moved })
  // 未动(越界交换)面板不动;动了则被移格跟到新位、被换的相邻格同步换位
  // (list.ts followMove,评审修复补齐相邻格跟随)。
  if (moved !== panels) {
    const p = pickerFor.value
    if (p !== null && p.page === pageIndex) {
      pickerFor.value = { page: pageIndex, panel: followMove(p.panel, panelIndex, delta) }
    }
  }
}

// ---- 素材挑选面板(层右缘,AssetPicker 复用)----

/** 正在挑画面的格(页/格下标;null = 面板关着)。 */
const pickerFor = ref<{ page: number; panel: number } | null>(null)

function openPicker(pageIndex: number, panelIndex: number): void {
  const current = pickerFor.value
  pickerFor.value =
    current?.page === pageIndex && current?.panel === panelIndex
      ? null
      : { page: pageIndex, panel: panelIndex }
}

/** 已选高亮 = 当前格的画面素材;点选即设置(再点取消),无专用
 * 「设为分镜画面」动作(53/54 号票零新交互)。 */
const pickerSelectedIds = computed<number[]>(() => {
  const pos = pickerFor.value
  if (pos === null) {
    return []
  }
  const assetId = props.data.pages[pos.page]?.panels[pos.panel]?.asset_id ?? 0
  return assetId > 0 ? [assetId] : []
})

function onPickerToggle(a: AssetRecord): void {
  const pos = pickerFor.value
  if (pos === null) {
    return
  }
  const current = props.data.pages[pos.page]?.panels[pos.panel]?.asset_id ?? 0
  patchPanel(pos.page, pos.panel, { asset_id: current === a.id ? 0 : a.id })
}

// ---- 打开时收焦点、Esc 关闭并归还(StoryboardEditorLayer 同款)----

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
          title="关闭ネーム编辑,返回画布(Esc)"
          @click="emit('close')"
        >
          ← 返回画布
        </button>
        <span class="title">
          ネーム<span v-if="data.title"> · {{ data.title }}</span>
        </span>
        <span class="meta">{{ data.pages.length }} 页 · {{ countPanels(data.pages) }} 格</span>
      </header>

      <div class="body">
        <div class="pages">
          <p
            v-if="data.pages.length === 0"
            class="empty"
          >
            空板 —— 页可删到空;按下方「+ 页」继续。
          </p>

          <section
            v-for="(page, pi) in data.pages"
            :key="pi"
            class="page"
          >
            <div class="page-head">
              <span class="page-no">第 {{ pi + 1 }} 页</span>
              <input
                class="page-title"
                type="text"
                :value="page.title ?? ''"
                placeholder="页标题(可选)"
                maxlength="128"
                title="页标题"
                aria-label="页标题"
                @input="onPageTitleInput(pi, ($event.target as HTMLInputElement).value)"
              >
              <div class="page-actions">
                <button
                  type="button"
                  :disabled="pi === 0"
                  title="上移本页(页序自动重排)"
                  @click="movePageAt(pi, -1)"
                >
                  ↑ 上移
                </button>
                <button
                  type="button"
                  :disabled="pi === data.pages.length - 1"
                  title="下移本页(页序自动重排)"
                  @click="movePageAt(pi, 1)"
                >
                  ↓ 下移
                </button>
                <button
                  type="button"
                  class="danger"
                  title="删除本页(板可删到空;素材行不动)"
                  @click="removePageAt(pi)"
                >
                  删除
                </button>
              </div>
            </div>

            <p
              v-if="page.panels.length === 0"
              class="page-empty"
            >
              空页 —— 格可删到空;按「+ 格」继续。
            </p>

            <div
              v-for="(panel, ni) in page.panels"
              :key="ni"
              class="panel"
            >
              <div class="panel-no">
                {{ ni + 1 }}
              </div>

              <div class="panel-main">
                <div class="panel-frame">
                  <StoryboardFrameSlot
                    :asset-id="panel.asset_id"
                    :uploading="uploading"
                    @pick="openPicker(pi, ni)"
                    @upload="emit('upload', pi, ni, $event)"
                    @drop="emit('drop', pi, ni)"
                  />
                </div>

                <div class="panel-fields">
                  <label class="field lines">
                    <span>台词 / 旁白 / 拟音</span>
                    <textarea
                      :value="panel.lines"
                      rows="3"
                      placeholder="台词、旁白、拟音注记(多行)"
                      @input="onLinesInput(pi, ni, ($event.target as HTMLTextAreaElement).value)"
                    />
                  </label>

                  <div class="field size">
                    <span>格子大小</span>
                    <div class="chips">
                      <button
                        type="button"
                        class="chip"
                        :class="{ on: panel.size === 'large' }"
                        title="设为大格"
                        @click="patchPanel(pi, ni, { size: 'large' })"
                      >
                        大格
                      </button>
                      <button
                        type="button"
                        class="chip"
                        :class="{ on: panel.size === 'small' }"
                        title="设为小格"
                        @click="patchPanel(pi, ni, { size: 'small' })"
                      >
                        小格
                      </button>
                    </div>
                  </div>
                </div>
              </div>

              <div class="panel-actions">
                <button
                  type="button"
                  :disabled="ni === 0"
                  title="上移本格(格序自动重排)"
                  @click="movePanelAt(pi, ni, -1)"
                >
                  ↑ 上移
                </button>
                <button
                  type="button"
                  :disabled="ni === page.panels.length - 1"
                  title="下移本格(格序自动重排)"
                  @click="movePanelAt(pi, ni, 1)"
                >
                  ↓ 下移
                </button>
                <button
                  type="button"
                  class="danger"
                  title="删除本格(页可删到空;素材行不动)"
                  @click="removePanelAt(pi, ni)"
                >
                  删除
                </button>
              </div>
            </div>

            <button
              class="add-panel"
              type="button"
              title="在本页尾追加一个空格"
              @click="addPanel(pi)"
            >
              + 格
            </button>
          </section>

          <button
            class="add-page"
            type="button"
            title="追加一页(自带 1 个空格)"
            @click="addPage"
          >
            + 页
          </button>
        </div>

        <!-- 素材挑选面板挂层右缘(AssetPicker 复用,scope=ip 缺省):
             点选即设为正在编辑的格画面,再点取消。 -->
        <AssetPicker
          v-if="pickerFor !== null"
          :key="`name-picker-${pickerFor.page}-${pickerFor.panel}`"
          class="layer-picker"
          title="选为格画面"
          :kind-whitelist="['image']"
          hint="点选设为本格画面,再点一次取消;生成产物即素材行,按最近使用排序就近可得。"
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
  z-index: 90;
  display: flex;
  flex-direction: column;
  background: #10162a;
  color: #cdd7ee;
  font-size: 13px;
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
  border-bottom: 1px solid rgba(255, 255, 255, 0.08);
  background: rgba(20, 26, 43, 0.95);
}

.bar .title {
  font-weight: 600;
  color: #38bdf8;
}

.bar .meta {
  color: #8b91a7;
  font-size: 12px;
}

.bar button {
  border: 1px solid rgba(255, 255, 255, 0.14);
  border-radius: 8px;
  padding: 6px 12px;
  font-size: 12px;
  cursor: pointer;
  background: rgba(255, 255, 255, 0.05);
  color: #cdd7ee;
  white-space: nowrap;
}

.bar button:hover {
  border-color: rgba(56, 189, 248, 0.6);
}

.bar .back:hover {
  border-color: rgba(122, 162, 247, 0.7);
}

/* ---- 主体:页列表(每页内格子垂直流)---- */

.body {
  position: relative;
  flex: 1;
  display: flex;
  min-height: 0;
}

.pages {
  flex: 1;
  min-width: 0;
  overflow-y: auto;
  padding: 16px 16px 28px;
  display: flex;
  flex-direction: column;
  gap: 16px;
  max-width: 920px;
  margin: 0 auto;
  width: 100%;
  box-sizing: border-box;
}

.empty {
  margin: 0;
  color: #8b91a7;
}

.page {
  display: grid;
  gap: 10px;
  border: 1px solid rgba(255, 255, 255, 0.1);
  border-radius: 12px;
  padding: 12px;
  background: rgba(255, 255, 255, 0.03);
}

.page-head {
  display: grid;
  grid-template-columns: auto 1fr auto;
  align-items: center;
  gap: 10px;
}

.page-no {
  font-size: 13px;
  font-weight: 700;
  color: #38bdf8;
  white-space: nowrap;
}

.page-title {
  background: rgba(255, 255, 255, 0.05);
  border: 1px solid rgba(255, 255, 255, 0.12);
  border-radius: 8px;
  padding: 5px 10px;
  color: inherit;
  font-size: 13px;
  min-width: 0;
  box-sizing: border-box;
}

.page-title:focus {
  outline: none;
  border-color: rgba(56, 189, 248, 0.6);
}

.page-actions,
.panel-actions {
  display: flex;
  gap: 6px;
  justify-content: flex-end;
}

.page-actions button,
.panel-actions button {
  border: 1px solid rgba(255, 255, 255, 0.14);
  border-radius: 8px;
  padding: 4px 10px;
  font-size: 12px;
  cursor: pointer;
  background: transparent;
  color: #aab1c5;
  white-space: nowrap;
}

.page-actions button:hover:not(:disabled),
.panel-actions button:hover:not(:disabled) {
  border-color: rgba(56, 189, 248, 0.6);
  color: inherit;
}

.page-actions button:disabled,
.panel-actions button:disabled {
  opacity: 0.4;
  cursor: not-allowed;
}

.page-actions .danger,
.panel-actions .danger {
  border-color: rgba(255, 143, 143, 0.4);
  color: #ff8f8f;
}

.page-empty {
  margin: 0;
  color: #8b91a7;
  font-size: 12px;
}

.panel {
  display: grid;
  grid-template-columns: 26px 1fr;
  gap: 10px;
  border: 1px solid rgba(255, 255, 255, 0.07);
  border-radius: 10px;
  padding: 10px;
  background: rgba(255, 255, 255, 0.02);
}

.panel-no {
  display: grid;
  place-items: start center;
  padding-top: 4px;
  font-size: 14px;
  font-weight: 700;
  color: #38bdf8;
}

.panel-main {
  display: grid;
  grid-template-columns: 220px 1fr;
  gap: 12px;
  min-width: 0;
}

.panel-fields {
  display: grid;
  gap: 10px;
  align-content: start;
  min-width: 0;
}

.field {
  display: grid;
  gap: 4px;
  font-size: 12px;
  color: #8b91a7;
}

.field textarea {
  background: rgba(255, 255, 255, 0.05);
  border: 1px solid rgba(255, 255, 255, 0.12);
  border-radius: 8px;
  padding: 6px 10px;
  color: inherit;
  font-size: 13px;
  width: 100%;
  box-sizing: border-box;
  resize: vertical;
  white-space: pre-wrap;
}

.field textarea:focus {
  outline: none;
  border-color: rgba(56, 189, 248, 0.6);
}

.chips {
  display: flex;
  gap: 6px;
}

.chip {
  border: 1px solid rgba(255, 255, 255, 0.16);
  border-radius: 999px;
  padding: 3px 12px;
  font-size: 12px;
  cursor: pointer;
  background: transparent;
  color: #aab1c5;
}

.chip:hover {
  border-color: rgba(56, 189, 248, 0.6);
}

.chip.on {
  background: rgba(56, 189, 248, 0.18);
  border-color: rgba(56, 189, 248, 0.65);
  color: #38bdf8;
  font-weight: 600;
}

.panel-actions {
  grid-column: 2;
}

.add-panel {
  justify-self: start;
  border: 1px dashed rgba(56, 189, 248, 0.4);
  border-radius: 8px;
  padding: 5px 14px;
  font-size: 12px;
  font-weight: 600;
  cursor: pointer;
  background: transparent;
  color: #38bdf8;
}

.add-panel:hover {
  background: rgba(56, 189, 248, 0.12);
}

.add-page {
  align-self: flex-start;
  border: 1px dashed rgba(56, 189, 248, 0.5);
  border-radius: 10px;
  padding: 8px 18px;
  font-size: 13px;
  font-weight: 600;
  cursor: pointer;
  background: transparent;
  color: #38bdf8;
}

.add-page:hover {
  background: rgba(56, 189, 248, 0.12);
}

/* 素材挑选面板:AssetPicker 原为编辑器右缘浮层,这里锚在层右缘。 */
.layer-picker {
  position: absolute;
}
</style>
