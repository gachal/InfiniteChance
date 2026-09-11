package objectstore

import (
	"context"
	"errors"
	"io"
	"io/fs"
	"log"
	"sync"

	"github.com/gachal/InfiniteChance/internal/settings"
)

// Dynamic routes Store calls by the storage settings row (19 号票). 语义:
//
//   - 写(任务产物转存、上传)按 driver 落位 —— oss 时只写 OSS,新对象
//     不再落本地;
//   - 读(/api/assets/{id}/content)oss 先试,未命中回退本地卷 —— 单向
//     迁移:启用 OSS 前的对象留在本地,读取对调用方透明;
//   - 删除两头都试(Delete 幂等 no-op 已是接口契约);
//   - driver=local 时逐字节等同现状;settings 读失败按 local 缺省回答
//     (与「未配置」同一行为,坏配置不至于让素材面瘫痪)。
//
// The settings reader is injected by the wiring side (canvas/server 与桌面
// 装配各自注入同库读取器)。调用量小,每次调用读表即时生效,不加缓存
// (prompt_templates 先例);仅 OSS 客户端按配置指纹缓存,避免每请求重建
// 连接池。
type Dynamic struct {
	local Store
	read  settings.StorageConfigReader
	// newOSS is swappable for the routing suite (real builds use NewOSS).
	newOSS func(cfg settings.OSSConfig) (Store, error)

	mu     sync.Mutex
	ossCfg settings.OSSConfig // fingerprint of the cached driver below
	oss    Store
}

// NewDynamic wraps local with settings-driven routing. local is always
// non-nil (it is the built-in default and the read fallback); read may be
// nil, which pins the store to local.
func NewDynamic(local Store, read settings.StorageConfigReader) *Dynamic {
	return &Dynamic{
		local:  local,
		read:   read,
		newOSS: func(cfg settings.OSSConfig) (Store, error) { return NewOSS(cfg) },
	}
}

// primary answers the driver the settings row selects: the cached OSS
// store when the row says oss with a complete connection, nil for local
// (callers then talk to d.local directly). A settings read failure logs
// and answers local — the built-in default. OSS 构造失败(如 endpoint 畸形)
// 同样降级本地并大声记日志:字节仍有着落,读取回退也找得到。
func (d *Dynamic) primary(ctx context.Context) Store {
	if d.read == nil {
		return nil
	}
	cfg, err := d.read(ctx)
	if err != nil {
		log.Printf("objectstore: read storage settings: %v (staying local)", err)
		return nil
	}
	if cfg.EffectiveDriver() != settings.DriverOSS {
		return nil
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.oss != nil && d.ossCfg == *cfg.OSS {
		return d.oss
	}
	st, err := d.newOSS(*cfg.OSS)
	if err != nil {
		log.Printf("objectstore: build oss driver: %v (staying local)", err)
		return nil
	}
	d.oss, d.ossCfg = st, *cfg.OSS
	return st
}

func (d *Dynamic) Put(ctx context.Context, key string, r io.Reader, size int64, contentType string) error {
	if st := d.primary(ctx); st != nil {
		return st.Put(ctx, key, r, size, contentType)
	}
	return d.local.Put(ctx, key, r, size, contentType)
}

func (d *Dynamic) Open(ctx context.Context, key string) (io.ReadCloser, error) {
	st := d.primary(ctx)
	if st == nil {
		return d.local.Open(ctx, key)
	}
	obj, err := st.Open(ctx, key)
	if errors.Is(err, fs.ErrNotExist) {
		// 未命中才回退;其余错误(网络/凭证)原样透出 —— 对调用方诚实,
		// 也避免把「OSS 配置坏了」伪装成「对象不存在」。
		return d.local.Open(ctx, key)
	}
	return obj, err
}

func (d *Dynamic) Delete(ctx context.Context, key string) error {
	st := d.primary(ctx)
	if st == nil {
		return d.local.Delete(ctx, key)
	}
	// 两头都删(单向迁移:同一键可能存在两代字节),本地错误优先 ——
	// 它是缺省驱动,先让它把话说完;两头都试过后才汇总。
	localErr := d.local.Delete(ctx, key)
	ossErr := st.Delete(ctx, key)
	if localErr != nil {
		return localErr
	}
	return ossErr
}

// 动态与 OSS 两代驱动都兑现同一接缝。
var _ Store = (*Dynamic)(nil)
var _ Store = (*OSS)(nil)
