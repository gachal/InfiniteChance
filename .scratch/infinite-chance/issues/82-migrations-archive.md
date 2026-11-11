# 82 全库 Schema 文件化留底:migrations 基线对 + 增量对 + 形状对账(纯留底不执行)

Type: feat(工程基建)
Status: 已实现(2026-11-11 grill-with-docs 三轮,Q1–Q8 全裁;同日 implement-spec 落 feat/82-migrations-archive——`migrations/` 基线两份冻结(MySQL `SHOW CREATE TABLE` 归一 / SQLite sqlite_master 原文,双方言 22 表)+ `internal/migrations` 形状对账(文件链真实执行 vs 全部 EnsureSchema 逐表比名集合,MySQL 实连 3307 侧 22 表/217 列名互证、可 skip,SQLite 临时文件库永不 skip;切换提交零增量);评审修复轮收 4 条(执行原语收进测试文件编译期锁死纯留底 / 17-store 装配清单共享 helper / Split 注释名实相符 / 测试名核实);取舍落 [ADR 0008](../../../docs/adr/0008-migrations-archive-not-runtime.md);遗留 = 首个真实增量随 [04 号 Lore 实体票](../../ip-capabilities/issues/04-lore-entity-model.md)加表出现,走全量双写纪律)

## Question

全库 schema 的唯一权威出处散在 16 个 store 包的 DDL 字符串常量里(22 张表、双方言 34 个 EnsureSchema 方法),没有任何一处文件能看到全量结构——「SQL 数据结构不清楚」痛点的根源。本票定文件化 migrations 机制:它与 EnsureSchema 什么关系、基线几份、如何防文档烂掉。

## 事实底座(2026-11-11 研究纪要,落 `research/schema-inventory` 分支)

- 全库 22 张表 / 16 包 / 34 个 EnsureSchema;唯一显式顺序依赖 canvases → assets → ips(ips 自愈迁移读 `canvases.ip_id`)。
- 迁移形态不止加列:两处 MySQL 形状迁移(apikey expires_at 改 DATETIME、auth 去 AUTO_INCREMENT)+ 一处双方言数据自愈(ips 默认 IP 收编)。
- SQLite 侧 6 列只活在建表 DDL、无迁移条目(assets 4 列、canvas_tasks 2 列;桌面库诞生即含)——两侧迁移清单不对称。
- 方言差异 11 类(自增主键/时间列/JSON/大文本/布尔/引擎子句/约束落点/索引落点/ON UPDATE 语义/形状迁移不对称/索引长度)。
- 全库零 SQL 外键,引用完整性全在应用层;SQLite「列迁移先于引用该列的索引」是执行序硬约束(asset 包是正确范例)。
- MySQL 8.4 不支持 `ADD COLUMN IF NOT EXISTS` / `CREATE INDEX IF NOT EXISTS`(MariaDB 才有)——纯 SQL 文件在 MySQL 侧做不到逐语句幂等。

## 定案(2026-11-11)

1. **关系口径 = 双轨·全量双写·纯留底**:EnsureSchema 保持唯一运行时机制,双方言现状一字不动;`migrations/` 目录是 schema 的可读档案,**文件从不执行**——无 runner、无 `schema_migrations` 记账表、无启动判定树、无启动顺序约束(runner 时代的桌面启动备份阀一并砍)。「存量库升级」命题随之消解:文件不执行,存量库继续 EnsureSchema 自愈,无需任何标记。
2. **基线两份,切换时冻结**:切换提交一次性导出 `migrations/database.sql`(MySQL 权威)与 `migrations/database.sqlite.sql`;导出法 = 临时空库跑齐全部 EnsureSchema 后逐表 `SHOW CREATE TABLE` 归一(与运行时真形同源,拒绝手工翻译);SQLite 侧 6 列不对称保留,不从 MySQL 清单机械照搬。基线此后**冻结不改**,未来结构 = 基线 + 增量链可读推导。
3. **增量成对**:此后每次 schema 变更加 `yyMMdd_英文功能描述.sql` + `yyMMdd_英文功能描述.sqlite.sql`(命名照钉;两侧语句逐字节相同时也写两份,不做单文件回落);切换提交本身零增量,首个增量随下一次变更出现。三处历史非 DDL 迁移**不回填成文件**——基线即收敛形状,历史演进看 git。
4. **全量双写纪律**:每个 schema 变更写 4 处 = 增量文件对(2)+ EnsureSchema 双方言(DDL 常量 + 迁移列表)。SQLite 侧增量文件内与文件序上继续遵守「列先于引用它的索引」;文件链在对账测试里真实执行,排序错误当场炸——纪律从口口相传变成可暴露。
5. **形状对账(go test)**:测试建临时库,一侧执行「基线 + 全部增量文件」,另一侧执行全部 EnsureSchema,逐表比对**表/列/索引名集合**(不比类型——11 类方言差异是设计内差异);MySQL 走测试 DSN(宿主机 3307),无 infra 自动 skip 与现有 `*_mysql_test.go` 同语义;**SQLite 用临时文件库,永不 skip**。双写漏写任一侧 = `make test` 红;测试真跑文件链,使留底保持可执行性——为未来翻面(文件接 runner)留门,但不预支复杂度。
6. **文档回写**:CONTEXT.md 工程术语增「迁移留底」词条;ADR 0008 记录取舍;[04 号 Lore 实体票](../../ip-capabilities/issues/04-lore-entity-model.md)解锁——加表加列按本票双写纪律走。

## Out of Scope

- runner / 记账表 / 文件执行(未来翻面另票;ADR 0008 挡住「给留底接运行时」的第一反应)。
- EnsureSchema 代码清理/退役(纯留底下它就是运行时,永不退役)。
- 桌面 SQLite 与 Docker 栈数据互通迁移(ADR 0001 既定不做)。
- 历史迁移回填成文件(看 git)。

## 验证点

1. 形状对账测试绿:MySQL 侧连 3307 跑通(无 infra 时 skip 语义正确);SQLite 侧 `make test` 必跑。
2. 两份基线与真实库形状一致:对账测试双通道互证(文件链终形 ≡ EnsureSchema 终形)。
3. 首个真实变更(预计 04 号票 Lore 加表)走双写纪律,对账测试能捕捉漏写。
