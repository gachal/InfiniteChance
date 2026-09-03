package videotask_test

import (
	"testing"

	"github.com/gachal/InfiniteChance/internal/videotask"
)

// MergeStatus 是五态归并的纯函数缝(08 号票验收:「fake 上游的多种真实
// 状态被正确归并为对外五态」的词汇表本体)。样例全部来自 wayfinder 厂商
// 调研票里的真实厂商状态名。
func TestMergeStatusVendorVocabulary(t *testing.T) {
	for _, tc := range []struct {
		raw  string
		want videotask.Status
	}{
		// 排队语义:各家排队态 + Runway THROTTLED(节流归并 queued)。
		{"queued", videotask.StatusQueued},
		{"PENDING", videotask.StatusQueued},
		{"submitted", videotask.StatusQueued}, // Kling
		{"created", videotask.StatusQueued},   // Vidu
		{"Queueing", videotask.StatusQueued},  // Vidu
		{"Preparing", videotask.StatusQueued}, // MiniMax
		{"IN_QUEUE", videotask.StatusQueued},  // fal
		{"THROTTLED", videotask.StatusQueued}, // Runway:节流未排队
		// 运行态。
		{"running", videotask.StatusRunning},
		{"RUNNING", videotask.StatusRunning},
		{"processing", videotask.StatusRunning},
		{"IN_PROGRESS", videotask.StatusRunning},
		// 成功态。
		{"succeeded", videotask.StatusSucceeded},
		{"SUCCEEDED", videotask.StatusSucceeded},
		{"succeed", videotask.StatusSucceeded}, // Kling
		{"Success", videotask.StatusSucceeded}, // MiniMax
		{"COMPLETED", videotask.StatusSucceeded},
		// 失败态,含 DashScope UNKNOWN(过期)归并 failed。
		{"failed", videotask.StatusFailed},
		{"FAILED", videotask.StatusFailed},
		{"Fail", videotask.StatusFailed},
		{"UNKNOWN", videotask.StatusFailed},
		{"expired", videotask.StatusFailed},
		// 取消态:两种拼写都收。
		{"canceled", videotask.StatusCanceled},
		{"CANCELLED", videotask.StatusCanceled},
		{"cancelled", videotask.StatusCanceled},
		// 未知态一律归并 failed(未知态归并 failed)。
		{"brand-new-vendor-state", videotask.StatusFailed},
		{"", videotask.StatusFailed},
	} {
		if got := videotask.MergeStatus(tc.raw); got != tc.want {
			t.Errorf("MergeStatus(%q) = %s, want %s", tc.raw, got, tc.want)
		}
	}
}

func TestTerminalStates(t *testing.T) {
	for _, s := range []videotask.Status{videotask.StatusQueued, videotask.StatusRunning} {
		if videotask.Terminal(s) {
			t.Errorf("Terminal(%s) = true, want active", s)
		}
	}
	for _, s := range []videotask.Status{videotask.StatusSucceeded, videotask.StatusFailed, videotask.StatusCanceled} {
		if !videotask.Terminal(s) {
			t.Errorf("Terminal(%s) = false, want terminal", s)
		}
	}
}

func TestNewIDShape(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 100; i++ {
		id, err := videotask.NewID()
		if err != nil {
			t.Fatalf("NewID: %v", err)
		}
		if len(id) != 35 || id[:3] != "vt_" {
			t.Fatalf("id = %q, want vt_ + 32 hex chars", id)
		}
		if seen[id] {
			t.Fatalf("id %q repeated", id)
		}
		seen[id] = true
	}
}
