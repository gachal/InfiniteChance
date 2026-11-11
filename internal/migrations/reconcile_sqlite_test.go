package migrations_test

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"sort"
	"testing"

	"github.com/gachal/InfiniteChance/internal/sqlitedb"
)

// migrateDir 是仓库根的 migrations/ 留底目录(测试 cwd = internal/migrations)。
const migrateDir = "../../migrations"

// TestReconcileSQLite 形状对账(SQLite 侧,永不 skip):
//
//	库 A(临时文件库):真实执行「基线 + 全部增量」文件链;
//	库 B(临时文件库):跑全部 17 store EnsureSchema;
//	逐表比对表名集合、列名集合(PRAGMA table_info)、索引名集合(PRAGMA index_list)。
//
// 双写纪律(增量文件对 + EnsureSchema 双方言)漏写任一侧,本测试当场红。
// 只比名集合不比类型:11 类方言差异(MySQL JSON vs SQLite TEXT 等)是设计内
// 差异(见研究纪要第四节 / ADR 0001),不是对账对象。
//
// 索引比对口径(实证结论,2026-11-11 导出基线时核对):票面假设「SQLite 隐式
// 主键索引 sqlite_autoindex_* 不在 index_list 出现」经实证修正 —— 内联 UNIQUE
// 约束(admin_accounts/api_keys 的 `CONSTRAINT uniq_* UNIQUE (...)`,origin='u',
// 约束名在索引侧丢失)与 TEXT 主键(settings/model_prices 的 name/public_model、
// video_tasks/music_tasks/canvas_tasks 的 id,origin='pk')都会以
// sqlite_autoindex_<表>_<n> 出现在 index_list;voices 的唯一约束是独立具名
// CREATE UNIQUE INDEX(uk_voices_voice_id,origin='c'),不产生 autoindex。
// 本对账是同方言比对(文件链库 vs EnsureSchema 库),autoindex 编号由 DDL 内
// 约束顺序决定、两侧同 DDL 必同名,因此 index_list 名集合**全量**比对(含
// autoindex)两侧确定性一致;MySQL 侧 PRIMARY 的对应口径见 reconcile_mysql_test。
func TestReconcileSQLite(t *testing.T) {
	ctx := context.Background()
	chain, err := Load(migrateDir)
	if err != nil {
		t.Fatalf("load migrations: %v", err)
	}

	// 库 A:基线 + 全部增量,真实执行(为未来翻面留门 —— 文件必须保持可执行)。
	dbA, err := sqlitedb.Open(filepath.Join(t.TempDir(), "chain.db"))
	if err != nil {
		t.Fatalf("open chain db: %v", err)
	}
	defer dbA.Close()
	if err := ApplySQLite(ctx, dbA, chain); err != nil {
		t.Fatalf("apply chain: %v", err)
	}

	// 库 B:全部 EnsureSchema(装配清单见 chain_test.go 的 ensureAllSchemas)。
	dbB, err := sqlitedb.Open(filepath.Join(t.TempDir(), "ensure.db"))
	if err != nil {
		t.Fatalf("open ensure db: %v", err)
	}
	defer dbB.Close()
	ensureAllSchemas(t, ctx, dbB, "sqlite")

	tablesA, err := sqliteTables(dbA)
	if err != nil {
		t.Fatalf("tables A: %v", err)
	}
	tablesB, err := sqliteTables(dbB)
	if err != nil {
		t.Fatalf("tables B: %v", err)
	}
	if missing, extra := diffSets(tablesA, tablesB); len(missing)+len(extra) > 0 {
		t.Fatalf("表名集合不一致: 文件链缺 %v / 文件链多 %v", missing, extra)
	}

	colTotal, idxTotal := 0, 0
	for _, tb := range tablesA {
		colsA, err := sqliteColumns(dbA, tb)
		if err != nil {
			t.Fatalf("columns A(%s): %v", tb, err)
		}
		colsB, err := sqliteColumns(dbB, tb)
		if err != nil {
			t.Fatalf("columns B(%s): %v", tb, err)
		}
		if missing, extra := diffSets(colsA, colsB); len(missing)+len(extra) > 0 {
			t.Errorf("%s 列名集合不一致: 文件链缺 %v / 文件链多 %v", tb, missing, extra)
		}
		idxA, err := sqliteIndexes(dbA, tb)
		if err != nil {
			t.Fatalf("indexes A(%s): %v", tb, err)
		}
		idxB, err := sqliteIndexes(dbB, tb)
		if err != nil {
			t.Fatalf("indexes B(%s): %v", tb, err)
		}
		if missing, extra := diffSets(idxA, idxB); len(missing)+len(extra) > 0 {
			t.Errorf("%s 索引名集合不一致: 文件链缺 %v / 文件链多 %v", tb, missing, extra)
		}
		colTotal += len(colsA)
		idxTotal += len(idxA)
	}
	t.Logf("SQLite 对账通过:%d 表 / %d 列名 / %d 索引名(index_list 含 sqlite_autoindex_* 与具名索引)",
		len(tablesA), colTotal, idxTotal)
}

func sqliteTables(db *sql.DB) ([]string, error) {
	rows, err := db.Query(`SELECT name FROM sqlite_master WHERE type='table' AND name NOT LIKE 'sqlite_%'`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		out = append(out, name)
	}
	sort.Strings(out)
	return out, rows.Err()
}

func sqliteColumns(db *sql.DB, table string) ([]string, error) {
	rows, err := db.Query(fmt.Sprintf(`PRAGMA table_info(%s)`, table))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var cid int
		var name, ctype string
		var notNull, pk int
		var dflt any
		if err := rows.Scan(&cid, &name, &ctype, &notNull, &dflt, &pk); err != nil {
			return nil, err
		}
		out = append(out, name)
	}
	sort.Strings(out)
	return out, rows.Err()
}

func sqliteIndexes(db *sql.DB, table string) ([]string, error) {
	rows, err := db.Query(fmt.Sprintf(`PRAGMA index_list(%s)`, table))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var seq int
		var name string
		var unique int
		var origin string
		var partial int
		if err := rows.Scan(&seq, &name, &unique, &origin, &partial); err != nil {
			return nil, err
		}
		out = append(out, name)
	}
	sort.Strings(out)
	return out, rows.Err()
}

// diffSets 返回 a 缺少 b 的元素(missing)与 a 多出 b 的元素(extra)。
// 两侧调用方均已排序,输出保持稳定顺序,失败信息可读。
func diffSets(a, b []string) (missing, extra []string) {
	setA := make(map[string]bool, len(a))
	for _, s := range a {
		setA[s] = true
	}
	setB := make(map[string]bool, len(b))
	for _, s := range b {
		setB[s] = true
	}
	for _, s := range a {
		if !setB[s] {
			extra = append(extra, s)
		}
	}
	for _, s := range b {
		if !setA[s] {
			missing = append(missing, s)
		}
	}
	return
}
