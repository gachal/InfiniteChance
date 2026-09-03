# 08 — 视频异步任务契约与上游代理

**What to build:** 自定义 OpenAI 风格的视频异步接口:提交返回 task_id、轮询查询状态、取消进行中任务;网关代理各厂商异步任务并做状态归并——对外收敛五态 queued / running / succeeded / failed / canceled(上游节流态归并 queued、未知态归并 failed;该五态契约来自 wayfinder 厂商调研票);「仅成功计费」,失败预扣全额退回。

**Blocked by:** 07

**Status:** done

> 备注:新表 `video_tasks`(网关生成 `vt_` 任务 id 主键、钉死的渠道快照、上游 task id、外部五态 + 最近一次上游原始状态、size/seconds、产物 URL、error、预扣/实扣、价格快照;`internal/videotask` 包含状态机与条件更新 store——终态守卫在 UPDATE 的 WHERE 里,并发轮询/取消只有一个赢家能动账)。对外契约:`POST /v1/videos/generations`(提交回 `task_id`,seconds 缺省 5、限 1..100,`image` 可选图生视频参考,JSON 全量透传仅换 model)、`GET /v1/videos/tasks/{id}`(代理轮询:实时查上游并归并;终态后只答账本事实不打扰上游;轮询暂时失败不改状态不动账)、`POST /v1/videos/tasks/{id}/cancel`(上游取消尽力而为,本地取消 + 全额退款;已终态任务原样回答)。状态归并词汇表收进 `videotask.MergeStatus`(大小写不敏感:submitted/PENDING/Queueing/Preparing/IN_QUEUE/THROTTLED→queued;processing/RUNNING/IN_PROGRESS→running;succeed/Success/COMPLETED→succeeded;CANCELED/CANCELLED→canceled;UNKNOWN/expired 及一切未识别串→failed;queued→running 只进不退)。计价新落 `unit=second` 轨(算法与 call 轨同构:⌈每秒单价 × 分辨率系数⌉ × 秒数),「仅成功计费」:提交按秒预扣、成功定格实扣(零差额不落 settle)、失败/取消全额退款、成功却无产物 URL 按失败退款;渠道新增 `videos` 能力,视频只落 `videos` 渠道,提交失败换道与熔断语义与同步轨共用,换道史存任务行并进最终用量行摘要列。上游契约由 adaptor 定义(与对外同形的 `/videos/generations`、`/videos/tasks/{id}`、`/videos/tasks/{id}/cancel`),真实厂商接入时在 adaptor 内转换。

- [x] 提交→轮询→成功拿到视频产物;取消后状态为 canceled 且不扣费
- [x] fake 上游的多种真实状态被正确归并为对外五态
- [x] 失败任务预扣额度全额退回,任务与用量日志可查
