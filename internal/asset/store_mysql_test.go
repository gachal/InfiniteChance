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
