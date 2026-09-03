// Package videotask is the gateway side of the async video contract
// (08 号票): one durable row per submitted generation task, the external
// five-state machine, and the store the relay surface drives through
// submit → poll → cancel. Status merging (vendor raw states → the five
// external states) lives here too — the task entity owns its own state
// machine; the relay only feeds it vendor answers.
package videotask

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"strings"
	"time"
)

// ErrNotFound reports a task id that has no row.
var ErrNotFound = errors.New("videotask: not found")

// External task states (CONTEXT.md 任务):the contract every client programs
// against. Upstream throttled states merge into queued, unknown states into
// failed — MergeStatus owns both rules.
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

// MergeStatus folds a vendor's raw task status into the external machine.
// The vocabulary is the wayfinder vendor matrix's (Kling submitted,
// DashScope PENDING/UNKNOWN, Runway THROTTLED, …), matched
// case-insensitively: throttled states read as queued (排队语义), unknown
// and unrecognized states read as failed (未知态归并 failed,08 号票定案).
func MergeStatus(raw string) Status {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "queued", "pending", "submitted", "created", "queueing", "in_queue", "preparing", "throttled":
		return StatusQueued
	case "running", "processing", "in_progress":
		return StatusRunning
	case "succeeded", "succeed", "success", "completed", "done":
		return StatusSucceeded
	case "canceled", "cancelled":
		return StatusCanceled
	default:
		return StatusFailed
	}
}

// idPrefix marks gateway-issued task ids; idBytes double to 32 hex chars,
// keeping the full id at 35 — far under the column bound.
const (
	idPrefix = "vt_"
	idBytes  = 16
)

// NewID mints one public task id: vt_ + 32 hex chars from crypto/rand.
func NewID() (string, error) {
	buf := make([]byte, idBytes)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return idPrefix + hex.EncodeToString(buf), nil
}

// Task is one async video generation the gateway proxies for a caller. The
// channel identity is pinned at submit (ChannelName snapshotted as text so
// the trail survives a channel deletion, like usage.Log); price snapshot and
// the reserved amount carry the billing facts from submit time — polls
// never re-price.
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
	Size           string
	Seconds        int64
	VideoURL       string // succeeded 时的产物地址
	Error          string // failed/canceled 的原因摘要
	ReservedMicros int64
	ChargeMicros   int64 // succeeded 时定格为预扣额
	PriceSnapshot  []byte
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

// Patch is the mutable slice of a task row for Update. Status is required;
// nil pointers leave the column untouched (cancel keeps the last raw
// upstream status, finalizes stamp the one that decided the outcome).
type Patch struct {
	Status         Status
	UpstreamStatus *string
	VideoURL       *string
	ErrMsg         *string
	ChargeMicros   *int64
}

// Store persists video tasks. Update is the concurrency anchor: the status
// guard rides inside the UPDATE, so two racing polls (or a poll racing a
// cancel) can never both finalize the task — exactly one call wins, and the
// winner is the one that bills or refunds.
type Store interface {
	// Create stores a new task (id and facts already set) and returns it
	// with timestamps from the database.
	Create(ctx context.Context, t Task) (Task, error)
	// Get returns the task or ErrNotFound.
	Get(ctx context.Context, id string) (Task, error)
	// Update applies patch to the task while its current status is still one
	// of expect, atomically. It returns the fresh row and whether this call
	// performed the transition — false means the task already moved past
	// expect (a concurrent finalize won), and the caller must not bill.
	Update(ctx context.Context, id string, expect []Status, p Patch) (Task, bool, error)
}
