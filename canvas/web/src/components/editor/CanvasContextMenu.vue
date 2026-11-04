<script setup lang="ts">
// 画布右键上下文菜单(76 号票落、77 号票二级化):右键画布空白处弹出的
// 添加与入口菜单,取代底部工具栏。Teleport 到 body + position: fixed
// (CanvasHelpModal 先例),z 领 --z-context-menu(面板之上、全屏编辑层
// 之下)。两级结构(77 号票定案 1-4):一级只有分区入口(创作/规划/组织/
// 面板,纯文字 + CSS ▸ 指示符,不引图标库),hover 立即展开二级飞出条目
// (彩色 tone 语言完整保留在飞出内);点击一级分区入口本身不做任何事,
// 飞出内条目 hover 高亮、点击执行即关。
// 条目全部来自 canvasContextMenu.ts 内容模块,本组件不含任何条目字面量
// —— 改条目/换配色只动内容文件(循 canvasHelp.ts「内容即数据」范式)。
// hover 边界(定案 3):纯 hover 驱动,悬停入口立即展开(无延迟)、移到
// 另一分区自动切换;两块面板(一级菜单/飞出)相邻无缝,任一面板的
// mouseleave 以 relatedTarget 判定去向 —— 仍落在另一面板内即不收,出了
// 菜单 + 飞出整体范围飞出即收(一级菜单保持),不用关闭延迟定时器。
// 关闭 = 点菜单外(全视口透明垫,mousedown/contextmenu 都算)/ Esc
// (由 CanvasEditorView 全局栈收,飞出不作为独立 Esc 层,本组件不另挂
// window 监听以免一次 Esc 双重处理,帮助浮层同款纪律)/ 画布平移缩放
// (视图侧 onMoveStart 收)/ 点飞出内任一条目(执行即关)。贴边:一级
// 菜单挂载后按实际尺寸交 clampMenuPosition 钳制照旧;飞出在挂载/分区
// 切换后量自身尺寸交 clampFlyoutPosition 钳制(贴视口右缘翻菜单左侧、
// 纵向钳进视口)。上传隐藏 file input **不住本组件**(76 号票评审:菜
// 单一关组件即卸载,挂在组件里的 input 会让 change 发在脱附元素上,
// Chrome 可收但 Wails WebView 有风险)—— input 提升到 CanvasEditorView
// 持久层,本组件的上传条目只经 select 命令触发视图的 triggerUpload,
// 「点条目即关」体感不变、落档链路(onUploadChange)零变化。触屏/长按
// 不做(76·定案 5,产品口径 PC 主力)。
import { computed, nextTick, onMounted, ref } from 'vue'

import {
  clampFlyoutPosition,
  clampMenuPosition,
  CONTEXT_MENU_SECTIONS,
  type ContextMenuItem,
  type ContextMenuSection,
  type ContextMenuSectionId,
  type FlyoutAnchor,
} from '../../canvasContextMenu'

const props = defineProps<{
  /** 右键点(client 坐标)= 菜单左上角的期望位置(贴边时被钳制)。 */
  x: number
  y: number
  /** 上传进行中:上传条目禁用(原工具栏按钮的 disabled 语义随迁)。 */
  uploading: boolean
}>()

