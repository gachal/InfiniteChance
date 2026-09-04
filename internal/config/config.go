// Package config loads service configuration from environment variables.
//
// Infrastructure addresses default to localhost so the binaries also run on
// the host against `docker compose up mysql redis`.
package config

import (
	"os"
	"strconv"
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
	// GatewayBaseURL points canvas/server at the gateway's relay surface
	// (CANVAS_GATEWAY_URL). The gateway ignores it.
	GatewayBaseURL string
	// CanvasServiceKey is the canvas's service-level API key for the gateway
	// (CANVAS_SERVICE_KEY). Empty = canvas/server refuses generation submits
	// with gateway_unconfigured instead of queueing unworkable work. The
	// gateway ignores it.
	CanvasServiceKey string
	// CanvasTaskConcurrency caps the canvas worker's parallel generations
	// (CANVAS_TASK_CONCURRENCY). The gateway ignores it.
	CanvasTaskConcurrency int
	// AssetStorageDir is the object-storage root for generated artifacts
	// (ASSET_STORAGE_DIR, canvas only). 14 号票:S3 兼容接口的 MVP 落地
	// 是本地卷,切 MinIO/云 OSS 时由驱动实现替换,配置项语义不变。
	AssetStorageDir string
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
		Name:                  name,
		Port:                  envOr("PORT", defaultPort),
		MysqlDSN:              envOr("MYSQL_DSN", "root:infinitechance@tcp(localhost:3306)/infinitechance?parseTime=true"),
		RedisAddr:             envOr("REDIS_ADDR", "localhost:6379"),
		JWTSecret:             envOr("JWT_SECRET", devJWTSecret),
		JWTSecretInsecure:     jwtSecret == "",
		JWTSecretRequired:     boolEnv("JWT_SECRET_REQUIRED"),
		GatewayBaseURL:        envOr("CANVAS_GATEWAY_URL", "http://localhost:8080"),
		CanvasServiceKey:      os.Getenv("CANVAS_SERVICE_KEY"),
		CanvasTaskConcurrency: intEnv("CANVAS_TASK_CONCURRENCY", 2),
		AssetStorageDir:       envOr("ASSET_STORAGE_DIR", "./data/assets"),
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

// intEnv reads a positive integer, falling back to def when unset or
// malformed — a bad knob value degrades to the default, never to zero work.
func intEnv(key string, def int) int {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			return n
		}
	}
	return def
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
