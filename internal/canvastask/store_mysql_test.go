package canvastask_test

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	mysqldriver "github.com/go-sql-driver/mysql"

	"github.com/gachal/InfiniteChance/internal/asset"
	"github.com/gachal/InfiniteChance/internal/canvastask"
)

// openTaskTestDB connects to a dedicated throwaway database on the compose
// MySQL (host port 3307). MYSQL_TEST_DSN overrides the DSN. Tests skip when
// the database is unreachable so `go test ./...` stays green without infra.
func openTaskTestDB(t *testing.T) (*canvastask.MySQLStore, *sql.DB) {
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
	dbName := cfg.DBName + "_canvastask"
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
	assets := asset.NewMySQLStore(db)
	if err := assets.EnsureSchema(ctx); err != nil {
		t.Fatalf("ensure asset schema: %v", err)
	}
	store := canvastask.NewMySQLStore(db, assets)
	if err := store.EnsureSchema(ctx); err != nil {
		t.Fatalf("EnsureSchema: %v", err)
	}
	if _, err := db.ExecContext(ctx, "DELETE FROM canvas_tasks"); err != nil {
		t.Fatalf("clean canvas_tasks: %v", err)
	}
	if _, err := db.ExecContext(ctx, "DELETE FROM assets"); err != nil {
		t.Fatalf("clean assets: %v", err)
	}
	t.Cleanup(func() {
		db.Close()
		server.Close()
	})
	return store, db
}

