# 15 — 用量审计视图

**What to build:** admin「网关管理」区的用量审计:请求级日志列表(时间、key、渠道、模型、计费单位与数量、耗时、状态、扣费、画布来源标记)支持按时间/key/渠道/模型/状态过滤分页;按天、按模型、按渠道的汇总视图,数字与请求级日志一致。

**Blocked by:** 04

**Status:** done

> 备注:新增 `internal/usage` 的审计读取面 —— `Store` 扩展 `List`(过滤 + 分页,最新在前,随页返回过滤后总数)与 `Summary`(by=day/model/channel 聚合;与 List 共用同一 WHERE,数字对账一致;按天按库会话时区 UTC 取整,模型/渠道按扣费降序)。管理 API 挂网关:`GET /admin/usage/logs`(from/to RFC3339 含头不含尾、key_id、channel_id、model 精确匹配、status、source=canvas|direct;limit 缺省 50 上限 500 + offset)与 `GET /admin/usage/summary?by=…`(同一套过滤)。按次/按秒轨的计费数量(尺寸 × 张/秒数)从行内 `price_snapshot.request` 解出为响应的 `request` 字段,token 轨读 token 列;失败行 `upstream_error` 摘要随行返回。前端:网关管理区新增「用量审计」页(UsageView.vue,路由 /usage)—— 请求明细表(时间/key/渠道/模型/用量/耗时/状态/扣费/来源)与三种汇总桶共用一条过滤栏;画布来源以徽章区分(解析画布 id,完整标记悬停可见),失败行内联上游错误摘要,total 驱动上一页/下一页。共享请求层 `@infinitechance/api` 增 `listUsageLogs`/`usageSummary` 与类型。

- [x] 日志列表过滤与分页正确,失败请求含上游错误摘要
- [x] 按天/模型/渠道汇总与请求级明细对账一致
- [x] 画布来源的用量可在视图中区分
