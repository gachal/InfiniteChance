package channel_test

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"testing"
	"time"

	mysqldriver "github.com/go-sql-driver/mysql"

	"github.com/gachal/InfiniteChance/internal/channel"
)

// openTestDB connects to a dedicated throwaway database on the compose
// MySQL (host port 3307). MYSQL_TEST_DSN overrides the DSN. Tests skip when
// the database is unreachable so `go test ./...` stays green without infra.
func openTestDB(t *testing.T) (*channel.MySQLStore, *sql.DB) {
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
	dbName := cfg.DBName + "_channel"
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
	store := channel.NewMySQLStore(db)
	if err := store.EnsureSchema(ctx); err != nil {
		t.Fatalf("EnsureSchema: %v", err)
	}
	if _, err := db.ExecContext(ctx, "DELETE FROM channels"); err != nil {
		t.Fatalf("clean channels: %v", err)
	}
	t.Cleanup(func() {
		db.Close()
		server.Close()
	})
	return store, db
}

func sampleChannel(name string) channel.Channel {
	return channel.Channel{
		Name:    name,
		Type:    channel.TypeOpenAI,
		BaseURL: "https://api.openai.com/v1",
		APIKey:  "sk-vendor-" + name,
		ModelMap: map[string]string{
			"gpt-4o": "gpt-4o-2024-11-20",
		},
		Priority: 5,
		Weight:   1,
		Enabled:  true,
	}
}

func TestMySQLChannelSchemaIsIdempotent(t *testing.T) {
	store, _ := openTestDB(t)

	if err := store.EnsureSchema(context.Background()); err != nil {
		t.Fatalf("second EnsureSchema: %v", err)
	}
}

