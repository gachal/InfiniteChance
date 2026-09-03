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
	"github.com/gachal/InfiniteChance/internal/pricing"
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
			Gateway:  gateway,
		})

		// 生成模型的目录(按次计价的公开模型)挂 JWT 会话;素材内容寻址
		// 例外 —— 节点用 <img> 预览,带不了 Authorization 头(见 asset 包)。
		canvastask.RegisterModelRoutes(r.Group("/image-models", auth.RequireAuth(issuer)),
			&canvastask.ModelHandlers{Prices: prices})
		asset.RegisterRoutes(r.Group("/assets"), &asset.Handlers{Store: assets})

		startWorker(tasks, gateway, d.Config)
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
// the lifetime of the process.
func startWorker(tasks *canvastask.MySQLStore, gateway canvastask.Gateway, cfg config.Config) {
	if gateway == nil {
		return
	}
	ctx := context.Background()
	if n, err := tasks.RequeueRunning(ctx); err != nil {
		log.Printf("canvastask: requeue running: %v", err)
	} else if n > 0 {
		log.Printf("canvastask: requeued %d orphaned task(s) from the previous run", n)
	}
	worker := canvastask.NewWorker(tasks, gateway,
		canvastask.WithConcurrency(cfg.CanvasTaskConcurrency))
	go worker.Run(ctx)
}
