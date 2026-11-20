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
	"time"

	"github.com/gin-gonic/gin"

	"github.com/gachal/InfiniteChance/internal/pet"
	"github.com/gachal/InfiniteChance/internal/settings"
)

// ---- 假件:settings KV 与对象存储,测试不碰数据库与真卷 ----

type fakeSettings struct {
	mu    sync.Mutex
	rows  map[string]settings.Setting
	stamp time.Time
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
	s.stamp = s.stamp.Add(time.Second)
	row := settings.Setting{Name: name, Value: json.RawMessage(value), UpdatedAt: s.stamp}
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
	h := &pet.Handlers{Store: store, Storage: storage}
	engine := gin.New()
	pet.RegisterAdminRoutes(engine.Group("/admin"), h)
	pet.RegisterConfigRoutes(engine.Group("/pet"), h)
	pet.RegisterObjectRoutes(engine.Group("/pet"), h)
	return petEnv{engine: engine, store: store, storage: storage}
}

// seedPetsDoc 把整份文档写进 settings 假件(绕过 REST 直铺库内状态)。
func seedPetsDoc(t *testing.T, env petEnv, doc settings.PetsDocument) {
	t.Helper()
	if err := settings.NormalizePetsDoc(&doc); err != nil {
		t.Fatalf("seed doc normalize: %v", err)
	}
	raw, err := json.Marshal(doc)
	if err != nil {
		t.Fatalf("marshal doc: %v", err)
	}
	if _, err := env.store.Put(t.Context(), settings.NamePets, raw); err != nil {
		t.Fatalf("seed pets row: %v", err)
	}
}

// getDoc 走 GET /admin/pets 解出整份文档。
func getDoc(t *testing.T, env petEnv) map[string]any {
	t.Helper()
	w := env.do(t, http.MethodGet, "/admin/pets")
	if w.Code != http.StatusOK {
		t.Fatalf("GET /admin/pets: HTTP %d: %s", w.Code, w.Body.String())
	}
	var parsed struct {
		Doc map[string]any `json:"doc"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &parsed); err != nil {
		t.Fatalf("decode doc: %v (body: %s)", err, w.Body.String())
	}
	return parsed.Doc
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

// doJSON 发一次带 JSON 体的请求。
func doJSON(t *testing.T, env petEnv, method, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal body: %v", err)
	}
	req := httptest.NewRequest(method, path, bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	env.engine.ServeHTTP(w, req)
	return w
}

// ---- 形象上传(92 号票语义不变:路径 /admin/settings/pet/image 不动)----

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

func uploadGif(t *testing.T, env petEnv) string {
	t.Helper()
	w := postImage(t, env, "idle.gif", gifBytes)
	if w.Code != http.StatusCreated {
		t.Fatalf("upload: HTTP %d: %s", w.Code, w.Body.String())
	}
	return decodePetImage(t, w).Key
}

func errCode(t *testing.T, w *httptest.ResponseRecorder) string {
	t.Helper()
	var resp struct {
		Error struct{ Code string } `json:"error"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("error body not JSON: %v", err)
	}
	return resp.Error.Code
}

// 魔数嗅探钉测(92 号票):GIF/WebP/APNG-PNG 三族过嗅探,扩展名随
// 嗅探内容落,键带 uploads/ 前缀;字节真落进对象存储。
func TestUploadPetImageSniffsMagicAndLandsUploadsPrefix(t *testing.T) {
	env := newPetEnv()
	cases := []struct {
		name     string
		filename string // 故意给假扩展名:类型由魔数裁决,声明不受信
		content  []byte
		wantType string
		wantExt  string
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

// 白名单外拒绝:JPEG 静态图、不认识的文本都不收,错误码
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
			if code := errCode(t, w); code != "file_type_mismatch" {
				t.Fatalf("error code = %q, want file_type_mismatch", code)
			}
		})
	}
}

// ---- 对象读端点(94 号票并集口径)----

