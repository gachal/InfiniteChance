package canvastask

import (
	"context"
	"errors"
	"fmt"
	"log"
	"time"
	"unicode/utf8"

	"github.com/gachal/InfiniteChance/internal/asset"
	"github.com/gachal/InfiniteChance/internal/objectstore"
)

// Gateway is the worker's view of the relay surface: the synchronous
// text-to-image call (10 号票) and the async video contract's three faces —
// submit, poll, cancel (12 号票). *Client satisfies it; tests substitute
// fakes.
type Gateway interface {
	GenerateImage(ctx context.Context, req ImageRequest) (ImageResult, error)
	SubmitVideo(ctx context.Context, req VideoRequest) (VideoSubmitResult, error)
	PollVideo(ctx context.Context, taskID string) (VideoPoll, error)
	CancelVideo(ctx context.Context, taskID string) error
}

// Worker runs the canvas generation queue server-side (10 号票): claim one
// queued task → call the gateway with the service-level key → finalize the
// task and its asset. Because the loop lives in canvas/server, a generation
// outlives the browser that requested it; results are on the task row when
// the editor comes back.
type Worker struct {
	store   Store
	gateway Gateway
	// storage 落产物字节(14 号票):nil 时跳过转存,素材行只带厂商地址
	// —— 与 10 号票的旧行为一致,测试与未配存储的部署都靠这个 nil 分支。
	storage objectstore.Store

	concurrency  int
	pollInterval time.Duration
	taskTimeout  time.Duration
	// 视频异步任务的两个旋钮(12 号票):网关轮询节奏与单个任务的时限。
	// 视频生成普遍在分钟级,时限远宽于同步生图。
	videoPollInterval time.Duration
	videoTaskTimeout  time.Duration
	logger            *log.Logger
}

// Defaults for a bare NewWorker: two concurrent generations, a 1s queue
// scan, and a 3min ceiling on one image call (同步生图普遍在秒级到分钟级;
// 超时视为失败,任务行给出原因,可重试). Video jobs poll the gateway every
// 3s and get a 15min ceiling — minute-scale generations with headroom; the
// deadline cancels the gateway task so the pre-deduction is refunded.
const (
	defaultConcurrency      = 2
	defaultPollInterval     = time.Second
	defaultTaskTimeout      = 3 * time.Minute
	defaultVideoPoll        = 3 * time.Second
	defaultVideoTaskTimeout = 15 * time.Minute
)

