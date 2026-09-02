// Package app wires one service binary together: env config, MySQL/Redis
// dependency pingers, the shared HTTP server — then blocks serving.
package app

import (
	"log"

	"github.com/gachal/InfiniteChance/internal/config"
	"github.com/gachal/InfiniteChance/internal/health"
	"github.com/gachal/InfiniteChance/internal/server"
)

// Run boots the service identified by name (reported by /healthz) on
// defaultPort unless PORT overrides it, and only returns on failure.
func Run(name, defaultPort string) {
	cfg := config.Load(name, defaultPort)

	db, err := health.OpenMySQL(cfg.MysqlDSN)
	if err != nil {
		log.Fatalf("open mysql: %v", err)
	}

	r := server.New(cfg.Name, map[string]health.Pinger{
		"mysql": health.MySQL{DB: db},
		"redis": health.Redis{Client: health.NewRedis(cfg.RedisAddr)},
	})

	log.Printf("%s listening on :%s", name, cfg.Port)
	if err := r.Run(":" + cfg.Port); err != nil {
		log.Fatal(err)
	}
}
