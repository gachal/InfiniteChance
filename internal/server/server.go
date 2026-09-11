// Package server assembles the HTTP surface shared by both binaries:
// logging, panic recovery and the /healthz dependency report.
package server

import (
	"github.com/gin-gonic/gin"

	"github.com/gachal/InfiniteChance/internal/health"
)

// New builds the gin engine with common middleware and the health endpoint.
// deps maps dependency names to pingers reported by /healthz.
func New(service string, deps map[string]health.Pinger) *gin.Engine {
	r := gin.New()
	r.Use(gin.Logger(), gin.Recovery(), securityHeaders())
	r.GET("/healthz", health.Handler(service, deps))
	return r
}

// securityHeaders stamps the baseline hardening headers on every response of
// both services (web binaries and the desktop shell share this engine):
// browsers and webviews honoring them cost nothing, and the API surface gets
// the same protection when responses render outside the dedicated SPAs.
func securityHeaders() gin.HandlerFunc {
	return func(c *gin.Context) {
		h := c.Writer.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "strict-origin-when-cross-origin")
		c.Next()
	}
}
