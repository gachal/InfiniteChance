# Wayfinder Map: InfiniteChance — Token 网关 + 无限画布

Label: wayfinder:map

## Destination

一套可直接开工的完整规格:monorepo 目录结构、领域术语表、MySQL 数据模型、网关 OpenAI 兼容 API 契约(chat/images/videos)、画布节点语义与任务流、计费额度规则——足以让后续实现会话无需再做决策,即可分工实现 gateway/server、canvas/server、canvas/web、admin-web 四个组件。

## Notes

- 领域:自用 LLM token 网关 + 无限画布创作工具。网关以统一 OpenAI 兼容风格代理各厂商的聊天/生图/生视频;画布的生图、生视频、提示词生成、视频反推提示词一律经网关调用,不直连厂商。
- 已钉死的前提(绘制地图时 grilling 确定):
  - **目的地 = 完整开工规格**(非代码;执行在地图之外)。
  - **产品形态 = 自用工具**:单管理员、key 手工发放、额度手工充值;无注册、无支付、无多租户。
  - **骨架 = Monorepo·4模块·管理后台合一**:`gateway/server`(Go+Gin 网关)、`canvas/server`(Go+Gin 画布持久化与异步任务编排)、`canvas/web`(Vue3+vue-flow)、`admin-web`(Vue3 统一管理后台,内分「网关管理」「画布管理」两区)。
- 技术栈:Go+Gin、Vue3、vue-flow、MySQL、Redis。
- 每个会话应咨询的技能:/grilling、/domain-modeling(术语表落 `CONTEXT.md`)、/prototype、/research。
- Tracker:local-markdown——地图为本文件,ticket 在 `issues/`。

## Decisions so far

## Not yet specified

- 流式转发细节(SSE 透传、超时、重试与渠道降级)——等网关 API 契约定了再切票。
- 额度扣减的并发安全与失败退款时序——等计费模型定了再切票。
- 素材生命周期(对象存储选型、过期清理、缩略图)——等画布范围与部署形态清晰后切票。
- 统一管理后台的信息架构(页面清单、路由、两区如何分栏)——等计费与画布范围都定后切票。
- 安全与限流(key 泄漏防护、速率限制、管理后台鉴权)——等账号与计费细节定了再切票。
- 画布任务编排细节(并发生成、失败重试、节点状态机、断线恢复)——等画布范围定了再切票。
- OpenAI 兼容程度(function calling、多模态输入、embeddings 是否纳入)——等厂商调研与 API 契约一起定。

## Out of scope

- 注册登录、在线充值、多租户——「自用工具」形态已排除。
- 画布多人协作/实时协同——单人自用已排除。
