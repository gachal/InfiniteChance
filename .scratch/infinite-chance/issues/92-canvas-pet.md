# 92 桌宠化身:画布桌宠 UI + settings pet 配置 + 管理台设置页(2026-11-19 定案)

Type: feat
Status: implemented(2026-11-19,feat/92-canvas-pet)

## Question

用户要求画布增加 agent 桌宠:桌宠需要有动态动作、可以拉动,点击后右侧固定区域出现 agent 对话框(91 号票画布助手面板);桌宠可以在管理后台修改。参考截图为即梦式绿毛球桌宠(待机呼吸/眨眼动效)与其唤起的右侧助手面板。

## 定案

- **术语**:「桌宠」(pet)= 画布助手(91 号票)在画布上的**化身壳**,纯 UI 形态实体,不承载会话/工具语义;助手面板亦可由顶栏入口独立打开(91 号票),桌宠只是最顺手的唤起点。
- **配置本体 = settings KV 新 key `pet`**(照 `storage` 先例:按请求读表热生效、网关挂 admin CRUD(`GET/PUT /admin/settings/pet`)、canvas 侧同库只读、`NormalizePetConfig` 校验、密钥外字段原值回显)。配置项:
  - **名称**:悬浮提示与面板标题用。
  - **问候语**:助手面板新会话首屏文案。
  - **建议 chips**:开场快捷指令数组(参考即梦式「SD提示词书-镜头」等),面板「换一批」轮换;点击 = 预填助手输入框不直接发送(照玩法预设范式)。
  - **缺省对话模型**:新会话模型下拉初值(目录仍以 `GET /prompt-models` 实时为准,配置值不在目录时回落首项)。
  - **形象素材**:待机**动图**一张 + 互动**动图**一张(可选);GIF/WebP 动图(APNG 随 PNG 嗅探天然可用),管理台上传,经网关 admin 面上传端点(与 settings CRUD 同组)落 objectstore `uploads/` 前缀——桌宠配置是**系统资产,不进创作素材库**(不落 assets 行,配置存对象键)。
- **缺省形象**:前端内置 **CSS 动画形象**(呼吸/眨眼类 keyframes,零素材依赖);仓库不内置任何 GIF 文件。动画实现 = 配置了动图时 `<img>` 循环播放,**不引入 Lottie**(仓库无此依赖,CSS @keyframes 是现状);拖动/点击微动效(拖起放大等)走 CSS。
- **交互**:
  - 画布区内自由拖动;**按下位移阈值**区分点击与拖动(位移小 = 点击,超阈值 = 拖动)。
  - **单击**开助手面板;拖动仅改位置不触发点击。
  - 位置**按画布存 localStorage**(键含画布 id;不进库、不同步多端);缺省驻画布右下。
  - 右键桌宠无菜单;收起态(顶栏开关,见下)同样按画布存 localStorage。
  - **顶栏开关**:收起/唤出桌宠(收起时不渲染,非仅隐藏)。
- **管理台**:admin-web 画布管理区加「**桌宠设置**」页(路由挂 `/canvas-admin` 组,照 StorageView 范式:表单 + 保存 + 形象上传预览);中文标题进 `zh-CN/page.json`。

## 裁量与注记

- 上传端点挂网关 admin 面的裁量理由:settings 读写已挂网关 `/admin`,桌宠形象属系统配置资产,与创作素材(assets 库、canvas JWT 面)分域;上传走 objectstore `Dynamic` 按当前存储驱动路由(网关已装配,23 号票先例)。
- 互动动图播放时机(点击瞬间/拖动落定)实现期裁量;MVP 可先只对待机图生效,互动图到位后接入。
- 桌宠悬浮无对话气泡(动画只做形象,对话全部进面板)——防止画布浮层栈再添一层。
- 桌宠与 vue-flow 无交互(不进节点层,`--z-side-panel` 同族独立 z 档,领取照 style.css 档位表纪律)。

## 验证点

