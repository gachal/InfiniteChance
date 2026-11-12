# 87 IP 详情页 display 时刻升格(taste-skill 续章:band 海报位 + 画布网格)(2026-11-12 定案)

Type: feat
Status: 定案

## Question

86 号票用 design-taste-frontend skill(tasteskill 2.5)重设计了登录页、IP 列表页与画布氛围层,但**明确把 IpDetailView 排除在范围外**(只吃 token 级联)。DESIGN.md「IP 企划面」明文「IP 卡片墙与详情页是全站唯一允许海报感的地方」——详情页升 display 时刻是 86 号票留白的有案下一站。现状 ip-head 仅 168px 小封面 + wash 面板,无 band/kicker/brand-bar;画布区是单列文本行;资产卡墙 hover 数字已对齐但呼吸节奏未对齐(260ms vs skill 图像节奏 600/400)。

用户裁决(2026-11-12):①力度 = 视觉升格 **+ 画布区双列网格化**(超出 86 号票对列表页的「结构不动」口径,但仍不改交互行为);②封面形态 = **框内大海报**(非全幅 band 背景)。

## 定案(2026-11-12)

### IP 头升 band display 时刻

- `section.ip-head` 升 band 面板(`overflow:hidden`):IpListView masthead 同款配方——`var(--noise-tile)` 细噪 + 三层角光(accent 左上 0.09 / 雾靛右上 0.07 / 企划紫右下 `color-mix(in oklab, var(--ip-violet) 14%, transparent)`)+ `--line-section` hairline。**每页至多一支 band,详情页的 band 就是 ip-head**(masthead 降为素工具行)。
- kicker:「InfiniteChance」,`var(--font-display)` 500 / 0.3em 字距 / uppercase / `--text-muted`(照抄 IpListView)。
- h1 升 `clamp(28px, 3vw, 36px)` 700(IP 身份时刻例外档);下加 64px×3px 企划渐变 brand-bar。
- 封面 168px → **约 320px 16:9 框内大海报**,hover 呼吸对齐 skill 图像节奏:静态 `transition: transform var(--dur-image-out) var(--ease)`,hover `scale(1.03)` + `transition-duration: var(--dur-image)`;渐变首字 fallback 保持。
- meta 时间戳「更新于 …」用 `var(--font-mono)` 点缀(DM Mono「mono 数字时刻」授权范围)。
- 「+ 新建画布」从 masthead 移入 band 底部升 primary(实心主按钮微动效:仅 `(hover:hover) and (pointer:fine)` 下 hover `translateY(-1px)` 150ms / active `scale(0.985)` 90ms);编辑 IP/删除 IP ghost 簇留 band 右上,两步确认逻辑不变。
- band 一次性入场:fade-up 8px + opacity ~520ms `var(--ease-enter)`,`prefers-reduced-motion: reduce` 下静止(skill rule 5「hero enters once on load」;单元素一次性入场,非编排级)。

### 画布区双列网格化(行为不变)

- `ul.list` 单列 → `repeat(2, minmax(0,1fr))` 双列网格;行卡化:名称 600 title 档 + 更新时间 DM Mono micro + 操作簇(重命名/移动/删除 ghost 小按钮常驻,排进卡片底部)。
- 卡 hover:lift 2px + 描边升 `--line-action`,**无阴影**(浮起即模态);重命名表单态在卡内展开,handler 零改动。
- 空态换画面:hairline 迷你画布 SVG(照抄 IpListView 空态,内联复制,`aria-hidden`,零动画)。
- ADR 0005 平铺序不变(仍 updated_at 新到旧,网格只是视觉)。

### 资产库区与其余

- section-head 补 `<h2>资产库</h2>`(标题层级 h1→h2 完整);tabs/计数/新建按钮保持。
- 资产卡 zoom 呼吸从 `--dur-slow` 260ms 改 `--dur-image-out` 400ms 静态 / `--dur-image` 600ms hover(与封面同节奏);lift 2px 已有保持。
- 素材区 h2 硬编码 16px → `var(--fs-title)`;壳 max-width 1080 → 1200(与列表页家族一致);对话框表单结构不动,按钮统一吃微动效。

### 方向纪律(承 86 号票)

企划渐变只进封面 fallback/brand-bar/首字徽标;交互色仍只工作灯蓝;hover 无影;描边三档/底纹四档;Instrument Sans 仅 kicker、DM Mono 仅时间戳 meta;本页无常动件(零 >5s 循环,不需暂停盘);新样式全走既有 token,零硬编码色值、零 npm 依赖;交互行为、API 调用、路由名(`ip-detail`)、子组件(AssetDrawer/AssetPanel/AssetPicker)零改动。

## 验证点

- make test / lint / build 全绿;grep 回验:`linear-gradient.*ip-indigo` 仅封面 fallback×2 + brand-bar;无新增硬编码 hex;`--font-display` 仅 kicker、`--font-mono` 仅 meta;路由名与 handler diff 为零。
- dev-canvas(:5174)实看:1440×900 主验 + 1920 基准(canvas 前端桌面唯一形态,不做移动端);band 入场/封面呼吸/画布网格 hover/重命名/移动/删除/三对话框/空态/reduced-motion。

## Out of Scope

- AssetDrawer/AssetPanel/AssetPicker 内部;对话框表单结构;交互逻辑与 handler;画布编辑器页面。
- Lore 实体区挂点(ip-capabilities 04 号票未定案,资产 tab 体系天然可扩,届时另票)。
- 编排级动效(58 号票已另票悬置);admin-web;移动端;npm 新依赖。
