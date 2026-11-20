# 94 多桌宠管理:绿毛球固定默认 + 多自定义桌宠 + 随时切换(2026-11-20 定案)

Type: feat
Status: implemented(2026-11-20,feat/94-multi-pet-management)

## Question

用户要求:管理后台中「绿毛球」为**固定默认桌宠**(不可删),支持**添加多个自定义桌宠**并保留,**默认绿毛球可以随时与自定义桌宠切换**(切 active)。

现状(探查坐实):全链路「单桌宠」是隐式假设而非显式建模——settings 单 KV 行 `pet`,无桌宠 id、无列表端点、无 active 概念;`GET /pet/object` 防遍历 = 键与当前配置登记两键精确相等;管理台设置页单表单。

## 定案

- **领域模型三词**(回写 CONTEXT.md「桌宠」条目):
  - **默认桌宠(绿毛球)**:内置实体,固定 id `default`;形象**锁定**为 93 号票改版后的 CSS 缺省形象(无图槽,服务端拒绝为其设置形象键);**不可删除**;名称/问候语/chips/缺省模型照常可编辑(空文案走 92 号票既有前端内置兜底,行为与今天一致);列表恒居首位。
  - **自定义桌宠**:用户添加的实体,服务端生成 uuid id;一份完整配置(名称/问候/chips/缺省模型/待机动图/互动动图);上限 **20 只**(文档总数 ≤21)。
  - **当前桌宠(active)**:文档级指针 `active_id`,自由切换,缺省 `default`。
- **配置粒度**:每只桌宠**各带一份完整文案配置**(问候/chips/模型)——切桌宠 = 换皮 + 换人设,助手面板首屏文案与默认模型跟着 active 走(2026-11-20 用户裁决 a 口径)。
- **存储**:settings KV **新 key `pets`**,单 JSON 文档 `{active_id, pets: [...]}`(照画布图「一列文档」哲学,零 schema 变更;KV 行不是 DDL)。每只桌宠字段沿用 92 号票 PetConfig 字段名(`id` + `name/greeting/chips/model/idle_image/interactive_image`)。
- **校验 `NormalizePetsDoc`**(沿用 PetConfig 规则逐只套用 + 文档级新规):
  - 逐字段 trim、chips 空串剔除全空规约 nil、形象键必须 `uploads/` 前缀规范键(照 `normalizePetObjectKey`);
  - 文档级:pets 数 1..21、id 非空且唯一、`default` 必在数组、`active_id` 失靶规约回落 `default`、**default 的形象键恒规约为空**(不报错,锁定形象的 enforcement);
  - 上传端点沿用 `POST /admin/settings/pet/image` **路径不动**(92 号票先例,白名单/20MiB/魔数嗅探全不变)。
- **admin 面端点**(REST 式五端点,网关 `/admin` JWT 组;整文档 PUT 的隐式删除 diff 回收是暗坑,REST 让对象回收挂在 DELETE 语义上):
  - `GET /admin/pets` → 整份文档(含 updated_at);
  - `POST /admin/pets` → 新建自定义桌宠(服务端生成 uuid id);
  - `PUT /admin/pets/:id` → 更新单只(指针字段合并语义照 92 先例:未携带沿用已存、显式空串/空数组清空);
  - `DELETE /admin/pets/:id` → 删自定义;**id=default 拒绝**(409);物理回收其登记的对象键(uploads/ 前缀、无 assets 行引用、无共享,照「先删行后删对象」纪律);**删 active 桌宠时 active 回落 default**;
  - `PUT /admin/pets/active` → body `{id}` 切换当前桌宠。
  - 旧 `GET/PUT /admin/settings/pet` **退役删除**(admin-web 同步改调新端点)。
- **canvas 面契约零破坏**:`GET /pet/config` 路径与响应形状**不变** = active 桌宠的投影(`name/greeting/chips/model/idle_image/interactive_image`);绿毛球 active 时 image 字段空 → 前端 `CanvasPet.vue` CSS 回落天然生效,画布侧消费端零改动。
- **`GET /pet/object` 防遍历集合放宽为「全部桌宠登记键的并集」**(2026-11-20 用户裁决):避免切换瞬间画布 `<img>` 404 抖动、管理台预览未激活桌宠直接可用;键全为本人上传系统资产,并集不放大暴露面。其余纪律不变(未登记 404 无探针回声、读侧内容类型再嗅探、`Cache-Control: private, max-age=3600`)。
- **旧 `pet` 行一次性惰性迁移**(2026-11-20 用户裁决 a 口径):首次读 `pets` 缺行时读旧 `pet` 行——
  - 旧配置**有形象**(idle_image 非空)→ 转成一只自定义桌宠(文案照搬)并设为 active;
  - **纯文案无形象** → 文案并入绿毛球(名称非空也并入),active 保持 `default`;
  - 迁移写库后旧行**保留不删**(弃用即可,历史可溯);Reader 不做双形状兼容。
- **管理台 UI**(2026-11-20 用户裁决 a 口径,两栏列表 + 表单):`PetSettingsView.vue` 重构——左侧桌宠列表(绿毛球恒首项带「默认」徽标、active 带标记、自定义项带删除)、右侧选中项编辑表单(名称/问候/chips/模型/两图上传预览 + 「设为当前」按钮);绿毛球表单无图槽(显示「内置 CSS 形象」说明);「添加桌宠」按钮新建。
- **localStorage 不分键**(2026-11-20 用户裁决):位置/收起键照旧按 canvasId——摆放习惯跟人走不跟皮走。

## 裁量与注记

