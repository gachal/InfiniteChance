package canvastask

import (
	"context"
	"errors"
	"fmt"
	"log"
	"time"
	"unicode/utf8"

	"github.com/gachal/InfiniteChance/internal/asset"
)

// Gateway is the worker's view of the relay surface: one synchronous
// text-to-image call. *Client satisfies it; tests substitute fakes.
type Gateway interface {
	GenerateImage(ctx context.Context, req ImageRequest) (ImageResult, error)
}

// Worker runs the canvas generation queue server-side (10 号票): claim one
// queued task → call the gateway with the service-level key → finalize the
// task and its asset. Because the loop lives in canvas/server, a generation
// outlives the browser that requested it; results are on the task row when
// the editor comes back.
type Worker struct {
	store   Store
	gateway Gateway

	concurrency  int
	pollInterval time.Duration
	taskTimeout  time.Duration
	logger       *log.Logger
}

// Defaults for a bare NewWorker: two concurrent generations, a 1s queue
// scan, and a 3min ceiling on one image call (同步生图普遍在秒级到分钟级;
// 超时视为失败,任务行给出原因,可重试).
const (
	defaultConcurrency  = 2
	defaultPollInterval = time.Second
	defaultTaskTimeout  = 3 * time.Minute
)

// NewWorker wires a worker with production defaults; the With… options
// override them (tests mostly).
func NewWorker(store Store, gateway Gateway, opts ...WorkerOption) *Worker {
	w := &Worker{
		store:        store,
		gateway:      gateway,
		concurrency:  defaultConcurrency,
		pollInterval: defaultPollInterval,
		taskTimeout:  defaultTaskTimeout,
		logger:       log.Default(),
	}
	for _, opt := range opts {
		opt(w)
	}
	return w
}

// WorkerOption customizes one Worker knob.
type WorkerOption func(*Worker)

// WithConcurrency caps how many generations run at once.
func WithConcurrency(n int) WorkerOption {
	return func(w *Worker) {
		if n > 0 {
			w.concurrency = n
		}
	}
}

// WithPollInterval sets the idle scan cadence of the queue loop.
func WithPollInterval(d time.Duration) WorkerOption {
	return func(w *Worker) { w.pollInterval = d }
}

// WithTaskTimeout bounds one gateway call.
func WithTaskTimeout(d time.Duration) WorkerOption {
	return func(w *Worker) { w.taskTimeout = d }
}

// WithLogger routes worker logging (nil keeps the default logger).
func WithLogger(l *log.Logger) WorkerOption {
	return func(w *Worker) {
		if l != nil {
			w.logger = l
		}
	}
}

// maxErrorRunes keeps a failure reason well inside the TEXT error column.
const maxErrorRunes = 500

// Run drives the queue until ctx is canceled. Each free concurrency slot
// claims the next queued task and runs it in its own goroutine; when the
// queue is empty the loop sleeps a poll interval.
func (w *Worker) Run(ctx context.Context) {
	slots := make(chan struct{}, w.concurrency)
	for {
		// 空位先占住再认领:没有空位就不从队列拿任务,任务留在 queued。
		select {
		case <-ctx.Done():
			return
		case slots <- struct{}{}:
		}

		task, err := w.store.Claim(ctx)
		if errors.Is(err, ErrNotFound) {
			<-slots
			if !w.sleep(ctx) {
				return
			}
			continue
		}
		if err != nil {
			<-slots
			w.logger.Printf("canvastask: claim: %v", err)
			if !w.sleep(ctx) {
				return
			}
			continue
		}

		go func(t Task) {
			defer func() { <-slots }()
			w.runOne(ctx, t)
		}(task)
	}
}

// sleep waits one poll interval, reporting whether the loop should go on.
func (w *Worker) sleep(ctx context.Context) bool {
	select {
	case <-ctx.Done():
		return false
	case <-time.After(w.pollInterval):
		return true
	}
}

// runOne carries one claimed task through the gateway and closes it out.
// Finalizes run on a context detached from the task deadline: the outcome
// (success or the timeout reason) must reach the row even at the deadline's
// edge; only a dead process loses it, and boot-time recovery requeues those.
func (w *Worker) runOne(parent context.Context, t Task) {
	ctx, cancel := context.WithTimeout(parent, w.taskTimeout)
	defer cancel()

	w.logger.Printf("canvastask: running %s (canvas %d, node %s, attempt %d)",
		t.ID, t.CanvasID, t.NodeID, t.Attempts)
	result, err := w.gateway.GenerateImage(ctx, ImageRequest{
		Model:  t.Model,
		Prompt: t.Prompt,
		Size:   t.Size,
		Source: fmt.Sprintf("canvas=%d task=%s node=%s", t.CanvasID, t.ID, t.NodeID),
	})
	bookkeeping := context.WithoutCancel(parent)
	if err != nil {
		reason := err.Error()
		if utf8.RuneCountInString(reason) > maxErrorRunes {
			reason = string([]rune(reason)[:maxErrorRunes])
		}
		if _, _, ferr := w.store.FinalizeFailure(bookkeeping, t.ID, reason); ferr != nil {
			w.logger.Printf("canvastask: finalize failure %s: %v", t.ID, ferr)
		}
		return
	}
	finalized, won, err := w.store.FinalizeSuccess(bookkeeping, t.ID, asset.Asset{
		Kind:     asset.KindImage,
		CanvasID: t.CanvasID,
		TaskID:   t.ID,
		Model:    t.Model,
		Prompt:   t.Prompt,
		URL:      result.URL,
	})
	if err != nil {
		w.logger.Printf("canvastask: finalize success %s: %v", t.ID, err)
		return
	}
	if !won {
		// 终态被抢先(理论上只有重启恢复可能撞上):不重复落素材。
		w.logger.Printf("canvastask: finalize success %s lost the race (status now %s)", t.ID, finalized.Status)
		return
	}
	w.logger.Printf("canvastask: task %s succeeded, asset %d", t.ID, finalized.AssetID)
}
