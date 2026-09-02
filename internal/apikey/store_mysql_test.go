package apikey_test

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"strings"
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
	// 每个测试包独占一个库:go test 会并行跑不同包的二进制,
	// 共库会让彼此的清理 DELETE 互删数据。
	dbName := cfg.DBName + "_apikey"
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

func TestMySQLKeyTopUpRejectsInactiveKeys(t *testing.T) {
	store, _ := openKeyTestDB(t)
	ctx := context.Background()
	now := time.Now()
	past := now.Add(-time.Hour)

	revoked, err := store.Create(ctx, apikey.Key{
		Name: "revoked", Prefix: "sk-revoked00", KeyHash: apikey.Hash("sk-revoked-full"),
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if _, err := store.Revoke(ctx, revoked.ID, now); err != nil {
		t.Fatalf("Revoke: %v", err)
	}
	expired, err := store.Create(ctx, apikey.Key{
		Name: "expired", Prefix: "sk-expired00", KeyHash: apikey.Hash("sk-expired-full"),
		ExpiresAt: &past,
	})
	if err != nil {
		t.Fatalf("Create expired: %v", err)
	}

	for _, tc := range []struct {
		name string
		id   int64
		hash string
	}{
		{"revoked", revoked.ID, "sk-revoked-full"},
		{"expired", expired.ID, "sk-expired-full"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := store.TopUp(ctx, tc.id, 100, apikey.ReasonManualTopUp); !errors.Is(err, apikey.ErrKeyNotActive) {
				t.Fatalf("TopUp on %s key err = %v, want ErrKeyNotActive", tc.name, err)
			}
			// 余额与流水都不动。
			k, err := store.ByHash(ctx, apikey.Hash(tc.hash))
			if err != nil || k.QuotaMicros != 0 {
				t.Errorf("balance = %d (err %v), want untouched 0", k.QuotaMicros, err)
			}
			entries, err := store.QuotaLog(ctx, tc.id, 10)
			if err != nil || len(entries) != 0 {
				t.Errorf("ledger = %v (err %v), want empty", entries, err)
			}
		})
	}
}

func TestMySQLKeyExpiryBeyond2038RoundTrips(t *testing.T) {
	store, _ := openKeyTestDB(t)
	ctx := context.Background()
	// DATETIME(6) 才装得下 2038 之后的过期时间(TIMESTAMP 上限 2038-01-19)。
	far := time.Date(2050, 1, 2, 3, 4, 5, 0, time.UTC)
	if _, err := store.Create(ctx, apikey.Key{
		Name: "far-future", Prefix: "sk-farfuture", KeyHash: apikey.Hash("sk-far-full"),
		ExpiresAt: &far,
	}); err != nil {
		t.Fatalf("Create with 2050 expiry: %v", err)
	}
	got, err := store.ByHash(ctx, apikey.Hash("sk-far-full"))
	if err != nil {
		t.Fatalf("ByHash: %v", err)
	}
	if got.ExpiresAt == nil || !got.ExpiresAt.Equal(far) {
		t.Errorf("expires_at = %v, want %v preserved across DATETIME round trip", got.ExpiresAt, far)
	}
	if got.Status(time.Now()) != apikey.StatusActive {
		t.Errorf("status = %q, want active", got.Status(time.Now()))
	}
}

func TestMySQLKeyMigratesTimestampExpiryToDateTime(t *testing.T) {
	store, db := openKeyTestDB(t)
	ctx := context.Background()

	// 还原上一构建的表:expires_at 是 TIMESTAMP(6),并留下一行数据。
	if _, err := db.ExecContext(ctx, `DROP TABLE api_keys`); err != nil {
		t.Fatalf("drop legacy table: %v", err)
	}
	if _, err := db.ExecContext(ctx, `CREATE TABLE api_keys (
		id           BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,
		name         VARCHAR(64) NOT NULL,
		prefix       VARCHAR(16) NOT NULL,
		key_hash     CHAR(64)    NOT NULL,
		quota_micros BIGINT      NOT NULL DEFAULT 0,
		expires_at   TIMESTAMP(6) NULL,
		revoked_at   TIMESTAMP(6) NULL,
		created_at   TIMESTAMP(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
		updated_at   TIMESTAMP(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6) ON UPDATE CURRENT_TIMESTAMP(6),
		UNIQUE KEY uniq_api_key_hash (key_hash)
	) ENGINE = InnoDB DEFAULT CHARSET = utf8mb4`); err != nil {
		t.Fatalf("create legacy table: %v", err)
	}
	if _, err := db.ExecContext(ctx,
		`INSERT INTO api_keys (name, prefix, key_hash) VALUES ('legacy', 'sk-legacy000', 'legacy-hash')`); err != nil {
		t.Fatalf("seed legacy row: %v", err)
	}

	if err := store.EnsureSchema(ctx); err != nil {
		t.Fatalf("EnsureSchema with legacy table: %v", err)
	}

	var dataType string
	if err := db.QueryRowContext(ctx,
		`SELECT DATA_TYPE FROM information_schema.COLUMNS
		 WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'api_keys' AND COLUMN_NAME = 'expires_at'`,
	).Scan(&dataType); err != nil {
		t.Fatalf("query column type: %v", err)
	}
	if !strings.EqualFold(dataType, "datetime") {
		t.Fatalf("expires_at type = %q, want migrated to datetime", dataType)
	}
	var name string
	if err := db.QueryRowContext(ctx,
		`SELECT name FROM api_keys WHERE key_hash = 'legacy-hash'`).Scan(&name); err != nil {
		t.Fatalf("legacy row lost in migration: %v", err)
	}
	if name != "legacy" {
		t.Errorf("row name = %q, want legacy", name)
	}

	// 幂等:再次 EnsureSchema 不报错。
	if err := store.EnsureSchema(ctx); err != nil {
		t.Fatalf("second EnsureSchema: %v", err)
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

func TestMySQLKeyReserveSettleRefundLedger(t *testing.T) {
	store, _ := openKeyTestDB(t)
	ctx := context.Background()
	created, err := store.Create(ctx, apikey.Key{
		Name: "billing", Prefix: "sk-billing00", KeyHash: apikey.Hash("sk-billing-full"),
		QuotaMicros: apikey.USDToMicros(1),
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	// 结算差额符号约定:delta = 预扣 − 实际。正数退回多扣(多退),
	// 负数补扣低估的差额(少补)。
	type step struct {
		op             string // reserve | settle | refund
		amount         int64
		wantBalance    int64
		wantLedgerRows int
	}
	steps := []step{
		// 预扣 0.4,实际 0.5:少补,再扣 0.1。
		{"reserve", apikey.USDToMicros(0.4), apikey.USDToMicros(0.6), 2},
		{"settle", -apikey.USDToMicros(0.1), apikey.USDToMicros(0.5), 3},
		// 预扣 0.3,实际 0.1:多退,退回 0.2。
		{"reserve", apikey.USDToMicros(0.3), apikey.USDToMicros(0.2), 4},
		{"settle", apikey.USDToMicros(0.2), apikey.USDToMicros(0.4), 5},
		// 预扣 0.4 后上游失败:全额退款。
		{"reserve", apikey.USDToMicros(0.4), 0, 6},
		{"refund", apikey.USDToMicros(0.4), apikey.USDToMicros(0.4), 7},
	}
	for i, s := range steps {
		var balance int64
		var err error
		switch s.op {
		case "reserve":
			balance, err = store.Reserve(ctx, created.ID, s.amount, apikey.ReasonEstimate)
		case "settle":
			balance, err = store.Adjust(ctx, created.ID, s.amount, apikey.ReasonSettle)
		case "refund":
			balance, err = store.Adjust(ctx, created.ID, s.amount, apikey.ReasonRefund)
		}
		if err != nil {
			t.Fatalf("step %d %+v: %v", i, s, err)
		}
		if balance != s.wantBalance {
			t.Errorf("step %d %s: balance = %d, want %d", i, s.op, balance, s.wantBalance)
		}
	}

	entries, err := store.QuotaLog(ctx, created.ID, 10)
	if err != nil {
		t.Fatalf("QuotaLog: %v", err)
	}
	// newest first,每条带变动量与变动后余额。
	want := []struct {
		delta   int64
		balance int64
		reason  string
	}{
		{apikey.USDToMicros(0.4), apikey.USDToMicros(0.4), apikey.ReasonRefund},
		{-apikey.USDToMicros(0.4), 0, apikey.ReasonEstimate},
		{apikey.USDToMicros(0.2), apikey.USDToMicros(0.4), apikey.ReasonSettle},
		{-apikey.USDToMicros(0.3), apikey.USDToMicros(0.2), apikey.ReasonEstimate},
		{-apikey.USDToMicros(0.1), apikey.USDToMicros(0.5), apikey.ReasonSettle},
		{-apikey.USDToMicros(0.4), apikey.USDToMicros(0.6), apikey.ReasonEstimate},
		{apikey.USDToMicros(1), apikey.USDToMicros(1), apikey.ReasonInitial},
	}
	if len(entries) != len(want) {
		t.Fatalf("ledger = %d entries, want %d", len(entries), len(want))
	}
	for i, w := range want {
		if entries[i].DeltaMicros != w.delta || entries[i].BalanceMicros != w.balance || entries[i].Reason != w.reason {
			t.Errorf("entry %d = {%d, %d, %s}, want {%d, %d, %s}",
				i, entries[i].DeltaMicros, entries[i].BalanceMicros, entries[i].Reason,
				w.delta, w.balance, w.reason)
		}
	}

	// 零差额结算不改余额、不落流水。
	entriesBefore, _ := store.QuotaLog(ctx, created.ID, 100)
	balance, err := store.Adjust(ctx, created.ID, 0, apikey.ReasonSettle)
	if err != nil || balance != apikey.USDToMicros(0.4) {
		t.Fatalf("Adjust zero = %d (err %v), want unchanged", balance, err)
	}
	entriesAfter, _ := store.QuotaLog(ctx, created.ID, 100)
	if len(entriesAfter) != len(entriesBefore) {
		t.Errorf("zero-delta settle wrote a ledger row")
	}
}

func TestMySQLKeyReserveRejectsInsufficientAndDead(t *testing.T) {
	store, _ := openKeyTestDB(t)
	ctx := context.Background()
	now := time.Now()

	poor, err := store.Create(ctx, apikey.Key{
		Name: "poor", Prefix: "sk-poor00000", KeyHash: apikey.Hash("sk-poor-full"),
		QuotaMicros: 100,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	revoked, err := store.Create(ctx, apikey.Key{
		Name: "revoked", Prefix: "sk-rsvoked0", KeyHash: apikey.Hash("sk-reserve-revoked"),
		QuotaMicros: apikey.USDToMicros(5),
	})
	if err != nil {
		t.Fatalf("Create revoked: %v", err)
	}
	if _, err := store.Revoke(ctx, revoked.ID, now); err != nil {
		t.Fatalf("Revoke: %v", err)
	}
	past := now.Add(-time.Hour)
	expired, err := store.Create(ctx, apikey.Key{
		Name: "expired", Prefix: "sk-rsxpired", KeyHash: apikey.Hash("sk-reserve-expired"),
		QuotaMicros: apikey.USDToMicros(5),
		ExpiresAt:   &past,
	})
	if err != nil {
		t.Fatalf("Create expired: %v", err)
	}

	if _, err := store.Reserve(ctx, poor.ID, 101, apikey.ReasonEstimate); !errors.Is(err, apikey.ErrInsufficientQuota) {
		t.Errorf("over-balance Reserve err = %v, want ErrInsufficientQuota", err)
	}
	// 恰好等于余额可以扣到 0。
	if balance, err := store.Reserve(ctx, poor.ID, 100, apikey.ReasonEstimate); err != nil || balance != 0 {
		t.Errorf("exact Reserve = %d (err %v), want success draining to 0", balance, err)
	}
	if _, err := store.Reserve(ctx, poor.ID, 1, apikey.ReasonEstimate); !errors.Is(err, apikey.ErrInsufficientQuota) {
		t.Errorf("empty-balance Reserve err = %v, want ErrInsufficientQuota", err)
	}
	if _, err := store.Reserve(ctx, revoked.ID, 1, apikey.ReasonEstimate); !errors.Is(err, apikey.ErrKeyNotActive) {
		t.Errorf("revoked Reserve err = %v, want ErrKeyNotActive", err)
	}
	if _, err := store.Reserve(ctx, expired.ID, 1, apikey.ReasonEstimate); !errors.Is(err, apikey.ErrKeyNotActive) {
		t.Errorf("expired Reserve err = %v, want ErrKeyNotActive", err)
	}
	if _, err := store.Reserve(ctx, 999999, 1, apikey.ReasonEstimate); !errors.Is(err, apikey.ErrKeyNotFound) {
		t.Errorf("missing Reserve err = %v, want ErrKeyNotFound", err)
	}

	// 余额被恰好扣到 0 之后,任何再预扣都被拒。
	k, err := store.ByHash(ctx, apikey.Hash("sk-poor-full"))
	if err != nil {
		t.Fatalf("ByHash: %v", err)
	}
	if k.QuotaMicros != 0 {
		t.Errorf("balance = %d, want 0", k.QuotaMicros)
	}
}

func TestMySQLKeyConcurrentReserveNeverOverDeducts(t *testing.T) {
	store, _ := openKeyTestDB(t)
	ctx := context.Background()
	// 余额只够 5 笔预扣;8 笔并发必须恰好 5 成 3 败,余额始终 ≥ 0。
	const perReserve = int64(100_000)
	created, err := store.Create(ctx, apikey.Key{
		Name: "race", Prefix: "sk-race0000", KeyHash: apikey.Hash("sk-race-full"),
		QuotaMicros: perReserve * 5,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	const goroutines = 8
	errs := make([]error, goroutines)
	var wg sync.WaitGroup
	for i := range goroutines {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, errs[i] = store.Reserve(ctx, created.ID, perReserve, apikey.ReasonEstimate)
		}(i)
	}
	wg.Wait()

	var succeeded int
	for _, err := range errs {
		switch {
		case err == nil:
			succeeded++
		case errors.Is(err, apikey.ErrInsufficientQuota):
		default:
			t.Fatalf("concurrent Reserve: %v", err)
		}
	}
	if succeeded != 5 {
		t.Errorf("succeeded = %d reserves, want exactly 5", succeeded)
	}
	k, err := store.ByHash(ctx, apikey.Hash("sk-race-full"))
	if err != nil {
		t.Fatalf("ByHash: %v", err)
	}
	if k.QuotaMicros != 0 {
		t.Errorf("balance = %d, want 0 (no over-deduction, no leftover)", k.QuotaMicros)
	}
	entries, err := store.QuotaLog(ctx, created.ID, 100)
	if err != nil {
		t.Fatalf("QuotaLog: %v", err)
	}
	if len(entries) != 6 { // initial + 5 successful reserves
		t.Errorf("ledger = %d entries, want 6", len(entries))
	}
	for _, e := range entries {
		if e.BalanceMicros < 0 {
			t.Errorf("ledger balance went negative: %+v", e)
		}
	}
}
