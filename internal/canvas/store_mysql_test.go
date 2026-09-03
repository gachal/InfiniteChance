package canvas_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"reflect"
	"strconv"
	"sync"
	"testing"
	"time"

	mysqldriver "github.com/go-sql-driver/mysql"

	"github.com/gachal/InfiniteChance/internal/canvas"
)

// openTestStore connects to a dedicated throwaway database on the compose
// MySQL (host port 3307). MYSQL_TEST_DSN overrides the DSN. Tests skip when
// the database is unreachable so `go test ./...` stays green without infra;
// with infra up, the store's real SQL and optimistic-lock semantics are
// what's under test.
func openTestStore(t *testing.T) (*canvas.MySQLStore, *sql.DB) {
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
	dbName := cfg.DBName + "_canvas"
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
	if _, err := server.ExecContext(ctx, "DROP TABLE IF EXISTS `"+dbName+"`.`canvases`"); err != nil {
		t.Fatalf("drop stale table: %v", err)
	}

	cfg.DBName = dbName
	db, err := sql.Open("mysql", cfg.FormatDSN())
	if err != nil {
		t.Fatalf("open test database: %v", err)
	}
	store := canvas.NewMySQLStore(db)
	if err := store.EnsureSchema(ctx); err != nil {
		t.Fatalf("EnsureSchema: %v", err)
	}
	t.Cleanup(func() {
		db.Close()
		server.Close()
	})
	return store, db
}

func mustCreate(t *testing.T, store *canvas.MySQLStore, name string) canvas.Canvas {
	t.Helper()
	c, err := store.Create(context.Background(), name, []byte(`{"nodes":[],"edges":[]}`))
	if err != nil {
		t.Fatalf("create %q: %v", name, err)
	}
	return c
}

// graphEqual compares two graph documents semantically: the MySQL JSON
// column normalizes key order and spacing, so the stored contract is
// document equality, not byte equality.
func graphEqual(t *testing.T, got []byte, want string) {
	t.Helper()
	var gotDoc, wantDoc any
	if err := json.Unmarshal(got, &gotDoc); err != nil {
		t.Fatalf("stored graph is not valid JSON (%v): %s", err, got)
	}
	if err := json.Unmarshal([]byte(want), &wantDoc); err != nil {
		t.Fatalf("test fixture is not valid JSON: %v", err)
	}
	if !reflect.DeepEqual(gotDoc, wantDoc) {
		t.Fatalf("graph = %s, want document-equal to %s", got, want)
	}
}

func TestCreateStartsAtVersionOneWithGivenGraph(t *testing.T) {
	store, _ := openTestStore(t)

	c := mustCreate(t, store, "第一张画布")

	if c.ID == 0 {
		t.Fatal("created canvas has zero id")
	}
	if c.Version != 1 {
		t.Fatalf("version = %d, want 1", c.Version)
	}
	if string(c.Graph) == "" {
		t.Fatal("created canvas has empty graph")
	}
	graphEqual(t, c.Graph, `{"nodes":[],"edges":[]}`)
	if c.CreatedAt.IsZero() || c.UpdatedAt.IsZero() {
		t.Fatal("created canvas missing timestamps")
	}
}

func TestListOrdersRecentFirst(t *testing.T) {
	store, _ := openTestStore(t)

	first := mustCreate(t, store, "早")
	time.Sleep(10 * time.Millisecond) // TIMESTAMP(6) 粒度足够,仍留出可见间隔
	second := mustCreate(t, store, "晚")

	list, err := store.List(context.Background())
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("len(list) = %d, want 2", len(list))
	}
	if list[0].ID != second.ID || list[1].ID != first.ID {
		t.Fatalf("list order = [%d %d], want most recent first", list[0].ID, list[1].ID)
	}
}

