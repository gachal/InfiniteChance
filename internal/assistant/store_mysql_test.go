package assistant_test

import (
	"context"
	"database/sql"
	"os"
	"testing"
	"time"

	mysqldriver "github.com/go-sql-driver/mysql"

	"github.com/gachal/InfiniteChance/internal/assistant"
	"github.com/gachal/InfiniteChance/internal/canvas"
)

// openTestStore connects to a dedicated throwaway database on the compose
// MySQL (host port 3307),照 canvas 包 openTestStore 同款:MYSQL_TEST_DSN
// 覆写,不可达自动 skip(无基础设施 go test 保持绿)。
func openTestStore(t *testing.T) (*assistant.MySQLStore, *canvas.MySQLStore) {
	t.Helper()

	dsn := os.Getenv("MYSQL_TEST_DSN")
	if dsn == "" {
		dsn = "root:infinitechance@tcp(localhost:3307)/infinitechance_test?parseTime=true"
	}
	cfg, err := mysqldriver.ParseDSN(dsn)
	if err != nil {
		t.Fatalf("parse MYSQL_TEST_DSN: %v", err)
	}
	dbName := cfg.DBName + "_assistant"
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
	if _, err := server.ExecContext(ctx, "DROP TABLE IF EXISTS `"+dbName+"`.`canvas_assistant_sessions`"); err != nil {
		t.Fatalf("drop stale table: %v", err)
	}
	if _, err := server.ExecContext(ctx, "DROP TABLE IF EXISTS `"+dbName+"`.`canvases`"); err != nil {
		t.Fatalf("drop stale canvases: %v", err)
	}

	cfg.DBName = dbName
	db, err := sql.Open("mysql", cfg.FormatDSN())
	if err != nil {
		t.Fatalf("open test database: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	canvases := canvas.NewMySQLStore(db)
	if err := canvases.EnsureSchema(ctx); err != nil {
		t.Fatalf("canvas EnsureSchema: %v", err)
	}
	sessions := assistant.NewMySQLStore(db)
	if err := sessions.EnsureSchema(ctx); err != nil {
		t.Fatalf("assistant EnsureSchema: %v", err)
	}
	return sessions, canvases
}

func mustMySQLCanvas(t *testing.T, canvases *canvas.MySQLStore) int64 {
	t.Helper()
	cv, err := canvases.Create(context.Background(), 1, "测试画布", []byte(`{"nodes":[],"edges":[]}`), "general")
	if err != nil {
		t.Fatalf("create canvas: %v", err)
	}
	return cv.ID
}

// 与 SQLite 侧同断言集:双方言同构纪律(笔记 01 §6)。
func TestMySQLSessionStoreCRUDAndCascade(t *testing.T) {
	sessions, canvases := openTestStore(t)
	ctx := context.Background()
	canvasID := mustMySQLCanvas(t, canvases)
	otherCanvas := mustMySQLCanvas(t, canvases)

	s1, err := sessions.Create(ctx, canvasID, "")
	if err != nil {
		t.Fatalf("create s1: %v", err)
	}
	if s1.ID == 0 || s1.CanvasID != canvasID || s1.Mode != assistant.ModeAsk || s1.Title != "" {
		t.Fatalf("s1 = %+v, want canvas-bound ask session with empty title", s1)
	}
	s2, err := sessions.Create(ctx, canvasID, assistant.ModeFull)
	if err != nil {
		t.Fatalf("create s2: %v", err)
	}

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

	if _, err := sessions.Get(ctx, otherCanvas, s1.ID); err != assistant.ErrNotFound {
		t.Fatalf("cross-canvas get err = %v, want ErrNotFound", err)
	}
	if err := sessions.Delete(ctx, otherCanvas, s1.ID); err != assistant.ErrNotFound {
		t.Fatalf("cross-canvas delete err = %v, want ErrNotFound", err)
	}

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
}
