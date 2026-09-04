# InfiniteChance

自用 Token 网关 + 无限画布。规格见 [.scratch/infinite-chance/spec.md](.scratch/infinite-chance/spec.md),任务拆解见 [.scratch/infinite-chance-build/issues/](.scratch/infinite-chance-build/issues/)。

## 结构

```
gateway/server   Go+Gin 网关入口(OpenAI 兼容 API,票 04 起)
canvas/server    Go+Gin 画布持久化与任务编排入口(票 09 起)
canvas/web       Vue3 创作画布前端(:5174 dev / :8091 部署)
admin-web        Vue3 统一管理后台(:5173 dev / :8090 部署)
packages/api     前端共享请求层(@infinitechance/api)
packages/ui      前端共享组件(HealthCard 健康卡片)
deploy/          部署物:nginx 配置 + 备份/恢复脚本(票 16)
```

Go 侧单 module(`github.com/gachal/InfiniteChance`)、双入口;`internal/` 为两服务共享代码。前端为 pnpm workspace。

## 一键部署(票 16)

全新机器只要装了 Docker:

```bash
cp .env.example .env   # 可选;所有配置项都有内置缺省
docker compose up -d --build
```

一条命令拉起六件套:MySQL、Redis、gateway、canvas,以及两个前端——Dockerfile 多阶段先在容器内 `pnpm build` 出两个 SPA 静态产物,再交给两个 nginx 运行时托管并反代对应后端;**运行时容器只含镜像与构建产物,不依赖宿主机源码、Node 或 pnpm**。

然后访问管理后台 `http://localhost:8090` 完成初始化引导(两步):

1. 创建唯一管理员账号(密码仅以 bcrypt 哈希入库);
2. 录入首个厂商渠道(OpenAI 兼容 BaseURL + 密钥 + 可选模型映射,可跳过)。

之后到「API Keys」创建一把服务级 key 填入 `.env` 的 `CANVAS_SERVICE_KEY` 并 `docker compose up -d`,画布的 AI 动作即可用。

公网/长期部署:`openssl rand -hex 32` 生成 `JWT_SECRET` 填入 `.env`,并把 `JWT_SECRET_REQUIRED=true`(密钥缺失时服务拒绝启动)。全部可配项与注释见 [.env.example](.env.example)。

前端开发仍可走 dev 服务器(热更新):`pnpm install && make dev-admin`(:5173)/ `make dev-canvas`(:5174),经 vite 代理访问 8080/8081。

## 端口

| 服务 | 端口 | 说明 |
| --- | --- | --- |
| admin-web(部署) | 8090 | 管理后台静态托管,初始化引导入口 |
| canvas-web(部署) | 8091 | 无限画布静态托管 |
| gateway/server | 8080 | 网关 API(OpenAI 兼容 `/v1`) |
| canvas/server | 8081 | 画布 API |
| admin-web(dev) | 5173 | 管理后台 dev 服务器 |
| canvas/web(dev) | 5174 | 画布前端 dev 服务器 |
| MySQL | 宿主机 3307 → 容器 3306 | 避开本机常见 3306 占用 |
| Redis | 宿主机 6380 → 容器 6379 | 避开本机常见 6379 占用 |

以上端口都可用 `.env` 覆写(`ADMIN_WEB_PORT`、`CANVAS_WEB_PORT`、`GATEWAY_PORT`、`CANVAS_PORT`、`MYSQL_PORT`、`REDIS_PORT`)。

宿主机直跑 Go 服务时(`go run ./gateway/server`),默认连接 `localhost:3306/6379`;若基础设施用的是 compose 映射出来的端口,设置:

```bash
export MYSQL_DSN='root:infinitechance@tcp(localhost:3307)/infinitechance?parseTime=true'
export REDIS_ADDR=localhost:6380
```

## 备份与恢复(票 16)

`deploy/backup.sh` 把三处状态打包进一个带清单的目录(MySQL 一致性逻辑转储、Redis RDB、素材卷整卷 tar,附时间戳/git 提交/SHA-256 清单),`deploy/restore.sh` 把目录原样灌回运行中的栈(覆盖式,先确认再动手):

```bash
make backup                          # → backups/YYYYmmdd-HHMMSS/
make restore DIR=backups/YYYYmmdd-HHMMSS   # 追加 Y=1 跳过交互确认
```

两者都要求 compose 栈在运行(借助容器内的 mysqldump/redis-cli/tar,宿主机无需装任何数据库工具)。备份打包素材卷时 canvas 会停服数秒(防 tar 撕裂,在途画布任务随重启恢复机制照常重跑);跨三个存储间没有全局一致点,建议在低峰期执行。定期备份示例(每天凌晨 3 点):

```
0 3 * * * cd /path/to/InfiniteChance && deploy/backup.sh >> backups/backup.log 2>&1
```

恢复演练:备份 → `docker compose down -v` 清掉全部数据卷 → `docker compose up -d` → `deploy/restore.sh <目录> -y` → 验证登录、渠道、Redis 键与素材俱在。

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

依赖不可达时对应 `checks` 项为 `"status": "down"` 并带 `error` 摘要。compose 里 gateway/canvas 各挂了 `/healthz` 健康检查,两个前端容器等后端 healthy 才启动——页面打开即服务可用。

## 管理端鉴权(票 02)

单管理员账号 + JWT 会话,网关与画布共用一套账号体系:

- **首次使用**:全新库访问管理后台时出现初始化引导,创建唯一管理员账号并顺路录入首个厂商渠道(票 16);初始化完成后引导不再出现。
- **登录**:网关校验账号密码后签发 HS256 JWT(有效期 7 天);无 token、签名不符或过期的请求一律返回标准 401(`{"error":{"code","message"}}` + `WWW-Authenticate`)。
- **跨服务校验**:canvas/server 用同一密钥校验网关签发的 JWT,`JWT_SECRET` 环境变量必须两个服务一致。compose 透传宿主机的 `JWT_SECRET`;未设置时两服务回退到内置开发密钥(仅限本地开发)。生产部署请设置 `JWT_SECRET` 并开启 `JWT_SECRET_REQUIRED=true`——后者会在密钥缺失时拒绝启动,避免带着公开密钥上线。

`GET /auth/status`、`POST /auth/init`、`POST /auth/login` 为公开端点;`GET /auth/me` 需 Bearer token(网关与画布均提供)。

## 常用命令

```bash
make up          # compose 拉起全栈(六个服务)
make down        # 停止(数据卷保留)
make backup      # 备份 MySQL/Redis/素材卷 → backups/<时间戳>/
make restore     # 从备份恢复(需 DIR= 参数,见上)
make test        # go vet + go test + pnpm -r test
make lint        # go vet + pnpm -r lint
```

配置经环境变量注入:`PORT`、`MYSQL_DSN`、`REDIS_ADDR`、`JWT_SECRET`(服务),compose 栈的端口/密码/密钥经 `.env` 注入(见 `.env.example`)。