func TestGetRenameDeleteRoundTrip(t *testing.T) {
	store, _ := openTestStore(t)
	c := mustCreate(t, store, "旧名")
	ctx := context.Background()

	got, err := store.Get(ctx, c.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Name != "旧名" || got.Version != 1 {
		t.Fatalf("get = %+v", got)
	}

	renamed, err := store.Rename(ctx, c.ID, "新名")
	if err != nil {
		t.Fatalf("rename: %v", err)
	}
	if renamed.Name != "新名" {
		t.Fatalf("renamed.Name = %q, want 新名", renamed.Name)
	}
	// 改名不动图,也不推进版本:版本号只由 SaveGraph 推进,
	// 否则并发标签页的自动保存会被无关的重命名打断。
	if renamed.Version != 1 {
		t.Fatalf("renamed.Version = %d, want 1", renamed.Version)
	}

	if err := store.Delete(ctx, c.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := store.Get(ctx, c.ID); !errors.Is(err, canvas.ErrNotFound) {
		t.Fatalf("get after delete = %v, want ErrNotFound", err)
	}
}

func TestRenameSameNameStillSucceeds(t *testing.T) {
	store, _ := openTestStore(t)
	c := mustCreate(t, store, "同名")
	ctx := context.Background()

	// MySQL 默认只把「值真的变了」的行算 affected;同名改名若按
	// affected==0 判缺失会误报不存在,必须仍成功。
	if _, err := store.Rename(ctx, c.ID, "同名"); err != nil {
		t.Fatalf("rename to same name: %v", err)
	}
}

func TestMissingIDsAnswerNotFound(t *testing.T) {
	store, _ := openTestStore(t)
	ctx := context.Background()
	const missing = int64(424242)

	if _, err := store.Get(ctx, missing); !errors.Is(err, canvas.ErrNotFound) {
		t.Fatalf("get = %v, want ErrNotFound", err)
	}
	if _, err := store.Rename(ctx, missing, "x"); !errors.Is(err, canvas.ErrNotFound) {
		t.Fatalf("rename = %v, want ErrNotFound", err)
	}
	if err := store.Delete(ctx, missing); !errors.Is(err, canvas.ErrNotFound) {
		t.Fatalf("delete = %v, want ErrNotFound", err)
	}
	if _, err := store.SaveGraph(ctx, missing, []byte(`{}`), 1); !errors.Is(err, canvas.ErrNotFound) {
		t.Fatalf("save = %v, want ErrNotFound", err)
	}
}

func TestSaveGraphBumpsVersionAndStoresDocument(t *testing.T) {
	store, _ := openTestStore(t)
	c := mustCreate(t, store, "画")

	saved, err := store.SaveGraph(context.Background(), c.ID,
		[]byte(`{"nodes":[{"id":"n1"}],"edges":[]}`), 1)
	if err != nil {
		t.Fatalf("save: %v", err)
	}
	if saved.Version != 2 {
		t.Fatalf("saved.Version = %d, want 2", saved.Version)
	}
	graphEqual(t, saved.Graph, `{"nodes":[{"id":"n1"}],"edges":[]}`)

	got, err := store.Get(context.Background(), c.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Version != 2 {
		t.Fatalf("get after save = v%d", got.Version)
	}
	graphEqual(t, got.Graph, `{"nodes":[{"id":"n1"}],"edges":[]}`)
}

func TestSaveGraphStaleVersionConflicts(t *testing.T) {
	store, _ := openTestStore(t)
	c := mustCreate(t, store, "画")
	ctx := context.Background()

	if _, err := store.SaveGraph(ctx, c.ID, []byte(`{"v":2}`), 1); err != nil {
		t.Fatalf("first save: %v", err)
	}
	// 旧版本(1)再来保存:被乐观锁拒之门外。
	if _, err := store.SaveGraph(ctx, c.ID, []byte(`{"v":"stale"}`), 1); !errors.Is(err, canvas.ErrVersionConflict) {
		t.Fatalf("stale save = %v, want ErrVersionConflict", err)
	}
	// 新版本(2)继续保存:畅通。
	saved, err := store.SaveGraph(ctx, c.ID, []byte(`{"v":3}`), 2)
	if err != nil {
		t.Fatalf("save with current version: %v", err)
	}
	if saved.Version != 3 {
		t.Fatalf("saved.Version = %d, want 3", saved.Version)
	}
}

// 两个标签页并发保存同一画布:只有一个能以版本 1 成功,
// 输家拿到 ErrVersionConflict —— 验收标准「后保存者收到版本冲突」的库侧保证。
func TestSaveGraphConcurrentOnlyOneWinner(t *testing.T) {
	store, _ := openTestStore(t)
	c := mustCreate(t, store, "竞速")

	const racers = 8
	var wg sync.WaitGroup
	winner := make(chan int, 1)
	conflicts := make(chan struct{}, racers)
	for i := 1; i <= racers; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			_, err := store.SaveGraph(context.Background(), c.ID,
				[]byte(`{"winner":`+strconv.Itoa(n)+`}`), 1)
			switch {
			case err == nil:
				select {
				case winner <- n:
				default: // 第二个成功者:乐观锁失效
					t.Errorf("racer %d also saved with the same expected version", n)
				}
			case errors.Is(err, canvas.ErrVersionConflict):
				conflicts <- struct{}{}
			default:
				t.Errorf("racer %d: unexpected error: %v", n, err)
			}
		}(i)
	}
	wg.Wait()
	close(winner)
	close(conflicts)

	if len(winner) != 1 {
		t.Fatalf("winners = %d, want exactly 1", len(winner))
	}
	if len(conflicts) != racers-1 {
		t.Fatalf("conflicts = %d, want %d", len(conflicts), racers-1)
	}
}

// EnsureSchema 幂等:重复执行不报错,存量数据保留。
func TestEnsureSchemaIdempotent(t *testing.T) {
	store, db := openTestStore(t)
	c := mustCreate(t, store, "留存")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := store.EnsureSchema(ctx); err != nil {
		t.Fatalf("second EnsureSchema: %v", err)
	}
	got, err := store.Get(ctx, c.ID)
	if err != nil || got.Name != "留存" {
		t.Fatalf("get after re-ensure = %+v, %v", got, err)
	}
	var count int
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM canvases").Scan(&count); err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 1 {
		t.Fatalf("canvases count = %d, want 1", count)
	}
}
