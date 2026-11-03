# 76 画布右键上下文菜单取代底部工具栏(2026-11-03 grill-with-docs 定案)

Type: feat
Status: implemented(2026-11-03 定案当日 implement-spec 落 feat/76-canvas-context-menu;双轴评审 + 修复轮收口,536 测试全绿;遗留浏览器点验/Wails WebView 右键实测)。**口径注记**:见文末「实现增补(2026-11-03)」。

## Question

底部工具栏(`CanvasToolbar.vue`,58 号票自 CanvasEditorView 抽出)随 53–55/57/60/62/68 等票持续膨胀,现为「创作 8钮 + 规划 4钮 + 组织 1钮 + 素材库/生成记录/上传/? 4钮」共 17 钮一条,视觉过载且占据底部整行。应改为**右键画布空白处弹出上下文菜单**选择生成,工具栏退场。

现状(已查证):

- 工具栏配置驱动:`CREATE_NODE_TYPES`(agent/image/video/previz/character/voice/audio)与 `PLAN_NODE_TYPES`(storyboard/name/level/script)两数组 `v-for` 生成按钮,音效 = `add('audio', {source:'sfx'})`;全部动作 emit 上抛宿主 `CanvasEditorView.vue:4690-4701`。
- 节点添加统一入口 `addNode(type, dataPatch?)`(CanvasEditorView.vue:3735-3759):落点 `viewportCenterPosition()`(视口中心 + `newNodeAnchor` 48px 级联,placement.ts:18-27),随后唯一选中 + `adoptLandedNode(id)` 落点自动入组。
- 全 canvas/web **无任何右键菜单实现**(唯 PrevizStage.vue:594 对 3D 舞台抑制默认菜单);`useVueFlow` 未解构 pane/node/edge 的 contextmenu 事件。
- 上传的隐藏 file input 内聚在 CanvasToolbar.vue 内,change 后 emit 给编辑器 `onUploadChange`(约 :3991);「?」= `toggleHelp` 开 CanvasHelpModal;分组 = `toggleGroupDraw()` 画框模式;素材库/生成记录 = 右缘面板 toggle。
- 无任何触屏/响应式适配(全 @media 匹配数 0),产品口径 PC 主力(58 号票)。

## 定案(2026-11-03 grilling 六问全按推荐)

1. **范围**:底部工具栏**整体删除**(含「?」);全部动作移入右键菜单。左下快捷键 hint 文字保留(位置不动)。
2. **落点语义**:**右键位置即新节点落点**——右键屏幕坐标经 `screenToFlowCoordinate` 换算为 flow 锚点,`newNodeAnchor` 级联偏移从该锚点起算;唯一选中 + 落点自动入组(`adoptLandedNode`)语义照旧。素材插入/生成记录插入等无锚点路径仍落视口中心,`viewportCenterPosition()` 保留。
3. **菜单形态**:新组件(暂名 `CanvasContextMenu.vue`),Teleport to body + `position: fixed`(循 CanvasHelpModal 先例,z-index 走现有令牌体系按需增档);四段暗色小标题:
   - **创作**:Agent / 图片 / 视频 / 预演台 / 角色 / 音色 / 配乐 / 音效(音效带 `source:'sfx'`);
   - **规划**:分镜 / 分镜稿 / 关卡 / 剧本;
   - **组织**:分组(点击即进画框模式,菜单先关、劫持拖画随之生效);
   - **面板**:素材库 / 生成记录 / 上传 / 帮助——保持既有 toggle/打开语义,菜单不做开启态标记。
   条目纯文字沿用 `NODE_TYPE_LABEL`,**不带「+」前缀、不引入图标库**,沿用现有按钮的彩色配色语言。
