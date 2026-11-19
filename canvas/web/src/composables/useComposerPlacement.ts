import { computed, type ComputedRef, type Ref } from 'vue'
import type { GraphNode, ViewportTransform } from '@vue-flow/core'

/** 悬浮定位:贴选中节点正下方(视口变换 + 节点尺寸换算成画布区坐标,
 * 拖动/缩放/图片加载都跟随),并 clamp 在画布区内。90 号票重排后,clamp
 * 的估算高度按模式 × 常规/展开两态取档:展开 toggle 把 textarea
 * min-height 120 → 240(约 +124px),视频参考条带角色行比图片条略高。
 * 58 号票阶段一自 CanvasEditorView 抽出,90 号票加 expanded 档。 */
export function useComposerPlacement(opts: {
  node: ComputedRef<GraphNode | null>
  mode: Ref<'image' | 'video'>
  /** 展开态(90 号票):输入区大留白两档,展开时 clamp 估算高 +124。 */
  expanded: Ref<boolean>
  wrapEl: Ref<HTMLDivElement | null>
  viewport: Ref<ViewportTransform>
}): ComputedRef<Record<string, string>> {
  const { node, mode, expanded, wrapEl, viewport } = opts
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
    const estH = (mode.value === 'video' ? 364 : 328) + (expanded.value ? 124 : 0)
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
