// Command gateway-server runs the OpenAI-compatible token API gateway.
package main

import (
	"context"
	"log"

	"github.com/gin-gonic/gin"

	"github.com/gachal/InfiniteChance/internal/apikey"
	"github.com/gachal/InfiniteChance/internal/app"
	"github.com/gachal/InfiniteChance/internal/auth"
	"github.com/gachal/InfiniteChance/internal/channel"
)

func main() {
	app.Run("gateway", "8080", func(r *gin.Engine, d app.Deps) {
		store := auth.NewMySQLStore(d.DB)
		if err := store.EnsureSchema(context.Background()); err != nil {
			log.Fatalf("ensure admin schema: %v", err)
		}
		channels := channel.NewMySQLStore(d.DB)
		if err := channels.EnsureSchema(context.Background()); err != nil {
			log.Fatalf("ensure channel schema: %v", err)
		}
		keys := apikey.NewMySQLStore(d.DB)
		if err := keys.EnsureSchema(context.Background()); err != nil {
			log.Fatalf("ensure api key schema: %v", err)
		}

		issuer := auth.NewIssuerFromConfig(d.Config)
		auth.RegisterRoutes(r, &auth.Handlers{Store: store, Issuer: issuer})

		// 管理面:统一走 JWT 会话;中转面(/v1)由 04 号票挂 apikey.RequireKey。
		admin := r.Group("/admin", auth.RequireAuth(issuer))
		channel.RegisterAdminRoutes(admin, &channel.Handlers{
			Store:  channels,
			Tester: &channel.Tester{},
		})
		apikey.RegisterAdminRoutes(admin, &apikey.Handlers{Store: keys})
	})
}
