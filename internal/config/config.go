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
	// JWTSecret signs admin session tokens. Gateway and canvas must run
	// with the same value for canvas to accept gateway-issued tokens.
	JWTSecret string
	// JWTSecretInsecure reports that JWT_SECRET was not set and the
	// built-in dev secret is in use — surfaces as a startup warning.
	JWTSecretInsecure bool
}

// devJWTSecret keeps host-side dev runs working with zero configuration.
// It is deliberately public; real deployments set JWT_SECRET.
const devJWTSecret = "infinitechance-dev-jwt-secret"

// Load reads configuration from the environment. name and defaultPort are
// fixed per binary; everything else is env-driven.
func Load(name, defaultPort string) Config {
	jwtSecret := os.Getenv("JWT_SECRET")
	return Config{
		Name:              name,
		Port:              envOr("PORT", defaultPort),
		MysqlDSN:          envOr("MYSQL_DSN", "root:infinitechance@tcp(localhost:3306)/infinitechance?parseTime=true"),
		RedisAddr:         envOr("REDIS_ADDR", "localhost:6379"),
		JWTSecret:         envOr("JWT_SECRET", devJWTSecret),
		JWTSecretInsecure: jwtSecret == "",
	}
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
