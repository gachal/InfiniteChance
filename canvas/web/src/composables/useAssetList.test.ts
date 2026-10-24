import { ref } from 'vue'
import { describe, expect, it } from 'vitest'

import { ASSET_LIST_PAGE_SIZE, useAssetListQuery } from './useAssetList'

// AssetPanel 与 AssetPicker 共用的列表状态(评审修复提取):参数形状与
// 判定与两组件原实现逐字节等价。
describe('useAssetListQuery', () => {
  it('builds ip-scoped params by default', () => {
    const kind = ref<'' | 'image' | 'video' | 'audio'>('')
    const q = useAssetListQuery({ ipId: () => 5, kind, count: () => 0 })

    expect(q.scope.value).toBe('ip')
    expect(q.listParams(0)).toEqual({
      kind: undefined,
      scope: 'ip',
      ip_id: 5,
      sort: 'last_used',
      limit: ASSET_LIST_PAGE_SIZE,
      offset: 0,
    })
  })

  it('scopes to the whole library without an ip dimension', () => {
    const kind = ref<'' | 'image' | 'video' | 'audio'>('image')
    const q = useAssetListQuery({ ipId: () => undefined, kind, count: () => 0 })

    expect(q.listParams(30)).toEqual({
      kind: 'image',
      scope: 'all',
      ip_id: undefined,
      sort: 'last_used',
      limit: ASSET_LIST_PAGE_SIZE,
      offset: 30,
    })
  })

  it('drops ip_id once the scope switches to all', () => {
    const kind = ref<'' | 'image' | 'video' | 'audio'>('')
    const q = useAssetListQuery({ ipId: () => 5, kind, count: () => 0 })
    q.scope.value = 'all'

    expect(q.listParams(0).ip_id).toBeUndefined()
    expect(q.listParams(0).scope).toBe('all')
  })

  it('reads ipId at call time (the editor resolves it async)', () => {
    // 面板可能先于画布 IP 落定挂载:归属须在每次构造参数时求值。
    let ipId: number | undefined = undefined
    const kind = ref<'' | 'image' | 'video' | 'audio'>('')
    const q = useAssetListQuery({ ipId: () => ipId, kind, count: () => 0 })

    expect(q.listParams(0).scope).toBe('all')
    ipId = 7
    expect(q.listParams(0)).toMatchObject({ scope: 'ip', ip_id: 7 })
  })

  it('hasMore only when a full page is loaded', () => {
    const kind = ref<'' | 'image' | 'video' | 'audio'>('')
    let count = 0
    const q = useAssetListQuery({ ipId: () => undefined, kind, count: () => count })

    expect(q.hasMore()).toBe(false)
    count = ASSET_LIST_PAGE_SIZE
    expect(q.hasMore()).toBe(true)
    // 非整页(尾页)不再有下一页。
    count = ASSET_LIST_PAGE_SIZE + 5
    expect(q.hasMore()).toBe(false)
  })
})
