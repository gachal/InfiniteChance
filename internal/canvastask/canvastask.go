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
	"encoding/json"
	"errors"
	"time"

	"github.com/gachal/InfiniteChance/internal/asset"
)

// ErrNotFound reports a task id that has no row.
var ErrNotFound = errors.New("canvastask: not found")

// ErrNotRetryable reports a retry against a task that is not failed —
// only failed tasks may be sent back to the queue.
var ErrNotRetryable = errors.New("canvastask: not retryable")

// ErrNotCancelable reports a cancel against a task that is no longer in
// flight — only queued/running tasks can be withdrawn.
var ErrNotCancelable = errors.New("canvastask: not cancelable")

// Task states. The image flow is two-phase (queued → running → terminal):
// queued = accepted, waiting for a worker; running = a worker holds it and
// the gateway call is in flight; succeeded/failed are final until a failed
// task is retried back into the queue. Cancellation is not part of the
// image contract — the synchronous image call cannot be revoked once
// submitted. 12 号票的图生视频走网关的异步契约,canceled 因此进状态机:
// 用户撤回在途任务,行就地关闭,网关侧预扣原路退回。
const (
	StatusQueued    Status = "queued"
	StatusRunning   Status = "running"
	StatusSucceeded Status = "succeeded"
	StatusFailed    Status = "failed"
	StatusCanceled  Status = "canceled"
)

// Status is one canvas task's lifecycle state.
type Status string

// Terminal reports whether s closes the task: succeeded / failed / canceled.
func Terminal(s Status) bool {
	return s != StatusQueued && s != StatusRunning
}

// Task kinds. 10 号票落了文生图;12 号票的图生视频沿用同一张表与状态机。
const (
	KindImage = "image"
	KindVideo = "video"
)

// 视频参考的角色集(24 号票,对齐即梦「全能参考」):首帧/尾帧/参考图是
// 图片,参考视频/参考音频各自一种媒体。角色决定网关侧的落位 —— worker
// 翻译 first_frame → image、last_frame → last_image,其余按 kind 进
// references(25 号票契约)。
const (
	RoleFirstFrame   = "first_frame"
	RoleLastFrame    = "last_frame"
	RoleReferenceImg = "reference_image"
	RoleReferenceVid = "reference_video"
	RoleReferenceAud = "reference_audio"
)

