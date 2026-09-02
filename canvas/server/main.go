// Command canvas-server runs the canvas persistence and task-orchestration API.
package main

import (
	"github.com/gin-gonic/gin"

	"github.com/gachal/InfiniteChance/internal/app"
	"github.com/gachal/InfiniteChance/internal/auth"
)

func main() {
	app.Run("canvas", "8081", func(r *gin.Engine, d app.Deps) {
		// 与网关共享 JWT_SECRET,校验网关签发的管理员会话。
		auth.RegisterMeRoute(r, auth.NewIssuerFromConfig(d.Config))
	})
}
