# admin-web/vben — vue-vben-admin 裁剪底座(不跟上游)

vue-vben-admin v5.8.0(MIT)的 `apps/web-antd` 依赖闭包,按 35 号票定案抽取打平:
`@vben/*`、`@vben-core/*` 内部包与 `@vben/vite-config` 构建链收编进本仓,playground、
docs、backend-mock、e2e、turbo 全部裁掉。**抽取后不跟上游,当自己的代码维护**;
升级靠手动对齐。这里几十个内部包是刻意的裁剪产物,新读者不要当冗余依赖「清理」
(ADR 0003)。

## 目录对照(上游 → 本仓)

| 本仓 | 上游 | 包名 |
| --- | --- | --- |
| `access`…`utils` | `packages/effects/*`、`packages/*` | `@vben/*` |
| `core-*` | `packages/@core/**` | `@vben-core/*` |
| `node-utils`、`tailwind-config`、`tsconfig`、`vite-config` | `internal/*` | `@vben/node-utils` 等 |

上游通过 package.json `exports` 的 `development`/`production` 条件让应用直接消费
TS 源码(无需逐包构建);只有 `node-utils` 与 `vite-config` 会被 vite 配置加载链以
node 方式解析,需要 `dist`(根 package.json 的 `postinstall` 跑各包 `stub` 脚本生成,
产物不入库)。

## 与上游的差异

- 各 package.json 裁掉 `build`/`lint`/`test`/`typecheck` 脚本与仓库元数据,避免混进
  根 `pnpm -r` 链路;`tsdown`/`typescript` 从上游根 devDeps 下放到 node-utils /
  vite-config 自己的 devDeps。
- catalog 版本表照搬进根 `pnpm-workspace.yaml`;版本统一由 catalog 管控。
- i18n 保持包内 en-US/zh-CN/zh-TW 语言包不动(类型联合与回退依赖它们),应用侧只
  暴露 zh-CN:偏好里 `locale: 'zh-CN'`、关掉语言切换按钮(单语形态)。
