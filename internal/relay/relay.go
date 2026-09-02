package relay

import (
	"github.com/gin-gonic/gin"

	"github.com/gachal/InfiniteChance/internal/apikey"
	"github.com/gachal/InfiniteChance/internal/channel"
	"github.com/gachal/InfiniteChance/internal/pricing"
	"github.com/gachal/InfiniteChance/internal/usage"
)

// Handlers serves the relay (/v1) surface. Every dependency is the store
// interface the respective package owns; Adaptor is the vendor seam and
// defaults to the OpenAI-compatible adaptor when nil.
type Handlers struct {
	Channels channel.Store
	Keys     apikey.Store
	Prices   pricing.Store
	Usage    usage.Store
	Adaptor  Adaptor
}

// RegisterRoutes mounts the relay endpoints on group. The caller owns the
// group's middleware — the gateway mounts it as r.Group("/v1",
// apikey.RequireKey(keys)) so no route here can bypass key auth.
func RegisterRoutes(group *gin.RouterGroup, h *Handlers) {
	group.POST("/chat/completions", h.ChatCompletions)
}

// adaptor returns the configured vendor seam, defaulting to the
// OpenAI-compatible one (the only channel type this build supports).
func (h *Handlers) adaptor() Adaptor {
	if h.Adaptor != nil {
		return h.Adaptor
	}
	return NewOpenAIAdaptor()
}
