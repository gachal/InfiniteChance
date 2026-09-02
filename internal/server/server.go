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
	r.Use(gin.Logger(), gin.Recovery())
	r.GET("/healthz", health.Handler(service, deps))
	return r
}
