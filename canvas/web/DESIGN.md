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
    fontFamily: "'Instrument Sans', 'PingFang SC', 'Hiragino Sans GB', 'Microsoft YaHei', system-ui, -apple-system, sans-serif"
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

这是一个只在深夜亮灯的单人工作室:整间屋子沉在蓝黑里,光只落在正在工作的东西上——选中的节点、打开的对话框、进行中的任务。界面底色是一整片深夜蓝黑(`#14284b → #0d1524 → #090c14` 的径向渐变,像窗外最后一点天光),其上的一切面板都是半透明的深色玻璃,靠 1px 细描边勾出轮廓而不是靠阴影托起。色彩在这里从不装饰:每一种颜色都在值班——蓝是可交互、绿是成功、红是失败;89 号票起节点卡不再带类型身份色,全画布统一一种深色卡面,没有语义的颜色不出现。

气质是安静克制的:11–13px 的高密度小字、600 字重的标签、紧凑的 8px 圆角,信息密度明显优先于留白——这是工具,不是海报。但克制里有两处刻意的高光:品牌渐变(深蓝→紫→酒红,135°)只出现在 IP 企划的身份时刻(卡片墙封面、空态徽标),像工作室里唯一一件永久陈列;单色画布上,工作灯蓝只照选中与运行中的节点——「谁是谁」靠标题与内容辨认,灯只照正在干活的卡。

**Key Characteristics:**

- 深夜蓝黑径向渐变打底(86 号票增补:左下 accent 微光 + 整幅静态细噪),`color-scheme: dark`,全程无纯黑纯白
- 半透明深色面板(`rgba(20,26,43,0.92)`)+ 白系透明 hairline 描边,平面靠描边不靠阴影(唯一例外:生成对话框自 90 号票起改无边框填充语言,见「生成对话框」签名组件段)
- **单色节点卡(89 号票)**:全画布一种深色卡面 + 白系三档描边;工作灯蓝 = 可交互/选中/运行,失败绯 = 失败(类型色板保留在组框调板、composer 元素小标等非节点卡面)
- 高密度小字(11–13px)、600 字重层级、系统中文字体栈零加载;品牌时刻另有一支自托管 Latin 展示字(见 Typography)
- 阴影只给真正浮起的层(灯箱/侧滑/编辑层),浮起即模态
- 8px 圆角为骨、999px 胶囊为饰,圆而不泡

## Colors

调色板是一片深夜色域:近黑蓝的底、蓝灰阶的文字、一支交互蓝,外加一套各司其职的信号色与类型色。所有色值都为深底调过,没有从浅色主题移植的痕迹。

### Primary

