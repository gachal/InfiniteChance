# 10 — 文生图节点闭环

**What to build:** 画布中从提示词节点发起文生图:canvas/server 以服务级 key 调网关生图接口并在请求上标记画布来源;生成以任务表驱动、服务端编排,浏览器关闭后任务继续、重开可查;产物自动入素材库;节点上呈现排队/生成中/成功/失败,失败可原地重试。

**Blocked by:** 09, 07

**Status:** done

> 备注:新表 `canvas_tasks`(`ct_` 任务 id、`node_id` 绑定编辑器节点、四态 `queued/running/succeeded/failed`、attempts 计数)与最小 `assets` 表;`internal/canvastask` 含条件更新状态机 store、网关客户端与 worker(并发认领、成功时「素材落行+任务终态」同事务、失败落原因、启动时 running 孤儿回队)。canvas/server 配 `CANVAS_SERVICE_KEY`/`CANVAS_GATEWAY_URL`/`CANVAS_TASK_CONCURRENCY`,未配 key 时提交以 `gateway_unconfigured` 拒绝。网关 `usage_logs` 原地加 `source` 列,`X-InfiniteChance-Source` 头原样入列(画布侧 `canvas=<id> task=<ct_…> node=<节点id>`)。前端:提示词节点带模型选择与生成按钮,结果图片节点先落库再提交任务;`useCanvasTasks` 有活动任务时轮询列表、产物写回节点(data URI 走 `GET /assets/{id}/content` 内容寻址,该路由不挂 JWT 供 img 预览);失败节点原地重试(409 静默)。模型目录 `GET /image-models` = 按次计价的公开模型。

- [x] 点生成→节点经等待后显示图片;关闭浏览器重开后任务结果不丢
- [x] 网关用量日志可见画布来源标记,按次计费入账正确
- [x] 失败任务可在节点上重试,预扣/退款一致
- [x] 生成图片自动成为素材且节点内可预览
