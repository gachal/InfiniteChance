# 02 开源网关参考调研(one-api/new-api)

Type: research
Status: open
Blocked by:

## Question

one-api、new-api 等开源 LLM 网关在以下方面是怎么做的?产出可借鉴要点与应避开的坑清单,侂数据模型(05)与计费模型(04)参考:

- 渠道(channel)适配层架构:如何抽象「OpenAI 兼容/厂商原生」两类上游,模型名映射怎么做
- 数据模型:channel、token(key)、usage log、quota 的表结构与字段
- 计费:倍率(group/model ratio)、预扣与退款、按次计费的生图怎么处理
- OpenAI 兼容层:流式 SSE 转发、错误格式归一化的实现方式
- 有哪些已知的设计债或社区抱怨(如渠道健康检查、并发扣减精度问题)

## Answer
