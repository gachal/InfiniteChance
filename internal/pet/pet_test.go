package pet_test

import (
	"bytes"
	"context"
	"encoding/json"
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

	"github.com/gachal/InfiniteChance/internal/pet"
	"github.com/gachal/InfiniteChance/internal/settings"
)

// ---- 假件:settings KV 与对象存储,测试不碰数据库与真卷 ----

type fakeSettings struct {
	mu   sync.Mutex
	rows map[string]settings.Setting
}

func newFakeSettings() *fakeSettings {
	return &fakeSettings{rows: map[string]settings.Setting{}}
}

func (s *fakeSettings) Get(_ context.Context, name string) (settings.Setting, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	row, ok := s.rows[name]
	if !ok {
		return settings.Setting{}, settings.ErrNotFound
	}
	return row, nil
}

func (s *fakeSettings) Put(_ context.Context, name string, value []byte) (settings.Setting, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	row := settings.Setting{Name: name, Value: json.RawMessage(value)}
	s.rows[name] = row
	return row, nil
}

type fakeStorage struct {
	mu      sync.Mutex
	objects map[string][]byte
}

func newFakeStorage() *fakeStorage {
	return &fakeStorage{objects: map[string][]byte{}}
}

func (s *fakeStorage) Put(_ context.Context, key string, r io.Reader, size int64, _ string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	raw, err := io.ReadAll(io.LimitReader(r, size))
	if err != nil {
		return err
	}
	if int64(len(raw)) != size {
		return io.ErrShortWrite
	}
	s.objects[key] = raw
	return nil
}

func (s *fakeStorage) Open(_ context.Context, key string) (io.ReadCloser, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	raw, ok := s.objects[key]
	if !ok {
		return nil, fmt.Errorf("objectstore: object %q: %w", key, fs.ErrNotExist)
	}
	return io.NopCloser(bytes.NewReader(raw)), nil
}

func (s *fakeStorage) Delete(_ context.Context, key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.objects, key)
	return nil
}

func (s *fakeStorage) ThumbnailParam(context.Context, int) string { return "" }

// ---- 测试环境:与生产同一挂载形状(admin 组 + /pet 组),测试路由不挂
// JWT 中间件(鉴权是 wiring 的职责,handler 只管语义)。 ----

type petEnv struct {
	engine  *gin.Engine
	store   *fakeSettings
	storage *fakeStorage
}

func newPetEnv() petEnv {
	gin.SetMode(gin.TestMode)
	store := newFakeSettings()
	storage := newFakeStorage()
	h := &pet.Handlers{Config: settings.NewPetReader(store), Storage: storage}
	engine := gin.New()
	pet.RegisterAdminRoutes(engine.Group("/admin"), h)
	pet.RegisterConfigRoutes(engine.Group("/pet"), h)
	pet.RegisterObjectRoutes(engine.Group("/pet"), h)
	return petEnv{engine: engine, store: store, storage: storage}
}

