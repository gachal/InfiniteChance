package canvastask

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/gachal/InfiniteChance/internal/asset"
	"github.com/gachal/InfiniteChance/internal/sqlitedb"
)

// newSQLiteStore opens a throwaway desktop-shaped store.
func newSQLiteStore(t *testing.T) *SQLiteStore {
	t.Helper()
	db, err := sqlitedb.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	assets := asset.NewSQLiteStore(db)
	if err := assets.EnsureSchema(context.Background()); err != nil {
		t.Fatal(err)
	}
	s := NewSQLiteStore(db, assets)
	if err := s.EnsureSchema(context.Background()); err != nil {
		t.Fatal(err)
	}
	return s
}

func TestSQLiteCanvasTaskImageRefsRoundTrip(t *testing.T) {
	s := newSQLiteStore(t)
	ctx := context.Background()

	id, err := NewID()
	if err != nil {
		t.Fatal(err)
	}
	task, err := s.Create(ctx, Task{
		ID: id, CanvasID: 1, NodeID: "image-5-1", Kind: KindImage,
		Prompt: "把背景换成雪原", Model: "img-m",
		ImageRefs: []string{"https://img.example/ref1.png", "https://img.example/ref2.png"},
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	got, err := s.Get(ctx, task.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if len(got.ImageRefs) != 2 || got.ImageRefs[0] != "https://img.example/ref1.png" || got.ImageRefs[1] != "https://img.example/ref2.png" {
		t.Fatalf("image_refs = %v, want both references in order", got.ImageRefs)
	}

	plain, err := s.Create(ctx, Task{
		ID: mustTaskID(t), CanvasID: 1, NodeID: "image-5-2", Kind: KindImage,
		Prompt: "p", Model: "img-m",
	})
	if err != nil {
		t.Fatalf("Create plain: %v", err)
	}
	got, err = s.Get(ctx, plain.ID)
	if err != nil {
		t.Fatalf("Get plain: %v", err)
	}
	if got.ImageRefs != nil {
		t.Errorf("plain image_refs = %v, want nil", got.ImageRefs)
	}
}

// 存量桌面库由旧建表创建(没有 image_refs 列):EnsureSchema 必须原地加列
// 而不是要求重建 —— 与 MySQL 侧的迁移语义对齐(21 号票起桌面库也有存量)。
func TestSQLiteCanvasTaskSchemaMigratesOldTable(t *testing.T) {
	db, err := sqlitedb.Open(filepath.Join(t.TempDir(), "old.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	ctx := context.Background()

	// 旧形状:手工建一张没有 image_refs 的表。
	if _, err := db.ExecContext(ctx, `CREATE TABLE canvas_tasks (
		id TEXT NOT NULL PRIMARY KEY,
		canvas_id INTEGER NOT NULL,
		node_id TEXT NOT NULL,
		kind TEXT NOT NULL,
		prompt TEXT NOT NULL,
		model TEXT NOT NULL,
		size TEXT NOT NULL DEFAULT '',
		seconds INTEGER NOT NULL DEFAULT 0,
		image_ref TEXT NULL,
		status TEXT NOT NULL,
		attempts INTEGER NOT NULL DEFAULT 0,
		error TEXT NULL,
		asset_id INTEGER NULL,
		image_url TEXT NULL,
		video_url TEXT NULL,
		remote_task_id TEXT NOT NULL DEFAULT '',
		created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%f000Z','now')),
		updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%f000Z','now'))
	)`); err != nil {
		t.Fatalf("create old table: %v", err)
	}

	assets := asset.NewSQLiteStore(db)
	if err := assets.EnsureSchema(ctx); err != nil {
		t.Fatal(err)
	}
	s := NewSQLiteStore(db, assets)
	if err := s.EnsureSchema(ctx); err != nil {
		t.Fatalf("EnsureSchema on old table: %v", err)
	}

	id, err := NewID()
	if err != nil {
		t.Fatal(err)
	}
	task, err := s.Create(ctx, Task{
		ID: id, CanvasID: 1, NodeID: "n", Kind: KindImage, Prompt: "p", Model: "img-m",
		ImageRefs: []string{"https://img.example/ref.png"},
	})
	if err != nil {
		t.Fatalf("Create after migration: %v", err)
	}
	got, err := s.Get(ctx, task.ID)
	if err != nil || len(got.ImageRefs) != 1 {
		t.Fatalf("got = %+v err %v, want the references back after migration", got, err)
	}

	// 幂等:再跑一遍 EnsureSchema 不得报错(列已存在)。
	if err := s.EnsureSchema(ctx); err != nil {
		t.Errorf("second EnsureSchema: %v", err)
	}
}

func mustTaskID(t *testing.T) string {
	t.Helper()
	id, err := NewID()
	if err != nil {
		t.Fatal(err)
	}
	return id
}
