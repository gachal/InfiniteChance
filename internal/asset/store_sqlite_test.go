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
