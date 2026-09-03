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
// requests it saw and answers from a script.
type stubGateway struct {
	mu       sync.Mutex
	requests []canvastask.ImageRequest

	// fn, when set, answers every call (and may block).
	fn func(context.Context, canvastask.ImageRequest) (canvastask.ImageResult, error)
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

func (g *stubGateway) seen() []canvastask.ImageRequest {
	g.mu.Lock()
	defer g.mu.Unlock()
	return append([]canvastask.ImageRequest(nil), g.requests...)
}

// newTestWorker wires a worker tuned for tests: fast polling, short task
// timeouts, quiet logging.
func newTestWorker(store *canvastask.MySQLStore, gateway canvastask.Gateway) *canvastask.Worker {
	return canvastask.NewWorker(store, gateway,
		canvastask.WithPollInterval(20*time.Millisecond),
		canvastask.WithTaskTimeout(2*time.Second),
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

// awaitTask polls the store until the task leaves the active states or the
// deadline passes — the test-side twin of the editor's polling.
func awaitTask(t *testing.T, store *canvastask.MySQLStore, id string) canvastask.Task {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		task, err := store.Get(context.Background(), id)
		if err != nil {
			t.Fatalf("Get %s: %v", id, err)
		}
		if task.Status == canvastask.StatusSucceeded || task.Status == canvastask.StatusFailed {
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
