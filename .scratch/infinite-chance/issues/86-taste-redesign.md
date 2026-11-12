# 86 taste-skill 品牌面重设计(登录页分屏 hero + IP 列表 display 时刻 + 画布氛围层)(2026-11-12 定案)

Type: feat
Status: 已实现(2026-11-12,feat/86-taste-redesign 并 main c94ff35,未 push)。实现注记:登录拼贴两组件轮换的负 delay 必须写在 animation 简写之后(简写重置 delay 曾致两组同相空窗);暂停盘原被 aria-hidden 容器吞掉,已下沉 aria-hidden 到装饰件、控件对读屏可达;字标 14 字在 56px 溢出左列被 mask 裁断,收为 clamp(40px,3.3vw,52px)+nowrap;窄屏拼贴 max-height 320px 保表单探出首屏;点阵/连线覆写经 computed style 钉验(dots rgba(139,145,167,.32)/edges rgba(139,145,167,.38)@1.5px)。

## Question

用户要求用 design-taste-frontend skill(tasteskill 2.5)重新设计画布前端三页:登录页、IP 列表页、画布页。现状:登录页是一张 380px 居中卡片、零品牌资产、零入场动效;IP 列表页 masthead 平淡、空态纯文字;画布页是 58 号票「深夜工作室」体系下的成熟工作台,45 个测试文件以源码钉子锁住编辑器模板。

## 定案(2026-11-12,skill dealer 产出 + 裁量)

### 方向纪律

- kind = **software**(dark 页合法),trade = ai。方向先 deal 后落码,历史存 `.taste-skill/`。
- **暗色基因内升级**:11 种节点类型色 + 信号色语义全保(「只在值班时亮」活着);violet(#a78bfa)不进任何新氛围设计(skill 铁律 never violet),仅留音频节点身份色。
- 存活 named rules:浮起即模态、描边即轮廓(三档)、底纹四档、深夜小字(11–15px)、成本不惊扰。

### Deal 产出(逐条)

1. **hero = triptych**(`deal.mjs --type software --has ui`):headline 左 / 作品拼贴中(3–5 件一组,2.8s 轮换,0.7s 交叉淡化,ui.loop + 暂停盘)/ 大按钮右线。拼贴件 = 自家 UI 缩微(类型色节点卡、图片/视频帧、提示词条),「show the real thing」。
2. **palette:三支全数相撞,走 brand 模式**——swap-pastels / white-mint-tiles 为浅底(与全站深夜单主题硬约束相撞),lunar-grey-bronze 为中性近黑(撞「无纯黑、深底在蓝调色域内」禁令)且铜色 accent 与 amber(视频/运行态)、gold(寂静金/剧本)值班语义冲突。按 skill 正路 `start.mjs brand` 以客户自有色落定:bg #0d1524 / surface #10162a / ink #e8eaf2 / accent #7aa2f7(工作灯蓝保位)。
3. **type = instrument**(首支,Instrument Sans + DM Mono;「design and productivity software, studios」正中);display 500 / tracking -2.8%(xl -3.6%)/ stretch 96%。**自托管** woff2 入 `canvas/web/public/fonts/`(Wails 离线),仅品牌时刻(Latin 字标、display 大字);中文界面维持系统栈、深夜小字不动。→ DESIGN.md「Don't 引入 webfont」修订为「品牌时刻 Latin 展示字」。
4. **ground = bg-halo(hero)+ bg-band(ips)**(`pick.mjs --mode dark`):halo = 三层 accent 色相径向、上下渐隐遮罩;band = 亮调双角光 + 细噪。配方按项目 token 语移植(蓝调域,每页 ≤2 支)。
5. **moment = liquid-type dealt 后弃用**:指针涟漪无值班语义,登录页动效预算已由 triptych 拼贴轮换占满;skill 的 moment 规则面向 marketing page,app 面以克制为先。
6. **radius = soft**(映射现有 8px 骨档位,不变)。

### 三页改法

- **登录页**:AuthLayout 重写为 triptych 分屏(左 headline 列 / 中拼贴 / 右表单列),入场动效 word-mask 逐行升 + 后续 12px fade-up(~1.1s,--ease-enter,once,reduced-motion 静止);LoginView/InitView 脚本逻辑零改动(InitView 自动继承)。
- **IP 列表页**:masthead 升 display 时刻(Latin 字标 + 大字,企划渐变仍只作身份点缀);bg-band 一支;卡片 hover 层对齐 skill 数字(lift 2px + 描边升档 + 封面 zoom 1.02–1.03,150/120/600/400ms);空态给画面(CSS hairline 迷你画布 diagram);IpDetailView 不在范围(token 级联受益)。
- **画布页**:氛围层 only——style.css token 值 + vue-flow CSS 覆写(背景点色/edge 描边)+ composer/顶栏/菜单 scoped 精修;**模板/class/文案零改动**(45 个测试文件零风险),git diff 验收。

## 验证点

- make test / lint / build 全绿;grep 回验无旧色值硬编码残留;CanvasEditorView + nodes/* 模板段 diff 为零。
- 真栈点验:登录(triptych 轮换、暂停盘、reduced-motion)→ IP 列表(display 时刻、hover 层)→ 进画布(氛围层无回归)。
- 1440 主验 + 登录页 1024/390 截图;shoot.mjs 对照 checklist。

## Out of Scope

- admin-web;IpDetailView 结构;画布模板与交互行为;多主题亮色;移动端适配;npm 新依赖与动画库;编排级画布动效(58 号票已另票悬置)。
