package relay

import (
	"context"
	"errors"
	"math/rand/v2"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/gachal/InfiniteChance/internal/apierr"
	"github.com/gachal/InfiniteChance/internal/apikey"
	"github.com/gachal/InfiniteChance/internal/channel"
	"github.com/gachal/InfiniteChance/internal/pricing"
	"github.com/gachal/InfiniteChance/internal/usage"
	"github.com/gachal/InfiniteChance/internal/videotask"
)

// Handlers serves the relay (/v1) surface. Every dependency is the store
// interface the respective package owns; Adaptor is the vendor seam and
// defaults to the OpenAI-compatible adaptor when nil. Breaker is the
// per-channel circuit state shared by every request (06 号票); RegisterRoutes
// defaults it — a Handlers served without RegisterRoutes must set it itself.
// Rand is the scheduling randomness source — nil uses the package generator
// (goroutine-safe); an injected one is test plumbing and must then be safe
// for concurrent use itself. CacheTTL > 0 opts the hot-path channel/price
// reads into a short in-process TTL cache (wiring sets DefaultCacheTTL;
// tests leave it zero to hit the stores directly).
type Handlers struct {
	Channels channel.Store
	Keys     apikey.Store
	Prices   pricing.Store
	Usage    usage.Store
	Tasks    videotask.Store
	Adaptor  Adaptor
	Breaker  *channel.Breaker
	Rand     *rand.Rand
	CacheTTL time.Duration

	cache *relayCache
}

// RegisterRoutes mounts the relay endpoints on group. The caller owns the
// group's middleware — the gateway mounts it as r.Group("/v1",
// apikey.RequireKey(keys)) so no route here can bypass key auth. Called
// once at startup, before serving.
func RegisterRoutes(group *gin.RouterGroup, h *Handlers) {
	if h.Breaker == nil {
		h.Breaker = channel.NewBreaker()
	}
	if h.CacheTTL > 0 {
		h.cache = newRelayCache(h.CacheTTL)
	}
	group.Use(LimitBody)
	group.GET("/models", h.ListModels)
	group.POST("/chat/completions", h.ChatCompletions)
	group.POST("/images/generations", h.ImagesGenerations)
	group.POST("/images/edits", h.ImagesEdits)
	group.POST("/videos/generations", h.CreateVideoGeneration)
	group.GET("/videos/tasks/:id", h.GetVideoTask)
	group.POST("/videos/tasks/:id/cancel", h.CancelVideoTask)
}

// maxRequestBody caps how much of a client request body the /v1 surface
// reads into memory (chat JSON, generations JSON, multipart edits) —
// aligned with the upstream response cap; oversize is refused before any
// billing or upstream dialing instead of exhausting the gateway.
const maxRequestBody = maxUpstreamBody

// LimitBody bounds the request body: an explicit Content-Length over the
// cap is refused up front with 413, a chunked body is cut by
// http.MaxBytesReader so reads fail with *http.MaxBytesError once over.
func LimitBody(c *gin.Context) {
	if c.Request.ContentLength > maxRequestBody {
		tooLarge(c)
		return
	}
	if c.Request.Body != nil {
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxRequestBody)
	}
	c.Next()
}

// refuseBody maps one body-read failure onto the relay error contract:
// an oversized body is 413, anything else the plain could-not-read 400.
func refuseBody(c *gin.Context, err error) {
	var tooBig *http.MaxBytesError
	if errors.As(err, &tooBig) {
		tooLarge(c)
		return
	}
	apierr.OpenAI(c, http.StatusBadRequest, CodeInvalidRequest, TypeInvalidRequestError, "The request body could not be read.")
}

func tooLarge(c *gin.Context) {
	apierr.OpenAI(c, http.StatusRequestEntityTooLarge, CodeInvalidRequest, TypeInvalidRequestError,
		"The request body exceeds the gateway's 32 MiB limit.")
	c.Abort()
}

// adaptor returns the configured vendor seam, defaulting to the
// OpenAI-compatible one (the only channel type this build supports).
func (h *Handlers) adaptor() Adaptor {
	if h.Adaptor != nil {
		return h.Adaptor
	}
	return NewOpenAIAdaptor()
}

// listChannels reads the channel list through the TTL cache when enabled.
func (h *Handlers) listChannels(ctx context.Context) ([]channel.Channel, error) {
	if h.cache == nil {
		return h.Channels.List(ctx)
	}
	return h.cache.channels(ctx, h.Channels)
}

// priceFor reads one model's price through the TTL cache when enabled.
func (h *Handlers) priceFor(ctx context.Context, model string) (pricing.Price, error) {
	if h.cache == nil {
		return h.Prices.ByModel(ctx, model)
	}
	return h.cache.price(ctx, h.Prices, model)
}
