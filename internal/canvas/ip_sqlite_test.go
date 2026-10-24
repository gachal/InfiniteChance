package canvas_test

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/gachal/InfiniteChance/internal/canvas"
	"github.com/gachal/InfiniteChance/internal/sqlitedb"
)

// newSQLiteIPWorld opens a throwaway desktop-shaped pair of stores over one
// database: canvases (whose EnsureSchema also creates the ips table so the
// List join is always safe) and the IP store (whose EnsureSchema carries the
// self-heal backfill). All 50 号票 migration scenarios run on this world.
func newSQLiteIPWorld(t *testing.T) (*canvas.SQLiteIPStore, *canvas.SQLiteStore, *sql.DB) {
	t.Helper()
	db, err := sqlitedb.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	canvases := canvas.NewSQLiteStore(db)
	if err := canvases.EnsureSchema(context.Background()); err != nil {
		t.Fatalf("canvas EnsureSchema: %v", err)
	}
	// 删除拦截的素材计数查询 assets 表(生产由 asset store 的 EnsureSchema
	// 先行建出);测试 world 落最小同形表保证同序依赖。
	if _, err := db.ExecContext(context.Background(), `CREATE TABLE IF NOT EXISTS assets (
		id INTEGER NOT NULL PRIMARY KEY AUTOINCREMENT,
		kind TEXT NOT NULL,
		canvas_id INTEGER NOT NULL,
		ip_id INTEGER NOT NULL DEFAULT 0,
		url TEXT NOT NULL
	)`); err != nil {
		t.Fatalf("ensure assets table: %v", err)
	}
	ips := canvas.NewSQLiteIPStore(db)
	return ips, canvases, db
}

// mustCreateSQLiteCanvas seeds one canvas row (50 号票后的 Create 形状:
// ip_id + type 随行)。
func mustCreateSQLiteCanvas(t *testing.T, store *canvas.SQLiteStore, ipID int64, name, typ string) canvas.Canvas {
	t.Helper()
	c, err := store.Create(context.Background(), ipID, name, []byte(`{"nodes":[],"edges":[]}`), typ)
	if err != nil {
		t.Fatalf("create canvas %q: %v", name, err)
	}
	return c
}

func ptr[T any](v T) *T { return &v }

// ---- 自愈迁移四场景(50 号票验证点)----

