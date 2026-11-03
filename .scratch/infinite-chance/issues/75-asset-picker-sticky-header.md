# 75 挑选参考素材面板头部固定、列表独立滚动(2026-11-03 grill-with-docs 定案)

Type: fix
Status: approved(2026-11-03 grill-with-docs 定案,Q3 一题定范围,与 74 号票同批切票;待实现)

## Question

「挑选参考素材」面板(`AssetPicker.vue`,47 号票素材库挑选)整个内容在一个滚动流里:标题区、tab 行(素材/角色/场景/道具)、筛选行(本IP+全局/全部)、kind 筛选与卡片网格同在根元素 `.asset-picker` 的 `overflow-y: auto`(:610)里整块滚走——往下翻素材时筛选与 tab 被滚出视野,想换条件得先滚回顶。应把头部固定、只有下面的列表滚动。

现状(已查证):面板为内嵌右缘浮层(`position: absolute; top/right/bottom: 0`,宽 `--w-panel` 380px,锚在 `.canvas-wrap` 右缘),根元素自身就是滚动容器;`ul.asset-list` 无独立 overflow/max-height,「加载更多」按钮是列表的兄弟节点;模板全部平铺在 `<aside class="asset-picker">` 下,无中间包裹层。

## 定案(2026-11-03 grilling,Q3)

- 根元素 `.asset-picker` 改为 `overflow: hidden` 的 flex 列;标题区(`.panel-head`)、说明行(`.panel-hint`)、tab 行(`.view-switch`)、scope 筛选行(`.scope-filter`)、kind 筛选行(`.kind-filter`)`flex-shrink: 0` 固定;卡片网格(`.asset-list`)+「加载更多」进独立滚动区(`flex: 1; min-height: 0; overflow-y: auto`)——「加载更多」跟随列表内容滚动,不吸底。
- 改动全在 `AssetPicker.vue` 模板结构与 scoped 样式(必要时列表区包一层滚动容器 div);组件为通用选择器,所有挂载点统一受益。
- 纯 UI 布局修复,无领域语义变化,不回写 CONTEXT.md。

## 边界

- 只动 `canvas/web/src/components/AssetPicker.vue`;状态(view/kind/scope 与 refresh 链)、面板定位与尺寸(`--w-panel`、z-index)、47 号票挑选/last_used 语义零改动。

## 验证点

- 面板内容超出一屏时:滚动只发生在列表区,标题/tab/筛选恒可见;
- 切 tab、切 scope、「加载更多」翻页、空态/加载态各形态下布局不破;内容不足一屏时无多余空白;
- `make build`(内含 vue-tsc)过;浏览器点验。
