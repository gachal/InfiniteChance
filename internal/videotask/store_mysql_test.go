package videotask_test

import (
	"context"
	"database/sql"
	"os"
	"reflect"
	"testing"
	"time"

	mysqldriver "github.com/go-sql-driver/mysql"

	"github.com/gachal/InfiniteChance/internal/videotask"
)

// openTaskTestDB connects to a dedicated throwaway database on the compose
// MySQL (host port 3307). MYSQL_TEST_DSN overrides the DSN. Tests skip when
// the database is unreachable so `go test ./...` stays green without infra.
func openTaskTestDB(t *testing.T) (*videotask.MySQLStore, *sql.DB) {
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
	dbName := cfg.DBName + "_videotask"
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
	store := videotask.NewMySQLStore(db)
	if err := store.EnsureSchema(ctx); err != nil {
		t.Fatalf("EnsureSchema: %v", err)
	}
	if _, err := db.ExecContext(ctx, "DELETE FROM video_tasks"); err != nil {
		t.Fatalf("clean video_tasks: %v", err)
	}
	t.Cleanup(func() {
		db.Close()
		server.Close()
	})
	return store, db
}

func seedTask(t *testing.T, store *videotask.MySQLStore) videotask.Task {
	t.Helper()
	id, err := videotask.NewID()
	if err != nil {
		t.Fatalf("NewID: %v", err)
	}
	task, err := store.Create(context.Background(), videotask.Task{
		ID: id, KeyID: 7, ChannelID: 3, ChannelName: "wan-main",
		PublicModel: "vid-m", UpstreamModel: "wan2.2-t2v", UpstreamTaskID: "vendor-1",
		Status: videotask.StatusQueued, Size: "720p", Seconds: 5,
		ReservedMicros: 500_000, ChargeMicros: 0,
		PriceSnapshot: []byte(`{"unit":"second"}`),
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	return task
}

func TestMySQLVideoTaskSchemaIsIdempotent(t *testing.T) {
	store, _ := openTaskTestDB(t)
	if err := store.EnsureSchema(context.Background()); err != nil {
		t.Fatalf("second EnsureSchema: %v", err)
	}
}

func TestMySQLVideoTaskCreateGetRoundTrip(t *testing.T) {
	store, _ := openTaskTestDB(t)
	ctx := context.Background()
	task := seedTask(t, store)

	if task.ID == "" || task.CreatedAt.IsZero() || task.UpdatedAt.IsZero() {
		t.Fatalf("task = %+v, want id and timestamps from the DB", task)
	}
	got, err := store.Get(ctx, task.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !reflect.DeepEqual(got, task) {
		t.Errorf("Get = %+v, want the created row %+v", got, task)
	}
	if _, err := store.Get(ctx, "vt_missing"); err != videotask.ErrNotFound {
		t.Errorf("Get missing = %v, want ErrNotFound", err)
	}

	// NULL 列(upstream_status/video_url/error)缺省往返。
	if got.UpstreamStatus != "" || got.VideoURL != "" || got.Error != "" {
		t.Errorf("null columns = %q/%q/%q, want empty", got.UpstreamStatus, got.VideoURL, got.Error)
	}
}

func TestMySQLVideoTaskUpdateGuardedTransition(t *testing.T) {
	store, _ := openTaskTestDB(t)
	ctx := context.Background()
	task := seedTask(t, store)

	url := "https://cdn.example/v.mp4"
	charge := task.ReservedMicros
	raw := "SUCCEEDED"
	got, won, err := store.Update(ctx, task.ID, videotask.ActiveStatuses, videotask.Patch{
		Status: videotask.StatusSucceeded, UpstreamStatus: &raw,
		VideoURL: &url, ChargeMicros: &charge,
	})
	if err != nil || !won {
		t.Fatalf("Update = won %v err %v, want the finalize to win", won, err)
	}
	if got.Status != videotask.StatusSucceeded || got.VideoURL != url || got.ChargeMicros != charge {
		t.Errorf("updated = %+v, want succeeded with url and charge", got)
	}

	// 终态后守卫必须挡住一切再迁移:本次 Update 拿回的是现状,won=false。
	msg := "late failure"
	got, won, err = store.Update(ctx, task.ID, videotask.ActiveStatuses, videotask.Patch{
		Status: videotask.StatusFailed, ErrMsg: &msg,
	})
	if err != nil {
		t.Fatalf("late Update: %v", err)
	}
	if won {
		t.Errorf("late update won, want the succeeded terminal state to hold")
	}
	if got.Status != videotask.StatusSucceeded || got.Error != "" {
		t.Errorf("row after lost race = %+v, want untouched succeeded task", got)
	}
}

func TestMySQLVideoTaskUpdateNilColumnsStay(t *testing.T) {
	store, _ := openTaskTestDB(t)
	ctx := context.Background()
	task := seedTask(t, store)

	// queued→running:只动状态与原始状态,URL/error/charge 原封。
	raw := "RUNNING"
	got, won, err := store.Update(ctx, task.ID, []videotask.Status{videotask.StatusQueued}, videotask.Patch{
		Status: videotask.StatusRunning, UpstreamStatus: &raw,
	})
	if err != nil || !won {
		t.Fatalf("Update = won %v err %v, want transition", won, err)
	}
	if got.Status != videotask.StatusRunning || got.UpstreamStatus != "RUNNING" ||
		got.VideoURL != "" || got.Error != "" || got.ChargeMicros != 0 {
		t.Errorf("updated = %+v, want running with untouched optional columns", got)
	}
}
