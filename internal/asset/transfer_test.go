package asset_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gachal/InfiniteChance/internal/asset"
	"github.com/gachal/InfiniteChance/internal/objectstore"
)

func newTransferStore(t *testing.T) *objectstore.FileSystem {
	t.Helper()
	s, err := objectstore.NewFileSystem(t.TempDir())
	if err != nil {
		t.Fatalf("NewFileSystem: %v", err)
	}
	return s
}

func mustTransfer(t *testing.T, s objectstore.Store, canvasID int64, taskID, kind, url string) asset.Stored {
	t.Helper()
	got, err := asset.Transfer(context.Background(), s, http.DefaultClient, canvasID, taskID, kind, url)
	if err != nil {
		t.Fatalf("Transfer: %v", err)
	}
	return got
}

func readObject(t *testing.T, s objectstore.Store, key string) string {
	t.Helper()
	f, err := s.Open(context.Background(), key)
	if err != nil {
		t.Fatalf("Open %q: %v", key, err)
	}
	defer f.Close()
	var sb strings.Builder
	buf := make([]byte, 512)
	for {
		n, err := f.Read(buf)
		sb.Write(buf[:n])
		if err != nil {
			break
		}
	}
	return sb.String()
}

func TestTransferDataURLArchivesDecodedBytes(t *testing.T) {
	s := newTransferStore(t)
	got := mustTransfer(t, s, 42, "ct_abc", asset.KindImage, "data:image/png;base64,cG5n")

	wantKey := "canvases/42/ct_abc/image.png"
	if got.Key != wantKey {
		t.Errorf("key = %q, want %q", got.Key, wantKey)
	}
	if got.ContentType != "image/png" {
		t.Errorf("content type = %q, want image/png", got.ContentType)
	}
	if body := readObject(t, s, got.Key); body != "png" {
		t.Errorf("object body = %q, want the decoded bytes", body)
	}
	if got.SizeBytes != int64(len("png")) {
		t.Errorf("size = %d, want %d", got.SizeBytes, len("png"))
	}
}

func TestTransferHTTPURLDownloadsIntoArchiveLayout(t *testing.T) {
	s := newTransferStore(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "video/mp4; charset=binary")
		_, _ = w.Write([]byte("mp4-bytes"))
	}))
	defer srv.Close()

	got := mustTransfer(t, s, 7, "ct_v1", asset.KindVideo, srv.URL+"/clip")

	wantKey := "canvases/7/ct_v1/video.mp4"
	if got.Key != wantKey {
		t.Errorf("key = %q, want %q (按画布/任务归档)", got.Key, wantKey)
	}
	if got.ContentType != "video/mp4" {
		t.Errorf("content type = %q, want parameters stripped", got.ContentType)
	}
	if body := readObject(t, s, got.Key); body != "mp4-bytes" {
		t.Errorf("object body = %q, want the downloaded bytes", body)
	}
}

func TestTransferNon200IsAnError(t *testing.T) {
	s := newTransferStore(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	_, err := asset.Transfer(context.Background(), s, http.DefaultClient, 1, "ct_x", asset.KindImage, srv.URL+"/gone")
	if err == nil || !strings.Contains(err.Error(), "404") {
		t.Errorf("Transfer = %v, want an HTTP-status failure", err)
	}
}

func TestTransferUnknownContentTypeDefaultsByKind(t *testing.T) {
	s := newTransferStore(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/octet-stream")
		_, _ = w.Write([]byte("bytes"))
	}))
	defer srv.Close()

	got := mustTransfer(t, s, 1, "ct_i", asset.KindImage, srv.URL+"/img")
	if got.ContentType != "image/png" {
		t.Errorf("image content type = %q, want the kind default", got.ContentType)
	}
	if filepath.Ext(got.Key) != ".png" {
		t.Errorf("key = %q, want a kind-matched extension", got.Key)
	}
}

func TestTransferWithoutStorageFails(t *testing.T) {
	_, err := asset.Transfer(context.Background(), nil, nil, 1, "ct_x", asset.KindImage, "https://img.example/a.png")
	if err == nil {
		t.Errorf("Transfer without storage accepted, want error")
	}
}

func TestObjectKeySanitizesArchiveShape(t *testing.T) {
	got := asset.ObjectKey(42, "ct_abc", asset.KindVideo, "video/quicktime")
	if got != "canvases/42/ct_abc/video.mov" {
		t.Errorf("ObjectKey = %q", got)
	}
}
