# 07 — 生图同步转发 + 按次计费

**What to build:** OpenAI 兼容 images 生成/编辑接口同步转发;双轨计价的「按次 USD 单价 × 尺寸系数」落地,预扣/结算/退款语义与聊天轨一致;仅会被路由到支持生图的渠道。

**Blocked by:** 04

**Status:** ready-for-agent

- [ ] 以 OpenAI SDK 方式生图成功,返回 URL 或 base64(fake 上游验证)
- [ ] 按次计费正确入账,失败预扣退回
- [ ] 生图请求不会被不支持生图的渠道选中