- **工作灯蓝 Accent** (#7aa2f7):唯一的全局交互色——焦点环、发送按钮、连线起点高亮、链接与主操作。它是深夜工作室的工作灯:亮,但只照该照的地方。

### Secondary

- **企划渐变** (#243b8f → #57348a → #8a2f5c,135° 线性):IP 企划的身份色,只用于 IP 封面、IP 卡片墙空态徽标、IP 详情页头图。这是品牌色,不是功能色,不许挪作按钮或状态。

### Tertiary(信号色 / 组框与元素语义色)

节点卡自 89 号票起单色化,不再以颜色表身份;类型色板 token 保留,继续值班于组框调板(72 号票,用户可选)、composer 元素类型小标等非节点卡面:

- **工作灯蓝 Accent** (#7aa2f7):唯一的全局交互色——焦点环、发送按钮、连线起点、选中节点 halo、运行中节点描边与呼吸 pill(89 号票:运行态从类型色亮起改归工作灯)
- **信号绿** (#4ade80):任务成功 + 连线合法目标
- **琥珀黄** (#facc15):部署中/契约提示一类弱警示(VoiceNode 部署徽章、Panorama 比例提示、composer 目录禁态与参考契约小字)
- **设定青** (#2dd4bf):composer 元素类型小标(元素 chip、@ 列表类型注记、设定图类型选中)
- **寂静金** (#d3b571):视频素材标签与声纹克隆提示的识别色——低饱和,不与琥珀警报混用(剧本节点卡身份自 89 号票退役)
- **失败绯** (#ff8f8f):错误文案、失败状态、驳回徽章(Character/Voice 的 rejected 也归此,两支红的混淆就此清掉)
- **警报红** (#f87171):关卡域告警的存留 token;节点卡身份自 89 号票退役,不再与失败绯抢语义
- **雾靛 / 罗兰紫 / 夜樱粉 / 暮橙 / 晴空蓝**(#a5b4fc / #a78bfa / #f472b6 / #fb923c / #38bdf8):原分析/音频/预演台/分镜/分镜稿节点身份色,89 号票自节点卡退役,token 保留待他面复用

### Neutral

- **深夜三层底**:bg-glow #14284b(径向高光)→ bg-navy #0d1524(主底)→ bg-deep #090c14(渐变末端),只作页面底,不作面板;全屏编辑层另有更沉一级的工作台底 --bg-layer #10162a。86 号票氛围层:主光仍在右上,左下补一层 6% accent 微光(skill corner-glow),整幅叠静态细噪(--noise-tile,全站唯一 effect,永不动画)。品牌面 ground 另有两支 deal 产:halo(登录页标后光晕,accent 色相三层径向、上下渐隐)与 band(IP masthead 暗页内缩亮带:角光 + 细噪 + hairline),每页至多一支。
- **卡面玻璃** surface-card rgba(20, 26, 43, 0.92):节点卡面
- **面板玻璃** surface-panel rgba(13, 21, 36, 0.92):控件、小地图、端口圆点
- **提亮面** surface-raised rgba(28, 40, 68):hover 反馈与浮起小件(控件 hover、chip 删除钮、灯箱按钮)——比面板亮半档的「被照到」
- **正文字** #e8eaf2 / 亮字 #cdd7ee:主内容文字
- **次级字** #aab1c5 / 弱字 #8b91a7 / 极弱字 #6f7690:标签、说明、禁用态文字
- **失败绯** #ff8f8f:错误文案、失败状态、「素材不可用」占位(信号红 #f87171 是关卡卡身份色,别混)
- **预估灰蓝** #9fb3d9:价格预估文案——成本是信息,不是警报;同一支蓝灰也用于保存态与中性容器动作(安静信息色)

### Named Rules

**The 只在值班时亮规则。** 颜色只承载语义:类型身份、状态、交互。不允许无语义的装饰色;想加一点颜色时,先回答它替用户省了哪次辨认。

**The 成本不惊扰规则。** 价格预估永远用预估灰蓝(#9fb3d9)小字,失败绯(#ff8f8f)只留给失败。钱要可见,但不必看起来像出错。

## Typography

**Display Font:** 系统中文栈——'PingFang SC' / 'Hiragino Sans GB' / 'Microsoft YaHei' / system-ui
**Body Font:** 同上(界面单一字体栈,零加载成本,桌面版与浏览器渲染一致)
**Brand Display Font(86 号票增补):** 'Instrument Sans'(可变字 wdth+wght,自托管 woff2 latin 子集,`--font-display`)——只用于品牌时刻:登录页字标、IP 列表 masthead kicker;500 字重、-2.8% 字距、96% stretch,中文不落web字体
**Label/Mono Font(86 号票增补):** 'DM Mono'(自托管 400/500,`--font-mono`)——点缀用:登录页拼贴的参数 pill、mono 数字时刻;界面正文与代码不换栈

**Character:** 务实的工具字体姿态——不追字体的表现力,靠字号与字重做层级。中文界面在 11–13px 的小字号下依然清晰,是整个高密度布局成立的前提。品牌展示字只在品牌时刻点亮,不进入任何工具面。

### Hierarchy

- **Display**(700,clamp(28px, 4vw, 42px),1.2):只出现在 IP 企划的身份时刻——卡片墙标题、空态徽标、详情页头。画布里没有 display 字。
- **Title**(600,15px,1.4):面板标题、节点卡标题、对话框标题。
- **Body**(400,13px,1.5):节点卡正文、对话框表单、说明文案。节点卡基准字号即 13px。
- **Label**(600,12px,1.4):全站最常用的层级——字段标签、按钮、chip、表头。121 处 12px + 64 处 600 字重的用量重心在这里。
- **Micro**(400,11px,1.4):占位提示、辅助注脚、徽章内的次级信息。

### Named Rules

**The 600 就够规则。** 层级靠 600 对 400 的对比建立,700 只留给 Display 与主按钮字符。不引入更多字重档位。

**The 深夜小字规则。** 界面文字 11–15px(字阶 token:--fs-micro 11 / --fs-label 12 / --fs-body 13 / --fs-title 15),15px 是上限(Title 档);只有 IP 身份时刻允许更大。这套小字是 **1920×1080 设计基准下的密度承诺**——大屏的价值是同屏信息量而非字更大。这不是缩小的理由,是密度的承诺。

## Layout

画布即桌面:全视口工作区,深夜渐变底直接可见,节点浮在其上。没有页面网格——空间组织单位是节点与视口。

- **节点宽度按类型定宽**:视频 220px、Agent/分析/角色/图片 260–280px、音频 300px、预演台/分镜/分镜稿/关卡/音色 340px;高度随内容。新节点落当前视口中心(级联偏移)。
- **密度**:节点内边距 10px 12px,卡片间距靠画布自由摆放;侧滑面板与对话框内的表单行距用 spacing md(12px)。
- **面板宽度族**:右侧滑面板(素材库/生成记录/Agent 历史/素材挑选)一律 `--w-panel` 380px,角色抽屉最宽 `--w-drawer` 400px;生成对话框 560px、IP 页对话框 440/520px 不属侧滑族。
- **浮层系**:生成对话框贴选中节点;素材/生成记录/Agent 历史为右侧滑面板;灯箱与三个编辑层(预演台/分镜/分镜稿)为全屏 Teleport 浮层。浮层永远是全高、贴边、带大阴影的。
- **响应式与分辨率基准**:桌面浏览器是唯一目标形态(移动端明确不支持),无断点系统。**1920×1080 为设计基准,1440×900 为最低可用档;低于最低可用不做任何兜底(破就破)**——窗口缩放由画布缩放(滚轮/pinch)承担,不做布局重排。桌面壳(Wails)画布主窗最小窗同步锁定 1440×900。

## Elevation & Depth

**浮起即模态**:静止的界面是平的。节点卡、工具栏、面板内容在静止时只靠 1px 白系透明描边和半透明玻璃底区分层级,不加阴影。阴影是"这一层是模态的"的信号,只出现在三种真正浮起的东西上:全屏灯箱/编辑层、侧滑面板、悬浮控件(缩放控件、小地图)。深度感的日常载体是描边亮度与玻璃透明度,不是投影。

### Shadow Vocabulary

阴影词汇与 z-index 档位表以 `style.css` 的 `:root` token 为单一事实源(`--shadow-control/slide/dialog/dialog-strong/overlay/overlay-strong`;z-index 十档 `--z-*`,新浮层先去那里领档,不许自造数值),语义注释见 token 行内注释:

- **控件浮影** (`--shadow-control`):vue-flow 缩放控件与小地图——常驻但不属于画布平面的悬浮件
- **侧滑影** (`--shadow-slide`):右侧滑面板与角色抽屉,光源感来自左侧画布
- **对话框影** (`--shadow-dialog` / `--shadow-dialog-strong`):中浮层与对话框内弹层(@ 引用浮层、参数面板);生成对话框本体自 90 号票起弱化为单层极淡的控件浮影 `--shadow-control`(无边框协议,见「生成对话框」段)
- **全屏浮层影** (`--shadow-overlay` / `--shadow-overlay-strong`):灯箱与编辑层——最重的层级用最重的影

### Named Rules

**The 浮起即模态规则。** 阴影是模态性的信号:一个元素带阴影,它就在打断用户的工作流(灯箱/编辑层/对话框/侧滑)。静止内容永不带阴影;想给卡片加影时,先问它凭什么打断用户。

**The 描边即轮廓规则。** 平面元素的轮廓一律用白系透明描边勾出,不用灰色实线、不用阴影兜底。描边只设三档(`--line-section` 0.08 分区 / `--line-control` 0.14 控件边界 / `--line-action` 0.28 可点击件),底纹同理只设四档(`--wash-subtle/soft/fill/active`)——档位语义的单一事实源在 `style.css` 的 `:root`。

## Motion(基础过渡包)

深夜工作室不表演动效——东西只是「被灯照到」和「就位」。全套过渡统一走 58 号票立的基础过渡包(86 号票对齐 skill 节奏并增补,`style.css` `:root`),组件不得自造 duration/easing:

- **时长档**:`--dur-fast` 150ms(hover 进入/描边与底色反馈)、`--dur-out` 120ms(hover 收回)、`--dur-press` 90ms(按下)、`--dur` 180ms(灯箱遮罩淡入、toast、编辑层淡入)、`--dur-slow` 260ms(面板滑入、对话框弹入)、`--dur-image` 600ms / `--dur-image-out` 400ms(封面海报呼吸,skill 图像节奏)。
- **缓动两档**:`--ease` cubic-bezier(0.22, 0.61, 0.36, 1)——深夜缓出,只出不动进;`--ease-enter` cubic-bezier(0.22, 1, 0.36, 1)——入场专用 settle 曲线,普通过渡不走它。
- **入场语义**:滑入的从右来(translateX 24px),弹入的轻轻上浮归位(translateY 8px + scale 0.98),遮罩类只做透明度;退场不做动画(工具要快关)。
- **按钮微动效(skill 数字)**:实心主按钮 hover 上步 1px、按下回沉 scale 0.985;链接卡 lift 2px、封面 zoom 1.02–1.03;只在 `(hover: hover) and (pointer: fine)` 生效。
- **常动件必配暂停**:任何持续 >5s 的循环动画(登录页拼贴轮换)必须带暂停盘,reduced-motion 下静止显示首帧组。**桌宠缺省形象豁免**(2026-11-19 用户裁决:暂停盘压住形象观感,移除;reduced-motion 静止守卫保留)。
- **编排级动效(连线动画、入场编排)不在本体系内,另票处理。**

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

13 种节点共享同一卡面骨架,89 号票起样式收编为 `style.css` 全局共享段(「One card style」落地):surface-card 玻璃底、12px 圆角、10px 12px 内边距、13px 基准字号、`--line-control` 白系描边,组件 scoped 只留宽度与独有内部件。**单色画布协议**:静止 0.14 档;hover 升 0.28 档(skill「指针必答」,真指针环境才生效);选中 = 工作灯蓝描边 0.9 + 2px halo;运行中 = 工作灯蓝 0.85 + accent 胶囊 pill 呼吸(全局 `pulse` 1.4s);排队 = 虚线描边;失败 = 失败绯 0.6 + 失败绯文案。标题统一亮字 600,无类型色、无灯牌点——「谁是谁」靠标题与内容辨认;类型定宽(220–380px)与连线三态(起点交互蓝 / 合法目标信号绿 / 非法目标降透明 + 灰阶 + 虚线)不变。媒体族四节点(Image/Video/Panorama/Audio,根类含 `media`)的占位/失败/重试/运行 pill 共享件也收在全局段,以 `.node.media` 限命名空间。组框(72 号票)仍以八色调板区分多个组——那是功能色,不是节点身份色。**卡内嵌控件自 90 号票起同走填充代描边协议**(与生成对话框对齐):模型下拉与复制/分析/反推/取消等内联按钮 = wash-soft 填充零描边、hover 提亮 wash-active;提示词区(MediaPrompt)与分析报告文本块 = wash-subtle 弱底零描边(composer textarea 已收编容器底色,卡内件仍 wash-subtle);表单 `.field`/输入框去描边,焦点靠全局 focus-visible accent 轮廓。不在其列:占位虚线与任务态描边(卡面骨架语义)、浮层族(技能浮层/@ 引用)、91 号票与 AssistantPanel 逐字归一的共享族(chip/media-chip/tool)、琥珀/绯红语义提示描边。

### 生成对话框(签名组件,90 号票无边框协议)

生成对话框是全站唯一的无边框面板:**填充代描边**——近实面板填充(surface-panel @0.98)靠色差与画布断开,外描边退役;89 号票的底部 hairline 分组(`.composer-foot` 描边 + 参数栏)随之**收编退役**,对话框内部无分割线;投影弱化为单层极淡(控件浮影档——悬浮层贴着密集线稿,一点断开还是要留)。内部控件同语言:wash 填充代描边(设定图 chips 等交互件;vref 角色下拉、玩法下拉、meta 行的模型下拉与参数摘要按钮以及 textarea 进一步**收编容器底色**——transparent 静默融合,hover 才显常色 wash-soft,参数摘要按钮右缘带模型下拉同款下拉箭头),语义色描边例外保留(目录禁态的琥珀警示);对话框内的浮层分层待遇分档:@ 引用浮层不属于本协议,保留 accent 细描边 + 对话框影以与面板本体分层;参数面板则恪守无边框协议——无描边,仅对话框影(弹出浮层不回添描边)。结构三段:顶部动作行(@ 引用 / + 上传 / 库 图标钮聚左、展开 toggle 居右)→ 大留白输入区(textarea 常规 120 / 展开 240 两档,resize 手柄移除,宽恒 560px)→ 底部 meta 行(模型带价下拉 · 参数摘要按钮 · ¥ 成本胶囊 · 圆形发送键);参数收纳进弹出面板——分段 chips 族(选中实心工作灯蓝)+ 比例网格带形状预览(图片左列竖块、视频网格 tile 两形态),时长 = 自动 chip + 滑杆 + 数字输入。

### IP 企划面(签名时刻)

IP 卡片墙与详情页是全站唯一允许"海报感"的地方:企划渐变(#243b8f → #57348a → #8a2f5c,135°)铺封面与空态徽标,Display 档大字(28–42px)在此出现。86 号票升级:masthead 升 band 面板(暗页内缩亮带:accent/雾靛/企划紫角光 + 细噪 + hairline),kicker 用品牌展示字(Instrument Sans 大写字距),h1 下一条 64px 企划渐变 bar——渐变的品牌笔触收束成一条线,不再漫漶。空态给画面:hairline 迷你画布(三枚类型色节点 + 虚线连线),替代纯文字。离开 IP 身份时刻,渐变与大字都不该再出现。87 号票补详情页:ip-head 升本页唯一一支 band(配方同 masthead),kicker + 64px 渐变 bar 照用;封面升 320px 框内大海报,hover 呼吸 600/400;画布区双列网格卡(lift 2px + 描边升档,hover 无影)、空态沿用迷你画布;时间戳 meta 用 DM Mono 点缀(mono 数字时刻);band 一次性入场(520ms `--ease-enter`),reduced-motion 静止。

### 登录面(品牌门面,86 号票)

登录/初始化共用 triptych 外壳(deal.mjs software 产):左 headline 列(字标 Instrument Sans 逐行出罩入场 + 中文宽字距行 + 一句 claim)、中作品拼贴(两组件 2.8s 交叉淡化轮换,件 = 画布自家节点卡缩微景:类型色描边 + 端口圆点 + glass 高光,件内零动画;暂停盘 32px 右下常驻)、右表单列(平板玻璃面板)。ground = halo(标后光晕)。拼贴是页面唯一常动物,「show the real thing」——展示的永远是画布自己的 UI 语汇,不放虚构插画。

## Do's and Don'ts

### Do:

- **Do** 用白系透明描边勾轮廓:分区 rgba(255,255,255,0.08)、控件 0.12–0.16、可点击件 0.28
- **Do** 新节点复用 style.css 节点共享骨架段:scoped 只声明宽度与独有内部件,任务态走 `data-task`,不为卡面挑新身份色(89 号票)
- **Do** 把阴影留给灯箱、编辑层、侧滑面板与悬浮控件;普通卡片保持平面
- **Do** 价格与用量信息用预估灰蓝 #9fb3d9 的 12px 小字,安静可见
- **Do** 焦点指示统一交给全局 `:focus-visible`(2px #7aa2f7,offset 2px),组件不再自写 focus 样式
- **Do** 状态可辨不只靠透明度:非法连线目标 = 降透明 + 灰阶 + 虚线描边三管齐下

### Don't:

- **Don't** 使用无语义的装饰色;每种颜色必须能回答"它替用户省了哪次辨认"
- **Don't** 给节点卡引入类型身份色:画布 chrome 单色,颜色只留状态语义(选中/运行/失败);组框调板与 composer 元素小标是仅有的例外(89 号票)
- **Don't** 给生成对话框壳体或其内部控件加回白系描边或内部分割线:唯一的无边框面板靠填充代描边,投影只用控件浮影单层(90 号票);对话框内浮层与语义警示(目录禁态琥珀)不在此列
- **Don't** 把企划渐变(#243b8f→#57348a→#8a2f5c)用于按钮、状态或 IP 时刻之外的任何地方
- **Don't** 在静止卡片上加阴影,或用阴影替代描边做层级
- **Don't** 混用两支红:失败绯 #ff8f8f 是错误/失败/驳回,警报红 #f87171 只作关卡域告警存留 token,不再是节点卡身份色(89 号票)
- **Don't** 在界面字上引入 webfont 或在 15px 以上再加界面字级;字重只用 400/600/700 三档(86 号票修订:品牌时刻的 Latin 展示字 'Instrument Sans' 与点缀 mono 'DM Mono' 例外——自托管、latin 子集、永不进入工具面正文字)
- **Don't** 使用纯黑(#000)或纯白(#fff)作大面积底色/文字;深底与浅字都在蓝调色域内
- **Don't** 给 hover 中的卡片加阴影:lift 2px + 描边升档就是悬浮反馈的全部,阴影仍只属于模态(浮起即模态不因 hover 破例,86 号票裁定)
- **Don't** 让常动的东西没有暂停盘,或在 reduced-motion 下不停(86 号票增补;桌宠缺省形象豁免暂停盘,2026-11-19 用户裁决)
