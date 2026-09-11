package asset_test

import (
	"bytes"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/gachal/InfiniteChance/internal/asset"
)

// ---- 18 号票上传入口的测试 ----

// 各受支持媒体的最小魔数样本:内容只要够嗅探器裁决即可。
var (
	pngBytes = append([]byte("\x89PNG\r\n\x1a\n"), bytes.Repeat([]byte{0x42}, 64)...)
	mp4Bytes = append([]byte("\x00\x00\x00\x20ftypisom"), bytes.Repeat([]byte{0x55}, 48)...)
	movBytes = append([]byte("\x00\x00\x00\x14ftypqt  "), bytes.Repeat([]byte{0x66}, 32)...)
)

// postUpload 按编辑器的请求形状构造一次 multipart 上传。
func postUpload(t *testing.T, r http.Handler, filename, kind string, content []byte) *httptest.ResponseRecorder {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	_ = mw.WriteField("kind", kind)
	part, err := mw.CreateFormFile("file", filename)
	if err != nil {
		t.Fatalf("build form file: %v", err)
	}
	if _, err := part.Write(content); err != nil {
		t.Fatalf("write form file: %v", err)
	}
	if err := mw.Close(); err != nil {
		t.Fatalf("close form: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/assets/upload", &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

type uploadAssetJSON struct {
	ID          int64  `json:"id"`
	Kind        string `json:"kind"`
	URL         string `json:"url"`
	ContentType string `json:"content_type"`
	SizeBytes   int64  `json:"size_bytes"`
	ContentURL  string `json:"content_url"`
}

func decodeUpload(t *testing.T, w *httptest.ResponseRecorder) uploadAssetJSON {
	t.Helper()
	var resp struct {
		Asset uploadAssetJSON `json:"asset"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("response not JSON: %v", err)
	}
	return resp.Asset
}

func TestUploadImageArchivesObjectAndRow(t *testing.T) {
	env := newAssetEnv(t)
	w := postUpload(t, env.engine, "photo.png", "image", pngBytes)
	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d body %s, want 201", w.Code, w.Body.String())
	}
	a := decodeUpload(t, w)
	if a.Kind != "image" || a.URL != "" {
		t.Errorf("kind/url = %q/%q, want image and an empty vendor url", a.Kind, a.URL)
	}
	if a.ContentType != "image/png" || a.SizeBytes != int64(len(pngBytes)) {
		t.Errorf("content_type/size = %q/%d, want image/png and %d", a.ContentType, a.SizeBytes, len(pngBytes))
	}
	if a.ContentURL != "/api/assets/"+strconv.FormatInt(a.ID, 10)+"/content" {
		t.Errorf("content_url = %q, want the content-addressed path", a.ContentURL)
	}

	stored := env.store.assets[a.ID]
	if !strings.HasPrefix(stored.ObjectKey, "uploads/") {
		t.Errorf("object key = %q, want the uploads/ prefix", stored.ObjectKey)
	}
	// 键内日期段是上传当天(UTC):uploads/{yyyymmdd}/…。
	wantPrefix := "uploads/" + time.Now().UTC().Format("20060102") + "/"
	if !strings.HasPrefix(stored.ObjectKey, wantPrefix) {
		t.Errorf("object key = %q, want the %q prefix", stored.ObjectKey, wantPrefix)
	}
	if ext := filepath.Ext(stored.ObjectKey); ext != ".png" {
		t.Errorf("object key ext = %q, want .png", ext)
	}
	if stored.CanvasID != 0 || stored.TaskID != "" || stored.Model != "" {
		t.Errorf("canvas/task/model = %d/%q/%q, want zero: 上传素材独立于画布与任务", stored.CanvasID, stored.TaskID, stored.Model)
	}

	obj, err := env.storage.Open(t.Context(), stored.ObjectKey)
	if err != nil {
		t.Fatalf("open stored object: %v", err)
	}
	defer obj.Close()
	raw, _ := io.ReadAll(obj)
	if !bytes.Equal(raw, pngBytes) {
		t.Errorf("stored bytes differ from the upload (%d vs %d)", len(raw), len(pngBytes))
	}
}

func TestUploadVideoSniffsMP4AndQuicktime(t *testing.T) {
	env := newAssetEnv(t)
	for _, tc := range []struct {
		name, filename, wantType, wantExt string
		payload                           []byte
	}{
		{"mp4", "clip.mp4", "video/mp4", ".mp4", mp4Bytes},
		{"mov by qt brand", "clip.mov", "video/quicktime", ".mov", movBytes},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := postUpload(t, env.engine, tc.filename, "video", tc.payload)
			if w.Code != http.StatusCreated {
				t.Fatalf("status = %d body %s, want 201", w.Code, w.Body.String())
			}
			a := decodeUpload(t, w)
			if a.Kind != "video" || a.ContentType != tc.wantType {
				t.Errorf("kind/content_type = %q/%q, want video/%q", a.Kind, a.ContentType, tc.wantType)
			}
			if ext := filepath.Ext(env.store.assets[a.ID].ObjectKey); ext != tc.wantExt {
				t.Errorf("object key ext = %q, want %q", ext, tc.wantExt)
			}
		})
	}
}

func TestUploadUnknownExtensionStillSniffedByMagic(t *testing.T) {
	// 魔数是权威:无扩展名的 PNG 照常入库,不因名字定罪。
	env := newAssetEnv(t)
	w := postUpload(t, env.engine, "blob", "image", pngBytes)
	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d body %s, want 201", w.Code, w.Body.String())
	}
	if a := decodeUpload(t, w); a.ContentType != "image/png" {
		t.Errorf("content_type = %q, want image/png by magic", a.ContentType)
	}
}

func TestUploadRejections(t *testing.T) {
	cases := []struct {
		name     string
		filename string
		kind     string
		payload  []byte
		wantCode string
	}{
		{"unknown magic", "note.txt", "image", bytes.Repeat([]byte("just text"), 8), "file_type_mismatch"},
		{"extension disagrees with magic", "clip.mp4", "image", pngBytes, "file_type_mismatch"},
		{"kind disagrees with content", "photo.png", "video", pngBytes, "file_type_mismatch"},
		{"bad kind value", "photo.png", "audio", pngBytes, "invalid_request"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			env := newAssetEnv(t)
			w := postUpload(t, env.engine, tc.filename, tc.kind, tc.payload)
			if w.Code != http.StatusBadRequest {
				t.Fatalf("status = %d body %s, want 400", w.Code, w.Body.String())
			}
			var parsed struct {
				Error struct {
					Code string `json:"code"`
				} `json:"error"`
			}
			if err := json.Unmarshal(w.Body.Bytes(), &parsed); err != nil {
				t.Fatalf("response not JSON: %v", err)
			}
			if parsed.Error.Code != tc.wantCode {
				t.Errorf("code = %q, want %q", parsed.Error.Code, tc.wantCode)
			}
			if len(env.store.assets) != 0 {
				t.Errorf("store has %d rows, want none on a rejected upload", len(env.store.assets))
			}
		})
	}
}

func TestUploadMissingFileFieldAnswers400(t *testing.T) {
	env := newAssetEnv(t)
	req := httptest.NewRequest(http.MethodPost, "/assets/upload", strings.NewReader("kind=image"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	env.engine.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d body %s, want 400", w.Code, w.Body.String())
	}
}

func TestUploadWithoutStorageAnswers503(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := &asset.Handlers{Store: newFakeStore(), Storage: nil}
	r := gin.New()
	asset.RegisterLibraryRoutes(r.Group("/assets"), h)

	w := postUpload(t, r, "photo.png", "image", pngBytes)
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d body %s, want 503", w.Code, w.Body.String())
	}
	var parsed struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &parsed)
	if parsed.Error.Code != "storage_unconfigured" {
		t.Errorf("code = %q, want storage_unconfigured", parsed.Error.Code)
	}
}

func TestUploadCleansObjectWhenRowFails(t *testing.T) {
	env := newAssetEnv(t)
	env.store.createErr = io.ErrUnexpectedEOF

	w := postUpload(t, env.engine, "photo.png", "image", pngBytes)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", w.Code)
	}
	// 行没落库:库里无行、卷上无孤儿字节。
	if len(env.store.assets) != 0 {
		t.Errorf("store has %d rows, want none", len(env.store.assets))
	}
	if files := volumeFiles(t, env.storage.Root); len(files) != 0 {
		t.Errorf("orphan objects left behind: %v", files)
	}
}

// volumeFiles lists every file under the storage root:空卷 = 无孤儿对象。
func volumeFiles(t *testing.T, root string) []string {
	t.Helper()
	var out []string
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			out = append(out, path)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk storage root: %v", err)
	}
	return out
}
