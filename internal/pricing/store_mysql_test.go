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
			CNYPerCallMicros: 40_000,
			SizeFactorMicros: map[string]int64{"1024x1024": 1_000_000, "1792x1024": 2_000_000},
		},
	}
	stored, err := store.Upsert(ctx, p)
	if err != nil {
		t.Fatalf("Upsert call row: %v", err)
	}
	if stored.Call == nil || stored.Call.CNYPerCallMicros != 40_000 ||
		stored.Call.SizeFactorMicros["1792x1024"] != 2_000_000 {
		t.Fatalf("stored = %+v, want the call payload back", stored)
	}
	if stored.Token != nil {
		t.Errorf("stored token payload = %+v, want nil (dual-track invariant)", stored.Token)
	}

	got, err := store.ByModel(ctx, "dall-e-3")
	if err != nil || got.Call == nil || got.Call.CNYPerCallMicros != 40_000 {
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

func TestMySQLPriceCharacterTrackRoundTrip(t *testing.T) {
	store, _ := openPricingTestDB(t)
	ctx := context.Background()

	stored, err := store.Upsert(ctx, pricing.Price{
		PublicModel: "qwen-audio-3.1-tts-plus", Unit: pricing.UnitCharacter,
		Character: &pricing.CharacterPrice{CNYPer10KCharsMicros: 1_400_000},
	})
	if err != nil {
		t.Fatalf("Upsert: %v", err)
	}
	if stored.Character == nil || stored.Character.CNYPer10KCharsMicros != 1_400_000 {
		t.Fatalf("upsert readback = %+v, want the character payload intact", stored)
	}
	got, err := store.ByModel(ctx, "qwen-audio-3.1-tts-plus")
	if err != nil {
		t.Fatalf("ByModel: %v", err)
	}
	if got.Unit != pricing.UnitCharacter || got.Character == nil || got.Character.CNYPer10KCharsMicros != 1_400_000 {
		t.Fatalf("ByModel = %+v, want character track at 1400000 micros/万字符", got)
	}
	list, err := store.List(ctx)
	if err != nil || len(list) != 1 || list[0].Character == nil {
		t.Fatalf("List = %+v (%v), want the character row", list, err)
	}
}

// 88 号票:capabilities 列双方言读写 —— 空落 NULL、非空落 JSON 数组,
// 只改能力的 Upsert 也落库(MySQL 侧 ON UPDATE 语义天然前进 updated_at)。
func TestMySQLPriceCapabilitiesRoundTrip(t *testing.T) {
	store, db := openPricingTestDB(t)
	ctx := context.Background()

	stored, err := store.Upsert(ctx, pricing.Price{
		PublicModel: "og-image-2.5", Unit: pricing.UnitCall, Capabilities: []string{"images", "panorama"},
		Call: &pricing.CallPrice{CNYPerCallMicros: 40_000},
	})
	if err != nil {
		t.Fatalf("upsert: %v", err)
	}
	if len(stored.Capabilities) != 2 || stored.Capabilities[0] != "images" || stored.Capabilities[1] != "panorama" {
		t.Fatalf("upsert capabilities = %v, want [images panorama]", stored.Capabilities)
	}
	byModel, err := store.ByModel(ctx, "og-image-2.5")
	if err != nil {
		t.Fatalf("ByModel: %v", err)
	}
	if !byModel.HasCapability(pricing.CapImages) || !byModel.HasCapability(pricing.CapPanorama) {
		t.Fatalf("ByModel capabilities = %v, want images+panorama", byModel.Capabilities)
	}

	// 空能力落 NULL,读回 nil。
	if _, err := store.Upsert(ctx, pricing.Price{
		PublicModel: "plain-img", Unit: pricing.UnitCall, Call: &pricing.CallPrice{},
	}); err != nil {
		t.Fatalf("upsert empty: %v", err)
	}
	var raw []byte
	if err := db.QueryRowContext(ctx,
		`SELECT capabilities FROM model_prices WHERE public_model = 'plain-img'`).Scan(&raw); err != nil {
		t.Fatalf("raw scan: %v", err)
	}
	if raw != nil {
		t.Fatalf("empty capabilities stored as %s, want NULL", raw)
	}

	// 只改能力:覆盖生效。
	if _, err := store.Upsert(ctx, pricing.Price{
		PublicModel: "og-image-2.5", Unit: pricing.UnitCall, Capabilities: []string{"panorama"},
		Call: &pricing.CallPrice{CNYPerCallMicros: 40_000},
	}); err != nil {
		t.Fatalf("upsert change: %v", err)
	}
	p, err := store.ByModel(ctx, "og-image-2.5")
	if err != nil {
		t.Fatalf("ByModel 2: %v", err)
	}
	if len(p.Capabilities) != 1 || p.Capabilities[0] != "panorama" {
		t.Fatalf("capabilities after change = %v, want [panorama]", p.Capabilities)
	}
}

// 88 号票一次性回填(加列分支):存量库(无 capabilities 列)跑
// EnsureSchema → 加列 + 按「启用渠道能力位并集」回填;NULL 能力渠道按
// 07 号票生效集口径贡献 chat(聊天模型得 ["chat"],非聊天模型保持
// NULL);panorama 单名特判;不在任何启用渠道 model_map 的行保持 NULL;
// 重复 EnsureSchema 不重放(人工收窄不被覆盖)。
func TestMySQLEnsureSchemaBackfillsCapabilitiesOnce(t *testing.T) {
	_, db := openPricingTestDB(t)
	ctx := context.Background()

	// 造存量形态:88 前的 model_prices(无 capabilities 列)+ channels 表。
	if _, err := db.ExecContext(ctx, `DROP TABLE IF EXISTS model_prices`); err != nil {
		t.Fatalf("drop model_prices: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
CREATE TABLE model_prices (
	public_model VARCHAR(200) NOT NULL PRIMARY KEY,
	unit         VARCHAR(16)  NOT NULL,
	config       JSON         NOT NULL,
	created_at   TIMESTAMP(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
	updated_at   TIMESTAMP(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6) ON UPDATE CURRENT_TIMESTAMP(6)
) ENGINE = InnoDB DEFAULT CHARSET = utf8mb4`); err != nil {
		t.Fatalf("create legacy model_prices: %v", err)
	}
	t.Cleanup(func() {
		_, _ = db.ExecContext(ctx, `DROP TABLE IF EXISTS channels`)
	})
	if _, err := db.ExecContext(ctx, `DROP TABLE IF EXISTS channels`); err != nil {
		t.Fatalf("drop channels: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
CREATE TABLE channels (
	id       BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,
	name     VARCHAR(255) NOT NULL,
	type     VARCHAR(32)  NOT NULL,
	base_url VARCHAR(500) NOT NULL,
	api_key  TEXT         NOT NULL,
	model_map JSON        NOT NULL,
	capabilities JSON     NULL,
	config   JSON         NULL,
	priority INT          NOT NULL DEFAULT 0,
	weight   INT          NOT NULL DEFAULT 0,
	enabled  TINYINT(1)   NOT NULL DEFAULT 1,
	created_at TIMESTAMP(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
	updated_at TIMESTAMP(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6) ON UPDATE CURRENT_TIMESTAMP(6)
) ENGINE = InnoDB DEFAULT CHARSET = utf8mb4`); err != nil {
		t.Fatalf("create channels: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
INSERT INTO model_prices (public_model, unit, config) VALUES
	('og-image-2.5', 'call', '{"cny_per_call_micros":40000}'),
	('hunyuan-world-panorama', 'call', '{"cny_per_call_micros":50000}'),
	('vod-minimax-music-2.0-cover', 'call', '{"cny_per_call_micros":250000}'),
	('qwen-plus', 'token', '{}'),
	('nullcap-img', 'call', '{"cny_per_call_micros":30000}'),
	('orphan-m', 'call', '{"cny_per_call_micros":1000}')`); err != nil {
		t.Fatalf("seed legacy prices: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
INSERT INTO channels (name, type, base_url, api_key, model_map, capabilities, enabled) VALUES
	('img', 'openai', 'https://a.example', 'k', '{"og-image-2.5":"og-image-2.5","hunyuan-world-panorama":"hunyuan-world-panorama"}', '["images"]', 1),
	('bgm', 'tencent-vod', 'https://b.example', 'k', '{"vod-minimax-music-2.0-cover":"Music 2.0"}', '["music","sfx"]', 1),
	('chat', 'openai', 'https://c.example', 'k', '{"qwen-plus":"qwen-plus","nullcap-img":"nullcap-img"}', NULL, 1),
	('off', 'openai', 'https://d.example', 'k', '{"og-image-2.5":"og-image-2.5"}', '["chat"]', 0)`); err != nil {
		t.Fatalf("seed channels: %v", err)
	}

	store := pricing.NewMySQLStore(db)
	if err := store.EnsureSchema(ctx); err != nil {
		t.Fatalf("EnsureSchema on legacy db: %v", err)
	}

	want := map[string][]string{
		"og-image-2.5":                {"images"},       // 启用 images 渠道映射(停用渠道不计)
		"hunyuan-world-panorama":      {"panorama"},     // 单名特判(照并集会得 images)
		"vod-minimax-music-2.0-cover": {"music", "sfx"}, // 并集排序
		"qwen-plus":                   {"chat"},         // NULL 能力渠道按生效集贡献 chat(07 号票)
		"nullcap-img":                 nil,              // 非聊天模型剥掉默认 chat 后无并集 → 保持 NULL(显式开启原则)
		"orphan-m":                    nil,              // 不在任何启用渠道 model_map → 保持空
	}
	for model, caps := range want {
		p, err := store.ByModel(ctx, model)
		if err != nil {
			t.Fatalf("ByModel %s: %v", model, err)
		}
		if len(p.Capabilities) != len(caps) {
			t.Fatalf("%s capabilities = %v, want %v", model, p.Capabilities, caps)
		}
		for i := range caps {
			if p.Capabilities[i] != caps[i] {
				t.Fatalf("%s capabilities = %v, want %v", model, p.Capabilities, caps)
			}
		}
	}

	// 幂等:人工把 og-image-2.5 收窄成空后重复 EnsureSchema,收窄保留。
	if _, err := db.ExecContext(ctx,
		`UPDATE model_prices SET capabilities = NULL WHERE public_model = 'og-image-2.5'`); err != nil {
		t.Fatalf("manual narrowing: %v", err)
	}
	if err := store.EnsureSchema(ctx); err != nil {
		t.Fatalf("second EnsureSchema: %v", err)
	}
	p, err := store.ByModel(ctx, "og-image-2.5")
	if err != nil {
		t.Fatalf("ByModel after replay: %v", err)
	}
	if len(p.Capabilities) != 0 {
		t.Fatalf("backfill replayed over manual narrowing: %v, want empty", p.Capabilities)
	}
}