// 桌宠对象读端点防遍历集合 = 全部桌宠登记键的并集(94 号票裁决:切 active
// 不 404 抖动、管理台预览未激活桌宠直接可用)。已登记键回字节与嗅探出的
// Content-Type;未登记键(哪怕在存储里真实存在)与穿越键一律 404。
func TestPetObjectServesUnionOfRegisteredKeys(t *testing.T) {
	env := newPetEnv()

	// 上传两张:一张登记在非 active 的自定义桌宠名下,另一张不登记。
	key := uploadGif(t, env)
	ghost := uploadGif(t, env)
	seedPetsDoc(t, env, settings.PetsDocument{
		ActiveID: settings.DefaultPetID,
		Pets: []settings.PetEntry{
			{ID: settings.DefaultPetID},
			{ID: "c1", PetConfig: settings.PetConfig{IdleImage: key}},
		},
	})

	// 非 active 桌宠登记的键:并集放行,200 + 嗅探 Content-Type + 字节一致。
	w2 := env.do(t, http.MethodGet, "/pet/object?key="+key)
	if w2.Code != http.StatusOK {
		t.Fatalf("GET object: HTTP %d: %s", w2.Code, w2.Body.String())
	}
	if ct := w2.Header().Get("Content-Type"); ct != "image/gif" {
		t.Fatalf("Content-Type = %q, want image/gif", ct)
	}
	if !bytes.Equal(w2.Body.Bytes(), gifBytes) {
		t.Fatalf("served bytes diverge from stored object")
	}
	if cc := w2.Header().Get("Cache-Control"); cc != "private, max-age=3600" {
		t.Fatalf("Cache-Control = %q", cc)
	}

	// 存储里存在但任何桌宠都没登记的键:404(不给遍历探针回声)。
	w3 := env.do(t, http.MethodGet, "/pet/object?key="+ghost)
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

// ---- 配置读端点(active 投影,契约钉测:形状与 92 号票逐字段一致)----

func TestGetPetConfigAnswersActiveProjection(t *testing.T) {
	env := newPetEnv()

	// 缺文档:绿毛球 active,全空缺省,chips 恒数组。
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
	if resp.Pet["idle_image"] != "" || resp.Pet["interactive_image"] != "" {
		t.Fatalf("default projection must be image-free: %v", resp.Pet)
	}

	// active 指向自定义桌宠:投影它的文案与形象。
	key := uploadGif(t, env)
	seedPetsDoc(t, env, settings.PetsDocument{
		ActiveID: "c1",
		Pets: []settings.PetEntry{
			{ID: settings.DefaultPetID, PetConfig: settings.PetConfig{Name: "绿毛球", Greeting: "默认问候"}},
			{ID: "c1", PetConfig: settings.PetConfig{Name: "皮皮", Greeting: "自定义问候", Chips: []string{"a"}, Model: "chat-x", IdleImage: key}},
		},
	})
	w = env.do(t, http.MethodGet, "/pet/config")
	if w.Code != http.StatusOK {
		t.Fatalf("HTTP %d: %s", w.Code, w.Body.String())
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Pet["name"] != "皮皮" || resp.Pet["greeting"] != "自定义问候" || resp.Pet["model"] != "chat-x" || resp.Pet["idle_image"] != key {
		t.Fatalf("active projection = %v, want c1 fields", resp.Pet)
	}
	if chips, ok := resp.Pet["chips"].([]any); !ok || len(chips) != 1 {
		t.Fatalf("chips = %v, want one entry array", resp.Pet["chips"])
	}
}

// ---- admin REST 五端点(94 号票)----

// GET 缺行回答缺省文档(绿毛球单实体,列表恒首位);惰性迁移已落库,
// updated_at 随迁移写入带上时戳。
func TestListPetsAnswersDefaultDocOnFreshStore(t *testing.T) {
	env := newPetEnv()
	doc := getDoc(t, env)
	if doc["active_id"] != settings.DefaultPetID {
		t.Fatalf("active = %v, want default", doc["active_id"])
	}
	pets, _ := doc["pets"].([]any)
	if len(pets) != 1 {
		t.Fatalf("pets = %v, want single default entry", doc["pets"])
	}
	first, _ := pets[0].(map[string]any)
	if first["id"] != settings.DefaultPetID {
		t.Fatalf("first entry = %v, want default", first)
	}
	if doc["updated_at"] == "" {
		t.Fatalf("updated_at should carry the migration write stamp")
	}
}

// POST 新建自定义桌宠:服务端生成 uuid id,绿毛球恒居首位,缺省不偏移 active。
func TestCreatePetAddsCustomEntry(t *testing.T) {
	env := newPetEnv()
	key := uploadGif(t, env)
	w := doJSON(t, env, http.MethodPost, "/admin/pets", map[string]any{
		"name":       " 皮皮 ",
		"greeting":   "你好",
		"chips":      []string{" 画个球 "},
		"model":      "chat-x",
		"idle_image": key,
	})
	if w.Code != http.StatusCreated {
		t.Fatalf("POST: HTTP %d: %s", w.Code, w.Body.String())
	}
	var resp struct {
		Pet map[string]any `json:"pet"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	id, _ := resp.Pet["id"].(string)
	if id == "" || id == settings.DefaultPetID {
		t.Fatalf("server must generate a uuid id, got %q", id)
	}
	if resp.Pet["name"] != "皮皮" || resp.Pet["idle_image"] != key {
		t.Fatalf("created entry = %v", resp.Pet)
	}

	// 回读:两 entry,default 恒首,active 仍为 default。
	doc := getDoc(t, env)
	if doc["active_id"] != settings.DefaultPetID {
		t.Fatalf("active moved on create: %v", doc["active_id"])
	}
	pets, _ := doc["pets"].([]any)
	if len(pets) != 2 {
		t.Fatalf("pets = %v, want two entries", pets)
	}
	first, _ := pets[0].(map[string]any)
	second, _ := pets[1].(map[string]any)
	if first["id"] != settings.DefaultPetID || second["id"] != id {
		t.Fatalf("order drifted: %v %v", first, second)
	}
	if doc["updated_at"] == "" {
		t.Fatalf("updated_at should be stamped after write")
	}
}

// POST 校验沿用:非法形象键拒;上限 21 只(default + 20 自定义)后 400。
func TestCreatePetRejectsInvalidAndOverLimit(t *testing.T) {
	env := newPetEnv()
	w := doJSON(t, env, http.MethodPost, "/admin/pets", map[string]any{
		"name":       "坏键",
		"idle_image": "canvases/1/2/x.png",
	})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("invalid key: HTTP %d: %s, want 400", w.Code, w.Body.String())
	}

	doc := settings.DefaultPetsDocument()
	for i := 0; i < settings.MaxCustomPets; i++ {
		doc.Pets = append(doc.Pets, settings.PetEntry{ID: fmt.Sprintf("c%02d", i)})
	}
	seedPetsDoc(t, env, doc)
	w = doJSON(t, env, http.MethodPost, "/admin/pets", map[string]any{"name": "第 21 只"})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("over limit: HTTP %d: %s, want 400", w.Code, w.Body.String())
	}
}

// PUT 单只合并语义(照 92 先例:未携带沿用已存,显式空串/空数组清空);
// 未知 id 404。
func TestUpdatePetMergesPointerFields(t *testing.T) {
	env := newPetEnv()
	w := doJSON(t, env, http.MethodPost, "/admin/pets", map[string]any{
		"name": "皮皮", "greeting": "旧问候", "chips": []string{"a"}, "model": "chat-x", "idle_image": uploadGif(t, env),
	})
	var resp struct {
		Pet map[string]any `json:"pet"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	id, _ := resp.Pet["id"].(string)

	// 只带 greeting 与 chips:其余沿用。
	w = doJSON(t, env, http.MethodPut, "/admin/pets/"+id, map[string]any{
		"greeting": "新问候",
		"chips":    []string{"b"},
	})
	if w.Code != http.StatusOK {
		t.Fatalf("PUT: HTTP %d: %s", w.Code, w.Body.String())
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Pet["name"] != "皮皮" || resp.Pet["model"] != "chat-x" || resp.Pet["idle_image"] == "" {
		t.Fatalf("absent fields must keep stored values: %v", resp.Pet)
	}
	if resp.Pet["greeting"] != "新问候" {
		t.Fatalf("greeting = %v, want replaced", resp.Pet["greeting"])
	}

	// 显式空串/空数组 = 清空。
	w = doJSON(t, env, http.MethodPut, "/admin/pets/"+id, map[string]any{
		"idle_image": "",
		"chips":      []string{},
	})
	if w.Code != http.StatusOK {
		t.Fatalf("PUT: HTTP %d: %s", w.Code, w.Body.String())
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Pet["idle_image"] != "" {
		t.Fatalf("idle_image = %v, want cleared", resp.Pet["idle_image"])
	}
	if chips, ok := resp.Pet["chips"].([]any); !ok || len(chips) != 0 {
		t.Fatalf("chips = %v, want cleared", resp.Pet["chips"])
	}

	// 未知 id 404。
	if w := doJSON(t, env, http.MethodPut, "/admin/pets/ghost", map[string]any{"name": "x"}); w.Code != http.StatusNotFound {
		t.Fatalf("unknown id: HTTP %d, want 404", w.Code)
	}
}

// default 表单:文案字段照常可编辑;形象键拒绝(服务端拒为其设形象)。
func TestUpdateDefaultPetTextOnly(t *testing.T) {
	env := newPetEnv()

	w := doJSON(t, env, http.MethodPut, "/admin/pets/"+settings.DefaultPetID, map[string]any{
		"name":       "球球",
		"idle_image": uploadGif(t, env),
	})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("default with image key: HTTP %d: %s, want 400", w.Code, w.Body.String())
	}

	w = doJSON(t, env, http.MethodPut, "/admin/pets/"+settings.DefaultPetID, map[string]any{
		"name":     "球球",
		"greeting": "默认问候",
		"chips":    []string{"画一个角色"},
	})
	if w.Code != http.StatusOK {
		t.Fatalf("default text PUT: HTTP %d: %s", w.Code, w.Body.String())
	}
	doc := getDoc(t, env)
	pets, _ := doc["pets"].([]any)
	first, _ := pets[0].(map[string]any)
	if first["name"] != "球球" || first["idle_image"] != "" {
		t.Fatalf("default entry = %v, want text updated and images empty", first)
	}
}

// DELETE:default 拒 409;删自定义物理回收其登记对象;删 active 回落 default;
// 未知 id 404。
func TestDeletePetRecyclesObjectsAndFallsBackActive(t *testing.T) {
	env := newPetEnv()
	keyA, keyB := uploadGif(t, env), uploadGif(t, env)

	w := doJSON(t, env, http.MethodPost, "/admin/pets", map[string]any{"name": "甲", "idle_image": keyA})
	var created struct {
		Pet map[string]any `json:"pet"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode: %v", err)
	}
	idA, _ := created.Pet["id"].(string)
	w = doJSON(t, env, http.MethodPost, "/admin/pets", map[string]any{"name": "乙", "idle_image": keyB})
	if err := json.Unmarshal(w.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode: %v", err)
	}
	idB, _ := created.Pet["id"].(string)

	// default 不可删:409。
	if w := env.do(t, http.MethodDelete, "/admin/pets/"+settings.DefaultPetID); w.Code != http.StatusConflict {
		t.Fatalf("delete default: HTTP %d: %s, want 409", w.Code, w.Body.String())
	}
	// 未知 id:404。
	if w := env.do(t, http.MethodDelete, "/admin/pets/ghost"); w.Code != http.StatusNotFound {
		t.Fatalf("delete unknown: HTTP %d, want 404", w.Code)
	}

	// 删 active 桌宠(乙):204,对象物理回收,active 回落 default。
	if w := doJSON(t, env, http.MethodPut, "/admin/pets/active", map[string]any{"id": idB}); w.Code != http.StatusOK {
		t.Fatalf("set active: HTTP %d: %s", w.Code, w.Body.String())
	}
	if w := env.do(t, http.MethodDelete, "/admin/pets/"+idB); w.Code != http.StatusNoContent {
		t.Fatalf("delete custom: HTTP %d: %s, want 204", w.Code, w.Body.String())
	}
	if _, ok := env.storage.objects[keyB]; ok {
		t.Fatalf("object %q must be physically recycled", keyB)
	}
	doc := getDoc(t, env)
	if doc["active_id"] != settings.DefaultPetID {
		t.Fatalf("active = %v, want fallback to default", doc["active_id"])
	}
	pets, _ := doc["pets"].([]any)
	if len(pets) != 2 {
		t.Fatalf("pets = %v, want default + 甲", pets)
	}
	// 甲的对象仍在(未删它)。
	if _, ok := env.storage.objects[keyA]; !ok {
		t.Fatalf("object %q must survive", keyA)
	}

	// 删甲:对象回收。
	if w := env.do(t, http.MethodDelete, "/admin/pets/"+idA); w.Code != http.StatusNoContent {
		t.Fatalf("delete custom: HTTP %d, want 204", w.Code)
	}
	if _, ok := env.storage.objects[keyA]; ok {
		t.Fatalf("object %q must be physically recycled", keyA)
	}
}

// 无共享不回收:同一对象键被另一只桌宠登记时,DELETE 只删行不删对象
// (键去重纪律:按文档内引用计数)。
func TestDeletePetKeepsObjectReferencedBySibling(t *testing.T) {
	env := newPetEnv()
	key := uploadGif(t, env)

	w := doJSON(t, env, http.MethodPost, "/admin/pets", map[string]any{"name": "甲", "idle_image": key})
	var created struct {
		Pet map[string]any `json:"pet"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode: %v", err)
	}
	idA, _ := created.Pet["id"].(string)
	// 乙复用甲的键(登记键是本人上传系统资产,规范键即放行)。
	doJSON(t, env, http.MethodPost, "/admin/pets", map[string]any{"name": "乙", "interactive_image": key})

	if w := env.do(t, http.MethodDelete, "/admin/pets/"+idA); w.Code != http.StatusNoContent {
		t.Fatalf("delete: HTTP %d, want 204", w.Code)
	}
	if _, ok := env.storage.objects[key]; !ok {
		t.Fatalf("object %q still referenced by sibling, must survive", key)
	}
}

// PUT /admin/pets/active 切换当前桌宠;空 id 400、失靶 404。
func TestSetActivePetSwitchesPointer(t *testing.T) {
	env := newPetEnv()
	w := doJSON(t, env, http.MethodPost, "/admin/pets", map[string]any{"name": "皮皮"})
	var created struct {
		Pet map[string]any `json:"pet"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode: %v", err)
	}
	id, _ := created.Pet["id"].(string)

	w = doJSON(t, env, http.MethodPut, "/admin/pets/active", map[string]any{"id": id})
	if w.Code != http.StatusOK {
		t.Fatalf("set active: HTTP %d: %s", w.Code, w.Body.String())
	}
	doc := getDoc(t, env)
	if doc["active_id"] != id {
		t.Fatalf("active = %v, want %q", doc["active_id"], id)
	}

	if w := doJSON(t, env, http.MethodPut, "/admin/pets/active", map[string]any{"id": ""}); w.Code != http.StatusBadRequest {
		t.Fatalf("empty id: HTTP %d, want 400", w.Code)
	}
	if w := doJSON(t, env, http.MethodPut, "/admin/pets/active", map[string]any{"id": "ghost"}); w.Code != http.StatusNotFound {
		t.Fatalf("dangling id: HTTP %d, want 404", w.Code)
	}
}

func (env petEnv) do(t *testing.T, method, path string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, nil)
	w := httptest.NewRecorder()
	env.engine.ServeHTTP(w, req)
	return w
}
