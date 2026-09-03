# 12 — 图生视频节点闭环

**What to build:** 从图片节点(或素材)发起图生视频:canvas/server 经网关视频异步契约提交任务,节点轮询呈现进度;进行中可取消(取消不计费);产物视频入素材库并在视频节点内可播放;失败可重试。

**Blocked by:** 10, 08

**Status:** done

> 备注:`canvas_tasks` 原地加列(`seconds`/`image_ref`/`video_url`/`remote_task_id`),状态机加入 `canceled`(仅视频可达——取消端点对 image 任务以 409 拒绝,`Terminal()` 收敛终态)。`kind=video` 提交要求参考图 `image_url`(必须 http(s)、≤4096 字符,与网关参考图契约一致;data URI 在边界即拒,编辑器入口也只对 http(s) 产物出现)与 `seconds`(缺省 5,1..100),模型须按秒计价;模型目录 `GET /video-models` = second 轨公开模型。worker 视频路径:提交 `/v1/videos/generations` → 远端句柄落行(`AttachRemote`,守卫 running;提交在途被取消时 worker 输掉守卫并当场取消刚受理的网关任务退预扣)→ 3s 轮询至终态,每拍先复查本地行——handler 的网关取消 RPC 若曾失败,worker 就地补一次取消(预扣原路退回),堵住「已取消却被结算」的口子;暂态轮询失败不动任务,15min 时限到先取消网关任务再按失败收尾(可重试)。取消 `POST /canvases/:id/tasks/:tid/cancel`:先取消网关任务(网关本地取消并全额退预扣)再守卫关闭行为 canceled,已终态原样回放。重试/重启恢复带旧远端句柄入队时,worker 提交前先取消旧句柄(释放崩溃遗留的预扣)。成功落 `kind=video` 素材,`video_url` 写回视频节点内嵌播放。前端:图片节点带图生视频表单(模型/提示词/时长),视频节点呈现排队/生成中(可取消)/失败(可重试)/已取消/播放;`useCanvasTasks` 增 `cancel`;任务列表按 kind 把 `image_url`/`video_url` 写回节点,刷新断线后状态与产物正确恢复。「(或素材)」的独立入口随素材库管理面(14 号票)落地——当前素材即图片节点持有的产物引用,同一条提交路径已就绪。

- [x] 图片节点→生成→视频节点出现可播放视频
- [x] 进行中取消后状态 canceled 且不扣费;失败可重试且预扣退回
- [x] 刷新或断线后任务状态正确恢复