- 绿毛球实体**存进 pets 数组**(id=default 完整实体)而非文档顶层独立字段——列表/表单/active 指针三处统一口径,缺省文档 = `{active_id: "default", pets: [default 实体]}`。
- 助手面板开着时切 active:面板文案经既有 `window focus → refreshPetConfig` 链路下轮生效,不另做实时推送。
- 互动动图语义不变(点击短播 1.6s),每只自定义桌宠独立配。
- 迁移防御性写:旧行零值/坏 JSON 时直接落缺省文档,不报错(照 NewPetReader 容错先例)。
- uploads/ 孤儿对象(上传后不保存、删除桌宠时对象已不在)可接受,不另做清扫任务(92 号票同款裁量)。

## 验证点

- `NormalizePetsDoc` 钉测:缺省构造、default 形象恒空规约、id 唯一、active 失靶回落、上限 21、逐字段校验沿用。
- Reader 惰性迁移钉测:有形象分支 / 纯文案分支 / 零值容错 / 幂等(二次读不再迁移)/ 旧行保留。
- DELETE 语义钉测:删 default 拒绝、删 active 回落 default、对象物理回收 grep 回验。
- `GET /pet/object` 并集放行/未登记 404 钉测改造。
- `GET /pet/config` active 投影形状与 92 号票逐字段一致(契约钉测)。
- packages/api 新函数 envelope 断言;admin-web 构建通过 + 浏览器点验(列表/编辑/切换/删除/迁移后的既有数据)。
- make test / lint / build 全绿;双方言无 DDL(KV 行)确认。

## 实现注记(2026-11-20,feat/94-multi-pet-management)

- **端点落包**:REST 五端点 + 上传端点全在 `internal/pet`(新文件 `pets_admin.go`);settings 包不 import objectstore 的方向纪律保住(DELETE 的对象回收需要 objectstore,settings→objectstore 会成环)。`GET /admin/pets` 回 `{"doc": …}`、POST/PUT 回 `{"pet": …}`(都附 `doc` 整份)、DELETE 回 204。
- **类型命名**:settings 侧 `PetsDocument`/`PetEntry`(嵌入 92 号票 `PetConfig`,条目线形即 `{id, name, …}`)/`NormalizePetsDoc`/`DefaultPetsDocument`/`ActiveEntry()`/`NewPetsDocReader`/`NamePets`/`DefaultPetID`/`MaxCustomPets=20`/`MaxPetsTotal=21`;旧 `PetConfigReader`/`NewPetReader` 退役删除。前端 packages/api 侧 `PetEntry`/`PetEntryInput`/`PetsDocument`(`PetConfigInfo` 改为 `Omit<PetEntry, 'id'>`,canvas/web 零改动)。
- **迁移实测分支**:fake settings store 上钉测四分支全过 —— 有形象(idle_image 非空)→ 转自定义桌宠(uuid id)设 active;纯文案 → 并入绿毛球文案、active 保持 default;旧行坏 JSON → 缺省文档;pets 行坏 JSON → 宽容回缺省。迁移幂等(二次读以 pets 行为准)与旧行保留均有钉测。迁移即写库(缺行首次读落 pets 行),写失败回内存文档不瘫痪画布面。
- **NormalizePetsDoc 增补规约**:default 恒居数组首位(stable move)、`PUT /admin/pets/default` 携带形象键直接 400(服务端拒绝为其设形象键;文档级规约恒空是第二道防线)。
- **DELETE 对象回收**:按文档内引用计数去重(兄弟条目仍登记的键不删),先删 KV 行后删对象,对象删除失败仅记日志(孤儿可接受,92 号票同款裁量)。
- **gin 路由静态段与参数段共存**:`PUT /admin/pets/active` 与 `PUT /admin/pets/:id` 同树注册,gin v1.12 静态优先,钉测坐实。
- **旧端点退役**:`settings/pet_handler.go` 删除,`GET/PUT /admin/settings/pet` 挂载摘除;`POST /admin/settings/pet/image` 路径与实现不动;grep 回验 CRUD 旧路径零残留(仅注释提及退役史与上传端点既有路径)。
- **canvas 面**:`GET /pet/config` 形状逐字段不变(active 投影)、`GET /pet/object` 并集口径(`Cache-Control: private, max-age=3600` 与读侧再嗅探钉测保留);canvas/web 零改动,localStorage 不分键。
- **回验**:make test / make lint / make build 三绿(Go 两包 42 测试、packages/api 73 测试、canvas/web 772 测试);集成分支顶点 32178ba 已 fast-forward 并入(93 号票 canvas/web 改版,无冲突)。
- **评审修复轮(2026-11-20,feat/93-94-pet-review-fixes)**:双轴评审 6 项修复全落 —— C1 读侧规约静默覆盖收口(settings 拆 `loadPetsDocCore(strict)`:读路径容错回落缺省文档绝不写库;写路径走 `LoadPetsDocForWrite`,库存行 decode/规约失败 500 `pets_doc_corrupt` 拒写,坏 JSON 同口径;钉测坐实 22 只非法形状下 GET 容错行字节不变、四写端点全拒);S1 `findEntry`/`docReferencesKey` 收敛重复;S2 `docWithStamp` 单 Get 同取文档与时戳,时戳读错不再静默吞;S3 packages/api 导出 `MAX_PETS_TOTAL`/`MAX_CUSTOM_PETS` 镜像常量,admin-web 三处硬编码改引用;S4 整文档读改写末写胜出与双服务并发惰性迁移两条裁量载明 settings/pets.go 顶部注释;S5 Create/Update 删「落库后全表找回」不可达兜底,写前捕获条目直接回显。新增钉测:Go 2 个(pet 端点级 + settings 包级)、packages/api 1 个(常量镜像),三绿回验(packages/api 74 测试)。
