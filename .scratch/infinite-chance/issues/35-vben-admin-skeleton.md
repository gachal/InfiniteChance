# 35 管理后台底座迁移 vue-vben-admin(web-antd 抽取打平)

Type: grilling
Status: 已实现(2026-10-03;抽取打平 + 鉴权接 vben + 部署三形态回验)

## Question

admin-web 现为自绘零 UI 依赖的 Vue 3.5 + Router 5 + Vite 7 SPA(9 视图 ≈3.5k 行,AdminShell 顶部导航,auth.ts 模块单例管 JWT)。要换成 vue-vben-admin 的现成后台骨架(侧边栏/标签页/暗色模式),框架以什么形态进仓、鉴权与部署三形态怎么接,且八个业务页功能不回退?

## Answer

1. **动机锚定**:要的是现成企业级骨架(布局/标签页/主题/暗色),antd 组件按需使用;验收基准 = 功能不回退,不追求 vben 权限/i18n/mock 全家桶。
2. **形态 = 抽取 web-antd 打平**:`apps/web-antd` + 其 pnpm 依赖闭包内的 `@vben/*` 与 `@vben-core/*` 内部包(含 `@vben/vite-config` 构建链)收编进本仓;playground、docs、backend-mock、e2e、turbo 全裁。落点:app 本体仍在 `admin-web/` 根,vben 内部包落 `admin-web/vben/`(根 pnpm-workspace.yaml glob 增补 `admin-web/vben/*`),catalog 版本照搬进我们的 pnpm-workspace.yaml。**不跟上游**,当自己的代码维护,全仓单一 lockfile。
3. **鉴权全面接 vben**:vben 登录页改接 `POST /auth/login`,token 走 vben access store;`/auth/me` 校验、401 自动回登录;守卫用 vben access 机制,auth.ts 单例退役。首管理员 `/auth/init` 两步向导改造成 vben 风格独立页(`/auth/status` 引导逻辑保留)。菜单用 vben frontend 模式前端写死两组(网关管理/画布管理,对应现路由),单管理员不做角色;i18n 裁到 zh-CN 单语。**旧 localStorage token(`infinitechance.admin.token`)不迁移,直接作废重登**(单管理员,重登成本最低)。
4. **过渡态已接受**:八个业务视图组件原样保留(自绘 CSS),挂进 vben 布局路由;风格混搭为刻意中间态,36 号票收尾。`@infinitechance/api` 与 ApiClient 双 base(`/api`、`/canvas-api`)原样不动;后端零改动(/admin、/auth API 照旧;素材 content 端点不挂鉴权是故意的,不碰)。
5. **部署三形态全回验**:vite dev 代理(:5173,`/api`→8080、`/canvas-api`→8081 同前缀去前缀约定)、nginx :8090(SPA history 回退照旧)、desktop `go:embed` + Wails 管理台窗口。vben/antd 产物体积增大接受,desktop 二进制变大是已知代价。
6. **显式 out of scope**:页面 antd 重写与价格页(36 号票)、canvas-web、后端。
7. **验证点**:`make test` / `make lint` / `make build` + `make desktop`;dev 实际点验——首初始化向导、登录、退出、401 回登录、暗色/主题切换、八页功能逐页过一遍、desktop 配置窗打开管理台正常。

## 实现注记(2026-10-03)

- 收编闭包实测 30 个内部包(@vben 14 + @vben-core 12 + node-utils/tailwind-config/tsconfig/vite-config):`tailwind-reference` 插件向用 @apply 的 SFC 注入 `@reference "@vben/tailwind-config/theme"`,该包必须随闭包收编,闭包扫描只看 package.json 会漏它;`@vben/plugins` 里的 vxe-table 因无 import 被摇树,包本体仍被 `plugins/motion` 引用,须保留。
- 「八个业务视图」实为 7 个(9 视图 − Login/Init 两个认证页),本票口径即 7 个 *View.vue。
- `@vben/vite-config` 与 `@vben/node-utils` 被 vite 配置加载链以 node 方式解析,需 dist;根 postinstall 跑各包 `stub` 生成,产物不入库。
- 旧 localStorage token(`infinitechance.admin.token`)未迁移,vben access store 以 `VITE_APP_NAMESPACE` 前缀另存,重登即换轨。
- Dockerfile web-build 需在 `pnpm install` 前整目录拷入 `admin-web/vben/`(30 个 workspace 成员缺一即 frozen-lockfile 失败)。
- dev 代理新增 `ADMIN_DEV_GATEWAY`/`ADMIN_DEV_CANVAS` 环境覆写(默认 :8080/:8081 不变),隔离库冒烟用。