func TestMySQLChannelCreateGetRoundTrip(t *testing.T) {
	store, _ := openTestDB(t)
	ctx := context.Background()

	created, err := store.Create(ctx, sampleChannel("openai-main"))
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if created.ID <= 0 {
		t.Fatalf("id = %d, want auto-increment id", created.ID)
	}
	if created.CreatedAt.IsZero() || created.UpdatedAt.IsZero() {
		t.Error("timestamps should be set by the database")
	}

	got, err := store.Get(ctx, created.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Name != "openai-main" || got.APIKey != "sk-vendor-openai-main" {
		t.Errorf("row = %+v, want the stored channel back", got)
	}
	if got.ModelMap["gpt-4o"] != "gpt-4o-2024-11-20" {
		t.Errorf("model_map = %v, want the JSON round trip preserved", got.ModelMap)
	}
	if got.CreatedAt != created.CreatedAt {
		t.Errorf("created_at drifted across reads: %v vs %v", got.CreatedAt, created.CreatedAt)
	}

	if _, err := store.Get(ctx, 999999); !errors.Is(err, channel.ErrNotFound) {
		t.Errorf("Get missing err = %v, want ErrNotFound", err)
	}
}

func TestMySQLChannelUpdateKeepsOrReplacesSecret(t *testing.T) {
	store, _ := openTestDB(t)
	ctx := context.Background()

	created, err := store.Create(ctx, sampleChannel("openai-main"))
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	// 空密钥 = 保留原密钥。
	updated := sampleChannel("openai-main")
	updated.ID = created.ID
	updated.APIKey = ""
	updated.BaseURL = "https://api.deepseek.com/v1"
	updated.ModelMap = map[string]string{"deepseek-chat": "deepseek-chat"}
	got, err := store.Update(ctx, updated)
	if err != nil {
		t.Fatalf("Update with blank key: %v", err)
	}
	if got.APIKey != "sk-vendor-openai-main" {
		t.Errorf("api_key = %q, want kept", got.APIKey)
	}
	if got.BaseURL != "https://api.deepseek.com/v1" {
		t.Errorf("base_url = %q, want updated", got.BaseURL)
	}
	if _, ok := got.ModelMap["gpt-4o"]; ok {
		t.Errorf("model_map = %v, want full replacement", got.ModelMap)
	}

	// 新密钥 = 替换。
	replaced := updated
	replaced.APIKey = "sk-brand-new"
	got, err = store.Update(ctx, replaced)
	if err != nil {
		t.Fatalf("Update with new key: %v", err)
	}
	if got.APIKey != "sk-brand-new" {
		t.Errorf("api_key = %q, want replaced", got.APIKey)
	}

	missing := sampleChannel("ghost")
	missing.ID = 999999
	if _, err := store.Update(ctx, missing); !errors.Is(err, channel.ErrNotFound) {
		t.Errorf("Update missing err = %v, want ErrNotFound", err)
	}
}

func TestMySQLChannelUpdateWithIdenticalValuesStillSucceeds(t *testing.T) {
	store, _ := openTestDB(t)
	ctx := context.Background()

	created, err := store.Create(ctx, sampleChannel("stable"))
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	// MySQL 的 UPDATE 只计「被更改」的行:内容完全一致的更新
	// 不能被误判为不存在。
	again := sampleChannel("stable")
	again.ID = created.ID
	got, err := store.Update(ctx, again)
	if err != nil {
		t.Fatalf("Update with identical values: %v", err)
	}
	if got.ID != created.ID || got.APIKey != "sk-vendor-stable" {
		t.Errorf("row = %+v, want the same channel back", got)
	}
}

func TestMySQLChannelListOrdersByPriorityThenID(t *testing.T) {
	store, _ := openTestDB(t)
	ctx := context.Background()

	highA := sampleChannel("high-a")
	highA.Priority = 9
	highB := sampleChannel("high-b")
	highB.Priority = 9
	low := sampleChannel("low")
	low.Priority = 1
	for _, ch := range []channel.Channel{highA, highB, low} {
		if _, err := store.Create(ctx, ch); err != nil {
			t.Fatalf("Create %s: %v", ch.Name, err)
		}
	}

	got, err := store.List(ctx)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	// 优先级降序,同优先级按 id 升序:high-a 先于 high-b。
	want := []string{"high-a", "high-b", "low"}
	if len(got) != len(want) {
		t.Fatalf("List = %d rows, want %d", len(got), len(want))
	}
	for i, name := range want {
		if got[i].Name != name {
			t.Errorf("row %d = %s, want %s", i, got[i].Name, name)
		}
	}
}

func TestMySQLChannelDelete(t *testing.T) {
	store, _ := openTestDB(t)
	ctx := context.Background()

	created, err := store.Create(ctx, sampleChannel("doomed"))
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := store.Delete(ctx, created.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := store.Get(ctx, created.ID); !errors.Is(err, channel.ErrNotFound) {
		t.Errorf("Get after delete err = %v, want ErrNotFound", err)
	}
	if err := store.Delete(ctx, created.ID); !errors.Is(err, channel.ErrNotFound) {
		t.Errorf("second Delete err = %v, want ErrNotFound", err)
	}
}

func TestMySQLChannelCapabilitiesRoundTrip(t *testing.T) {
	store, _ := openTestDB(t)
	ctx := context.Background()

	// 显式能力集落库后原样读回;HasCapability 是调度唯一入口。
	ch, err := store.Create(ctx, sampleChannel("caps"))
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	got, err := store.Get(ctx, ch.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if len(got.Capabilities) != 0 || !got.HasCapability(channel.CapChat) || got.HasCapability(channel.CapImages) {
		t.Fatalf("legacy row = %+v, want chat-only via the nil default", got.Capabilities)
	}

	ch.Capabilities = []channel.Capability{channel.CapChat, channel.CapImages}
	if _, err := store.Update(ctx, ch); err != nil {
		t.Fatalf("Update: %v", err)
	}
	got, err = store.Get(ctx, ch.ID)
	if err != nil {
		t.Fatalf("Get after update: %v", err)
	}
	if !got.HasCapability(channel.CapImages) || !got.HasCapability(channel.CapChat) {
		t.Errorf("stored capabilities = %v, want both honored after reload", got.Capabilities)
	}

	// 能力可收回:改回仅生图后,聊天不再命中。
	ch.Capabilities = []channel.Capability{channel.CapImages}
	if _, err := store.Update(ctx, ch); err != nil {
		t.Fatalf("Update revoke chat: %v", err)
	}
	got, err = store.Get(ctx, ch.ID)
	if err != nil {
		t.Fatalf("Get after revoke: %v", err)
	}
	if got.HasCapability(channel.CapChat) || !got.HasCapability(channel.CapImages) {
		t.Errorf("after revoke = %v, want images only", got.Capabilities)
	}
}
