<script setup lang="ts">
// 画布右键上下文菜单(76 号票):右键画布空白处弹出的添加与入口菜单,
// 取代底部工具栏。Teleport 到 body + position: fixed(CanvasHelpModal
// 先例),z 领 --z-context-menu(面板之上、全屏编辑层之下)。
// 条目全部来自 canvasContextMenu.ts 内容模块,本组件不含任何条目字面量
// —— 改条目/换配色只动内容文件(循 canvasHelp.ts「内容即数据」范式)。
// 关闭 = 点菜单外(全视口透明垫,mousedown/contextmenu 都算)/ Esc
// (由 CanvasEditorView 全局栈收,本组件不另挂 window 监听以免一次 Esc
// 双重处理,帮助浮层同款纪律)/ 画布平移缩放(视图侧 onMoveStart 收)
// / 点任一条目(执行即关)。贴边翻转在挂载后按实际尺寸交
// clampMenuPosition 钳制(贴底向上弹、贴右向左收)。上传隐藏 file input
// **不住本组件**(76 号票评审:菜单一关组件即卸载,挂在组件里的 input
// 会让 change 发在脱附元素上,Chrome 可收但 Wails WebView 有风险)——
// input 提升到 CanvasEditorView 持久层,本组件的上传条目只经 select
// 命令触发视图的 triggerUpload,「点条目即关」体感不变、落档链路
// (onUploadChange)零变化。触屏/长按不做(票·定案 5,产品口径 PC 主力)。
import { onMounted, ref } from 'vue'

import {
  clampMenuPosition,
  CONTEXT_MENU_SECTIONS,
  type ContextMenuItem,
} from '../../canvasContextMenu'

const props = defineProps<{
  /** 右键点(client 坐标)= 菜单左上角的期望位置(贴边时被钳制)。 */
  x: number
  y: number
  /** 上传进行中:上传条目禁用(原工具栏按钮的 disabled 语义随迁)。 */
  uploading: boolean
}>()

const emit = defineEmits<{
  /** 点任一条目:执行即关 —— 视图按 command.kind 分发行为,并收起菜单。 */
  select: [item: ContextMenuItem]
  close: []
}>()

// 贴边翻转:先按右键点渲染,挂载后量实际菜单尺寸再钳制。菜单尺寸由
// 纯 CSS 决定、条目集静态,定位一次即稳,不随内容变动跳位。
const menuEl = ref<HTMLElement | null>(null)
const pos = ref({ x: props.x, y: props.y })

onMounted(() => {
  const el = menuEl.value
  if (!el) {
    return
  }
  pos.value = clampMenuPosition(
    { x: props.x, y: props.y },
    { width: el.offsetWidth, height: el.offsetHeight },
    { width: window.innerWidth, height: window.innerHeight },
  )
})

/** 点任一条目:上抛命令并即关。上传条目不在此触发 file input —— 隐藏
 * input 住视图持久层(见上方注释),视图侧按 select 命令触发,本组件
 * 卸载与否不影响文件选择器。 */
function onPick(item: ContextMenuItem): void {
  emit('select', item)
  emit('close')
}
</script>

<template>
  <Teleport to="body">
    <!-- 全视口透明垫:点外即关的承载;右键垫上同关不重开(再开靠画布
         pane 的 contextmenu,垫收起后自然恢复)。 -->
    <div
      class="ctx-backdrop"
      @mousedown.prevent="emit('close')"
      @contextmenu.prevent="emit('close')"
    >
      <nav
        ref="menuEl"
        class="ctx-menu"
        :style="{ left: `${pos.x}px`, top: `${pos.y}px` }"
        @mousedown.stop
        @contextmenu.stop.prevent
      >
        <section
          v-for="section in CONTEXT_MENU_SECTIONS"
          :key="section.id"
          class="ctx-section"
        >
          <span class="ctx-section-label">{{ section.label }}</span>
          <button
            v-for="item in section.items"
            :key="item.id"
            type="button"
            :class="`ctx-item tone-${item.tone}`"
            :title="item.title"
            :disabled="item.command.kind === 'upload' && uploading"
            @click="onPick(item)"
          >
            {{ item.label }}
          </button>
        </section>
      </nav>
    </div>
  </Teleport>
