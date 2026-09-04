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
	"github.com/gachal/InfiniteChance/internal/promptgen"
	"github.com/gachal/InfiniteChance/internal/prompttemplate"
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

		issuer := auth.NewIssuerFromConfig(d.Config)
		auth.RegisterRoutes(r, &auth.Handlers{Store: store, Issuer: issuer})

		// 画布面:一律先过 JWT 会话,与管理面同一套令牌体系。
		gateway := serviceGateway(d.Config)
		group := r.Group("/canvases", auth.RequireAuth(issuer))
		canvas.RegisterRoutes(group, &canvas.Handlers{Store: canvases})
		canvastask.RegisterRoutes(group, &canvastask.Handlers{
			Tasks:    tasks,
			Canvases: canvases,
			Models:   prices,
			Assets:   assets,
			Gateway:  gateway,
		})
		// 提示词生成与画布任务共用同一服务 key:client 为 nil 时动作
		// 直接以 gateway_unconfigured 拒绝(serviceGateway 已打警告)。
		var chatGateway promptgen.Gateway
		if gateway != nil {
			chatGateway = promptgen.NewClient(d.Config.GatewayBaseURL, d.Config.CanvasServiceKey)
		}
		promptgen.RegisterRoutes(group, &promptgen.Handlers{
			Templates: templates,
			Canvases:  canvases,
			Assets:    assets,
			Models:    prices,
			Gateway:   chatGateway,
		})

		// 素材内容寻址例外 —— 节点用 <img>/<video> 预览,带不了
		// Authorization 头(见 asset 包);素材库的列表/删除挂 JWT 会话,
		// 画布素材面板与管理端素材页共用。
		canvastask.RegisterModelRoutes(r.Group("/image-models", auth.RequireAuth(issuer)),
			&canvastask.ModelHandlers{Prices: prices})
		canvastask.RegisterVideoModelRoutes(r.Group("/video-models", auth.RequireAuth(issuer)),
			&canvastask.ModelHandlers{Prices: prices})
		promptgen.RegisterCatalogRoutes(r.Group("/prompt-templates", auth.RequireAuth(issuer)),
			&promptgen.CatalogHandlers{Templates: templates})
		promptgen.RegisterModelRoutes(r.Group("/prompt-models", auth.RequireAuth(issuer)),
			&promptgen.ModelHandlers{Prices: prices})
		asset.RegisterContentRoutes(r.Group("/assets"), &asset.Handlers{Store: assets, Storage: storage})
		asset.RegisterLibraryRoutes(r.Group("/assets", auth.RequireAuth(issuer)),
			&asset.Handlers{Store: assets, Storage: storage})

		startWorker(tasks, gateway, d.Config, storage)
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
// process go back to the queue) and drives the queue in the background for
// the lifetime of the process. storage 非 nil 时产物在终态前转存对象存储.
func startWorker(tasks *canvastask.MySQLStore, gateway canvastask.Gateway, cfg config.Config, storage objectstore.Store) {
	if gateway == nil {
		return
	}
	ctx := context.Background()
	if n, err := tasks.RequeueRunning(ctx); err != nil {
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
