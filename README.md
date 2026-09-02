# InfiniteChance

自用 Token 网关 + 无限画布。规格见 [.scratch/infinite-chance/spec.md](.scratch/infinite-chance/spec.md),任务拆解见 [.scratch/infinite-chance-build/issues/](.scratch/infinite-chance-build/issues/)。

## 结构

```
gateway/server   Go+Gin 网关入口(OpenAI 兼容 API,票 04 起)
canvas/server    Go+Gin 画布持久化与任务编排入口(票 09 起)
canvas/web       Vue3 创作画布前端(:5174)
admin-web        Vue3 统一管理后台(:5173)
packages/api     前端共享请求层(@infinitechance/api)
packages/ui      前端共享组件(HealthCard 健康卡片)
```

Go 侧单 module(`github.com/gachal/InfiniteChance`)、双入口;`internal/` 为两服务共享代码。前端为 pnpm workspace。

## 快速开始

```bash
# 1. 拉起 MySQL + Redis + 两个 Go 服务
docker compose up -d --build

# 2. 前端(pnpm 8+,Node 20+)
pnpm install
make dev-admin    # 管理后台  http://localhost:5173
make dev-canvas   # 无限画布  http://localhost:5174
```

两个前端页面通过 dev 代理(`/api` → 对应后端)展示服务真实健康状态,每 10 秒自动刷新。

## 端口

| 服务 | 端口 | 说明 |
| --- | --- | --- |
| gateway/server | 8080 | 网关 API |
| canvas/server | 8081 | 画布 API |
| admin-web | 5173 | 管理后台 dev |
| canvas/web | 5174 | 画布前端 dev |
| MySQL | 宿主机 3307 → 容器 3306 | 避开本机常见 3306 占用 |
| Redis | 宿主机 6380 → 容器 6379 | 避开本机常见 6379 占用 |

宿主机直跑 Go 服务时(`go run ./gateway/server`),默认连接 `localhost:3306/6379`;若基础设施用的是 compose 映射出来的端口,设置:

```bash
export MYSQL_DSN='root:infinitechance@tcp(localhost:3307)/infinitechance?parseTime=true'
export REDIS_ADDR=localhost:6380
```

## 健康检查契约

`GET /healthz`(两个服务相同),全部依赖可达返回 200,否则 503:

```json
{
  "service": "gateway",
  "status": "ok",
  "checks": {
    "mysql": { "status": "up" },
    "redis": { "status": "up" }
  }
}
```

依赖不可达时对应 `checks` 项为 `"status": "down"` 并带 `error` 摘要。

## 常用命令

```bash
make up          # compose 拉起全栈
make down        # 停止(数据卷保留)
make test        # go vet + go test + pnpm -r test
make lint        # go vet + pnpm -r lint
```

配置经环境变量注入:`PORT`、`MYSQL_DSN`、`REDIS_ADDR`。
