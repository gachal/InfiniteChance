---
name: 无限画布 (InfiniteChance Canvas)
description: 深夜工作室——单用户 ACG IP 打造平台的创作画布视觉体系
colors:
  bg-deep: "#090c14"
  bg-navy: "#0d1524"
  bg-glow: "#14284b"
  surface-card: "rgba(20, 26, 43, 0.92)"
  surface-panel: "rgba(13, 21, 36, 0.92)"
  text-primary: "#e8eaf2"
  text-bright: "#cdd7ee"
  text-secondary: "#aab1c5"
  text-muted: "#8b91a7"
  text-faint: "#6f7690"
  accent: "#7aa2f7"
  accent-ink: "#10152a"
  accent-soft: "#9fb4f9"
  estimate-ink: "#9fb3d9"
  success: "#4ade80"
  danger: "#ff8f8f"
  amber: "#facc15"
  sky: "#38bdf8"
  indigo-soft: "#a5b4fc"
  violet: "#a78bfa"
  pink: "#f472b6"
  teal: "#2dd4bf"
  teal-light: "#5eead4"
  orange: "#fb923c"
  red-signal: "#f87171"
  ip-indigo: "#243b8f"
  ip-violet: "#57348a"
  ip-wine: "#8a2f5c"
typography:
  display:
    fontFamily: "'PingFang SC', 'Hiragino Sans GB', 'Microsoft YaHei', system-ui, -apple-system, sans-serif"
    fontSize: "clamp(28px, 4vw, 42px)"
    fontWeight: 700
    lineHeight: 1.2
    letterSpacing: "normal"
  title:
    fontFamily: "'PingFang SC', 'Hiragino Sans GB', 'Microsoft YaHei', system-ui, -apple-system, sans-serif"
    fontSize: "15px"
    fontWeight: 600
    lineHeight: 1.4
    letterSpacing: "normal"
  body:
    fontFamily: "'PingFang SC', 'Hiragino Sans GB', 'Microsoft YaHei', system-ui, -apple-system, sans-serif"
    fontSize: "13px"
    fontWeight: 400
    lineHeight: 1.5
    letterSpacing: "normal"
  label:
    fontFamily: "'PingFang SC', 'Hiragino Sans GB', 'Microsoft YaHei', system-ui, -apple-system, sans-serif"
    fontSize: "12px"
    fontWeight: 600
    lineHeight: 1.4
    letterSpacing: "normal"
  micro:
    fontFamily: "'PingFang SC', 'Hiragino Sans GB', 'Microsoft YaHei', system-ui, -apple-system, sans-serif"
    fontSize: "11px"
    fontWeight: 400
    lineHeight: 1.4
    letterSpacing: "normal"
rounded:
  sm: "6px"
  md: "8px"
  lg: "10px"
  xl: "12px"
  pill: "999px"
spacing:
  xs: "4px"
  sm: "8px"
  md: "12px"
  lg: "16px"
  xl: "24px"
components:
  button-primary:
    backgroundColor: "{colors.accent}"
    textColor: "{colors.accent-ink}"
    rounded: "{rounded.pill}"
    size: "36px"
  button-ghost:
    backgroundColor: "rgba(255, 255, 255, 0.08)"
    textColor: "{colors.text-secondary}"
    rounded: "{rounded.md}"
  node-card:
    backgroundColor: "{colors.surface-card}"
    textColor: "{colors.text-primary}"
    rounded: "{rounded.xl}"
    padding: "10px 12px"
  port-dot:
    backgroundColor: "{colors.surface-panel}"
    textColor: "{colors.text-secondary}"
    rounded: "{rounded.pill}"
    size: "24px"
---

# Design System: 无限画布 (InfiniteChance Canvas)

## Overview

**Creative North Star: "深夜工作室"**

这是一个只在深夜亮灯的单人工作室:整间屋子沉在蓝黑里,光只落在正在工作的东西上——选中的节点、打开的对话框、进行中的任务。界面底色是一整片深夜蓝黑(`#14284b → #0d1524 → #090c14` 的径向渐变,像窗外最后一点天光),其上的一切面板都是半透明的深色玻璃,靠 1px 细描边勾出轮廓而不是靠阴影托起。色彩在这里从不装饰:每一种颜色都在值班——11 种节点类型各占一种身份色,绿是成功、红是失败、蓝是可交互;没有语义的颜色不出现。

