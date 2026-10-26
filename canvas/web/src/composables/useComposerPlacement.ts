import { computed, type ComputedRef, type Ref } from 'vue'
import type { GraphNode, ViewportTransform } from '@vue-flow/core'

/** 悬浮定位:贴选中节点正下方(视口变换 + 节点尺寸换算成画布区坐标,
 * 拖动/缩放/图片加载都跟随),并 clamp 在画布区内。视频模式控件更多,
 * clamp 的估算高度按模式取档(评审:旧值一律 240,视频模式会贴边不准)。
 * 58 号票阶段一自 CanvasEditorView 抽出。 */
export function useComposerPlacement(opts: {
  node: ComputedRef<GraphNode | null>
  mode: Ref<'image' | 'video'>
  wrapEl: Ref<HTMLDivElement | null>
  viewport: Ref<ViewportTransform>
}): ComputedRef<Record<string, string>> {
  const { node, mode, wrapEl, viewport } = opts
  return computed<Record<string, string>>(() => {
    const n = node.value
    if (!n) {
      // v-if 已挡住渲染,这里只是类型兜底。
      const hidden: Record<string, string> = { display: 'none' }
      return hidden
    }
    const vp = viewport.value
    const wrapW = wrapEl.value?.clientWidth ?? 0
    const wrapH = wrapEl.value?.clientHeight ?? 0
    const halfW = 280
    const estH = mode.value === 'video' ? 360 : 260
    const dims = n.dimensions
    const left = vp.x + (n.position.x + (dims?.width ?? 200) / 2) * vp.zoom
    const top = vp.y + (n.position.y + (dims?.height ?? 160)) * vp.zoom + 12
    const clampedLeft = Math.min(Math.max(left, halfW + 8), Math.max(wrapW - halfW - 8, halfW + 8))
    const clampedTop = Math.min(Math.max(top, 8), Math.max(wrapH - estH, 8))
    const placed: Record<string, string> = {
      left: `${clampedLeft}px`,
      top: `${clampedTop}px`,
      transform: 'translateX(-50%)',
    }
    return placed
  })
}
