package migrations_test

import (
	"context"
	"database/sql"
	"os"
	"sort"
	"testing"
	"time"

	mysqldriver "github.com/go-sql-driver/mysql"

	"github.com/gachal/InfiniteChance/internal/apikey"
	"github.com/gachal/InfiniteChance/internal/asset"
	"github.com/gachal/InfiniteChance/internal/auth"
	"github.com/gachal/InfiniteChance/internal/canvas"
	"github.com/gachal/InfiniteChance/internal/canvastask"
	"github.com/gachal/InfiniteChance/internal/channel"
	"github.com/gachal/InfiniteChance/internal/character"
	"github.com/gachal/InfiniteChance/internal/migrations"
	"github.com/gachal/InfiniteChance/internal/musictask"
	"github.com/gachal/InfiniteChance/internal/pricing"
	"github.com/gachal/InfiniteChance/internal/prompttemplate"
	"github.com/gachal/InfiniteChance/internal/prop"
	"github.com/gachal/InfiniteChance/internal/scene"
	"github.com/gachal/InfiniteChance/internal/settings"
	"github.com/gachal/InfiniteChance/internal/usage"
	"github.com/gachal/InfiniteChance/internal/videotask"
	"github.com/gachal/InfiniteChance/internal/voice"
)

// openMigrationsMySQL 按 internal/settings/store_mysql_test.go 的 MYSQL_TEST_DSN
// 约定连上测试 MySQL:默认连 compose 宿主机 3307 的 infinitechance_test,
// 本包独占 <db>_migrations 前缀的库;不可达时 t.Skip —— 语义与现有
// *_mysql_test.go 完全一致(无基础设施保持 go test ./... 绿)。
func openMigrationsMySQL(t *testing.T, suffix string) *sql.DB {
	t.Helper()
	dsn := os.Getenv("MYSQL_TEST_DSN")
	if dsn == "" {
		dsn = "root:infinitechance@tcp(localhost:3307)/infinitechance_test?parseTime=true"
	}
	cfg, err := mysqldriver.ParseDSN(dsn)
	if err != nil {
		t.Fatalf("parse MYSQL_TEST_DSN: %v", err)
	}
	// 每个测试包独占一个库;本包对账要两份已知干净形状(chain / ensure),
	// 在 <db>_migrations 下再分两库,开测即重建,不留跨次运行残留。
	dbName := cfg.DBName + "_migrations_" + suffix
	cfg.DBName = ""
	server, err := sql.Open("mysql", cfg.FormatDSN())
	if err != nil {
		t.Fatalf("open mysql server: %v", err)
	}
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := server.PingContext(ctx); err != nil {
		t.Skipf("mysql unreachable, skipping store tests: %v", err)
	}
	if _, err := server.ExecContext(ctx, "DROP DATABASE IF EXISTS `"+dbName+"`"); err != nil {
		t.Fatalf("drop test database: %v", err)
	}
	if _, err := server.ExecContext(ctx, "CREATE DATABASE `"+dbName+"`"); err != nil {
		t.Fatalf("create test database: %v", err)
	}
	t.Cleanup(func() {
		server2, err := sql.Open("mysql", cfg.FormatDSN())
		if err != nil {
			return
		}
		defer server2.Close()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, _ = server2.ExecContext(ctx, "DROP DATABASE IF EXISTS `"+dbName+"`")
	})
	cfg.DBName = dbName
	db, err := sql.Open("mysql", cfg.FormatDSN())
	if err != nil {
		t.Fatalf("open test database: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

// ensureMySQLSchema 按 desktop/runtime.go 的 17 store 装配顺序跑全部 MySQL 侧
// EnsureSchema(与 reconcile_sqlite_test.go 的 ensureSQLiteSchema、desktop/runtime.go
// 三方同步 —— 唯一显式顺序依赖 canvases → assets → ips)。
func ensureMySQLSchema(ctx context.Context, db *sql.DB) error {
	assets := asset.NewMySQLStore(db)
	stores := []interface {
		EnsureSchema(ctx context.Context) error
	}{
		auth.NewMySQLStore(db),
		channel.NewMySQLStore(db),
		apikey.NewMySQLStore(db),
		pricing.NewMySQLStore(db),
		usage.NewMySQLStore(db),
		videotask.NewMySQLStore(db),
		musictask.NewMySQLStore(db),
		prompttemplate.NewMySQLStore(db),
		settings.NewMySQLStore(db),
		canvas.NewMySQLStore(db),
		assets,
		canvas.NewMySQLIPStore(db),
		character.NewMySQLStore(db),
		scene.NewMySQLStore(db),
		prop.NewMySQLStore(db),
		voice.NewMySQLStore(db),
		canvastask.NewMySQLStore(db, assets),
	}
	for _, s := range stores {
		if err := s.EnsureSchema(ctx); err != nil {
			return err
		}
	}
	return nil
}

// TestReconcileMySQL 形状对账(MySQL 侧):一侧执行「基线 + 全部增量」文件链,
// 一侧跑全部 EnsureSchema,逐表比对表名集合、列名集合(information_schema.COLUMNS)、
// 索引名集合(information_schema.STATISTICS 按 INDEX_NAME 去重)。只比名集合
// 不比类型 —— 11 类方言差异是设计内差异。
//
// 索引比对口径:MySQL 侧排除 PRIMARY。SQLite 侧 INTEGER 主键是 rowid 别名、
// 无独立索引条目,TEXT 主键与内联 UNIQUE 约束对应 sqlite_autoindex_*(见
// reconcile_sqlite_test.go 的实证注释)—— PRIMARY / sqlite_autoindex_* 是
// 同一约束在两侧的不同形态,所以 MySQL 侧排除 PRIMARY 后,具名索引(idx_* /
// uniq_* / uk_*)与 SQLite index_list 名集合逐一对应(voices 的
// uk_voices_voice_id 两侧同名;admin_accounts/api_keys 的 uniq_* 在 SQLite
// 侧是内联约束,由同方言对账兜住)。
func TestReconcileMySQL(t *testing.T) {
	ctx := context.Background()
	chain, err := migrations.Load(migrateDir)
	if err != nil {
		t.Fatalf("load migrations: %v", err)
	}

	// 库 A:基线 + 全部增量,真实执行。
	dbA := openMigrationsMySQL(t, "chain")
	if err := migrations.ApplyMySQL(ctx, dbA, chain); err != nil {
		t.Fatalf("apply chain: %v", err)
	}
	// 库 B:全部 EnsureSchema。
	dbB := openMigrationsMySQL(t, "ensure")
	if err := ensureMySQLSchema(ctx, dbB); err != nil {
		t.Fatalf("ensure schema: %v", err)
	}

	tablesA := mysqlTables(t, dbA)
	tablesB := mysqlTables(t, dbB)
	if missing, extra := diffSets(tablesA, tablesB); len(missing)+len(extra) > 0 {
		t.Fatalf("表名集合不一致: 文件链缺 %v / 文件链多 %v", missing, extra)
	}

	colTotal, idxTotal := 0, 0
	for _, tb := range tablesA {
		colsA := mysqlColumns(t, dbA, tb)
		colsB := mysqlColumns(t, dbB, tb)
		if missing, extra := diffSets(colsA, colsB); len(missing)+len(extra) > 0 {
			t.Errorf("%s 列名集合不一致: 文件链缺 %v / 文件链多 %v", tb, missing, extra)
		}
		idxA := mysqlIndexes(t, dbA, tb)
		idxB := mysqlIndexes(t, dbB, tb)
		if missing, extra := diffSets(idxA, idxB); len(missing)+len(extra) > 0 {
			t.Errorf("%s 索引名集合不一致: 文件链缺 %v / 文件链多 %v", tb, missing, extra)
		}
		colTotal += len(colsA)
		idxTotal += len(idxA)
	}
	t.Logf("MySQL 对账通过:%d 表 / %d 列名 / %d 索引名(STATISTICS 已排除 PRIMARY)",
		len(tablesA), colTotal, idxTotal)
}

func mysqlTables(t *testing.T, db *sql.DB) []string {
	t.Helper()
	rows, err := db.Query(`SELECT TABLE_NAME FROM information_schema.TABLES
		WHERE TABLE_SCHEMA = DATABASE() AND TABLE_TYPE = 'BASE TABLE'`)
	if err != nil {
		t.Fatalf("query tables: %v", err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatalf("scan table: %v", err)
		}
		out = append(out, name)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("tables: %v", err)
	}
	sort.Strings(out)
	return out
}

func mysqlColumns(t *testing.T, db *sql.DB, table string) []string {
	t.Helper()
	rows, err := db.Query(`SELECT COLUMN_NAME FROM information_schema.COLUMNS
		WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = ?`, table)
	if err != nil {
		t.Fatalf("query columns(%s): %v", table, err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatalf("scan column: %v", err)
		}
		out = append(out, name)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("columns(%s): %v", table, err)
	}
	sort.Strings(out)
	return out
}

// mysqlIndexes 取该表全部索引名,排除 PRIMARY(MySQL 主键索引在 STATISTICS
// 里固定叫 PRIMARY;对应口径注释见 TestReconcileMySQL)。
func mysqlIndexes(t *testing.T, db *sql.DB, table string) []string {
	t.Helper()
	rows, err := db.Query(`SELECT DISTINCT INDEX_NAME FROM information_schema.STATISTICS
		WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = ? AND INDEX_NAME <> 'PRIMARY'`, table)
	if err != nil {
		t.Fatalf("query indexes(%s): %v", table, err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatalf("scan index: %v", err)
		}
		out = append(out, name)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("indexes(%s): %v", table, err)
	}
	sort.Strings(out)
	return out
}
