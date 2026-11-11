# 02 migrations 机制定案

Label: wayfinder:grilling
Status: closed(2026-11-11 定案,落主项目 [82 号定案票](../../infinite-chance/issues/82-migrations-archive.md) + [ADR 0008](../../../docs/adr/0008-migrations-archive-not-runtime.md))
Assignee: (未认领)
Blocked-by: [01 全库 Schema 全景调查](01-schema-inventory.md)

## Question

文件化迁移机制如何与现有 EnsureSchema 共存。已钉死:建 `migrations/` 目录、统一基线 `database.sql`、每次变更新增增量脚本、命名 `yyMMdd_英文功能描述.sql`。待定:

1. **关系口径**:migrations 取代 EnsureSchema(MySQL 侧全量)?双轨并存?EnsureSchema 退役只留 migrations?桌面 SQLite 侧是否同款处理?
2. **基线方言**:`database.sql` 一份(MySQL 权威)还是两份(双方言各一)?
3. **执行与记账**:启动 runner 自动应用 + `schema_migrations` 记账表?还是校验脚本手工跑?增量脚本本身要不要幂等?
4. **存量对账**:现有库已由 EnsureSchema 建好,基线如何标记「已应用」,避免存量库重复执行?
5. A/B 两域的加表加列从此走 migrations——机制定了才切(04 号票依赖本票)。

## Resolution(2026-11-11)

四问全裁 + 三问衍生(Q5–Q8),定案票 = 主项目 82 号:

- **Q1 关系口径** → 双轨·全量双写(Q6 补裁):每个变更 4 处同步(增量文件对 + EnsureSchema 双方言 DDL 常量与迁移列表)。
- **Q2 基线方言** → 两份(`database.sql` + `database.sqlite.sql`),切换时一次性导出、此后冻结。
- **Q3 执行与记账 / Q4 存量对账** → 随 Q7 反悔原推荐:**纯留底**——文件从不执行,无 runner 无记账表无判定树,EnsureSchema 保持唯一运行时,存量对账命题消解。
- **Q5 增量命名** → 成对两文件(`yyMMdd_英文功能描述.sql` + `.sqlite.sql`),不做单文件回落。
- **Q8 防烂** → go test 形状对账:「基线+增量链」终形 vs「全部 EnsureSchema」终形逐表比表/列/索引名集合;MySQL 3307 无 infra 自动 skip,SQLite 临时文件库永不 skip;双写漏写 = `make test` 红。

04 号 Lore 实体票解锁。
