# 77 右键菜单二级化:分区入口 + hover 飞出子菜单(2026-11-04 grill-with-docs 定案)

Type: feat
Status: implemented(2026-11-04 定案当日 implement-spec 落 feat/77-context-menu-flyout)

## Question

76 号票落地的右键菜单是**单层平铺**:四段(创作 8 / 规划 4 / 组织 1 / 面板 4)共 17+ 条一次性全部可见,段标题只是分组标签,菜单过长。应改为**两级级联**:一级只出现四个分区入口,hover 后展开二级菜单显示该分区的条目。

**术语约定**(本票统一口径,避免与 72 号票领域词撞车):**分区(section)= 菜单一级分类**(创作/规划/组织/面板,沿用代码 `ContextMenuSection` 既有概念);**飞出子菜单(flyout)= hover 一级分区入口展开的二级条目列表**;「分组」一词继续专指画布分组(组织分区里那条 `group-draw` 条目),不用于称呼菜单层级。

现状(已查证):

- 条目数据与渲染分离:`canvasContextMenu.ts` 内容模块持有 `CONTEXT_MENU_SECTIONS: ContextMenuSection[]`(`{id,label,items[]}` 四段平铺),`CanvasContextMenu.vue` 只 import 常量与类型渲染、不含条目字面量;`canvasContextMenu.test.ts` 按四段结构钉住条目集。
- 渲染:Teleport to body + `position: fixed`;全视口透明垫(mousedown/contextmenu 即关);挂载后 `clampMenuPosition` 量实际尺寸贴边钳制;条目按钮按 `tone-*` 13 值字面量取色,`title` 悬停提示,upload 条目受 `uploading` prop 禁用。
- 交互现状:无 hover 二级概念;点条目 `onPick` 执行即关;Esc 由视图全局栈收(序:灯箱→右键菜单→帮助→面板→编辑层);画布平移缩放即关。

## 定案(2026-11-04 grilling 六问全按推荐)

1. **二级化范围——全部四段**:一级菜单只剩 4 行分区入口(`创作 ▸ / 规划 ▸ / 组织 ▸ / 面板 ▸`),每行 hover 展开对应飞出子菜单;以后创作/规划增节点类型不撑爆一级。代价是面板四入口从一击变两击(hover + click),已接受。76 定案 3 的四段条目集、文案、tone 配色、音效 `source:'sfx'` 第二入口**原样随迁**,只是从「段标题 + 平铺」改为「分区入口 + 飞出」。
2. **一级分区入口形态**:纯文字 + 右侧 ▸ 指示符(**CSS 画,不引图标库**,守 76 定案 3 口径),中性 tone(现 `tone-neutral` 灰);不加条目数徽标、不加分区内容预览。彩色语言(13 tone)完整保留在飞出内条目上。
3. **hover 交互——纯 hover 驱动**:悬停一级分区入口**立即**展开飞出(无延迟);移到另一分区自动切换;移出菜单与飞出整体范围飞出即收;**点击一级分区入口本身不做任何事**(不执行、不关闭菜单);飞出内条目 hover 高亮、点击执行即关(与现有一致)。触屏/长按继续不做(76 定案 5,PC 主力)。
4. **飞出位置与贴边**:飞出默认贴分区入口右侧、顶部与该入口对齐;菜单贴视口右缘时飞出翻到左侧;纵向钳制在视口内(飞出挂载/切换后量自身尺寸钳制,思路同 `clampMenuPosition`);一级菜单本身的贴边钳制照旧不动。
5. **76 号票其余定案零改动**:落点语义(右键点即落点)、关闭语义(点外 / Esc 全局退栈一次关整个菜单——飞出**不**作为独立 Esc 层 / 平移缩放即关)、节点与连线右键抑制、画框模式优先、上传 input 住视图持久层、条目 title 与菜单文案同源生成规则。
6. **回写**:`canvasHelp.ts` 「右键菜单」词条的形态描述随迁(四段平铺 → 分区 + 飞出);CONTEXT.md 不动(菜单层级是实现语义,不触及领域词);76 号票文末加「菜单形态被本票取代」指向注记。

## 边界

