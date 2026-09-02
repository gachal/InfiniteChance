package auth_test

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"sync"
	"testing"
	"time"

	mysqldriver "github.com/go-sql-driver/mysql"

	"github.com/gachal/InfiniteChance/internal/auth"
)

// openTestStore connects to a dedicated throwaway database on the compose
// MySQL (host port 3307). MYSQL_TEST_DSN overrides the DSN. Tests skip when
// the database is unreachable so `go test ./...` stays green without infra;
// with infra up, the store's real SQL and concurrency semantics are what's
// under test.
func openTestStore(t *testing.T) (*auth.MySQLStore, *sql.DB) {
	t.Helper()

	dsn := os.Getenv("MYSQL_TEST_DSN")
	if dsn == "" {
		dsn = "root:infinitechance@tcp(localhost:3307)/infinitechance_test?parseTime=true"
	}
	cfg, err := mysqldriver.ParseDSN(dsn)
	if err != nil {
		t.Fatalf("parse MYSQL_TEST_DSN: %v", err)
	}
	dbName := cfg.DBName
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
	store := auth.NewMySQLStore(db)
	if err := store.EnsureSchema(ctx); err != nil {
		t.Fatalf("EnsureSchema: %v", err)
	}
	if _, err := db.ExecContext(ctx, "DELETE FROM admin_accounts"); err != nil {
		t.Fatalf("clean admin_accounts: %v", err)
	}
	t.Cleanup(func() {
		db.Close()
		server.Close()
	})
	return store, db
}

func TestMySQLStoreEnsureSchemaIsIdempotent(t *testing.T) {
	store, _ := openTestStore(t)

	if err := store.EnsureSchema(context.Background()); err != nil {
		t.Fatalf("second EnsureSchema: %v", err)
	}
}

func TestMySQLStoreFirstAdminLifecycle(t *testing.T) {
	store, _ := openTestStore(t)
	ctx := context.Background()

	initialized, err := store.Initialized(ctx)
	if err != nil {
		t.Fatalf("Initialized: %v", err)
	}
	if initialized {
		t.Fatal("cleaned table should report not initialized")
	}

	hash, err := auth.HashPassword("test-password-123")
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	if err := store.CreateFirstAdmin(ctx, "admin", hash); err != nil {
		t.Fatalf("CreateFirstAdmin: %v", err)
	}

	initialized, err = store.Initialized(ctx)
	if err != nil {
		t.Fatalf("Initialized: %v", err)
	}
	if !initialized {
		t.Error("after CreateFirstAdmin, Initialized should be true")
	}

	// 第二次创建首位管理员必须被拒绝。
	if err := store.CreateFirstAdmin(ctx, "other", hash); !errors.Is(err, auth.ErrAdminExists) {
		t.Errorf("second CreateFirstAdmin err = %v, want ErrAdminExists", err)
	}

	account, err := store.AccountByUsername(ctx, "admin")
	if err != nil {
		t.Fatalf("AccountByUsername: %v", err)
	}
	if account.Username != "admin" {
		t.Errorf("username = %q, want %q", account.Username, "admin")
	}
	if account.PasswordHash != hash {
		t.Errorf("password_hash changed across a read/write round trip")
	}
	if !auth.CheckPassword(account.PasswordHash, "test-password-123") {
		t.Error("stored hash should verify against the original password")
	}

	if _, err := store.AccountByUsername(ctx, "nobody"); !errors.Is(err, auth.ErrAdminNotFound) {
		t.Errorf("unknown user err = %v, want ErrAdminNotFound", err)
	}
}

func TestMySQLStoreMigratesLegacyAutoIncrement(t *testing.T) {
	store, db := openTestStore(t)
	ctx := context.Background()

	// 还原旧结构的表:自增主键,且仅存的管理员行 id 已漂移到 2
	// (模拟历史上删除重建过管理员)。
	if _, err := db.ExecContext(ctx, `DROP TABLE admin_accounts`); err != nil {
		t.Fatalf("drop legacy table: %v", err)
	}
	if _, err := db.ExecContext(ctx, `CREATE TABLE admin_accounts (
		id            BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
		username      VARCHAR(64)  NOT NULL,
		password_hash VARCHAR(255) NOT NULL
	) ENGINE = InnoDB DEFAULT CHARSET = utf8mb4`); err != nil {
		t.Fatalf("create legacy table: %v", err)
	}
	hash, err := auth.HashPassword("legacy-password-123")
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	if _, err := db.ExecContext(ctx,
		`INSERT INTO admin_accounts (username, password_hash) VALUES ('tmp', ?)`, hash); err != nil {
		t.Fatalf("seed tmp row: %v", err)
	}
	if _, err := db.ExecContext(ctx, `DELETE FROM admin_accounts WHERE username = 'tmp'`); err != nil {
		t.Fatalf("delete tmp row: %v", err)
	}
	if _, err := db.ExecContext(ctx,
		`INSERT INTO admin_accounts (username, password_hash) VALUES ('admin', ?)`, hash); err != nil {
		t.Fatalf("seed admin row: %v", err)
	}

	if err := store.EnsureSchema(ctx); err != nil {
		t.Fatalf("EnsureSchema with legacy table: %v", err)
	}

	// 迁移后旧行归一到 id=1,再次创建首位管理员仍被拒绝。
	if err := store.CreateFirstAdmin(ctx, "other", hash); !errors.Is(err, auth.ErrAdminExists) {
		t.Errorf("CreateFirstAdmin after migration err = %v, want ErrAdminExists", err)
	}
	var id int
	if err := db.QueryRowContext(ctx,
		`SELECT id FROM admin_accounts WHERE username = 'admin'`).Scan(&id); err != nil {
		t.Fatalf("query migrated row: %v", err)
	}
	if id != 1 {
		t.Errorf("legacy row id = %d, want normalized to 1", id)
	}
}

func TestMySQLStoreConcurrentFirstAdminHasSingleWinner(t *testing.T) {
	store, _ := openTestStore(t)
	ctx := context.Background()

	hash, err := auth.HashPassword("race-password-123")
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}

	const contenders = 4
	errs := make([]error, contenders)
	var wg sync.WaitGroup
	for i := range contenders {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			errs[i] = store.CreateFirstAdmin(ctx, "admin"+string(rune('0'+i)), hash)
		}(i)
	}
	wg.Wait()

	winners := 0
	for _, err := range errs {
		if err == nil {
			winners++
		} else if !errors.Is(err, auth.ErrAdminExists) {
			t.Fatalf("unexpected store error: %v", err)
		}
	}
	if winners != 1 {
		t.Errorf("winners = %d, want exactly 1", winners)
	}
}
