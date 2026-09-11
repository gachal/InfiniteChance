package asset

import (
	"bytes"
	"context"
	"errors"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/gachal/InfiniteChance/internal/objectstore"
)

// 内部测试:超限分支直接调小未导出的 maxUploadBytes、两张方向表互查;
// 其余上传行为测试在 upload_test.go(asset_test 包)。

// tinyStore 只实现上传路径用到的 Create;Get/List/Delete 不上桌。
type tinyStore struct {
	mu    sync.Mutex
	rows  []Asset
	fails bool
}

func (s *tinyStore) Create(_ context.Context, a Asset) (Asset, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.fails {
		return Asset{}, errors.New("store down")
	}
	a.ID = int64(len(s.rows) + 1)
	s.rows = append(s.rows, a)
	return a, nil
}

func (s *tinyStore) Get(context.Context, int64) (Asset, error)      { return Asset{}, ErrNotFound }
func (s *tinyStore) List(context.Context, Filter) ([]Listed, error) { return nil, nil }
func (s *tinyStore) Delete(context.Context, int64) error            { return ErrNotFound }

func TestUploadOversizeAnswersFileTooLarge(t *testing.T) {
	old := maxUploadBytes
	maxUploadBytes = 64
	defer func() { maxUploadBytes = old }()

	gin.SetMode(gin.TestMode)
	storage, err := objectstore.NewFileSystem(t.TempDir())
	if err != nil {
		t.Fatalf("objectstore: %v", err)
	}
	store := &tinyStore{}
	h := &Handlers{Store: store, Storage: storage}
	r := gin.New()
	RegisterLibraryRoutes(r.Group("/assets"), h)

	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	_ = mw.WriteField("kind", "image")
	part, _ := mw.CreateFormFile("file", "big.png")
	_, _ = part.Write(bytes.Repeat([]byte{1}, 256))
	_ = mw.Close()
	req := httptest.NewRequest(http.MethodPost, "/assets/upload", &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d body %s, want 400", w.Code, w.Body.String())
	}
	if !bytes.Contains(w.Body.Bytes(), []byte("file_too_large")) {
		t.Errorf("body = %s, want the file_too_large code", w.Body.String())
	}
	if len(store.rows) != 0 {
		t.Errorf("rows = %d, want none", len(store.rows))
	}
}

// TestUploadKeyLayout 锁定键形状:uploads/{yyyymmdd}/{uuid}.{ext} — 日期
// 取 UTC(北京 9/12 凌晨的 uploads 落 9/11),扩展名按内容类型,uuid 段
// 防碰撞。
func TestUploadKeyLayout(t *testing.T) {
	// 北京时间 2026-09-12 07:30 = UTC 2026-09-11 23:30。
	now := time.Date(2026, 9, 12, 7, 30, 0, 0, time.FixedZone("CST", 8*3600))
	key := uploadKey(now, "video/mp4")

	const prefix = "uploads/20260911/"
	if !strings.HasPrefix(key, prefix) {
		t.Errorf("key = %q, want the %q prefix (UTC day)", key, prefix)
	}
	if !strings.HasSuffix(key, ".mp4") {
		t.Errorf("key = %q, want the .mp4 extension", key)
	}
	id := strings.TrimSuffix(strings.TrimPrefix(key, prefix), ".mp4")
	if len(id) != 36 || !strings.Contains(id, "-") {
		t.Errorf("uuid segment = %q, want a canonical uuid", id)
	}
}

// TestUploadableMediaTablesAgree 锁定两张方向表的一致:每个受支持的媒体
// 类型都必须能从 extensionOf 得到键扩展名(不能落 .bin),新增格式漏改
// 任何一张表都在这里炸。
func TestUploadableMediaTablesAgree(t *testing.T) {
	for _, e := range uploadableMedia {
		ext := extensionOf(e.typ)
		if ext == "" || ext == ".bin" {
			t.Errorf("%s ↔ %s: extensionOf(%q) = %q, 受支持的类型必须能落出键扩展名", e.ext, e.typ, e.typ, ext)
		}
		if got, ok := extensionContentType(ext); ok && got != e.typ {
			t.Errorf("extensionContentType(%q) = %q, want %q (往返走样)", ext, got, e.typ)
		}
	}
}
