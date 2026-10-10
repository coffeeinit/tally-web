package config

import (
	"os"
	"path/filepath"
	"strconv"
)

// Config is everything the server needs to boot.
type Config struct {
	Port         int
	RegistryPath string
	WebDir       string
	DataDir      string
	AllowWrites  bool
}

func Load() Config {
	dataDir := env("TWUI_DATA_DIR", "data")
	return Config{
		Port:         envInt("TWUI_PORT", 8787),
		RegistryPath: env("TWUI_REGISTRY", "tally-instances.json"),
		WebDir:       env("TWUI_WEB_DIR", filepath.Join("..", "frontend", "dist")),
		DataDir:      dataDir,
		AllowWrites:  envBool("TWUI_ALLOW_WRITES", false),
	}
}

func env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func envInt(key string, def int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}

func envBool(key string, def bool) bool {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	return v == "1" || v == "true" || v == "yes"
}
