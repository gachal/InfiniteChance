package canvastask_test

import (
	"context"
	"errors"
	"log"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gachal/InfiniteChance/internal/asset"
	"github.com/gachal/InfiniteChance/internal/canvastask"
)

// stubGateway is a programmable Gateway for worker tests: it records the
// requests it saw and answers from a script. The video face (12 号票) is
// programmable per call — submit through submitFn, polls through pollFn
// (called with the 1-based poll count); cancels are always recorded.
type stubGateway struct {
	mu       sync.Mutex
	requests []canvastask.ImageRequest

	// fn, when set, answers every image call (and may block).
	fn func(context.Context, canvastask.ImageRequest) (canvastask.ImageResult, error)

	videoSubmits []canvastask.VideoRequest
	videoCancels []string
	videoPolls   int
	submitFn     func(context.Context, canvastask.VideoRequest) (canvastask.VideoSubmitResult, error)
	pollFn       func(call int) (canvastask.VideoPoll, error)
}

func (g *stubGateway) GenerateImage(ctx context.Context, req canvastask.ImageRequest) (canvastask.ImageResult, error) {
	g.mu.Lock()
	g.requests = append(g.requests, req)
	fn := g.fn
	g.mu.Unlock()
	if fn == nil {
		return canvastask.ImageResult{}, errors.New("stub gateway: no script")
	}
	return fn(ctx, req)
}

func (g *stubGateway) SubmitVideo(ctx context.Context, req canvastask.VideoRequest) (canvastask.VideoSubmitResult, error) {
	g.mu.Lock()
	g.videoSubmits = append(g.videoSubmits, req)
	fn := g.submitFn
	g.mu.Unlock()
	if fn == nil {
		return canvastask.VideoSubmitResult{}, errors.New("stub gateway: no video script")
	}
	return fn(ctx, req)
}

func (g *stubGateway) PollVideo(_ context.Context, _ string) (canvastask.VideoPoll, error) {
	g.mu.Lock()
	g.videoPolls++
	call := g.videoPolls
	fn := g.pollFn
	g.mu.Unlock()
	if fn == nil {
		return canvastask.VideoPoll{Status: "running"}, nil
	}
	return fn(call)
}

func (g *stubGateway) CancelVideo(_ context.Context, taskID string) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.videoCancels = append(g.videoCancels, taskID)
	return nil
}

func (g *stubGateway) seen() []canvastask.ImageRequest {
	g.mu.Lock()
	defer g.mu.Unlock()
	return append([]canvastask.ImageRequest(nil), g.requests...)
}

func (g *stubGateway) seenVideoSubmits() []canvastask.VideoRequest {
	g.mu.Lock()
	defer g.mu.Unlock()
	return append([]canvastask.VideoRequest(nil), g.videoSubmits...)
}

func (g *stubGateway) seenVideoCancels() []string {
	g.mu.Lock()
	defer g.mu.Unlock()
	return append([]string(nil), g.videoCancels...)
}

// newTestWorker wires a worker tuned for tests: fast polling, short task
// timeouts, quiet logging.
func newTestWorker(store *canvastask.MySQLStore, gateway canvastask.Gateway) *canvastask.Worker {
	return canvastask.NewWorker(store, gateway,
		canvastask.WithPollInterval(20*time.Millisecond),
		canvastask.WithTaskTimeout(2*time.Second),
		canvastask.WithVideoPollInterval(20*time.Millisecond),
		canvastask.WithVideoTaskTimeout(2*time.Second),
		canvastask.WithConcurrency(2),
		canvastask.WithLogger(log.New(&strings.Builder{}, "", 0)),
	)
}

// runWorker starts the worker loop and stops it at test cleanup.
func runWorker(t *testing.T, w *canvastask.Worker) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		w.Run(ctx)
		close(done)
	}()
	t.Cleanup(func() {
		cancel()
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			t.Errorf("worker did not stop within 2s of context cancel")
		}
	})
}

