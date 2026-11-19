# 89 画布单色化:节点统一深色卡面 + 共享骨架收敛 + composer 精修(2026-11-19 定案)

Type: feat
Status: 已实现(2026-11-19,未提交)。实现注记:媒体族共享件命名空间用 `.node.media`(Character/Analysis 各有语义不同的同名 `.placeholder`,宽命名空间会误伤);AudioNode 根补 `media` 类入族;运行 pill 取 AudioNode 的胶囊底为唯一形态(原三节点只有颜色无形状);Character/Voice 的驳回徽章从警报红改归失败绯(两支红混淆清掉);Audio/Panorama/Voice 付费主按钮升 accent 实心(花钱的动作亮灯);AnalysisNode 用 data-state 非 data-task,工作态规则留 scoped、色归 accent;AudioNode 源码钉子测试 `accent-color: var(--violet)` 改钉 `var(--accent)`(712 测试全绿);登录拼贴仅中性化 chrome 描边,缩略渐变是内容指代保留;make test / lint / build 全过。遗留浏览器点验(13 种节点五态 + composer 分组 + 登录页)。

## Question

用户要求用 design-taste-frontend skill 重新设计画布节点样式与提示词填写(composer)样式。摸底结论:同一张节点卡面骨架(玻璃底/描边/圆角/内边距/字号)在 12 个 SFC 里手写了 12 份无共享层,且已漂移——PanoramaNode 硬编码非 token 色 `#67e8f9`(三处)、AudioNode 绑了 `data-task` 却无对应 CSS(运行/失败无描边反馈)、运行态 pill 三种写法、选中态规则散在 6 个文件各自为政;composer 七段堆叠层级平均,发送键缺 skill 数字微动效。

## 定案

- **单色画布(用户裁决,取代早前「B 路线降噪」)**:节点不出现不同颜色,统一一种深色卡面。11 种类型色描边从节点卡整体退役,标题不再读类型色、不加灯牌点。画布 chrome 归一为「一种深色卡面 + 白系三档描边 + 工作灯蓝状态」。颜色只留语义:accent = 可交互/运行中/选中,danger = 失败——「只在值班时亮」规则的彻底执行。DESIGN.md 签名协议「描边色=这张卡是谁」随之修订。
- **状态词汇表**:静止 `--line-control`(0.14)/ hover `--line-action`(0.28)(不发明新档,沿用既有三档)/ 选中 accent 0.9 + 2px halo 0.3 / 运行 accent 0.85 + pill 呼吸 accent / 排队虚线 / 失败 danger 0.6 + 失败绯文案。连线三态(accent/success/dimmed)是语义信号,保留。
- **共享骨架**:泛化收编 12 份 `.node` 拷贝进 style.css 全局段(`.vue-flow__node .node` 扩展),组件只留 width 与真正独有的内部件;媒体族共享件(placeholder/missing/failed/error/retry/working pill)以 `.node` 前缀限命名空间收进全局。无需 `--tone-rgb`、无需新 token(`#67e8f9` 直接中性化)。
- **端口微动效**:`.port:hover` 叠 `scale(1.06)`(skill 按钮 step 语义)。
- **composer 精修**:estimate + controls 包进 `.composer-foot` 顶部 hairline 分组(参考条/输入区/参数栏三段秩序);发送键 hover translateY(-1px) step + active scale(0.985) press,包 `@media (hover:hover) and (pointer:fine)`;`useComposerPlacement.ts` estH 360/260 → 372/272。
- **登录拼贴同步**:AuthLayout `.mini-node` 单色化(它展示的永远是画布自己的 UI 语汇,86 号票原则)。

## 裁量与注记

- **GroupNode 组框保留多色**:组框颜色是区分多个组的功能色(用户可选),不是节点身份色,本票不动;若也要中性化另票小改。
- composer 内 teal 小标(ref-element-type/mention-type/sheet-types 选中)是元素类型语义,不在节点卡内,保留。
- AudioNode 的 data-task 反馈由共享骨架免费获得(修漂移);ImageNode 分析按钮 indigo 残留一并中性化。
- 范围外:IpDetailView 网格卡与空态迷你画布插画(87 号票面)、AgentNode 内部 chat/timeline、编排级连线动画(58 号票已另票悬置)。

## 验证点

- make test / lint / build 全绿(样式层改动,测试不断言类名)。
- grep 回验:nodes/ 目录 `#67e8f9` 清零;类型色 0.4/0.45 描边声明清零。
- 浏览器点验(make dev-canvas + 自签 JWT 法):13 种节点静止/hover/选中/运行/失败五态、连线三态、composer 三段分组与发送键微动效、登录页拼贴。
