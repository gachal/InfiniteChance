// Package canvastask is the canvas side of generation orchestration
// (10 号票): one durable row per canvas generation, driven by the
// canvas/server worker — the browser can close mid-generation and the task
// keeps running server-side; reopening finds the task (and its asset) by
// canvas. A task is bound to the canvas node that displays its result
// (node_id is the editor's client-side node id), so the graph JSON never has
// to be edited server-side. Failures stay on the node and are retried in
// place; every retry re-enters the queue as a fresh attempt.
package canvastask

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"time"

	"github.com/gachal/InfiniteChance/internal/asset"
)

// ErrNotFound reports a task id that has no row.
var ErrNotFound = errors.New("canvastask: not found")

// ErrNotRetryable reports a retry against a task that is not failed —
// only failed tasks may be sent back to the queue.
var ErrNotRetryable = errors.New("canvastask: not retryable")

// Task states. The image flow is two-phase (queued → running → terminal):
// queued = accepted, waiting for a worker; running = a worker holds it and
// the gateway call is in flight; succeeded/failed are final until a failed
// task is retried back into the queue. Cancellation is not part of this
// contract — the synchronous image call cannot be revoked once submitted.
const (
	StatusQueued    Status = "queued"
	StatusRunning   Status = "running"
	StatusSucceeded Status = "succeeded"
	StatusFailed    Status = "failed"
)

// Status is one canvas task's lifecycle state.
type Status string

// Task kinds. 10 号票只落文生图;12 号票的图生视频沿用同一张表与状态机。
const KindImage = "image"

// idPrefix marks canvas-server-issued task ids; idBytes double to 32 hex
// chars, keeping the full id at 35 — same shape as the gateway's vt_ ids.
const (
	idPrefix = "ct_"
	idBytes  = 16
)

// NewID mints one public task id: ct_ + 32 hex chars from crypto/rand.
func NewID() (string, error) {
	buf := make([]byte, idBytes)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return idPrefix + hex.EncodeToString(buf), nil
}

// Task is one canvas generation job. NodeID binds it to the editor node that
// renders the outcome; Attempts counts gateway submissions (a retried task
// keeps its row and grows the count, so audit can see repeats).
type Task struct {
	ID        string
	CanvasID  int64
	NodeID    string
	Kind      string
	Prompt    string
	Model     string
	Size      string
	Status    Status
	Attempts  int64
	Error     string // failed 的原因摘要;重试入队时清空
	AssetID   int64  // succeeded 时产物素材
	ImageURL  string // succeeded 时产物地址(与素材行同值,节点直读)
	CreatedAt time.Time
	UpdatedAt time.Time
}

// Store persists canvas tasks and drives the state machine. Every
// transition is a conditional UPDATE whose WHERE clause picks the winner:
// two workers can never claim one task, and a finalize can never land after
// a retry resurrected the row.
type Store interface {
	// Create stores a new task (id and facts already set, status queued)
	// and returns it with timestamps from the database.
	Create(ctx context.Context, t Task) (Task, error)
	// Get returns the task or ErrNotFound.
	Get(ctx context.Context, id string) (Task, error)
	// ListByCanvas returns the canvas's tasks, newest first, capped at limit.
	ListByCanvas(ctx context.Context, canvasID int64, limit int) ([]Task, error)
	// Claim atomically moves one queued task (FIFO) to running and bumps its
	// attempt counter; ErrNotFound when the queue is empty.
	Claim(ctx context.Context) (Task, error)
	// RequeueRunning returns running tasks to the queue — startup recovery
	// for tasks orphaned by a server restart — and reports how many moved.
	RequeueRunning(ctx context.Context) (int64, error)
	// FinalizeSuccess inserts the asset and closes the task succeeded in one
	// transaction: 产物入素材库与任务终态原子,要么都发生要么都不。Expects
	// status running; a lost race returns the current row with ok=false and
	// the caller must not treat the outcome as recorded.
	FinalizeSuccess(ctx context.Context, id string, a asset.Asset) (Task, bool, error)
	// FinalizeFailure closes the task failed with a reason. Guarded on
	// running, same winner semantics as FinalizeSuccess.
	FinalizeFailure(ctx context.Context, id, errMsg string) (Task, bool, error)
	// ResetForRetry returns one failed task of the given canvas to the queue,
	// clearing the failure fields; ErrNotRetryable when the row exists but is
	// not failed, ErrNotFound when there is no row.
	ResetForRetry(ctx context.Context, id string, canvasID int64) (Task, error)
}
