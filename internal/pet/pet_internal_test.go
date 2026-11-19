package pet

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"io/fs"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/gin-gonic/gin"
)

// 内部包测试:上限是包级变量,测试把它调小验证 MaxBytesReader 在 multipart
// 解析期掐断(asset.maxUploadBytes 同款手法)。生产行为不变(20 MiB)。

// tinyStorage 是只计数的假驱动:超限分支根本不该有字节落桶。
type tinyStorage struct {
	mu      sync.Mutex
	objects map[string][]byte
}

func (s *tinyStorage) Put(_ context.Context, key string, r io.Reader, size int64, _ string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	raw, err := io.ReadAll(io.LimitReader(r, size))
	if err != nil {
		return err
	}
	s.objects[key] = raw
	return nil
}

func (s *tinyStorage) Open(_ context.Context, key string) (io.ReadCloser, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	raw, ok := s.objects[key]
	if !ok {
		return nil, fmt.Errorf("objectstore: object %q: %w", key, fs.ErrNotExist)
	}
	return io.NopCloser(bytes.NewReader(raw)), nil
}

func (s *tinyStorage) Delete(_ context.Context, key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.objects, key)
	return nil
}

func (s *tinyStorage) ThumbnailParam(context.Context, int) string { return "" }

func TestUploadPetImageOversizeRejectedAtParse(t *testing.T) {
	old := maxImageBytes
	maxImageBytes = 64
	t.Cleanup(func() { maxImageBytes = old })

	gin.SetMode(gin.TestMode)
	storage := &tinyStorage{objects: map[string][]byte{}}
	engine := gin.New()
	RegisterAdminRoutes(engine.Group("/admin"), &Handlers{Storage: storage})

	big := bytes.Repeat([]byte("GIF89a"), 100) // 600 字节 > 调小后的 64 上限
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	part, err := mw.CreateFormFile("file", "big.gif")
	if err != nil {
		t.Fatalf("build form file: %v", err)
	}
	if _, err := part.Write(big); err != nil {
		t.Fatalf("write form file: %v", err)
	}
	mw.Close()
	req := httptest.NewRequest(http.MethodPost, "/admin/settings/pet/image", &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	w := httptest.NewRecorder()
	engine.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("HTTP %d: %s, want 400", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "file_too_large") {
		t.Fatalf("body %s should carry file_too_large", w.Body.String())
	}
	if len(storage.objects) != 0 {
		t.Fatalf("oversize upload must not land any object")
	}
}
