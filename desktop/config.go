package main

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// appDirName is the per-OS application-data directory name. DataDir resolves
// to ~/Library/Application Support/InfiniteChance on macOS, %AppDATA%
// (Roaming) on Windows and $XDG_CONFIG_HOME/InfiniteChance on Linux.
const appDirName = "InfiniteChance"

// defaultPorts follow the Docker stack so an SDK base_url survives a move
// between the two deployment shapes (8080 gateway / 8081 canvas).
const (
	defaultGatewayPort = 8080
	defaultCanvasPort  = 8081
)

// Config is the desktop's local configuration, persisted as config.json in
// the data directory. Secrets live here in plaintext by design: the SQLite
// database sits in the same directory, so this is one trust domain — the
// same level as the Docker stack's .env (see CONTEXT.md 桌面版).
type Config struct {
	GatewayPort int    `json:"gateway_port"`
	CanvasPort  int    `json:"canvas_port"`
	JWTSecret   string `json:"jwt_secret"`
	// CanvasServiceKey is the auto-provisioned service key's full sk- value.
	// Empty until first boot provisions one (desktop/servicekey.go).
	CanvasServiceKey      string `json:"canvas_service_key"`
	CanvasTaskConcurrency int    `json:"canvas_task_concurrency"`
}

// DataDir returns (and creates) the per-OS application data directory.
// INFINITECHANCE_DATA_DIR overrides it wholesale — portable mode for power
// users and the hook tests use to run against a throwaway directory.
func DataDir() (string, error) {
	if override := os.Getenv("INFINITECHANCE_DATA_DIR"); override != "" {
		if err := os.MkdirAll(override, 0o755); err != nil {
			return "", fmt.Errorf("create data dir: %w", err)
		}
		return override, nil
	}
	root, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("resolve user config dir: %w", err)
	}
	dir := filepath.Join(root, appDirName)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("create data dir: %w", err)
	}
	return dir, nil
}

func configPath(dataDir string) string { return filepath.Join(dataDir, "config.json") }

// LoadConfig reads config.json (missing file = defaults), fills in zero
// values and mints a fresh JWT secret on first run. The secret is persisted
// by the caller via Save so sessions survive restarts; nothing here uses
// the built-in public dev secret.
func LoadConfig(dataDir string) (Config, error) {
	cfg := Config{
		GatewayPort:           defaultGatewayPort,
		CanvasPort:            defaultCanvasPort,
		CanvasTaskConcurrency: 2,
	}
	raw, err := os.ReadFile(configPath(dataDir))
	if errors.Is(err, os.ErrNotExist) {
		// First run: keep defaults below.
	} else if err != nil {
		return Config{}, fmt.Errorf("read config: %w", err)
	} else if err := json.Unmarshal(raw, &cfg); err != nil {
		return Config{}, fmt.Errorf("parse config: %w", err)
	}
	if cfg.GatewayPort <= 0 {
		cfg.GatewayPort = defaultGatewayPort
	}
	if cfg.CanvasPort <= 0 {
		cfg.CanvasPort = defaultCanvasPort
	}
	if cfg.CanvasTaskConcurrency <= 0 {
		cfg.CanvasTaskConcurrency = 2
	}
	if cfg.JWTSecret == "" {
		secret := make([]byte, 32)
		if _, err := rand.Read(secret); err != nil {
			return Config{}, fmt.Errorf("generate jwt secret: %w", err)
		}
		cfg.JWTSecret = hex.EncodeToString(secret)
	}
	return cfg, nil
}

// Save persists the config so first-run secrets survive restarts.
func (c Config) Save(dataDir string) error {
	raw, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(configPath(dataDir), raw, 0o600)
}