// awaitTask polls the store until the task reaches a terminal state (or the
// deadline passes) — the test-side twin of the editor's polling. Canceled
// counts: the video flow closes rows that way.
func awaitTask(t *testing.T, store *canvastask.MySQLStore, id string) canvastask.Task {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		task, err := store.Get(context.Background(), id)
		if err != nil {
			t.Fatalf("Get %s: %v", id, err)
		}
		if canvastask.Terminal(task.Status) {
			return task
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("task %s still active after 3s", id)
	return canvastask.Task{}
}

func TestWorkerRunsQueuedTaskToSuccessAndAsset(t *testing.T) {
	store, db := openTaskTestDB(t)
	gateway := &stubGateway{fn: func(_ context.Context, _ canvastask.ImageRequest) (canvastask.ImageResult, error) {
		return canvastask.ImageResult{URL: "https://img.example/worker.png"}, nil
	}}
	runWorker(t, newTestWorker(store, gateway))

	task := seedTask(t, store, 7, "image-1-1", canvastask.StatusQueued)
	final := awaitTask(t, store, task.ID)

	if final.Status != canvastask.StatusSucceeded || final.ImageURL != "https://img.example/worker.png" || final.AssetID == 0 {
		t.Fatalf("task = %+v, want succeeded with url and asset", final)
	}
	if final.Attempts != 1 {
		t.Errorf("attempts = %d, want 1", final.Attempts)
	}

	// 产物自动入素材库:任务行里的 asset_id 能取回素材,出处齐全。
	assets := asset.NewMySQLStore(db)
	a, err := assets.Get(context.Background(), final.AssetID)
	if err != nil {
		t.Fatalf("asset Get: %v", err)
	}
	if a.TaskID != task.ID || a.CanvasID != 7 || a.Kind != asset.KindImage ||
		a.Prompt != task.Prompt || a.URL != final.ImageURL {
		t.Errorf("asset = %+v, want the task's provenance carried over", a)
	}

	// 网关调用带画布来源标记(用量审计的依据)。
	reqs := gateway.seen()
	if len(reqs) != 1 {
		t.Fatalf("gateway requests = %d, want 1", len(reqs))
	}
	if reqs[0].Prompt != task.Prompt || reqs[0].Model != task.Model || reqs[0].Size != task.Size {
		t.Errorf("request = %+v, want the task's generation facts", reqs[0])
	}
	if reqs[0].Source != "canvas=7 task="+task.ID+" node=image-1-1" {
		t.Errorf("source = %q, want the canvas origin mark", reqs[0].Source)
	}
}

func TestWorkerFinalizesFailureWithReason(t *testing.T) {
	store, _ := openTaskTestDB(t)
	gateway := &stubGateway{fn: func(_ context.Context, _ canvastask.ImageRequest) (canvastask.ImageResult, error) {
		return canvastask.ImageResult{}, errors.New("gateway 503: queue overloaded")
	}}
	runWorker(t, newTestWorker(store, gateway))

	task := seedTask(t, store, 7, "image-1-1", canvastask.StatusQueued)
	final := awaitTask(t, store, task.ID)

	if final.Status != canvastask.StatusFailed {
		t.Fatalf("status = %s, want failed", final.Status)
	}
	if !strings.Contains(final.Error, "queue overloaded") {
		t.Errorf("error = %q, want the gateway failure reason", final.Error)
	}
	if final.Attempts != 1 || final.AssetID != 0 || final.ImageURL != "" {
		t.Errorf("task = %+v, want one attempt and no artifact", final)
	}
}

func TestWorkerRetriedTaskRunsAgain(t *testing.T) {
	store, _ := openTaskTestDB(t)
	calls := 0
	gateway := &stubGateway{fn: func(_ context.Context, _ canvastask.ImageRequest) (canvastask.ImageResult, error) {
		calls++
		if calls == 1 {
			return canvastask.ImageResult{}, errors.New("flaky vendor")
		}
		return canvastask.ImageResult{URL: "https://img.example/second-try.png"}, nil
	}}
	runWorker(t, newTestWorker(store, gateway))

	task := seedTask(t, store, 7, "image-1-1", canvastask.StatusQueued)
	failed := awaitTask(t, store, task.ID)
	if failed.Status != canvastask.StatusFailed {
		t.Fatalf("first run status = %s, want failed", failed.Status)
	}

	// 原地重试:同一任务回队,worker 再跑一次,成功落产物。
	if _, err := store.ResetForRetry(context.Background(), task.ID, 7); err != nil {
		t.Fatalf("ResetForRetry: %v", err)
	}
	final := awaitTask(t, store, task.ID)
	if final.Status != canvastask.StatusSucceeded || final.ImageURL != "https://img.example/second-try.png" {
		t.Fatalf("final = %+v, want the retry to succeed", final)
	}
	if final.Attempts != 2 {
		t.Errorf("attempts = %d, want 2", final.Attempts)
	}
}

func TestWorkerFailsTaskOnTimeout(t *testing.T) {
	store, _ := openTaskTestDB(t)
	release := make(chan struct{})
	defer close(release)
	gateway := &stubGateway{fn: func(ctx context.Context, _ canvastask.ImageRequest) (canvastask.ImageResult, error) {
		// 模拟一个不设防的慢上游:任务时限(100ms)一到,context 取消把
		// 这次调用切断 —— 与真实 HTTP 调用随 ctx 中止同形。
		select {
		case <-release:
			return canvastask.ImageResult{URL: "https://img.example/late.png"}, nil
		case <-ctx.Done():
			return canvastask.ImageResult{}, ctx.Err()
		}
	}}
	w := canvastask.NewWorker(store, gateway,
		canvastask.WithPollInterval(20*time.Millisecond),
		canvastask.WithTaskTimeout(100*time.Millisecond),
		canvastask.WithConcurrency(1),
		canvastask.WithLogger(log.New(&strings.Builder{}, "", 0)),
	)
	runWorker(t, w)

	task := seedTask(t, store, 7, "image-1-1", canvastask.StatusQueued)
	final := awaitTask(t, store, task.ID)
	if final.Status != canvastask.StatusFailed {
		t.Fatalf("status = %s, want the timeout to fail the task", final.Status)
	}
}

func TestWorkerHonorsConcurrency(t *testing.T) {
	store, _ := openTaskTestDB(t)

	// 并发位 1:第二个任务必须等第一个跑完才被认领。
	gateway := &stubGateway{fn: func(_ context.Context, _ canvastask.ImageRequest) (canvastask.ImageResult, error) {
		time.Sleep(150 * time.Millisecond) // 占住唯一并发位
		return canvastask.ImageResult{URL: "https://img.example/c.png"}, nil
	}}
	w := canvastask.NewWorker(store, gateway,
		canvastask.WithPollInterval(10*time.Millisecond),
		canvastask.WithTaskTimeout(5*time.Second),
		canvastask.WithConcurrency(1),
		canvastask.WithLogger(log.New(&strings.Builder{}, "", 0)),
	)
	runWorker(t, w)

	first := seedTask(t, store, 7, "image-1-1", canvastask.StatusQueued)
	second := seedTask(t, store, 7, "image-1-2", canvastask.StatusQueued)

	// 等第一个任务被认领(进入 running)。
	deadline := time.Now().Add(2 * time.Second)
	for {
		got, err := store.Get(context.Background(), first.ID)
		if err != nil {
			t.Fatalf("Get first: %v", err)
		}
		if got.Status == canvastask.StatusRunning {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("first task never claimed")
		}
		time.Sleep(5 * time.Millisecond)
	}

	// 第一个任务还在途:并发 1 意味着第二个任务此刻必须仍未认领。
	mid, err := store.Get(context.Background(), second.ID)
	if err != nil {
		t.Fatalf("Get second: %v", err)
	}
	if mid.Status != canvastask.StatusQueued {
		t.Errorf("second task status mid-flight = %s, want queued behind the single slot", mid.Status)
	}

	final := awaitTask(t, store, second.ID)
	if final.Status != canvastask.StatusSucceeded {
		t.Errorf("second task final status = %s, want succeeded after the slot freed", final.Status)
	}
}

func TestWorkerRunStopsOnContextCancel(t *testing.T) {
	store, _ := openTaskTestDB(t)
	w := newTestWorker(store, &stubGateway{})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		w.Run(ctx)
		close(done)
	}()
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatalf("Run did not return after context cancel")
	}
}

