// Package config loads service configuration from environment variables.
//
// Infrastructure addresses default to localhost so the binaries also run on
// the host against `docker compose up mysql redis`.
package config

import (
	"os"
	"strings"
)

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
	// JWTSecretRequired comes from JWT_SECRET_REQUIRED: when enabled, boot
	// refuses the built-in dev secret so production cannot ship a public
	// signing key by accident.
	JWTSecretRequired bool
}

// devJWTSecret keeps host-side dev runs working with zero configuration.
// It is deliberately public; real deployments set JWT_SECRET (and should
// also set JWT_SECRET_REQUIRED to make a forgotten secret fail the boot).
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
		JWTSecretRequired: boolEnv("JWT_SECRET_REQUIRED"),
	}
}

// boolEnv accepts 1/true/yes/on (any case) as true.
func boolEnv(key string) bool {
	switch strings.ToLower(os.Getenv(key)) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
