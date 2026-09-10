package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gachal/InfiniteChance/internal/apikey"
	"github.com/gachal/InfiniteChance/internal/health"
	"github.com/gachal/InfiniteChance/internal/sqlitedb"
)

// newTestDesktop assembles the full desktop against a throwaway data dir on
// free ports, so the test never touches the real user data or the default
// 8080/8081 (a dev machine may run the Docker stack there).
func newTestDesktop(t *testing.T) *Desktop {
	t.Helper()
	dataDir := t.TempDir()
	t.Setenv("INFINITECHANCE_DATA_DIR", dataDir)

	gatewayPort := freePort(t)
	canvasPort := freePort(t)
	cfg := Config{
		GatewayPort:           gatewayPort,
		CanvasPort:            canvasPort,
		JWTSecret:             "test-jwt-secret",
		CanvasTaskConcurrency: 1,
	}
	raw, err := json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dataDir, "config.json"), raw, 0o600); err != nil {
		t.Fatal(err)
	}

	dt, err := NewDesktop()
	if err != nil {
		t.Fatalf("NewDesktop: %v", err)
	}
	t.Cleanup(dt.Shutdown)
	return dt
}

func freePort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	return ln.Addr().(*net.TCPAddr).Port
}

// TestNewDesktopAssembly verifies the whole in-process wiring before any
// window exists: schemas, healthz on both services, the OpenAI-facing /v1
// guard and the auto-provisioned service key.
func TestNewDesktopAssembly(t *testing.T) {
	dt := newTestDesktop(t)

	if dt.Initialized() {
		t.Fatal("fresh data dir should report uninitialized")
	}

	// 两个服务的 /healthz 都只探 sqlite,且必须可达。
	for _, svc := range []struct {
		name    string
		engine  http.Handler
		service string
	}{
		{"gateway", dt.gateway.Engine, "gateway"},
		{"canvas", dt.canvas.Engine, "canvas"},
	} {
		t.Run(svc.name, func(t *testing.T) {
			srv := httptest.NewServer(svc.engine)
			defer srv.Close()

			resp, err := http.Get(srv.URL + "/healthz")
			if err != nil {
				t.Fatal(err)
			}
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusOK {
				t.Fatalf("healthz = %d, want 200", resp.StatusCode)
			}
			var report health.Report
			if err := json.NewDecoder(resp.Body).Decode(&report); err != nil {
				t.Fatal(err)
			}
			if report.Service != svc.service {
				t.Fatalf("service = %q, want %q", report.Service, svc.service)
			}
			sqliteCheck, ok := report.Checks["sqlite"]
			if !ok || sqliteCheck.Status != health.StatusUp {
				t.Fatalf("sqlite check = %+v, want up", sqliteCheck)
			}
			if _, hasRedis := report.Checks["redis"]; hasRedis {
				t.Fatal("desktop assembly must not ping redis")
			}
		})
	}

	// 中转面无 key 一律 401 OpenAI error object。
	gw := httptest.NewServer(dt.gateway.Engine)
	defer gw.Close()
	resp, err := http.Get(gw.URL + "/v1/models")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("/v1/models without key = %d, want 401", resp.StatusCode)
	}
	var body struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body.Error.Code == "" {
		t.Fatal("expected an OpenAI error object with a code")
	}

	// 服务 key:配置里有明文、库里能按哈希解析到且处于 active。
	if dt.cfg.CanvasServiceKey == "" {
		t.Fatal("service key was not auto-provisioned into config")
	}
	keys := apikey.NewSQLiteStore(dt.db)
	row, err := keys.ByHash(context.Background(), apikey.Hash(dt.cfg.CanvasServiceKey))
	if err != nil {
		t.Fatalf("service key not resolvable by hash: %v", err)
	}
	if row.Status(time.Now()) != apikey.StatusActive {
		t.Fatalf("service key status = %q, want active", row.Status(time.Now()))
	}
	if row.QuotaMicros <= 0 {
		t.Fatalf("service key quota = %d, want a positive starting balance", row.QuotaMicros)
	}

	// 幂等:重启后同一把 key 原样保留,而不是重新铸造。
	dataDir := dt.dataDir
	serviceKey := dt.cfg.CanvasServiceKey
	dt.Shutdown()
	t.Setenv("INFINITECHANCE_DATA_DIR", dataDir)
	again, err := NewDesktop()
	if err != nil {
		t.Fatalf("second NewDesktop: %v", err)
	}
	defer again.Shutdown()
	if again.cfg.CanvasServiceKey != serviceKey {
		t.Fatal("second boot re-minted the service key; it must be reused")
	}
}

// TestStartServesOrigins boots the real listeners and walks the origin
// contracts: /api self-aliasing on both services, /canvas-api forwarding on
// the admin origin, and the SPA fallback (placeholder until the dists are
// embedded by make desktop-frontend).
func TestStartServesOrigins(t *testing.T) {
	dt := newTestDesktop(t)
	if err := dt.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	base := func(port int) string { return fmt.Sprintf("http://127.0.0.1:%d", port) }
	gateway := base(dt.cfg.GatewayPort)
	canvas := base(dt.cfg.CanvasPort)

	// /api/* 去前缀后打到引擎:画布源上的 auth 状态接口。
	resp, err := http.Get(canvas + "/api/auth/status")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("/api/auth/status = %d, want 200", resp.StatusCode)
	}

	// 管理台源的 /canvas-api/* 转发到画布服务的 /healthz。
	resp, err = http.Get(gateway + "/canvas-api/healthz")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("/canvas-api/healthz = %d, want 200", resp.StatusCode)
	}

	// SPA 回退:未构建 dist 时占位页(501),构建后是 200 index。
	resp, err = http.Get(canvas + "/some/spa/route")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNotImplemented {
		t.Fatalf("SPA fallback = %d, want 200 (built) or 501 (placeholder)", resp.StatusCode)
	}

	// 根路径的引擎路由不被 SPA 回退遮蔽:/v1 仍由引擎作答(401)。
	resp, err = http.Get(gateway + "/v1/models")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("/v1/models = %d, want 401 (engine wins over SPA fallback)", resp.StatusCode)
	}

	dt.Shutdown()

	// Shutdown 后端口应已释放,可再次监听(应用正常退出的前提)。
	ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", dt.cfg.CanvasPort))
	if err != nil {
		t.Fatalf("canvas port not released after shutdown: %v", err)
	}
	ln.Close()
}

// sqlite 时间规约:定宽 TEXT 往返无损(sqlitedb 契约的回归锚)。
func TestSQLiteTimeRoundTrip(t *testing.T) {
	db, err := sqlitedb.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`CREATE TABLE ts (v TEXT)`); err != nil {
		t.Fatal(err)
	}
	want := time.Now().UTC().Truncate(time.Microsecond)
	if _, err := db.Exec(`INSERT INTO ts (v) VALUES (?)`, sqlitedb.FormatTime(want)); err != nil {
		t.Fatal(err)
	}
	var raw string
	if err := db.QueryRow(`SELECT v FROM ts`).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	got, err := sqlitedb.ParseTime(raw)
	if err != nil {
		t.Fatalf("ParseTime(%q): %v", raw, err)
	}
	if !got.Equal(want) {
		t.Fatalf("round trip = %v, want %v", got, want)
	}
}
