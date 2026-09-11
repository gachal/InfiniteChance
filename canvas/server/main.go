// Command canvas-server runs the canvas persistence and task-orchestration API.
package main

import (
	"context"
	"log"

	"github.com/gin-gonic/gin"

	"github.com/gachal/InfiniteChance/internal/app"
	"github.com/gachal/InfiniteChance/internal/asset"
	"github.com/gachal/InfiniteChance/internal/auth"
	"github.com/gachal/InfiniteChance/internal/canvas"
	"github.com/gachal/InfiniteChance/internal/canvastask"
	"github.com/gachal/InfiniteChance/internal/config"
	"github.com/gachal/InfiniteChance/internal/objectstore"
	"github.com/gachal/InfiniteChance/internal/pricing"
	"github.com/gachal/InfiniteChance/internal/prompttemplate"
	"github.com/gachal/InfiniteChance/internal/wiring"
)

func main() {
	app.Run("canvas", "8081", func(r *gin.Engine, d app.Deps) {
		// 与网关共享同一 admin_accounts 表与 JWT_SECRET:账号只建一次,
		// 网关侧初始化/登录后,画布侧用同一账号登录即可,无需二次注册。
		// EnsureSchema 幂等,画布服务独立先启动时也保证账号表存在。
		store := auth.NewMySQLStore(d.DB)
		if err := store.EnsureSchema(context.Background()); err != nil {
			log.Fatalf("ensure admin schema: %v", err)
		}
		canvases := canvas.NewMySQLStore(d.DB)
		if err := canvases.EnsureSchema(context.Background()); err != nil {
			log.Fatalf("ensure canvas schema: %v", err)
		}
		assets := asset.NewMySQLStore(d.DB)
		if err := assets.EnsureSchema(context.Background()); err != nil {
			log.Fatalf("ensure asset schema: %v", err)
		}
		prices := pricing.NewMySQLStore(d.DB)
		if err := prices.EnsureSchema(context.Background()); err != nil {
			log.Fatalf("ensure model price schema: %v", err)
		}
		tasks := canvastask.NewMySQLStore(d.DB, assets)
		if err := tasks.EnsureSchema(context.Background()); err != nil {
			log.Fatalf("ensure canvas task schema: %v", err)
		}
		// 提示词模板:表由网关管理端维护,这里只读(同库共享,11 号票),
		// EnsureSchema 保证画布服务独立先启动时表也存在。
		templates := prompttemplate.NewMySQLStore(d.DB)
		if err := templates.EnsureSchema(context.Background()); err != nil {
			log.Fatalf("ensure prompt template schema: %v", err)
		}
		// 产物对象存储(14 号票):S3 兼容接口的本地卷落地,键按画布/任务
		// 归档;建不出来只影响素材转存,不拦整个服务起来 —— 生成任务会以
		// 「转存失败」落在任务行上,可重试。
		storage, err := objectstore.NewFileSystem(d.Config.AssetStorageDir)
		if err != nil {
			log.Printf("WARNING: 素材对象存储不可用(%v),生成产物将无法转存", err)
		}

		gateway := serviceGateway(d.Config)
		wiring.CanvasRoutes(r, d.Config, wiring.CanvasStores{
			Auth:      store,
			Canvases:  canvases,
			Assets:    assets,
			Prices:    prices,
			Tasks:     tasks,
			Templates: templates,
		}, gateway, storage)

		lifetime := d.Lifetime
		if lifetime == nil {
			lifetime = context.Background()
		}
		startWorker(tasks, gateway, d.Config, storage, lifetime)
	})
}

// serviceGateway builds the gateway client when a service key is configured;
// nil leaves the handlers refusing generation submits with a clear error
// instead of queueing work that can never run.
func serviceGateway(cfg config.Config) canvastask.Gateway {
	if cfg.CanvasServiceKey == "" {
		log.Printf("WARNING: CANVAS_SERVICE_KEY 未设置,画布生成任务将无法提交")
		return nil
	}
	return canvastask.NewClient(cfg.GatewayBaseURL, cfg.CanvasServiceKey)
}

// startWorker recovers orphaned generations (running rows from a previous
// process go back to the queue) and drives the queue until lifetime is
// canceled (SIGINT/SIGTERM, see app.Run). storage 非 nil 时产物在终态前转存
// 对象存储.
func startWorker(tasks *canvastask.MySQLStore, gateway canvastask.Gateway, cfg config.Config, storage objectstore.Store, lifetime context.Context) {
	if gateway == nil {
		return
	}
	ctx := lifetime
	if n, err := tasks.RequeueRunning(context.Background()); err != nil {
		log.Printf("canvastask: requeue running: %v", err)
	} else if n > 0 {
		log.Printf("canvastask: requeued %d orphaned task(s) from the previous run", n)
	}
	opts := []canvastask.WorkerOption{canvastask.WithConcurrency(cfg.CanvasTaskConcurrency)}
	if storage != nil {
		opts = append(opts, canvastask.WithStorage(storage))
	}
	worker := canvastask.NewWorker(tasks, gateway, opts...)
	go worker.Run(ctx)
}
