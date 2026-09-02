package apikey_test

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"sync"
	"testing"
	"time"

	mysqldriver "github.com/go-sql-driver/mysql"

	"github.com/gachal/InfiniteChance/internal/apikey"
)

// openKeyTestDB connects to a dedicated throwaway database on the compose
// MySQL (host port 3307). MYSQL_TEST_DSN overrides the DSN. Tests skip when
// the database is unreachable so `go test ./...` stays green without infra.
func openKeyTestDB(t *testing.T) (*apikey.MySQLStore, *sql.DB) {
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
	store := apikey.NewMySQLStore(db)
	if err := store.EnsureSchema(ctx); err != nil {
		t.Fatalf("EnsureSchema: %v", err)
	}
	for _, table := range []string{"api_key_quota_log", "api_keys"} {
		if _, err := db.ExecContext(ctx, "DELETE FROM "+table); err != nil {
			t.Fatalf("clean %s: %v", table, err)
		}
	}
	t.Cleanup(func() {
		db.Close()
		server.Close()
	})
	return store, db
}

func TestMySQLKeySchemaIsIdempotent(t *testing.T) {
	store, _ := openKeyTestDB(t)

	if err := store.EnsureSchema(context.Background()); err != nil {
		t.Fatalf("second EnsureSchema: %v", err)
	}
}

