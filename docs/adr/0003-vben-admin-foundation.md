# 管理后台采用 vue-vben-admin 裁剪版底座

admin-web 最初是自绘的零第三方 UI 依赖 SPA(顶部导航 + 手写 CSS),单管理员管理台
页面增多后,布局、主题、暗色模式这类骨架能力都在重复自造。34–36 号票起改为抽取
vue-vben-admin(v5)的 web-antd app 及其 `@vben/*` / `@vben-core/*` 依赖闭包,打平
进本仓 workspace,以此为底座重建管理台:vben 布局/标签页/主题接管壳层,登录与首
管理员初始化接现有 /auth JWT 流,菜单 frontend 模式写死(单管理员无角色),i18n
裁到中文单语。配套把 pnpm workspace 从 8 升到 11(vben engines 要求,其包依赖用
`catalog:` 协议)。抽取后的 vben 代码不跟上游,当自己的代码维护。

## Considered Options

- **整仓 vendor vben monorepo**:跟上游升级方便,但仓库出现嵌套 workspace、双
  lockfile、turbo,make 链路复杂化,放弃。
- **只引 Ant Design Vue 不引 vben 框架**:依赖最轻,但侧边栏/标签页/主题骨架要
  自写一遍,与「要现成骨架」的动机相悖,放弃。
- **保持自绘**:零升级成本,但骨架能力持续自造,放弃。

## Consequences

- pnpm 11 成为 workspace 硬约束(engines + lockfile 格式),corepack 管控版本。
- 不跟上游:vben 自身 bug 要自己修,升级靠手动对齐;`admin-web/vben/` 下的几十个
  内部包是刻意的裁剪产物,新读者不要当作冗余依赖「清理」。
- desktop `go:embed` 的管理台产物体积显著变大,二进制变大是已知代价。
- 权限体系停留在 frontend 菜单模式:这是单管理员形态的刻意简化,不要往 RBAC 方向
  「补全」(35 号票定案)。