- 只动 canvas/web:`canvasContextMenu.ts`(数据结构两层化:分区入口 + 飞出条目;`ContextMenuSection` 形态演化,`nodeItem` 工厂与 tone 体系不动)、`CanvasContextMenu.vue`(两层渲染 + hover 态管理 + 飞出定位钳制)、`canvasContextMenu.test.ts`(重钉两层结构)、`canvasHelp.ts` 词条。
- 后端零改动;节点类型/落点语义/关闭语义/画框模式/上传落档/面板行为零改动;不引图标库;键盘导航维持 76 号票现状(未做)。

## 验证点

- 空白处右键弹一级 4 行;hover 「创作」飞出 8 条、「规划」飞出 4 条、「组织」飞出分组 1 条、「面板」飞出 4 条;分区间移动自动切换;飞出条目集与 76 号票完全一致(测试按两层结构钉住);
- 飞出内点「图片」仍落**右键点位置**(级联防重叠);音效带 `source:'sfx'`;分组进画框模式;素材库/生成记录/上传/帮助与原按钮等价;
- 点一级分区入口本身:菜单不关、无任何动作;移出菜单 + 飞出整体范围飞出收起;
- 菜单贴右缘时飞出翻左侧不溢出视口;一级菜单贴边钳制照旧;Esc 一次关整个菜单(飞出不是独立层);点外关、平移缩放关;
- 上传中 upload 条目在飞出内仍禁用;`make test` / `make lint` / `make build` 全绿;
- **遗留点验**:浏览器手点 hover 全流程 + Wails 桌面壳实测(hover 飞出在 WebView 的行为)。

## 落地注记(2026-11-04 implement-spec 同日实现)

- **分支与提交**:`feat/77-context-menu-flyout`(基于 main 顶点 e2b12c0):票面入册 b4f39a5 → 实现三提交 d0ba287(数据层二级化 + `clampFlyoutPosition` 纯函数)/ 36f6906(两层渲染 + hover 态)/ 4f04f7f(帮助词条随迁)→ 评审修复轮 a6c1341(邻接不变量钉测)+ ca3c42d(样式与单轴钳制去重)。
- **验证**:`make test` 547 测试全绿(其中 `canvasContextMenu.test.ts` 26 条:两层结构重钉 + 一级钳制照旧 + 飞出钳制 7 用例 + 邻接不变量 3 场景)/ `make lint` / `make build`(含 vue-tsc)三连全绿;MySQL 集成测试实跑非 skip。
- **实现裁量**(评审备查,实质满足):①飞出横向锚取**菜单外缘**而非入口按钮右缘(按钮内缩在 padding 里,贴按钮右缘会盖住 ▸ 箭头列;翻左侧贴菜单左缘允许 ≤9px padding 条重叠保无缝);②`ContextMenuSection` 形状保持 `{id,label,items}`,label 语义升为一级入口文案、items 即飞出(测试钉形状 + 票禁徽标/预览,演化走语义与文档);③「移出即收」取立即收 + `relatedTarget` 守卫(仍落任一面板内不收),不用关闭延迟定时器,守定案 3「无延迟/即收」字面;④入口 hover/展开态 `is-open` 高亮按级联菜单惯例补齐。
- **邻接不变量**(评审疑点收口):纵钳数学保证钳后飞出顶 ≤ 入口顶、下缘不低于被同 margin 钳制的菜单底,飞出纵向跨度必覆盖入口行,指针跨缝不会先离开飞出被收;该不变量已写纯函数测试钉死(不钳位/贴底钳位/翻左三场景,变异验证测试非空)。
- **hover 边界**:一级菜单与飞出共用 `onPanelLeave`,`mouseleave` 以 `relatedTarget` 判定去向;分区间移动不触发面板 mouseleave,自然自动切换。
- **评审收口**:面板 CSS 11 条同形声明合并 `.ctx-menu, .ctx-flyout` 共享选择器(只合并不改值);提取私有 `clampAxis` 单轴钳制(`clampMenuPosition` 两轴与 `clampFlyoutPosition` 纵向共用,导出面与行为逐字节不变);删 `openSection` 同值判等冗余守卫。
- **遗留点验**:浏览器手点 hover 全流程 + Wails 桌面壳实测(hover 飞出在 WebView 的行为)。
