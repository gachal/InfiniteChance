# AGENTS.md — 编码代理工作指南

InfiniteChance:自用 LLM Token 网关(OpenAI 兼容中转)+ 无限画布创作工具。**单管理员、单用户形态**——没有注册、支付、多租户,不要往代码里加这些。文档、代码注释与提交信息均以中文为主(见「提交与验证纪律」)。

## 必读文档(改动前先查,按优先级)

- **[CONTEXT.md](CONTEXT.md) — 领域术语表,动任何领域行为前必读**。渠道/调度熔断/API key/额度/计价/生图转发/任务/素材/设置等全部核心语义都在这里;术语冲突时以较新的决策票为准。
- `.scratch/infinite-chance/spec.md` — 总规格;`.scratch/infinite-chance/issues/` — 决策票(01–23 号),历史决策的原始出处。
- [docs/adr/](docs/adr/) — 架构决策记录(SQLite 双方言、生成对话框范式);[docs/api.md](docs/api.md) — API curl 用例。
- [README.zh-CN.md](README.zh-CN.md) — 部署、端口、鉴权契约的完整叙述。

**工作流是「先票后码」**:新需求先经 grilling 切票写进 `issues/`,定案后再实现;领域语义变化必须**回写 CONTEXT.md**;重大架构取舍落 `docs/adr/`。

## 仓库结构

| 路径 | 内容 |
| --- | --- |
| `gateway/server/` | Go+Gin 网关入口(OpenAI 兼容 `/v1`、管理面 `/admin`、`/auth`),仅 main.go |
| `canvas/server/` | Go+Gin 画布服务入口(画布 CRUD、任务编排、素材面),仅 main.go |
| `internal/` | 两服务共享的业务包(见下「internal 包速览」) |
| `canvas/web/` | Vue3 + vue-flow 画布前端(dev :5174 / 部署 :8091) |
| `admin-web/` | Vue3 管理后台(dev :5173 / 部署 :8090),内分「网关管理」「画布管理」两区 |
| `packages/api`、`packages/ui` | 前端共享包 `@infinitechance/api`(请求层)、`@infinitechance/ui`(组件) |
| `desktop/` | Wails v3 桌面壳:单进程装双服务,SQLite、无 Redis,画布主窗 + 管理台配置窗 |
| `deploy/` | nginx 配置 + `backup.sh` / `restore.sh` |
| `internal/objectstore` | 对象存储抽象:`Store` 接口 + local 卷 / OSS / COS 驱动 + `Dynamic` 按设置路由 |

### internal 包速览

`relay`(OpenAI 兼容中转:调度、熔断、SSE 流、生图、视频任务、VOD adaptor)· `apikey`(key 存储与 `/v1` 鉴权中间件)· `channel`(渠道 CRUD 与连通测试)· `pricing`(双轨计价)· `usage`(用量日志与审计)· `videotask`(视频异步任务)· `canvas` / `canvastask`(画布持久化 / 任务 worker)· `asset`(素材库)· `objectstore`(见上)· `settings`(运行时 KV 配置,存储驱动行)· `auth`(单管理员 + JWT)· `prompttemplate` / `promptgen`(提示词模板 / 生成)· `tc3`(腾讯云 TC3 签名)· `apierr`(统一错误形状)· `config` / `app` / `server` / `health` / `wiring` / `sqlitedb`(装配层:env 配置、服务组装、HTTP 基面、健康检查、路由挂载、SQLite 方言)。

## 技术形态

- Go 1.26,**单 module**(`github.com/gachal/InfiniteChance`)双入口;服务二进制的全部逻辑在 `internal/`,`gateway/server` 与 `canvas/server` 只是入口。
- `internal/wiring` 把两个服务的路由树挂上 gin;**wiring 不建 schema——每个 store 的 `EnsureSchema` 必须先跑**(MySQL 与 SQLite 双方言都要建,ADR 0001)。
- 前端 pnpm workspace:Vue3 + TypeScript + Vite;**`build` 脚本内含 `vue-tsc --noEmit`**,类型错误在 build 而非 lint 报出。
- Docker 栈六服务:MySQL、Redis、gateway(:8080)、canvas(:8081)、admin-web(:8090)、canvas-web(:8091);MySQL 宿主机 **3307**、Redis **6380**(避开常见占用)。

## 常用命令

```bash
make up            # compose 拉起全栈(六服务)
make down          # 停止(数据卷保留)
make test          # go vet + go test ./... + pnpm -r test
make lint          # go vet + pnpm -r lint
make build         # 前端产物(Go 用 go build ./...)
make dev-admin     # 管理后台 dev 服务器(:5173)
make dev-canvas    # 画布前端 dev 服务器(:5174)
make backup        # 备份 MySQL/Redis/素材卷 → backups/<时间戳>/
make restore       # 恢复:make restore DIR=backups/<时间戳> [Y=1]
make desktop       # 构建两个 SPA 并内嵌 → desktop/build/bin/InfiniteChance
go run ./gateway/server   # 宿主机直跑网关
go run ./canvas/server    # 宿主机直跑画布服务
```

### 测试

