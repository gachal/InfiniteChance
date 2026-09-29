package settings_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/gachal/InfiniteChance/internal/settings"
)

// fakeStore is an in-memory Store; handler tests never touch a database.
type fakeStore struct {
	mu    sync.Mutex
	rows  map[string]settings.Setting
	stamp time.Time
}

func newFakeStore() *fakeStore {
	return &fakeStore{rows: map[string]settings.Setting{}}
}

func (s *fakeStore) Get(_ context.Context, name string) (settings.Setting, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	row, ok := s.rows[name]
	if !ok {
		return settings.Setting{}, settings.ErrNotFound
	}
	return row, nil
}

func (s *fakeStore) Put(_ context.Context, name string, value []byte) (settings.Setting, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.stamp = s.stamp.Add(time.Second)
	row := settings.Setting{Name: name, Value: json.RawMessage(value), UpdatedAt: s.stamp}
	s.rows[name] = row
	return row, nil
}

type handlerEnv struct {
	engine *gin.Engine
	store  *fakeStore
}

func newHandlerEnv() handlerEnv {
	gin.SetMode(gin.TestMode)
	store := newFakeStore()
	engine := gin.New()
	settings.RegisterAdminRoutes(engine.Group("/admin"), &settings.Handlers{Store: store})
	return handlerEnv{engine: engine, store: store}
}

func (env handlerEnv) do(t *testing.T, method, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal body: %v", err)
		}
		reader = strings.NewReader(string(raw))
	}
	w := httptest.NewRecorder()
	req := httptest.NewRequest(method, path, reader)
	env.engine.ServeHTTP(w, req)
	return w
}

