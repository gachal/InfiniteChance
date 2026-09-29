package relay

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/gachal/InfiniteChance/internal/asset"
	"github.com/gachal/InfiniteChance/internal/objectstore"
	"github.com/gachal/InfiniteChance/internal/settings"
)

// fakeCloud is a CloudStore stub: it answers one frozen snapshot and backs
// puts with an in-memory map.
type fakeCloud struct {
	mu   sync.Mutex
	cfg  settings.StorageConfig
	objs map[string][]byte
	err  error
}

func newFakeCloud(cfg settings.StorageConfig) *fakeCloud {
	return &fakeCloud{cfg: cfg, objs: map[string][]byte{}}
}

func (f *fakeCloud) Cloud(context.Context) (objectstore.Store, settings.StorageConfig) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return nil, settings.DefaultStorageConfig()
	}
	if f.cfg.EffectiveDriver() == settings.DriverLocal {
		return nil, f.cfg
	}
	return f, f.cfg
}

func (f *fakeCloud) Put(_ context.Context, key string, r io.Reader, size int64, _ string) error {
	body, err := io.ReadAll(io.LimitReader(r, size))
	if err != nil {
		return err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.objs[key] = body
	return nil
}

func (f *fakeCloud) Open(_ context.Context, key string) (io.ReadCloser, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	body, ok := f.objs[key]
	if !ok {
		return nil, io.EOF
	}
	return io.NopCloser(strings.NewReader(string(body))), nil
}

func (f *fakeCloud) Delete(_ context.Context, key string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.objs, key)
	return nil
}

// fakeAssets records Create calls and can fail on demand.
type fakeAssets struct {
	mu      sync.Mutex
	rows    []asset.Asset
	failNow bool
}

func (f *fakeAssets) Create(_ context.Context, a asset.Asset) (asset.Asset, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.failNow {
		return asset.Asset{}, context.DeadlineExceeded
	}
	a.ID = int64(len(f.rows) + 1)
	f.rows = append(f.rows, a)
	return a, nil
}

func (f *fakeAssets) Get(context.Context, int64) (asset.Asset, error) {
	return asset.Asset{}, asset.ErrNotFound
}
func (f *fakeAssets) List(context.Context, asset.Filter) ([]asset.Listed, error) {
	return nil, nil
}
func (f *fakeAssets) Delete(context.Context, int64) error { return asset.ErrNotFound }

func cosSnapshot(base string, persist bool) settings.StorageConfig {
	return settings.StorageConfig{
		Driver:       settings.DriverCOS,
		RelayPersist: persist,
		COS: &settings.COSConfig{
			Endpoint: "cos.ap-guangzhou.myqcloud.com", Bucket: "demo-1250000000",
			SecretID: "ID", SecretKey: "SK", PublicBaseURL: base,
		},
	}
}

func imageServer(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write([]byte("pngbytes"))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func decodeDataURLs(t *testing.T, body []byte) []string {
	t.Helper()
	var env struct {
		Data []struct {
			URL string `json:"url"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &env); err != nil {
		t.Fatalf("decode body %s: %v", body, err)
	}
	urls := make([]string, 0, len(env.Data))
	for _, d := range env.Data {
		urls = append(urls, d.URL)
	}
	return urls
}

// 开着 relay_persist:url 形态的产物落桶、行落库、URL 回写为
// public_base + relay 键;b64 形态与未带 url 的项不动;顶层其余字段
// (created/model)保留。
func TestImagePersistRewritesURLs(t *testing.T) {
	img := imageServer(t)
	cloud := newFakeCloud(cosSnapshot("https://cdn.example.com", true))
	assets := &fakeAssets{}
	p := &ImagePersist{Cloud: cloud, Assets: assets}

	body := []byte(`{"created":1759152000,"model":"img-m","data":[{"url":"` + img.URL + `/a.png"},{"b64_json":"zzz"},{"url":"` + img.URL + `/b.png"}]}`)
	out := p.rewriteBody(context.Background(), body, "img-m", "a cat")

	urls := decodeDataURLs(t, out)
	if len(urls) != 3 {
		t.Fatalf("data items = %d, want 3", len(urls))
	}
	for _, u := range []string{urls[0], urls[2]} {
		if !strings.HasPrefix(u, "https://cdn.example.com/relay/") {
			t.Fatalf("url = %q, want durable relay address", u)
		}
	}
	if urls[1] != "" {
		t.Fatalf("b64 item should carry no url, got %q", urls[1])
	}

	if len(assets.rows) != 2 {
		t.Fatalf("asset rows = %d, want 2", len(assets.rows))
	}
	row := assets.rows[0]
	if row.Kind != asset.KindImage || row.Model != "img-m" || row.Prompt != "a cat" {
		t.Fatalf("row provenance = %+v", row)
	}
	if row.URL != urls[0] || row.ObjectKey == "" || row.ContentType != "image/png" || row.SizeBytes != 8 {
		t.Fatalf("row facts = %+v (url[0]=%q)", row, urls[0])
	}

	var env map[string]any
	if err := json.Unmarshal(out, &env); err != nil {
		t.Fatal(err)
	}
	if env["created"] != float64(1759152000) || env["model"] != "img-m" {
		t.Fatalf("envelope extras clobbered: %v", env)
	}
}

// 关着转存(或没有公网基座):原字节原样返回 —— key 顺序与空白不动。
func TestImagePersistOffReturnsBodyVerbatim(t *testing.T) {
	img := imageServer(t)

	for _, cfg := range []settings.StorageConfig{
		cosSnapshot("https://cdn.example.com", false),
		cosSnapshot("", true),
		{Driver: settings.DriverLocal, RelayPersist: true},
	} {
		p := &ImagePersist{Cloud: newFakeCloud(cfg), Assets: &fakeAssets{}}
		body := []byte(`{"created":1,"data":[{"url":"` + img.URL + `/a.png"}]}`)
		out := p.rewriteBody(context.Background(), body, "img-m", "p")
		if string(out) != string(body) {
			t.Fatalf("cfg %+v: body rewritten to %s", cfg, out)
		}
	}
}

// 尽力而为:下载失败(404)与落行失败(回收字节)都保留厂商原地址,
// 不让转存问题伤害已成功的交付。
func TestImagePersistFailuresKeepVendorURL(t *testing.T) {
	img := imageServer(t)
	gone := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	t.Cleanup(gone.Close)

	// 下载失败:整包原字节。
	cloud := newFakeCloud(cosSnapshot("https://cdn.example.com", true))
	p := &ImagePersist{Cloud: cloud, Assets: &fakeAssets{}}
	body := []byte(`{"data":[{"url":"` + gone.URL + `/gone.png"}]}`)
	if out := p.rewriteBody(context.Background(), body, "m", "p"); string(out) != string(body) {
		t.Fatalf("download failure rewrote body: %s", out)
	}

	// 落行失败:对象回收、URL 保留原址。
	cloud2 := newFakeCloud(cosSnapshot("https://cdn.example.com", true))
	assets := &fakeAssets{failNow: true}
	p2 := &ImagePersist{Cloud: cloud2, Assets: assets}
	mixed := []byte(`{"data":[{"url":"` + img.URL + `/a.png"}]}`)
	out := p2.rewriteBody(context.Background(), mixed, "m", "p")
	if urls := decodeDataURLs(t, out); urls[0] != img.URL+"/a.png" {
		t.Fatalf("row failure url = %q, want vendor original", urls[0])
	}
	cloud2.mu.Lock()
	n := len(cloud2.objs)
	cloud2.mu.Unlock()
	if n != 0 {
		t.Fatalf("orphan objects left after row failure: %d", n)
	}
}
