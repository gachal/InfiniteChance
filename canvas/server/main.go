// Command canvas-server runs the canvas persistence and task-orchestration API.
package main

import (
	"context"
	"log"

	"github.com/gin-gonic/gin"

	"github.com/gachal/InfiniteChance/internal/app"
	"github.com/gachal/InfiniteChance/internal/auth"
	"github.com/gachal/InfiniteChance/internal/canvas"
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

		issuer := auth.NewIssuerFromConfig(d.Config)
		auth.RegisterRoutes(r, &auth.Handlers{Store: store, Issuer: issuer})

		// 画布面:一律先过 JWT 会话,与管理面同一套令牌体系。
		group := r.Group("/canvases", auth.RequireAuth(issuer))
		canvas.RegisterRoutes(group, &canvas.Handlers{Store: canvases})
	})
}