- `go test ./...`:MySQL 集成测试(`*_mysql_test.go`)默认连 compose 的宿主机 3307,数据库不可达时**自动 skip**(无基础设施也保持绿);`MYSQL_TEST_DSN` 覆写 DSN;每个测试包独占一个 `*_test` 库。
- 前端 `pnpm -r test`(vitest;两个 SPA 目前 `--passWithNoTests`)。

### 本机开发环境(macOS)

- `go` 在 `/usr/local/go/bin/go`;shell 找不到时先 `export PATH=$PATH:/usr/local/go/bin`。
- 宿主机直跑 Go 服务时,基础设施是 compose 映射端口,要 `export MYSQL_DSN='root:infinitechance@tcp(localhost:3307)/infinitechance?parseTime=true' REDIS_ADDR=localhost:6380`;画布服务另需 `CANVAS_SERVICE_KEY=sk-…`(网关不在缺省地址时加 `CANVAS_GATEWAY_URL`),缺它画布生成以 `gateway_unconfigured` 拒绝。
- 并行会话装包后 pnpm 原生依赖可能丢,`pnpm install` 修复;Node 已升 v22,vite 可本地构建。

## 硬性约束(违反即 bug)

1. **密钥只写不读**:厂商密钥明文存库(需重放签名)但管理 API 只回 `has_key` + 尾 4 位;渠道 `config` 列里键名含 `secret`/`key` 的值同款处理,PUT 空值 = 保留原密。API key 仅存 SHA-256 哈希,完整值只在创建响应出现一次。任何新端点不得回显密钥。
2. **计费不变量**:库内金额一律微美元(1e6 = $1)整数、扣费向上取整不低估;时序为「预扣 → 多退少补 → 失败退款」;并发安全靠数据库条件更新(`WHERE quota >= ?`),不做先查后扣;每次变动落 `api_key_quota_log` 流水。
3. **未配价模型一律拒绝**(`model_not_priced`),不做静默兜底倍率;计价轨道不匹配(聊天打非 token 轨等)同码拒绝。
4. **错误形状**:中转面 `/v1` 统一 OpenAI error object(`invalid_api_key` / `insufficient_quota` / `model_unavailable` / `upstream_error` 等 code);管理面统一 `{"error":{"code","message"}}`。
5. **路由鉴权面互不混用**:`/v1` 挂 `apikey.RequireKey`,`/admin` 挂 JWT 会话,`/auth` 公开。canvas 侧 `GET /assets/{id}/content` **故意不挂鉴权**(`<img>/<video>` 带不了 Authorization 头)——不要"修复"它;素材管理面照常挂 JWT。
6. **data: URI 进不了网关媒体契约**:参考图、反推、分析的媒体引用一律拒绝内联,要求 http(s) 地址或素材内容寻址路径(服务端解出真实地址)。
7. **素材对象键三分归档**:`canvases/{canvasID}/{taskID}/{kind}.{ext}` 任务产物、`uploads/{yyyymmdd}/{uuid}.{ext}` 用户上传、`relay/{yyyymmdd}/{uuid}.{ext}` 直连转存——新代码按语义选前缀,不混用。对象先删行后删不留孤儿字节;行落库失败要回收已写对象。
8. **换道/熔断语义照 CONTEXT.md「调度与熔断」**:只有上游临时失败才换道(预扣原封带往下一渠道);客户端 4xx 与主动断开不换道不记熔断账;流一开不换道。不要在调用处自创重试逻辑。
9. **上游产物 URL 是临时的(约 24h)**:任务成功即转存自有存储,`url` 列留厂商原址仅作出处;「LLM 可达地址」解析顺序 = 自有存储公网地址优先、厂商原址回落。
10. **新渠道类型走 adaptor 模式**(在 `internal/relay`,现有 `openai` 与 `tencent-vod` 两类);**新对象存储平台 = 新驱动文件实现 `objectstore.Store` 接口**(原生 SDK × N 路线,已否决通用 S3 兼容层),键布局不变。
11. **双方言 schema**:任何新表/加列要在 MySQL 与 SQLite 两条 `EnsureSchema` 里同时落地;桌面版 SQLite 装配不用 Redis,数据与 Docker 栈互不通用(不做迁移)。
12. **画布前端不持任何密钥**:画布侧调网关一律走 canvas/server 的服务级 key。

## 提交与验证纪律

- 提交信息:Conventional Commits,**自 2026-10-01 起 subject 与正文一律用中文**;类型前缀(`feat`/`fix`/`docs`/`chore` 等)保留英文,正文中文段落 + 按域分点,票号放行尾括号,如 `feat(asset): 腾讯云 COS 存储驱动与生图转存落桶(23 号票)`;docs 类提交不带票号。此前历史提交为英文,不作回改。
- **静默补丁必须回验**:字符串替换后 grep 确认真的生效;tsc/lint 通过 ≠ UI 存在;改完跑 `make test` 与 `make lint`,动前端后要实际构建或打开页面确认。
- 管理台已知缺口:模型价格页(`/admin/prices` 有 API 无 UI,配价走 curl);旧渠道编辑页面会重置能力位——动渠道表单时留意。