气质是安静克制的:11–13px 的高密度小字、600 字重的标签、紧凑的 8px 圆角,信息密度明显优先于留白——这是工具,不是海报。但克制里有两处刻意的高光:品牌渐变(深蓝→紫→酒红,135°)只出现在 IP 企划的身份时刻(卡片墙封面、空态徽标),像工作室里唯一一件永久陈列;类型色描边让 11 种节点在画布上一眼可辨,像不同工组的灯牌。

**Key Characteristics:**

- 深夜蓝黑径向渐变打底,`color-scheme: dark`,全程无纯黑纯白
- 半透明深色面板(`rgba(20,26,43,0.92)`)+ 白系透明 hairline 描边,平面靠描边不靠阴影
- 11 种节点类型色板(描边 0.4–0.45 透明度),色彩即身份
- 高密度小字(11–13px)、600 字重层级、系统中文字体栈零加载
- 阴影只给真正浮起的层(灯箱/侧滑/编辑层),浮起即模态
- 8px 圆角为骨、999px 胶囊为饰,圆而不泡

## Colors

调色板是一片深夜色域:近黑蓝的底、蓝灰阶的文字、一支交互蓝,外加一套各司其职的信号色与类型色。所有色值都为深底调过,没有从浅色主题移植的痕迹。

### Primary

