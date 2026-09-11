package objectstore

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/aliyun/aliyun-oss-go-sdk/oss"

	"github.com/gachal/InfiniteChance/internal/settings"
)

// fakeOSS is a minimal in-process OSS REST service: PUT/GET/DELETE object
// under one bucket, answering the SDK's XML error shape for unknown keys.
// The driver suite points the real SDK at it with ForcePathStyle — that is
// the only production-invisible knob the test needs.
type fakeOSS struct {
	mu     sync.Mutex
	objs   map[string][]byte
	ctypes map[string]string
}

const fakeBucket = "test-bucket"

func newFakeOSS() *fakeOSS {
	return &fakeOSS{objs: map[string][]byte{}, ctypes: map[string]string{}}
}

func (f *fakeOSS) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("PUT /{bucket}/{key...}", func(w http.ResponseWriter, r *http.Request) {
		if r.PathValue("bucket") != fakeBucket {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		body, _ := io.ReadAll(r.Body)
		f.mu.Lock()
		f.objs[r.PathValue("key")] = body
		f.ctypes[r.PathValue("key")] = r.Header.Get("Content-Type")
		f.mu.Unlock()
		w.WriteHeader(http.StatusOK)
	})
	mux.HandleFunc("GET /{bucket}/{key...}", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		body, ok := f.objs[r.PathValue("key")]
		ct := f.ctypes[r.PathValue("key")]
		f.mu.Unlock()
		if !ok {
			w.Header().Set("Content-Type", "application/xml")
			w.WriteHeader(http.StatusNotFound)
			fmt.Fprint(w, `<?xml version="1.0"?><Error><Code>NoSuchKey</Code><Message>not found</Message></Error>`)
			return
		}
		w.Header().Set("Content-Type", ct)
		_, _ = w.Write(body)
	})
	mux.HandleFunc("DELETE /{bucket}/{key...}", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		delete(f.objs, r.PathValue("key"))
		f.mu.Unlock()
		w.WriteHeader(http.StatusNoContent)
	})
	return mux
}

// newTestOSS builds the driver against the fake service.
func newTestOSS(t *testing.T) (*OSS, *fakeOSS) {
	t.Helper()
	fake := newFakeOSS()
	server := httptest.NewServer(fake.handler())
	t.Cleanup(server.Close)
	st, err := newOSS(settings.OSSConfig{
		Endpoint: server.URL, Bucket: fakeBucket,
		AccessKey: "AK", SecretKey: "SK",
	}, oss.ForcePathStyle(true))
	if err != nil {
		t.Fatalf("newOSS: %v", err)
	}
	return st, fake
}

func TestOSSPutOpenDeleteRoundTrip(t *testing.T) {
	st, _ := newTestOSS(t)
	ctx := context.Background()
	key := "canvases/1/ct_x/image.png"

	payload := []byte("png-bytes")
	if err := st.Put(ctx, key, bytes.NewReader(payload), int64(len(payload)), "image/png"); err != nil {
		t.Fatalf("Put: %v", err)
	}

	obj, err := st.Open(ctx, key)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	got, err := io.ReadAll(obj)
	obj.Close()
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !bytes.Equal(got, payload) {
		t.Fatalf("round trip = %q, want %q", got, payload)
	}

	if err := st.Delete(ctx, key); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := st.Open(ctx, key); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("Open after delete err = %v, want fs.ErrNotExist-compatible", err)
	}
}

// Open 未命中按接口契约回答 fs.ErrNotExist 兼容错误 —— Dynamic 的回退
// 信号。
func TestOSSOpenMissingKeyAnswersErrNotExist(t *testing.T) {
	st, _ := newTestOSS(t)
	if _, err := st.Open(context.Background(), "uploads/20260911/nope.png"); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("err = %v, want fs.ErrNotExist-compatible", err)
	}
}

// 删除不存在的键是 no-op(接口契约),不因服务端 404 报错。
func TestOSSDeleteMissingKeyIsNoop(t *testing.T) {
	st, _ := newTestOSS(t)
	if err := st.Delete(context.Background(), "uploads/20260911/nope.png"); err != nil {
		t.Fatalf("Delete missing key = %v, want nil", err)
	}
}

// 坏键(逃出根目录/反斜杠)与 FileSystem 同规拒绝,两代驱动的键语义一致。
func TestOSSRejectsInvalidKeys(t *testing.T) {
	st, _ := newTestOSS(t)
	ctx := context.Background()
	for _, key := range []string{"", "../escape", "a/../b", `back\slash`, "/abs"} {
		if err := st.Put(ctx, key, strings.NewReader("x"), 1, "text/plain"); err == nil {
			t.Fatalf("Put(%q) should be rejected", key)
		}
		if _, err := st.Open(ctx, key); err == nil {
			t.Fatalf("Open(%q) should be rejected", key)
		}
		if err := st.Delete(ctx, key); err == nil {
			t.Fatalf("Delete(%q) should be rejected", key)
		}
	}
}

// 不完整连接拒绝构造。
func TestOSSIncompleteConfigRejected(t *testing.T) {
	for name, cfg := range map[string]settings.OSSConfig{
		"no endpoint": {Bucket: "b", AccessKey: "AK", SecretKey: "SK"},
		"no bucket":   {Endpoint: "e", AccessKey: "AK", SecretKey: "SK"},
		"no creds":    {Endpoint: "e", Bucket: "b"},
	} {
		if _, err := NewOSS(cfg); err == nil {
			t.Fatalf("%s: NewOSS should fail", name)
		}
	}
}
