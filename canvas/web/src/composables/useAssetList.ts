/**
 * 素材列表面的共享状态(50 号票两处成对实现的收敛,评审修复提取):
 * AssetPanel 与 AssetPicker 持有同一套「IP 归属视图 scope + 列表参数
 * 构造 + 分页窗口判断」,此前逐字重复、改动要两处同步;收进本 composable
 * 后行为与原实现逐字节等价(ipId 走 getter,保留调用时求值 —— 编辑器的
 * canvasIPID 异步落定,面板可能在归属未就绪时已挂载)。
 */
import { ref, type Ref } from 'vue'

/** 素材列表过滤档(与素材域 kind 同形,'' = 全部)。 */
export type AssetListKind = '' | 'image' | 'video' | 'audio'

/** 分页窗口:一页 30 条,「加载更多」向后翻页;上一页拉满才可能出现下一页
 * (与素材面板/选择器原实现同宽)。 */
export const ASSET_LIST_PAGE_SIZE = 30

export interface AssetListQuery {
  /** 归属视图:'ip' = 本 IP + 全局(缺省),'all' = 全库。 */
  scope: Ref<'ip' | 'all'>
  /** GET /assets 的列表参数(最近使用序,分页窗口同宽)。 */
  listParams: (offset: number) => {
    kind: 'image' | 'video' | 'audio' | undefined
    scope: 'ip' | 'all'
    ip_id: number | undefined
    sort: 'last_used'
    limit: number
    offset: number
  }
  /** 已加载条数是否还可能有下一页。 */
  hasMore: () => boolean
}

/** 归属视图与列表参数的单一来源:ipId 声明「本 IP」(缺省 = 无 IP 维度,
 * 恒全库);kind 是组件自己的过滤档 ref;count 回答当前已加载条数。 */
export function useAssetListQuery(options: {
  ipId: () => number | undefined
  kind: Ref<AssetListKind>
  count: () => number
}): AssetListQuery {
  /** 归属视图:'ip' = 本 IP + 全局(缺省),'all' = 全库。 */
  const scope = ref<'ip' | 'all'>('ip')

  const listParams = (offset: number) => ({
    kind: options.kind.value === '' ? undefined : options.kind.value,
    scope: (options.ipId() !== undefined ? scope.value : 'all') as 'ip' | 'all',
    ip_id: options.ipId() !== undefined && scope.value === 'ip' ? options.ipId() : undefined,
    sort: 'last_used' as const,
    limit: ASSET_LIST_PAGE_SIZE,
    offset,
  })

  const hasMore = () =>
    options.count() >= ASSET_LIST_PAGE_SIZE && options.count() % ASSET_LIST_PAGE_SIZE === 0

  return { scope, listParams, hasMore }
}