// VideoRef is one structured video reference (24 号票):the address the
// vendor fetches (resolved at submit time), the media kind it carries, and
// the role it plays in the generation. Roles map onto kinds one way —
// first_frame/last_frame/reference_image carry images, reference_video
// videos, reference_audio audio — enforced on both the wire input and the
// asset rows referenced through it.
type VideoRef struct {
	URL  string `json:"url"`
	Kind string `json:"kind"`
	Role string `json:"role"`
}

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
// keeps its row and grows the count, so audit can see repeats). Video tasks
// (12 号票) additionally carry the reference image, the clip length, the
// gateway task handle once the submit was accepted, and the delivered video
// address; the image product column stays image-only. Image tasks with
// references (21 号票的图生图) carry the resolved vendor addresses in
// ImageRefs — resolved at submit time so the row is self-sufficient across
// retries and restarts. Video tasks with structured references (24 号票)
// carry them in VideoRefs the same way — resolved, kind-checked, capped;
// ImageRef remains the single-string legacy shape old rows keep, and Seconds
// 0 means "auto" (omit seconds on the wire, vendor default applies).
// Ratio/Size carry the aspect-ratio and resolution tier strings verbatim.
// Background (37 号票) is the image task's transparency request — 空串 =
// 不传,非空只可能是 "transparent"(createInput 已校验),worker 非空即
// 上送网关;视频任务不读它(视频透明显式 out of scope)。
type Task struct {
	ID           string
	CanvasID     int64
	NodeID       string
	Kind         string
	Prompt       string
	Model        string
	Size         string
	Ratio        string     // video: 显式宽高比串(如 16:9);空 = 不传
	Background   string     // image: "transparent" = 透明背景;空 = 不传
	Seconds      int64      // video: 期望时长(秒);0 = 自动(不传,厂商缺省);image 恒 0
	ImageRef     string     // video: 图生视频的单串参考图(12 号票旧形态)
	VideoRefs    []VideoRef // video: 结构化多模态参考(24 号票,已解析);空 = 文生视频
	ImageRefs    []string   // image: 图生图的参考图片地址(已解析,21 号票);空 = 文生图
	Status       Status
	Attempts     int64
	Error        string // failed 的原因摘要;重试入队时清空
	AssetID      int64  // succeeded 时产物素材
	ImageURL     string // image 任务成功时的产物地址(与素材行同值)
	VideoURL     string // video 任务成功时的产物地址(与素材行同值)
	RemoteTaskID string // 网关侧任务 id(vt_…),提交受理后回填
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

// encodeVideoRefs serializes the structured reference list for its column;
// an empty list lands as NULL. Marshaling cannot fail, so the error branch
// exists only to keep the signature honest.
func encodeVideoRefs(refs []VideoRef) any {
	if len(refs) == 0 {
		return nil
	}
	b, err := json.Marshal(refs)
	if err != nil {
		return nil
	}
	return string(b)
}

// decodeVideoRefs parses the column back; anything unreadable reads as no
// references (文生视频) — a corrupt column must not take the worker down.
func decodeVideoRefs(s string) []VideoRef {
	if s == "" {
		return nil
	}
	var refs []VideoRef
	if err := json.Unmarshal([]byte(s), &refs); err != nil {
		return nil
	}
	return refs
}

// encodeImageRefs serializes the reference list for its TEXT column; an
// empty list lands as NULL. Marshaling []string cannot fail, so the error
// branch exists only to keep the signature honest.
func encodeImageRefs(refs []string) any {
	if len(refs) == 0 {
		return nil
	}
	b, err := json.Marshal(refs)
	if err != nil {
		return nil
	}
	return string(b)
}

// decodeImageRefs parses the column back; anything unreadable reads as no
// references (文生图) — a corrupt column must not take the worker down.
func decodeImageRefs(s string) []string {
	if s == "" {
		return nil
	}
	var refs []string
	if err := json.Unmarshal([]byte(s), &refs); err != nil {
		return nil
	}
	return refs
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
	// the caller must not treat the outcome as recorded. The delivered URL
	// lands in image_url.
	FinalizeSuccess(ctx context.Context, id string, a asset.Asset) (Task, bool, error)
	// FinalizeVideoSuccess is the video twin of FinalizeSuccess: same
	// transaction shape, the delivered URL lands in video_url and the asset
	// carries kind video.
	FinalizeVideoSuccess(ctx context.Context, id string, a asset.Asset) (Task, bool, error)
	// FinalizeFailure closes the task failed with a reason. Guarded on
	// running, same winner semantics as FinalizeSuccess.
	FinalizeFailure(ctx context.Context, id, errMsg string) (Task, bool, error)
	// FinalizeCanceled closes the task canceled (the user withdrew it, or a
	// poll observed the gateway task canceled). Guarded on running, same
	// winner semantics as the other finalizers.
	FinalizeCanceled(ctx context.Context, id string) (Task, bool, error)
	// Cancel closes one active (queued/running) task of the given canvas as
	// canceled: ErrNotRetryable-style, ErrNotCancelable when the row exists
	// but already reached a terminal state, ErrNotFound when there is no row.
	Cancel(ctx context.Context, id string, canvasID int64) (Task, error)
	// AttachRemote records the gateway task handle on a running video task,
	// after the submit was accepted and before polling starts. ok=false
	// reports the row already left running — the user canceled while the
	// submit was in flight — and the caller must cancel the just-created
	// gateway task (releasing the quota hold) and stop.
	AttachRemote(ctx context.Context, id, remoteTaskID string) (Task, bool, error)
	// ResetForRetry returns one failed task of the given canvas to the queue,
	// clearing the failure fields; ErrNotRetryable when the row exists but is
	// not failed, ErrNotFound when there is no row.
	ResetForRetry(ctx context.Context, id string, canvasID int64) (Task, error)
}
