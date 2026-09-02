// Command gateway-server runs the OpenAI-compatible token API gateway.
package main

import (
	"context"
	"log"

	"github.com/gin-gonic/gin"

	"github.com/gachal/InfiniteChance/internal/app"
	"github.com/gachal/InfiniteChance/internal/auth"
)

func main() {
	app.Run("gateway", "8080", func(r *gin.Engine, d app.Deps) {
		store := auth.NewMySQLStore(d.DB)
		if err := store.EnsureSchema(context.Background()); err != nil {
			log.Fatalf("ensure admin schema: %v", err)
		}
		auth.RegisterRoutes(r, &auth.Handlers{
			Store:  store,
			Issuer: auth.NewIssuerFromConfig(d.Config),
		})
	})
}