const emit = defineEmits<{
  /** 点飞出内任一条目:执行即关 —— 视图按 command.kind 分发行为,并收起菜单。 */
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

// hover 态:null = 无飞出;移到另一分区自动切换(computed 从内容模块查
// 飞出条目,组件不含条目字面量)。
const activeSectionId = ref<ContextMenuSectionId | null>(null)
const activeSection = computed<ContextMenuSection | null>(
  () => CONTEXT_MENU_SECTIONS.find((s) => s.id === activeSectionId.value) ?? null,
)

// 飞出定位:锚 = 一级菜单外缘右边界(不是入口按钮的右边界 —— 按钮内缩
// 在菜单 padding 里,贴它会盖住菜单右缘的 ▸ 箭头)、顶部与入口对齐;
// flipX = 菜单外缘左边界(贴视口右缘翻左时飞出右缘贴它)。先按锚渲染
// 再量尺寸钳制,与一级菜单同款「挂载后量实际尺寸」节奏。
const flyoutEl = ref<HTMLElement | null>(null)
const flyoutAnchor = ref<FlyoutAnchor>({ x: 0, y: 0, flipX: 0 })
const flyoutPos = ref({ x: 0, y: 0 })

/** 飞出挂载/分区切换后量自身尺寸钳位(nextTick 等 DOM 落定再量)。 */
async function positionFlyout(): Promise<void> {
  await nextTick()
  const el = flyoutEl.value
  if (!el) {
    return
  }
  flyoutPos.value = clampFlyoutPosition(
    flyoutAnchor.value,
    { width: el.offsetWidth, height: el.offsetHeight },
    { width: window.innerWidth, height: window.innerHeight },
  )
}

/** 悬停分区入口:立即展开该分区飞出(无延迟),移到另一分区自动切换。 */
function openSection(section: ContextMenuSection, event: MouseEvent): void {
  const menu = menuEl.value
  const entry = event.currentTarget
  if (!menu || !(entry instanceof HTMLElement)) {
    return
  }
  const menuRect = menu.getBoundingClientRect()
  const entryRect = entry.getBoundingClientRect()
  flyoutAnchor.value = { x: menuRect.right, y: entryRect.top, flipX: menuRect.left }
  // 未钳制的锚先顶一帧(与一级菜单同款节奏),量到尺寸后钳位纠正。
  flyoutPos.value = { x: flyoutAnchor.value.x, y: flyoutAnchor.value.y }
  // ref 同值赋值不触发响应(Object.is 判等),无需同值守卫。
  activeSectionId.value = section.id
  void positionFlyout()
}

/** 两块面板(一级菜单/飞出)共用的移出判定:指针去向还在任一面板内
 * (相邻面板间移动的 mouseleave)即不收;出了菜单 + 飞出整体范围飞出
 * 即收(一级菜单保持,关整个菜单走点外/Esc/平移缩放)。 */
function onPanelLeave(event: MouseEvent): void {
  const next = event.relatedTarget
  if (next instanceof Node && (menuEl.value?.contains(next) || flyoutEl.value?.contains(next))) {
    return
  }
  activeSectionId.value = null
}

/** 点飞出内任一条目:上抛命令并即关。上传条目不在此触发 file input ——
 * 隐藏 input 住视图持久层(见上方注释),视图侧按 select 命令触发,本
 * 组件卸载与否不影响文件选择器。 */
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
      <!-- 一级:分区入口行,hover 展开、点击不做任何事(不执行不关闭,
           77 号票定案 3)。 -->
      <nav
        ref="menuEl"
        class="ctx-menu"
        :style="{ left: `${pos.x}px`, top: `${pos.y}px` }"
        @mousedown.stop
        @contextmenu.stop.prevent
        @mouseleave="onPanelLeave"
      >
        <button
          v-for="section in CONTEXT_MENU_SECTIONS"
          :key="section.id"
          type="button"
          class="ctx-entry"
          :class="{ 'is-open': section.id === activeSectionId }"
          @mouseenter="openSection(section, $event)"
        >
          <span class="ctx-entry-label">{{ section.label }}</span>
          <span
            class="ctx-entry-arrow"
            aria-hidden="true"
          />
        </button>
      </nav>
      <!-- 二级飞出:随 hover 态出现,不是独立 Esc 层(Esc 一次关整个
           菜单,由视图全局栈收)。 -->
      <nav
        v-if="activeSection"
        ref="flyoutEl"
        class="ctx-flyout"
        :style="{ left: `${flyoutPos.x}px`, top: `${flyoutPos.y}px` }"
        @mousedown.stop
        @contextmenu.stop.prevent
        @mouseleave="onPanelLeave"
      >
        <button
          v-for="item in activeSection.items"
          :key="item.id"
          type="button"
          class="ctx-item"
          :class="`tone-${item.tone}`"
          :title="item.title"
          :disabled="item.command.kind === 'upload' && uploading"
          @click="onPick(item)"
        >
          {{ item.label }}
        </button>
      </nav>
    </div>
  </Teleport>
</template>

<style scoped>
/* 垫与菜单同层(z-context-menu:面板之上、编辑层之下);菜单与飞出是
   垫的子元素,飞出在后自然浮于一级菜单上。 */
.ctx-backdrop {
  position: fixed;
  inset: 0;
  z-index: var(--z-context-menu);
}

/* 两块面板共用的暗色小面板质感:卡片底 + 控制线描边 + 对话框影(帮助
   浮层同族)。一级菜单与二级飞出当前声明完全一致,合并为共享选择器;
   两面板日后出现差异时,差异声明写进各自的选择器块。飞出位置由
   clampFlyoutPosition 钳定。 */
.ctx-menu,
.ctx-flyout {
  position: fixed;
  display: flex;
  flex-direction: column;
  gap: 4px;
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

/* 一级分区入口:纯文字 + 右侧 ▸,中性灰(77 号票定案 2),点击不做
   任何事;hover/展开态提到可点击档。 */
.ctx-entry {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  text-align: left;
  border: 1px solid var(--line-section);
  border-radius: var(--r-md);
  padding: 7px 12px;
  font-size: var(--fs-label);
  font-weight: 600;
  cursor: pointer;
  white-space: nowrap;
  background: rgba(var(--estimate-ink-rgb), 0.14);
  color: var(--estimate-ink);
  transition:
    filter var(--dur-fast) var(--ease),
    border-color var(--dur-fast) var(--ease);
}

.ctx-entry:hover,
.ctx-entry.is-open {
  filter: brightness(1.18);
  border-color: var(--line-action);
}

/* ▸ 指示符:CSS 边框三角,不引图标库(77 号票定案 2);currentColor
   随入口文字色走。 */
.ctx-entry-arrow {
  width: 0;
  height: 0;
  border-top: 4px solid transparent;
  border-bottom: 4px solid transparent;
  border-left: 5px solid currentColor;
}

/* 飞出条目:沿用原工具栏按钮的彩色配色语言(色底 + 1px 白系细描边,
   hover 提到可点击档,描边即轮廓规则),彩色 tone 只住在飞出内。 */
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
