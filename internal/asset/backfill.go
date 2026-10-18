package asset

import (
	"context"
	"log"
	"net/http"
	"sync"
	"sync/atomic"

	"github.com/gin-gonic/gin"

	"github.com/gachal/InfiniteChance/internal/objectstore"
)

// BackfillStore is the row access the 48 号票 hash backfill needs, on top of
// the plain Store: 批量扫描无哈希行、落列、按哈希分组与改指。两方言 store
// 都实现它;handler 测试的内存 fake 不实现(回填不打那套假件)。
type BackfillStore interface {
	Store
	// ListWithoutHash pages un-hashed rows carrying an object key,
	// ascending by id with afterID as the cursor.
	ListWithoutHash(ctx context.Context, afterID int64, limit int) ([]Asset, error)
	// SetContentHash stamps one row's content hash.
	SetContentHash(ctx context.Context, id int64, contentHash string) error
	// DuplicateHashes answers hashes carried by more than one row.
	DuplicateHashes(ctx context.Context) ([]string, error)
	// ListByHash answers every row of one hash group, oldest first.
	ListByHash(ctx context.Context, contentHash string) ([]Asset, error)
	// SetObjectKey repoints one row at another object key.
	SetObjectKey(ctx context.Context, id int64, objectKey string) error
}

// backfillBatch is how many rows one hashing pass reads; 分批推进防一次性
// 读爆内存,批间不加刻意 sleep(单管理员库小,COS 读流量是已知代价)。
const backfillBatch = 50

// Backfill computes content hashes for rows missing one and merges duplicate
// objects (48 号票,手动触发、幂等、无独立取消 —— 断点续跑 = 已有哈希的行
// 跳过,重跑安全):
//
//  1. 游标分批取 content_hash 为空且 object_key 非空的行(仅厂商地址或
//     data: URI 的历史行没有对象,无从算哈希,直接跳过),经 Dynamic 读
//     对象(云优先,ErrNotExist 回退本地)流式算 SHA-256 落列;
//  2. 按 content_hash 分组,组内保留最老行(最小 id)的对象,其余行改指
//     后删除冗余对象(Dynamic 双端幂等)。行不合并 —— canvas/task/prompt
//     的出处语义各自保留。
//
// Progress 是只增计数器,GET 随时可读;任务在跑时再触发 POST 是 no-op,
// 回当前进度。
type Backfill struct {
	Store   BackfillStore
	Storage objectstore.Store

	mu        sync.Mutex
	running   bool
	scanned   atomic.Int64 // 已扫行(含对象读不动的跳过行)
	hashed    atomic.Int64 // 已算哈希落列
	repointed atomic.Int64 // 已改指行
	deleted   atomic.Int64 // 已删冗余对象
}

// Progress is the wire shape of one backfill snapshot.
type Progress struct {
	Running     bool  `json:"running"`
	ScannedRows int64 `json:"scanned_rows"`
	HashedRows  int64 `json:"hashed_rows"`
	Repointed   int64 `json:"repointed_rows"`
	DeletedObjs int64 `json:"deleted_objects"`
}

// snapshot answers the current counters; running 取 mu 下的真值,计数器
// 原子读,单次快照内部不要求严格同刻。
func (b *Backfill) snapshot() Progress {
	b.mu.Lock()
	running := b.running
	b.mu.Unlock()
	return Progress{
		Running:     running,
		ScannedRows: b.scanned.Load(),
		HashedRows:  b.hashed.Load(),
		Repointed:   b.repointed.Load(),
		DeletedObjs: b.deleted.Load(),
	}
}

// Start launches the backfill unless it is already running (幂等:在跑则
// no-op 回当前进度). The task outlives the HTTP request — 管理台关掉页面
// 合并照常推进,进度由 GET 轮询。
func (b *Backfill) Start(c *gin.Context) {
	b.mu.Lock()
	if b.running {
		b.mu.Unlock()
		c.JSON(http.StatusOK, b.snapshot())
		return
	}
	b.running = true
	b.scanned.Store(0)
	b.hashed.Store(0)
	b.repointed.Store(0)
	b.deleted.Store(0)
	b.mu.Unlock()
	go b.run(context.WithoutCancel(c.Request.Context()))
	c.JSON(http.StatusAccepted, b.snapshot())
}

// Status answers the current backfill progress.
func (b *Backfill) Status(c *gin.Context) {
	c.JSON(http.StatusOK, b.snapshot())
}

