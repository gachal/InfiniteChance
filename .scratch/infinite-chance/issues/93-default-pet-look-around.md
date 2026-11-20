# 93 缺省桌宠形象改版:去腮红去嘴、常驻动画改左看右看(2026-11-20 定案)

Type: feat
Status: approved(2026-11-20 grill-with-docs 定案,待实现)

## Question

用户要求默认助手(即 92 号票的 CSS 缺省桌宠形象「绿毛球」)改为**无腮红、无嘴巴,只有 2 只眼睛**;默认动画从呼吸/眨眼改为**左看右看**。

术语对齐:「助手」本体无形象(CONTEXT.md 三词切分),本票落点是 `canvas/web` 的 `CanvasPet.vue` scoped CSS 缺省形象——绿毛球目前 = `.pet-default` 身体 + 两眼(`.pet-eye`)+ 两腮红(`.pet-cheek`)+ 一嘴(`.pet-mouth`),常驻动画 = 呼吸 `pet-breathe` 2.8s + 眨眼 `pet-blink` 4.4s。

## 定案

- **形象删减**:模板删除 `.pet-mouth` 与 `.pet-cheek`(left/right)三个 span 及其 CSS 块;保留两只 `.pet-eye`。绿毛球名称与绿色不变。
- **常驻动画 = 左看右看为主**:
  - 眼睛组:两眼同步 translateX 左右平移(观感 = 眼珠瞟动),节奏「中 → 左停 → 回中 → 右停 → 回中」循环;
  - 身体轻微跟随:与眼睛同节奏的 ±2° 旋转倾斜(张望感),可与既有 `pet-breathe` 以逗号并列多动画叠加;
  - **呼吸(2.8s)与眨眼(4.4s)保留叠加**,不删除。
  - 具体平移量/周期时长实现期裁量(参照:眼宽 10%、形象 72px;平移量取脸宽 6–12% 量级);`prefers-reduced-motion` 守卫照旧(reduce = 静止首帧)。
- **范围**:纯前端 `canvas/web`(CanvasPet.vue 模板 + scoped CSS),**不碰后端、不碰 94 号票**;形象是 94 号票「绿毛球实体」的外观底座,但两票无依赖顺序。

## 裁量与注记

- 眼睛无眼眶(纯色圆点贴脸),平移即「瞟」,不引入瞳孔分层——保持零素材依赖纪律(仓库不内置 GIF/SVG)。
- 左看右看是 CSS keyframes(与 92 号票「不引入 Lottie」纪律一致)。
- 自定义桌宠的动图自带动画,不受本票影响(改版只作用于 CSS 缺省形象)。

## 验证点

- grep 回验:`pet-cheek` / `pet-mouth` 在 canvas/web 全仓零残留。
- `pnpm -r test` 全绿(纯 CSS 无新逻辑用例,既有 canvasPet 测试同绿)。
- 浏览器点验:未配置桌宠时看缺省形象——无腮红无嘴、左看右看节奏自然、呼吸眨眼仍在、拖动/点击微动效不变。