4. **节点/连线右键**:仅抑制浏览器默认菜单、不弹任何东西(节点右键动作——删除/复制等——留待未来另票)。**例外**:文本输入框(prompt 编辑等 typing target)内右键保留浏览器原生菜单(复制粘贴)。
5. **关闭语义**:点菜单外任意处 / Esc 关闭,Esc 挂进 `onGlobalKeydown` 现有浮层退栈序列(后开先关原则,具体序实现定);点任一条目即执行即关;画布平移/缩放(vue-flow move 事件)即关;菜单贴窗口边缘向内翻转(贴底向上弹,含 min(max) 钳制);触屏/长按不做。
6. **随迁与回写**:上传隐藏 file input 与 change 链随迁新组件(语义不变,仍走 `onUploadChange` 落 uploads 档);画框模式激活期间 pane 被劫持,右键不弹菜单(画框优先);帮助浮层 `canvasHelp.ts` 补右键菜单词条、涉及「工具栏」的章节改写;CONTEXT.md 各处「工具栏」表述按新入口回写(行 7/9/27/28/33/36/40/48/49 等);本票落文档后实现期回写。

## 边界

- 只动 canvas/web:`CanvasToolbar.vue` 删除、`CanvasEditorView.vue` 的 emit 挂线改为菜单挂线、新增菜单组件、`addNode` 增可选锚点参数(默认仍视口中心,不传不改变任何现有调用方行为)、`canvasHelp.ts` 词条。
- 后端零改动;节点类型/生成对话框/composer/画框模式/素材面板/记录面板/上传落档语义零改动;桌面壳(Wails)不动代码,仅点验。

## 验证点

- 空白处右键弹四段菜单;点「图片」节点落在**右键点位置**(级联防重叠),选中且落入已有 group 矩形自动入组;
- 音效落 audio 节点带 `source:'sfx'`;配乐/音色/预演台等各类型 data 与原工具栏路径一致;
- 分组进画框模式;素材库/生成记录/上传/帮助四入口行为与原按钮等价;上传选文件后媒体节点落档不变;
- Esc 退栈序正确(后开先关);点外关;平移/缩放关;贴底向上翻转不溢出视口;
- 节点/连线上右键无浏览器默认菜单、无自建菜单;prompt 输入框内右键原生菜单可用;
- hint 文字仍在左下;`make test` / `make lint` / `make build` 全绿(菜单条目定义若抽纯模块随附 vitest 钉住);
- **遗留点验**:浏览器手点全流程 + Wails 桌面壳右键实测(WebView contextmenu 行为)。

## 实现增补(2026-11-03)

- **级联序按定案 2 字面落**:`addNode` 锚点路径先 `screenToFlowCoordinate` 换算 flow 锚点,再在 **flow 空间**做 48px 级联(`cascadeAnchor` 纯函数,坐标系由调用方持有)——缩放≠1 时图坐标步长恒 48px,与视口中心路径 `newNodeAnchor` 语义完全一致;评审修复轮由「client 像素先叠加再换算」改回。
- **上传 file input 住视图持久层**:旧工具栏 input 本住常驻 footer 从不脱附,搬进瞬态菜单组件会让 change 发在脱附元素上(浏览器行为依赖,Wails WebView 有险);评审修复轮把隐藏 input 与 change 处理提升到 CanvasEditorView 常驻模板,菜单条目仅触发,`onUploadChange` 链零变化。
- **vue-flow 1.48.2 实证**:pane/node/edge 的 contextmenu 事件**均不代为 preventDefault**(源码核实),抑制默认菜单必须在处理器里显式 `preventDefault`;画框 overlay 另补 `@contextmenu.prevent` 兜底「画框优先不弹」。
- **Esc 退栈实测序**:`onGlobalKeydown` 内为 灯箱 → 右键菜单 → 帮助 → 面板 → 编辑层(菜单开着时全视口垫挡住画布交互,天然栈顶先收);`canvasHelp.ts` 键位速查按此序回写。
- **z-index**:`--z-context-menu: 40`(面板/抽屉之上、全屏编辑层之下),style.css 档位表增档。
- **评审收口**:`ContextMenuItemTone` 13 值字面量联合(与 `.tone-*` CSS 类一一对应)、`nodeItem` 对象参数、条目 title 生成与菜单文案同源(`落一个${text}节点`,sfx 保留旧工具栏原 title 随迁)。
