# SQLite 双方言:平行 store 实现 + modernc 纯 Go 驱动

桌面版(desktop/,Wails 壳)需要单文件、零运维的存储,而服务器部署继续用 MySQL;
两边共享 `internal/` 的同一套领域逻辑。决定:每个 store 包加 `store_sqlite.go`
平行实现既有 store 接口(接口测试双方言共用),桌面装配选 SQLite、服务器装配不动;
驱动用 `modernc.org/sqlite`(纯 Go,免 cgo),保证三平台交叉构建链干净。

## Considered Options

- **运行时方言分支**(单文件内按驱动切 SQL):文件少但两种 SQL 混杂难读,维护易漂移。
- **打包 MySQL/Redis 进桌面 App**:违背桌面化初衷,分发与升级都不可行。
- **cgo 驱动 mattn/go-sqlite3**:Windows 交叉编译需要 mingw 工具链,得不偿失;
  单用户单写者场景性能差异无关紧要。

## Consequences

- schema 演进要双处同步:MySQL 的 DDL 在各 store 的 `EnsureSchema`,SQLite 在
  对应 `store_sqlite.go`;改表结构时两边一起改。
- MySQL 特有语法逐处换写:`ON DUPLICATE KEY UPDATE` → `ON CONFLICT … DO UPDATE`;
  错误归一(`MySQLError 1062` → SQLITE_CONSTRAINT)按驱动分别判定。
- 桌面版数据落 OS 应用数据目录的单个 `app.db`,Redis 依赖在桌面装配中移除
  (Redis 本就只承担健康检查 ping,无业务逻辑)。
