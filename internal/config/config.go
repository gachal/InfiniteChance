// Package config loads service configuration from environment variables.
//
// Infrastructure addresses default to localhost so the binaries also run on
// the host against `docker compose up mysql redis`.
package config

import "os"

type Config struct {
	Name      string // service name reported by /healthz
	Port      string
	MysqlDSN  string
	RedisAddr string
}

// Load reads configuration from the environment. name and defaultPort are
// fixed per binary; everything else is env-driven.
func Load(name, defaultPort string) Config {
	return Config{
		Name:      name,
		Port:      envOr("PORT", defaultPort),
		MysqlDSN:  envOr("MYSQL_DSN", "root:infinitechance@tcp(localhost:3306)/infinitechance?parseTime=true"),
		RedisAddr: envOr("REDIS_ADDR", "localhost:6379"),
	}
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
