# migrations 只留底不执行:EnsureSchema 保持唯一运行时

全库 schema 的唯一出处散在各 store 的 DDL 常量里,没有一处文件能看到全量结构。决定:建 `migrations/` 目录做**可读档案**——`database.sql` / `database.sqlite.sql` 两份基线(切换时从临时库跑齐 EnsureSchema 后逐表导出,此后冻结)+ 每次变更加一对 `yyMMdd_英文功能描述.sql` / `.sqlite.sql` 增量;**文件从不执行**,运行时机制保持各 store 的 EnsureSchema 双方言自愈不变,无 runner、无记账表。防文档烂掉靠形状对账测试:临时库上「基线+增量链」终形与「全部 EnsureSchema」终形逐表比对名集合,双写漏写当场红灯(82 号票)。

## Considered Options

- **启动 runner 执行文件 + schema_migrations 记账**(业界标准形态):文件成执行权威后要背负 MySQL 无 `ADD COLUMN IF NOT EXISTS` 的幂等难题、DDL 非事务的半成品恢复、双入口并发加锁;与 EnsureSchema 双活时启动顺序敏感(runner 必先,否则重复加列炸启动)。单用户自用场景复杂度倒挂。
- **migrations 取代 EnsureSchema**(存量库借旧机制收编后退役 DDL 常量):一张票背 17 个 store 族大扫除,风险集中;新域(Lore)测试装库要换 runner 基建。
- **双轨·全量双写·纯留底 + 对账测试**(采纳):零运行时风险,文档真话由测试机械保证,文件链保持可执行为未来翻面留门。

## Consequences

- 每个 schema 变更 4 处同步:增量文件对 + EnsureSchema 双方言(DDL 常量与迁移列表);漏写由对账测试兜。
- 基线冻结,等价性由对账测试持续验证;三处历史非 DDL 迁移(expires_at 改型、auth 去 AUTO_INCREMENT、ips 自愈)不回填,历史演进看 git。
- 增量文件虽不执行,按可执行标准写(测试真跑);SQLite「列先于引用它的索引」在文件内与文件序上继续生效。
- 未来把文件接上 runner 是另一张票;本 ADR 挡住「给留底文件接运行时」的第一反应。
