# 16 — 部署固化与备份

**What to build:** docker-compose 一键全栈:两个前端构建产物静态托管、MySQL/Redis/素材卷编排、env 配置样例;首次初始化引导串起管理员账号与首个渠道;MySQL、Redis、素材卷的备份与恢复脚本。

**Blocked by:** 12, 13, 14, 15

**Status:** done

> 备注:compose 扩为六服务——新增 `admin-web`(:8090)与 `canvas-web`(:8091)两个 nginx 运行时,由 Dockerfile 同文件多阶段构建:`web-build` 阶段容器内 `pnpm install --frozen-lockfile` 后分别 `pnpm --filter admin-web/canvas-web build` 编出两个 SPA dist,分别 COPY 进 `nginx:1.28-alpine`,反代规则与 dev 代理同形(`deploy/nginx/admin.conf`:/api → gateway、/canvas-api → canvas;`canvas.conf`:/api → canvas;SPA history 回退 + gzip + 300s 读超时),运行时容器只含镜像与构建产物。gateway/canvas 各挂 `/healthz` 健康检查(busybox wget),前端容器等后端 healthy 才启动;全部服务 `restart: unless-stopped`;宿主机端口与 MySQL root 密码改由 env 参数化,样例 `.env.example`(JWT_SECRET/JWT_SECRET_REQUIRED/MYSQL_ROOT_PASSWORD/CANVAS_SERVICE_KEY/CANVAS_TASK_CONCURRENCY/六个端口)。redis 补挂 `redis-data` 数据卷(RDB 随 compose down 保留、进备份链路)。初始化引导(InitView.vue)扩为两步:管理员账号(POST /auth/init,成功即登录)→ 首个渠道(POST /admin/channels,与渠道管理页同形,可跳过)。备份/恢复:`deploy/backup.sh`(mysqldump --single-transaction --routines --triggers --databases、redis-cli SAVE 后取 RDB、素材卷在 canvas 停服窗口内整卷 tar 防撕裂,产物 + MANIFEST.txt 时间戳/git 提交/SHA-256)与 `deploy/restore.sh`(MySQL 重放转储;Redis stop → 同服务临时容器写 RDB → start,绕开 SIGTERM 落盘覆盖;素材卷停服清空重灌;-y 跳过确认),密码全程引用容器内 MYSQL_ROOT_PASSWORD;`make backup` / `make restore DIR=…`。恢复演练通过:备份 → down -v → up → restore → 登录/渠道/Redis 键/素材字节俱在,演练前的原数据快照也已回灌。

- [x] 全新机器上一条 compose 命令拉起全栈并完成初始化引导
- [x] 备份脚本产出可恢复的备份,恢复演练通过
- [x] 运行时仅依赖镜像与构建产物,无源码依赖
