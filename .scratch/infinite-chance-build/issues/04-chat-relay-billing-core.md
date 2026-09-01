# 04 — 聊天转发与计费核心(tracer)

**What to build:** 网关核心竖切:以 Bearer key 调 OpenAI 兼容 chat completions(非流式),网关按公开模型名经渠道映射选中渠道,经窄 Adaptor(构造 URL、鉴权头、请求转换、响应转换并产出用量)转发厂商,返回归一化响应;计费走「按估算预扣 → 完成后多退少补 → 失败退款」,并发安全靠额度字段条件更新;每请求落用量日志(渠道、模型、token、耗时、状态、扣费、倍率快照)。计价结构按双轨设计(本票先落 token 倍率轨,按次轨留给 07)。

**Blocked by:** 03

**Status:** ready-for-agent

- [ ] OpenAI SDK 改 base_url 与 key 即可非流式聊天拿到厂商回答(测试用内存 fake 上游验证)
- [ ] 用量日志逐请求落库且含倍率快照
- [ ] 并发请求下额度不超扣(真实 MySQL 条件更新验证)
- [ ] 额度不足返回 OpenAI error object 形状的明确错误;上游失败时已预扣额度退回