- `NormalizePetConfig` 校验钉测(缺省回落、动图键缺失、chips 数组形状)。
- 上传端点魔数嗅探(GIF/WebP/APNG-PNG)与落桶前缀 grep 回验。
- 热生效:改配置不重启即时可见(照 storage 按请求读表)。
- 浏览器点验:拖动/点击阈值手感、位置与收起态按画布记忆、CSS 缺省形象、动图形象、管理台设置页构建与保存链。
- make test / lint / build 全绿。

## 实现注记(2026-11-19,feat/92-canvas-pet)

- **分支**:feat/92-canvas-pet(开工时 merge 了集成分支顶点 461fd5c;TDD 纪律逐域红→绿)。
- **配置本体**:settings KV `pet`(零 schema 变更,KV 行不是 DDL);`NormalizePetConfig`/`NewPetReader` 落 `internal/settings/pet.go`,CRUD 落 `pet_handler.go` 照 storage 合并语义(PUT 指针字段:未携带沿用已存、显式空串/空数组清空);无密钥字段全部原值回显。
- **上传端点**:`POST /admin/settings/pet/image` 落 `internal/pet` 包(要吃 asset 嗅探与 objectstore,与 settings 分包防 import 环;wiring 挂同一 admin 组);GIF/WebP/PNG(APNG)白名单、20 MiB 上限、MaxBytesReader 先于解析;复用 asset 包导出的 `SniffMagic`/`ExtensionOf` 薄包装(单一事实源,零嗅探复制);落 `uploads/{yyyymmdd}/{uuid}.{ext}`、不落 assets 行(grep 回验:键拼装在 pet.go:petImageKey)。
- **取用面裁量**(票面留白):读端点 `GET /pet/object?key=…` 挂画布侧(internal/pet),照 `/assets/:id/content` 先例**故意不挂鉴权**(AGENTS 硬性约束 5 理由:img 带不了 Authorization);防遍历 = 键与当前配置登记的 idle_image/interactive_image 精确相等才放行(未登记与穿越一律 404,改配置即刻生效);读侧对内容类型再嗅探一次(不信任键扩展名),`Cache-Control: private, max-age=3600`(键不可变,新形象走新键)。画布侧配置端点 `GET /pet/config` 挂 JWT。管理台预览双态:保存前本地 objectURL、保存后读端点。
- **关键裁量**:拖动阈值 6px(欧氏距离,压线算点击);形象边长 72px、内边距 24px;chips 每批 4 条;localStorage 键 `infinitechance.canvas.pet.position.{canvasId}` / `.pet.collapsed.{canvasId}`;z 档领 `--z-pet: 7`(盖过侧面板 6 便于面板开合时桌宠仍可拖拽,低于画布浮动 10);互动动图 MVP = 点击瞬间短播 1.6s 回待机(播放时机裁量落点);缺省对话模型目录以 canvas 面实时为准、不在目录回落首项(admin 页存纯配置串,不校验目录——管理台不消费创作面目录,笔记 05 坑 5 倾向采纳)。
- **顺手修正**:91 号票 `toggleAssistantPanel` 在开面板分支里把 `assistantPanelOpen` 自己关掉(面板从顶栏打不开);92 号票接线时抽 `openAssistantPanel()` 作互斥组唯一开面板入口并修正(桌宠单击只开不关,顶栏按钮 toggle)。
- **测试数**:Go 新增 12 个测试函数(含子测试共 22 例;settings 7 + pet 包 5,`make test` 全绿 26 包);前端新增 16 例(packages/api 4 + canvas-web canvasPet 12,canvas-web 既有全量 763 同绿);`make test && make lint && make build` 全绿。
- **遗留点**:浏览器点验(拖动手感/位置记忆/动图/管理台保存链)留集成验收;互动动图只有「点击短播」一种时机,拖动落定时机未做;AGENTS.md「internal 包速览」表未加 `pet` 行(留合并时顺手补);uploads/ 孤儿对象(上传后不保存配置)可接受,未做清扫任务。
