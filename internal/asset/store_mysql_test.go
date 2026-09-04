package asset_test

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"testing"
	"time"

	mysqldriver "github.com/go-sql-driver/mysql"

	"github.com/gachal/InfiniteChance/internal/asset"
)

// openAssetTestDB connects to a dedicated throwaway database on the compose
// MySQL (host port 3307). MYSQL_TEST_DSN overrides the DSN. Tests skip when
// the database is unreachable so `go test ./...` stays green without infra.
func openAssetTestDB(t *testing.T) *asset.MySQLStore {
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
	dbName := cfg.DBName + "_asset"
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
	store := asset.NewMySQLStore(db)
	if err := store.EnsureSchema(ctx); err != nil {
		t.Fatalf("EnsureSchema: %v", err)
	}
	if _, err := db.ExecContext(ctx, "DELETE FROM assets"); err != nil {
		t.Fatalf("clean assets: %v", err)
	}
	t.Cleanup(func() {
		db.Close()
		server.Close()
	})
	return store
}

func TestMySQLAssetSchemaIsIdempotent(t *testing.T) {
	store := openAssetTestDB(t)
	if err := store.EnsureSchema(context.Background()); err != nil {
		t.Fatalf("second EnsureSchema: %v", err)
	}
}

func TestMySQLAssetCreateGetRoundTrip(t *testing.T) {
	store := openAssetTestDB(t)
	ctx := context.Background()

	created, err := store.Create(ctx, asset.Asset{
		Kind: asset.KindImage, CanvasID: 42, TaskID: "ct_abc",
		Model: "img-m", Prompt: "一只在月光下奔跑的猫",
		URL: "https://img.example/cat.png",
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if created.ID == 0 || created.CreatedAt.IsZero() {
		t.Fatalf("created = %+v, want id and timestamp from the DB", created)
	}

	got, err := store.Get(ctx, created.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got != created {
		t.Errorf("Get = %+v, want the created row %+v", got, created)
	}
	if _, err := store.Get(ctx, 999_999); !errors.Is(err, asset.ErrNotFound) {
		t.Errorf("Get missing = %v, want ErrNotFound", err)
	}
}

func TestMySQLAssetNullablePromptRoundTripsEmpty(t *testing.T) {
	store := openAssetTestDB(t)
	ctx := context.Background()

	created, err := store.Create(ctx, asset.Asset{
		Kind: asset.KindImage, CanvasID: 1, TaskID: "ct_x", URL: "data:image/png;base64,aGVsbG8=",
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	got, err := store.Get(ctx, created.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Prompt != "" || got.Model != "" {
		t.Errorf("null columns = prompt %q model %q, want empty", got.Prompt, got.Model)
	}
}

func TestMySQLAssetListFilterAndDelete(t *testing.T) {
	store := openAssetTestDB(t)
	ctx := context.Background()

	// List 的画布名来自 canvases 的 LEFT JOIN;该表由画布服务的
	// EnsureSchema 建,这里补最小形以验证 join 与「画布已删名字为空」。
	if _, err := store.DB.ExecContext(ctx,
		`CREATE TABLE IF NOT EXISTS canvases (
			id   BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,
			name VARCHAR(128) NOT NULL
		 ) ENGINE = InnoDB DEFAULT CHARSET = utf8mb4`); err != nil {
		t.Fatalf("create canvases: %v", err)
	}
	if _, err := store.DB.ExecContext(ctx, "DELETE FROM canvases"); err != nil {
		t.Fatalf("clean canvases: %v", err)
	}
	if _, err := store.DB.ExecContext(ctx,
		"INSERT INTO canvases (id, name) VALUES (7, '主画布')"); err != nil {
		t.Fatalf("seed canvas: %v", err)
	}

	img7, err := store.Create(ctx, asset.Asset{Kind: asset.KindImage, CanvasID: 7, URL: "u1", ObjectKey: "k1"})
	if err != nil {
		t.Fatalf("create 1: %v", err)
	}
	if _, err := store.Create(ctx, asset.Asset{Kind: asset.KindVideo, CanvasID: 7, URL: "u2"}); err != nil {
		t.Fatalf("create 2: %v", err)
	}
	if _, err := store.Create(ctx, asset.Asset{Kind: asset.KindImage, CanvasID: 8, URL: "u3"}); err != nil {
		t.Fatalf("create 3: %v", err)
	}

	// kind+canvas 过滤:只剩 7 号画布的图片,带画布名。
	rows, err := store.List(ctx, asset.Filter{Kind: asset.KindImage, CanvasID: 7})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(rows) != 1 || rows[0].ID != img7.ID || rows[0].CanvasName != "主画布" ||
		rows[0].ObjectKey != "k1" {
		t.Fatalf("rows = %+v, want the one image of canvas 7 with its name and object key", rows)
	}

	// 无过滤:最新在前。
	all, err := store.List(ctx, asset.Filter{})
	if err != nil {
		t.Fatalf("List all: %v", err)
	}
	if len(all) != 3 || all[0].ID < all[1].ID || all[1].ID < all[2].ID {
		t.Fatalf("all = %+v, want three rows newest first", all)
	}

	// 删除:行消失,再删 404。
	if err := store.Delete(ctx, img7.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if err := store.Delete(ctx, img7.ID); !errors.Is(err, asset.ErrNotFound) {
		t.Errorf("second Delete = %v, want ErrNotFound", err)
	}
}

func TestMySQLAssetLegacyRowMigratesWithDefaults(t *testing.T) {
	store := openAssetTestDB(t)
	ctx := context.Background()

	// 14 号票前的行没有对象存储列:原地加列后必须读出默认值,预览仍走
	// 旧的 url 契约。
	if _, err := store.DB.ExecContext(ctx,
		`INSERT INTO assets (kind, canvas_id, task_id, url)
		 VALUES ('image', 1, 'ct_old', 'https://img.example/legacy.png')`); err != nil {
		t.Fatalf("seed legacy row: %v", err)
	}
	// 该行 id 是当前自增位置;直接取回最新一行验证默认值。
	rows, err := store.List(ctx, asset.Filter{})
	if err != nil || len(rows) == 0 {
		t.Fatalf("List legacy: %v (%d rows)", err, len(rows))
	}
	got := rows[0]
	if got.ObjectKey != "" || got.ContentType != "" || got.SizeBytes != 0 {
		t.Errorf("legacy row = key %q type %q size %d, want zero defaults",
			got.ObjectKey, got.ContentType, got.SizeBytes)
	}
}
