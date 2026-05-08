package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	DatabaseURL string
	Port        string
	LogLevel    string
	DataDir     string
	SchemaPath  string
	StartupWait time.Duration
}

func Load() Config {
	cfg := Config{
		DatabaseURL: getenv("DATABASE_URL", "postgres://app:app@localhost:5432/app?sslmode=disable"),
		Port:        getenv("PORT", "8080"),
		LogLevel:    strings.ToLower(getenv("LOG_LEVEL", "info")),
		DataDir:     getenv("DATA_DIR", "data"),
		SchemaPath:  getenv("SCHEMA_PATH", ""),
		StartupWait: 60 * time.Second,
	}

	if value := os.Getenv("STARTUP_WAIT_SECONDS"); value != "" {
		if seconds, err := strconv.Atoi(value); err == nil && seconds > 0 {
			cfg.StartupWait = time.Duration(seconds) * time.Second
		}
	}

	return cfg
}

func getenv(key, fallback string) string {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	return value
}

func (c Config) Addr() string {
	return fmt.Sprintf(":%s", c.Port)
}
