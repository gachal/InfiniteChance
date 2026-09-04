package usage_test

import (
	"context"
	"database/sql"
	"os"
	"reflect"
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

// seedAuditRows inserts the audit fixture: five rows across two keys,
// two channels, three models and three UTC days. Insert stamps created_at
// itself, so the test pins the timestamps afterwards via UPDATE.
func seedAuditRows(t *testing.T, store *usage.MySQLStore, db *sql.DB) {
	t.Helper()
	ctx := context.Background()

	rows := []struct {
		entry usage.Log
		at    time.Time
	}{
		{usage.Log{KeyID: 7, ChannelID: 3, ChannelName: "c3", PublicModel: "deepseek-chat",
			UpstreamModel: "ds-up", Unit: "token", PromptTokens: 120, CompletionTokens: 340,
			DurationMS: 900, Status: usage.StatusSuccess, ChargeMicros: 100}, time.Date(2026, 9, 1, 10, 0, 0, 0, time.UTC)},
		{usage.Log{KeyID: 8, ChannelID: 3, ChannelName: "c3", PublicModel: "deepseek-chat",
			UpstreamModel: "ds-up", Unit: "token", DurationMS: 80,
			Status: usage.StatusUpstreamError, UpstreamError: "502: bad gateway",
			Source: "canvas=1 task=ct_a node=n1"}, time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)},
		{usage.Log{KeyID: 7, ChannelID: 4, ChannelName: "c4", PublicModel: "flux-pro",
			UpstreamModel: "flux-up", Unit: "call", DurationMS: 4000,
			Status: usage.StatusSuccess, ChargeMicros: 250,
			PriceSnapshot: []byte(`{"unit":"call","request":{"size":"1024x1024","n":1}}`)}, time.Date(2026, 9, 2, 8, 0, 0, 0, time.UTC)},
		{usage.Log{KeyID: 7, ChannelID: 4, ChannelName: "c4", PublicModel: "flux-pro",
			UpstreamModel: "flux-up", Unit: "call", DurationMS: 4100,
			Status: usage.StatusSuccess, ChargeMicros: 500,
			PriceSnapshot: []byte(`{"unit":"call","request":{"size":"1792x1024","n":2}}`),
			Source:        "canvas=2 task=ct_b node=n2"}, time.Date(2026, 9, 2, 9, 0, 0, 0, time.UTC)},
		{usage.Log{KeyID: 8, ChannelID: 3, ChannelName: "c3", PublicModel: "video-x",
			UpstreamModel: "vid-up", Unit: "second", DurationMS: 60000,
			Status: usage.StatusUpstreamError, UpstreamError: "canceled by client",
			Source: "canvas=1 task=ct_c node=n3"}, time.Date(2026, 9, 3, 23, 30, 0, 0, time.UTC)},
	}
	for i, r := range rows {
		if _, err := store.Insert(ctx, r.entry); err != nil {
			t.Fatalf("seed row %d: %v", i, err)
		}
		if _, err := db.ExecContext(ctx,
			`UPDATE usage_logs SET created_at = ? WHERE public_model = ? AND charge_micros = ?`,
			r.at, r.entry.PublicModel, r.entry.ChargeMicros); err != nil {
			t.Fatalf("seed created_at %d: %v", i, err)
		}
	}
}

func TestMySQLUsageListFiltersAndPaginates(t *testing.T) {
	store, db := openUsageTestDB(t)
	seedAuditRows(t, store, db)
	ctx := context.Background()

	// 全量:最新在前,total 是过滤后的总数而非本页行数。
	page, err := store.List(ctx, usage.Filter{}, 10, 0)
	if err != nil {
		t.Fatalf("List all: %v", err)
	}
	if page.Total != 5 {
		t.Errorf("total = %d, want 5", page.Total)
	}
	wantOrder := []string{"video-x", "flux-pro", "flux-pro", "deepseek-chat", "deepseek-chat"}
	gotOrder := make([]string, 0, len(page.Logs))
	for _, l := range page.Logs {
		gotOrder = append(gotOrder, l.PublicModel)
	}
	if !reflect.DeepEqual(gotOrder, wantOrder) {
		t.Errorf("order = %v, want %v", gotOrder, wantOrder)
	}

	// 逐个维度过滤:每个谓词只留自己的行。
	cases := []struct {
		name   string
		filter usage.Filter
		total  int64
	}{
		{"by key", usage.Filter{KeyID: 7}, 3},
		{"by channel", usage.Filter{ChannelID: 3}, 3},
		{"by model", usage.Filter{Model: "flux-pro"}, 2},
		{"by status", usage.Filter{Status: usage.StatusUpstreamError}, 2},
		{"canvas source", usage.Filter{Source: usage.SourceCanvas}, 3},
		{"direct source", usage.Filter{Source: usage.SourceDirect}, 2},
	}
	for _, tc := range cases {
		page, err := store.List(ctx, tc.filter, 10, 0)
		if err != nil {
			t.Fatalf("List %s: %v", tc.name, err)
		}
		if page.Total != tc.total || int64(len(page.Logs)) != tc.total {
			t.Errorf("%s: total = %d, rows = %d, want %d", tc.name, page.Total, len(page.Logs), tc.total)
		}
	}

	// 时间窗:from 含端点,to 不含。
	from := time.Date(2026, 9, 2, 0, 0, 0, 0, time.UTC)
	to := time.Date(2026, 9, 3, 0, 0, 0, 0, time.UTC)
	page, err = store.List(ctx, usage.Filter{From: &from, To: &to}, 10, 0)
	if err != nil {
		t.Fatalf("List window: %v", err)
	}
	if page.Total != 2 {
		t.Errorf("window total = %d, want 2 (09-02 的两行)", page.Total)
	}

	// 分页:limit/offset 切的是同一排序结果。
	page, err = store.List(ctx, usage.Filter{}, 2, 1)
	if err != nil {
		t.Fatalf("List page: %v", err)
	}
	if page.Total != 5 || len(page.Logs) != 2 {
		t.Fatalf("page = total %d, %d rows, want total 5, 2 rows", page.Total, len(page.Logs))
	}
	if page.Logs[0].PublicModel != "flux-pro" || page.Logs[1].PublicModel != "flux-pro" {
		t.Errorf("page rows = %s/%s, want 第 2、3 行(flux-pro/flux-pro)",
			page.Logs[0].PublicModel, page.Logs[1].PublicModel)
	}
}

