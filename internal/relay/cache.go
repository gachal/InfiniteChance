package relay

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/gachal/InfiniteChance/internal/channel"
	"github.com/gachal/InfiniteChance/internal/pricing"
)

// DefaultCacheTTL is the read-through lifetime wiring opts the relay into:
// an admin channel/price edit lands on the relay surface within one TTL,
// and in exchange every forwarded request skips its two hottest DB reads
// (full channel list + per-model price). Admin writes go through the same
// process, so no cross-instance invalidation is needed for the single-gateway
// and desktop shapes this TTL serves.
const DefaultCacheTTL = 3 * time.Second

// relayCache carries the short-TTL read-through state for those two reads.
// Cached channels/prices are shared across requests and must stay read-only
// — the scheduling and billing code only reads them.
type relayCache struct {
	ttl time.Duration

	mu         sync.Mutex
	chanList   []channel.Channel
	channelsAt time.Time
	prices     map[string]priceEntry
}

type priceEntry struct {
	price pricing.Price
	err   error // 仅缓存 ErrNotFound:未配价模型不许反复打库
	at    time.Time
}

func newRelayCache(ttl time.Duration) *relayCache {
	return &relayCache{ttl: ttl, prices: map[string]priceEntry{}}
}

// channels returns the cached channel list while it is fresh, refilling it
// from the store once per TTL.
func (c *relayCache) channels(ctx context.Context, store channel.Store) ([]channel.Channel, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.chanList != nil && time.Since(c.channelsAt) < c.ttl {
		return c.chanList, nil
	}
	list, err := store.List(ctx)
	if err != nil {
		return nil, err
	}
	c.chanList = list
	c.channelsAt = time.Now()
	return list, nil
}

// price returns the cached per-model price while fresh. ErrNotFound is
// cached too — an unpriced model must not turn every rejection into a DB
// round trip — while store failures stay uncached so a transient outage
// clears with the next request.
func (c *relayCache) price(ctx context.Context, store pricing.Store, model string) (pricing.Price, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if e, ok := c.prices[model]; ok && time.Since(e.at) < c.ttl {
		return e.price, e.err
	}
	p, err := store.ByModel(ctx, model)
	if err != nil && !errors.Is(err, pricing.ErrNotFound) {
		return pricing.Price{}, err
	}
	c.prices[model] = priceEntry{price: p, err: err, at: time.Now()}
	return p, err
}
