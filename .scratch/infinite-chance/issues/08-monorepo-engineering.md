# 08 Monorepo 工程规范

Type: grilling
Status: open
Blocked by: 03

## Question

monorepo 的工程规范如何定?

- Go 项目布局(cmd/internal/pkg)、Go module 路径(github.com/YuJiaQuan/InfiniteChance/…?)、gateway/server 与 canvas/server 是同一 module 还是两个
- 前后端契约:手写 OpenAPI 再生成 TS 客户端?还是手写前端类型?管理后台与两个后端 API 的客户端如何组织
- admin-web、canvas/web 共享组件/请求层的机制(pnpm workspace 共享包?)
- lint/test/CI 基线(golangci-lint、eslint、GitHub Actions?)
- 本地开发编排:两个 Go 服务分进程跑还是单二进制多角色?Makefile/taskfile 约定

## Answer