// RegisterBackfillRoutes mounts the backfill surfaces (relative to the
// group, which wiring mounts at /admin/assets/hash-backfill behind JWT):
//
//	POST / — launch (no-op with current progress while running)
//	GET  / — progress snapshot
func RegisterBackfillRoutes(group *gin.RouterGroup, b *Backfill) {
	group.POST("", b.Start)
	group.GET("", b.Status)
}

// run executes the two phases; any error is logged and ends the pass —
// 重跑安全是兜底,不引入取消与状态机。
func (b *Backfill) run(ctx context.Context) {
	defer func() {
		b.mu.Lock()
		b.running = false
		b.mu.Unlock()
	}()
	b.hashPhase(ctx)
	b.mergePhase(ctx)
}

// hashPhase walks every un-hashed row cursor-style. 对象读不动(缺失、
// 网络)的行记日志跳过 —— 游标按行 id 推进,不会死循环;下次触发自然重试。
func (b *Backfill) hashPhase(ctx context.Context) {
	after := int64(0)
	for {
		rows, err := b.Store.ListWithoutHash(ctx, after, backfillBatch)
		if err != nil {
			log.Printf("asset: backfill scan: %v", err)
			return
		}
		if len(rows) == 0 {
			return
		}
		for _, row := range rows {
			after = row.ID
			b.scanned.Add(1)
			if row.ObjectKey == "" {
				continue // 防御:查询已过滤,历史行(仅厂商地址/data: URI)跳过
			}
			hash, err := b.hashObject(ctx, row.ObjectKey)
			if err != nil {
				log.Printf("asset: backfill hash %q of asset %d: %v (skipped)", row.ObjectKey, row.ID, err)
				continue
			}
			if err := b.Store.SetContentHash(ctx, row.ID, hash); err != nil {
				log.Printf("asset: backfill stamp asset %d: %v (skipped)", row.ID, err)
				continue
			}
			b.hashed.Add(1)
		}
	}
}

// hashObject streams one object through SHA-256. Dynamic.Open 已做云优先
// 本地回退;两边都缺(手工清理过)或云侧网络故障同样按「跳过」计 ——
// 下次触发自然重试。
func (b *Backfill) hashObject(ctx context.Context, key string) (string, error) {
	obj, err := b.Storage.Open(ctx, key)
	if err != nil {
		return "", err
	}
	defer obj.Close()
	return hashReader(obj)
}

// mergePhase collapses duplicate objects per hash group: 组内最老行(最小
// id)的对象保留,其余行改指后删除旧对象。改指失败的行不动也不删对象
// (下一轮重跑再收敛);删除失败留孤儿键,只记日志。
func (b *Backfill) mergePhase(ctx context.Context) {
	dupes, err := b.Store.DuplicateHashes(ctx)
	if err != nil {
		log.Printf("asset: backfill group: %v", err)
		return
	}
	for _, hash := range dupes {
		rows, err := b.Store.ListByHash(ctx, hash)
		if err != nil {
			log.Printf("asset: backfill members of %s: %v", shortKey(hash), err)
			continue
		}
		if len(rows) < 2 || rows[0].ObjectKey == "" {
			continue // 保留行必有对象:无对象的行没有哈希,进不了组
		}
		keep := rows[0].ObjectKey
		for _, row := range rows[1:] {
			if row.ObjectKey == keep {
				continue // 已共享(上传复用或前轮改指),无需动
			}
			if err := b.Store.SetObjectKey(ctx, row.ID, keep); err != nil {
				log.Printf("asset: backfill repoint asset %d: %v (kept %q)", row.ID, err, shortKey(row.ObjectKey))
				continue
			}
			b.repointed.Add(1)
			if err := b.Storage.Delete(ctx, row.ObjectKey); err != nil {
				// 删除失败留孤儿对象,记日志即止:下轮重跑时该行已指 keep
				// 键,不会再走到这里 —— 孤儿回收靠日志暴露人工处理,不引入
				// 待删队列(票面明示豁免;Dynamic 双端幂等使手工重删安全)。
				log.Printf("asset: backfill delete redundant object %q: %v (orphan left)", shortKey(row.ObjectKey), err)
				continue
			}
			b.deleted.Add(1)
		}
	}
}

// shortKey keeps object keys readable in log lines — 键里没有秘密,只是
// 回填日志一行一个键太长,首尾各留 8 字符足够定位。
func shortKey(s string) string {
	if len(s) <= 16 {
		return s
	}
	return s[:8] + "…" + s[len(s)-8:]
}
