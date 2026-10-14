---
target: 无限画布编辑器(canvas/web 主界面)
total_score: 27
max_score: 40
na_heuristics: 
p0_count: 0
p1_count: 3
target_identity: "file:/usr/local/var/www/InfiniteChance/canvas/web/src/views/CanvasEditorView.vue"
target_fingerprint: "sha256:e71920f204ca9dacff31af5e336e272c055defa575e6224840f93141297b98a0"
target_path: /usr/local/var/www/InfiniteChance/canvas/web/src/views/CanvasEditorView.vue
timestamp: 2026-10-05T05-39-01Z
slug: src-views-canvaseditorview-vue
---
# 无限画布编辑器 · 设计评审

Method: dual-agent (A: agent_f7e5ae86 · B: agent_5e7a26d1)
目标:canvas/web/src/views/CanvasEditorView.vue 及其子表面(节点/对话框/面板/灯箱)。A 为设计总监评审(源码全量阅读;子代理内浏览器面不可用,由 B 经 CDP 无头浏览器完成四视图目检)。

## Design Health Score

| # | 启发式 | 分 | 关键问题 |
|---|---|---|---|
| 1 | 系统状态可见 | 3 | 保存六态灯、任务脉冲、顶栏计数齐;视频任务无耗时预期,成本变化全程不可见 |
| 2 | 贴近真实世界 | 3 | 中文领域语言一致、参考组合换占位文案贴心智;模型下拉是裸 ID,上游 4xx 英文直通 |
| 3 | 用户控制与自由 | 2 | 取消/重试/冲突双出口完整;全应用零撤销/重做,误删不可恢复 |
| 4 | 一致性与标准 | 2 | 面板/节点色纪律好;工具栏后三彩脱离语义色系,「已保存」绿与「图片」绿撞色 |
| 5 | 错误预防 | 3 | 连接矩阵预校验、时长夹紧、上传预检;节点删除无确认 |
| 6 | 识别而非回忆 | 3 | tooltip 密集、技能卡带描述;+ 左右语义靠页脚常驻行记忆 |
| 7 | 灵活与效率 | 2 | 斜杠命令/两击连线/双草稿好;对话框无键盘发送、无快捷键、无 fit-view/minimap |
| 8 | 美学与极简 | 3 | line-clamp 纪律严格;视频模式 6 控件并排、页脚提示永不消失 |
| 9 | 错误恢复 | 3 | 失败保 prompt 可重试、流中断保文本、冲突横幅双出口;错误文案未翻译未分级 |
| 10 | 帮助与文档 | 3 | 空态文案带教学;两击连线新范式只有页脚一行 hint |
| 总分 | | 27/40 | Acceptable(20-27 上沿,离 Good 差 1 分) |

## Design Specificity Verdict

LLM 评审:特异性高,但由行为而非外观承载——生成对话框贴选中节点(ADR 0002)、左右 + 编码连线方向、参考 chip 就地换角色、Agent 投递沿连线覆写 prompt,每处都对得上决策票号;视觉皮肤(深色海军蓝+半透明卡片+四色描边)是 AI 画布品类的公约数,「不拖后腿」但不出彩。

