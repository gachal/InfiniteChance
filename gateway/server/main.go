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
	"github.com/gachal/InfiniteChance/internal/pricing"
	"github.com/gachal/InfiniteChance/internal/prompttemplate"
	"github.com/gachal/InfiniteChance/internal/usage"
	"github.com/gachal/InfiniteChance/internal/videotask"
	"github.com/gachal/InfiniteChance/internal/wiring"
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
		prices := pricing.NewMySQLStore(d.DB)
		if err := prices.EnsureSchema(context.Background()); err != nil {
			log.Fatalf("ensure model price schema: %v", err)
		}
		usageLogs := usage.NewMySQLStore(d.DB)
		if err := usageLogs.EnsureSchema(context.Background()); err != nil {
			log.Fatalf("ensure usage log schema: %v", err)
		}
		videoTasks := videotask.NewMySQLStore(d.DB)
		if err := videoTasks.EnsureSchema(context.Background()); err != nil {
			log.Fatalf("ensure video task schema: %v", err)
		}
		promptTemplates := prompttemplate.NewMySQLStore(d.DB)
		if err := promptTemplates.EnsureSchema(context.Background()); err != nil {
			log.Fatalf("ensure prompt template schema: %v", err)
		}

		wiring.GatewayRoutes(r, d.Config, wiring.GatewayStores{
			Auth:            store,
			Channels:        channels,
			Keys:            keys,
			Prices:          prices,
			UsageLogs:       usageLogs,
			VideoTasks:      videoTasks,
			PromptTemplates: promptTemplates,
		})
	})
}
