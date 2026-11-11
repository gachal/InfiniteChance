package migrations_test

// 本文件是原 migrations.go 全部代码的收编处(82 号票评审修复轮):装载原语
// (Script/Step/Chain/Load/Split)与执行原语(ApplyMySQL/ApplySQLite)只存在
// 于测试包 —— 生产构建里本包只剩 doc.go,任何启动路径在编译期都无法引用
// 它们执行迁移文件,「文件从不执行」(ADR 0008)由编译器锁死而非靠约定。

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// Script 是一份 SQL 文件装载后的形态:文件名与切分后的语句序列。
type Script struct {
	File  string
	Stmts []string
}

// Step 是一对增量文件(MySQL 侧 + SQLite 侧同名主干)。
type Step struct {
	// Name 是两侧共同的文件名主干,形如 yyMMdd_英文功能描述。
	Name string
	// MySQL 是 <主干>.sql,SQLite 是 <主干>.sqlite.sql。
	MySQL  Script
	SQLite Script
}

// Chain 是 migrations/ 目录装载后的文件链:两份基线在前,增量按文件名序。
// 增量链为空是合法态(切换提交零增量)。
type Chain struct {
	// BaselineMySQL / BaselineSQLite 对应 database.sql 与 database.sqlite.sql。
	BaselineMySQL  Script
	BaselineSQLite Script
	Steps          []Step
}

// stepStemRe 校验增量文件名主干:yyMMdd_ + 非空描述。命名纪律见 CONTEXT.md
// 「迁移留底」词条;写错名字在 Load 即报错,不做静默忽略。
var stepStemRe = regexp.MustCompile(`^[0-9]{6}_.+$`)

// Load 扫描 dir 装载文件链。基线两份缺任一即 error;增量按
// yyMMdd_*.sql 与 yyMMdd_*.sqlite.sql 成对扫描 —— 任一侧缺对即 error,
// 这是「增量成对」纪律的机械兜底。装载时同时做语句切分,文件不可执行
// (缺行尾分号)同样当场报错。
func Load(dir string) (*Chain, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	c := &Chain{}
	steps := map[string]*Step{}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		switch {
		case name == "database.sql":
			c.BaselineMySQL, err = loadScript(dir, name)
			if err != nil {
				return nil, err
			}
		case name == "database.sqlite.sql":
			c.BaselineSQLite, err = loadScript(dir, name)
			if err != nil {
				return nil, err
			}
		case strings.HasSuffix(name, ".sqlite.sql"):
			stem := strings.TrimSuffix(name, ".sqlite.sql")
			s := steps[stem]
			if s == nil {
				s = &Step{Name: stem}
				steps[stem] = s
			}
			if s.SQLite.File != "" {
				return nil, fmt.Errorf("migrations: 增量主干 %q 的 sqlite 侧重复出现", stem)
			}
			if s.SQLite, err = loadScript(dir, name); err != nil {
				return nil, err
			}
		case strings.HasSuffix(name, ".sql"):
			stem := strings.TrimSuffix(name, ".sql")
			s := steps[stem]
			if s == nil {
				s = &Step{Name: stem}
				steps[stem] = s
			}
			if s.MySQL.File != "" {
				return nil, fmt.Errorf("migrations: 增量主干 %q 的 mysql 侧重复出现", stem)
			}
			if s.MySQL, err = loadScript(dir, name); err != nil {
				return nil, err
			}
		default:
			// 非 SQL 文件(如 README)忽略。
		}
	}
	if c.BaselineMySQL.File == "" || c.BaselineSQLite.File == "" {
		return nil, fmt.Errorf("migrations: 基线缺失(%s 下需同时存在 database.sql 与 database.sqlite.sql)", dir)
	}
	for stem, s := range steps {
		if !stepStemRe.MatchString(stem) {
			return nil, fmt.Errorf("migrations: 增量文件名 %q 不符 yyMMdd_描述 纪律", stem)
		}
		if s.MySQL.File == "" || s.SQLite.File == "" {
			return nil, fmt.Errorf("migrations: 增量 %q 不成对: mysql=%q sqlite=%q —— 增量必须两份同名主干成对",
				stem, s.MySQL.File, s.SQLite.File)
		}
		c.Steps = append(c.Steps, *s)
	}
	sort.Slice(c.Steps, func(i, j int) bool { return c.Steps[i].Name < c.Steps[j].Name })
	return c, nil
}

