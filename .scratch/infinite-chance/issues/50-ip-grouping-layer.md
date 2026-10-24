# 50 IP 企划分组层:第一层 IP,画布类型化

Type: feat
Status: implemented(2026-10-24;feat/ip-platform,见记忆)。**注记(2026-10-24)**:本票「画布类型 + 特色节点」结构与对 53-55 的预切口径,已被 53 号票 grilling 推翻——画布回归通用、节点全画布可用、组织下沉到节点分组,见 [ADR 0005](../../../docs/adr/0005-universal-canvas-and-node-grouping.md);ip_id/素材 IP 维度/默认 IP 收编等其余定案不受影响。票面正文保留历史原貌,不随推翻改写。

## Question

产品定位重塑(2026-10-24):以打造 ACG IP 为主线——进入应用第一层应为 IP 分组,下挂多种类型画布(动画[当前形态]/游戏/漫画/通用),每种画布有通用节点与特色节点。现状障碍:

- 无任何分组实体:canvases 只有 id/name/graph/version,第一层是平铺的画布列表页 CanvasListView.vue;
- 画布无类型字段,媒介语义(动画/游戏/漫画)无处安放;
- 素材无 IP 维度,IP 资产散落全局;
- 节点类型清单无「适用画布类型」概念,特色节点无处挂靠。

业界依据(研究纪要):六家 IP 工厂(迪士尼/漫威/任天堂/宝可梦/集英社/吉卜力)共同工件集;World Anvil/Campfire/Plottr/Obsidian 收敛出的标准 IA「容器 → 类型化实体库 + 视图 + 资产库」;AI 工具(LTX/Sudowrite/Scenario 等)全部以项目为第一层,而「设定图锚」(三视图/色指定/表情集)这一动画工业验证 60 年的一致性机制在 AI 工具中全部缺席——差异化空档。

grilling 两轮定案要点(2026-10-24):两层结构不引入「作品」中间层;画布必归属 IP(ip_id NOT NULL);存量收编 `animation`;类型注册表驱动、后端不校验枚举;角色库 = IP 级实体(51 号票存储面,52 号票消费面);素材 ip_id 传导、0 = 全局;技能不分组、admin-web 不加 IP 管理页;其余业界工件(世界观圣经/风格指南/大纲/时间线/关系图)显式推迟。

## Answer

**数据模型**:

- 新表 `ips`(双方言):`id` BIGINT UNSIGNED AUTO_INCREMENT PK / `name` VARCHAR(255) NOT NULL / `description` VARCHAR(1024)(可空,一句话 logline)/ `cover_asset_id` BIGINT UNSIGNED NOT NULL DEFAULT 0(引用素材行;0 = 无封面)/ `created_at` / `updated_at`。封面素材行被删后卡片回落「渐变 + 首字」占位(节点-素材引用同哲学),无对象回收问题。
- `canvases` 双方言加列 `ip_id BIGINT UNSIGNED NOT NULL DEFAULT 0` 与 `type VARCHAR(32) NOT NULL DEFAULT 'animation'`(存量收编口径:本票定案「当前的画布就是动画画布」;新建请求缺省 type 落 `general`)。
- `assets` 双方言加列 `ip_id BIGINT UNSIGNED NOT NULL DEFAULT 0`(0 = 全局/未归属),加普通索引 `idx_assets_ip (ip_id, id)`——**先加列后建索引**(48 号票 SQLite 教训)。

**启动自愈迁移**(canvas/server 的 EnsureSchema 尾部,幂等):建 ips 表 → 两处加列 → 仅当「存在 ip_id=0 的画布行**且** ips 表为空」时插入「默认 IP」(name='默认 IP')并把全部 ip_id=0 画布收编到它;新库(无画布)不预建 IP。gateway 不建 ips 表(中转面无 IP 概念,relay 行 ip_id 恒 0);桌面版 SQLite 同一迁移路径。

**API(canvas/server,挂 JWT 会话)**:

- `GET /api/ips`:列表(带 canvas_count;排序键 = ips.updated_at 与名下画布 max(updated_at) 取大者,新到旧)。
- `POST /api/ips` `{name, description?, cover_asset_id?}`;`PATCH /api/ips/:id`(部分更新,cover_asset_id=0 = 清除封面);`GET /api/ips/:id`;`DELETE /api/ips/:id`:名下有画布**或**有直接归属素材(assets.ip_id = 该 IP)→ 409 `ip_not_empty`(管理面错误形状),否则删行;不级联、不归档。
- `POST /canvases` 增必填 `ip_id`(服务端校验 IP 存在,不存在 400 `ip_not_found`)与 `type`(可缺省,缺省 `general`;不校验枚举值照存);`GET /canvases` 加 `ip_id` 过滤参数,summary 响应加 ip_id/ip_name/type;`GET /canvases/:id` 响应同加(ip_name 供编辑器面包屑);`PATCH /canvases/:id` 支持 `type` 与 `ip_id`(= 画布移动)。
- **画布移动语义**:PATCH ip_id 只改画布归属,该画布名下**历史素材行 ip_id 不回填**(历史产物留在原 IP 名下、过滤仍可见,历史行不清洗原则);此后新产物传导新 IP。

**素材 IP 维度**:

