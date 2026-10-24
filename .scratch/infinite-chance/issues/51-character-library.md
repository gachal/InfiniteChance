# 51 角色库:IP 级角色实体、造型变体与设定图锚

Type: feat
Status: drafted(2026-10-24 grilling 定案;上游:50 号票 IP 层;取舍依据 [ADR 0004](../../../docs/adr/0004-ip-container-and-canvas-typing.md) 定案 3 与 [研究纪要](../research/ip-methodology.md);消费面在 52 号票)

## Question

角色圣经(character bible)是六家 IP 工厂共同工件集的核心,其视觉载体是「设定(settei)系统」:三视图模型表、表情集、色指定——业界验证 60 年的角色一致性锚,且在现有 AI 工具中全部缺席(研究纪要 §一/§四/§五)。现状:产品无角色实体,角色资产散落素材库;50 号票落地 IP 层后,角色作为 IP 级实体有了挂靠点。

**用户领域口径(2026-10-24 定案)**:①角色的主要资产是**三视图/多视图**设定画;②**每换一次装即产生成套新视图**——造型变体(outfit variant)是角色资产的一级组织维度,不是可选装饰。

grilling 定案要点:架构取向 A(IP 级实体,否决「角色卡=画布节点」与「角色库=特殊画布」,见 ADR 0004);MVP 只做图像锚(文本注入另票);存储面(本票)先于消费面(52 号票)。

## Answer

**数据模型**(三表,全双方言 EnsureSchema,空起步无存量迁移):

- `characters`:`id` PK / `ip_id` BIGINT UNSIGNED NOT NULL(索引 `idx_characters_ip`)/ `name` VARCHAR(255) NOT NULL / `alias` VARCHAR(255) 可空 / `description` VARCHAR(1024) 可空(一句话)/ `detail` TEXT(详细设定,纯文本)/ `created_at` / `updated_at`。同 IP 内允许重名。
- `character_variants`(造型变体):`id` PK / `character_id` BIGINT UNSIGNED NOT NULL(索引)/ `name` VARCHAR(255) NOT NULL(如「默认」「校服」「战斗形态」)/ `note` VARCHAR(1024) 可空(服装描述,自由文本)/ `sort` INT NOT NULL DEFAULT 0 / `created_at` / `updated_at`。
- `character_images`(设定图,纯引用):`id` PK / `character_id` BIGINT UNSIGNED NOT NULL(索引)/ `variant_id` BIGINT UNSIGNED NOT NULL(索引)/ `asset_id` BIGINT UNSIGNED NOT NULL / `label` VARCHAR(32) 可空 / `sort` INT NOT NULL DEFAULT 0。**label 前端白名单六类:三视图/多视图/表情集/色板/姿态/其他**,后端存字符串不校验枚举(画布类型注册表同哲学);**视图类(三视图/多视图)是主资产**,画廊排序置前。

**变体纪律**:创建角色自动建首个变体(name='默认',可改名)——换装 = 新建变体后成套上传新视图;**最后一个变体不可删**(409 `variant_last_one`,零变体角色没有资产载体);变体缺视图类图时 UI 弱提示「该造型还没有三视图/多视图」,不做硬约束。

**归属与校验**:角色严格限本 IP——创建带 ip_id(= 路径 IP);消费面(52)服务端校验 `character.ip_id == canvas.ip_id`,不符 400 `character_ip_mismatch`。不做「全局角色」。

**API(canvas/server,挂 JWT 会话,错误形状管理面统一)**:

- `POST /api/ips/:id/characters` `{name, alias?, description?, detail?}`(建角色 + 默认变体);`GET /api/ips/:id/characters` 列表——**响应含完整树**(角色 → 变体 → 图,单管理员量级小,画布侧目录一次取全;卡片首图 = 首个变体中排序最前的视图类图,无则任意第一张),排序 updated_at DESC。
- `GET / PATCH / DELETE /api/characters/:cid`;DELETE = 删角色行 + 级联删其变体与图行,**素材行不动**(引用不复制);无拦截条件(52 落地前的引用占位语义在 52 处理)。
- 变体:`POST /api/characters/:cid/variants` `{name, note?}`;`PATCH / DELETE /api/character-variants/:vid`(最后一个拒删)。
- 图:`POST /api/characters/:cid/images` `{variant_id, asset_id, label?, sort?}`(双入口:本机先经 `/api/assets/upload?ip_id=` 入素材库再引用,或素材库选已有图);`PATCH /api/character-images/:imgid`(label/sort/换变体);`DELETE /api/character-images/:imgid`(只删图行)。
- 被引用素材行被删:图行保留,UI 显示「素材不可用」占位、可手动移除(与画布节点同哲学,不做跨包级联清理)。

**UI(IP 详情页新增「角色」区,在画布区与素材区之间)**:

- 角色卡墙(首图 + 名称 + 别名 + 变体/图计数);新建角色对话框(名称必填 + 别名/简介/详细设定)。
- 详情抽屉:字段编辑(name/alias/description/detail,**detail 纯文本 pre-wrap,不解析 markdown**)+ 变体条(名称/服装描述/增删改名/排序)+ 选中变体的设定图画廊(缩略复用 thumb 302、label 徽章、上传/从库选/删除/排序,视图类置前)。
- 排序:角色 updated_at DESC;变体与图按 sort + id。

**生成链图像锚**:AssetPicker(素材挑选器,Agent 附件与生成对话框两模式共用,47 号票泛化组件)加「角色」分组视图——**角色 → 变体两级分组**列设定图,点选进参考条/附件条,与普通素材同款 chip 流;**chip 不带角色/变体标注**(MVP 它就是素材);服务端零改动——设定图引用 = 既有 asset_id + 内容寻址,走既有参考图契约,网关零改动。

**不动**:网关零改动;admin-web 不加角色管理页(底层素材行已可审计);文本注入不在本票(52 号票);detail 的 markdown 渲染、角色搜索、手动排序、变体独立封面均另票。

## 产出面

- `internal/character`(新包,与 asset/canvas 平级):三表 store(双方言 EnsureSchema)+ handler;`internal/wiring` 挂 /api/ips/:id/characters 与 /api/characters 路由。
- `canvas/web`:IP 详情页角色区 + 角色详情抽屉 + AssetPicker 角色分组视图。
- `packages/api`:角色/变体/图 CRUD 请求函数。
- 测试:双方言 schema、CRUD、删角色级联(素材行不动)、最后变体拒删、素材删除后占位、列表树形状、限本 IP。

## 部署侧动作(代码外)

无——全新表空起步,无迁移。

## 验证点

- 建角色自动带「默认」变体;换装流程:新建变体 → 上传成套三视图 → 画廊视图类置前;删最后变体被拒。
- 删角色后素材行仍在素材库可见;删素材行后角色画廊「素材不可用」占位可移除。
- IP 详情页只见本 IP 角色;AssetPicker 按角色 → 变体两级分组,图片模式/视频模式/Agent 附件三处点选进条全链路。
- 跨 IP 不可见;重名允许。
- `make test` + `make lint` + 浏览器点验。

## Out of Scope(52 号票及更远)

- **52 号票**:角色引用节点(通用节点,画布消费)+ 角色设定文本注入 Agent;注入 MVP 只带角色级字段(name/alias/description/detail),**变体名与服装描述的注入后续票**。
- 更远不占号:detail 的 markdown 渲染、角色搜索与手动排序、变体独立封面、跨 IP 角色引用、逐角色微调模型(Scenario 路线)。
