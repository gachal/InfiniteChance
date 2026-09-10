package main

import (
	"context"
	"fmt"
	"time"

	"github.com/gachal/InfiniteChance/internal/apikey"
)

// The desktop auto-provisions the canvas service key (CONTEXT.md 桌面版):
// the shell mints an sk- key through the apikey store on first boot and
// stores the full value in config.json. Plaintext here matches the trust
// level of the Docker stack's CANVAS_SERVICE_KEY env. The initial quota is
// an accounting device for self-play usage metering (the money goes to the
// configured vendor channels, not to this key), so it starts at the same
// ceiling a manual top-up allows; the admin console can top up further.
const (
	serviceKeyName       = "canvas-service(桌面版自动开通)"
	serviceKeyInitialUSD = apikey.MaxAmountUSD
)

// ensureServiceKey returns the service key to run the canvas with: the one
// already stored in config if it still resolves to an active row, otherwise
// a freshly minted one. A revoked/deleted key therefore self-heals on the
// next app start instead of failing every canvas submit.
func ensureServiceKey(ctx context.Context, keys apikey.Store, cfg *Config) error {
	if cfg.CanvasServiceKey != "" {
		row, err := keys.ByHash(ctx, apikey.Hash(cfg.CanvasServiceKey))
		switch {
		case err == nil && row.Status(time.Now()) == apikey.StatusActive:
			return nil
		case err != nil && err != apikey.ErrKeyNotFound:
			return fmt.Errorf("check service key: %w", err)
		}
		// 未知或已吊销/过期:重新开通一把,替换配置里的旧值。
	}
	full, err := apikey.Generate()
	if err != nil {
		return fmt.Errorf("generate service key: %w", err)
	}
	_, err = keys.Create(ctx, apikey.Key{
		Name:        serviceKeyName,
		Prefix:      apikey.PrefixOf(full),
		KeyHash:     apikey.Hash(full),
		QuotaMicros: serviceKeyInitialUSD * apikey.MicrosPerUSD,
	})
	if err != nil {
		return fmt.Errorf("store service key: %w", err)
	}
	cfg.CanvasServiceKey = full
	return nil
}
