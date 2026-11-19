package assistant

// 回执中枢测试(91 号票):投递路由、错号显式报错、超时收尾。

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestHubDeliverRoutesBySessionAndCall(t *testing.T) {
	hub := NewHub()
	hub.Begin(5, "call_1")

	type awaitResult struct {
		r   Receipt
		err error
	}
	done := make(chan awaitResult, 1)
	go func() {
		r, err := hub.Await(context.Background(), 5, "call_1")
		done <- awaitResult{r: r, err: err}
	}()

	// 错号:在途挂起下投别的 call_id 显式报错,不吞。
	if err := hub.Deliver(5, Receipt{CallID: "call_other", Status: ReceiptApplied}); err != ErrCallMismatch {
		t.Fatalf("mismatch deliver err = %v, want ErrCallMismatch", err)
	}
	// 正投:applied + 结果体按 (session, call) 路由到 Await。
	r := Receipt{CallID: "call_1", Status: ReceiptApplied, Result: []byte(`{"nodes":[{"index":0,"id":"image-9"}]}`)}
	if err := hub.Deliver(5, r); err != nil {
		t.Fatalf("deliver: %v", err)
	}
	out := <-done
	if out.err != nil {
		t.Fatalf("await: %v", out.err)
	}
	if out.r.CallID != r.CallID || out.r.Status != r.Status || string(out.r.Result) != string(r.Result) {
		t.Fatalf("receipt = %+v, want %+v", out.r, r)
	}

	// 收尾后挂起已清:再投 = 无挂起。
	if err := hub.Deliver(5, Receipt{CallID: "call_1", Status: ReceiptApplied}); err != ErrNoPendingCall {
		t.Fatalf("deliver after await err = %v, want ErrNoPendingCall", err)
	}
}

func TestHubDeliverWithoutPendingCallFails(t *testing.T) {
	hub := NewHub()
	if err := hub.Deliver(9, Receipt{CallID: "call_x", Status: ReceiptApplied}); err != ErrNoPendingCall {
		t.Fatalf("err = %v, want ErrNoPendingCall", err)
	}
}

func TestHubAwaitTimesOut(t *testing.T) {
	hub := NewHub()
	hub.timeout = 20 * time.Millisecond
	hub.Begin(3, "call_t")

	start := time.Now()
	_, err := hub.Await(context.Background(), 3, "call_t")
	if !errors.Is(err, ErrReceiptTimeout) {
		t.Fatalf("err = %v, want ErrReceiptTimeout", err)
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("timeout took %v, want the injected short bound", elapsed)
	}
	// 超时后挂起已清,流收尾的 Abort 幂等。
	hub.Abort(3)
}

func TestHubAwaitUnblocksOnContextCancel(t *testing.T) {
	hub := NewHub()
	hub.Begin(4, "call_c")
	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan error, 1)
	go func() {
		_, err := hub.Await(ctx, 4, "call_c")
		done <- err
	}()
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("err = %v, want context.Canceled", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("await did not unblock on cancel")
	}
}

func TestHubClaimsOneStreamPerSession(t *testing.T) {
	hub := NewHub()
	if !hub.TryClaim(7) {
		t.Fatal("first claim rejected")
	}
	if hub.TryClaim(7) {
		t.Fatal("second concurrent claim accepted")
	}
	if !hub.TryClaim(8) {
		t.Fatal("other session claim rejected")
	}
	hub.Release(7)
	if !hub.TryClaim(7) {
		t.Fatal("claim after release rejected")
	}
}
