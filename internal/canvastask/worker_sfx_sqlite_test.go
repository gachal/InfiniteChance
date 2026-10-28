package canvastask_test

// runSfx 的 SQLite 全链测试(61 号票):同步 CreateSfx → 产物转存
// kind=audio 素材行 → 复用 FinalizeMusicSuccess 同事务收尾;恢复语义照
// runTts 现状 —— 同步任务无远端句柄,重启回队后按全新调用重跑(窗口有界,
// 见 runSfx 注释)。走临时桌面形 SQLite 库,不依赖 MySQL 基础设施。

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

// newSQLiteSfxWorker wires a worker tuned for the sfx tests: fast scanning,
// short sfx deadline, quiet logging。
func newSQLiteSfxWorker(store *canvastask.SQLiteStore, gateway *stubGateway, opts ...canvastask.WorkerOption) *canvastask.Worker {
	base := []canvastask.WorkerOption{
		canvastask.WithPollInterval(20 * time.Millisecond),
		canvastask.WithSfxTaskTimeout(3 * time.Second),
		canvastask.WithConcurrency(1),
		canvastask.WithLogger(log.New(&strings.Builder{}, "", 0)),
	}
	return canvastask.NewWorker(store, gateway, append(base, opts...)...)
}

func seedSfxTask(t *testing.T, store *canvastask.SQLiteStore, text, model string, seconds int64) canvastask.Task {
	t.Helper()
	id, err := canvastask.NewID()
	if err != nil {
		t.Fatal(err)
	}
	task, err := store.Create(context.Background(), canvastask.Task{
		ID: id, CanvasID: 7, NodeID: "audio-1", Kind: canvastask.KindSfx,
		Prompt: text, Model: model, Seconds: seconds,
	})
	if err != nil {
		t.Fatalf("seed sfx task: %v", err)
	}
	return task
}

// 快乐路径:同步 CreateSfx(任务行事实:描述/模型/秒数)→ kind=audio 素材
// 行(prompt 随行)与任务终态同事务;seconds 在任务行上保真(composer 回填
// 音频节点元数据的依据)。
func TestSQLiteWorkerSfxHappyPath(t *testing.T) {
	store, _, db := newSQLiteVoiceWorld(t)
	assets := asset.NewSQLiteStore(db)
	gateway := &stubGateway{
		sfxFn: func(_ context.Context, req canvastask.SfxRequest) (canvastask.SfxResult, error) {
			if req.Model != "vid-m" || req.Text != "雷声轰鸣,由远及近" || req.Seconds != 5 {
				t.Errorf("sfx request = %+v, want the task row facts", req)
			}
			if !strings.Contains(req.Source, "kind=sfx") {
				t.Errorf("sfx source = %q, want the nodeTaskSource shape", req.Source)
			}
			return canvastask.SfxResult{AudioURL: "https://vod.example/boom.mp3", Seconds: 5}, nil
		},
	}
	runSQLiteWorker(t, newSQLiteSfxWorker(store, gateway))

	task := seedSfxTask(t, store, "雷声轰鸣,由远及近", "vid-m", 5)
	final := awaitSQLiteTask(t, store, task.ID)

	if final.Status != canvastask.StatusSucceeded {
		t.Fatalf("task = %s (%q), want succeeded", final.Status, final.Error)
	}
	if final.AssetID <= 0 {
		t.Fatalf("asset_id = %d, want the archived audio asset", final.AssetID)
	}
	if final.Seconds != 5 {
		t.Errorf("seconds = %d, want 5 preserved on the row", final.Seconds)
	}
	a, err := assets.Get(context.Background(), final.AssetID)
	if err != nil {
		t.Fatalf("asset get: %v", err)
	}
	if a.Kind != asset.KindAudio || a.Prompt != "雷声轰鸣,由远及近" || a.Model != "vid-m" ||
		a.URL != "https://vod.example/boom.mp3" {
		t.Errorf("asset row = %+v, want kind=audio with the sfx facts", a)
	}
}

// 网关拒绝(无 sfx 能力渠道 / 模型未配价):任务失败并带原因;素材行不落。
func TestSQLiteWorkerSfxFailureLandsReason(t *testing.T) {
	store, _, _ := newSQLiteVoiceWorld(t)
	gateway := &stubGateway{
		sfxFn: func(context.Context, canvastask.SfxRequest) (canvastask.SfxResult, error) {
			return canvastask.SfxResult{}, errors.New("gateway 404: The model 'vid-m' does not exist or no sfx-capable channel serves it.")
		},
	}
	runSQLiteWorker(t, newSQLiteSfxWorker(store, gateway))

	task := seedSfxTask(t, store, "玻璃碎裂", "vid-m", 3)
	final := awaitSQLiteTask(t, store, task.ID)

	if final.Status != canvastask.StatusFailed || !strings.Contains(final.Error, "no sfx-capable channel") {
		t.Fatalf("task = %s (%q), want failed with the gateway reason", final.Status, final.Error)
	}
	if final.AssetID != 0 {
		t.Errorf("asset_id = %d, want none on failure", final.AssetID)
	}
}

// 恢复语义(照 runTts 现状):同步任务无远端句柄,重启回队后按全新调用
// 重跑 —— 这里固化「重跑一次即成功」的既有取舍;双计费窗口的边界说明见
// runSfx 注释(网关同步面必然给出终态,窗口有界)。
func TestSQLiteWorkerSfxRerunsAfterRequeue(t *testing.T) {
	store, _, _ := newSQLiteVoiceWorld(t)
	gateway := &stubGateway{
		sfxFn: func(context.Context, canvastask.SfxRequest) (canvastask.SfxResult, error) {
			return canvastask.SfxResult{AudioURL: "https://vod.example/rerun.mp3", Seconds: 10}, nil
		},
	}
	runSQLiteWorker(t, newSQLiteSfxWorker(store, gateway))

	task := seedSfxTask(t, store, "远处的犬吠", "vid-m", 10)
	// 模拟进程死亡:任务进 running 后不收尾,重启恢复把它回队。
	if _, err := store.Claim(context.Background()); err != nil {
		t.Fatalf("claim: %v", err)
	}
	if n, err := store.RequeueRunning(context.Background()); err != nil || n != 1 {
		t.Fatalf("requeue = %d err %v, want the task back in queue", n, err)
	}

	final := awaitSQLiteTask(t, store, task.ID)
	if final.Status != canvastask.StatusSucceeded || final.AssetID <= 0 {
		t.Fatalf("task = %s (%q) asset %d, want succeeded via the fresh synchronous call", final.Status, final.Error, final.AssetID)
	}
	if len(gateway.sfxCalls) != 1 {
		t.Errorf("sfx calls = %d, want 1 fresh call after requeue", len(gateway.sfxCalls))
	}
}
