<script setup lang="ts">
// 剧本全屏编辑层(64 号票,絵コンテ 53 / ネーム 54 / 预演台 40 号票浮层
// 范式同族):编辑器级单实例浮层(Teleport 到 body,ADR 0002 同族),全屏
// 遮挡时画布不卸载;标题 + 正文整包走 scene-change → updateNodeData →
// autosave 落盘链(逐键上抛;大文本 autosave 全量提交是 64 号票已接受的
// 代价,不做节流优化)。正文 30 万字上限 = 前端校验:超限在横幅提示分段
// 粘贴,不硬截断不阻断输入(截断会打断 IME 组词与粘贴流,由提示引导)。
// 大 textarea 一块,场次用 markdown 场头约定(如「【第N场|内|日|地点】」),
// 不做结构化场次编辑 UI。Esc/返回按钮双路等价,Esc 走元素级(层家族纪律
// ——层上可再开编辑器级浮层,两层同听 window 会一次全关)。
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'

import type { ScriptNodeData } from '../graph'
import {
  isScriptTextWithinLimit,
  MAX_SCRIPT_CHARS,
  scriptCardTitle,
  scriptCharCountLabel,
  scriptLimitNotice,
} from '../script'

const props = defineProps<{
  data: ScriptNodeData
}>()

const emit = defineEmits<{
  /** 标题/正文整包变更,编辑器 updateNodeData 落盘。 */
  'scene-change': [patch: { title: string; text: string }]
  /** 关闭编辑层,返回画布(原位不丢)。 */
  close: []
}>()

/** 数据统一出口:整包上抛(不改动入参),由编辑器落盘。 */
function emitScene(patch: Partial<{ title: string; text: string }> = {}): void {
  emit('scene-change', {
    title: patch.title ?? props.data.title ?? '',
    text: patch.text ?? props.data.text,
  })
}

function onTitleInput(value: string): void {
  emitScene({ title: value })
}

function onTextInput(value: string): void {
  emitScene({ text: value })
}

// 超限横幅与标题栏字数:全部走 script.ts 纯函数(与卡面同判据)。
const title = computed(() => scriptCardTitle(props.data.title))
const countLabel = computed(() => scriptCharCountLabel(props.data.text))
const overLimit = computed(() => !isScriptTextWithinLimit(props.data.text))
const limitNotice = computed(() => scriptLimitNotice(props.data.text))

// ---- 打开时收焦点、Esc 关闭并归还(StoryboardEditorLayer 同款)----

const rootEl = ref<HTMLDivElement | null>(null)
let opener: HTMLElement | null = null

function onKeydown(event: KeyboardEvent): void {
  if (event.key === 'Escape') {
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
          title="关闭剧本编辑,返回画布(Esc)"
          @click="emit('close')"
        >
          ← 返回画布
        </button>
        <span class="title">
          剧本<span v-if="title !== '剧本'"> · {{ title }}</span>
        </span>
        <span
          class="meta"
          :class="{ over: overLimit }"
        >{{ countLabel }} / 上限 {{ MAX_SCRIPT_CHARS }} 字</span>
      </header>

      <div class="body">
        <!-- 超限横幅:前端校验的可见出口(不阻断输入,提示分段粘贴)。 -->
        <p
          v-if="overLimit"
          class="limit-warn"
          role="alert"
        >
          {{ limitNotice }}
        </p>

        <label class="field title-field">
          <span>标题(区分用途:分集大纲 / 第3集剧本 / 原文:书名)</span>
          <input
            type="text"
            :value="data.title ?? ''"
            placeholder="如:分集大纲 / 第3集剧本 / 原文:书名"
            maxlength="128"
            title="剧本标题"
            aria-label="剧本标题"
            @input="onTitleInput(($event.target as HTMLInputElement).value)"
          >
        </label>

        <label class="field text-field">
          <span>正文(场次用场头行约定,如「【第1场|内|日|咖啡馆】」)</span>
          <textarea
            class="script-text"
            :value="data.text"
            placeholder="分集大纲 / 每集剧本 / 原文全文;上限 30 万字,大文本建议分段粘贴。"
            spellcheck="false"
            @input="onTextInput(($event.target as HTMLTextAreaElement).value)"
          />
        </label>
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
  color: var(--gold);
}

.bar .meta {
  color: var(--text-muted);
  font-size: var(--fs-label);
}

/* 超限:顶栏字数一并提为警示色(与卡面同款信号)。 */
.bar .meta.over {
  color: var(--danger);
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
  border-color: rgba(var(--gold-rgb), 0.6);
}

.bar .back:hover {
  border-color: rgba(var(--accent-rgb), 0.7);
}

/* ---- 主体:标题 + 大 textarea ---- */

.body {
  flex: 1;
  min-height: 0;
  display: flex;
  flex-direction: column;
  gap: 12px;
  padding: 16px 16px 24px;
  max-width: 920px;
  margin: 0 auto;
  width: 100%;
  box-sizing: border-box;
  overflow-y: auto;
}

.limit-warn {
  margin: 0;
  border: 1px solid rgba(var(--danger-rgb), 0.4);
  border-radius: var(--r-md);
  padding: 8px 12px;
  color: var(--danger);
  font-size: var(--fs-label);
}

.field {
  display: grid;
  gap: 4px;
  font-size: var(--fs-label);
  color: var(--text-muted);
}

.field input {
  background: var(--wash-soft);
  border: 1px solid var(--line-control);
  border-radius: var(--r-md);
  padding: 6px 10px;
  color: inherit;
  font-size: var(--fs-body);
  min-width: 0;
  box-sizing: border-box;
}

.field input:focus {
  outline: none;
  border-color: rgba(var(--gold-rgb), 0.6);
}

/* 正文区吃满剩余高度:长文一次铺开,滚动在 textarea 内完成(编辑层
   body 的 overflow 只是窄窗兜底)。 */
.text-field {
  flex: 1;
  min-height: 0;
  display: flex;
  flex-direction: column;
}

.script-text {
  flex: 1;
  min-height: 320px;
  resize: none;
  background: var(--wash-soft);
  border: 1px solid var(--line-control);
  border-radius: var(--r-md);
  padding: 10px 12px;
  color: inherit;
  font-size: var(--fs-body);
  line-height: 1.7;
  width: 100%;
  box-sizing: border-box;
  white-space: pre-wrap;
}

.script-text:focus {
  outline: none;
  border-color: rgba(var(--gold-rgb), 0.6);
}
</style>
