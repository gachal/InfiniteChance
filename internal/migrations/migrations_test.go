package migrations_test

import (
	"os"
	"path/filepath"
	"testing"
)

func writeFile(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func baseFiles(t *testing.T, dir string) {
	t.Helper()
	writeFile(t, dir, "database.sql", "-- 基线占位\nSELECT 1;\n")
	writeFile(t, dir, "database.sqlite.sql", "SELECT 2;\n")
}

func TestSplitLineEndSemicolon(t *testing.T) {
	script := "-- 头注释\nCREATE TABLE a (\n  x TEXT DEFAULT 'a;b' -- 行内非行尾分号\n);\n\n-- 尾注释\n"
	stmts, err := Split(script)
	if err != nil {
		t.Fatalf("split: %v", err)
	}
	if len(stmts) != 1 {
		t.Fatalf("want 1 statement, got %d: %q", len(stmts), stmts)
	}
	// 分号只在行尾才收束:字符串字面量里的 'a;b' 不得切开语句。
	if want := "CREATE TABLE a (\n  x TEXT DEFAULT 'a;b' -- 行内非行尾分号\n)"; stmts[0] != want {
		t.Fatalf("statement mismatch:\n got %q\nwant %q", stmts[0], want)
	}
}

func TestSplitMissingTrailingSemicolon(t *testing.T) {
	if _, err := Split("SELECT 1;\nSELECT 2\n"); err == nil {
		t.Fatal("want error for trailing statement without semicolon")
	}
	// 纯注释尾巴不是残余语句。
	stmts, err := Split("SELECT 1;\n-- 只是注释\n")
	if err != nil {
		t.Fatalf("comment tail should be legal: %v", err)
	}
	if len(stmts) != 1 {
		t.Fatalf("want 1 statement, got %d", len(stmts))
	}
}

func TestLoadRequiresBaselines(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "database.sql", "SELECT 1;\n")
	if _, err := Load(dir); err == nil {
		t.Fatal("want error when database.sqlite.sql missing")
	}
}

func TestLoadRejectsUnpairedIncrement(t *testing.T) {
	dir := t.TempDir()
	baseFiles(t, dir)
	writeFile(t, dir, "260101_add_lore.sql", "ALTER TABLE x ADD COLUMN y INT;\n")
	if _, err := Load(dir); err == nil {
		t.Fatal("want error for unpaired increment (sqlite side missing)")
	}
	writeFile(t, dir, "260101_add_lore.sqlite.sql", "ALTER TABLE x ADD COLUMN y INTEGER;\n")
	c, err := Load(dir)
	if err != nil {
		t.Fatalf("paired increment should load: %v", err)
	}
	if len(c.Steps) != 1 || c.Steps[0].Name != "260101_add_lore" {
		t.Fatalf("unexpected steps: %+v", c.Steps)
	}
	if got := c.Steps[0].SQLite.Stmts[0]; got != "ALTER TABLE x ADD COLUMN y INTEGER" {
		t.Fatalf("unexpected sqlite stmt: %q", got)
	}
}

func TestLoadRejectsBadStem(t *testing.T) {
	dir := t.TempDir()
	baseFiles(t, dir)
	writeFile(t, dir, "add_lore.sql", "SELECT 1;\n")
	writeFile(t, dir, "add_lore.sqlite.sql", "SELECT 1;\n")
	if _, err := Load(dir); err == nil {
		t.Fatal("want error for stem missing yyMMdd prefix")
	}
}

// TestLoadStepsSorted 只验证增量按文件名序排列(不涉及执行;链的真实执行
// 由 TestReconcileMySQL / TestReconcileSQLite 两侧兜底)。
func TestLoadStepsSorted(t *testing.T) {
	dir := t.TempDir()
	baseFiles(t, dir)
	// 故意乱序写文件名,Load 须按文件名序排列。
	writeFile(t, dir, "260201_second.sql", "SELECT 20;\n")
	writeFile(t, dir, "260201_second.sqlite.sql", "SELECT 21;\n")
	writeFile(t, dir, "260101_first.sql", "SELECT 10;\n")
	writeFile(t, dir, "260101_first.sqlite.sql", "SELECT 11;\n")
	c, err := Load(dir)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(c.Steps) != 2 || c.Steps[0].Name != "260101_first" || c.Steps[1].Name != "260201_second" {
		t.Fatalf("steps not sorted by filename: %+v", c.Steps)
	}
}
