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

## 管理端鉴权(票 02)

单管理员账号 + JWT 会话,网关与画布共用一套账号体系:

- **首次使用**:全新库访问管理后台时出现初始化引导,创建唯一管理员账号(密码仅以 bcrypt 哈希入库);初始化完成后引导不再出现。
- **登录**:网关校验账号密码后签发 HS256 JWT(有效期 7 天);无 token、签名不符或过期的请求一律返回标准 401(`{"error":{"code","message"}}` + `WWW-Authenticate`)。
- **跨服务校验**:canvas/server 用同一密钥校验网关签发的 JWT,`JWT_SECRET` 环境变量必须两个服务一致。compose 透传宿主机的 `JWT_SECRET`;未设置时两服务回退到内置开发密钥(仅限本地开发)。生产部署请设置 `JWT_SECRET` 并开启 `JWT_SECRET_REQUIRED=true`——后者会在密钥缺失时拒绝启动,避免带着公开密钥上线。

`GET /auth/status`、`POST /auth/init`、`POST /auth/login` 为公开端点;`GET /auth/me` 需 Bearer token(网关与画布均提供)。

## 常用命令

```bash
make up          # compose 拉起全栈
make down        # 停止(数据卷保留)
make test        # go vet + go test + pnpm -r test
make lint        # go vet + pnpm -r lint
```

配置经环境变量注入:`PORT`、`MYSQL_DSN`、`REDIS_ADDR`、`JWT_SECRET`。
