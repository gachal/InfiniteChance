# 07 — 生图同步转发 + 按次计费

**What to build:** OpenAI 兼容 images 生成/编辑接口同步转发;双轨计价的「按次 USD 单价 × 尺寸系数」落地,预扣/结算/退款语义与聊天轨一致;仅会被路由到支持生图的渠道。

**Blocked by:** 04

**Status:** done

> 备注:渠道新增 `capabilities` 能力集(`chat`/`images`,JSON 列,EnsureSchema 原地加列);缺省视为仅 `chat`,生图必须显式开启,调度按能力过滤、聊天与生图互不串道。次轨落 `unit=call`(config:`usd_per_call_micros` + `size_factor_micros`,1e6 = ×1.0,未配置尺寸恒 ×1.0),admin API 人类单位 `usd_per_call` + `size_factors`。计费:预扣 = 单价 × 系数 × n(缺省 1,超出 1..100 直接 400 拒绝),结算按响应 `data` 实交张数多退少补(差额为零不落 settle 流水),失败全额退款;换道/熔断与聊天轨共用。`/v1/images/generations` 走 JSON 透传,`/v1/images/edits` 的 multipart 按渠道重建(仅换 model 字段,文件分节原样保留);响应 `model` 仅在厂商回带时回写公开名。用量日志 `unit=call`、token 恒 0,价格快照附 `request{size,n}` 供审计重算。

- [x] 以 OpenAI SDK 方式生图成功,返回 URL 或 base64(fake 上游验证)
- [x] 按次计费正确入账,失败预扣退回
- [x] 生图请求不会被不支持生图的渠道选中
