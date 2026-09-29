// Command gateway-server runs the OpenAI-compatible token API gateway.
package main

import (
	"context"
	"log"

	"github.com/gin-gonic/gin"

	"github.com/gachal/InfiniteChance/internal/apikey"
	"github.com/gachal/InfiniteChance/internal/app"
	"github.com/gachal/InfiniteChance/internal/asset"
	"github.com/gachal/InfiniteChance/internal/auth"
	"github.com/gachal/InfiniteChance/internal/channel"
	"github.com/gachal/InfiniteChance/internal/objectstore"
	"github.com/gachal/InfiniteChance/internal/pricing"
	"github.com/gachal/InfiniteChance/internal/prompttemplate"
	"github.com/gachal/InfiniteChance/internal/settings"
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
		// 动态配置(19 号票):管理面挂 /admin/settings/storage,画布侧
		// 同库只读。
		settingsStore := settings.NewMySQLStore(d.DB)
		if err := settingsStore.EnsureSchema(context.Background()); err != nil {
			log.Fatalf("ensure settings schema: %v", err)
		}
		// 素材行 + 对象存储(23 号票 relay_persist):直连生图产物落库落桶
		// 的依赖,asset 表与画布侧同库同表。本地卷只是 Dynamic 的缺省与
		// 读回退;转存写入永远按 settings 落活跃云驱动。
		assets := asset.NewMySQLStore(d.DB)
		if err := assets.EnsureSchema(context.Background()); err != nil {
			log.Fatalf("ensure asset schema: %v", err)
		}
		var storage *objectstore.Dynamic
		if local, err := objectstore.NewFileSystem(d.Config.AssetStorageDir); err != nil {
			log.Printf("WARNING: 素材对象存储不可用(%v),直连生图转存将不可用", err)
		} else {
			storage = objectstore.NewDynamic(local, settings.NewStorageReader(settingsStore))
		}

		wiring.GatewayRoutes(r, d.Config, wiring.GatewayStores{
			Auth:            store,
			Channels:        channels,
			Keys:            keys,
			Prices:          prices,
			UsageLogs:       usageLogs,
			VideoTasks:      videoTasks,
			PromptTemplates: promptTemplates,
			Settings:        settingsStore,
			Assets:          assets,
			Storage:         storage,
		})
	})
}
