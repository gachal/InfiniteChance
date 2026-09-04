package canvastask_test

import (
	"context"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gachal/InfiniteChance/internal/canvastask"
	"github.com/gachal/InfiniteChance/internal/objectstore"
)

// archiveWorker wires a test worker with object storage attached (14 号票):
// 产物在终态前转存,素材行携带 object_key 三件套。
func archiveWorker(t *testing.T, store *canvastask.MySQLStore, gw *stubGateway, st objectstore.Store) *canvastask.Worker {
	t.Helper()
	return canvastask.NewWorker(store, gw,
		canvastask.WithPollInterval(20*time.Millisecond),
		canvastask.WithTaskTimeout(2*time.Second),
		canvastask.WithVideoPollInterval(20*time.Millisecond),
		canvastask.WithVideoTaskTimeout(2*time.Second),
		canvastask.WithConcurrency(2),
		canvastask.WithStorage(st),
		canvastask.WithLogger(log.New(&strings.Builder{}, "", 0)),
	)
}

// queueImage submits one queued image task straight through the store.
func queueImage(t *testing.T, store *canvastask.MySQLStore, id string) canvastask.Task {
	t.Helper()
	task, err := store.Create(context.Background(), canvastask.Task{
		ID: id, CanvasID: 42, NodeID: "image-1", Kind: canvastask.KindImage,
		Prompt: "p", Model: "img-m", Status: canvastask.StatusQueued,
	})
	if err != nil {
		t.Fatalf("create task: %v", err)
	}
	return task
}

func TestWorkerArchivesAssetIntoCanvasTaskLayout(t *testing.T) {
	store, _ := openTaskTestDB(t)
	st, err := objectstore.NewFileSystem(t.TempDir())
	if err != nil {
		t.Fatalf("objectstore: %v", err)
	}
	media := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write([]byte("png-bytes"))
	}))
	defer media.Close()

	task := queueImage(t, store, "ct_archive_ok")
	gw := &stubGateway{fn: func(context.Context, canvastask.ImageRequest) (canvastask.ImageResult, error) {
		return canvastask.ImageResult{URL: media.URL + "/ok.png"}, nil
	}}
	runWorker(t, archiveWorker(t, store, gw, st))

	done := awaitTask(t, store, task.ID)
	if done.Status != canvastask.StatusSucceeded {
		t.Fatalf("status = %s (%s), want succeeded", done.Status, done.Error)
	}
	wantKey := "canvases/42/" + task.ID + "/image.png"
	got, err := store.Assets.Get(context.Background(), done.AssetID)
	if err != nil {
		t.Fatalf("asset get: %v", err)
	}
	if got.ObjectKey != wantKey {
		t.Errorf("object key = %q, want %q", got.ObjectKey, wantKey)
	}
	body, err := os.ReadFile(filepath.Join(st.Root, filepath.FromSlash(wantKey)))
	if err != nil || string(body) != "png-bytes" {
		t.Errorf("stored object = %q (%v), want the downloaded bytes", body, err)
	}
}

func TestWorkerFailsTaskWhenArchiveFails(t *testing.T) {
	store, _ := openTaskTestDB(t)
	st, err := objectstore.NewFileSystem(t.TempDir())
	if err != nil {
		t.Fatalf("objectstore: %v", err)
	}

	// 转存前置失败:厂商地址指向一个不存在的服务器,下载当场报错,
	// 任务按失败收尾并把原因留在行上(可重试,不落素材)。
	task := queueImage(t, store, "ct_archive_fail")
	gw := &stubGateway{fn: func(context.Context, canvastask.ImageRequest) (canvastask.ImageResult, error) {
		return canvastask.ImageResult{URL: "http://127.0.0.1:1/unreachable.png"}, nil
	}}
	runWorker(t, archiveWorker(t, store, gw, st))

	done := awaitTask(t, store, task.ID)
	if done.Status != canvastask.StatusFailed {
		t.Fatalf("status = %s, want failed", done.Status)
	}
	if done.AssetID != 0 {
		t.Errorf("asset id = %d, want none on a failed transfer", done.AssetID)
	}
	if !strings.Contains(done.Error, "产物下载失败") {
		t.Errorf("error = %q, want the transfer failure reason", done.Error)
	}
}

func TestWorkerWithoutStorageKeepsLegacyVendorRows(t *testing.T) {
	store, _ := openTaskTestDB(t)
	media := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("png-bytes"))
	}))
	defer media.Close()

	task := queueImage(t, store, "ct_legacy_ok")
	gw := &stubGateway{fn: func(context.Context, canvastask.ImageRequest) (canvastask.ImageResult, error) {
		return canvastask.ImageResult{URL: media.URL + "/ok.png"}, nil
	}}
	runWorker(t, newTestWorker(store, gw))

	done := awaitTask(t, store, task.ID)
	if done.Status != canvastask.StatusSucceeded {
		t.Fatalf("status = %s (%s), want succeeded", done.Status, done.Error)
	}
	got, err := store.Assets.Get(context.Background(), done.AssetID)
	if err != nil {
		t.Fatalf("asset get: %v", err)
	}
	if got.ObjectKey != "" {
		t.Errorf("object key = %q, want empty without storage", got.ObjectKey)
	}
}