- **工作灯蓝 Accent** (#7aa2f7):唯一的全局交互色——焦点环、发送按钮、连线起点高亮、链接与主操作。它是深夜工作室的工作灯:亮,但只照该照的地方。

### Secondary

- **企划渐变** (#243b8f → #57348a → #8a2f5c,135° 线性):IP 企划的身份色,只用于 IP 封面、IP 卡片墙空态徽标、IP 详情页头图。这是品牌色,不是功能色,不许挪作按钮或状态。

### Tertiary(节点类型色板 / 信号色)

每种画布节点以一种色作描边身份(0.4–0.45 透明度落在深底上),同一色可能兼任语义:

- **信号绿** (#4ade80):图片节点身份 + 任务成功 + 连线合法目标
- **琥珀黄** (#facc15):视频节点身份 + 运行态警示
- **晴空蓝** (#38bdf8):分镜稿节点身份
- **雾靛** (#a5b4fc):分析节点身份
- **罗兰紫** (#a78bfa):音频节点身份
- **夜樱粉** (#f472b6):预演台节点身份
- **设定青** (#2dd4bf):角色引用卡身份
- **声波青** (#5eead4):音色节点身份
- **暮橙** (#fb923c):分镜节点身份
- **警报红** (#f87171):关卡卡身份(注意:与失败色 #ff8f8f 是两支红,不可互换)

### Neutral

- **深夜三层底**:bg-glow #14284b(径向高光)→ bg-navy #0d1524(主底)→ bg-deep #090c14(渐变末端),只作页面底,不作面板
- **卡面玻璃** surface-card rgba(20, 26, 43, 0.92):节点卡面
- **面板玻璃** surface-panel rgba(13, 21, 36, 0.92):控件、小地图、端口圆点
- **正文字** #e8eaf2 / 亮字 #cdd7ee:主内容文字
- **次级字** #aab1c5 / 弱字 #8b91a7 / 极弱字 #6f7690:标签、说明、禁用态文字
- **失败绯** #ff8f8f:错误文案、失败状态、「素材不可用」占位(信号红 #f87171 是关卡卡身份色,别混)
- **预估灰蓝** #9fb3d9:价格预估文案——成本是信息,不是警报

### Named Rules

**The 只在值班时亮规则。** 颜色只承载语义:类型身份、状态、交互。不允许无语义的装饰色;想加一点颜色时,先回答它替用户省了哪次辨认。

**The 成本不惊扰规则。** 价格预估永远用预估灰蓝(#9fb3d9)小字,失败绯(#ff8f8f)只留给失败。钱要可见,但不必看起来像出错。

## Typography

**Display Font:** 系统中文栈——'PingFang SC' / 'Hiragino Sans GB' / 'Microsoft YaHei' / system-ui
**Body Font:** 同上(全站单一字体栈,无 webfont,零加载成本,桌面版与浏览器渲染一致)
**Label/Mono Font:** 无独立等宽;数字与代码沿用同栈

**Character:** 务实的工具字体姿态——不追字体的表现力,靠字号与字重做层级。中文界面在 11–13px 的小字号下依然清晰,是整个高密度布局成立的前提。

### Hierarchy

- **Display**(700,clamp(28px, 4vw, 42px),1.2):只出现在 IP 企划的身份时刻——卡片墙标题、空态徽标、详情页头。画布里没有 display 字。
- **Title**(600,15px,1.4):面板标题、节点卡标题、对话框标题。
- **Body**(400,13px,1.5):节点卡正文、对话框表单、说明文案。节点卡基准字号即 13px。
- **Label**(600,12px,1.4):全站最常用的层级——字段标签、按钮、chip、表头。121 处 12px + 64 处 600 字重的用量重心在这里。
- **Micro**(400,11px,1.4):占位提示、辅助注脚、徽章内的次级信息。

### Named Rules

**The 600 就够规则。** 层级靠 600 对 400 的对比建立,700 只留给 Display 与主按钮字符。不引入更多字重档位。

**The 深夜小字规则。** 界面文字 11–15px,15px 是上限(Title 档);只有 IP 身份时刻允许更大。这不是缩小的理由,是密度的承诺。

## Layout

画布即桌面:全视口工作区,深夜渐变底直接可见,节点浮在其上。没有页面网格——空间组织单位是节点与视口。

- **节点宽度按类型定宽**:视频 220px、Agent/分析/角色/图片 260–280px、音频 300px、预演台/分镜/分镜稿/关卡/音色 340px;高度随内容。新节点落当前视口中心(级联偏移)。
- **密度**:节点内边距 10px 12px,卡片间距靠画布自由摆放;侧滑面板与对话框内的表单行距用 spacing md(12px)。
- **浮层系**:生成对话框贴选中节点;素材/生成记录/Agent 历史为右侧滑面板;灯箱与三个编辑层(预演台/分镜/分镜稿)为全屏 Teleport 浮层。浮层永远是全高、贴边、带大阴影的。
- **响应式**:桌面浏览器是唯一目标形态(移动端明确不支持),无断点系统;窗口缩放由画布缩放(滚轮/pinch)承担,不做布局重排。

## Elevation & Depth

**浮起即模态**:静止的界面是平的。节点卡、工具栏、面板内容在静止时只靠 1px 白系透明描边和半透明玻璃底区分层级,不加阴影。阴影是"这一层是模态的"的信号,只出现在三种真正浮起的东西上:全屏灯箱/编辑层、侧滑面板、悬浮控件(缩放控件、小地图)。深度感的日常载体是描边亮度与玻璃透明度,不是投影。

### Shadow Vocabulary

- **控件浮影** (`0 8px 24px rgba(0, 0, 0, 0.35)`):vue-flow 缩放控件与小地图——常驻但不属于画布平面的悬浮件
- **侧滑影** (`-12px 0 32px rgba(0, 0, 0, 0.45)`):右侧滑面板,光源感来自左侧画布
- **对话框影** (`0 8px 24px rgba(0, 0, 0, 0.4)` / `0 8px 28px rgba(0, 0, 0, 0.5)`):生成对话框与中浮层
- **全屏浮层影** (`0 12px 40px rgba(0, 0, 0, 0.45)` / `0 12px 48px rgba(0, 0, 0, 0.55)`):灯箱与编辑层——最重的层级用最重的影

### Named Rules

**The 浮起即模态规则。** 阴影是模态性的信号:一个元素带阴影,它就在打断用户的工作流(灯箱/编辑层/对话框/侧滑)。静止内容永不带阴影;想给卡片加影时,先问它凭什么打断用户。

**The 描边即轮廓规则。** 平面元素的轮廓一律用白系透明描边勾出,不用灰色实线、不用阴影兜底。描边透明度即层级:0.08 是分区,0.12–0.16 是控件边界,0.28 是可点击件。

## Shapes

圆角语言是"圆而不泡":默认 8px(控件、按钮、输入框,全站用量最大),容器级 12px(节点卡、灯箱图框、大面板),小型浮层 10px(缩放控件、小地图),输入类小件 6px。胶囊 999px 专属于三样东西:连线端口圆点(24px)、chip、圆形主按钮——胶囊是"端口/标签/击发"的形状语义,不用于普通容器。描边一律 1px,没有 2px 边框;分割线是 rgba(255,255,255,0.08) 的 border-bottom。没有斜切、噪点、纹理;形式语言全靠圆角档位 + 描边透明度两套刻度表达。

## Components

### Buttons

- **Shape:** 主按钮为 36px 圆形(生成对话框发送键);普通按钮 8px 圆角
- **Primary:** 工作灯蓝底 #7aa2f7 + 深夜墨字 #10152a(注意:蓝底上压的是深墨不是白),字重 700
- **Hover / Focus:** hover 提亮描边与文字(描边转 rgba(122,162,247,0.85) 一族);焦点统一走全局 `:focus-visible`(2px #7aa2f7 outline,offset 2px,只在键盘导航出现)
- **Ghost / 次级:** 白系透明底 rgba(255,255,255,0.08) + 次级字色 #aab1c5,hover 描边升 0.28 档;危险操作用失败绯文字而非红底
- **Disabled:** opacity 0.4 + cursor: not-allowed,不改色

### Chips

- **Style:** 胶囊 999px,白系透明底(0.08–0.12),12px 字,可带类型色文字或小圆点
- **State:** 选中态描边转交互蓝或类型色;参考条 chip 点击接灯箱预览

### Cards / Containers

- **Corner Style:** 12px(节点卡)、10px(悬浮控件)
- **Background:** surface-card rgba(20, 26, 43, 0.92)——0.92 的不透明度保住画布内容的隐约透底,又不让背景纹理干扰正文
- **Shadow Strategy:** 静止无影(见 Elevation)
- **Border:** 1px 类型色 @ 0.4–0.45(节点卡)或 1px 白系 @ 0.1(中性容器)
- **Internal Padding:** 10px 12px(节点卡);面板内容 12–16px

### Inputs / Fields

- **Style:** 深底(surface 同族)+ 1px 白系描边(0.12–0.16 档)+ 8px 圆角,13px 字
- **Focus:** 描边转交互蓝族,全局 focus-visible 兜底
- **Error / Disabled:** 错误文案用失败绯 #ff8f8f 12px 列在字段下方;禁用 opacity 0.4

### Navigation

- **Toolbar:** 画布左上工具栏,创作/规划分组(53 号票),ghost 按钮横向排列,12px 字
- **侧滑面板:** 右侧全高滑入,面板玻璃底 + 侧滑影,头部 15px/600 标题 + 分割线
- **端口圆点:** 24px 胶囊,面板玻璃底 + 0.28 白描边,悬浮在节点左右边缘外半枚;hover 描边转交互蓝、文字转白

### 节点卡(签名组件)

11 种节点共享同一卡面骨架:surface-card 玻璃底、12px 圆角、10px 12px 内边距、13px 基准字号,唯一变量是 1px 类型色描边(0.4–0.45 透明度)与类型定宽(220–340px)。类型色同时用于卡内图标、进度与状态徽章,让"描边色 = 这张卡是谁"成为全画布通用的辨认协议。连线态是描边协议的延伸:连线起点卡描边转交互蓝(rgba(122,162,247,0.9) + 2px 光晕),合法目标转信号绿(rgba(74,222,128,0.9) + 2px 光晕),非法目标降透明至 0.35 + 灰阶 0.7 + 虚线描边——状态一眼可辨,且不只靠透明度。

### IP 企划面(签名时刻)

IP 卡片墙与详情页是全站唯一允许"海报感"的地方:企划渐变(#243b8f → #57348a → #8a2f5c,135°)铺封面与空态徽标,Display 档大字(28–42px)在此出现。离开 IP 身份时刻,渐变与大字都不该再出现。

## Do's and Don'ts

### Do:

- **Do** 用白系透明描边勾轮廓:分区 rgba(255,255,255,0.08)、控件 0.12–0.16、可点击件 0.28
- **Do** 为每种新节点类型选一个未占用的信号色作描边身份,透明度落在 0.4–0.45,并同步用于卡内状态色
- **Do** 把阴影留给灯箱、编辑层、侧滑面板与悬浮控件;普通卡片保持平面
- **Do** 价格与用量信息用预估灰蓝 #9fb3d9 的 12px 小字,安静可见
- **Do** 焦点指示统一交给全局 `:focus-visible`(2px #7aa2f7,offset 2px),组件不再自写 focus 样式
- **Do** 状态可辨不只靠透明度:非法连线目标 = 降透明 + 灰阶 + 虚线描边三管齐下

### Don't:

- **Don't** 使用无语义的装饰色;每种颜色必须能回答"它替用户省了哪次辨认"
- **Don't** 把企划渐变(#243b8f→#57348a→#8a2f5c)用于按钮、状态或 IP 时刻之外的任何地方
- **Don't** 在静止卡片上加阴影,或用阴影替代描边做层级
- **Don't** 混用两支红:失败绯 #ff8f8f 是错误/失败,警报红 #f87171 是关卡卡类型色
- **Don't** 引入 webfont 或在 15px 以上再加界面字级;字重只用 400/600/700 三档
- **Don't** 使用纯黑(#000)或纯白(#fff)作大面积底色/文字;深底与浅字都在蓝调色域内