func TestWorkerVideoTaskReachesSuccessAndAsset(t *testing.T) {
	store, db := openTaskTestDB(t)
	gateway := &stubGateway{
		submitFn: func(_ context.Context, _ canvastask.VideoRequest) (canvastask.VideoSubmitResult, error) {
			return canvastask.VideoSubmitResult{TaskID: "vt_remote_success"}, nil
		},
		pollFn: func(call int) (canvastask.VideoPoll, error) {
			switch call {
			case 1:
				return canvastask.VideoPoll{Status: "queued"}, nil
			case 2:
				return canvastask.VideoPoll{Status: "running"}, nil
			default:
				return canvastask.VideoPoll{Status: "succeeded", VideoURL: "https://vid.example/cat.mp4"}, nil
			}
		},
	}
	runWorker(t, newTestWorker(store, gateway))

	task := seedVideoTask(t, store, 7, "video-1-1", canvastask.StatusQueued)
	final := awaitTask(t, store, task.ID)

	if final.Status != canvastask.StatusSucceeded || final.VideoURL != "https://vid.example/cat.mp4" || final.AssetID == 0 {
		t.Fatalf("task = %+v, want succeeded with video url and asset", final)
	}
	if final.RemoteTaskID != "vt_remote_success" || final.Attempts != 1 {
		t.Errorf("task = %+v, want the remote handle attached and one attempt", final)
	}

	// 视频产物自动入素材库,kind=video。
	a, err := asset.NewMySQLStore(db).Get(context.Background(), final.AssetID)
	if err != nil {
		t.Fatalf("asset Get: %v", err)
	}
	if a.Kind != asset.KindVideo || a.TaskID != task.ID || a.CanvasID != 7 || a.URL != final.VideoURL {
		t.Errorf("asset = %+v, want the video artifact with the task's provenance", a)
	}

	// 网关提交带齐图生视频的事实与画布来源标记。
	subs := gateway.seenVideoSubmits()
	if len(subs) != 1 {
		t.Fatalf("video submits = %d, want 1", len(subs))
	}
	if subs[0].Model != task.Model || subs[0].Prompt != task.Prompt ||
		subs[0].Seconds != 5 || subs[0].Image != "https://img.example/cat.png" {
		t.Errorf("submit = %+v, want the task's generation facts", subs[0])
	}
	if subs[0].Source != "canvas=7 task="+task.ID+" node=video-1-1" {
		t.Errorf("source = %q, want the canvas origin mark", subs[0].Source)
	}
	// 成功路径上没有任何取消。
	if cancels := gateway.seenVideoCancels(); len(cancels) != 0 {
		t.Errorf("cancels = %v, want none on the happy path", cancels)
	}
}

