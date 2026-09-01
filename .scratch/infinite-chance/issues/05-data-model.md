# 05 MySQL 数据模型与 Redis 用途

Type: grilling
Status: open
Blocked by: 03, 04

## Question

核心表结构如何设计,Redis 承担什么?

- 表:管理员(单管理员形态简化到什么程度——还建 users 表吗)、api_key(哈希存储、过期、启用)、channel(厂商类型、密钥、模型映射、优先级/权重、启用)、usage_log、task(视频等异步任务)、canvas/node/edge/asset、prompt_template(若画布需要)
- 各表关键索引;日志与任务的保留/归档策略
- Redis 用途划界:渠道选择缓存?分布式限流?异步任务状态缓存?幂等去重?
- 画布图的持久化粒度对表设计的影响(整图 JSON vs 节点级表)

## Answer