func TestMySQLKeyCreateAndLookupByHash(t *testing.T) {
	store, _ := openKeyTestDB(t)
	ctx := context.Background()
	full, err := apikey.Generate()
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}

	created, err := store.Create(ctx, apikey.Key{
		Name:        "canvas-service",
		Prefix:      apikey.PrefixOf(full),
		KeyHash:     apikey.Hash(full),
		QuotaMicros: apikey.USDToMicros(10),
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if created.ID <= 0 || created.CreatedAt.IsZero() {
		t.Fatalf("stored key = %+v, want id and timestamps from the DB", created)
	}

	got, err := store.ByHash(ctx, apikey.Hash(full))
	if err != nil {
		t.Fatalf("ByHash: %v", err)
	}
	if got.ID != created.ID || got.QuotaMicros != apikey.USDToMicros(10) {
		t.Errorf("ByHash = %+v, want the created key", got)
	}
	if got.Status(time.Now()) != apikey.StatusActive {
		t.Errorf("status = %q, want active", got.Status(time.Now()))
	}

	// 初始额度自动落账。
	entries, err := store.QuotaLog(ctx, created.ID, 10)
	if err != nil {
		t.Fatalf("QuotaLog: %v", err)
	}
	if len(entries) != 1 || entries[0].Reason != apikey.ReasonInitial ||
		entries[0].BalanceMicros != apikey.USDToMicros(10) {
		t.Fatalf("initial ledger = %+v, want one initial entry at 10 USD", entries)
	}

	// 零额度 key 不留充值流水。
	zero, err := store.Create(ctx, apikey.Key{
		Name: "zero", Prefix: "sk-zero0000", KeyHash: apikey.Hash("sk-zero-full"),
	})
	if err != nil {
		t.Fatalf("Create zero: %v", err)
	}
	entries, err = store.QuotaLog(ctx, zero.ID, 10)
	if err != nil || len(entries) != 0 {
		t.Errorf("zero-quota ledger = %v (err %v), want empty", entries, err)
	}

	if _, err := store.ByHash(ctx, apikey.Hash("sk-unknown")); !errors.Is(err, apikey.ErrKeyNotFound) {
		t.Errorf("ByHash unknown err = %v, want ErrKeyNotFound", err)
	}
}

func TestMySQLKeyTopUpAccumulatesWithLedger(t *testing.T) {
	store, _ := openKeyTestDB(t)
	ctx := context.Background()
	created, err := store.Create(ctx, apikey.Key{
		Name: "topup", Prefix: "sk-topup000", KeyHash: apikey.Hash("sk-topup-full"),
		QuotaMicros: apikey.USDToMicros(1),
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	k, err := store.TopUp(ctx, created.ID, apikey.USDToMicros(2.5), apikey.ReasonManualTopUp)
	if err != nil {
		t.Fatalf("TopUp: %v", err)
	}
	if k.QuotaMicros != apikey.USDToMicros(3.5) {
		t.Errorf("balance = %d, want 3.5 USD in micros", k.QuotaMicros)
	}
	k, err = store.TopUp(ctx, created.ID, apikey.USDToMicros(0.5), apikey.ReasonManualTopUp)
	if err != nil {
		t.Fatalf("TopUp 2: %v", err)
	}
	if k.QuotaMicros != apikey.USDToMicros(4) {
		t.Errorf("balance = %d, want 4 USD in micros", k.QuotaMicros)
	}

	entries, err := store.QuotaLog(ctx, created.ID, 10)
	if err != nil {
		t.Fatalf("QuotaLog: %v", err)
	}
	if len(entries) != 3 {
		t.Fatalf("ledger = %d entries, want 3", len(entries))
	}
	// newest first,每条带充值后的余额快照。
	want := []struct {
		delta   int64
		balance int64
		reason  string
	}{
		{apikey.USDToMicros(0.5), apikey.USDToMicros(4), apikey.ReasonManualTopUp},
		{apikey.USDToMicros(2.5), apikey.USDToMicros(3.5), apikey.ReasonManualTopUp},
		{apikey.USDToMicros(1), apikey.USDToMicros(1), apikey.ReasonInitial},
	}
	for i, w := range want {
		if entries[i].DeltaMicros != w.delta || entries[i].BalanceMicros != w.balance || entries[i].Reason != w.reason {
			t.Errorf("entry %d = {%d, %d, %s}, want {%d, %d, %s}",
				i, entries[i].DeltaMicros, entries[i].BalanceMicros, entries[i].Reason,
				w.delta, w.balance, w.reason)
		}
	}

	if _, err := store.TopUp(ctx, 999999, 1, apikey.ReasonManualTopUp); !errors.Is(err, apikey.ErrKeyNotFound) {
		t.Errorf("TopUp missing err = %v, want ErrKeyNotFound", err)
	}
}

func TestMySQLKeyTopUpUnderConcurrencyNeverLosesCredit(t *testing.T) {
	store, _ := openKeyTestDB(t)
	ctx := context.Background()
	created, err := store.Create(ctx, apikey.Key{
		Name: "concurrent", Prefix: "sk-concurren", KeyHash: apikey.Hash("sk-concurrent-full"),
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	const goroutines, perTopUp = 8, int64(100_000)
	errs := make([]error, goroutines)
	var wg sync.WaitGroup
	for i := range goroutines {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, err := store.TopUp(ctx, created.ID, perTopUp, apikey.ReasonManualTopUp)
			errs[i] = err
		}(i)
	}
	wg.Wait()
	for _, err := range errs {
		if err != nil {
			t.Fatalf("concurrent TopUp: %v", err)
		}
	}

	k, err := store.ByHash(ctx, apikey.Hash("sk-concurrent-full"))
	if err != nil {
		t.Fatalf("ByHash: %v", err)
	}
	if want := perTopUp * goroutines; k.QuotaMicros != want {
		t.Errorf("balance = %d, want %d (every credit must land)", k.QuotaMicros, want)
	}
	entries, err := store.QuotaLog(ctx, created.ID, 100)
	if err != nil {
		t.Fatalf("QuotaLog: %v", err)
	}
	if len(entries) != goroutines {
		t.Errorf("ledger = %d entries, want %d", len(entries), goroutines)
	}
}

func TestMySQLKeyRevokeIdempotent(t *testing.T) {
	store, _ := openKeyTestDB(t)
	ctx := context.Background()
	created, err := store.Create(ctx, apikey.Key{
		Name: "revocable", Prefix: "sk-revocable", KeyHash: apikey.Hash("sk-revocable-full"),
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	at := time.Date(2026, 9, 1, 8, 0, 0, 0, time.UTC)
	k, err := store.Revoke(ctx, created.ID, at)
	if err != nil {
		t.Fatalf("Revoke: %v", err)
	}
	if k.RevokedAt == nil || !k.RevokedAt.Equal(at) {
		t.Errorf("revoked_at = %v, want %v", k.RevokedAt, at)
	}

	again, err := store.Revoke(ctx, created.ID, at.Add(time.Hour))
	if err != nil {
		t.Fatalf("second Revoke: %v", err)
	}
	if !again.RevokedAt.Equal(at) {
		t.Errorf("revoked_at = %v, want the first stamp %v kept", again.RevokedAt, at)
	}

	if _, err := store.Revoke(ctx, 999999, at); !errors.Is(err, apikey.ErrKeyNotFound) {
		t.Errorf("Revoke missing err = %v, want ErrKeyNotFound", err)
	}
}

func TestMySQLKeyStatusesFromRealRows(t *testing.T) {
	store, _ := openKeyTestDB(t)
	ctx := context.Background()
	now := time.Now()
	past := now.Add(-time.Hour)
	future := now.Add(time.Hour)

	_, err := store.Create(ctx, apikey.Key{
		Name: "expired", Prefix: "sk-expired00", KeyHash: apikey.Hash("sk-expired-full"),
		ExpiresAt: &past,
	})
	if err != nil {
		t.Fatalf("Create expired: %v", err)
	}
	revoked, err := store.Create(ctx, apikey.Key{
		Name: "revoked", Prefix: "sk-revoked00", KeyHash: apikey.Hash("sk-revoked-full"),
	})
	if err != nil {
		t.Fatalf("Create revoked: %v", err)
	}
	if _, err := store.Revoke(ctx, revoked.ID, now); err != nil {
		t.Fatalf("Revoke: %v", err)
	}
	if _, err := store.Create(ctx, apikey.Key{
		Name: "later", Prefix: "sk-later0000", KeyHash: apikey.Hash("sk-later-full"),
		ExpiresAt: &future,
	}); err != nil {
		t.Fatalf("Create future: %v", err)
	}

	// 过期/吊销行仍然可按哈希查到,由调用方按状态拒绝——这样错误码能区分原因。
	got, err := store.ByHash(ctx, apikey.Hash("sk-expired-full"))
	if err != nil {
		t.Fatalf("ByHash expired: %v", err)
	}
	if got.Status(now) != apikey.StatusExpired {
		t.Errorf("status = %q, want expired", got.Status(now))
	}
	got, err = store.ByHash(ctx, apikey.Hash("sk-revoked-full"))
	if err != nil {
		t.Fatalf("ByHash revoked: %v", err)
	}
	if got.Status(now) != apikey.StatusRevoked {
		t.Errorf("status = %q, want revoked", got.Status(now))
	}
	got, err = store.ByHash(ctx, apikey.Hash("sk-later-full"))
	if err != nil {
		t.Fatalf("ByHash future: %v", err)
	}
	if got.Status(now) != apikey.StatusActive || got.ExpiresAt == nil {
		t.Errorf("status = %q (expires %v), want active with expiry", got.Status(now), got.ExpiresAt)
	}
}