func decodeStorage(t *testing.T, w *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	if w.Code != http.StatusOK {
		t.Fatalf("HTTP %d: %s", w.Code, w.Body.String())
	}
	var body struct {
		Storage map[string]any `json:"storage"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	return body.Storage
}

func ossField(t *testing.T, storage map[string]any) map[string]any {
	t.Helper()
	oss, ok := storage["oss"].(map[string]any)
	if !ok {
		t.Fatalf("response has no oss object: %v", storage)
	}
	return oss
}

// 零配置起步:GET 对缺行回答 local 缺省,且绝不出现任何密钥字段。
func TestGetStorageMissingRowAnswersLocalDefault(t *testing.T) {
	env := newHandlerEnv()

	storage := decodeStorage(t, env.do(t, http.MethodGet, "/admin/settings/storage", nil))
	if storage["driver"] != "local" {
		t.Fatalf("driver = %v, want local", storage["driver"])
	}
	if storage["updated_at"] != "" {
		t.Fatalf("updated_at = %v, want empty", storage["updated_at"])
	}
	for _, key := range []string{"access_key", "secret_key"} {
		if _, ok := ossField(t, storage)[key]; ok {
			t.Fatalf("response leaks %q", key)
		}
	}
}

// 完整 PUT 落库;GET 只回 has_* 与尾 4 位提示(渠道密钥同款)。
func TestPutOSSRoundTripMasksCredentials(t *testing.T) {
	env := newHandlerEnv()

	body := map[string]any{
		"driver": "oss",
		"oss": map[string]any{
			"endpoint":        "oss-cn-hangzhou.aliyuncs.com",
			"bucket":          "infinitechance",
			"public_base_url": "https://infinitechance.oss-cn-hangzhou.aliyuncs.com",
			"access_key":      "LTAI5tEXAMPLEKEY9ab",
			"secret_key":      "Secr3tEXAMPLEwxyz",
		},
	}
	storage := decodeStorage(t, env.do(t, http.MethodPut, "/admin/settings/storage", body))
	if storage["driver"] != "oss" {
		t.Fatalf("driver = %v, want oss", storage["driver"])
	}
	oss := ossField(t, storage)
	if oss["has_access_key"] != true || oss["has_secret"] != true {
		t.Fatalf("has_* flags wrong: %v", oss)
	}
	if hint, _ := oss["access_key_hint"].(string); !strings.HasSuffix(hint, "z9ab") && !strings.HasSuffix(hint, "9ab") {
		t.Fatalf("access_key_hint = %q, want tail-4 of the key", hint)
	}
	if hint, _ := oss["secret_hint"].(string); !strings.HasSuffix(hint, "wxyz") {
		t.Fatalf("secret_hint = %q, want tail-4 of the secret", hint)
	}
	if v, _ := oss["endpoint"].(string); v != "oss-cn-hangzhou.aliyuncs.com" {
		t.Fatalf("endpoint = %v", v)
	}

	// 行里存的是完整密钥(驱动要用),只是不出 wire。
	row, err := env.store.Get(context.Background(), settings.NameStorage)
	if err != nil {
		t.Fatal(err)
	}
	var cfg settings.StorageConfig
	if err := json.Unmarshal(row.Value, &cfg); err != nil {
		t.Fatal(err)
	}
	if cfg.OSS == nil || cfg.OSS.AccessKey != "LTAI5tEXAMPLEKEY9ab" || cfg.OSS.SecretKey != "Secr3tEXAMPLEwxyz" {
		t.Fatalf("stored row lost credentials: %+v", cfg.OSS)
	}
}

// PUT 空 AK/SK = 保留原密(渠道密钥同款);其余字段照常替换。
func TestPutEmptySecretsKeepStoredOnes(t *testing.T) {
	env := newHandlerEnv()
	env.do(t, http.MethodPut, "/admin/settings/storage", map[string]any{
		"driver": "oss",
		"oss": map[string]any{
			"endpoint": "e1", "bucket": "b1",
			"access_key": "AK1111aaaa", "secret_key": "SK1111bbbb",
		},
	})

	storage := decodeStorage(t, env.do(t, http.MethodPut, "/admin/settings/storage", map[string]any{
		"driver": "oss",
		"oss": map[string]any{
			"endpoint": "e2", "bucket": "b2",
			"access_key": "", "secret_key": "",
		},
	}))
	oss := ossField(t, storage)
	if v, _ := oss["endpoint"].(string); v != "e2" {
		t.Fatalf("endpoint = %v, want e2", v)
	}
	if hint, _ := oss["access_key_hint"].(string); !strings.HasSuffix(hint, "aaaa") {
		t.Fatalf("access_key_hint = %q, want kept secret's tail", hint)
	}
	if hint, _ := oss["secret_hint"].(string); !strings.HasSuffix(hint, "bbbb") {
		t.Fatalf("secret_hint = %q, want kept secret's tail", hint)
	}
}

// 半套密钥没有意义:只给一把另一把留空 → 400。
func TestPutHalfCredentialRejected(t *testing.T) {
	env := newHandlerEnv()

	w := env.do(t, http.MethodPut, "/admin/settings/storage", map[string]any{
		"driver": "oss",
		"oss": map[string]any{
			"endpoint": "e", "bucket": "b",
			"access_key": "AK", "secret_key": "",
		},
	})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("HTTP %d, want 400", w.Code)
	}
}

// 启用 OSS 而连接不完整(无既有密钥可保留)→ 400;「启用 OSS 但没有可用
// 连接」的行进不了库。
func TestPutOSSWithoutCompleteConnectionRejected(t *testing.T) {
	env := newHandlerEnv()

	for name, body := range map[string]any{
		"no oss block": map[string]any{"driver": "oss"},
		"no credentials": map[string]any{
			"driver": "oss",
			"oss":    map[string]any{"endpoint": "e", "bucket": "b"},
		},
		"no endpoint": map[string]any{
			"driver": "oss",
			"oss":    map[string]any{"bucket": "b", "access_key": "AK", "secret_key": "SK"},
		},
		"bad driver": map[string]any{"driver": "minio"},
		"bad public base": map[string]any{
			"driver": "oss",
			"oss": map[string]any{
				"endpoint": "e", "bucket": "b",
				"access_key": "AK", "secret_key": "SK",
				"public_base_url": "ftp://x",
			},
		},
	} {
		w := env.do(t, http.MethodPut, "/admin/settings/storage", body)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("%s: HTTP %d (%s), want 400", name, w.Code, w.Body.String())
		}
	}
}

// 切回 local 不必清空 OSS 连接:空 endpoint/bucket/AK/SK 全部沿用已存
// (惰性配置,只有 public_base_url 是「空 = 显式停用」)。
func TestPutLocalKeepsInertOSSBlock(t *testing.T) {
	env := newHandlerEnv()
	env.do(t, http.MethodPut, "/admin/settings/storage", map[string]any{
		"driver": "oss",
		"oss": map[string]any{
			"endpoint": "e", "bucket": "b",
			"access_key": "AK1234aaaa", "secret_key": "SK1234bbbb",
		},
	})

	storage := decodeStorage(t, env.do(t, http.MethodPut, "/admin/settings/storage", map[string]any{
		"driver": "local",
		"oss": map[string]any{
			"endpoint": "", "bucket": "",
			"access_key": "", "secret_key": "",
		},
	}))
	if storage["driver"] != "local" {
		t.Fatalf("driver = %v, want local", storage["driver"])
	}
	oss := ossField(t, storage)
	if v, _ := oss["endpoint"].(string); v != "e" {
		t.Fatalf("endpoint = %q, want kept e", v)
	}
	if v, _ := oss["bucket"].(string); v != "b" {
		t.Fatalf("bucket = %q, want kept b", v)
	}
	if !oss["has_access_key"].(bool) || !oss["has_secret"].(bool) {
		t.Fatalf("credentials lost on local switch: %v", oss)
	}
}

// 不带 oss 块的 PUT = 只切驱动,连接原样 —— 靠已存完整连接切回 oss 是
// 一次 PUT 的事。
func TestPutDriverFlipWithoutOSSBlock(t *testing.T) {
	env := newHandlerEnv()
	env.do(t, http.MethodPut, "/admin/settings/storage", map[string]any{
		"driver": "oss",
		"oss": map[string]any{
			"endpoint": "e", "bucket": "b",
			"access_key": "AK1234aaaa", "secret_key": "SK1234bbbb",
		},
	})

	storage := decodeStorage(t, env.do(t, http.MethodPut, "/admin/settings/storage",
		map[string]any{"driver": "local"}))
	if storage["driver"] != "local" {
		t.Fatalf("driver = %v, want local", storage["driver"])
	}
	storage = decodeStorage(t, env.do(t, http.MethodPut, "/admin/settings/storage",
		map[string]any{"driver": "oss"}))
	if storage["driver"] != "oss" {
		t.Fatalf("driver = %v, want oss (stored connection reused)", storage["driver"])
	}
	oss := ossField(t, storage)
	if v, _ := oss["endpoint"].(string); v != "e" || !oss["has_secret"].(bool) {
		t.Fatalf("stored connection not reused: %v", oss)
	}
}

// 语义层:EffectiveDriver 只在连接完整时承认 oss —— 手改库写出的半行
// 配置读侧降级 local,而不是把转存打到不可用的连接上。
func TestEffectiveDriverDegradesIncompleteOSS(t *testing.T) {
	incomplete := settings.StorageConfig{Driver: settings.DriverOSS,
		OSS: &settings.OSSConfig{Endpoint: "e", Bucket: "b"}}
	if got := incomplete.EffectiveDriver(); got != settings.DriverLocal {
		t.Fatalf("incomplete row driver = %q, want local", got)
	}
	complete := settings.StorageConfig{Driver: settings.DriverOSS,
		OSS: &settings.OSSConfig{Endpoint: "e", Bucket: "b", AccessKey: "AK", SecretKey: "SK"}}
	if got := complete.EffectiveDriver(); got != settings.DriverOSS {
		t.Fatalf("complete row driver = %q, want oss", got)
	}
}

// 读取器:缺行/坏行 → local 缺省;好行按行回答 —— 画布侧按请求读表的
// 全部语义。
func TestNewStorageReaderDefaults(t *testing.T) {
	ctx := context.Background()
	store := newFakeStore()

	cfg, err := settings.NewStorageReader(store)(ctx)
	if err != nil || cfg.Driver != settings.DriverLocal {
		t.Fatalf("missing row: cfg %+v err %v, want local default", cfg, err)
	}

	// 坏 JSON:降级 local 而非报错(坏配置不至于让素材面瘫痪)。
	store.rows[settings.NameStorage] = settings.Setting{
		Name: settings.NameStorage, Value: json.RawMessage(`{not json`), UpdatedAt: time.Now(),
	}
	cfg, err = settings.NewStorageReader(store)(ctx)
	if err != nil || cfg.Driver != settings.DriverLocal {
		t.Fatalf("corrupt row: cfg %+v err %v, want local default", cfg, err)
	}

	raw, _ := json.Marshal(settings.StorageConfig{Driver: settings.DriverOSS,
		OSS: &settings.OSSConfig{Endpoint: "e", Bucket: "b", AccessKey: "AK", SecretKey: "SK"}})
	store.rows[settings.NameStorage] = settings.Setting{
		Name: settings.NameStorage, Value: raw, UpdatedAt: time.Now(),
	}
	cfg, err = settings.NewStorageReader(store)(ctx)
	if err != nil || cfg.EffectiveDriver() != settings.DriverOSS {
		t.Fatalf("good row: cfg %+v err %v, want oss", cfg, err)
	}
}

// 公网地址提供者(18 号票接缝 → 19 号票接线;23 号票起读活跃驱动块):
// 未配置恒空,活跃云驱动配置后原样回答;driver=local 时惰性云块的地址
// 不再冒充自有公网基座;nil Store 保持升级前行为(nil provider)。
func TestPublicBaseURLProvider(t *testing.T) {
	ctx := context.Background()
	if settings.PublicBaseURL(nil) != nil {
		t.Fatal("nil store should answer a nil provider")
	}

	store := newFakeStore()
	provider := settings.PublicBaseURL(store)
	if addr := provider(ctx); addr != "" {
		t.Fatalf("unconfigured base = %q, want empty", addr)
	}

	raw, _ := json.Marshal(settings.StorageConfig{Driver: settings.DriverOSS,
		OSS: &settings.OSSConfig{
			Endpoint: "oss-cn-hangzhou.aliyuncs.com", Bucket: "demo",
			AccessKey: "ak", SecretKey: "sk", PublicBaseURL: "https://cdn.example.com/base/",
		}})
	store.rows[settings.NameStorage] = settings.Setting{
		Name: settings.NameStorage, Value: raw, UpdatedAt: time.Now(),
	}
	if addr := provider(ctx); addr != "https://cdn.example.com/base/" {
		t.Fatalf("configured base = %q", addr)
	}

	// 23 号票语义:切回 local(oss 块惰性保留)后公网基座随之停用 ——
	// 本地卷的对象没有云侧公网地址可指。
	local, _ := json.Marshal(settings.StorageConfig{Driver: settings.DriverLocal,
		OSS: &settings.OSSConfig{
			Endpoint: "oss-cn-hangzhou.aliyuncs.com", Bucket: "demo",
			AccessKey: "ak", SecretKey: "sk", PublicBaseURL: "https://cdn.example.com/base/",
		}})
	store.rows[settings.NameStorage] = settings.Setting{
		Name: settings.NameStorage, Value: local, UpdatedAt: time.Now(),
	}
	if addr := provider(ctx); addr != "" {
		t.Fatalf("local driver with lazy oss block = %q, want empty", addr)
	}

	// cos 活跃时读 cos 块的基座(每驱动块各自携带)。
	cos, _ := json.Marshal(settings.StorageConfig{Driver: settings.DriverCOS,
		COS: &settings.COSConfig{
			Endpoint: "cos.ap-guangzhou.myqcloud.com", Bucket: "demo-1250000000",
			SecretID: "id", SecretKey: "sk", PublicBaseURL: "https://cos.example.com/base/",
		}})
	store.rows[settings.NameStorage] = settings.Setting{
		Name: settings.NameStorage, Value: cos, UpdatedAt: time.Now(),
	}
	if addr := provider(ctx); addr != "https://cos.example.com/base/" {
		t.Fatalf("cos base = %q", addr)
	}
}

func cosField(t *testing.T, storage map[string]any) map[string]any {
	t.Helper()
	cos, ok := storage["cos"].(map[string]any)
	if !ok {
		t.Fatalf("response has no cos object: %v", storage)
	}
	return cos
}

func putCOSBody(relayPersist any) map[string]any {
	body := map[string]any{
		"driver": "cos",
		"cos": map[string]any{
			"endpoint":        "cos.ap-guangzhou.myqcloud.com",
			"bucket":          "demo-1250000000",
			"public_base_url": "https://demo-1250000000.cos.ap-guangzhou.myqcloud.com",
			"secret_id":       "AKIDdemo",
			"secret_key":      "sk-demo",
		},
	}
	if relayPersist != nil {
		body["relay_persist"] = relayPersist
	}
	return body
}

// 23 号票:cos 块完整落库;GET 只回 has_* 与尾 4 位,relay_persist 透传。
func TestPutCOSRoundTripMasksCredentials(t *testing.T) {
	env := newHandlerEnv()

	storage := decodeStorage(t, env.do(t, http.MethodPut, "/admin/settings/storage", putCOSBody(true)))
	if storage["driver"] != "cos" || storage["relay_persist"] != true {
		t.Fatalf("driver/relay_persist = %v/%v, want cos/true", storage["driver"], storage["relay_persist"])
	}
	cos := cosField(t, storage)
	if cos["has_secret_id"] != true || cos["has_secret_key"] != true {
		t.Fatalf("has_* hints missing: %v", cos)
	}
	if cos["secret_id_hint"] != "…demo" || cos["secret_key_hint"] != "…demo" {
		t.Fatalf("tail hints = %v/%v", cos["secret_id_hint"], cos["secret_key_hint"])
	}
	for _, key := range []string{"secret_id", "secret_key"} {
		if _, ok := cos[key]; ok {
			t.Fatalf("response leaks %q", key)
		}
	}
}

// 半套密钥拒绝(oss 同款纪律)。
func TestPutCOSHalfSecretRejected(t *testing.T) {
	env := newHandlerEnv()

	body := putCOSBody(nil)
	body["cos"].(map[string]any)["secret_key"] = ""
	w := env.do(t, http.MethodPut, "/admin/settings/storage", body)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("HTTP %d: %s", w.Code, w.Body.String())
	}
}

// driver=cos 而无连接块/半截连接 → 400。
func TestPutCOSWithoutCompleteConnectionRejected(t *testing.T) {
	env := newHandlerEnv()

	w := env.do(t, http.MethodPut, "/admin/settings/storage", map[string]any{"driver": "cos"})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("no block: HTTP %d: %s", w.Code, w.Body.String())
	}

	body := putCOSBody(nil)
	blk := body["cos"].(map[string]any)
	blk["endpoint"] = ""
	blk["secret_id"] = ""
	blk["secret_key"] = ""
	w = env.do(t, http.MethodPut, "/admin/settings/storage", body)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("hollow block: HTTP %d: %s", w.Code, w.Body.String())
	}
}

// relay_persist 前置:local 拒;云驱动缺 public_base_url 拒;齐全放行。
func TestPutRelayPersistRequiresCloudDriverAndBase(t *testing.T) {
	env := newHandlerEnv()

	w := env.do(t, http.MethodPut, "/admin/settings/storage", map[string]any{"driver": "local", "relay_persist": true})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("local+persist: HTTP %d: %s", w.Code, w.Body.String())
	}

	noBase := putCOSBody(true)
	noBase["cos"].(map[string]any)["public_base_url"] = ""
	w = env.do(t, http.MethodPut, "/admin/settings/storage", noBase)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("cos without base: HTTP %d: %s", w.Code, w.Body.String())
	}

	// 齐全:先落一份带 base 的连接,再开开关也能过。
	if w := env.do(t, http.MethodPut, "/admin/settings/storage", putCOSBody(false)); w.Code != http.StatusOK {
		t.Fatalf("seed: HTTP %d: %s", w.Code, w.Body.String())
	}
	w = env.do(t, http.MethodPut, "/admin/settings/storage", map[string]any{"driver": "cos", "relay_persist": true})
	if w.Code != http.StatusOK {
		t.Fatalf("cos with base: HTTP %d: %s", w.Code, w.Body.String())
	}
}

// PUT 不带 relay_persist 字段 = 沿用已存开关(一次只切驱动不顺手关转存)。
func TestPutRelayPersistAbsentKeepsStored(t *testing.T) {
	env := newHandlerEnv()

	if w := env.do(t, http.MethodPut, "/admin/settings/storage", putCOSBody(true)); w.Code != http.StatusOK {
		t.Fatalf("seed: HTTP %d: %s", w.Code, w.Body.String())
	}
	storage := decodeStorage(t, env.do(t, http.MethodPut, "/admin/settings/storage", map[string]any{"driver": "cos"}))
	if storage["relay_persist"] != true {
		t.Fatalf("relay_persist = %v, want kept true", storage["relay_persist"])
	}
}

// 切回 local(字段缺省、已存 true)自动关转存 —— 行不允许 local+persist,
// 缺省继承不该把「切回本地卷」堵成 400;显式送 true 才拒绝(见上)。
func TestPutLocalAutoDisablesInheritedRelayPersist(t *testing.T) {
	env := newHandlerEnv()

	if w := env.do(t, http.MethodPut, "/admin/settings/storage", putCOSBody(true)); w.Code != http.StatusOK {
		t.Fatalf("seed: HTTP %d: %s", w.Code, w.Body.String())
	}
	storage := decodeStorage(t, env.do(t, http.MethodPut, "/admin/settings/storage", map[string]any{"driver": "local"}))
	if storage["driver"] != "local" || storage["relay_persist"] != false {
		t.Fatalf("flip to local = %v/%v, want local/false", storage["driver"], storage["relay_persist"])
	}
}
