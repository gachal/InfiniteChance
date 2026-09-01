# 03 网关统一 API 契约

Type: grilling
Status: open
Blocked by: 01

## Question

网关对外暴露的 OpenAI 兼容端点集合与请求/响应/错误格式如何定义?

- chat completions(含 SSE 流式)、images generations/edits、**视频异步任务接口**(OpenAI 无官方生视频 API——自定义 OpenAI 风格:提交返回 task_id + 查询/取消端点?任务状态枚举?)各自的字段与语义
- 模型名映射规则:公开模型名 → 渠道+厂商模型的解析方式
- 鉴权:Bearer API key;错误码与 OpenAI error object 的归一化映射
- 兼容到什么程度:function calling、多模态输入、embeddings 是否纳入 v1
- 流式转发语义:透传 vs 网关重组;用量(token 数)在流式下如何统计

## Answer
