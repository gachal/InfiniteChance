package objectstore

import (
	"bytes"
	"context"
	"errors"
	"io"
	"io/fs"
	"strings"
	"sync"
	"testing"

	"github.com/gachal/InfiniteChance/internal/settings"
)

// memStore is an in-memory Store: the local volume and the remote driver
// share this stand-in, recording touched keys so routing stays assertable.
type memStore struct {
	mu    sync.Mutex
	objs  map[string][]byte
	calls []string // "put:key" / "open:key" / "delete:key"
}

func newMemStore() *memStore { return &memStore{objs: map[string][]byte{}} }

func (m *memStore) record(call string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.calls = append(m.calls, call)
}

func (m *memStore) called(call string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, c := range m.calls {
		if c == call {
			return true
		}
	}
	return false
}

func (m *memStore) Put(_ context.Context, key string, r io.Reader, size int64, _ string) error {
	m.record("put:" + key)
	body, err := io.ReadAll(io.LimitReader(r, size))
	if err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.objs[key] = body
	return nil
}

func (m *memStore) Open(_ context.Context, key string) (io.ReadCloser, error) {
	m.record("open:" + key)
	m.mu.Lock()
	defer m.mu.Unlock()
	body, ok := m.objs[key]
	if !ok {
		return nil, fs.ErrNotExist
	}
	return io.NopCloser(bytes.NewReader(body)), nil
}

func (m *memStore) Delete(_ context.Context, key string) error {
	m.record("delete:" + key)
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.objs, key)
	return nil
}

// fixedReader answers the settings row from mutable state, with an
// injectable read failure.
type fixedReader struct {
	mu  sync.Mutex
	cfg settings.StorageConfig
	err error
}

func (f *fixedReader) read(context.Context) (settings.StorageConfig, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.cfg, f.err
}

func ossCfg() *settings.OSSConfig {
	return &settings.OSSConfig{Endpoint: "e", Bucket: "b", AccessKey: "AK", SecretKey: "SK"}
}

// newDynamicTest wires a Dynamic whose「OSS 驱动」is injectable, so the
// routing suite never touches the network.
func newDynamicTest(reader *fixedReader) (*Dynamic, *memStore, *memStore) {
	local := newMemStore()
	remote := newMemStore()
	d := NewDynamic(local, reader.read)
	d.newOSS = func(cfg settings.OSSConfig) (Store, error) { return remote, nil }
	return d, local, remote
}

func (r *fixedReader) set(cfg settings.StorageConfig) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.cfg = cfg
}

// 写按 driver 落位:local 时字节只进本地;oss 时只进远端(单向迁移,新
// 对象不再落本地)。
func TestDynamicPutRoutesByDriver(t *testing.T) {
	ctx := context.Background()

	reader := &fixedReader{cfg: settings.StorageConfig{Driver: settings.DriverLocal}}
	d, local, remote := newDynamicTest(reader)
	if err := d.Put(ctx, "k", strings.NewReader("v"), 1, "text/plain"); err != nil {
		t.Fatal(err)
	}
	if !local.called("put:k") || remote.called("put:k") {
		t.Fatalf("local put=%v remote put=%v, want local only", local.called("put:k"), remote.called("put:k"))
	}

	reader.set(settings.StorageConfig{Driver: settings.DriverOSS, OSS: ossCfg()})
	if err := d.Put(ctx, "k2", strings.NewReader("v"), 1, "text/plain"); err != nil {
		t.Fatal(err)
	}
	if !remote.called("put:k2") || local.called("put:k2") {
		t.Fatalf("local put=%v remote put=%v, want remote only", local.called("put:k2"), remote.called("put:k2"))
	}
}

// 读回退:oss 时先远端,未命中回退本地 —— 启用 OSS 前的历史对象读取对
// 调用方透明;两边都未命中按本地错误回答。
func TestDynamicOpenFallsBackToLocal(t *testing.T) {
	ctx := context.Background()
	reader := &fixedReader{cfg: settings.StorageConfig{Driver: settings.DriverLocal}}
	d, local, remote := newDynamicTest(reader)

	// 历史对象:local 时代写入。
	if err := local.Put(ctx, "legacy", strings.NewReader("old"), 3, "text/plain"); err != nil {
		t.Fatal(err)
	}
	// 切到 oss 后仍读得到。
	reader.set(settings.StorageConfig{Driver: settings.DriverOSS, OSS: ossCfg()})
	obj, err := d.Open(ctx, "legacy")
	if err != nil {
		t.Fatalf("Open legacy after switch: %v", err)
	}
	body, _ := io.ReadAll(obj)
	obj.Close()
	if string(body) != "old" {
		t.Fatalf("legacy bytes = %q", body)
	}

	// 远端对象直接命中。
	if err := remote.Put(ctx, "fresh", strings.NewReader("new"), 3, "text/plain"); err != nil {
		t.Fatal(err)
	}
	if obj, err := d.Open(ctx, "fresh"); err != nil {
		t.Fatalf("Open fresh: %v", err)
	} else {
		obj.Close()
	}

	// 两边都没有:fs.ErrNotExist,且确实试过本地(回退发生)。
	if _, err := d.Open(ctx, "gone"); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("missing both = %v, want fs.ErrNotExist", err)
	}
	if !local.called("open:gone") {
		t.Fatal("fallback should have tried local for the miss")
	}
}

