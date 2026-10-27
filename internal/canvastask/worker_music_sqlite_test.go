package canvastask_test

// runMusic 的 SQLite 全链测试(60 号票):提交 → 远端句柄落行 → 轮询五态
// → 产物转存 kind=audio 素材行 → FinalizeMusicSuccess 同事务收尾;失败
// 退款语义在网关侧,worker 只负责把原因落到任务行。走临时桌面形 SQLite
// 库,不依赖 MySQL 基础设施(worker_voice_sqlite_test.go 同范式)。

import (
	"context"
	"errors"
	"log"
	"strings"
	"testing"
	"time"

	"github.com/gachal/InfiniteChance/internal/asset"
	"github.com/gachal/InfiniteChance/internal/canvastask"
)

// newSQLiteMusicWorker wires a worker tuned for the music tests: fast
// polling, short music deadline, quiet logging。
func newSQLiteMusicWorker(store *canvastask.SQLiteStore, gateway *stubGateway, opts ...canvastask.WorkerOption) *canvastask.Worker {
	base := []canvastask.WorkerOption{
		canvastask.WithPollInterval(20 * time.Millisecond),
		canvastask.WithTaskTimeout(3 * time.Second),
		canvastask.WithMusicPollInterval(10 * time.Millisecond),
		canvastask.WithMusicTaskTimeout(2 * time.Second),
		canvastask.WithConcurrency(1),
		canvastask.WithLogger(log.New(&strings.Builder{}, "", 0)),
	}
	return canvastask.NewWorker(store, gateway, append(base, opts...)...)
}

func seedMusicTask(t *testing.T, store *canvastask.SQLiteStore, prompt, model, remoteTaskID string) canvastask.Task {
	t.Helper()
	id, err := canvastask.NewID()
	if err != nil {
		t.Fatal(err)
	}
	task, err := store.Create(context.Background(), canvastask.Task{
		ID: id, CanvasID: 7, NodeID: "audio-1", Kind: canvastask.KindMusic,
		Prompt: prompt, Model: model, RemoteTaskID: remoteTaskID,
	})
	if err != nil {
		t.Fatalf("seed music task: %v", err)
	}
	return task
}

// 快乐路径:提交受理 → 句柄落行 → 轮询至 succeeded → kind=audio 素材行
// (prompt 随行)与任务终态同事务;网关任务句柄在任务行上。
func TestSQLiteWorkerMusicHappyPath(t *testing.T) {
	store, _, db := newSQLiteVoiceWorld(t)
	assets := asset.NewSQLiteStore(db)
	gateway := &stubGateway{
		musicSubmitFn: func(_ context.Context, req canvastask.MusicRequest) (canvastask.MusicSubmitResult, error) {
			if req.Model != "bgm-m" || req.Prompt != "轻快的电子爵士" {
				t.Errorf("submit request = %+v, want the task row facts", req)
			}
			return canvastask.MusicSubmitResult{TaskID: "mt_remote_1"}, nil
		},
		musicPollFn: func(call int) (canvastask.MusicPoll, error) {
			if call == 1 {
				return canvastask.MusicPoll{Status: "running"}, nil
			}
			return canvastask.MusicPoll{Status: "succeeded", AudioURL: "https://cos.example/a.mp3"}, nil
		},
	}
	runSQLiteWorker(t, newSQLiteMusicWorker(store, gateway))

	task := seedMusicTask(t, store, "轻快的电子爵士", "bgm-m", "")
	final := awaitSQLiteTask(t, store, task.ID)

	if final.Status != canvastask.StatusSucceeded {
		t.Fatalf("task = %s (%q), want succeeded", final.Status, final.Error)
	}
	if final.RemoteTaskID != "mt_remote_1" {
		t.Errorf("remote_task_id = %q, want the gateway handle attached", final.RemoteTaskID)
	}
	if final.AssetID <= 0 {
		t.Fatalf("asset_id = %d, want the archived audio asset", final.AssetID)
	}
	a, err := assets.Get(context.Background(), final.AssetID)
	if err != nil {
		t.Fatalf("asset get: %v", err)
	}
	if a.Kind != asset.KindAudio || a.Prompt != "轻快的电子爵士" || a.Model != "bgm-m" ||
		a.URL != "https://cos.example/a.mp3" {
		t.Errorf("asset row = %+v, want kind=audio with the music facts", a)
	}
}

