package auth

import (
	"sync"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/gachal/InfiniteChance/internal/apierr"
)

// rateLimitBuckets caps how many per-IP buckets the limiter tracks before it
// sweeps expired ones — the map itself must not become the DoS memory.
const rateLimitBuckets = 4096

// RateLimiter is the in-process per-IP fixed-window limiter for the
// credential endpoints: every login/init attempt costs ~60ms of bcrypt, so
// an unbounded surface is both a cheap DoS and an offline-brute-force
// invitation. Single-process memory fits the single-gateway and desktop
// shapes; a sharded deployment would swap in a shared store.
type RateLimiter struct {
	Max    int           // 每 IP 每窗口允许的尝试数
	Window time.Duration // 窗口长度

	mu      sync.Mutex
	buckets map[string]*rateBucket
}

type rateBucket struct {
	count int
	reset time.Time
}

// Limit is the gin middleware: over-limit requests answer 429 with a
// Retry-After and never reach the bcrypt path.
func (l *RateLimiter) Limit() gin.HandlerFunc {
	return func(c *gin.Context) {
		ip := c.ClientIP()
		now := time.Now()

		l.mu.Lock()
		if l.buckets == nil {
			l.buckets = map[string]*rateBucket{}
		}
		if len(l.buckets) > rateLimitBuckets {
			for k, b := range l.buckets {
				if now.After(b.reset) {
					delete(l.buckets, k)
				}
			}
		}
		b, ok := l.buckets[ip]
		if !ok || now.After(b.reset) {
			b = &rateBucket{reset: now.Add(l.Window)}
			l.buckets[ip] = b
		}
		b.count++
		over, retry := b.count > l.Max, b.reset
		l.mu.Unlock()

		if over {
			apierr.TooManyRequests(c, "too_many_attempts",
				"尝试过于频繁,请稍后再试", int(time.Until(retry).Seconds())+1)
			c.Abort()
			return
		}
		c.Next()
	}
}