// 删除:oss 时两头都试(同一键可能有本地与 OSS 两代字节);local 时只
// 动本地(与现状逐字节一致)。
func TestDynamicDeleteTouchesBothSides(t *testing.T) {
	ctx := context.Background()
	reader := &fixedReader{cfg: settings.StorageConfig{Driver: settings.DriverOSS, OSS: ossCfg()}}
	d, local, remote := newDynamicTest(reader)

	if err := d.Delete(ctx, "k"); err != nil {
		t.Fatal(err)
	}
	if !local.called("delete:k") || !remote.called("delete:k") {
		t.Fatalf("delete local=%v remote=%v, want both", local.called("delete:k"), remote.called("delete:k"))
	}

	reader.set(settings.StorageConfig{Driver: settings.DriverLocal})
	if err := d.Delete(ctx, "k2"); err != nil {
		t.Fatal(err)
	}
	if remote.called("delete:k2") {
		t.Fatal("local driver must not touch the remote side")
	}
}

// settings 读故障 → 降级 local(与「未配置」同一行为),不把素材面打瘫。
func TestDynamicSettingsReadFailureDegradesToLocal(t *testing.T) {
	ctx := context.Background()
	reader := &fixedReader{err: errors.New("db down")}
	d, local, remote := newDynamicTest(reader)

	if err := d.Put(ctx, "k", strings.NewReader("v"), 1, "text/plain"); err != nil {
		t.Fatal(err)
	}
	if !local.called("put:k") || remote.called("put:k") {
		t.Fatal("settings read failure should degrade to local")
	}
	if obj, err := d.Open(ctx, "k"); err != nil {
		t.Fatalf("Open after degraded put: %v", err)
	} else {
		obj.Close()
	}
}

// 连接不完整的 oss 行读侧按 local 处理(EffectiveDriver 兜底)。
func TestDynamicIncompleteOSSRowActsLocal(t *testing.T) {
	ctx := context.Background()
	reader := &fixedReader{cfg: settings.StorageConfig{Driver: settings.DriverOSS,
		OSS: &settings.OSSConfig{Endpoint: "e", Bucket: "b"}}}
	d, local, remote := newDynamicTest(reader)

	if err := d.Put(ctx, "k", strings.NewReader("v"), 1, "text/plain"); err != nil {
		t.Fatal(err)
	}
	if !local.called("put:k") || remote.called("put:k") {
		t.Fatal("incomplete oss row should act local")
	}
}

// 配置变化时重建驱动(缓存按配置指纹失效),nil reader 钉死 local。
func TestDynamicDriverCacheAndNilReader(t *testing.T) {
	ctx := context.Background()

	// nil reader:恒 local。
	d := NewDynamic(newMemStore(), nil)
	if err := d.Put(ctx, "k", strings.NewReader("v"), 1, "text/plain"); err != nil {
		t.Fatal(err)
	}

	// 换配置 → 新驱动(以不同 remote 实例佐证重建;缓存按配置指纹失效,
	// 只换工厂不换配置不触发)。
	reader := &fixedReader{cfg: settings.StorageConfig{Driver: settings.DriverOSS, OSS: ossCfg()}}
	d2 := NewDynamic(newMemStore(), reader.read)
	firstRemote := newMemStore()
	secondRemote := newMemStore()
	d2.newOSS = func(cfg settings.OSSConfig) (Store, error) { return firstRemote, nil }
	if err := d2.Put(ctx, "a", strings.NewReader("1"), 1, "text/plain"); err != nil {
		t.Fatal(err)
	}
	d2.newOSS = func(cfg settings.OSSConfig) (Store, error) { return secondRemote, nil }
	// 同配置不重建:仍走 firstRemote。
	if err := d2.Put(ctx, "same", strings.NewReader("1"), 1, "text/plain"); err != nil {
		t.Fatal(err)
	}
	if !firstRemote.called("put:same") || secondRemote.called("put:same") {
		t.Fatal("same config should reuse the cached driver")
	}
	changed := ossCfg()
	changed.Bucket = "b2"
	reader.set(settings.StorageConfig{Driver: settings.DriverOSS, OSS: changed})
	if err := d2.Put(ctx, "b", strings.NewReader("1"), 1, "text/plain"); err != nil {
		t.Fatal(err)
	}
	if !firstRemote.called("put:a") || !secondRemote.called("put:b") {
		t.Fatal("config change should rebuild the driver")
	}
}
