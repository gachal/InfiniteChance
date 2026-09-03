package relay

import (
	"math/rand/v2"

	"github.com/gin-gonic/gin"

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
// for concurrent use itself.
type Handlers struct {
	Channels channel.Store
	Keys     apikey.Store
	Prices   pricing.Store
	Usage    usage.Store
	Tasks    videotask.Store
	Adaptor  Adaptor
	Breaker  *channel.Breaker
	Rand     *rand.Rand
}

// RegisterRoutes mounts the relay endpoints on group. The caller owns the
// group's middleware — the gateway mounts it as r.Group("/v1",
// apikey.RequireKey(keys)) so no route here can bypass key auth. Called
// once at startup, before serving.
func RegisterRoutes(group *gin.RouterGroup, h *Handlers) {
	if h.Breaker == nil {
		h.Breaker = channel.NewBreaker()
	}
	group.GET("/models", h.ListModels)
	group.POST("/chat/completions", h.ChatCompletions)
	group.POST("/images/generations", h.ImagesGenerations)
	group.POST("/images/edits", h.ImagesEdits)
	group.POST("/videos/generations", h.CreateVideoGeneration)
	group.GET("/videos/tasks/:id", h.GetVideoTask)
	group.POST("/videos/tasks/:id/cancel", h.CancelVideoTask)
}

// adaptor returns the configured vendor seam, defaulting to the
// OpenAI-compatible one (the only channel type this build supports).
func (h *Handlers) adaptor() Adaptor {
	if h.Adaptor != nil {
		return h.Adaptor
	}
	return NewOpenAIAdaptor()
}
