package usage_test

import (
	"context"
	"database/sql"
	"os"
	"strings"
	"testing"
	"time"

	mysqldriver "github.com/go-sql-driver/mysql"

	"github.com/gachal/InfiniteChance/internal/usage"
)

// openUsageTestDB connects to a dedicated throwaway database on the compose
// MySQL (host port 3307). MYSQL_TEST_DSN overrides the DSN. Tests skip when
// the database is unreachable so `go test ./...` stays green without infra.
func openUsageTestDB(t *testing.T) (*usage.MySQLStore, *sql.DB) {
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
	dbName := cfg.DBName + "_usage"
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
	store := usage.NewMySQLStore(db)
	if err := store.EnsureSchema(ctx); err != nil {
		t.Fatalf("EnsureSchema: %v", err)
	}
	if _, err := db.ExecContext(ctx, "DELETE FROM usage_logs"); err != nil {
		t.Fatalf("clean usage_logs: %v", err)
	}
	t.Cleanup(func() {
		db.Close()
		server.Close()
	})
	return store, db
}

func TestMySQLUsageSchemaIsIdempotent(t *testing.T) {
	store, _ := openUsageTestDB(t)

	if err := store.EnsureSchema(context.Background()); err != nil {
		t.Fatalf("second EnsureSchema: %v", err)
	}
}

func TestMySQLUsageInsertSuccessAndFailureRows(t *testing.T) {
	store, db := openUsageTestDB(t)
	ctx := context.Background()

	success, err := store.Insert(ctx, usage.Log{
		KeyID: 7, ChannelID: 3, ChannelName: "deepseek-main",
		PublicModel: "deepseek-chat", UpstreamModel: "deepseek-chat-upstream",
		Unit: "token", PromptTokens: 120, CompletionTokens: 340,
		DurationMS: 1234, Status: usage.StatusSuccess, ChargeMicros: 21_250,
		PriceSnapshot: []byte(`{"unit":"token","token":{"input_micros_per_mtokens":440000}}`),
	})
	if err != nil {
		t.Fatalf("Insert success row: %v", err)
	}
	if success.ID <= 0 || success.CreatedAt.IsZero() {
		t.Fatalf("row = %+v, want id and created_at from the DB", success)
	}

	failure, err := store.Insert(ctx, usage.Log{
		KeyID: 7, ChannelID: 3, ChannelName: "deepseek-main",
		PublicModel: "deepseek-chat", UpstreamModel: "deepseek-chat-upstream",
		Unit: "token", DurationMS: 87, Status: usage.StatusUpstreamError,
		UpstreamError: "502: upstream exploded",
	})
	if err != nil {
		t.Fatalf("Insert failure row: %v", err)
	}
	if failure.ChargeMicros != 0 {
		t.Errorf("failure charge = %d, want 0", failure.ChargeMicros)
	}

	// NULL 列可往返:无快照、无错误摘要的行也能落库。
	bare, err := store.Insert(ctx, usage.Log{
		KeyID: 8, ChannelID: 4, ChannelName: "x", PublicModel: "m", UpstreamModel: "m",
		Unit: "token", Status: usage.StatusSuccess,
	})
	if err != nil {
		t.Fatalf("Insert bare row: %v", err)
	}

	var snapshot, upstreamErr sql.NullString
	var status, channelName string
	err = db.QueryRowContext(ctx,
		`SELECT status, channel_name, price_snapshot, upstream_error FROM usage_logs WHERE id = ?`, bare.ID).
		Scan(&status, &channelName, &snapshot, &upstreamErr)
	if err != nil {
		t.Fatalf("scan bare row: %v", err)
	}
	if snapshot.Valid || upstreamErr.Valid {
		t.Errorf("bare row snapshot/error = %q/%q, want NULL", snapshot.String, upstreamErr.String)
	}
	if status != usage.StatusSuccess || channelName != "x" {
		t.Errorf("bare row = %s/%s, want success/x", status, channelName)
	}

	// 成功行带快照、无错误摘要;失败行反之。
	var gotSnapshot []byte
	var gotErr *string
	if err := db.QueryRowContext(ctx,
		`SELECT price_snapshot, upstream_error FROM usage_logs WHERE id = ?`, success.ID).
		Scan(&gotSnapshot, &gotErr); err != nil {
		t.Fatalf("scan success row: %v", err)
	}
	if len(gotSnapshot) == 0 || !strings.Contains(string(gotSnapshot), "token") {
		t.Errorf("success row snapshot = %q, want pricing JSON", gotSnapshot)
	}
	if gotErr != nil {
		t.Errorf("success row upstream_error = %q, want NULL", *gotErr)
	}
}