// NewWorker wires a worker with production defaults; the With… options
// override them (tests mostly).
func NewWorker(store Store, gateway Gateway, opts ...WorkerOption) *Worker {
	w := &Worker{
		store:             store,
		gateway:           gateway,
		concurrency:       defaultConcurrency,
		pollInterval:      defaultPollInterval,
		taskTimeout:       defaultTaskTimeout,
		videoPollInterval: defaultVideoPoll,
		videoTaskTimeout:  defaultVideoTaskTimeout,
		logger:            log.Default(),
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

// WithVideoPollInterval sets the cadence of the video task's gateway polls.
func WithVideoPollInterval(d time.Duration) WorkerOption {
	return func(w *Worker) { w.videoPollInterval = d }
}

// WithVideoTaskTimeout bounds one video task from submit to terminal state.
func WithVideoTaskTimeout(d time.Duration) WorkerOption {
	return func(w *Worker) { w.videoTaskTimeout = d }
}

// WithStorage routes generated artifacts into object storage before the
// task finalizes (14 号票); nil keeps the legacy vendor-URL-only rows.
func WithStorage(s objectstore.Store) WorkerOption {
	return func(w *Worker) { w.storage = s }
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

// runOne carries one claimed task through the gateway and closes it out,
// dispatching on the task kind: the synchronous image call, or the async
// video contract's submit-and-poll loop.
func (w *Worker) runOne(parent context.Context, t Task) {
	if t.Kind == KindVideo {
		w.runVideo(parent, t)
		return
	}

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
		w.fail(bookkeeping, t.ID, err.Error())
		return
	}
	a, aerr := w.archive(bookkeeping, t, result.URL)
	if aerr != nil {
		w.fail(bookkeeping, t.ID, aerr.Error())
		return
	}
	finalized, won, err := w.store.FinalizeSuccess(bookkeeping, t.ID, a)
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

// archive 产物转存(14 号票):storage 未配置时返回只带厂商地址的素材
// 事实(旧行为);否则把字节落进对象存储并带上 object_key 三件套。转存
// 失败以 error 上抛,由调用方按任务失败收尾 —— 原因留在任务行上,可重
// 试,不悄悄留下只有临时厂商地址(约 24h 过期)的素材。
func (w *Worker) archive(ctx context.Context, t Task, url string) (asset.Asset, error) {
	a := asset.Asset{
		Kind:     t.Kind,
		CanvasID: t.CanvasID,
		TaskID:   t.ID,
		Model:    t.Model,
		Prompt:   t.Prompt,
		URL:      url,
	}
	if w.storage == nil {
		return a, nil
	}
	stored, err := asset.Transfer(ctx, w.storage, nil, t.CanvasID, t.ID, t.Kind, url)
	if err != nil {
		return asset.Asset{}, err
	}
	a.ObjectKey = stored.Key
	a.ContentType = stored.ContentType
	a.SizeBytes = stored.SizeBytes
	return a, nil
}

// fail closes a task failed with a truncated reason (the error column is
// TEXT; a vendor stack trace is not a reason).
func (w *Worker) fail(ctx context.Context, id, reason string) {
	if utf8.RuneCountInString(reason) > maxErrorRunes {
		reason = string([]rune(reason)[:maxErrorRunes])
	}
	if _, _, ferr := w.store.FinalizeFailure(ctx, id, reason); ferr != nil {
		w.logger.Printf("canvastask: finalize failure %s: %v", id, ferr)
	}
}

// runVideo drives one image-to-video task through the gateway's async
// contract (12 号票): submit → persist the remote handle → poll until the
// gateway reaches a terminal state or the task deadline hits. The user's
// cancel runs on the HTTP handler — it cancels the gateway task (releasing
// the quota hold) and closes the row; the poll loop simply observes whatever
// the gateway then answers, and its guarded finalizers lose cleanly when the
// handler got there first.
func (w *Worker) runVideo(parent context.Context, t Task) {
	ctx, cancel := context.WithTimeout(parent, w.videoTaskTimeout)
	defer cancel()

	bookkeeping := context.WithoutCancel(parent)
	// 重试或重启恢复的任务带着上一次的远端任务:先取消它 —— 失败任务上
	// 它已终态(取消只是对账),崩溃遗留的任务则由此释放预扣。
	if t.RemoteTaskID != "" {
		if err := w.gateway.CancelVideo(bookkeeping, t.RemoteTaskID); err != nil {
			w.logger.Printf("canvastask: cancel stale remote %s for %s: %v", t.RemoteTaskID, t.ID, err)
		}
	}

	w.logger.Printf("canvastask: running %s (canvas %d, node %s, kind video, attempt %d)",
		t.ID, t.CanvasID, t.NodeID, t.Attempts)
	ref, err := w.gateway.SubmitVideo(ctx, VideoRequest{
		Model:   t.Model,
		Prompt:  t.Prompt,
		Seconds: t.Seconds,
		Image:   t.ImageRef,
		Source:  fmt.Sprintf("canvas=%d task=%s node=%s", t.CanvasID, t.ID, t.NodeID),
	})
	if err != nil {
		w.fail(bookkeeping, t.ID, err.Error())
		return
	}

	// 远端句柄落行,取消与恢复才找得到它。行已不在 running(提交在途时
	// 被取消)则当场取消刚受理的网关任务,预扣由网关原路退回。落行本身
	// 的 DB 错误不拦轮询:句柄仍在进程手里,丢进程的极端情形由重启恢复
	// 兜底(与 10 号票同一责任边界)。
	if _, ok, aerr := w.store.AttachRemote(bookkeeping, t.ID, ref.TaskID); aerr != nil {
		w.logger.Printf("canvastask: attach remote %s to %s: %v", ref.TaskID, t.ID, aerr)
	} else if !ok {
		if cerr := w.gateway.CancelVideo(bookkeeping, ref.TaskID); cerr != nil {
			w.logger.Printf("canvastask: cancel race-lost remote %s for %s: %v", ref.TaskID, t.ID, cerr)
		}
		return
	}

	for {
		select {
		case <-ctx.Done():
			// 时限到:取消网关任务让预扣退回,任务行按失败收尾并给出原因
			// (可重试)。取消走脱离时限的 context —— 决定性的收尾不因
			// deadline 到点而丢失。
			if cerr := w.gateway.CancelVideo(bookkeeping, ref.TaskID); cerr != nil {
				w.logger.Printf("canvastask: cancel on timeout for %s (remote %s): %v", t.ID, ref.TaskID, cerr)
			}
			w.fail(bookkeeping, t.ID, fmt.Sprintf("生成超时(上限 %s),已取消网关任务", w.videoTaskTimeout))
			return
		case <-time.After(w.videoPollInterval):
		}

		// 先看本地行:用户取消时 handler 会先取消网关任务再关行,但那次
		// RPC 可能失败(只留日志)。行已不在 running 而网关任务还在跑,
		// 厂商若先完成,网关会照常结算 —— 就地补一次取消,预扣原路退回。
		if row, gerr := w.store.Get(ctx, t.ID); gerr == nil && Terminal(row.Status) {
			if cerr := w.gateway.CancelVideo(bookkeeping, ref.TaskID); cerr != nil {
				w.logger.Printf("canvastask: cancel abandoned remote %s for %s: %v", ref.TaskID, t.ID, cerr)
			}
			return
		}

		poll, err := w.gateway.PollVideo(ctx, ref.TaskID)
		if err != nil {
			// 一次轮询失败不是任务失败:网关的轮询是代理语义,暂态故障
			// 稍后再试;总时限兜底。
			w.logger.Printf("canvastask: poll %s (remote %s): %v", t.ID, ref.TaskID, err)
			continue
		}
		switch poll.Status {
		case string(StatusQueued), string(StatusRunning):
			continue
		case string(StatusSucceeded):
			if poll.VideoURL == "" {
				w.fail(bookkeeping, t.ID, "网关报告任务成功但没有视频地址")
				return
			}
			a, aerr := w.archive(bookkeeping, t, poll.VideoURL)
			if aerr != nil {
				w.fail(bookkeeping, t.ID, aerr.Error())
				return
			}
			finalized, won, err := w.store.FinalizeVideoSuccess(bookkeeping, t.ID, a)
			if err != nil {
				w.logger.Printf("canvastask: finalize success %s: %v", t.ID, err)
				return
			}
			if !won {
				w.logger.Printf("canvastask: finalize success %s lost the race (status now %s)", t.ID, finalized.Status)
				return
			}
			w.logger.Printf("canvastask: task %s succeeded, asset %d", t.ID, finalized.AssetID)
			return
		case string(StatusCanceled):
			if _, _, ferr := w.store.FinalizeCanceled(bookkeeping, t.ID); ferr != nil {
				w.logger.Printf("canvastask: finalize canceled %s: %v", t.ID, ferr)
			}
			return
		default: // failed(网关已把未知态归并 failed)
			reason := poll.Error
			if reason == "" {
				reason = "上游报告任务失败"
			}
			w.fail(bookkeeping, t.ID, reason)
			return
		}
	}
}
