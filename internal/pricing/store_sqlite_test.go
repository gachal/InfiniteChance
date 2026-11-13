package pricing

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"github.com/gachal/InfiniteChance/internal/sqlitedb"
)

// newSQLiteTestDB opens a throwaway desktop-shaped database with the
// pricing store ensured. pricing 此前没有 SQLite store 测试(行为只靠
// migrations 对账兜底),88 号票动 SQLite Upsert 后按
// canvastask/store_sqlite_test.go 范式补上。
func newSQLiteTestDB(t *testing.T) (*SQLiteStore, *sql.DB) {
	t.Helper()
	db, err := sqlitedb.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	s := NewSQLiteStore(db)
	if err := s.EnsureSchema(context.Background()); err != nil {
		t.Fatalf("EnsureSchema: %v", err)
	}
	return s, db
}

// seedSQLiteCallRow writes one call-track row for the round-trip tests.
func seedSQLiteCallRow(t *testing.T, s *SQLiteStore, model string, caps []string) Price {
	t.Helper()
	stored, err := s.Upsert(context.Background(), Price{
		PublicModel: model, Unit: UnitCall, Capabilities: caps,
		Call: &CallPrice{CNYPerCallMicros: 40_000},
	})
	if err != nil {
		t.Fatalf("upsert %s: %v", model, err)
	}
	return stored
}

func TestSQLitePriceCapabilitiesRoundTrip(t *testing.T) {
	s, _ := newSQLiteTestDB(t)
	ctx := context.Background()

	stored := seedSQLiteCallRow(t, s, "og-image-2.5", []string{"images", "panorama"})
	if len(stored.Capabilities) != 2 || stored.Capabilities[0] != "images" || stored.Capabilities[1] != "panorama" {
		t.Fatalf("upsert capabilities = %v, want [images panorama]", stored.Capabilities)
	}
	byModel, err := s.ByModel(ctx, "og-image-2.5")
	if err != nil {
		t.Fatalf("ByModel: %v", err)
	}
	if len(byModel.Capabilities) != 2 {
		t.Fatalf("ByModel capabilities = %v, want [images panorama]", byModel.Capabilities)
	}

	// 空能力落 NULL,读回 nil = 不出现在任何下拉(合法状态)。
	stored = seedSQLiteCallRow(t, s, "plain-img", nil)
	if stored.Capabilities != nil {
		t.Fatalf("empty capabilities stored = %v, want nil", stored.Capabilities)
	}
	list, err := s.List(ctx)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("List = %d rows, want 2", len(list))
	}
	for _, p := range list {
		switch p.PublicModel {
		case "og-image-2.5":
			if !p.HasCapability(CapImages) || !p.HasCapability(CapPanorama) {
				t.Errorf("%s capabilities = %v, want images+panorama", p.PublicModel, p.Capabilities)
			}
		case "plain-img":
			if len(p.Capabilities) != 0 {
				t.Errorf("%s capabilities = %v, want empty", p.PublicModel, p.Capabilities)
			}
		}
	}
}

// SQLite Upsert 的 DO UPDATE WHERE 守卫(88 号票扩列):只改 capabilities
// 的更新必须落库且 updated_at 前进;值全同的重复 Upsert 不动 updated_at
// (MySQL 侧 ON UPDATE 语义对齐)。
func TestSQLiteUpsertCapabilitiesOnlyChangeBumpsUpdatedAt(t *testing.T) {
	s, _ := newSQLiteTestDB(t)
	ctx := context.Background()

	first := seedSQLiteCallRow(t, s, "og-image-2.5", []string{"images"})
	time.Sleep(5 * time.Millisecond) // TEXT 时间戳定宽毫秒,睡一下保证可比

	// 只改能力:WHERE 扩列后落库,updated_at 前进。
	second, err := s.Upsert(ctx, Price{
		PublicModel: "og-image-2.5", Unit: UnitCall, Capabilities: []string{"panorama"},
		Call: &CallPrice{CNYPerCallMicros: 40_000},
	})
	if err != nil {
		t.Fatalf("upsert capabilities-only change: %v", err)
	}
	if len(second.Capabilities) != 1 || second.Capabilities[0] != "panorama" {
		t.Fatalf("capabilities after capabilities-only change = %v, want [panorama]", second.Capabilities)
	}
	if !second.UpdatedAt.After(first.UpdatedAt) {
		t.Fatalf("updated_at %v not bumped after capabilities-only change (first %v)", second.UpdatedAt, first.UpdatedAt)
	}

	time.Sleep(5 * time.Millisecond)
	// 值全同:updated_at 保持不动。
	third, err := s.Upsert(ctx, Price{
		PublicModel: "og-image-2.5", Unit: UnitCall, Capabilities: []string{"panorama"},
		Call: &CallPrice{CNYPerCallMicros: 40_000},
	})
	if err != nil {
		t.Fatalf("idempotent upsert: %v", err)
	}
	if !third.UpdatedAt.Equal(second.UpdatedAt) {
		t.Fatalf("updated_at moved on identical upsert: %v → %v", second.UpdatedAt, third.UpdatedAt)
	}
}