确定性扫描:CLI 源码级 0 发现(经 --no-config --no-inline-ignores 复扫证实非过滤所致)。浏览器注入(4 视图)报 14 项,评估后 13 项为误报(vue-flow 视口裁剪是画布原生机制、覆盖式抽屉遮挡是抽屉语义、arial 占比是 headless 缺中文字体回落、em-dash 是中文规范标点),1 项真实:CanvasListView.vue:336/402 主按钮白字蓝底对比度 4.3:1,差 0.2 到 AA。检测器补上了 A 漏掉的这一点;A 找到的 3.0:1 小字灰(#5b627a 11px)超出检测器能力,两路互补。

覆盖层:注入在自动化无头浏览器中成功(黄色发现标签在该浏览器内可见),用户自己的浏览器未被触碰;此版 detect.js 无 [Human] 标签机制。

## Overall Impression

工程纪律罕见的自用工具:状态机、错误留痕、冲突处理都是「认真做过」的水准。但三个 P1 全部指向同一件事——这是给「已经全会的人」做的界面:没有撤销、没有键盘通道、主路径(生成)失败时静默蒸发。日用主力工具的第一天很稳,第三十天开始收摩擦税。最大机会:把「设计特异性押在交互语义上」的资产延伸到视觉层,同时补齐强用户通道。

## What's Working

1. 等待态文案精准对位焦虑源:「任务在服务端进行,关闭页面不丢失」写在用户盯着的占位卡里,而非 tooltip——长任务+意外关页这一最痛时刻的就地安抚。
2. 提示词与产物永久配对(产品原则 4 的 UI 兑现):MediaPrompt 随身携带、失败节点保留、截断可展开可复制,识别优于回忆的教科书实现。
3. 冲突处理不耍赖:多标签同改以显式横幅给「加载服务器版/以我的版覆盖」双出口,单用户工具里罕见的诚实设计。

## Priority Issues

1. [P1] 零撤销/重做 — 选中+Delete 即时删节点(连带连线与 prompt 携带),误触零恢复;迭代型画布的增删试错被抑制。修:snapshot() 已是完整图序列化,维护 20 帧环形栈接 Cmd+Z;或接 vue-flow history 插件。→ /impeccable harden
2. [P1] 模型目录失败时生成入口静默蒸发 — composerVisible 要求 models.length>0,目录拉取失败 catch 注释「不打扰画布编辑」→ 选中节点对话框就是不出现。修:目录空时渲染禁用态对话框+原因文案。→ /impeccable harden
3. [P1] 键盘与导航通道残缺 — 对话框 textarea 无 Enter/Cmd+Enter 发送(只绑 @input)、无加节点/开面板快捷键、无 fit-view 控件与 minimap。修:Cmd+Enter 发送、Esc 统一关闭、F fit-view、引 vue-flow Controls+MiniMap。→ /impeccable audit
4. [P2] 成本在决策点全程隐形 — 产品原则 3 写「成本透明可对账」,但模型/分辨率/时长选择处无任何价格信号。修:模型 option 带计价摘要、发送旁给本次预估。→ /impeccable clarify
5. [P2] 反馈与对比度细节 — 非法连线点击静默取消无 toast、置灰仅靠 opacity:0.3 传达;主按钮 4.3:1、11px 小字约 3:1。修:非法连线给轻量 toast、状态加图标不只靠透明度、两处对比度调档。→ /impeccable polish

## Persona Red Flags

- Alex(强用户):全流程鼠标依赖——无键盘发送、无节点复制/克隆、Esc 只覆盖连接态与灯箱、框选后无批量动作、多节点失败只能逐个重试。
- Sam(无障碍):键盘用户无法完成核心任务;icon-only 按钮多只有 title 无 aria-label(NodePorts 例外);全库几乎无 :focus-visible;灯箱无焦点捕获与归还;状态区分依赖颜色+透明度。
- Riley(压力测试):空画布零引导——「+ 图片加空节点→对话框贴上」冷启动路径纯靠猜;composer textarea 无高度上限,超长草稿撑破 clamp。
- 项目 persona(作者本人,三线并重长期日用):迭代链产物固定 +320px 右落且不 fitView,长链持续走出视口;200+ 节点后无 minimap/搜索/大纲,素材面板无搜索只 30 条/页硬滚;Agent 线 260px 卡与图片线 280px 大卡不对等。

## Minor Observations

- 非设计但重要(B 顺带发现):部署栈 JWT_SECRET 为空且 REQUIRED=false,线上接受源码公开开发常量签发的会话——建议尽快落 .env 真密钥。
- composer clamp 的 halfW=280/estH=240 硬编码,视频模式实际更高时贴边不准。
- 顶栏三横幅可同时堆叠,逐条把画布往下推。
- save-state 绿与图片节点绿、add-asset 紫与分析节点紫,工具栏后半段色系语义失守。
- 记录面板整行可点+行内按钮热区嵌套,误点率高区域。

## Questions to Consider

1. 页脚「拖拽节点排布,点 + 号连线」对唯一用户永远是噪声,而「这个模型多少钱、这段视频等多久」从未出现——这界面是给「已经全会的自己」做的,还是给「三周后的自己」做的?
2. 「成本透明」是不是被降级成了 admin 后台的特权?
3. 主打迭代实验的工具没有撤销栈——「素材永不死」能补偿「被误删的 20 轮 Agent 对话」吗?