func TestWorkerVideoTaskFailsWithUpstreamReason(t *testing.T) {
	store, _ := openTaskTestDB(t)
	gateway := &stubGateway{
		submitFn: func(_ context.Context, _ canvastask.VideoRequest) (canvastask.VideoSubmitResult, error) {
			return canvastask.VideoSubmitResult{TaskID: "vt_remote_failed"}, nil
		},
		pollFn: func(int) (canvastask.VideoPoll, error) {
			return canvastask.VideoPoll{Status: "failed", Error: "content policy violation"}, nil
		},
	}
	runWorker(t, newTestWorker(store, gateway))

	task := seedVideoTask(t, store, 7, "video-1-1", canvastask.StatusQueued)
	final := awaitTask(t, store, task.ID)

	if final.Status != canvastask.StatusFailed {
		t.Fatalf("status = %s, want failed", final.Status)
	}
	if !strings.Contains(final.Error, "content policy violation") {
		t.Errorf("error = %q, want the upstream failure reason", final.Error)
	}
	if final.AssetID != 0 || final.VideoURL != "" {
		t.Errorf("task = %+v, want no artifact on failure", final)
	}
}

func TestWorkerVideoTaskObservesUpstreamCancel(t *testing.T) {
	store, _ := openTaskTestDB(t)
	gateway := &stubGateway{
		submitFn: func(_ context.Context, _ canvastask.VideoRequest) (canvastask.VideoSubmitResult, error) {
			return canvastask.VideoSubmitResult{TaskID: "vt_remote_canceled"}, nil
		},
		pollFn: func(int) (canvastask.VideoPoll, error) {
			return canvastask.VideoPoll{Status: "canceled"}, nil
		},
	}
	runWorker(t, newTestWorker(store, gateway))

	task := seedVideoTask(t, store, 7, "video-1-1", canvastask.StatusQueued)
	final := awaitTask(t, store, task.ID)
	if final.Status != canvastask.StatusCanceled {
		t.Fatalf("status = %s, want canceled", final.Status)
	}
}

