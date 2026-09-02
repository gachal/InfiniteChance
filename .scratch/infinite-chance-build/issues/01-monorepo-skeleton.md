# 01 — Monorepo 骨架与全栈健康通路

**What to build:** 从零搭出四模块骨架(gateway/server、canvas/server、canvas/web、admin-web),compose 拉起 MySQL 与 Redis;两个 Go 服务提供健康检查端点,报告 DB/Redis 连通状态;两个前端可启动并显示来自后端健康接口的真实状态。这条最小竖切打通「前端→后端→存储」,是后续所有票的落点。Go 侧按单 module 双入口组织,前端用 pnpm workspace 共享请求层。

**Blocked by:** None — can start immediately.

**Status:** done

- [x] docker compose up 后 MySQL、Redis 与两个 Go 服务全部就绪
- [x] 健康检查端点返回 DB 与 Redis 的连通状态
- [x] 两个前端 dev 服务器可启动,页面展示后端健康真实状态
- [x] Go 与前端的 lint/测试基线命令可用(空测试通过)
