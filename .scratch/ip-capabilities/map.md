# Wayfinder Map: IP 能力下一阶段(世界观实体库 + 一致性深化 + migrations 基建)

Label: wayfinder:map

## Destination

一套可直接开工的决策票集(定案票落 `.scratch/infinite-chance/issues/`,82 号起):**migrations 基建**、**A 世界观实体库(Lore)**、**B 一致性深化**三域各有已定案票,定案后无需再决策即可逐票进 implement-spec。

## Notes

- 领域:InfiniteChance 无限画布的 IP 打造能力层;产品形态单用户自用,画布前端为主战场,网关侧仅随能力需要加渠道/adaptor。
- 已钉死前提(2026-11-11 grilling 确定):
  - **目的地形状 = 可开工决策票集**(非 spec 大文档);C 风格指南大文档 / D 大纲层 / E 时间线 / F 关系图排在 A/B 之后,暂入雾。
  - **方向 = A + B 打头**:A 是角色库已验证模式的横向复制(方法论依据见[研究纪要](../infinite-chance/research/ip-methodology.md):业界 AI 工具全部缺席「设定集中间件」,传统动画工业验证 60 年);B 直接强化日用最痛的 on-model 一致性。
  - **migrations 机制已定案(2026-11-11)**:建 `migrations/` 目录,统一基线 `database.sql` + 每次变更新增增量脚本,命名 `yyMMdd_英文功能描述.sql`。机制 = **纯留底**——文件从不执行,EnsureSchema 保持唯一运行时,全量双写纪律由形状对账测试兜底;定案票 = [82 号](../infinite-chance/issues/82-migrations-archive.md),取舍 ADR 0008。现状 = 全仓无任何文件迁移机制,schema 全靠各 store 的 `EnsureSchema` 双方言自愈(散在 10+ 个 store 包,这是「目前 SQL 数据结构不清楚」痛点的根源)。
  - 范围边界维持(2026-11-11 确认):单用户自用、双方言 schema(ADR 0001)、桌面 SQLite 与 Docker 栈数据互不通用不做迁移。
- 技能:每会话咨询 /grilling、/domain-modeling(术语回写 CONTEXT.md);研究票子代理调 /research。
- Tracker:local-markdown——地图为本文件,过程票在 `issues/`,研究产物在 `research/`(落 `research/<名>` 分支);认领 = 把票的 Assignee 改成自己,阻塞关系写在票头 `Blocked-by:`。
- 后续定案票落主项目票目录 `.scratch/infinite-chance/issues/`(82 号起),本目录 `issues/` 只放本图的过程票,两套编号互不相干。

## Decisions so far

- [01 全库 Schema 全景与 EnsureSchema 装配调查](issues/01-schema-inventory.md)(2026-11-11 关闭):22 张表/16 包/双方言 44 条 CREATE TABLE 全清单落 `research/schema-inventory` 分支;关键事实——迁移形态含列形状改写与启动数据自愈(不止加列)、MySQL 基线须取迁移链收敛后形状、全库零 SQL 外键引用完整性全在应用层、SQLite 列迁移先于索引是执行器硬约束。
- [03 角色库与消费链全景调查](issues/03-character-library-survey.md)(2026-11-11 关闭):纪要落 `research/character-library-survey` 分支;关键事实——65 号票 scene/prop 已是「角色库模式砍变体层」的复制先例(Lore 存储面照 scene 包改)、注入链三层截断口径互不通用(聊天 50 万/生图 2000/风格 500)、物化契约实体无关可换芯、但消费面(引用节点/连线矩阵/cut 标注/Agent chip)实体版整体缺位——05 号票独立成票被坐实。
- [02 migrations 机制定案](issues/02-migrations-mechanism.md)(2026-11-11 关闭):纯留底定案,定案票 = [82 号](../infinite-chance/issues/82-migrations-archive.md)——`migrations/` 只落文件(基线两份冻结 + 增量成对),EnsureSchema 保持唯一运行时,无 runner 无记账;全量双写纪律(每变更 4 处:增量对 + EnsureSchema 双方言)由 go test 形状对账兜底(SQLite 侧永不 skip);取舍落 ADR 0008,**04 号 Lore 实体票解锁**。
- [06 一致性深化定案](issues/06-consistency-deepening.md)(2026-11-11 关闭):三项全做、按项拆票、锁现有表边界(不预设 Lore 形状,需要动实体模型的子项移交 04);定案票 = [83 服装变体](../infinite-chance/issues/83-outfit-variants.md)(cut→变体绑定 + 成套生成)/ [84 色指定](../infinite-chance/issues/84-color-spec.md)(变体配色字段,migrations 纪律首次增量 + 光照色板第六类型)/ [85 群像同框便利层](../infinite-chance/issues/85-group-shot.md)(多选拉取 + 图片模式预设 + 提示泛化);实现顺序 83→84→85。三票已实现(2026-11-12,feat/consistency-deepening 并 main 8da7eab,含评审修复轮;84 号票完成 migrations 纪律首个增量实战)。

## Not yet specified

- C/D/E/F(风格指南大文档/大纲层/时间线/关系图):等 Lore 实体库定案后重估——时间线与关系图的节点来自实体,实体模型定了才切票。
- migrations 落地后的实施细节(implement-spec 产物,不属本图决策范围)。
- Lore 实体与 65 号票场景/道具素材行的锚定关系(实体↔素材),等实体模型定案时一并浮出。

## Out of scope

- 多人协作/实时协同/多租户/授权分发——单用户自用形态已排除(2026-11-11 确认维持)。
- 桌面 SQLite 与 Docker MySQL 数据互通迁移——ADR 0001 既定不做。
