// Package health implements the /healthz contract shared by both services:
// ping every dependency concurrently and report per-dependency connectivity.
package health

import (
	"context"
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
)

// Pinger is anything whose connectivity can be probed: the MySQL pool, the
// Redis client, or fakes in tests.
type Pinger interface {
	Ping(ctx context.Context) error
}

const pingTimeout = 2 * time.Second

// Wire values of the /healthz contract. The TypeScript mirror lives in
// packages/api (DepStatus / HealthReport); keep both sides in sync.
const (
	StatusUp       = "up"
	StatusDown     = "down"
	StatusOK       = "ok"
	StatusDegraded = "degraded"
)

// DepCheck is the connectivity result for a single dependency.
type DepCheck struct {
	Status string `json:"status"` // StatusUp | StatusDown
	Error  string `json:"error,omitempty"`
}

// Report is the JSON body of GET /healthz.
type Report struct {
	Service string              `json:"service"`
	Status  string              `json:"status"` // StatusOK | StatusDegraded
	Checks  map[string]DepCheck `json:"checks"`
}

// Handler returns a gin handler that pings all dependencies concurrently and
// answers 200 only when every one of them is reachable, 503 otherwise. Each
// dependency gets its own timeout, so one slow dependency cannot time out
// another's probe.
func Handler(service string, deps map[string]Pinger) gin.HandlerFunc {
	return func(c *gin.Context) {
		var mu sync.Mutex
		checks := make(map[string]DepCheck, len(deps))
		var wg sync.WaitGroup
		for name, pinger := range deps {
			wg.Add(1)
			go func(name string, pinger Pinger) {
				defer wg.Done()
				ctx, cancel := context.WithTimeout(c.Request.Context(), pingTimeout)
				defer cancel()

				check := DepCheck{Status: StatusUp}
				if err := pinger.Ping(ctx); err != nil {
					check = DepCheck{Status: StatusDown, Error: err.Error()}
				}
				mu.Lock()
				checks[name] = check
				mu.Unlock()
			}(name, pinger)
		}
		wg.Wait()

		status := StatusOK
		code := http.StatusOK
		for _, check := range checks {
			if check.Status != StatusUp {
				status = StatusDegraded
				code = http.StatusServiceUnavailable
			}
		}
		c.JSON(code, Report{Service: service, Status: status, Checks: checks})
	}
}