// 上游失败:任务失败并带厂商原因;素材行不落。
func TestSQLiteWorkerMusicFailureLandsReason(t *testing.T) {
	store, _, _ := newSQLiteVoiceWorld(t)
	gateway := &stubGateway{
		musicSubmitFn: func(context.Context, canvastask.MusicRequest) (canvastask.MusicSubmitResult, error) {
			return canvastask.MusicSubmitResult{TaskID: "mt_remote_2"}, nil
		},
		musicPollFn: func(int) (canvastask.MusicPoll, error) {
			return canvastask.MusicPoll{Status: "failed", Error: "tme audio url is empty"}, nil
		},
	}
	runSQLiteWorker(t, newSQLiteMusicWorker(store, gateway))

	task := seedMusicTask(t, store, "低沉的大提琴", "bgm-m", "")
	final := awaitSQLiteTask(t, store, task.ID)

	if final.Status != canvastask.StatusFailed || !strings.Contains(final.Error, "tme audio url is empty") {
		t.Fatalf("task = %s (%q), want failed with the vendor reason", final.Status, final.Error)
	}
	if final.AssetID != 0 {
		t.Errorf("asset_id = %d, want none on failure", final.AssetID)
	}
}

// 恢复/重试带句柄:不再重复提交,续查旧句柄直接收尾(与音色句柄续查同
// 哲学 —— 重复提交是双倍账)。句柄按生产路径就位:Claim 进 running →
// AttachRemote 落句柄 → RequeueRunning 回队(进程死亡恢复),worker 再
// 认领时行上已带句柄。
func TestSQLiteWorkerMusicResumesRemoteHandle(t *testing.T) {
	store, _, _ := newSQLiteVoiceWorld(t)
	gateway := &stubGateway{
		musicPollFn: func(int) (canvastask.MusicPoll, error) {
			return canvastask.MusicPoll{Status: "succeeded", AudioURL: "https://cos.example/b.mp3"}, nil
		},
	}

	task := seedMusicTask(t, store, "雨夜的钢琴", "bgm-m", "")
	if _, err := store.Claim(context.Background()); err != nil {
		t.Fatalf("claim: %v", err)
	}
	if _, ok, err := store.AttachRemote(context.Background(), task.ID, "mt_remote_old"); err != nil || !ok {
		t.Fatalf("attach remote = ok %v err %v, want attached", ok, err)
	}
	if n, err := store.RequeueRunning(context.Background()); err != nil || n != 1 {
		t.Fatalf("requeue = %d err %v, want the task back in queue", n, err)
	}

	runSQLiteWorker(t, newSQLiteMusicWorker(store, gateway))
	final := awaitSQLiteTask(t, store, task.ID)

	if final.Status != canvastask.StatusSucceeded || final.AssetID <= 0 {
		t.Fatalf("task = %s (%q) asset %d, want succeeded with asset", final.Status, final.Error, final.AssetID)
	}
	if len(gateway.musicSubmits) != 0 {
		t.Errorf("submits = %d, want 0 (旧句柄续查,不重复提交)", len(gateway.musicSubmits))
	}
	if final.RemoteTaskID != "mt_remote_old" {
		t.Errorf("remote_task_id = %q, want the resumed handle kept", final.RemoteTaskID)
	}
}

// 预查暂态失败(网络抖动)不重提:进续查循环,第二次查询拿到成功照样
// 收尾 —— 提交即计费,查询没成功就重提是双倍账(code review 修正的
// 回归用例)。
func TestSQLiteWorkerMusicResumeRetriesPollWithoutResubmit(t *testing.T) {
	store, _, _ := newSQLiteVoiceWorld(t)
	gateway := &stubGateway{
		musicPollFn: func(call int) (canvastask.MusicPoll, error) {
			if call == 1 {
				return canvastask.MusicPoll{}, errors.New("网络抖动")
			}
			return canvastask.MusicPoll{Status: "succeeded", AudioURL: "https://cos.example/c.mp3"}, nil
		},
	}

	task := seedMusicTask(t, store, "清晨的鸟鸣", "bgm-m", "")
	if _, err := store.Claim(context.Background()); err != nil {
		t.Fatalf("claim: %v", err)
	}
	if _, ok, err := store.AttachRemote(context.Background(), task.ID, "mt_remote_err"); err != nil || !ok {
		t.Fatalf("attach remote = ok %v err %v, want attached", ok, err)
	}
	if n, err := store.RequeueRunning(context.Background()); err != nil || n != 1 {
		t.Fatalf("requeue = %d err %v, want the task back in queue", n, err)
	}

	runSQLiteWorker(t, newSQLiteMusicWorker(store, gateway))
	final := awaitSQLiteTask(t, store, task.ID)

	if final.Status != canvastask.StatusSucceeded || final.AssetID <= 0 {
		t.Fatalf("task = %s (%q) asset %d, want succeeded via re-poll", final.Status, final.Error, final.AssetID)
	}
	if len(gateway.musicSubmits) != 0 {
		t.Errorf("submits = %d, want 0 (暂态查询失败不重提)", len(gateway.musicSubmits))
	}
	if final.RemoteTaskID != "mt_remote_err" {
		t.Errorf("remote_task_id = %q, want the original handle kept", final.RemoteTaskID)
	}
}
