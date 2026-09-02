package channel

import (
	"sync"
	"time"
)

// 熔断器(06 号票定案):每渠道独立的三态电路,内存态、不落库 ——
//   closed:正常放行;连续失败达阈值即 open。
//   open:一概不放行;冷却期(Cooldown)过后转半开。
//   half-open:只放一个探测请求(单飞);成功闭环、失败重开并重置冷却。
// 调度方(relay)每轮尝试前 TryAcquire 领取放行资格,事后三选一:
// RecordSuccess / RecordFailure / Release(渠道最终没被调用,不记成败)。

type circuitState int

const (
	circuitClosed circuitState = iota
	circuitOpen
	circuitHalfOpen
)

type circuit struct {
	state    circuitState
	failures int       // 连续失败计数(closed 态累计)
	openedAt time.Time // open 起点,冷却从此起算
	probing  bool      // half-open 探测在途
}

// Breaker tracks per-channel circuit state in memory: Threshold consecutive
// failures open the circuit, after Cooldown a single probe request passes
// (half-open) — success closes it, failure re-opens it. The zero value is
// not usable; build one with NewBreaker. All methods are safe for
// concurrent use; now is injected so tests can drive time.
type Breaker struct {
	mu        sync.Mutex
	states    map[int64]*circuit
	Threshold int           // 连续失败阈值(≤1 视为 1)
	Cooldown  time.Duration // open 态冷却期,过后允许半开探测
}

// NewBreaker builds a breaker with the shipping defaults: 3 consecutive
// failures open a channel, 30 seconds later the half-open probe goes out.
func NewBreaker() *Breaker {
	return &Breaker{
		states:    map[int64]*circuit{},
		Threshold: 3,
		Cooldown:  30 * time.Second,
	}
}

func (b *Breaker) threshold() int {
	if b.Threshold < 1 {
		return 1
	}
	return b.Threshold
}

// TryAcquire reports whether a request may use the channel now. A closed
// circuit admits freely; an open one admits nothing until the cooldown has
// elapsed, when it flips to half-open and admits exactly one probe (the
// caller must afterwards record an outcome or Release the slot).
func (b *Breaker) TryAcquire(id int64, now time.Time) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	st := b.states[id]
	if st == nil || st.state == circuitClosed {
		return true
	}
	if st.state == circuitOpen {
		if now.Sub(st.openedAt) < b.Cooldown {
			return false
		}
		st.state = circuitHalfOpen
	}
	if st.probing {
		return false
	}
	st.probing = true
	return true
}

// RecordSuccess closes the circuit and clears the failure streak.
func (b *Breaker) RecordSuccess(id int64, now time.Time) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.states[id] = &circuit{state: circuitClosed}
}

// RecordFailure counts one failed attempt. Past the threshold (or straight
// from a failed half-open probe) the circuit opens for a fresh cooldown.
func (b *Breaker) RecordFailure(id int64, now time.Time) {
	b.mu.Lock()
	defer b.mu.Unlock()
	st := b.states[id]
	if st == nil {
		st = &circuit{}
		b.states[id] = st
	}
	if st.state == circuitHalfOpen {
		// 探测失败:立即重开,冷却重新起算。
		st.state = circuitOpen
		st.openedAt = now
		st.probing = false
		st.failures = b.threshold()
		return
	}
	st.failures++
	if st.failures >= b.threshold() {
		st.state = circuitOpen
		st.openedAt = now
		st.probing = false
	}
}

// Release gives back a probe slot acquired via TryAcquire when the channel
// ended up not being called — neither success nor failure is recorded.
func (b *Breaker) Release(id int64) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if st := b.states[id]; st != nil && st.state == circuitHalfOpen {
		st.probing = false
	}
}
