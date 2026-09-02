// Command canvas-server runs the canvas persistence and task-orchestration API.
package main

import (
	"log"

	"github.com/gachal/InfiniteChance/internal/config"
	"github.com/gachal/InfiniteChance/internal/health"
	"github.com/gachal/InfiniteChance/internal/server"
)

func main() {
	cfg := config.Load("canvas", "8081")

	db, err := health.OpenMySQL(cfg.MysqlDSN)
	if err != nil {
		log.Fatalf("open mysql: %v", err)
	}

	r := server.New(cfg.Name, map[string]health.Pinger{
		"mysql": health.MySQL{DB: db},
		"redis": health.Redis{Client: health.NewRedis(cfg.RedisAddr)},
	})

	log.Printf("canvas listening on :%s", cfg.Port)
	if err := r.Run(":" + cfg.Port); err != nil {
		log.Fatal(err)
	}
}
