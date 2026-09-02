// Package app wires one service binary together: env config, MySQL/Redis
// dependency pingers, the shared HTTP server — then blocks serving.
package app

import (
	"database/sql"
	"log"

	"github.com/gin-gonic/gin"

	"github.com/gachal/InfiniteChance/internal/config"
	"github.com/gachal/InfiniteChance/internal/health"
	"github.com/gachal/InfiniteChance/internal/server"
)

// Deps carries the wired dependencies to a binary's route registration.
type Deps struct {
	Config config.Config
	DB     *sql.DB
}

// Run boots the service identified by name (reported by /healthz) on
// defaultPort unless PORT overrides it, hands the engine to register for
// binary-specific routes, and only returns on failure.
func Run(name, defaultPort string, register func(*gin.Engine, Deps)) {
	cfg := config.Load(name, defaultPort)
	if cfg.JWTSecretInsecure {
		log.Printf("WARNING: JWT_SECRET 未设置,正在使用内置开发密钥;跨服务令牌校验与公网部署必须设置 JWT_SECRET")
	}

	db, err := health.OpenMySQL(cfg.MysqlDSN)
	if err != nil {
		log.Fatalf("open mysql: %v", err)
	}

	r := server.New(cfg.Name, map[string]health.Pinger{
		"mysql": health.MySQL{DB: db},
		"redis": health.Redis{Client: health.NewRedis(cfg.RedisAddr)},
	})
	if register != nil {
		register(r, Deps{Config: cfg, DB: db})
	}

	log.Printf("%s listening on :%s", name, cfg.Port)
	if err := r.Run(":" + cfg.Port); err != nil {
		log.Fatal(err)
	}
}