// 88 号票一次性回填(加列分支):存量库(无 capabilities 列)跑
// EnsureSchema → 加列 + 按「启用渠道能力位并集」回填;panorama 单名特判;
// 不在任何启用渠道 model_map 的行保持 NULL;重复 EnsureSchema 不重放
// (人工收窄不被覆盖)。
func TestSQLiteEnsureSchemaBackfillsCapabilitiesOnce(t *testing.T) {
	db, err := sqlitedb.Open(filepath.Join(t.TempDir(), "legacy.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	ctx := context.Background()

	// 造存量形态:88 前的 model_prices(无 capabilities 列)+ channels 表。
	legacySchema := `
CREATE TABLE IF NOT EXISTS model_prices (
	public_model TEXT    NOT NULL PRIMARY KEY,
	unit         TEXT    NOT NULL,
	config       TEXT    NOT NULL,
	created_at   TEXT    NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%f000Z','now')),
	updated_at   TEXT    NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%f000Z','now'))
)`
	if _, err := db.ExecContext(ctx, legacySchema); err != nil {
		t.Fatalf("create legacy model_prices: %v", err)
	}
	// 时间戳显式写定宽 FormatTime:列 DEFAULT 的 %f 精度与 ParseTime 布局
	// 不一致,存量行回读会炸 —— 生产路径的行都是显式写的,测试同款。
	now := sqlitedb.FormatTime(time.Now())
	if _, err := db.ExecContext(ctx, `
INSERT INTO model_prices (public_model, unit, config, created_at, updated_at) VALUES
	('og-image-2.5', 'call', '{"cny_per_call_micros":40000}', ?, ?),
	('hunyuan-world-panorama', 'call', '{"cny_per_call_micros":50000}', ?, ?),
	('vod-minimax-music-2.0-cover', 'call', '{"cny_per_call_micros":250000}', ?, ?),
	('qwen-plus', 'token', '{}', ?, ?),
	('orphan-m', 'call', '{"cny_per_call_micros":1000}', ?, ?)`,
		now, now, now, now, now, now, now, now, now, now); err != nil {
		t.Fatalf("seed legacy prices: %v", err)
	}
	channelSchema := `
CREATE TABLE IF NOT EXISTS channels (
	id           INTEGER      PRIMARY KEY AUTOINCREMENT,
	name         TEXT         NOT NULL,
	type         TEXT         NOT NULL,
	base_url     TEXT         NOT NULL,
	api_key      TEXT         NOT NULL,
	model_map    TEXT         NOT NULL,
	capabilities TEXT         NULL,
	config       TEXT         NULL,
	priority     INTEGER      NOT NULL DEFAULT 0,
	weight       INTEGER      NOT NULL DEFAULT 0,
	enabled      INTEGER      NOT NULL DEFAULT 1,
	created_at   TEXT         NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%f000Z','now')),
	updated_at   TEXT         NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%f000Z','now'))
)`
	if _, err := db.ExecContext(ctx, channelSchema); err != nil {
		t.Fatalf("create channels: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
INSERT INTO channels (name, type, base_url, api_key, model_map, capabilities, enabled) VALUES
	('img', 'openai', 'https://a.example', 'k', '{"og-image-2.5":"og-image-2.5","hunyuan-world-panorama":"hunyuan-world-panorama"}', '["images"]', 1),
	('bgm', 'tencent-vod', 'https://b.example', 'k', '{"vod-minimax-music-2.0-cover":"Music 2.0"}', '["music","sfx"]', 1),
	('chat', 'openai', 'https://c.example', 'k', '{"qwen-plus":"qwen-plus"}', NULL, 1),
	('off', 'openai', 'https://d.example', 'k', '{"og-image-2.5":"og-image-2.5"}', '["chat"]', 0)`); err != nil {
		t.Fatalf("seed channels: %v", err)
	}

	s := NewSQLiteStore(db)
	if err := s.EnsureSchema(ctx); err != nil {
		t.Fatalf("EnsureSchema on legacy db: %v", err)
	}

	want := map[string][]string{
		"og-image-2.5":                {"images"},       // 启用 images 渠道映射(停用渠道不计)
		"hunyuan-world-panorama":      {"panorama"},     // 单名特判(照并集会得 images)
		"vod-minimax-music-2.0-cover": {"music", "sfx"}, // 并集排序
		"qwen-plus":                   nil,              // 渠道能力位 NULL 不贡献并集 → 保持空
		"orphan-m":                    nil,              // 不在任何启用渠道 model_map → 保持空
	}
	for model, caps := range want {
		p, err := s.ByModel(ctx, model)
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
	if err := s.EnsureSchema(ctx); err != nil {
		t.Fatalf("second EnsureSchema: %v", err)
	}
	p, err := s.ByModel(ctx, "og-image-2.5")
	if err != nil {
		t.Fatalf("ByModel after replay: %v", err)
	}
	if len(p.Capabilities) != 0 {
		t.Fatalf("backfill replayed over manual narrowing: %v, want empty", p.Capabilities)
	}
}