// postImage 构造一次 multipart 形象上传(filename 只做载体,服务端不信任它)。
func postImage(t *testing.T, env petEnv, filename string, content []byte) *httptest.ResponseRecorder {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
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
	req := httptest.NewRequest(http.MethodPost, "/admin/settings/pet/image", &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	w := httptest.NewRecorder()
	env.engine.ServeHTTP(w, req)
	return w
}

// 最小魔数样本:GIF89a / RIFF-WEBP / PNG(带 acTL chunk 的 APNG 形状 ——
// 嗅探按 PNG 过,票面「APNG 随 PNG 嗅探天然可用」)。JPEG 是静态图,桌宠
// 形象白名单不收。
var (
	gifBytes  = append([]byte("GIF89a"), bytes.Repeat([]byte{0x21}, 48)...)
	webpBytes = append([]byte("RIFF\x24\x08\x00\x00WEBPVP8 "), bytes.Repeat([]byte{0x22}, 48)...)
	apngBytes = append(
		[]byte("\x89PNG\r\n\x1a\n\x00\x00\x00\racTL\x00\x00\x00\x02\x00\x00\x00\x00"),
		bytes.Repeat([]byte{0x42}, 48)...,
	)
	jpegBytes = append([]byte("\xff\xd8\xff\xe0"), bytes.Repeat([]byte{0x33}, 48)...)
	textBytes = []byte("just a text file, not an image at all")
)

type petImageJSON struct {
	Key         string `json:"key"`
	ContentType string `json:"content_type"`
	SizeBytes   int64  `json:"size_bytes"`
}

func decodePetImage(t *testing.T, w *httptest.ResponseRecorder) petImageJSON {
	t.Helper()
	var resp struct {
		Image petImageJSON `json:"image"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("response not JSON: %v (body: %s)", err, w.Body.String())
	}
	return resp.Image
}

// 魔数嗅探钉测(票面验证点):GIF/WebP/APNG-PNG 三族过嗅探,扩展名随
// 嗅探内容落,键带 uploads/ 前缀;字节真落进对象存储。
func TestUploadPetImageSniffsMagicAndLandsUploadsPrefix(t *testing.T) {
	env := newPetEnv()
	cases := []struct {
		name        string
		filename    string // 故意给假扩展名:类型由魔数裁决,声明不受信
		content     []byte
		wantType    string
		wantExt     string
	}{
		{"GIF 动图", "x.txt", gifBytes, "image/gif", ".gif"},
		{"WebP 动图", "y.bin", webpBytes, "image/webp", ".webp"},
		{"APNG(按 PNG 嗅探)", "z.dat", apngBytes, "image/png", ".png"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := postImage(t, env, tc.filename, tc.content)
			if w.Code != http.StatusCreated {
				t.Fatalf("HTTP %d: %s", w.Code, w.Body.String())
			}
			img := decodePetImage(t, w)
			if !strings.HasPrefix(img.Key, "uploads/") {
				t.Fatalf("key %q missing uploads/ prefix", img.Key)
			}
			if !strings.HasSuffix(img.Key, tc.wantExt) {
				t.Fatalf("key %q should end with %s", img.Key, tc.wantExt)
			}
			if img.ContentType != tc.wantType {
				t.Fatalf("content_type = %q, want %q", img.ContentType, tc.wantType)
			}
			if img.SizeBytes != int64(len(tc.content)) {
				t.Fatalf("size = %d, want %d", img.SizeBytes, len(tc.content))
			}
			stored, ok := env.storage.objects[img.Key]
			if !ok {
				t.Fatalf("object %q not landed in storage", img.Key)
			}
			if !bytes.Equal(stored, tc.content) {
				t.Fatalf("stored bytes diverge from upload")
			}
		})
	}
}

// 白名单外拒绝:JPEG 静态图、不认识的文本、视频都不收,错误码
// file_type_mismatch(与素材上传同码)。
func TestUploadPetImageRejectsOutOfWhitelist(t *testing.T) {
	env := newPetEnv()
	cases := []struct {
		name    string
		content []byte
	}{
		{"JPEG 静态图", jpegBytes},
		{"未知文本", textBytes},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := postImage(t, env, "pet.gif", tc.content)
			if w.Code != http.StatusBadRequest {
				t.Fatalf("HTTP %d: %s, want 400", w.Code, w.Body.String())
			}
			var resp struct {
				Error struct{ Code string } `json:"error"`
			}
			if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
				t.Fatalf("error body not JSON: %v", err)
			}
			if resp.Error.Code != "file_type_mismatch" {
				t.Fatalf("error code = %q, want file_type_mismatch", resp.Error.Code)
			}
		})
	}
}

// 桌宠对象读端点只认配置登记过的键:已登记键回字节与嗅探出的 Content-Type;
// 未登记键(哪怕是合法 uploads/ 键)与穿越键一律 404 —— 防遍历(票面裁量,
// 读端点故意不挂 JWT,<img> 带不了 Authorization,AGENTS 硬性约束 5 同款理由)。
func TestPetObjectServesOnlyRegisteredKeys(t *testing.T) {
	env := newPetEnv()

	// 上传一张,拿到键,再把键登记进 pet 配置。
	w := postImage(t, env, "idle.gif", gifBytes)
	if w.Code != http.StatusCreated {
		t.Fatalf("upload: HTTP %d: %s", w.Code, w.Body.String())
	}
	img := decodePetImage(t, w)
	cfg := map[string]any{"idle_image": img.Key, "chips": []string{}}
	raw, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("marshal cfg: %v", err)
	}
	if _, err := env.store.Put(t.Context(), settings.NamePet, raw); err != nil {
		t.Fatalf("seed pet row: %v", err)
	}

	// 已登记键:200,内容类型嗅探为 image/gif,字节一致。
	w2 := env.do(t, http.MethodGet, "/pet/object?key="+img.Key)
	if w2.Code != http.StatusOK {
		t.Fatalf("GET object: HTTP %d: %s", w2.Code, w2.Body.String())
	}
	if ct := w2.Header().Get("Content-Type"); ct != "image/gif" {
		t.Fatalf("Content-Type = %q, want image/gif", ct)
	}
	if !bytes.Equal(w2.Body.Bytes(), gifBytes) {
		t.Fatalf("served bytes diverge from stored object")
	}

	// 未登记键 404(即使它在存储里真实存在)。
	w3 := env.do(t, http.MethodGet, "/pet/object?key=uploads/20261119/ghost.gif")
	if w3.Code != http.StatusNotFound {
		t.Fatalf("unregistered key: HTTP %d, want 404", w3.Code)
	}
	// 穿越键 404。
	w4 := env.do(t, http.MethodGet, "/pet/object?key=uploads/../settings.sqlite")
	if w4.Code != http.StatusNotFound {
		t.Fatalf("traversal key: HTTP %d, want 404", w4.Code)
	}
	// 空键 400。
	w5 := env.do(t, http.MethodGet, "/pet/object")
	if w5.Code != http.StatusBadRequest {
		t.Fatalf("missing key: HTTP %d, want 400", w5.Code)
	}
}

// 画布侧配置端点:chips 恒为数组(空配置回 [] 不回 null),文案字段原样。
func TestGetPetConfigAnswersNormalizedShape(t *testing.T) {
	env := newPetEnv()

	// 未配置:全空缺省。
	w := env.do(t, http.MethodGet, "/pet/config")
	if w.Code != http.StatusOK {
		t.Fatalf("HTTP %d: %s", w.Code, w.Body.String())
	}
	var resp struct {
		Pet map[string]any `json:"pet"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if chips, ok := resp.Pet["chips"].([]any); !ok || len(chips) != 0 {
		t.Fatalf("default chips = %v, want empty array", resp.Pet["chips"])
	}

	// 配置后:原值回显。
	raw, _ := json.Marshal(map[string]any{
		"name": "绿毛球", "greeting": "嗨", "chips": []string{"a", "b"}, "model": "chat-main",
	})
	if _, err := env.store.Put(t.Context(), settings.NamePet, raw); err != nil {
		t.Fatalf("seed: %v", err)
	}
	w = env.do(t, http.MethodGet, "/pet/config")
	if w.Code != http.StatusOK {
		t.Fatalf("HTTP %d: %s", w.Code, w.Body.String())
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Pet["name"] != "绿毛球" || resp.Pet["model"] != "chat-main" {
		t.Fatalf("pet = %v", resp.Pet)
	}
}

func (env petEnv) do(t *testing.T, method, path string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, nil)
	w := httptest.NewRecorder()
	env.engine.ServeHTTP(w, req)
	return w
}
