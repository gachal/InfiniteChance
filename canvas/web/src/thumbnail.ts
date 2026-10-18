/**
 * 素材缩略(49 号票):显示层的压缩地址派生与加载失败回落。
 *
 * 服务端 GET /assets/{id}/thumb 是纯 302 端点 —— 有 object_key 且活跃云
 * 驱动支持处理 → 云厂商原生处理参数 URL,其余一切(历史行、data: URI、
 * local 驱动、非图片行)→ 302 回 content。前端只管两件事:
 *
 *  1. 从内容寻址路径派生缩略地址(厂商地址 / data: URI 没有 thumb 端点,
 *     保持原图);
 *  2. <img> onerror 时把显示地址换回原图 —— CI 未开通、厂商 5xx、驱动切
 *     换前仅存 local 的对象 404 三种降级全靠这条回落兜底,所有显示点共用。
 *
 * 原图字节完全不动:下载、灯箱、发送链路(参考条 / 附件的 url 载荷)与
 * 视频节点的首帧技巧都不经这里。
 */
import { ref } from 'vue'

/** 内容寻址路径 → 缩略地址;非内容寻址(厂商 http(s) 地址、data: URI)
 * 返回 null,调用方保持原图。路径形态与服务端 contentPath/thumbPath 同源。 */
export function thumbnailURLOf(url: string): string | null {
  const m = /^\/api\/assets\/(\d+)\/content(?:\?.*)?$/.exec(url)
  return m ? `/api/assets/${m[1]}/thumb` : null
}

/** 一处显示点的回落簿:键 = 原图地址(节点重生成、chip 移除等场景键随内
 * 容走,无需清理;同一素材在不同显示点各自成簿,回落互不传染)。 */
export interface ThumbFallback {
  /** 显示地址:未回落 → 缩略;否则原图(回落链的终点)。thumb 传服务端
   * 下发的 thumbnail_url(AssertRecord 直挂点直接消费字段,49 号票「前端
   * 零判空」);载荷派生点(chip / 历史 / 节点 / 记录)不传,按原图地址
   * 内容寻址派生(票面「chip 按 asset_id 派生显示地址」同款策略)。 */
  displayURL(original: string, thumb?: string): string
  /** <img> onerror:缩略失败记回落并返回 true(响应式换 src 重载原图);
   * 已回落过或本就无缩略地址(原图自身失败)返回 false,由调用方按各自
   * 的不可用态收尾(占位 / 破图)。 */
  onThumbError(original: string): boolean
}

export function useThumbFallback(): ThumbFallback {
  const fallenBack = ref(new Set<string>())
  return {
    displayURL(original, thumb) {
      if (fallenBack.value.has(original)) {
        return original
      }
      return thumb || thumbnailURLOf(original) || original
    },
    onThumbError(original) {
      if (fallenBack.value.has(original) || thumbnailURLOf(original) === null) {
        return false
      }
      fallenBack.value = new Set([...fallenBack.value, original])
      return true
    },
  }
}
