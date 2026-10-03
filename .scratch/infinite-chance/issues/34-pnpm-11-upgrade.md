# 34 pnpm workspace 升级 pnpm 11

Type: grilling
Status: 已实现(2026-10-03)

## Question

管理后台拟迁 vue-vben-admin(35/36 号票)。vben v5 main 的 engines 要求 node `^22.18.0 || ^24.12.0`、pnpm >= 11,其内部包依赖声明用 pnpm `catalog:` 协议(需 pnpm >= 9.5);本仓现状 pnpm 8.6.12(本机)/ `pnpm@8.15.9`(Dockerfile),根 package.json 无 `packageManager` 字段。升级怎么切票、怎么控风险?

## Answer

1. **升级先行单独成票**:pnpm 8 → 11 会重生成整个 lockfile、四个包(admin-web、canvas/web、packages/api、packages/ui)依赖全量重解析,是独立风险面,不与 vben 迁移混票;本票全绿后 35 号票才动工。
2. **一次性全 workspace 升级**:根 package.json 补 `packageManager` 字段(corepack 管控,本机 corepack 0.34.6 就绪);Dockerfile 的 `npm install -g pnpm@8.15.9` 同步换 pnpm 11;`node:22-alpine` 保留(22.x 满足 `^22.18`)。
3. **lockfile 全量重生成**:删 pnpm-lock.yaml 重装;除 pnpm 语义必需的解析变化外,**不主动升级任何依赖版本**。
4. **本票不含 vben**:`catalog:` 段、@vben 包收编都留给 35 号票。
5. **回验面**:`make test` + `make lint` + `pnpm -r build`(两 SPA 内含 vue-tsc)+ `make desktop`(embed SPA 照常进二进制)+ compose 栈拉起,确认 admin-web(:8090)/ canvas-web(:8091)容器构建可用。
6. **显式 out of scope**:vben 本身、依赖版本主动升级、桌面 SQLite 装配(不受影响)。

## 实现记录(2026-10-03)

- 定版 **pnpm 11.28.2**(`latest-11`;11.28.3 在 next 通道):根 package.json `packageManager`、Dockerfile `npm install -g pnpm@11.28.2`、本机 npm 全局同步。pnpm 11 要求 Node 22+,`node:22-alpine` 不动。
- **票外必要配套**:pnpm 11 `strictDepBuilds` 默认开启,`pnpm install` 拦下 esbuild/vue-demi 的 postinstall(报 `ERR_PNPM_IGNORED_BUILDS`),在 `pnpm-workspace.yaml` 增 `allowBuilds` 显式放行(旧的 `onlyBuiltDependencies` 已废除)。35 号票引 vben 依赖时预计要再放行新包。
- **lockfile v6→v9**:pnpm 11 拒绝直迁 v6 旧锁(报 lockfileVersion 不兼容),删锁重装为票面既定路径;manifests 零改动,在界重解析漂移(直接依赖:vite 7.3.6 未动;typescript 5.8.2→5.9.3、vue 3.5.42→3.5.43、eslint 9.30→9.39.4、eslint-plugin-vue 10.0→10.11.1、typescript-eslint 8.35→8.71、vue-tsc 3.0→3.3.11、vitest 3.2.0→3.2.7 等)由全链回验覆盖。
- **回验全绿**:`pnpm -r build`(两 SPA 含 vue-tsc)、`make lint`、`make test`(canvas-web 96 用例)、`make desktop`(二进制 37MB)、compose 构建 admin-web/canvas-web 镜像(容器内实证 pnpm 11.28.2 + lockfile v9.0)、`make up` 后 :8090/:8091 均 200。
