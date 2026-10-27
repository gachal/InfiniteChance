package musictask

// 配乐异步任务实体的单元测试(60 号票):MPS 词表归并(WAIT/RUN/DONE/
// FAIL 与未知态)+ SQLite store 的建行/守卫更新轮转(桌面方言全链,不依
// 赖 MySQL 基础设施;MySQL store 与 SQLite 版同构,由 compose 环境的集成
// 测试覆盖)。

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/gachal/InfiniteChance/internal/sqlitedb"
)

func TestMergeStatusFoldsMPSVocabulary(t *testing.T) {
	cases := map[string]Status{
		"WAIT":  StatusQueued,
		"wait":  StatusQueued,
		"RUN":   StatusRunning,
		"DONE":  StatusSucceeded,
		"FAIL":  StatusFailed,
		"OTHER": StatusFailed, // 未知态归并 failed
		"":      StatusFailed,
	}
	for raw, want := range cases {
		if got := MergeStatus(raw); got != want {
			t.Errorf("MergeStatus(%q) = %s, want %s", raw, got, want)
		}
	}
}

func newSQLiteStore(t *testing.T) *SQLiteStore {
	t.Helper()
	db, err := sqlitedb.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	s := NewSQLiteStore(db)
	if err := s.EnsureSchema(context.Background()); err != nil {
		t.Fatalf("EnsureSchema: %v", err)
	}
	return s
}

func TestSQLiteStoreCreateGetUpdateRoundTrip(t *testing.T) {
	s := newSQLiteStore(t)
	ctx := context.Background()

	created, err := s.Create(ctx, Task{
		ID: "mt_test", KeyID: 7, ChannelID: 3, ChannelName: "mps",
		PublicModel: "bgm-m", UpstreamModel: "MiniMaxMusic 3.0",
		UpstreamTaskID: "mps-task-1", Status: StatusQueued,
		ReservedMicros: 200_000, PriceSnapshot: []byte(`{"unit":"call"}`),
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if created.CreatedAt.IsZero() || created.UpdatedAt.IsZero() {
		t.Errorf("timestamps not stamped: %+v", created)
	}

	got, err := s.Get(ctx, "mt_test")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.KeyID != 7 || got.PublicModel != "bgm-m" || got.ReservedMicros != 200_000 ||
		string(got.PriceSnapshot) != `{"unit":"call"}` {
		t.Errorf("round trip = %+v, want the seeded facts", got)
	}

	if _, err := s.Get(ctx, "mt_missing"); err != ErrNotFound {
		t.Errorf("missing Get err = %v, want ErrNotFound", err)
	}

	// 推进 running → succeeded(守卫命中,胜者拿走计费定格权)。
	raw := "DONE"
	charge := int64(200_000)
	url := "https://bucket.cos/a.mp3"
	fresh, won, err := s.Update(ctx, "mt_test", ActiveStatuses, Patch{
		Status: StatusSucceeded, UpstreamStatus: &raw, AudioURL: &url, ChargeMicros: &charge,
	})
	if err != nil || !won {
		t.Fatalf("Update = won %v err %v, want win", won, err)
	}
	if fresh.Status != StatusSucceeded || fresh.AudioURL != url || fresh.ChargeMicros != charge ||
		fresh.UpstreamStatus != "DONE" {
		t.Errorf("fresh = %+v, want succeeded with url/charge", fresh)
	}

	// 终态后不再受 active 守卫更新:0 行,现值回读、won=false。
	raw2 := "DONE"
	_, won, err = s.Update(ctx, "mt_test", ActiveStatuses, Patch{Status: StatusFailed, UpstreamStatus: &raw2})
	if err != nil || won {
		t.Errorf("terminal re-update = won %v err %v, want loss", won, err)
	}
	if got, _ := s.Get(ctx, "mt_test"); got.Status != StatusSucceeded {
		t.Errorf("status after lost update = %s, want still succeeded", got.Status)
	}
}

func TestSQLiteStoreTimestampsParseBack(t *testing.T) {
	s := newSQLiteStore(t)
	ctx := context.Background()

	if _, err := s.Create(ctx, Task{
		ID: "mt_time", KeyID: 1, ChannelID: 1, ChannelName: "mps",
		PublicModel: "bgm", UpstreamModel: "GL", UpstreamTaskID: "t1",
		Status: StatusQueued,
	}); err != nil {
		t.Fatalf("Create: %v", err)
	}
	got, err := s.Get(ctx, "mt_time")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	// 定宽 RFC3339 回读必须是无误差的 UTC 时间(sqlitedb 纪律:9 位小数
	// 的定宽 TEXT,ParseTime 往返不漂)。
	if want := sqlitedb.FormatTime(got.CreatedAt); len(want) != len("2006-01-02T15:04:05.000000000Z") {
		t.Errorf("created_at round trip = %v (%s), want the canonical fixed width", got.CreatedAt, want)
	}
	if time.Since(got.CreatedAt) > time.Minute {
		t.Errorf("created_at = %v, want a fresh stamp", got.CreatedAt)
	}
}
