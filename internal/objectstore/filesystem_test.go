package objectstore_test

import (
	"context"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gachal/InfiniteChance/internal/objectstore"
)

func newTestStore(t *testing.T) *objectstore.FileSystem {
	t.Helper()
	s, err := objectstore.NewFileSystem(t.TempDir())
	if err != nil {
		t.Fatalf("NewFileSystem: %v", err)
	}
	return s
}

func put(t *testing.T, s objectstore.Store, key, body string) {
	t.Helper()
	if err := s.Put(context.Background(), key, strings.NewReader(body), int64(len(body)), "image/png"); err != nil {
		t.Fatalf("Put %q: %v", key, err)
	}
}

func TestFileSystemPutOpenRoundTrip(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	put(t, s, "canvases/42/ct_abc/image.png", "png-bytes")

	f, err := s.Open(ctx, "canvases/42/ct_abc/image.png")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer f.Close()
	got, err := io.ReadAll(f)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if string(got) != "png-bytes" {
		t.Errorf("round trip = %q, want %q", got, "png-bytes")
	}
}

func TestFileSystemOpenMissingIsNotFound(t *testing.T) {
	s := newTestStore(t)
	_, err := s.Open(context.Background(), "canvases/1/ct_x/image.png")
	if !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("Open missing = %v, want fs.ErrNotExist", err)
	}
}

func TestFileSystemDeleteRemovesFileAndEmptyDirs(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	put(t, s, "canvases/7/ct_a/image.png", "a")
	put(t, s, "canvases/7/ct_b/image.png", "b")

	if err := s.Delete(ctx, "canvases/7/ct_a/image.png"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := s.Open(ctx, "canvases/7/ct_a/image.png"); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("Open after delete = %v, want fs.ErrNotExist", err)
	}
	// ct_b 仍在,7 号画布目录不能被清走。
	if _, err := s.Open(ctx, "canvases/7/ct_b/image.png"); err != nil {
		t.Fatalf("sibling object missing after delete: %v", err)
	}
	if err := s.Delete(ctx, "canvases/7/ct_b/image.png"); err != nil {
		t.Fatalf("Delete sibling: %v", err)
	}
	if _, err := os.Stat(filepath.Join(s.Root, "canvases")); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("empty archive tree survived deletes: %v", err)
	}
}

func TestFileSystemDeleteMissingIsNoop(t *testing.T) {
	s := newTestStore(t)
	if err := s.Delete(context.Background(), "canvases/1/ct_x/image.png"); err != nil {
		t.Errorf("Delete missing = %v, want nil", err)
	}
}

func TestFileSystemRefusesEscapingKeys(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	for _, key := range []string{
		"", "../escape.png", "a/../../escape.png", "/abs.png", `back\slash.png`, "a//b.png",
	} {
		err := s.Put(ctx, key, strings.NewReader("x"), 1, "text/plain")
		if err == nil {
			t.Errorf("Put %q accepted, want refused", key)
		}
	}
}

func TestFileSystemPutSizeMismatchFails(t *testing.T) {
	s := newTestStore(t)
	err := s.Put(context.Background(), "canvases/1/ct_x/image.png", strings.NewReader("hello"), 4, "text/plain")
	if err == nil {
		t.Errorf("Put with short body accepted, want error")
	}
	if _, serr := os.Stat(filepath.Join(s.Root, "canvases/1/ct_x/image.png")); !errors.Is(serr, fs.ErrNotExist) {
		t.Errorf("failed Put left an object behind: %v", serr)
	}
}

func TestFileSystemPutReplacesExistingObject(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	put(t, s, "canvases/1/ct_x/image.png", "first")
	put(t, s, "canvases/1/ct_x/image.png", "second")
	f, err := s.Open(ctx, "canvases/1/ct_x/image.png")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer f.Close()
	got, _ := io.ReadAll(f)
	if string(got) != "second" {
		t.Errorf("object = %q, want the replacement", got)
	}
}