func TestWorkerVideoToleratesTransientPollFailures(t *testing.T) {
	store, _ := openTaskTestDB(t)
	gateway := &stubGateway{
		submitFn: func(_ context.Context, _ canvastask.VideoRequest) (canvastask.VideoSubmitResult, error) {
			return canvastask.VideoSubmitResult{TaskID: "vt_remote_flaky"}, nil
		},
		pollFn: func(call int) (canvastask.VideoPoll, error) {
			if call == 1 {
				return canvastask.VideoPoll{}, errors.New("gateway 502: upstream hiccup")
			}
			return canvastask.VideoPoll{Status: "succeeded", VideoURL: "https://vid.example/ok.mp4"}, nil
		},
	}
	runWorker(t, newTestWorker(store, gateway))

	task := seedVideoTask(t, store, 7, "video-1-1", canvastask.StatusQueued)
	final := awaitTask(t, store, task.ID)
	if final.Status != canvastask.StatusSucceeded || final.VideoURL != "https://vid.example/ok.mp4" {
		t.Fatalf("task = %+v, want the transient poll failure to be survived", final)
	}
}

func TestWorkerVideoCancelRaceCancelsRemote(t *testing.T) {
	store, _ := openTaskTestDB(t)
	var taskID string
	gateway := &stubGateway{
		submitFn: func(ctx context.Context, _ canvastask.VideoRequest) (canvastask.VideoSubmitResult, error) {
			// 模拟用户在提交在途时点了取消:网关受理返回的同时,任务行
			// 已被 handler 关闭为 canceled。
			if _, err := store.Cancel(ctx, taskID, 7); err != nil {
				return canvastask.VideoSubmitResult{}, err
			}
			return canvastask.VideoSubmitResult{TaskID: "vt_race_lost"}, nil
		},
		pollFn: func(int) (canvastask.VideoPoll, error) {
			return canvastask.VideoPoll{Status: "running"}, nil
		},
	}
	// 任务先落行、taskID 先就位,worker 后启动:submitFn 依赖它模拟
	// 「提交在途时被取消」。
	task := seedVideoTask(t, store, 7, "video-1-1", canvastask.StatusQueued)
	taskID = task.ID
	runWorker(t, newTestWorker(store, gateway))

	final := awaitTask(t, store, task.ID)
	if final.Status != canvastask.StatusCanceled {
		t.Fatalf("status = %s, want the cancel to stand", final.Status)
	}
	// worker 输掉 AttachRemote 竞态后必须当场取消刚受理的网关任务,
	// 预扣由网关原路退回。awaitTask 在行到终态的瞬间就返回,而取消
	// 发生在其后的一瞬 —— 轮询等它,别跟 worker 抢跑。
	deadline := time.Now().Add(2 * time.Second)
	for {
		cancels := gateway.seenVideoCancels()
		if len(cancels) == 1 && cancels[0] == "vt_race_lost" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("cancels = %v, want exactly the race-lost remote canceled", cancels)
		}
		time.Sleep(10 * time.Millisecond)
	}
	// 没有素材落库。
	if final.AssetID != 0 || final.VideoURL != "" {
		t.Errorf("task = %+v, want no artifact on cancel", final)
	}
}

func TestWorkerVideoTimeoutCancelsRemoteAndFails(t *testing.T) {
	store, _ := openTaskTestDB(t)
	gateway := &stubGateway{
		submitFn: func(_ context.Context, _ canvastask.VideoRequest) (canvastask.VideoSubmitResult, error) {
			return canvastask.VideoSubmitResult{TaskID: "vt_slowpoke"}, nil
		},
		pollFn: func(int) (canvastask.VideoPoll, error) {
			return canvastask.VideoPoll{Status: "running"}, nil
		},
	}
	w := canvastask.NewWorker(store, gateway,
		canvastask.WithPollInterval(20*time.Millisecond),
		canvastask.WithVideoPollInterval(20*time.Millisecond),
		canvastask.WithVideoTaskTimeout(150*time.Millisecond),
		canvastask.WithConcurrency(1),
		canvastask.WithLogger(log.New(&strings.Builder{}, "", 0)),
	)
	runWorker(t, w)

	task := seedVideoTask(t, store, 7, "video-1-1", canvastask.StatusQueued)
	final := awaitTask(t, store, task.ID)
	if final.Status != canvastask.StatusFailed {
		t.Fatalf("status = %s, want the deadline to fail the task", final.Status)
	}
	if !strings.Contains(final.Error, "超时") {
		t.Errorf("error = %q, want the timeout reason", final.Error)
	}
	// 时限到必须取消网关任务:预扣由网关退回,任务可重试。
	if cancels := gateway.seenVideoCancels(); len(cancels) != 1 || cancels[0] != "vt_slowpoke" {
		t.Fatalf("cancels = %v, want exactly the remote canceled on timeout", cancels)
	}
}

