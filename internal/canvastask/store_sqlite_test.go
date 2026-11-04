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
		Background: "transparent",
		ImageRefs:  []string{"https://img.example/ref1.png", "https://img.example/ref2.png"},
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
	// 37 号票:background 列保真往返(非空 = "transparent")。
	if got.Background != "transparent" {
		t.Fatalf("background = %q, want transparent verbatim", got.Background)
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
	if got.Background != "" {
		t.Errorf("plain background = %q, want empty (缺省不传)", got.Background)
	}
}

// TestSQLiteCanvasTaskVideoRefsRoundTrip 覆盖 24 号票的结构化参考与比例列:
// video_refs JSON 原样回读、空列表落 NULL、ratio 串保真。
func TestSQLiteCanvasTaskVideoRefsRoundTrip(t *testing.T) {
	s := newSQLiteStore(t)
	ctx := context.Background()

	refs := []VideoRef{
		{URL: "https://img.example/first.png", Kind: "image", Role: RoleFirstFrame},
		{URL: "/api/assets/9/content", Kind: "audio", Role: RoleReferenceAud},
	}
	task, err := s.Create(ctx, Task{
		ID: mustTaskID(t), CanvasID: 1, NodeID: "video-5-1", Kind: KindVideo,
		Prompt: "p", Model: "vid-m", Seconds: 0, Size: "1080p", Ratio: "16:9",
		VideoRefs: refs,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	got, err := s.Get(ctx, task.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if len(got.VideoRefs) != 2 || got.VideoRefs[0] != refs[0] || got.VideoRefs[1] != refs[1] {
		t.Fatalf("video_refs = %v, want both refs in order", got.VideoRefs)
	}
	if got.Ratio != "16:9" || got.Size != "1080p" || got.Seconds != 0 {
		t.Errorf("ratio/size/seconds = %q/%q/%d, want verbatim and 0 (auto)", got.Ratio, got.Size, got.Seconds)
	}

	plain, err := s.Create(ctx, Task{
		ID: mustTaskID(t), CanvasID: 1, NodeID: "video-5-2", Kind: KindVideo,
		Prompt: "p", Model: "vid-m",
	})
	if err != nil {
		t.Fatalf("Create plain: %v", err)
	}
	got, err = s.Get(ctx, plain.ID)
	if err != nil {
		t.Fatalf("Get plain: %v", err)
	}
	if got.VideoRefs != nil || got.Ratio != "" {
		t.Errorf("plain video task = %+v, want nil refs and empty ratio", got)
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

// TestSQLiteCanvasTaskAudioRefRoundTrip 覆盖 78 号票的翻唱参考列:
// audio_ref(提交时已解析的地址)保真往返;普通音乐/歌曲任务空串落 NULL、
// 读回空串(存量行零迁移的既有语义)。
func TestSQLiteCanvasTaskAudioRefRoundTrip(t *testing.T) {
	s := newSQLiteStore(t)
	ctx := context.Background()

	cover, err := s.Create(ctx, Task{
		ID: mustTaskID(t), CanvasID: 1, NodeID: "audio-cover", Kind: KindMusic,
		Prompt: "city pop cover", Model: "vod-minimax-music-3.0-cover",
		Lyrics: "夜色慢慢降落", AudioRef: "https://cdn.example/uploads/20261104/abc.mp3",
	})
	if err != nil {
		t.Fatalf("Create cover: %v", err)
	}
	got, err := s.Get(ctx, cover.ID)
	if err != nil {
		t.Fatalf("Get cover: %v", err)
	}
	if got.AudioRef != "https://cdn.example/uploads/20261104/abc.mp3" {
		t.Fatalf("audio_ref = %q, want the resolved address verbatim", got.AudioRef)
	}

	plain, err := s.Create(ctx, Task{
		ID: mustTaskID(t), CanvasID: 1, NodeID: "audio-bgm", Kind: KindMusic,
		Prompt: "纯音乐", Model: "bgm-m",
	})
	if err != nil {
		t.Fatalf("Create plain: %v", err)
	}
	got, err = s.Get(ctx, plain.ID)
	if err != nil {
		t.Fatalf("Get plain: %v", err)
	}
	if got.AudioRef != "" {
		t.Errorf("plain audio_ref = %q, want empty(普通音乐/歌曲任务)", got.AudioRef)
	}
}

// TestSQLiteCanvasTaskIPTaskRoundTrip 覆盖 70 号票的无画布任务形状:
// canvas_id=0 + ip_id=N 的任务行落库回读,ListByIP 按归属列出、
// ListByCanvas 不见(生成记录面板按画布查、天然不见此类任务的定案)。
func TestSQLiteCanvasTaskIPTaskRoundTrip(t *testing.T) {	s := newSQLiteStore(t)
	ctx := context.Background()

	id, err := NewID()
	if err != nil {
		t.Fatal(err)
	}
	created, err := s.Create(ctx, Task{
		ID: id, CanvasID: 0, NodeID: "", IPID: 7, Kind: KindImage,
		Prompt: "三视图设定", Model: "img-m",
	})
	if err != nil {
		t.Fatalf("Create canvas-free task: %v", err)
	}
	got, err := s.Get(ctx, created.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.CanvasID != 0 || got.NodeID != "" {
		t.Errorf("canvas_id/node_id = %d/%q, want 0/空(无画布任务)", got.CanvasID, got.NodeID)
	}
	if got.IPID != 7 {
		t.Errorf("ip_id = %d, want 7(任务行携带 IP 归属)", got.IPID)
	}

	byIP, err := s.ListByIP(ctx, 7, 10)
	if err != nil {
		t.Fatalf("ListByIP: %v", err)
	}
	if len(byIP) != 1 || byIP[0].ID != created.ID {
		t.Fatalf("ListByIP(7) = %+v, want the task", byIP)
	}
	byOther, err := s.ListByIP(ctx, 8, 10)
	if err != nil {
		t.Fatalf("ListByIP(8): %v", err)
	}
	if len(byOther) != 0 {
		t.Fatalf("ListByIP(8) = %+v, want empty", byOther)
	}
	// 生成记录面板按真实画布 id 查(恒 ≥1),天然不见 canvas_id=0 任务
	// (70 号票定案:这是口径,不是漏数);真实画布 id 查不到本任务。
	byCanvas, err := s.ListByCanvas(ctx, 1, 10)
	if err != nil {
		t.Fatalf("ListByCanvas(1): %v", err)
	}
	if len(byCanvas) != 0 {
		t.Fatalf("ListByCanvas(1) = %+v, want empty(无画布任务不进任何画布列表)", byCanvas)
	}
}
