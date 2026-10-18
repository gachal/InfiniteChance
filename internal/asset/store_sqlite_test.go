package asset

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/gachal/InfiniteChance/internal/canvas"
	"github.com/gachal/InfiniteChance/internal/sqlitedb"
)

// newSQLiteAssetStore opens a throwaway desktop-shaped assets store; the
// canvases table comes along because List joins it for the display name.
func newSQLiteAssetStore(t *testing.T) *SQLiteStore {
	t.Helper()
	db, err := sqlitedb.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if err := canvas.NewSQLiteStore(db).EnsureSchema(context.Background()); err != nil {
		t.Fatal(err)
	}
	s := NewSQLiteStore(db)
	if err := s.EnsureSchema(context.Background()); err != nil {
		t.Fatal(err)
	}
	return s
}

// 47 号票:last_used_at 初始 = created_at,TouchLastUsed 刷新,sort=last_used
// 把最近使用的排前面,缺省仍 id DESC。
func TestSQLiteAssetLastUsedSort(t *testing.T) {
	s := newSQLiteAssetStore(t)
	ctx := context.Background()

	first, err := s.Create(ctx, Asset{Kind: KindImage, CanvasID: 1, URL: "https://img.example/1.png"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	second, err := s.Create(ctx, Asset{Kind: KindImage, CanvasID: 1, URL: "https://img.example/2.png"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if !first.LastUsedAt.Equal(first.CreatedAt) || !second.LastUsedAt.Equal(second.CreatedAt) {
		t.Fatalf("initial last_used_at must equal created_at (first %v second %v)",
			first.LastUsedAt, second.LastUsedAt)
	}

	// 缺省:创建序(id DESC)。
	rows, err := s.List(ctx, Filter{})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(rows) != 2 || rows[0].ID != second.ID || rows[1].ID != first.ID {
		t.Fatalf("default order = %d,%d, want id DESC", rows[0].ID, rows[1].ID)
	}

	// 用过的素材(先创建的老素材)升到最前。
	if err := s.TouchLastUsed(ctx, first.ID); err != nil {
		t.Fatalf("TouchLastUsed: %v", err)
	}
	touched, err := s.Get(ctx, first.ID)
	if err != nil {
		t.Fatalf("Get after touch: %v", err)
	}
	if !touched.LastUsedAt.After(touched.CreatedAt) {
		t.Fatalf("last_used_at = %v, want refreshed past created_at %v", touched.LastUsedAt, touched.CreatedAt)
	}
	rows, err = s.List(ctx, Filter{Sort: SortLastUsed})
	if err != nil {
		t.Fatalf("List sorted: %v", err)
	}
	if len(rows) != 2 || rows[0].ID != first.ID || rows[1].ID != second.ID {
		t.Fatalf("last_used order = %d,%d, want touched first", rows[0].ID, rows[1].ID)
	}

	// 未知 id:ErrNotFound(解引用侧并发删除不算错)。
	if err := s.TouchLastUsed(ctx, 999); err != ErrNotFound {
		t.Fatalf("touch missing = %v, want ErrNotFound", err)
	}
}

// EnsureSchema 幂等(桌面端启动跑两次)。
func TestSQLiteAssetSchemaIsIdempotent(t *testing.T) {
	s := newSQLiteAssetStore(t)
	if err := s.EnsureSchema(context.Background()); err != nil {
		t.Fatalf("second EnsureSchema: %v", err)
	}
}

// 48 号票:存量桌面库(47 号票形态,缺 content_hash 列)经 PRAGMA table_info
// 检查后原地 ALTER 加列,Create 带哈希照常可用 —— 桌面 app.db 跨版本保留。
func TestSQLiteAssetSchemaMigratesContentHash(t *testing.T) {
	db, err := sqlitedb.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if err := canvas.NewSQLiteStore(db).EnsureSchema(context.Background()); err != nil {
		t.Fatal(err)
	}
	// 47 号票定稿的表形状(无 content_hash)。
	legacy := `
	CREATE TABLE IF NOT EXISTS assets (
		id           INTEGER NOT NULL PRIMARY KEY AUTOINCREMENT,
		kind         TEXT    NOT NULL,
		canvas_id    INTEGER NOT NULL,
		task_id      TEXT    NOT NULL DEFAULT '',
		model        TEXT    NOT NULL DEFAULT '',
		prompt       TEXT    NULL,
		url          TEXT    NOT NULL,
		object_key   TEXT    NOT NULL DEFAULT '',
		content_type TEXT    NOT NULL DEFAULT '',
		size_bytes   INTEGER NOT NULL DEFAULT 0,
		created_at   TEXT    NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%f000Z','now')),
		last_used_at TEXT    NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%f000Z','now'))
	)`
	if _, err := db.Exec(legacy); err != nil {
		t.Fatalf("create legacy table: %v", err)
	}

	s := NewSQLiteStore(db)
	ctx := context.Background()
	if err := s.EnsureSchema(ctx); err != nil {
		t.Fatalf("EnsureSchema over legacy table: %v", err)
	}

	// 加列已生效且幂等。
	for i := 0; i < 2; i++ {
		rows, err := db.Query(`PRAGMA table_info(assets)`)
		if err != nil {
			t.Fatal(err)
		}
		found := false
		for rows.Next() {
			var cid int
			var name, ctype string
			var notNull, pk int
			var dfltValue any
			if err := rows.Scan(&cid, &name, &ctype, &notNull, &dfltValue, &pk); err != nil {
				rows.Close()
				t.Fatal(err)
			}
			if name == "content_hash" {
				found = true
			}
		}
		rows.Close()
		if !found {
			t.Fatalf("pass %d: content_hash column missing after migration", i)
		}
	}

	created, err := s.Create(ctx, Asset{
		Kind: KindImage, CanvasID: 1, URL: "https://img.example/1.png",
		ObjectKey: "uploads/20261018/m.png", ContentHash: "hash-m",
	})
	if err != nil {
		t.Fatalf("Create with hash on migrated table: %v", err)
	}
	got, err := s.Get(ctx, created.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.ContentHash != "hash-m" {
		t.Errorf("content_hash = %q, want hash-m", got.ContentHash)
	}
}