func TestWorkerVideoRetryCancelsStaleRemote(t *testing.T) {
	store, db := openTaskTestDB(t)
	gateway := &stubGateway{
		submitFn: func(_ context.Context, _ canvastask.VideoRequest) (canvastask.VideoSubmitResult, error) {
			return canvastask.VideoSubmitResult{TaskID: "vt_fresh"}, nil
		},
		pollFn: func(call int) (canvastask.VideoPoll, error) {
			return canvastask.VideoPoll{Status: "succeeded", VideoURL: "https://vid.example/second.mp4"}, nil
		},
	}
	// 模拟重启恢复/重试:任务带着上一次的远端句柄回到队列。句柄必须在
	// worker 启动前落行 —— 否则 worker 可能在 UPDATE 前就把任务认领。
	task := seedVideoTask(t, store, 7, "video-1-1", canvastask.StatusQueued)
	if _, err := db.Exec("UPDATE canvas_tasks SET remote_task_id = 'vt_stale' WHERE id = ?", task.ID); err != nil {
		t.Fatalf("seed stale remote: %v", err)
	}
	runWorker(t, newTestWorker(store, gateway))

	final := awaitTask(t, store, task.ID)
	if final.Status != canvastask.StatusSucceeded || final.VideoURL != "https://vid.example/second.mp4" {
		t.Fatalf("task = %+v, want the fresh submit to succeed", final)
	}
	if final.RemoteTaskID != "vt_fresh" {
		t.Errorf("remote handle = %q, want the fresh submit to take over", final.RemoteTaskID)
	}
	// 上一轮的远端任务被取消:崩溃遗留的预扣由此释放。
	cancels := gateway.seenVideoCancels()
	if len(cancels) != 1 || cancels[0] != "vt_stale" {
		t.Fatalf("cancels = %v, want exactly the stale remote canceled", cancels)
	}
}

func TestWorkerVideoRechecksRowWhenLocalCancelMissedRemote(t *testing.T) {
	store, _ := openTaskTestDB(t)
	// 任务先落行,pollFn 的闭包才有任务 id 可用。
	task := seedVideoTask(t, store, 7, "video-1-1", canvastask.StatusQueued)
	gateway := &stubGateway{
		submitFn: func(_ context.Context, _ canvastask.VideoRequest) (canvastask.VideoSubmitResult, error) {
			return canvastask.VideoSubmitResult{TaskID: "vt_abandoned"}, nil
		},
		pollFn: func(call int) (canvastask.VideoPoll, error) {
			if call == 2 {
				// 模拟 handler 的网关取消 RPC 失败过的情形:行被本地取消,
				// 而网关任务还在跑(轮询一路回答 running)。
				if _, err := store.Cancel(context.Background(), task.ID, 7); err != nil {
					return canvastask.VideoPoll{}, err
				}
			}
			return canvastask.VideoPoll{Status: "running"}, nil
		},
	}
	runWorker(t, newTestWorker(store, gateway))

	// worker 必须在随后的轮询节拍上发现行已终态,补一次网关取消让预扣
	// 退回,然后停止轮询。
	deadline := time.Now().Add(3 * time.Second)
	for {
		cancels := gateway.seenVideoCancels()
		if len(cancels) == 1 && cancels[0] == "vt_abandoned" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("cancels = %v, want the abandoned remote canceled after the row went canceled", cancels)
		}
		time.Sleep(10 * time.Millisecond)
	}
	got, err := store.Get(context.Background(), task.ID)
	if err != nil || got.Status != canvastask.StatusCanceled {
		t.Fatalf("row = %+v err %v, want the canceled state to stand", got, err)
	}
}
