package channel

import (
	"testing"
	"time"
)

// 熔断器(06 号票)的状态机测试:连续失败开闸、冷却后半开探测、探测成功
// 闭环、探测失败重开、半开单飞。时间由测试注入,不 sleep。

var t0 = time.Date(2026, 9, 2, 12, 0, 0, 0, time.UTC)

// failing records n consecutive failures on the channel.
func failing(b *Breaker, id int64, n int) {
	for i := 0; i < n; i++ {
		b.RecordFailure(id, t0.Add(time.Duration(i)*time.Second))
	}
}

func TestBreakerOpensAfterConsecutiveFailures(t *testing.T) {
	b := NewBreaker()
	b.Threshold = 3
	const id = 7

	failing(b, id, 2)
	if !b.TryAcquire(id, t0) {
		t.Fatalf("channel closed the circuit early: two failures must not open it")
	}
	failing(b, id, 1) // 第 3 次连续失败,达到阈值
	if b.TryAcquire(id, t0) {
		t.Fatalf("circuit stayed closed after %d consecutive failures", b.Threshold)
	}
	// 冷却未过,持续拒绝;其他渠道不受牵连。
	if b.TryAcquire(id, t0.Add(b.Cooldown-time.Second)) {
		t.Fatalf("channel admitted a request before the cooldown elapsed")
	}
	if !b.TryAcquire(8, t0) {
		t.Fatalf("unrelated channel caught the circuit")
	}
}

func TestBreakerSuccessResetsFailureStreak(t *testing.T) {
	b := NewBreaker()
	b.Threshold = 3
	const id = 7

	failing(b, id, 2)
	b.RecordSuccess(id)
	failing(b, id, 2)
	if !b.TryAcquire(id, t0) {
		t.Fatalf("success must reset the streak: two fresh failures must not open")
	}
	failing(b, id, 1)
	if b.TryAcquire(id, t0) {
		t.Fatalf("third consecutive failure must open the circuit")
	}
}

func TestBreakerHalfOpenProbeClosesOnSuccess(t *testing.T) {
	b := NewBreaker()
	b.Threshold = 2
	b.Cooldown = 30 * time.Second
	const id = 7

	failing(b, id, 2)
	if !b.TryAcquire(id, t0.Add(31*time.Second)) {
		t.Fatalf("cooldown elapsed: the next request must pass as the half-open probe")
	}
	b.RecordSuccess(id)
	// 探测成功即闭环:后续请求恢复正常放行。
	if !b.TryAcquire(id, t0.Add(32*time.Second)) {
		t.Fatalf("circuit must close after a successful probe")
	}
}

func TestBreakerHalfOpenProbeFailureReopens(t *testing.T) {
	b := NewBreaker()
	b.Threshold = 2
	b.Cooldown = 30 * time.Second
	const id = 7

	failing(b, id, 2)
	if !b.TryAcquire(id, t0.Add(31*time.Second)) {
		t.Fatalf("probe must be admitted after the cooldown")
	}
	b.RecordFailure(id, t0.Add(31*time.Second))
	// 探测失败:重开并重置冷却,新冷却窗口内继续拒绝。
	if b.TryAcquire(id, t0.Add(45*time.Second)) {
		t.Fatalf("failed probe must reopen the circuit with a fresh cooldown")
	}
	if !b.TryAcquire(id, t0.Add(61*time.Second)) {
		t.Fatalf("after the fresh cooldown a new probe must be admitted")
	}
}

func TestBreakerHalfOpenAdmitsOneProbe(t *testing.T) {
	b := NewBreaker()
	b.Threshold = 2
	b.Cooldown = 30 * time.Second
	const id = 7

	failing(b, id, 2)
	if !b.TryAcquire(id, t0.Add(31*time.Second)) {
		t.Fatalf("first probe must be admitted")
	}
	// 探测在途:其余请求一律绕行,不得涌向恢复中的渠道。
	if b.TryAcquire(id, t0.Add(32*time.Second)) {
		t.Fatalf("a second request must not enter while the probe is in flight")
	}
	b.RecordSuccess(id)
	if !b.TryAcquire(id, t0.Add(34*time.Second)) {
		t.Fatalf("circuit closed: requests must flow again")
	}
}

func TestBreakerReleaseFreesTheProbeSlot(t *testing.T) {
	b := NewBreaker()
	b.Threshold = 2
	b.Cooldown = 30 * time.Second
	const id = 7

	failing(b, id, 2)
	if !b.TryAcquire(id, t0.Add(31*time.Second)) {
		t.Fatalf("probe must be admitted")
	}
	// 渠道最终没被真正调用(调度换道):释放探测位,不记成败。
	b.Release(id)
	if !b.TryAcquire(id, t0.Add(31*time.Second)) {
		t.Fatalf("released probe slot must be acquirable again")
	}
}
