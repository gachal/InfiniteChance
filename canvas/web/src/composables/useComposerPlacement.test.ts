// 90 号票:composer 常规/展开两态各估高(readFileSync 范式不适用于
// composable,这里直接以最小 fake 节点驱动 computed,连 Vue 响应式一起
// 验)。估高值是 clamp 行为的契约:改对话框布局要连本测试一起改。
import { describe, expect, it } from 'vitest'
import { computed, ref } from 'vue'
import type { GraphNode, ViewportTransform } from '@vue-flow/core'

import { useComposerPlacement } from './useComposerPlacement'

/** 最小节点形状:composable 只读 position 与 dimensions。 */
function fakeNode(x: number, y: number, w = 200, h = 160): GraphNode {
  return {
    position: { x, y },
    dimensions: { width: w, height: h },
  } as unknown as GraphNode
}

function place(node: GraphNode | null, mode: 'image' | 'video', expanded: boolean, wrapW = 1600, wrapH = 900) {
  const style = useComposerPlacement({
    node: computed(() => node),
    mode: ref(mode),
    expanded: ref(expanded),
    wrapEl: ref({ clientWidth: wrapW, clientHeight: wrapH } as HTMLDivElement),
    viewport: ref<ViewportTransform>({ x: 0, y: 0, zoom: 1 }),
  })
  return style.value
}

describe('useComposerPlacement(90 号票:常规/展开两态各估高)', () => {
  it('无节点时隐藏(v-if 兜底不变)', () => {
    expect(place(null, 'image', false)).toEqual({ display: 'none' })
  })

  it('贴节点正下方 + 12px,水平居中(换算不变;贴左缘时按半宽 clamp)', () => {
    const s = place(fakeNode(300, 100), 'image', false)
    expect(s.left).toBe('400px')
    expect(s.top).toBe('272px')
    expect(s.transform).toBe('translateX(-50%)')
    expect(place(fakeNode(100, 100), 'image', false).left).toBe('288px')
  })

  it('常规态底部 clamp:画布矮时按模式估高收(视频比图片高)', () => {
    const wrapH = 560
    const img = place(fakeNode(300, 100), 'image', false, 1600, wrapH)
    const vid = place(fakeNode(300, 100), 'video', false, 1600, wrapH)
    expect(img.top).toBe(`${wrapH - 328}px`)
    expect(vid.top).toBe(`${wrapH - 364}px`)
  })

  it('展开态 textarea 翻倍(+124),clamp 更收紧;画布过矮时兜底 8px', () => {
    const wrapH = 560
    const img = place(fakeNode(300, 100), 'image', true, 1600, wrapH)
    const vid = place(fakeNode(300, 100), 'video', true, 1600, wrapH)
    expect(img.top).toBe(`${wrapH - 452}px`)
    expect(vid.top).toBe(`${wrapH - 488}px`)
    const tiny = place(fakeNode(300, 100), 'video', true, 1600, 120)
    expect(tiny.top).toBe('8px')
  })
})
