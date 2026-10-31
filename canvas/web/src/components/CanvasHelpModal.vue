<script setup lang="ts">
// 画布帮助浮层(68 号票):居中阅读模态,IpDetailView 居中对话框同族
// (遮罩 0.7 + --bg-navy 面板 + --shadow-dialog-strong + dialog-in 入场,
// z 档 --z-help-modal 领自 style.css 档位表)。纯渲染档:章节与块全部
// 来自 canvasHelp.ts 内容模块,本组件不含任何帮助文案字面量 —— 改文案/
// 加章只动内容文件,组件零 diff(票·验证点)。
// 关闭 = Esc / 点击遮罩 / ✕ 三路等价;Esc 由 CanvasEditorView 的全局
// Esc 栈收(帮助在灯箱让位判断之后、其余层之前,栈顶先收),本组件不另
// 挂 window 监听以免一次 Esc 双重处理。打开收焦点、关闭归还焦点,与
// MediaLightbox 同款(38 号票键盘可达性)。
import { onBeforeUnmount, onMounted, ref } from 'vue'

import { CANVAS_HELP_DOC } from '../canvasHelp'

const emit = defineEmits<{ close: [] }>()

// 38 号票评审:打开把焦点移进浮层(Tab 顺序不再落在被遮住的画布上),
// 关闭归还给打开它的元素。
const rootEl = ref<HTMLDivElement | null>(null)
let opener: HTMLElement | null = null

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
      class="help-mask"
      tabindex="-1"
      role="dialog"
      :aria-label="CANVAS_HELP_DOC.title"
      @click="emit('close')"
    >
      <section
        class="help-dialog"
        @click.stop
      >
        <header class="help-bar">
          <h2>{{ CANVAS_HELP_DOC.title }}</h2>
          <button
            class="help-close"
            type="button"
            title="关闭(Esc)"
            aria-label="关闭"
            @click="emit('close')"
          >
            ✕
          </button>
        </header>
        <div class="help-body">
          <section
            v-for="chapter in CANVAS_HELP_DOC.chapters"
            :key="chapter.id"
            class="help-chapter"
          >
            <h3>{{ chapter.title }}</h3>
            <template
              v-for="(block, i) in chapter.blocks"
              :key="`${chapter.id}-${i}`"
            >
              <h4 v-if="block.kind === 'heading'">
                {{ block.text }}
              </h4>
              <p v-else-if="block.kind === 'p'">
                {{ block.text }}
              </p>
              <ul v-else-if="block.kind === 'list'">
                <li
                  v-for="item in block.items"
                  :key="item"
                >
                  {{ item }}
                </li>
              </ul>
              <div
                v-else-if="block.kind === 'keys'"
                class="help-keys"
              >
                <span
                  v-for="entry in block.entries"
                  :key="entry.keys + entry.label"
                  class="help-key-row"
                >
                  <kbd>{{ entry.keys }}</kbd>
                  <span class="help-key-label">{{ entry.label }}</span>
                </span>
              </div>
              <p
                v-else
                class="help-note"
              >
                {{ block.text }}
              </p>
            </template>
          </section>
        </div>
      </section>
    </div>
  </Teleport>
</template>

<style scoped>
/* 遮罩 0.7 档(居中阅读模态,IpDetailView 同族;灯箱 0.88 更深) */
.help-mask {
  position: fixed;
  inset: 0;
  z-index: var(--z-help-modal);
  display: grid;
  place-items: center;
  padding: 24px;
  background: rgba(var(--bg-deep-rgb), 0.7);
  animation: mask-in var(--dur) var(--ease);
}

/* 面板:header 固定 + body 滚动(编辑层 .layer 骨架同构),
   宽度走居中对话框 520 档,视口小则让位到遮罩 padding 内。 */
.help-dialog {
  display: flex;
  flex-direction: column;
  width: min(520px, 100%);
  max-height: calc(100vh - 64px);
  overflow: hidden;
  background: var(--bg-navy);
  border: 1px solid var(--line-control);
  border-radius: var(--r-xl);
  box-shadow: var(--shadow-dialog-strong);
  animation: dialog-in var(--dur-slow) var(--ease);
}

.help-bar {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: var(--sp-md);
  padding: 12px 16px;
  border-bottom: 1px solid var(--line-section);
}

.help-bar h2 {
  margin: 0;
  font-size: var(--fs-title);
}

.help-close {
  width: 28px;
  height: 28px;
  display: grid;
  place-items: center;
  border: 1px solid var(--line-control);
  border-radius: var(--r-md);
  background: var(--wash-soft);
  color: var(--text-secondary);
  font-size: var(--fs-label);
  cursor: pointer;
  transition: background var(--dur-fast) var(--ease);
}

.help-close:hover {
  background: var(--wash-fill);
  color: var(--text-bright);
}

.help-body {
  overflow-y: auto;
  padding: 14px 16px 16px;
}

/* 章与章之间用分区线隔开(分区线 0.08 档,「面不是线」纪律) */
.help-chapter + .help-chapter {
  margin-top: var(--sp-md);
  padding-top: var(--sp-md);
  border-top: 1px solid var(--line-section);
}

.help-chapter h3 {
  margin: 0 0 var(--sp-sm);
  font-size: var(--fs-title);
}

.help-chapter h4 {
  margin: var(--sp-md) 0 6px;
  font-size: var(--fs-body);
  color: var(--text-bright);
}

.help-chapter p {
  margin: 0 0 var(--sp-sm);
  color: var(--text-secondary);
  line-height: 1.7;
}

.help-chapter ul {
  margin: 0 0 var(--sp-sm);
  padding-left: 18px;
  color: var(--text-secondary);
  line-height: 1.7;
}

/* 键位表:kbd 走控件底 + 描边,label 同正文色。 */
.help-keys {
  display: grid;
  gap: 6px;
  margin: 0 0 var(--sp-sm);
}

.help-key-row {
  display: flex;
  align-items: baseline;
  gap: var(--sp-sm);
}

.help-key-row kbd {
  padding: 1px 7px;
  border: 1px solid var(--line-control);
  border-radius: var(--r-sm);
  background: var(--wash-soft);
  color: var(--text-bright);
  font-family: inherit;
  font-size: var(--fs-label);
  white-space: nowrap;
}

.help-key-label {
  color: var(--text-secondary);
  font-size: var(--fs-body);
}

/* 边界注记:衬底小卡,信息色不用警报色(「面不是线」底纹档)。 */
.help-note {
  margin: 0 0 6px;
  padding: 8px 10px;
  border: 1px solid var(--line-section);
  border-radius: var(--r-md);
  background: var(--wash-subtle);
  color: var(--text-muted);
  font-size: var(--fs-label);
  line-height: 1.6;
}
</style>
