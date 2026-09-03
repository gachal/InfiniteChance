package prompttemplate_test

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"testing"
	"time"

	mysqldriver "github.com/go-sql-driver/mysql"

	"github.com/gachal/InfiniteChance/internal/prompttemplate"
)

// openTemplateTestDB connects to a dedicated throwaway database on the compose
// MySQL (host port 3307). MYSQL_TEST_DSN overrides the DSN. Tests skip when
// the database is unreachable so `go test ./...` stays green without infra.
func openTemplateTestDB(t *testing.T) (*prompttemplate.MySQLStore, *sql.DB) {
	t.Helper()

	dsn := os.Getenv("MYSQL_TEST_DSN")
	if dsn == "" {
		dsn = "root:infinitechance@tcp(localhost:3307)/infinitechance_test?parseTime=true"
	}
	cfg, err := mysqldriver.ParseDSN(dsn)
	if err != nil {
		t.Fatalf("parse MYSQL_TEST_DSN: %v", err)
	}
	// 每个测试包独占一个库:go test 会并行跑不同包的二进制,
	// 共库会让彼此的清理 DELETE 互删数据。
	dbName := cfg.DBName + "_prompttemplate"
	cfg.DBName = ""

	server, err := sql.Open("mysql", cfg.FormatDSN())
	if err != nil {
		t.Fatalf("open mysql server: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := server.PingContext(ctx); err != nil {
		t.Skipf("mysql unreachable, skipping store tests: %v", err)
	}
	if _, err := server.ExecContext(ctx, "CREATE DATABASE IF NOT EXISTS `"+dbName+"`"); err != nil {
		t.Fatalf("create test database: %v", err)
	}

	cfg.DBName = dbName
	db, err := sql.Open("mysql", cfg.FormatDSN())
	if err != nil {
		t.Fatalf("open test database: %v", err)
	}
	store := prompttemplate.NewMySQLStore(db)
	if err := store.EnsureSchema(ctx); err != nil {
		t.Fatalf("EnsureSchema: %v", err)
	}
	if _, err := db.ExecContext(ctx, "DELETE FROM prompt_templates"); err != nil {
		t.Fatalf("clean prompt_templates: %v", err)
	}
	t.Cleanup(func() {
		db.Close()
		server.Close()
	})
	return store, db
}

func seedTemplate(t *testing.T, store *prompttemplate.MySQLStore, name string, enabled bool) prompttemplate.Template {
	t.Helper()
	created, err := store.Create(context.Background(), prompttemplate.Template{
		Name: name, Template: "为「" + prompttemplate.TopicPlaceholder + "」写提示词", Enabled: enabled,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	return created
}

func TestMySQLPromptTemplateSchemaIsIdempotent(t *testing.T) {
	store, _ := openTemplateTestDB(t)
	if err := store.EnsureSchema(context.Background()); err != nil {
		t.Fatalf("second EnsureSchema: %v", err)
	}
}

func TestMySQLPromptTemplateCreateGetRoundTrip(t *testing.T) {
	store, _ := openTemplateTestDB(t)
	ctx := context.Background()

	created := seedTemplate(t, store, "文生图-中文", true)
	if created.ID == 0 {
		t.Fatal("created.ID = 0, want assigned")
	}
	if created.CreatedAt.IsZero() || created.UpdatedAt.IsZero() {
		t.Errorf("timestamps not populated: %+v", created)
	}

	got, err := store.Get(ctx, created.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Name != "文生图-中文" || !got.Enabled {
		t.Errorf("got = %+v", got)
	}
	if got.Template != "为「{topic}」写提示词" {
		t.Errorf("template = %q", got.Template)
	}
}

func TestMySQLPromptTemplateListOrdersByID(t *testing.T) {
	store, _ := openTemplateTestDB(t)
	ctx := context.Background()

	second := seedTemplate(t, store, "乙", true)
	first := seedTemplate(t, store, "甲", true)

	list, err := store.List(ctx)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) != 2 || list[0].ID != second.ID || list[1].ID != first.ID {
		t.Fatalf("list = %+v, want id order", list)
	}
}

func TestMySQLPromptTemplateUpdateReplacesRow(t *testing.T) {
	store, _ := openTemplateTestDB(t)
	ctx := context.Background()
	created := seedTemplate(t, store, "旧名", true)

	updated, err := store.Update(ctx, prompttemplate.Template{
		ID: created.ID, Name: "新名", Template: "新的 {topic}", Enabled: false,
	})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if updated.Name != "新名" || updated.Enabled {
		t.Errorf("updated = %+v, want 新名/disabled", updated)
	}

	got, err := store.Get(ctx, created.ID)
	if err != nil {
		t.Fatalf("Get after update: %v", err)
	}
	if got.Name != "新名" || got.Enabled {
		t.Errorf("persisted = %+v", got)
	}
}

func TestMySQLPromptTemplateUpdateMissingAnswersNotFound(t *testing.T) {
	store, _ := openTemplateTestDB(t)
	_, err := store.Update(context.Background(), prompttemplate.Template{
		ID: 424242, Name: "任意", Template: "{topic}", Enabled: true,
	})
	if !errors.Is(err, prompttemplate.ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

func TestMySQLPromptTemplateDelete(t *testing.T) {
	store, _ := openTemplateTestDB(t)
	ctx := context.Background()
	created := seedTemplate(t, store, "待删", true)

	if err := store.Delete(ctx, created.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if err := store.Delete(ctx, created.ID); !errors.Is(err, prompttemplate.ErrNotFound) {
		t.Fatalf("second Delete err = %v, want ErrNotFound", err)
	}
	if _, err := store.Get(ctx, created.ID); !errors.Is(err, prompttemplate.ErrNotFound) {
		t.Fatalf("Get after delete err = %v, want ErrNotFound", err)
	}
}

func TestMySQLPromptTemplateListEnabledFilters(t *testing.T) {
	store, _ := openTemplateTestDB(t)
	ctx := context.Background()

	on := seedTemplate(t, store, "启用中", true)
	seedTemplate(t, store, "已停用", false)

	enabled, err := store.ListEnabled(ctx)
	if err != nil {
		t.Fatalf("ListEnabled: %v", err)
	}
	if len(enabled) != 1 || enabled[0].ID != on.ID {
		t.Fatalf("enabled = %+v, want only the enabled row", enabled)
	}

	// 管理端把停用模板重新打开,画布侧下一次请求即见 —— 模板改动
	// 即时反映到画布动作的库里依据。
	if _, err := store.Update(ctx, prompttemplate.Template{
		ID: enabled[0].ID + 1, Name: "已停用", Template: "为「{topic}」写提示词", Enabled: true,
	}); err != nil {
		t.Fatalf("re-enable: %v", err)
	}
	enabled, err = store.ListEnabled(ctx)
	if err != nil {
		t.Fatalf("ListEnabled after re-enable: %v", err)
	}
	if len(enabled) != 2 {
		t.Fatalf("enabled after re-enable = %d rows, want 2", len(enabled))
	}
}
