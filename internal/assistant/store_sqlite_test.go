package assistant_test

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/gachal/InfiniteChance/internal/assistant"
	"github.com/gachal/InfiniteChance/internal/canvas"
	"github.com/gachal/InfiniteChance/internal/sqlitedb"
)

// newSQLiteWorld opens a throwaway desktop-shaped pair of stores over one
// database: canvases(建画布行用)+ assistant sessions。SQLite 侧永不 skip,
// 是无 infra 时双方言测试的兜底信号。
func newSQLiteWorld(t *testing.T) (*assistant.SQLiteStore, *canvas.SQLiteStore) {
	t.Helper()
	db, err := sqlitedb.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	ctx := context.Background()
	canvases := canvas.NewSQLiteStore(db)
	if err := canvases.EnsureSchema(ctx); err != nil {
		t.Fatalf("canvas EnsureSchema: %v", err)
	}
	sessions := assistant.NewSQLiteStore(db)
	if err := sessions.EnsureSchema(ctx); err != nil {
		t.Fatalf("assistant EnsureSchema: %v", err)
	}
	return sessions, canvases
}

func mustSQLiteCanvas(t *testing.T, canvases *canvas.SQLiteStore) int64 {
	t.Helper()
	cv, err := canvases.Create(context.Background(), 1, "测试画布", []byte(`{"nodes":[],"edges":[]}`), "general")
	if err != nil {
		t.Fatalf("create canvas: %v", err)
	}
	return cv.ID
}

// 主干:建会话缺省 ask、列序最新在前、读/改/删全在画布作用域内收窄,
// DeleteByCanvas 级联清空该画布全部会话。
func TestSQLiteSessionStoreCRUDAndCascade(t *testing.T) {
	sessions, canvases := newSQLiteWorld(t)
	ctx := context.Background()
	canvasID := mustSQLiteCanvas(t, canvases)
	otherCanvas := mustSQLiteCanvas(t, canvases)

	s1, err := sessions.Create(ctx, canvasID, "")
	if err != nil {
		t.Fatalf("create s1: %v", err)
	}
	if s1.ID == 0 || s1.CanvasID != canvasID || s1.Mode != assistant.ModeAsk || s1.Title != "" {
		t.Fatalf("s1 = %+v, want canvas-bound ask session with empty title", s1)
	}
	if len(s1.Messages) != 0 {
		t.Fatalf("s1.messages = %v, want empty", s1.Messages)
	}

	s2, err := sessions.Create(ctx, canvasID, assistant.ModeFull)
	if err != nil {
		t.Fatalf("create s2: %v", err)
	}
	if s2.Mode != assistant.ModeFull {
		t.Fatalf("s2.mode = %q, want full", s2.Mode)
	}

	// List:该画布两会话、另一画布看不到;最新在前。
	here, err := sessions.List(ctx, canvasID)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(here) != 2 || here[0].ID != s2.ID || here[1].ID != s1.ID {
		t.Fatalf("list = %+v, want [s2 s1] newest first", here)
	}
	elsewhere, err := sessions.List(ctx, otherCanvas)
	if err != nil {
		t.Fatalf("list other: %v", err)
	}
	if len(elsewhere) != 0 {
		t.Fatalf("other canvas list = %v, want empty", elsewhere)
	}

	// Update:messages + title 落库可读回;mode 切换。
	userMsg := assistant.Message{Role: "user", Content: "帮我在画布上加一个角色节点"}
	finalMsg := assistant.Message{Role: "assistant", Content: "已新建 1 个节点", Summary: "本轮工具调用:create_nodes(已新建 1 个节点)"}
	mode := assistant.ModeFull
	title := "加角色节点"
	if _, err := sessions.Update(ctx, canvasID, s1.ID, &assistant.UpdateInput{
		Messages: &[]assistant.Message{userMsg, finalMsg},
		Title:    &title,
		Mode:     &mode,
	}); err != nil {
		t.Fatalf("update: %v", err)
	}
	got, err := sessions.Get(ctx, canvasID, s1.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Title != title || got.Mode != assistant.ModeFull {
		t.Fatalf("got = %+v, want updated title/mode", got)
	}
	if len(got.Messages) != 2 || got.Messages[0] != userMsg || got.Messages[1] != finalMsg {
		t.Fatalf("messages = %+v, want the persisted pair", got.Messages)
	}
	if got.UpdatedAt.Before(got.CreatedAt) {
		t.Errorf("updated_at %v before created_at %v", got.UpdatedAt, got.CreatedAt)
	}

	// 跨画布访问 = 不存在(会话绑死画布,不泄露存在性)。
	if _, err := sessions.Get(ctx, otherCanvas, s1.ID); err != assistant.ErrNotFound {
		t.Fatalf("cross-canvas get err = %v, want ErrNotFound", err)
	}
	if _, err := sessions.Get(ctx, canvasID, 999); err != assistant.ErrNotFound {
		t.Fatalf("missing get err = %v, want ErrNotFound", err)
	}
	if err := sessions.Delete(ctx, otherCanvas, s1.ID); err != assistant.ErrNotFound {
		t.Fatalf("cross-canvas delete err = %v, want ErrNotFound", err)
	}

	// 单删与级联删。
	if err := sessions.Delete(ctx, canvasID, s2.ID); err != nil {
		t.Fatalf("delete s2: %v", err)
	}
	left, err := sessions.List(ctx, canvasID)
	if err != nil || len(left) != 1 {
		t.Fatalf("after single delete list = %v (err %v), want one row", left, err)
	}
	if err := sessions.DeleteByCanvas(ctx, canvasID); err != nil {
		t.Fatalf("delete by canvas: %v", err)
	}
	empty, err := sessions.List(ctx, canvasID)
	if err != nil || len(empty) != 0 {
		t.Fatalf("after cascade list = %v (err %v), want empty", empty, err)
	}
	// 级联删不波及别的画布。
	if err := sessions.DeleteByCanvas(ctx, otherCanvas); err != nil {
		t.Fatalf("delete by other canvas: %v", err)
	}
}
