# 08 — 视频异步任务契约与上游代理

**What to build:** 自定义 OpenAI 风格的视频异步接口:提交返回 task_id、轮询查询状态、取消进行中任务;网关代理各厂商异步任务并做状态归并——对外收敛五态 queued / running / succeeded / failed / canceled(上游节流态归并 queued、未知态归并 failed;该五态契约来自 wayfinder 厂商调研票);「仅成功计费」,失败预扣全额退回。

**Blocked by:** 07

**Status:** ready-for-agent

- [ ] 提交→轮询→成功拿到视频产物;取消后状态为 canceled 且不扣费
- [ ] fake 上游的多种真实状态被正确归并为对外五态
- [ ] 失败任务预扣额度全额退回,任务与用量日志可查