func seedTask(t *testing.T, store *canvastask.MySQLStore, canvasID int64, nodeID string, status canvastask.Status) canvastask.Task {
	t.Helper()
	id, err := canvastask.NewID()
	if err != nil {
		t.Fatalf("NewID: %v", err)
	}
	task, err := store.Create(context.Background(), canvastask.Task{
		ID: id, CanvasID: canvasID, NodeID: nodeID, Kind: canvastask.KindImage,
		Prompt: "一只在月光下奔跑的猫", Model: "img-m", Size: "1024x1024",
		Status: status,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	return task
}

func TestMySQLCanvasTaskSchemaIsIdempotent(t *testing.T) {
	store, _ := openTaskTestDB(t)
	if err := store.EnsureSchema(context.Background()); err != nil {
		t.Fatalf("second EnsureSchema: %v", err)
	}
}

func TestMySQLCanvasTaskCreateGetRoundTrip(t *testing.T) {
	store, _ := openTaskTestDB(t)
	ctx := context.Background()
	task := seedTask(t, store, 7, "image-1-1", canvastask.StatusQueued)

	if task.ID == "" || !strings.HasPrefix(task.ID, "ct_") || task.CreatedAt.IsZero() {
		t.Fatalf("task = %+v, want a ct_ id and timestamps from the DB", task)
	}
	if task.Status != canvastask.StatusQueued || task.Attempts != 0 ||
		task.Error != "" || task.AssetID != 0 || task.ImageURL != "" {
		t.Errorf("created = %+v, want a fresh queued task with empty result columns", task)
	}
	got, err := store.Get(ctx, task.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got != task {
		t.Errorf("Get = %+v, want the created row %+v", got, task)
	}
	if _, err := store.Get(ctx, "ct_missing"); !errors.Is(err, canvastask.ErrNotFound) {
		t.Errorf("Get missing = %v, want ErrNotFound", err)
	}
}

func TestMySQLCanvasTaskListByCanvasNewestFirst(t *testing.T) {
	store, _ := openTaskTestDB(t)
	ctx := context.Background()

	first := seedTask(t, store, 7, "image-1-1", canvastask.StatusSucceeded)
	other := seedTask(t, store, 8, "image-9-9", canvastask.StatusQueued)
	second := seedTask(t, store, 7, "image-1-2", canvastask.StatusFailed)

	tasks, err := store.ListByCanvas(ctx, 7, 10)
	if err != nil {
		t.Fatalf("ListByCanvas: %v", err)
	}
	if len(tasks) != 2 || tasks[0].ID != second.ID || tasks[1].ID != first.ID {
		t.Fatalf("tasks = %+v, want canvas 7's two rows newest first", tasks)
	}
	tasks, err = store.ListByCanvas(ctx, 8, 10)
	if err != nil || len(tasks) != 1 || tasks[0].ID != other.ID {
		t.Fatalf("tasks = %+v err %v, want only canvas 8's row", tasks, err)
	}

	tasks, err = store.ListByCanvas(ctx, 7, 1)
	if err != nil || len(tasks) != 1 || tasks[0].ID != second.ID {
		t.Fatalf("limited tasks = %+v err %v, want only the newest row", tasks, err)
	}
}

func TestMySQLCanvasTaskClaimIsFIFOAndExclusive(t *testing.T) {
	store, _ := openTaskTestDB(t)
	ctx := context.Background()

	older := seedTask(t, store, 7, "image-1-1", canvastask.StatusQueued)
	newer := seedTask(t, store, 7, "image-1-2", canvastask.StatusQueued)

	first, err := store.Claim(ctx)
	if err != nil {
		t.Fatalf("first Claim: %v", err)
	}
	if first.ID != older.ID || first.Status != canvastask.StatusRunning || first.Attempts != 1 {
		t.Errorf("first claim = %+v, want the older task running with attempts=1", first)
	}
	second, err := store.Claim(ctx)
	if err != nil {
		t.Fatalf("second Claim: %v", err)
	}
	if second.ID != newer.ID || second.Attempts != 1 {
		t.Errorf("second claim = %+v, want the newer task with attempts=1", second)
	}
	if _, err := store.Claim(ctx); !errors.Is(err, canvastask.ErrNotFound) {
		t.Errorf("empty-queue Claim = %v, want ErrNotFound", err)
	}

	// 重试后的任务再次认领:attempts 继续累加。先走失败路径才能重试。
	if _, _, err := store.FinalizeFailure(ctx, older.ID, "boom"); err != nil {
		t.Fatalf("FinalizeFailure: %v", err)
	}
	if _, err := store.ResetForRetry(ctx, older.ID, 7); err != nil {
		t.Fatalf("ResetForRetry: %v", err)
	}
	retried, err := store.Claim(ctx)
	if err != nil || retried.ID != older.ID || retried.Attempts != 2 {
		t.Errorf("retried claim = %+v err %v, want attempts=2", retried, err)
	}
}

func TestMySQLCanvasTaskFinalizeSuccessCreatesAssetAtomically(t *testing.T) {
	store, db := openTaskTestDB(t)
	ctx := context.Background()
	task := seedTask(t, store, 7, "image-1-1", canvastask.StatusQueued)
	if _, err := store.Claim(ctx); err != nil {
		t.Fatalf("Claim: %v", err)
	}

	finalized, won, err := store.FinalizeSuccess(ctx, task.ID, asset.Asset{
		Kind: asset.KindImage, CanvasID: task.CanvasID, TaskID: task.ID,
		Model: task.Model, Prompt: task.Prompt, URL: "https://img.example/cat.png",
	})
	if err != nil || !won {
		t.Fatalf("FinalizeSuccess = won %v err %v, want the finalize to win", won, err)
	}
	if finalized.Status != canvastask.StatusSucceeded || finalized.AssetID == 0 ||
		finalized.ImageURL != "https://img.example/cat.png" {
		t.Errorf("finalized = %+v, want succeeded with asset and url", finalized)
	}

	// 素材行与任务行同生共死:素材在,回填字段齐全。
	a, err := asset.NewMySQLStore(db).Get(ctx, finalized.AssetID)
	if err != nil {
		t.Fatalf("asset Get: %v", err)
	}
	if a.TaskID != task.ID || a.CanvasID != 7 || a.URL != finalized.ImageURL {
		t.Errorf("asset = %+v, want the task's provenance and url", a)
	}

	// 终态后守卫挡住迟到的失败:状态不再迁移。
	got, won, err := store.FinalizeFailure(ctx, task.ID, "late failure")
	if err != nil {
		t.Fatalf("late FinalizeFailure: %v", err)
	}
	if won || got.Status != canvastask.StatusSucceeded {
		t.Errorf("late finalize = won %v status %v, want the succeeded terminal state to hold", won, got.Status)
	}
}

func TestMySQLCanvasTaskFinalizeFailureRecordsReason(t *testing.T) {
	store, _ := openTaskTestDB(t)
	ctx := context.Background()
	task := seedTask(t, store, 7, "image-1-1", canvastask.StatusQueued)
	if _, err := store.Claim(ctx); err != nil {
		t.Fatalf("Claim: %v", err)
	}

	finalized, won, err := store.FinalizeFailure(ctx, task.ID, "upstream 503: queue overloaded")
	if err != nil || !won {
		t.Fatalf("FinalizeFailure = won %v err %v, want the finalize to win", won, err)
	}
	if finalized.Status != canvastask.StatusFailed || finalized.Error == "" ||
		finalized.AssetID != 0 || finalized.ImageURL != "" {
		t.Errorf("finalized = %+v, want failed with reason and empty result columns", finalized)
	}
}

func TestMySQLCanvasTaskResetForRetryOnlyFromFailed(t *testing.T) {
	store, _ := openTaskTestDB(t)
	ctx := context.Background()
	task := seedTask(t, store, 7, "image-1-1", canvastask.StatusQueued)

	// 非失败态不可重试:行存在但状态不对 → ErrNotRetryable。
	if _, err := store.ResetForRetry(ctx, task.ID, 7); !errors.Is(err, canvastask.ErrNotRetryable) {
		t.Fatalf("queued retry = %v, want ErrNotRetryable", err)
	}
	// 画布不匹配同样不可重试(任务属于别的画布)。
	if _, err := store.ResetForRetry(ctx, task.ID, 8); !errors.Is(err, canvastask.ErrNotRetryable) {
		t.Fatalf("cross-canvas retry = %v, want ErrNotRetryable", err)
	}
	// 未知 id → ErrNotFound。
	if _, err := store.ResetForRetry(ctx, "ct_missing", 7); !errors.Is(err, canvastask.ErrNotFound) {
		t.Fatalf("missing retry = %v, want ErrNotFound", err)
	}

	// 走完失败路径后重试成功:回 queued、失败字段清空、绑定不变。
	if _, err := store.Claim(ctx); err != nil {
		t.Fatalf("Claim: %v", err)
	}
	if _, _, err := store.FinalizeFailure(ctx, task.ID, "boom"); err != nil {
		t.Fatalf("FinalizeFailure: %v", err)
	}
	retried, err := store.ResetForRetry(ctx, task.ID, 7)
	if err != nil {
		t.Fatalf("ResetForRetry: %v", err)
	}
	if retried.Status != canvastask.StatusQueued || retried.Error != "" ||
		retried.NodeID != "image-1-1" || retried.CanvasID != 7 {
		t.Errorf("retried = %+v, want queued with cleared failure and intact binding", retried)
	}
}

func TestMySQLCanvasTaskRequeueRunningRecoversOrphans(t *testing.T) {
	store, _ := openTaskTestDB(t)
	ctx := context.Background()

	// 最先入队的任务会被 Claim 认领(FIFO)—— 它就是重启时的孤儿。
	running := seedTask(t, store, 7, "image-1-1", canvastask.StatusQueued)
	queued := seedTask(t, store, 7, "image-1-2", canvastask.StatusQueued)
	if _, err := store.Claim(ctx); err != nil {
		t.Fatalf("Claim: %v", err)
	}
	done := seedTask(t, store, 7, "image-1-3", canvastask.StatusSucceeded)

	// 重启恢复:running 的孤儿回队,queued/succeeded 不动。
	n, err := store.RequeueRunning(ctx)
	if err != nil || n != 1 {
		t.Fatalf("RequeueRunning = %d err %v, want exactly the orphan requeued", n, err)
	}
	got, err := store.Get(ctx, running.ID)
	if err != nil || got.Status != canvastask.StatusQueued || got.Attempts != 1 {
		t.Fatalf("requeued = %+v err %v, want queued keeping its attempt count", got, err)
	}
	skipped, err := store.Get(ctx, queued.ID)
	if err != nil || skipped.Status != canvastask.StatusQueued || skipped.Attempts != 0 {
		t.Errorf("untouched queued task = %+v, want still queued with attempts=0", skipped)
	}
	skipped, err = store.Get(ctx, done.ID)
	if err != nil || skipped.Status != canvastask.StatusSucceeded {
		t.Errorf("succeeded task after requeue = %+v, want untouched", skipped)
	}
}

// seedVideoTask seeds a kind=video task (12 号票) with the reference facts
// an image-to-video submit carries.
func seedVideoTask(t *testing.T, store *canvastask.MySQLStore, canvasID int64, nodeID string, status canvastask.Status) canvastask.Task {
	t.Helper()
	id, err := canvastask.NewID()
	if err != nil {
		t.Fatalf("NewID: %v", err)
	}
	task, err := store.Create(context.Background(), canvastask.Task{
		ID: id, CanvasID: canvasID, NodeID: nodeID, Kind: canvastask.KindVideo,
		Prompt: "镜头缓缓推进", Model: "vid-m", Seconds: 5,
		ImageRef: "https://img.example/cat.png", Status: status,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	return task
}

func TestMySQLCanvasTaskVideoFieldsRoundTrip(t *testing.T) {
	store, _ := openTaskTestDB(t)
	ctx := context.Background()
	task := seedVideoTask(t, store, 7, "video-1-1", canvastask.StatusQueued)

	got, err := store.Get(ctx, task.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Kind != canvastask.KindVideo || got.Seconds != 5 ||
		got.ImageRef != "https://img.example/cat.png" || got.VideoURL != "" ||
		got.RemoteTaskID != "" {
		t.Errorf("got = %+v, want the video facts preserved", got)
	}

	// 图片任务不受影响:video 专属列为零值。
	imageTask := seedTask(t, store, 7, "image-1-1", canvastask.StatusQueued)
	got, err = store.Get(ctx, imageTask.ID)
	if err != nil {
		t.Fatalf("Get image task: %v", err)
	}
	if got.Seconds != 0 || got.ImageRef != "" || got.VideoURL != "" {
		t.Errorf("image task = %+v, want zero-valued video columns", got)
	}
}

func TestMySQLCanvasTaskAttachRemoteOnlyOnRunning(t *testing.T) {
	store, _ := openTaskTestDB(t)
	ctx := context.Background()
	task := seedVideoTask(t, store, 7, "video-1-1", canvastask.StatusQueued)
	if _, err := store.Claim(ctx); err != nil {
		t.Fatalf("Claim: %v", err)
	}

	fresh, ok, err := store.AttachRemote(ctx, task.ID, "vt_remote_1")
	if err != nil || !ok {
		t.Fatalf("AttachRemote = ok %v err %v, want the handle attached", ok, err)
	}
	if fresh.RemoteTaskID != "vt_remote_1" {
		t.Errorf("remote_task_id = %q, want the handle stored", fresh.RemoteTaskID)
	}

	// 行被取消后 Attach 不再发生:提交在途的取消让 worker 输掉这场写入。
	if _, err := store.Cancel(ctx, task.ID, 7); err != nil {
		t.Fatalf("Cancel: %v", err)
	}
	got, ok, err := store.AttachRemote(ctx, task.ID, "vt_remote_2")
	if err != nil || ok {
		t.Fatalf("AttachRemote after cancel = ok %v err %v, want the race lost", ok, err)
	}
	if got.Status != canvastask.StatusCanceled || got.RemoteTaskID != "vt_remote_1" {
		t.Errorf("row = %+v, want canceled keeping the first handle", got)
	}
}

func TestMySQLCanvasTaskCancelClosesActiveTasks(t *testing.T) {
	store, _ := openTaskTestDB(t)
	ctx := context.Background()

	queued := seedVideoTask(t, store, 7, "video-1-1", canvastask.StatusQueued)
	canceled, err := store.Cancel(ctx, queued.ID, 7)
	if err != nil || canceled.Status != canvastask.StatusCanceled {
		t.Fatalf("queued cancel = %+v err %v, want canceled", canceled, err)
	}
	// 终态不可取消。
	if _, err := store.Cancel(ctx, queued.ID, 7); !errors.Is(err, canvastask.ErrNotCancelable) {
		t.Fatalf("terminal cancel = %v, want ErrNotCancelable", err)
	}

	// running 同样可取消;别的画布取消不了本画布的任务。
	running := seedVideoTask(t, store, 7, "video-1-2", canvastask.StatusQueued)
	if _, err := store.Claim(ctx); err != nil {
		t.Fatalf("Claim: %v", err)
	}
	if _, err := store.Cancel(ctx, running.ID, 8); !errors.Is(err, canvastask.ErrNotCancelable) {
		t.Fatalf("cross-canvas cancel = %v, want ErrNotCancelable", err)
	}
	canceled, err = store.Cancel(ctx, running.ID, 7)
	if err != nil || canceled.Status != canvastask.StatusCanceled {
		t.Fatalf("running cancel = %+v err %v, want canceled", canceled, err)
	}

	if _, err := store.Cancel(ctx, "ct_missing", 7); !errors.Is(err, canvastask.ErrNotFound) {
		t.Fatalf("missing cancel = %v, want ErrNotFound", err)
	}
}

func TestMySQLCanvasTaskFinalizeVideoSuccessRecordsVideoAsset(t *testing.T) {
	store, db := openTaskTestDB(t)
	ctx := context.Background()
	task := seedVideoTask(t, store, 7, "video-1-1", canvastask.StatusQueued)
	if _, err := store.Claim(ctx); err != nil {
		t.Fatalf("Claim: %v", err)
	}

	finalized, won, err := store.FinalizeVideoSuccess(ctx, task.ID, asset.Asset{
		Kind: asset.KindVideo, CanvasID: task.CanvasID, TaskID: task.ID,
		Model: task.Model, Prompt: task.Prompt, URL: "https://vid.example/cat.mp4",
	})
	if err != nil || !won {
		t.Fatalf("FinalizeVideoSuccess = won %v err %v, want the finalize to win", won, err)
	}
	if finalized.Status != canvastask.StatusSucceeded || finalized.AssetID == 0 ||
		finalized.VideoURL != "https://vid.example/cat.mp4" || finalized.ImageURL != "" {
		t.Errorf("finalized = %+v, want succeeded with video_url only", finalized)
	}
	a, err := asset.NewMySQLStore(db).Get(ctx, finalized.AssetID)
	if err != nil {
		t.Fatalf("asset Get: %v", err)
	}
	if a.Kind != asset.KindVideo || a.URL != finalized.VideoURL {
		t.Errorf("asset = %+v, want a video asset with the delivered url", a)
	}

	// queued 态下迟到终态被守卫挡住(任务已被取消的情形)。
	other := seedVideoTask(t, store, 7, "video-1-2", canvastask.StatusQueued)
	_, won, err = store.FinalizeVideoSuccess(ctx, other.ID, asset.Asset{Kind: asset.KindVideo, URL: "https://vid.example/x.mp4"})
	if err != nil || won {
		t.Fatalf("finalize from queued = won %v err %v, want the guard to win", won, err)
	}
}

func TestMySQLCanvasTaskFinalizeCanceledOnlyFromRunning(t *testing.T) {
	store, _ := openTaskTestDB(t)
	ctx := context.Background()
	task := seedVideoTask(t, store, 7, "video-1-1", canvastask.StatusQueued)

	// queued 态下 worker 看不到这个任务,守卫挡住迟到的 FinalizeCanceled。
	_, won, err := store.FinalizeCanceled(ctx, task.ID)
	if err != nil || won {
		t.Fatalf("finalize canceled from queued = won %v err %v, want the guard to win", won, err)
	}

	if _, err := store.Claim(ctx); err != nil {
		t.Fatalf("Claim: %v", err)
	}
	finalized, won, err := store.FinalizeCanceled(ctx, task.ID)
	if err != nil || !won {
		t.Fatalf("FinalizeCanceled = won %v err %v, want the close-out to win", won, err)
	}
	if finalized.Status != canvastask.StatusCanceled {
		t.Errorf("status = %s, want canceled", finalized.Status)
	}
}
