package asset

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/gachal/InfiniteChance/internal/objectstore"
)

// ---- 48 号票:哈希回填与去重(构造历史重复 → 触发 → 合并;重跑幂等) ----

// backfillRows 是种进环境的四行历史:A/B 同字节不同键(合并候选)、C 独
// 立字节、D 无 object_key(仅厂商地址的历史行,跳过)。
type backfillRows struct {
	A, B, C, D Asset
}

// backfillEnv 起一套 SQLite 假库 + 本地卷对象的回填环境并种入四行。
func backfillEnv(t *testing.T) (*Backfill, *SQLiteStore, *objectstore.FileSystem, backfillRows) {
	t.Helper()
	s := newSQLiteAssetStore(t)
	fs, err := objectstore.NewFileSystem(t.TempDir())
	if err != nil {
		t.Fatalf("objectstore: %v", err)
	}
	ctx := context.Background()

	const bytesAB = "same-bytes-ab"
	seed := func(key, content string) Asset {
		a, err := s.Create(ctx, Asset{
			Kind: KindImage, CanvasID: 7, URL: "https://img.example/x.png",
			ObjectKey: key, ContentType: "image/png", SizeBytes: int64(len(content)),
		})
		if err != nil {
			t.Fatalf("seed row %s: %v", key, err)
		}
		if err := fs.Put(ctx, key, strings.NewReader(content), int64(len(content)), "image/png"); err != nil {
			t.Fatalf("seed object %s: %v", key, err)
		}
		return a
	}
	rows := backfillRows{
		A: seed("canvases/7/ct_a/image.png", bytesAB),
		B: seed("canvases/7/ct_b/image.png", bytesAB),
		C: seed("uploads/20261018/legacy.png", "distinct-bytes-c"),
	}
	d, err := s.Create(ctx, Asset{Kind: KindImage, CanvasID: 7, URL: "https://vendor.example/only.png"})
	if err != nil {
		t.Fatalf("seed vendor-only row: %v", err)
	}
	rows.D = d

	bf := &Backfill{Store: s, Storage: fs}
	return bf, s, fs, rows
}

// TestBackfillMergesDuplicatesAndIsIdempotent 主链:首轮把 A/B/C 落哈希、
// B 改指 A 的对象并删冗余、D 跳过;重跑零动作。
func TestBackfillMergesDuplicatesAndIsIdempotent(t *testing.T) {
	bf, s, fs, rows := backfillEnv(t)
	ctx := context.Background()

	bf.run(ctx)

	// 哈希落列:A/B 同指纹,C 独立,D(无对象)保持空 = 跳过不报错。
	gotA, _ := s.Get(ctx, rows.A.ID)
	gotB, _ := s.Get(ctx, rows.B.ID)
	gotC, _ := s.Get(ctx, rows.C.ID)
	gotD, _ := s.Get(ctx, rows.D.ID)
	if gotA.ContentHash == "" || gotA.ContentHash != gotB.ContentHash {
		t.Fatalf("hashes %q vs %q, want A/B sharing one fingerprint", gotA.ContentHash, gotB.ContentHash)
	}
	if gotC.ContentHash == "" || gotC.ContentHash == gotA.ContentHash {
		t.Errorf("hash of C = %q, want an independent fingerprint", gotC.ContentHash)
	}
	if gotD.ContentHash != "" || gotD.ObjectKey != "" {
		t.Errorf("vendor-only row mutated: hash %q key %q, want untouched", gotD.ContentHash, gotD.ObjectKey)
	}

	// 改指:B 指向最老行 A 的对象,冗余对象已删。
	if gotB.ObjectKey != gotA.ObjectKey {
		t.Fatalf("B key = %q, want A's %q (组内保留最老对象)", gotB.ObjectKey, gotA.ObjectKey)
	}
	if _, err := fs.Open(ctx, "canvases/7/ct_b/image.png"); err == nil {
		t.Errorf("redundant object survived the merge")
	}
	if _, err := fs.Open(ctx, gotA.ObjectKey); err != nil {
		t.Errorf("kept object missing: %v", err)
	}
	// C 的对象不动。
	if _, err := fs.Open(ctx, gotC.ObjectKey); err != nil {
		t.Errorf("independent object missing: %v", err)
	}

	p := bf.snapshot()
	if p.ScannedRows != 3 || p.HashedRows != 3 || p.Repointed != 1 || p.DeletedObjs != 1 || p.Running {
		t.Fatalf("progress = %+v, want scanned/hashed 3, repointed/deleted 1, not running", p)
	}

	// 重跑幂等:已有哈希的行跳过,零动作(直调 run 不重置计数器,比增量)。
	before := bf.snapshot()
	bf.run(ctx)
	after := bf.snapshot()
	if after.ScannedRows-before.ScannedRows != 0 || after.HashedRows-before.HashedRows != 0 ||
		after.Repointed-before.Repointed != 0 || after.DeletedObjs-before.DeletedObjs != 0 {
		t.Fatalf("re-run progress = %+v (before %+v), want zero deltas", after, before)
	}
}

// TestBackfillStartIsNoOpWhileRunning 幂等口径:任务在跑时再触发 POST 是
// no-op —— 回当前进度(200),不重置计数、不另起任务。
func TestBackfillStartIsNoOpWhileRunning(t *testing.T) {
	bf, _, _, _ := backfillEnv(t)
	bf.mu.Lock()
	bf.running = true // 手工置「在跑」:任务体被顶替,只验证触发面分支
	bf.scanned.Store(7)
	bf.mu.Unlock()

	gin.SetMode(gin.TestMode)
	r := gin.New()
	RegisterBackfillRoutes(r.Group("/admin/assets/hash-backfill"), bf)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/admin/assets/hash-backfill", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("re-trigger status = %d body %s, want 200 (no-op)", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `"scanned_rows":7`) || !strings.Contains(w.Body.String(), `"running":true`) {
		t.Errorf("progress = %s, want the untouched counters with running intact", w.Body.String())
	}
}

// TestBackfillHandlersThroughHTTP 走 HTTP 面:POST 触发到跑完、GET 读到
// 终态进度(在跑时再 POST 由 Start 的幂等分支承接,不重复起任务)。
func TestBackfillHandlersThroughHTTP(t *testing.T) {
	bf, _, _, _ := backfillEnv(t)
	gin.SetMode(gin.TestMode)
	r := gin.New()
	RegisterBackfillRoutes(r.Group("/admin/assets/hash-backfill"), bf)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/admin/assets/hash-backfill", nil))
	if w.Code != http.StatusAccepted && w.Code != http.StatusOK {
		t.Fatalf("start status = %d body %s", w.Code, w.Body.String())
	}

	// 轮询到任务收尾(数据量小,毫秒级;2s 兜底)。
	deadline := time.Now().Add(2 * time.Second)
	for !strings.Contains(w.Body.String(), `"running":false`) && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
		w = httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/admin/assets/hash-backfill", nil))
		if w.Code != http.StatusOK {
			t.Fatalf("status endpoint = %d", w.Code)
		}
	}
	if !strings.Contains(w.Body.String(), `"running":false`) {
		t.Fatalf("backfill never finished: %s", w.Body.String())
	}
	for _, want := range []string{`"hashed_rows":3`, `"repointed_rows":1`, `"deleted_objects":1`} {
		if !strings.Contains(w.Body.String(), want) {
			t.Errorf("progress %s missing %s", w.Body.String(), want)
		}
	}
}
