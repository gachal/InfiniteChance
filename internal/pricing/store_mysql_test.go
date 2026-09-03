package pricing_test

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"testing"
	"time"

	mysqldriver "github.com/go-sql-driver/mysql"

	"github.com/gachal/InfiniteChance/internal/pricing"
)

// openPricingTestDB connects to a dedicated throwaway database on the compose
// MySQL (host port 3307). MYSQL_TEST_DSN overrides the DSN. Tests skip when
// the database is unreachable so `go test ./...` stays green without infra.
func openPricingTestDB(t *testing.T) (*pricing.MySQLStore, *sql.DB) {
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
	dbName := cfg.DBName + "_pricing"
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
	store := pricing.NewMySQLStore(db)
	if err := store.EnsureSchema(ctx); err != nil {
		t.Fatalf("EnsureSchema: %v", err)
	}
	if _, err := db.ExecContext(ctx, "DELETE FROM model_prices"); err != nil {
		t.Fatalf("clean model_prices: %v", err)
	}
	t.Cleanup(func() {
		db.Close()
		server.Close()
	})
	return store, db
}

func TestMySQLPriceCallTrackRoundTrip(t *testing.T) {
	store, _ := openPricingTestDB(t)
	ctx := context.Background()

	p := pricing.Price{
		PublicModel: "dall-e-3",
		Unit:        pricing.UnitCall,
		Call: &pricing.CallPrice{
			USDPerCallMicros: 40_000,
			SizeFactorMicros: map[string]int64{"1024x1024": 1_000_000, "1792x1024": 2_000_000},
		},
	}
	stored, err := store.Upsert(ctx, p)
	if err != nil {
		t.Fatalf("Upsert call row: %v", err)
	}
	if stored.Call == nil || stored.Call.USDPerCallMicros != 40_000 ||
		stored.Call.SizeFactorMicros["1792x1024"] != 2_000_000 {
		t.Fatalf("stored = %+v, want the call payload back", stored)
	}
	if stored.Token != nil {
		t.Errorf("stored token payload = %+v, want nil (dual-track invariant)", stored.Token)
	}

	got, err := store.ByModel(ctx, "dall-e-3")
	if err != nil || got.Call == nil || got.Call.USDPerCallMicros != 40_000 {
		t.Fatalf("ByModel = %+v (err %v), want the call payload", got, err)
	}
	// 同名换轨覆盖:call → token,config 列载荷随之替换。
	if _, err := store.Upsert(ctx, samplePrice("dall-e-3")); err != nil {
		t.Fatalf("Upsert overwrite with token row: %v", err)
	}
	got, err = store.ByModel(ctx, "dall-e-3")
	if err != nil || got.Token == nil || got.Call != nil {
		t.Errorf("after track switch = %+v (err %v), want token payload only", got, err)
	}
}

func samplePrice(model string) pricing.Price {
	return pricing.Price{
		PublicModel: model,
		Unit:        pricing.UnitToken,
		Token: &pricing.TokenPrice{
			InputMicrosPerMTokens:  440_000,
			OutputMicrosPerMTokens: 1_320_000,
			RatioMicros:            1_200_000,
		},
	}
}

func TestMySQLPriceSchemaIsIdempotent(t *testing.T) {
	store, _ := openPricingTestDB(t)

	if err := store.EnsureSchema(context.Background()); err != nil {
		t.Fatalf("second EnsureSchema: %v", err)
	}
}

func TestMySQLPriceUpsertListByModelDelete(t *testing.T) {
	store, _ := openPricingTestDB(t)
	ctx := context.Background()

	stored, err := store.Upsert(ctx, samplePrice("deepseek-chat"))
	if err != nil {
		t.Fatalf("Upsert: %v", err)
	}
	if stored.PublicModel != "deepseek-chat" || stored.Token == nil ||
		stored.Token.RatioMicros != 1_200_000 || stored.CreatedAt.IsZero() {
		t.Fatalf("stored = %+v, want payload and timestamps", stored)
	}

	// 第二条并存,List 按 model 名排序。
	if _, err := store.Upsert(ctx, samplePrice("gpt-test")); err != nil {
		t.Fatalf("Upsert 2: %v", err)
	}
	all, err := store.List(ctx)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(all) != 2 || all[0].PublicModel != "deepseek-chat" || all[1].PublicModel != "gpt-test" {
		t.Fatalf("List = %+v, want deepseek-chat then gpt-test", all)
	}

	// ByModel 命中与未命中。
	got, err := store.ByModel(ctx, "gpt-test")
	if err != nil || got.Token == nil || got.Token.OutputMicrosPerMTokens != 1_320_000 {
		t.Fatalf("ByModel = %+v (err %v), want gpt-test payload", got, err)
	}
	if _, err := store.ByModel(ctx, "nope"); !errors.Is(err, pricing.ErrNotFound) {
		t.Errorf("ByModel missing err = %v, want ErrNotFound", err)
	}

	// Upsert 同名覆盖而不是新增。
	updated := samplePrice("deepseek-chat")
	updated.Token.RatioMicros = 2_000_000
	if _, err := store.Upsert(ctx, updated); err != nil {
		t.Fatalf("Upsert overwrite: %v", err)
	}
	all, err = store.List(ctx)
	if err != nil || len(all) != 2 {
		t.Fatalf("List after overwrite = %+v (err %v), want 2 rows", all, err)
	}
	got, err = store.ByModel(ctx, "deepseek-chat")
	if err != nil || got.Token.RatioMicros != 2_000_000 {
		t.Errorf("overwritten price = %+v (err %v), want ratio 2.0", got, err)
	}

	if err := store.Delete(ctx, "gpt-test"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if err := store.Delete(ctx, "gpt-test"); !errors.Is(err, pricing.ErrNotFound) {
		t.Errorf("second Delete err = %v, want ErrNotFound", err)
	}
}
