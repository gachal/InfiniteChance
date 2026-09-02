# 03 — 渠道与 key 管理闭环

**What to build:** admin 后台的「网关管理」基础页:厂商渠道 CRUD(类型、BaseURL、密钥、公开模型名→渠道模型映射、优先级/权重、启用)与一键连通测试;API key 管理(创建时完整值仅显示一次、列表只露前缀、吊销、过期时间、额度);手工充值额度后 key 余额即时可见。

**Blocked by:** 02

**Status:** done

> 备注:吊销/过期的统一拒绝由 `internal/apikey.RequireKey` 中间件实现(OpenAI error object 形状,code 区分 missing/invalid/revoked/expired),已按 HTTP 缝测试验证;把它挂到 `/v1` 路由是 04 号票(中转面)的工作。Playwright「模拟调用→看到用量日志」冒烟同样依赖 04 号票的 `/v1` 端点。

- [x] 渠道增删改查可用,连通测试返回可判定的成功/失败
- [x] key 创建后完整值仅显示一次,吊销与过期生效
- [x] 手工充值后 key 余额变化,历史可追溯
- [x] 被吊销/过期 key 的请求被统一错误拒绝