</template>

<style scoped>
/* 垫与菜单同层(z-context-menu:面板之上、编辑层之下);菜单是垫的
   子元素,自然浮在垫上。 */
.ctx-backdrop {
  position: fixed;
  inset: 0;
  z-index: var(--z-context-menu);
}

/* 暗色小面板:卡片底 + 控制线描边 + 对话框影(帮助浮层同族质感)。 */
.ctx-menu {
  position: fixed;
  display: flex;
  flex-direction: column;
  gap: 6px;
  min-width: 148px;
  padding: 8px;
  background: var(--bg-navy);
  border: 1px solid var(--line-control);
  border-radius: var(--r-lg);
  box-shadow: var(--shadow-dialog);
  animation: ctx-in var(--dur-fast) var(--ease);
}

@keyframes ctx-in {
  from {
    opacity: 0;
    transform: translateY(4px);
  }
  to {
    opacity: 1;
    transform: translateY(0);
  }
}

/* 四段暗色小标题(原工具栏 group-label 同款退档灰)。 */
.ctx-section {
  display: flex;
  flex-direction: column;
  gap: 4px;
}

.ctx-section + .ctx-section {
  margin-top: 4px;
  padding-top: 8px;
  border-top: 1px solid var(--line-section);
}

.ctx-section-label {
  color: var(--text-faint);
  font-size: var(--fs-label);
  font-weight: 600;
  padding: 0 4px;
}

/* 条目:沿用原工具栏按钮的彩色配色语言(色底 + 1px 白系细描边,
   hover 提到可点击档,描边即轮廓规则)。 */
.ctx-item {
  text-align: left;
  border: 1px solid var(--line-section);
  border-radius: var(--r-md);
  padding: 7px 12px;
  font-size: var(--fs-label);
  font-weight: 600;
  cursor: pointer;
  white-space: nowrap;
  transition:
    filter var(--dur-fast) var(--ease),
    border-color var(--dur-fast) var(--ease);
}

.ctx-item:hover:not(:disabled) {
  filter: brightness(1.18);
  border-color: var(--line-action);
}

.ctx-item:active:not(:disabled) {
  filter: brightness(0.92);
}

.ctx-item:disabled {
  cursor: default;
  opacity: 0.4;
}

/* tone 色对与原工具栏按钮一一对应(类型色留给节点,容器类动作用
   中性 estimate-ink)。 */
.tone-accent {
  background: rgba(var(--accent-rgb), 0.2);
  color: var(--accent);
}

.tone-success {
  background: rgba(var(--success-rgb), 0.16);
  color: var(--success);
}

.tone-amber {
  background: rgba(var(--amber-rgb), 0.14);
  color: var(--amber);
}

.tone-pink {
  background: rgba(var(--pink-rgb), 0.16);
  color: var(--pink);
}

.tone-teal {
  background: rgba(var(--teal-rgb), 0.16);
  color: var(--teal);
}

.tone-teal-light {
  background: rgba(var(--teal-light-rgb), 0.14);
  color: var(--teal-light);
}

.tone-violet {
  background: rgba(var(--violet-rgb), 0.14);
  color: var(--violet);
}

.tone-violet-strong {
  background: rgba(var(--violet-rgb), 0.24);
  color: var(--violet);
}

.tone-orange {
  background: rgba(var(--orange-rgb), 0.16);
  color: var(--orange);
}

.tone-sky {
  background: rgba(var(--sky-rgb), 0.16);
  color: var(--sky);
}

.tone-red {
  background: rgba(var(--red-signal-rgb), 0.16);
  color: var(--red-signal);
}

.tone-gold {
  background: rgba(var(--gold-rgb), 0.16);
  color: var(--gold);
}

.tone-neutral {
  background: rgba(var(--estimate-ink-rgb), 0.14);
  color: var(--estimate-ink);
}
</style>
