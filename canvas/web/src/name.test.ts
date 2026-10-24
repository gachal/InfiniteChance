import { describe, expect, it } from 'vitest'

import type { NamePage, NamePanel } from './graph'
import {
  appendPage,
  appendPanel,
  countPanels,
  defaultName,
  emptyNamePage,
  emptyNamePanel,
  movePage,
  movePanel,
  removePage,
  removePanel,
  togglePanelSize,
} from './name'

const panel = (over: Partial<NamePanel> = {}): NamePanel => ({
  ...emptyNamePanel(),
  ...over,
})

const page = (panels: NamePanel[] = [emptyNamePanel()], title?: string): NamePage => ({
  ...(title === undefined ? {} : { title }),
  panels,
})

describe('emptyNamePanel / emptyNamePage / defaultName', () => {
  it('seeds a board with one page holding one empty panel', () => {
    // 添加即带 1 页 1 格(54 号票;预演台「添加即可见」/絵コンテ「1 空 cut」
    // 同先例):画面空、台词空、大格起步;页标题可空(不落键)。
    const data = defaultName()
    expect(data.title).toBe('')
    expect(data.pages).toHaveLength(1)
    expect(data.pages[0].title).toBeUndefined()
    expect(data.pages[0].panels).toHaveLength(1)
    expect(data.pages[0].panels[0]).toEqual({ asset_id: 0, lines: '', size: 'large' })
  })

  it('seeds a fresh page with no title and one empty panel', () => {
    // 新页不是白板:进去就有格子可画;页标题不落键(展示层回落「第 N 页」)。
    const p = emptyNamePage()
    expect(p.title).toBeUndefined()
    expect(p.panels).toEqual([{ asset_id: 0, lines: '', size: 'large' }])
  })

  it('does not share the pages array between boards', () => {
    expect(defaultName().pages).not.toBe(defaultName().pages)
    // 深层也不共享:每页的 panels 各自换新。
    expect(defaultName().pages[0].panels).not.toBe(defaultName().pages[0].panels)
  })
})

describe('appendPage / removePage / movePage', () => {
  it('appends an empty page (1 格) and returns a new array', () => {
    const seed = [page([panel({ lines: '第 1 页' })])]
    const next = appendPage(seed)
    expect(next).toHaveLength(2)
    expect(next[1].panels).toEqual([{ asset_id: 0, lines: '', size: 'large' }])
    expect(seed).toHaveLength(1)
  })

  it('removes a page by index and allows emptying the board', () => {
    // 板可空页(54 号票):页删到空没有「最后不可删」守卫,板是画布工作件
    // 而非库实体(与絵コンテ cut、角色变体守卫的对比同 53 号票定案)。
    const seed = [page([panel()], 'a'), page([panel()], 'b')]
    expect(removePage(seed, 0).map((p) => p.title)).toEqual(['b'])
    const once = removePage(seed, 0)
    expect(removePage(once, 0)).toEqual([])
    expect(seed).toHaveLength(2)
  })

  it('moves a page within bounds and no-ops at the edges', () => {
    const seed = [page([panel()], '1'), page([panel()], '2'), page([panel()], '3')]
    expect(movePage(seed, 0, -1)).toBe(seed)
    expect(movePage(seed, 2, 1)).toBe(seed)
    expect(movePage(seed, 9, 1)).toBe(seed)
    expect(movePage(seed, 1, -1).map((p) => p.title)).toEqual(['2', '1', '3'])
    expect(movePage(seed, 1, 1).map((p) => p.title)).toEqual(['1', '3', '2'])
    expect(seed.map((p) => p.title)).toEqual(['1', '2', '3'])
  })
})

describe('appendPanel / removePanel / movePanel', () => {
  it('appends an empty panel and returns a new array', () => {
    const seed = [panel({ lines: '开场' })]
    const next = appendPanel(seed)
    expect(next).toHaveLength(2)
    expect(next[1]).toEqual({ asset_id: 0, lines: '', size: 'large' })
    expect(seed).toHaveLength(1)
  })

  it('removes a panel by index and allows emptying the page', () => {
    // 格可删到空(54 号票):空页保留在板上,删页走页级删除。
    const seed = [panel({ lines: 'a' }), panel({ lines: 'b' })]
    expect(removePanel(seed, 0).map((p) => p.lines)).toEqual(['b'])
    const once = removePanel(seed, 0)
    expect(removePanel(once, 0)).toEqual([])
    expect(seed).toHaveLength(2)
  })

  it('moves a panel within bounds and no-ops at the edges', () => {
    const seed = [panel({ lines: '1' }), panel({ lines: '2' }), panel({ lines: '3' })]
    expect(movePanel(seed, 0, -1)).toBe(seed)
    expect(movePanel(seed, 2, 1)).toBe(seed)
    expect(movePanel(seed, 9, 1)).toBe(seed)
    expect(movePanel(seed, 1, -1).map((p) => p.lines)).toEqual(['2', '1', '3'])
    expect(movePanel(seed, 1, 1).map((p) => p.lines)).toEqual(['1', '3', '2'])
    expect(seed.map((p) => p.lines)).toEqual(['1', '2', '3'])
  })
})

describe('togglePanelSize', () => {
  it('flips between the two size grades without mutating the input', () => {
    // 大小格两档表达コマ割り节奏(54 号票):不做真二维排版,只有切档。
    expect(togglePanelSize(panel({ size: 'large' })).size).toBe('small')
    expect(togglePanelSize(panel({ size: 'small' })).size).toBe('large')
    const seed = panel({ size: 'large', lines: '台词' })
    const next = togglePanelSize(seed)
    expect(next.size).toBe('small')
    expect(next.lines).toBe('台词')
    expect(seed.size).toBe('large')
  })
})

describe('countPanels', () => {
  it('sums panels across all pages', () => {
    // 汇总 M 格 = 各页格子总数(板卡「N 页 · M 格」)。
    const pages = [
      page([panel(), panel(), panel()]),
      page([panel()]),
      page([], '空页'),
    ]
    expect(countPanels(pages)).toBe(4)
    expect(countPanels([])).toBe(0)
  })
})
