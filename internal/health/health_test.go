package health_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/gachal/InfiniteChance/internal/health"
)

type fakePinger struct{ err error }

func (f fakePinger) Ping(context.Context) error { return f.err }

func serveHealth(t *testing.T, deps map[string]health.Pinger) *httptest.ResponseRecorder {
	t.Helper()
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/healthz", health.Handler("test-service", deps))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	return w
}

func decode(t *testing.T, w *httptest.ResponseRecorder) health.Report {
	t.Helper()
	var report health.Report
	if err := json.Unmarshal(w.Body.Bytes(), &report); err != nil {
		t.Fatalf("response body is not valid health JSON: %v\nbody: %s", err, w.Body.String())
	}
	return report
}

func TestHandlerAllDepsUp(t *testing.T) {
	w := serveHealth(t, map[string]health.Pinger{
		"mysql": fakePinger{},
		"redis": fakePinger{},
	})

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}
	report := decode(t, w)
	if report.Status != "ok" {
		t.Errorf("status = %q, want %q", report.Status, "ok")
	}
	if report.Service != "test-service" {
		t.Errorf("service = %q, want %q", report.Service, "test-service")
	}
	for _, dep := range []string{"mysql", "redis"} {
		check, ok := report.Checks[dep]
		if !ok {
			t.Fatalf("missing check for %q in %+v", dep, report.Checks)
		}
		if check.Status != "up" {
			t.Errorf("%s status = %q, want %q", dep, check.Status, "up")
		}
		if check.Error != "" {
			t.Errorf("%s error = %q, want empty", dep, check.Error)
		}
	}
}

func TestHandlerOneDepDown(t *testing.T) {
	w := serveHealth(t, map[string]health.Pinger{
		"mysql": fakePinger{err: errors.New("connection refused")},
		"redis": fakePinger{},
	})

	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503", w.Code)
	}
	report := decode(t, w)
	if report.Status != "degraded" {
		t.Errorf("status = %q, want %q", report.Status, "degraded")
	}
	mysql := report.Checks["mysql"]
	if mysql.Status != "down" {
		t.Errorf("mysql status = %q, want %q", mysql.Status, "down")
	}
	if mysql.Error != "connection refused" {
		t.Errorf("mysql error = %q, want the pinger error", mysql.Error)
	}
	if redis := report.Checks["redis"]; redis.Status != "up" {
		t.Errorf("redis status = %q, want %q", redis.Status, "up")
	}
}
