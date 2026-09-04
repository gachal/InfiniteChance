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
	"github.com/gachal/InfiniteChance/internal/relay"
	"github.com/gachal/InfiniteChance/internal/usage"
	"github.com/gachal/InfiniteChance/internal/videotask"
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

		issuer := auth.NewIssuerFromConfig(d.Config)
		auth.RegisterRoutes(r, &auth.Handlers{Store: store, Issuer: issuer})

		// 管理面:统一走 JWT 会话;中转面(/v1)统一走 apikey.RequireKey,
		// 两者互不混用。
		admin := r.Group("/admin", auth.RequireAuth(issuer))
		channel.RegisterAdminRoutes(admin, &channel.Handlers{
			Store:  channels,
			Tester: &channel.Tester{},
		})
		apikey.RegisterAdminRoutes(admin, &apikey.Handlers{Store: keys})
		pricing.RegisterAdminRoutes(admin, &pricing.Handlers{Store: prices})
		// 提示词模板:管理端维护,画布侧经共享库即时读取(11 号票)。
		prompttemplate.RegisterAdminRoutes(admin, &prompttemplate.Handlers{Store: promptTemplates})
		// 用量审计:请求级日志列表与按天/模型/渠道汇总(15 号票)。
		usage.RegisterAdminRoutes(admin, &usage.Handlers{Store: usageLogs})

		v1 := r.Group("/v1", apikey.RequireKey(keys))
		relay.RegisterRoutes(v1, &relay.Handlers{
			Channels: channels,
			Keys:     keys,
			Prices:   prices,
			Usage:    usageLogs,
			Tasks:    videoTasks,
		})
	})
}
