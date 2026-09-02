package health

import (
	"context"

	"github.com/redis/go-redis/v9"
)

// Redis pings a go-redis client.
type Redis struct {
	Client *redis.Client
}

func (r Redis) Ping(ctx context.Context) error { return r.Client.Ping(ctx).Err() }

// NewRedis builds a go-redis client for the given address.
func NewRedis(addr string) *redis.Client {
	return redis.NewClient(&redis.Options{Addr: addr})
}