// 场景一:存量收编 —— 库里有 ip_id=0 的画布行、ips 表为空时,启动迁移
// 建「默认 IP」并把全部存量画布收编到它名下。
func TestSQLiteSelfHealAdoptsLegacyCanvases(t *testing.T) {
	ips, canvases, _ := newSQLiteIPWorld(t)
	ctx := context.Background()

	// 模拟存量:迁移前的画布行 ip_id=0、type='animation'(列默认值)。
	legacy := mustCreateSQLiteCanvas(t, canvases, 0, "存量画布", "animation")

	if err := ips.EnsureSchema(ctx); err != nil {
		t.Fatalf("self-heal EnsureSchema: %v", err)
	}
	list, err := ips.ListIPs(ctx)
	if err != nil {
		t.Fatalf("ListIPs: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("ips count = %d, want exactly the default IP", len(list))
	}
	if list[0].Name != "默认 IP" {
		t.Fatalf("default ip name = %q, want 默认 IP", list[0].Name)
	}
	if list[0].CanvasCount != 1 {
		t.Fatalf("default ip canvas_count = %d, want 1", list[0].CanvasCount)
	}

	got, err := canvases.Get(ctx, legacy.ID)
	if err != nil {
		t.Fatalf("get legacy canvas: %v", err)
	}
	if got.IPID != list[0].ID {
		t.Fatalf("legacy canvas ip_id = %d, want adopted into %d", got.IPID, list[0].ID)
	}
	if got.Type != "animation" {
		t.Fatalf("legacy canvas type = %q, want animation (存量收编口径)", got.Type)
	}
}

// 场景二:新库不预建 —— 没有画布行时,迁移不创建任何 IP。
func TestSQLiteSelfHealSkipsFreshDatabase(t *testing.T) {
	ips, _, _ := newSQLiteIPWorld(t)

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

// 场景三:幂等重跑 —— 迁移重复执行不报错、不重复建。
func TestSQLiteSelfHealIsIdempotent(t *testing.T) {
	ips, canvases, _ := newSQLiteIPWorld(t)
	ctx := context.Background()

	mustCreateSQLiteCanvas(t, canvases, 0, "画布", "animation")
	if err := ips.EnsureSchema(ctx); err != nil {
		t.Fatalf("first EnsureSchema: %v", err)
	}
	if err := ips.EnsureSchema(ctx); err != nil {
		t.Fatalf("second EnsureSchema: %v", err)
	}
	list, err := ips.ListIPs(ctx)
	if err != nil {
		t.Fatalf("ListIPs: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("ips count = %d after re-run, want 1", len(list))
	}
}

// 场景四:默认 IP 改名后不重复建 —— 收编条件只看「ips 表为空」,
// 不看名字,改过名的默认 IP 不会被第二次迁移再建一个。
func TestSQLiteSelfHealSkipsRenamedDefault(t *testing.T) {
	ips, canvases, _ := newSQLiteIPWorld(t)
	ctx := context.Background()

	mustCreateSQLiteCanvas(t, canvases, 0, "画布", "animation")
	if err := ips.EnsureSchema(ctx); err != nil {
		t.Fatalf("first EnsureSchema: %v", err)
	}
	list, err := ips.ListIPs(ctx)
	if err != nil || len(list) != 1 {
		t.Fatalf("ListIPs after first run = %v, %v", list, err)
	}
	renamed, err := ips.UpdateIP(ctx, list[0].ID, &canvas.IPUpdate{Name: ptr("我的 IP")})
	if err != nil {
		t.Fatalf("rename default ip: %v", err)
	}
	if err := ips.EnsureSchema(ctx); err != nil {
		t.Fatalf("second EnsureSchema after rename: %v", err)
	}
	list, err = ips.ListIPs(ctx)
	if err != nil {
		t.Fatalf("ListIPs: %v", err)
	}
	if len(list) != 1 || list[0].Name != "我的 IP" {
		t.Fatalf("ips after renamed re-run = %+v, want the renamed IP kept alone", list)
	}
	_ = renamed
}

// ---- CRUD 与删除拦截 ----

func TestSQLiteIPCRUDRoundTrip(t *testing.T) {
	ips, _, _ := newSQLiteIPWorld(t)
	ctx := context.Background()

	created, err := ips.CreateIP(ctx, "星穹之愿", "一群少女把愿望写上天空的企划", 0)
	if err != nil {
		t.Fatalf("CreateIP: %v", err)
	}
	if created.ID == 0 || created.Name != "星穹之愿" || created.CoverAssetID != 0 {
		t.Fatalf("created = %+v", created)
	}

	got, err := ips.GetIP(ctx, created.ID)
	if err != nil || got.Name != "星穹之愿" || got.Description != "一群少女把愿望写上天空的企划" {
		t.Fatalf("GetIP = %+v, %v", got, err)
	}

	name := "星穹之愿(改)"
	description := "改过的 logline"
	cover := int64(42)
	updated, err := ips.UpdateIP(ctx, created.ID, &canvas.IPUpdate{Name: &name, Description: &description, CoverAssetID: &cover})
	if err != nil {
		t.Fatalf("UpdateIP: %v", err)
	}
	if updated.Name != name || updated.Description != description || updated.CoverAssetID != 42 {
		t.Fatalf("updated = %+v", updated)
	}

	// 部分更新:只动一个字段,其余保持。
	name2 := "只有名字变"
	partial, err := ips.UpdateIP(ctx, created.ID, &canvas.IPUpdate{Name: &name2})
	if err != nil {
		t.Fatalf("partial UpdateIP: %v", err)
	}
	if partial.Name != name2 || partial.CoverAssetID != 42 || partial.Description != description {
		t.Fatalf("partial update lost other fields: %+v", partial)
	}

	// cover_asset_id=0 = 清除封面。
	zero := int64(0)
	cleared, err := ips.UpdateIP(ctx, created.ID, &canvas.IPUpdate{CoverAssetID: &zero})
	if err != nil {
		t.Fatalf("clear cover: %v", err)
	}
	if cleared.CoverAssetID != 0 {
		t.Fatalf("cleared cover = %d, want 0", cleared.CoverAssetID)
	}

	if _, err := ips.GetIP(ctx, 987654); err != canvas.ErrIPNotFound {
		t.Fatalf("missing GetIP = %v, want ErrIPNotFound", err)
	}
	if err := ips.DeleteIP(ctx, created.ID); err != nil {
		t.Fatalf("DeleteIP empty: %v", err)
	}
	if _, err := ips.GetIP(ctx, created.ID); err != canvas.ErrIPNotFound {
		t.Fatalf("GetIP after delete = %v, want ErrIPNotFound", err)
	}
}

// 删除拦截两条件:名下有画布拒、有直接归属素材拒、都没有才删。
// 素材面在该库的 assets 表上直接落行验证(同库真实语义)。
func TestSQLiteIPDeleteGuards(t *testing.T) {
	ips, canvases, db := newSQLiteIPWorld(t)
	ctx := context.Background()

	ip, err := ips.CreateIP(ctx, "有家底", "", 0)
	if err != nil {
		t.Fatalf("CreateIP: %v", err)
	}
	if _, err := canvases.Create(ctx, ip.ID, "名下画布", []byte(`{"nodes":[],"edges":[]}`), "general"); err != nil {
		t.Fatalf("create canvas under ip: %v", err)
	}
	if err := ips.DeleteIP(ctx, ip.ID); err != canvas.ErrIPNotEmpty {
		t.Fatalf("delete with canvas = %v, want ErrIPNotEmpty", err)
	}

	rows, err := canvases.List(ctx, 0)
	if err != nil || len(rows) != 1 {
		t.Fatalf("List canvases = %v, %v", rows, err)
	}
	if err := canvases.Delete(ctx, rows[0].ID); err != nil {
		t.Fatalf("delete canvas: %v", err)
	}

	// 直接归属素材:assets 表落一行 ip_id 指向该 IP(world 已建最小同形表;
	// 真实 schema 由 asset store 建,完整链路由 asset 测试覆盖)。
	if _, err := db.ExecContext(ctx,
		`INSERT INTO assets (kind, canvas_id, ip_id, url) VALUES ('image', 0, ?, 'https://x.example/a.png')`, ip.ID); err != nil {
		t.Fatalf("seed asset row: %v", err)
	}
	if err := ips.DeleteIP(ctx, ip.ID); err != canvas.ErrIPNotEmpty {
		t.Fatalf("delete with asset = %v, want ErrIPNotEmpty", err)
	}
	if _, err := db.ExecContext(ctx, `DELETE FROM assets WHERE ip_id = ?`, ip.ID); err != nil {
		t.Fatalf("clear asset row: %v", err)
	}
	if err := ips.DeleteIP(ctx, ip.ID); err != nil {
		t.Fatalf("delete empty ip: %v", err)
	}
}

// 排序:排序键 = ips.updated_at 与名下画布 max(updated_at) 取大者,新到旧;
// CanvasCount 随行。SQLite 的 TEXT 时间字典序即时间序。
func TestSQLiteIPListOrdersByLatestActivity(t *testing.T) {
	ips, canvases, _ := newSQLiteIPWorld(t)
	ctx := context.Background()

	old, err := ips.CreateIP(ctx, "老 IP", "", 0)
	if err != nil {
		t.Fatalf("CreateIP old: %v", err)
	}
	newIP, err := ips.CreateIP(ctx, "新 IP", "", 0)
	if err != nil {
		t.Fatalf("CreateIP new: %v", err)
	}
	// 老 IP 名下动一笔画布:它的活跃时间被画布顶到最新。
	mustCreateSQLiteCanvas(t, canvases, old.ID, "老 IP 的新画布", "general")

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

// 移动语义(50 号票):PATCH ip_id 只改画布归属,该画布名下历史素材行的
// ip_id 不回填 —— 旧产物留在原 IP 名下(过滤仍可见),此后新产物由 worker
// 传导新 IP(历史行不清洗原则)。
func TestSQLiteMoveDoesNotRepointHistoricalAssets(t *testing.T) {
	ips, canvases, db := newSQLiteIPWorld(t)
	ctx := context.Background()

	ipA, err := ips.CreateIP(ctx, "原 IP", "", 0)
	if err != nil {
		t.Fatalf("CreateIP A: %v", err)
	}
	ipB, err := ips.CreateIP(ctx, "新 IP", "", 0)
	if err != nil {
		t.Fatalf("CreateIP B: %v", err)
	}
	c := mustCreateSQLiteCanvas(t, canvases, ipA.ID, "要搬的画布", "general")
	// 历史素材行:归属原 IP(worker 传导的历史形态)。
	if _, err := db.ExecContext(ctx,
		`INSERT INTO assets (kind, canvas_id, ip_id, url) VALUES ('image', ?, ?, 'https://x.example/h.png')`, c.ID, ipA.ID); err != nil {
		t.Fatalf("seed historical asset: %v", err)
	}

	moved, err := canvases.Move(ctx, c.ID, ipB.ID)
	if err != nil {
		t.Fatalf("Move: %v", err)
	}
	if moved.IPID != ipB.ID {
		t.Fatalf("moved canvas ip = %d, want %d", moved.IPID, ipB.ID)
	}
	var assetIP int64
	if err := db.QueryRowContext(ctx, `SELECT ip_id FROM assets WHERE canvas_id = ?`, c.ID).Scan(&assetIP); err != nil {
		t.Fatalf("read historical asset: %v", err)
	}
	if assetIP != ipA.ID {
		t.Fatalf("historical asset ip_id = %d, want %d (历史行不回填)", assetIP, ipA.ID)
	}
}
