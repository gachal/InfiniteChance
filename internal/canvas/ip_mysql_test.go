package canvas_test

import (
	"context"
	"database/sql"
	"os"
	"testing"
	"time"

	mysqldriver "github.com/go-sql-driver/mysql"

	"github.com/gachal/InfiniteChance/internal/canvas"
)

// openTestIPStore 连 compose MySQL(宿主机 3307)上的专用一次性库,与
// store_mysql_test.go 同款 harness:库不可达自动 skip,保持 go test 全绿。
// 这里同时准备 canvases(带 50 号票加列)与 assets 最小同形表 —— IP 侧行为
// 的同序依赖(装配顺序 canvases → assets → ips)在测试里同样成立。
func openTestIPStore(t *testing.T) (*canvas.MySQLIPStore, *canvas.MySQLStore, *sql.DB) {
	t.Helper()

	dsn := os.Getenv("MYSQL_TEST_DSN")
	if dsn == "" {
		dsn = "root:infinitechance@tcp(localhost:3307)/infinitechance_test?parseTime=true"
	}
	cfg, err := mysqldriver.ParseDSN(dsn)
	if err != nil {
		t.Fatalf("parse MYSQL_TEST_DSN: %v", err)
	}
	dbName := cfg.DBName + "_canvas"
	cfg.DBName = ""

	server, err := sql.Open("mysql", cfg.FormatDSN())
	if err != nil {
		t.Fatalf("open mysql server: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := server.PingContext(ctx); err != nil {
		t.Skipf("mysql unreachable, skipping store tests: %v", err)
	}
	if _, err := server.ExecContext(ctx, "CREATE DATABASE IF NOT EXISTS `"+dbName+"`"); err != nil {
		t.Fatalf("create test database: %v", err)
	}
	for _, table := range []string{"canvases", "ips", "assets"} {
		if _, err := server.ExecContext(ctx, "DROP TABLE IF EXISTS `"+dbName+"`.`"+table+"`"); err != nil {
			t.Fatalf("drop stale table %s: %v", table, err)
		}
	}

	cfg.DBName = dbName
	db, err := sql.Open("mysql", cfg.FormatDSN())
	if err != nil {
		t.Fatalf("open test database: %v", err)
	}
	canvases := canvas.NewMySQLStore(db)
	if err := canvases.EnsureSchema(ctx); err != nil {
		t.Fatalf("canvas EnsureSchema: %v", err)
	}
	// assets 最小同形表:删除拦截的素材计数用(完整 schema 由 asset store
	// 的测试与启动 EnsureSchema 负责)。
	if _, err := db.ExecContext(ctx, `CREATE TABLE assets (
		id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,
		kind VARCHAR(16) NOT NULL,
		canvas_id BIGINT UNSIGNED NOT NULL,
		ip_id BIGINT UNSIGNED NOT NULL DEFAULT 0,
		url MEDIUMTEXT NOT NULL
	) ENGINE = InnoDB DEFAULT CHARSET = utf8mb4`); err != nil {
		t.Fatalf("create minimal assets table: %v", err)
	}
	ips := canvas.NewMySQLIPStore(db)
	t.Cleanup(func() {
		db.Close()
		server.Close()
	})
	return ips, canvases, db
}

// ---- 自愈迁移四场景(MySQL 同构)----

func TestMySQLSelfHealAdoptsLegacyCanvases(t *testing.T) {
	ips, canvases, _ := openTestIPStore(t)
	ctx := context.Background()

	legacy, err := canvases.Create(ctx, 0, "存量画布", []byte(`{"nodes":[],"edges":[]}`), "animation")
	if err != nil {
		t.Fatalf("seed legacy canvas: %v", err)
	}

	if err := ips.EnsureSchema(ctx); err != nil {
		t.Fatalf("self-heal EnsureSchema: %v", err)
	}
	list, err := ips.ListIPs(ctx)
	if err != nil {
		t.Fatalf("ListIPs: %v", err)
	}
	if len(list) != 1 || list[0].Name != "默认 IP" {
		t.Fatalf("ips = %+v, want exactly the default IP", list)
	}
	if list[0].CanvasCount != 1 {
		t.Fatalf("canvas_count = %d, want 1", list[0].CanvasCount)
	}
	got, err := canvases.Get(ctx, legacy.ID)
	if err != nil {
		t.Fatalf("get legacy canvas: %v", err)
	}
	if got.IPID != list[0].ID || got.Type != "animation" {
		t.Fatalf("legacy canvas = ip %d type %q, want adopted with animation", got.IPID, got.Type)
	}
}

func TestMySQLSelfHealSkipsFreshDatabase(t *testing.T) {
	ips, _, _ := openTestIPStore(t)

	if err := ips.EnsureSchema(context.Background()); err != nil {
		t.Fatalf("self-heal EnsureSchema: %v", err)
	}
	list, err := ips.ListIPs(context.Background())
	if err != nil {
		t.Fatalf("ListIPs: %v", err)
	}
	if len(list) != 0 {
		t.Fatalf("fresh database must not pre-create an IP, got %d", len(list))
	}
}

func TestMySQLSelfHealIsIdempotent(t *testing.T) {
	ips, canvases, _ := openTestIPStore(t)
	ctx := context.Background()

	if _, err := canvases.Create(ctx, 0, "画布", []byte(`{"nodes":[],"edges":[]}`), "animation"); err != nil {
		t.Fatalf("seed: %v", err)
	}
	for i := 0; i < 2; i++ {
		if err := ips.EnsureSchema(ctx); err != nil {
			t.Fatalf("EnsureSchema #%d: %v", i+1, err)
		}
	}
	list, err := ips.ListIPs(ctx)
	if err != nil {
		t.Fatalf("ListIPs: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("ips count = %d after re-run, want 1", len(list))
	}
}

func TestMySQLSelfHealSkipsRenamedDefault(t *testing.T) {
	ips, canvases, _ := openTestIPStore(t)
	ctx := context.Background()

	if _, err := canvases.Create(ctx, 0, "画布", []byte(`{"nodes":[],"edges":[]}`), "animation"); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := ips.EnsureSchema(ctx); err != nil {
		t.Fatalf("first EnsureSchema: %v", err)
	}
	list, err := ips.ListIPs(ctx)
	if err != nil || len(list) != 1 {
		t.Fatalf("ListIPs after first run = %v, %v", list, err)
	}
	renamed := "我的 IP"
	if _, err := ips.UpdateIP(ctx, list[0].ID, &canvas.IPUpdate{Name: &renamed}); err != nil {
		t.Fatalf("rename default ip: %v", err)
	}
	if err := ips.EnsureSchema(ctx); err != nil {
		t.Fatalf("second EnsureSchema after rename: %v", err)
	}
	list, err = ips.ListIPs(ctx)
	if err != nil {
		t.Fatalf("ListIPs: %v", err)
	}
	if len(list) != 1 || list[0].Name != renamed {
		t.Fatalf("ips after renamed re-run = %+v, want the renamed IP kept alone", list)
	}
}

// ---- CRUD 与删除拦截(MySQL 同构)----

func TestMySQLIPCRUDAndGuards(t *testing.T) {
	ips, canvases, db := openTestIPStore(t)
	ctx := context.Background()

	created, err := ips.CreateIP(ctx, "星穹之愿", "logline", 0)
	if err != nil {
		t.Fatalf("CreateIP: %v", err)
	}
	got, err := ips.GetIP(ctx, created.ID)
	if err != nil || got.Name != "星穹之愿" || got.Description != "logline" {
		t.Fatalf("GetIP = %+v, %v", got, err)
	}

	// 部分更新:只动名字,其余保持;cover_asset_id=0 = 清封面(先设非零)。
	cover := int64(7)
	if _, err := ips.UpdateIP(ctx, created.ID, &canvas.IPUpdate{CoverAssetID: &cover}); err != nil {
		t.Fatalf("set cover: %v", err)
	}
	name := "星穹之愿(改)"
	updated, err := ips.UpdateIP(ctx, created.ID, &canvas.IPUpdate{Name: &name})
	if err != nil {
		t.Fatalf("rename ip: %v", err)
	}
	if updated.Name != name || updated.CoverAssetID != 7 || updated.Description != "logline" {
		t.Fatalf("partial update lost fields: %+v", updated)
	}
	zero := int64(0)
	if cleared, err := ips.UpdateIP(ctx, created.ID, &canvas.IPUpdate{CoverAssetID: &zero}); err != nil || cleared.CoverAssetID != 0 {
		t.Fatalf("clear cover = %+v, %v", cleared, err)
	}

	// 删除拦截:有画布拒、有直接素材拒、空则删。
	if _, err := canvases.Create(ctx, created.ID, "名下画布", []byte(`{"nodes":[],"edges":[]}`), "general"); err != nil {
		t.Fatalf("create canvas under ip: %v", err)
	}
	if err := ips.DeleteIP(ctx, created.ID); err != canvas.ErrIPNotEmpty {
		t.Fatalf("delete with canvas = %v, want ErrIPNotEmpty", err)
	}
	rows, err := canvases.List(ctx, created.ID)
	if err != nil || len(rows) != 1 {
		t.Fatalf("List by ip = %v, %v", rows, err)
	}
	if err := canvases.Delete(ctx, rows[0].ID); err != nil {
		t.Fatalf("delete canvas: %v", err)
	}
	if _, err := db.ExecContext(ctx,
		`INSERT INTO assets (kind, canvas_id, ip_id, url) VALUES ('image', 0, ?, 'https://x.example/a.png')`, created.ID); err != nil {
		t.Fatalf("seed asset row: %v", err)
	}
	if err := ips.DeleteIP(ctx, created.ID); err != canvas.ErrIPNotEmpty {
		t.Fatalf("delete with asset = %v, want ErrIPNotEmpty", err)
	}
	if _, err := db.ExecContext(ctx, `DELETE FROM assets WHERE ip_id = ?`, created.ID); err != nil {
		t.Fatalf("clear asset row: %v", err)
	}
	if err := ips.DeleteIP(ctx, created.ID); err != nil {
		t.Fatalf("delete empty ip: %v", err)
	}
	if _, err := ips.GetIP(ctx, created.ID); err != canvas.ErrIPNotFound {
		t.Fatalf("get after delete = %v, want ErrIPNotFound", err)
	}
}

// 排序:排序键 = ips.updated_at 与名下画布 max(updated_at) 取大者(GREATEST)。
func TestMySQLIPListOrdersByLatestActivity(t *testing.T) {
	ips, canvases, _ := openTestIPStore(t)
	ctx := context.Background()

	old, err := ips.CreateIP(ctx, "老 IP", "", 0)
	if err != nil {
		t.Fatalf("CreateIP old: %v", err)
	}
	newIP, err := ips.CreateIP(ctx, "新 IP", "", 0)
	if err != nil {
		t.Fatalf("CreateIP new: %v", err)
	}
	if _, err := canvases.Create(ctx, old.ID, "老 IP 的新画布", []byte(`{"nodes":[],"edges":[]}`), "general"); err != nil {
		t.Fatalf("create canvas under old ip: %v", err)
	}

	list, err := ips.ListIPs(ctx)
	if err != nil {
		t.Fatalf("ListIPs: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("len = %d, want 2", len(list))
	}
	if list[0].ID != old.ID || list[1].ID != newIP.ID {
		t.Fatalf("order = [%d %d], want the ip with freshest canvas activity first", list[0].ID, list[1].ID)
	}
	if list[0].CanvasCount != 1 || list[1].CanvasCount != 0 {
		t.Fatalf("canvas counts = %d/%d, want 1/0", list[0].CanvasCount, list[1].CanvasCount)
	}
}
