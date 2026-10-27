// Package musictask is the gateway side of the async music contract
// (60 号票): one durable row per submitted 配乐 task, the external
// five-state machine, and the store the relay surface drives through
// submit → poll. 契约照 /v1/videos 的两面(受理回任务句柄、轮询五态),
// 但没有取消面 —— MPS 取消接口未查证,任务只能被上游推进到终态或由
// 网关的计费时限兜底收尾。状态归并(MPS 原始态 → 外部五态)与任务实体
// 同住这里;relay 只喂厂商应答。
package musictask

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"strings"
	"time"
)

// ErrNotFound reports a task id that has no row.
var ErrNotFound = errors.New("musictask: not found")

// External task states — the same five-state machine as the video contract
// (queued/running/succeeded/failed/canceled)。canceled 在本契约里不可达
// (无取消面),保留在状态机里只为与 /v1/videos 同形。
const (
	StatusQueued    Status = "queued"
	StatusRunning   Status = "running"
	StatusSucceeded Status = "succeeded"
	StatusFailed    Status = "failed"
	StatusCanceled  Status = "canceled"
)

// Status is one of the five external task states.
type Status string

// ActiveStatuses are the states a task can still leave; anything else is
// final and billing is already closed out.
var ActiveStatuses = []Status{StatusQueued, StatusRunning}

// Terminal reports whether s closes the task: succeeded / failed / canceled.
func Terminal(s Status) bool {
	return s != StatusQueued && s != StatusRunning
}

// MergeStatus folds a vendor's raw task status into the external machine,
// matched case-insensitively. 音乐上游今日只有 MPS(60 号票),词表以其
// DescribeAigcAudioTask 为准:WAIT(等待)/ RUN(执行中)/ DONE(成功)/
// FAIL(失败);已知词表照抄视频轨(同族供应商词汇收敛到同一外部语义),
// 未知态归并 failed —— 任务没交付就不该被当成还在跑。
func MergeStatus(raw string) Status {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "queued", "pending", "submitted", "created", "queueing", "in_queue", "preparing", "throttled",
		"wait", "waiting":
		return StatusQueued
	case "running", "processing", "in_progress", "run":
		return StatusRunning
	case "succeeded", "succeed", "success", "completed", "done":
		return StatusSucceeded
	case "canceled", "cancelled":
		return StatusCanceled
	default:
		return StatusFailed
	}
}

// idPrefix marks gateway-issued music task ids; idBytes double to 32 hex
// chars, keeping the full id at 35 — same shape as the video face's vt_ ids.
const (
	idPrefix = "mt_"
	idBytes  = 16
)

// NewID mints one public task id: mt_ + 32 hex chars from crypto/rand.
func NewID() (string, error) {
	buf := make([]byte, idBytes)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return idPrefix + hex.EncodeToString(buf), nil
}

// Task is one async music generation the gateway proxies for a caller. The
// channel identity is pinned at submit (ChannelName snapshotted as text so
// the trail survives a channel deletion); price snapshot and the reserved
// amount carry the billing facts from submit time — polls never re-price.
// 配乐按次计费,行上没有尺寸/秒数维度;产物地址在 AudioURL(succeeded 时
// 的上游临时地址,约 24h 过期,转存是调用方的事)。
type Task struct {
	ID             string
	KeyID          int64
	ChannelID      int64
	ChannelName    string
	PublicModel    string
	UpstreamModel  string
	UpstreamTaskID string
	Status         Status
	UpstreamStatus string // 最近一次上游原始状态(未轮询过为空)
	AudioURL       string // succeeded 时的产物地址
	Error          string // failed 的原因摘要
	ReservedMicros int64
	ChargeMicros   int64 // succeeded 时定格为预扣额
	PriceSnapshot  []byte
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

// Patch is the mutable slice of a task row for Update. Status is required;
// nil pointers leave the column untouched.
type Patch struct {
	Status         Status
	UpstreamStatus *string
	AudioURL       *string
	ErrMsg         *string
	ChargeMicros   *int64
}

// Store persists music tasks. Update is the concurrency anchor: the status
// guard rides inside the UPDATE's WHERE clause, so exactly one racing poll
// wins the terminal transition and closes the books; the loser reads the
// settled row back and must not bill again.
type Store interface {
	// Create stores a new task (id and facts already set, status queued) and
	// returns it with timestamps from the database.
	Create(ctx context.Context, t Task) (Task, error)
	// Get returns the task or ErrNotFound.
	Get(ctx context.Context, id string) (Task, error)
	// Update applies patch to the rows still in one of expect's states and
	// reports whether the caller won (won=false: the row already moved on;
	// the current row comes back for the caller to answer from).
	Update(ctx context.Context, id string, expect []Status, p Patch) (Task, bool, error)
}