func loadScript(dir, name string) (Script, error) {
	raw, err := os.ReadFile(filepath.Join(dir, name))
	if err != nil {
		return Script{}, err
	}
	stmts, err := Split(string(raw))
	if err != nil {
		return Script{}, fmt.Errorf("migrations: %s: %w", name, err)
	}
	return Script{File: name, Stmts: stmts}, nil
}

// Split 把 SQL 脚本按「行尾分号」切成单条语句(返回值不含行尾分号与首尾
// 空白)。分号只认行尾:基线由 SHOW CREATE TABLE / sqlite_master 导出,增量
// 按仓库纪律手写,字符串字面量(含 SQLite 默认值里的 strftime 格式串)不出
// 现行尾分号。整行注释(剥空白后以 -- 开头)与空白行在切分时一律丢弃、
// 不参与执行 —— 独立成行的 -- 注释不随其后语句保留,解释性注释活在 .sql
// 文件本身;附着在语句行上的行内/行尾注释(如建表列的行内说明)则随语句
// 原样保留。脚本末尾出现未以分号收束的残余语句时返回 error。
func Split(script string) ([]string, error) {
	var out []string
	var cur []string // 当前语句的行,直到行尾分号收束
	lineEndsStmt := func(ln string) bool {
		t := strings.TrimRight(ln, " \t\r")
		return strings.HasSuffix(t, ";")
	}
	appendChunk := func() {
		kept := make([]string, 0, len(cur))
		for _, ln := range cur {
			t := strings.TrimSpace(ln)
			if t == "" || strings.HasPrefix(t, "--") {
				continue
			}
			kept = append(kept, ln)
		}
		if len(kept) == 0 {
			return // 纯注释/空白块
		}
		out = append(out, strings.TrimRight(strings.Join(kept, "\n"), " \t\r"))
	}
	for _, ln := range strings.Split(script, "\n") {
		if lineEndsStmt(ln) {
			cur = append(cur, strings.TrimSuffix(strings.TrimRight(ln, " \t\r"), ";"))
			appendChunk()
			cur = cur[:0]
			continue
		}
		cur = append(cur, ln)
	}
	// 尾部残余:全部是注释/空白则合法;否则是缺行尾分号的语句。
	for _, ln := range cur {
		t := strings.TrimSpace(ln)
		if t != "" && !strings.HasPrefix(t, "--") {
			return nil, fmt.Errorf("语句缺少行尾分号,残余 %q", firstLine(ln))
		}
	}
	return out, nil
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

// ApplyMySQL 在 db 上按链序(基线 + 增量)真实执行 MySQL 侧脚本,逐语句
// Exec。仅供本包对账测试与未来 runner 翻面使用 —— 它只存在于测试包,
// 启动路径无从引用。
func ApplyMySQL(ctx context.Context, db *sql.DB, c *Chain) error {
	scripts := make([]Script, 0, len(c.Steps)+1)
	scripts = append(scripts, c.BaselineMySQL)
	for _, s := range c.Steps {
		scripts = append(scripts, s.MySQL)
	}
	return apply(ctx, db, scripts)
}

// ApplySQLite 同 ApplyMySQL,SQLite 侧。文件内与文件序上须遵守「列迁移
// 先于引用它的索引」纪律(asset 包为范例),排序错误在本函数当场炸。
func ApplySQLite(ctx context.Context, db *sql.DB, c *Chain) error {
	scripts := make([]Script, 0, len(c.Steps)+1)
	scripts = append(scripts, c.BaselineSQLite)
	for _, s := range c.Steps {
		scripts = append(scripts, s.SQLite)
	}
	return apply(ctx, db, scripts)
}

func apply(ctx context.Context, db *sql.DB, scripts []Script) error {
	for _, s := range scripts {
		for _, stmt := range s.Stmts {
			if _, err := db.ExecContext(ctx, stmt); err != nil {
				return fmt.Errorf("migrations: 执行 %s 失败: %w(语句首行: %s)", s.File, err, firstLine(stmt))
			}
		}
	}
	return nil
}