- 归属传导:canvastask worker 落素材行时按任务行 canvas_id 查画布取 ip_id(画布已删落 0,不阻塞);relay persist 直连行 ip_id 恒 0。
- 上传:`POST /api/assets/upload` 加可选 `ip_id`(服务端仅校验存在性);画布内上传前端固定传画布的 ip_id(不可改),IP 详情页素材区上传传该 IP;**不提供主动上传为全局的入口**,ip_id=0 只由直连产物与历史行自然形成。
- 列表过滤:`GET /assets` 加 `scope` 参数:`ip`(须带 ip_id,语义 = `ip_id IN (X, 0)` 即本 IP + 全局)/ `global`(仅 ip_id=0)/ `all`(不过滤 = 现状缺省);未知 scope 400。画布侧素材面板与 AssetPicker 默认 scope=ip、可切「全部」;admin 素材页(AssetsView)加 IP 列与过滤下拉(列表响应加 ip_id/ip_name,join ips,0 显示「全局」)。跨 IP 复用照旧引用不复制(48 号票哲学)。

**前端(canvas/web)**:

- 类型注册表 `src/canvasTypes.ts`:`{ id, label, description, icon }`,四类先行 `animation`(动画)/ `game`(游戏)/ `manga`(漫画)/ `general`(通用);`graph.ts` 节点类型清单加「适用画布类型」声明与过滤函数(本票全部现有节点 = 通用,全类型可见、行为暂无差异——机制就位,特色节点 53-55 号票挂靠)。
- 路由三层化:`/` → IpListView(IP 卡片墙:封面[复用 thumb 302]+ 名称 + 简介 + 画布数 + 最近更新;空封面渐变 + 首字;新建/编辑 IP 对话框:名称 + 简介 + 封面[素材库选或本机上传,上传带该 ip_id]);`/ip/:id` → IpDetailView(头部信息 + 编辑/删除;「新建画布」对话框 = 四张类型卡单选 + 名称输入,提交 POST 带 ip_id + type 进编辑器;画布区按类型分组展示、空类型不显示,行内操作:打开/重命名/移动到其他 IP/删除;素材区复用素材面板组件 scope=ip);`/canvas/:id` 编辑器不变。CanvasListView.vue 改造拆分为上述两视图。
- 编辑器顶栏加面包屑「IP 名 / 画布名」+ 类型徽章(数据来自 GET /canvases/:id 新增字段),点 IP 名回详情页。
- 素材面板与 AssetPicker 加 scope 切换,默认「本 IP + 全局」。

**不动**:技能(prompt_templates)不分组;admin-web 不加 IP 管理页(IP 增删改全在创作侧);生成记录面板不加 IP 维度(仍按画布);网关零改动;桌面版主窗默认 URL 是 canvas/web 根路径,新首屏自然生效。

## 产出面

- `internal/canvas`:ips store(双方言 EnsureSchema + CRUD)与 handler;canvases 加列/自愈迁移/ip_id 过滤/summary 扩展;`internal/wiring` 挂 /api/ips 路由。
- `internal/asset`:加列 + 索引、upload ip_id、scope 过滤、列表响应 ip_name;`internal/canvastask`:worker 落行传导 ip_id。
- `canvas/web`:canvasTypes.ts、IpListView/IpDetailView、路由与守卫、新建画布类型卡对话框、编辑器面包屑、节点类型适用声明、素材面板/挑选器 scope。
- `admin-web`:AssetsView 加 IP 列与过滤;`packages/api`:IP CRUD 与扩展参数的请求函数。
- `README.md`:定位句与「创作画布」段随实现更新(文案对齐新入口,定案不先行改)。
- 测试:双方言 schema 与迁移(存量收编/新库不预建/幂等重跑/改名后不重复建)、IP CRUD 与删除拦截两条件、画布必带 ip_id、type 透传回显、移动语义(历史素材不回填)、scope 三态、传导(产物随画布 IP、画布已删落 0)。

## 部署侧动作(代码外)

无——迁移启动自愈。唯一留意:升级后首屏变为 IP 卡片墙,存量画布在「默认 IP」名下(类型 animation),名称可改。

## 验证点

- 存量库启动:默认 IP 出现、全部画布归入且 type=animation;重启不重复建(默认 IP 改名后亦然);新库启动:空卡片墙、无预建 IP。
- IP 删除:有画布拒、有直接素材拒、空则删;封面素材被删后卡片回落占位不报错。
- 画布移动:新产物 ip_id 随新 IP,旧产物留在原 IP;素材面板默认视图随之正确。
- 上传:画布内上传素材行 ip_id = 画布 IP;IP 详情页上传 = 该 IP;直连 relay 行 = 0。
- scope=ip 恰好 = 本 IP + 全局两段;admin 素材页 IP 列显示与过滤正确。
- 类型卡新建四类画布各一,type 原样回显;PATCH 改 type 生效;未知 type 字符串照存不拒。
- 浏览器点验三层路由/卡片墙/新建对话框/面包屑/面板默认过滤;`make test` + `make lint` + 桌面版冒烟。

## Out of Scope(另票)

- **51 号票**:角色库——IP 级实体(名称必/别名/一句话简介/详细设定长文本)+ 设定图集(引用素材行,类型标签:三视图/表情集/色板/姿态/其他)+ 生成链图像锚(挑选器按角色分组、一键进参考条,网关零改动)。
- **52 号票**:角色消费面——角色引用节点(通用节点,持角色 id 渲染,不复制数据)+ 角色设定文本注入 Agent(Sudowrite 式,动 promptgen)。
- **53-55 号票**:动画分镜(絵コンテ:每 cut 画面+时长+台词+运镜注记)/ 漫画分镜页(ネーム:格子网格+每格图与台词)/ 游戏关卡实体卡特色节点,逐票 grilling。
- 更远不占号:世界观圣经、美术风格指南、大纲、时间线/关系图视图、逐角色微调模型、IP 封面自动生成、IP 搜索/手动排序。
