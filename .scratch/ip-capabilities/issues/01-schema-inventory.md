# 01 全库 Schema 全景与 EnsureSchema 装配调查

Label: wayfinder:research
Status: closed(2026-11-11,研究子代理交付,纪要落 `research/schema-inventory` 分支)
Assignee: 本图会话(子代理已交付)

## Question

用户痛点:「目前 SQL 的数据结构我不清楚」。全仓 schema 散在各 store 包的 `EnsureSchema` 双方言实现里,没有一处能看到全量结构。本票产出四份事实,给 02 号票(migrations 机制定案)供料:

1. **全表清单**:每张表一行——表名、所属 store 包、关键列(标注双方言差异点:类型/自增/默认值)、索引、表间逻辑外键语义。
2. **EnsureSchema 运行机制**:谁在启动时调用、调用顺序、幂等手法(CREATE TABLE IF NOT EXISTS / 加列探测)、SQLite 加列与索引顺序的坑(48 号票教训:加列迁移须先于索引建)。
3. **桌面 SQLite 路径**:desktop 装配如何建库、与 Docker 栈的差异面。
4. **基线建议输入**:`database.sql` 基线按哪个方言写、双方言如何共存于一套基线,只列事实与选项,不拍板(拍板是 02 号票的事)。

产物:`research/schema-inventory.md`(落 `research/schema-inventory` 分支),02 号票 grilling 时必读。

## Resolution(2026-11-11)

纪要:`.scratch/ip-capabilities/research/schema-inventory.md` @ `research/schema-inventory` 分支(commit `b936e88`,基于 main `1c9169d`,已推 origin)。阅读方式:`git show research/schema-inventory:.scratch/ip-capabilities/research/schema-inventory.md`。

**规模**:22 张表、16 个 store 包(17 个 EnsureSchema 实现族,MySQL+SQLite 双方言 34 个方法,合计 44 条 CREATE TABLE)。

给 02 号票的四条硬事实:

1. **迁移形态比「加列」丰富**:除 `CREATE TABLE IF NOT EXISTS` + 逐列探测外,还有两处 MySQL 列形状迁移(`api_keys.expires_at` 改 DATETIME 解 2038、`admin_accounts` 去 AUTO_INCREMENT 固定 id=1)和一处双方言启动数据自愈迁移(ips 默认 IP 收编,依赖 canvases→assets→ips 装配顺序)——迁移执行器必须能表达这三种非 ADD COLUMN 形态。
2. **两侧迁移清单不对称**:SQLite 侧 6 列(assets 的 object_key/content_type/size_bytes/last_used_at、canvas_tasks 的 seconds/image_ref)只活在建表 DDL、无迁移条目——SQLite 基线可直接拼建表 DDL,MySQL 基线须取「建表+迁移链收敛后的最终形状」,不能机械照搬。
3. **全库零 SQL 外键**:`FOREIGN KEY|REFERENCES` 在 internal/ 零命中,SQLite `foreign_keys(ON)` pragma 实际不约束任何表,引用完整性全在应用层。
4. **排序不变量**:「SQLite 列迁移先于引用该列的索引」(asset 是正确范例)目前只靠注释口口相传,是迁移执行器要继承的硬约束。
