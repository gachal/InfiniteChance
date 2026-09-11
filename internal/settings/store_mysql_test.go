package settings_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"testing"
	"time"

	mysqldriver "github.com/go-sql-driver/mysql"

	"github.com/gachal/InfiniteChance/internal/settings"
)

// openSettingsTestDB connects to a dedicated throwaway database on the
// compose MySQL (host port 3307). MYSQL_TEST_DSN overrides the DSN. Tests
// skip when the database is unreachable so `go test ./...` stays green
// without infra.
func openSettingsTestDB(t *testing.T) (*settings.MySQLStore, *sql.DB) {
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
	dbName := cfg.DBName + "_settings"
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
	store := settings.NewMySQLStore(db)
	if err := store.EnsureSchema(ctx); err != nil {
		t.Fatalf("EnsureSchema: %v", err)
	}
	if _, err := db.ExecContext(ctx, "DELETE FROM settings"); err != nil {
		t.Fatalf("clean settings: %v", err)
	}
	t.Cleanup(func() { db.Close(); server.Close() })
	return store, db
}

func TestMySQLGetMissingRow(t *testing.T) {
	store, _ := openSettingsTestDB(t)
	if _, err := store.Get(context.Background(), settings.NameStorage); !errors.Is(err, settings.ErrNotFound) {
		t.Fatalf("missing row err = %v, want ErrNotFound", err)
	}
}

func TestMySQLPutUpsertsAndRoundTrips(t *testing.T) {
	store, _ := openSettingsTestDB(t)
	ctx := context.Background()

	if _, err := store.Put(ctx, settings.NameStorage, []byte(`{"driver":"local"}`)); err != nil {
		t.Fatal(err)
	}
	got, err := store.Get(ctx, settings.NameStorage)
	if err != nil {
		t.Fatal(err)
	}
	// MySQL 的 JSON 列按标准空白重排字节,语义比较而非逐字节。
	var cfg settings.StorageConfig
	if err := json.Unmarshal(got.Value, &cfg); err != nil {
		t.Fatal(err)
	}
	if cfg.Driver != settings.DriverLocal {
		t.Fatalf("value = %s", got.Value)
	}

	row, err := store.Put(ctx, settings.NameStorage,
		[]byte(`{"driver":"oss","oss":{"endpoint":"e","bucket":"b","access_key":"AK","secret_key":"SK"}}`))
	if err != nil {
		t.Fatal(err)
	}
	got, err = store.Get(ctx, settings.NameStorage)
	if err != nil {
		t.Fatal(err)
	}
	cfg = settings.StorageConfig{}
	if err := json.Unmarshal(got.Value, &cfg); err != nil {
		t.Fatal(err)
	}
	if cfg.EffectiveDriver() != settings.DriverOSS {
		t.Fatalf("upserted value = %s", got.Value)
	}
	if got.UpdatedAt.Before(row.UpdatedAt.Add(-time.Second)) {
		t.Fatalf("updated_at not refreshed: %v", got.UpdatedAt)
	}
}

// JSON 列在写入时拒绝非 JSON 字节(与 SQLite 方言的差异在此:MySQL 帮
// 我们把坏值挡在门外)。
func TestMySQLPutRejectsNonJSON(t *testing.T) {
	store, _ := openSettingsTestDB(t)
	if _, err := store.Put(context.Background(), settings.NameStorage, []byte(`not json`)); err == nil {
		t.Fatal("non-JSON value should be rejected by the JSON column")
	}
}