func TestMySQLUsageSummaryReconcilesWithList(t *testing.T) {
	store, db := openUsageTestDB(t)
	seedAuditRows(t, store, db)
	ctx := context.Background()

	// 按天:数字与全量明细的手工汇总一致,最新的天在前。
	buckets, err := store.Summary(ctx, usage.ByDay, usage.Filter{})
	if err != nil {
		t.Fatalf("Summary by day: %v", err)
	}
	want := []usage.Bucket{
		{Day: "2026-09-03", Requests: 1, Errors: 1},
		{Day: "2026-09-02", Requests: 2, Errors: 0, ChargeMicros: 750},
		{Day: "2026-09-01", Requests: 2, Errors: 1, ChargeMicros: 100},
	}
	if !reflect.DeepEqual(buckets, want) {
		t.Errorf("by day = %+v, want %+v", buckets, want)
	}

	// 按模型:扣费降序(成本结构一眼可见),并列按名字稳定排序。
	buckets, err = store.Summary(ctx, usage.ByModel, usage.Filter{})
	if err != nil {
		t.Fatalf("Summary by model: %v", err)
	}
	wantModel := []usage.Bucket{
		{Model: "flux-pro", Requests: 2, ChargeMicros: 750},
		{Model: "deepseek-chat", Requests: 2, Errors: 1, ChargeMicros: 100},
		{Model: "video-x", Requests: 1, Errors: 1},
	}
	if !reflect.DeepEqual(buckets, wantModel) {
		t.Errorf("by model = %+v, want %+v", buckets, wantModel)
	}

	// 按渠道:名字取该渠道行里的快照。
	buckets, err = store.Summary(ctx, usage.ByChannel, usage.Filter{})
	if err != nil {
		t.Fatalf("Summary by channel: %v", err)
	}
	wantChannel := []usage.Bucket{
		{ChannelID: 4, ChannelName: "c4", Requests: 2, ChargeMicros: 750},
		{ChannelID: 3, ChannelName: "c3", Requests: 3, Errors: 2, ChargeMicros: 100},
	}
	if !reflect.DeepEqual(buckets, wantChannel) {
		t.Errorf("by channel = %+v, want %+v", buckets, wantChannel)
	}

	// 过滤同样作用于汇总:key 7 从 09-02 起只剩自己的两行。
	from := time.Date(2026, 9, 2, 0, 0, 0, 0, time.UTC)
	buckets, err = store.Summary(ctx, usage.ByDay, usage.Filter{KeyID: 7, From: &from})
	if err != nil {
		t.Fatalf("Summary filtered: %v", err)
	}
	wantFiltered := []usage.Bucket{
		{Day: "2026-09-02", Requests: 2, ChargeMicros: 750},
	}
	if !reflect.DeepEqual(buckets, wantFiltered) {
		t.Errorf("filtered by day = %+v, want %+v", buckets, wantFiltered)
	}

	// 画布来源过滤 + 按模型:只有画布侧的行进桶。
	buckets, err = store.Summary(ctx, usage.ByModel, usage.Filter{Source: usage.SourceCanvas})
	if err != nil {
		t.Fatalf("Summary canvas: %v", err)
	}
	wantCanvas := []usage.Bucket{
		{Model: "flux-pro", Requests: 1, ChargeMicros: 500},
		{Model: "deepseek-chat", Requests: 1, Errors: 1},
		{Model: "video-x", Requests: 1, Errors: 1},
	}
	if !reflect.DeepEqual(buckets, wantCanvas) {
		t.Errorf("canvas by model = %+v, want %+v", buckets, wantCanvas)
	}
}
