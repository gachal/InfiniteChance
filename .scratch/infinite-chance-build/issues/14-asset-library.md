# 14 — 素材库管理与跨画布复用

**What to build:** 画布内素材面板浏览历史素材并插入当前画布(跨画布复用同一素材引用而非复制);admin「画布管理」区的素材管理页:按类型/来源画布过滤、预览、下载、删除;素材以 S3 兼容接口抽象存储,图片/视频按画布/任务归档。

**Blocked by:** 10

**Status:** done

> 备注:新增 `internal/objectstore`(S3 兼容 Store 接口 + 本地卷 FileSystem 落地,键 `canvases/{canvasID}/{taskID}/{kind}.{ext}` 即归档布局,env `ASSET_STORAGE_DIR`,compose 挂 `asset-data` 卷)。`assets` 表原地加列 `object_key/content_type/size_bytes`;worker 在任务终态前经 `asset.Transfer` 转存产物(厂商 http 地址下载或 data URI 解码,256 MiB 上限),转存失败按任务失败收尾可重试;厂商原地址保留作出处(图生视频参考图解析仍用它)。管理 API 挂 canvas/server:`GET /assets`(kind/canvas_id 过滤 + 分页,LEFT JOIN 画布名)与 `DELETE /assets/:id`(对象先删、行后删)挂 JWT;`GET /assets/:id/content` 支持从自有存储流出字节,`?download=1` 走 attachment(历史行退化为代理)。前端:画布编辑器新增「素材库」面板(类型过滤、缩略图、插入/下载);插入与任务成功落位都写 `asset_id` + `/api/assets/{id}/content` 进节点 —— 跨画布复用同一素材引用;图生视频参考图与 13 号票同形支持内容寻址解析。删除素材后引用节点的 `<img>/<video>` @error 显示「素材不可用」占位而非破图/报错。

- [x] 素材面板可将历史素材插入当前画布,跨画布复用为同一引用
- [x] admin 可过滤、预览、下载、删除素材
- [x] 删除素材后引用它的画布节点显示占位而非报错
