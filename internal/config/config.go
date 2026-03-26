// Package config loads server configuration from environment variables.
package config

import (
	"log/slog"
	"os"
	"strconv"
	"time"
)

const (
	minProviderTimeout = 100 * time.Millisecond
	maxProviderTimeout = 30 * time.Second
)

// Config holds application configuration.
type Config struct {
	Port            int
	LogLevel        slog.Level
	ProviderTimeout time.Duration
}

// Load reads config from environment with sensible defaults.
func Load() Config {
	return Config{
		Port:            envInt("PORT", 8080),
		LogLevel:        envLogLevel("LOG_LEVEL", slog.LevelInfo),
		ProviderTimeout: envDuration("PROVIDER_TIMEOUT", 5*time.Second),
	}
}

func envInt(key string, def int) int {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		slog.Warn("invalid config value, using default", "key", key, "value", v, "default", def)
		return def
	}
	return n
}

func envDuration(key string, def time.Duration) time.Duration {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		slog.Warn("invalid config value, using default", "key", key, "value", v, "default", def.String())
		return def
	}
	if d < minProviderTimeout || d > maxProviderTimeout {
		slog.Warn("config value out of range, clamping", "key", key, "value", d.String(),
			"min", minProviderTimeout.String(), "max", maxProviderTimeout.String())
		if d < minProviderTimeout {
			return minProviderTimeout
		}
		return maxProviderTimeout
	}
	return d
}

func envLogLevel(key string, def slog.Level) slog.Level {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	switch v {
	case "debug", "DEBUG":
		return slog.LevelDebug
	case "info", "INFO":
		return slog.LevelInfo
	case "warn", "WARN":
		return slog.LevelWarn
	case "error", "ERROR":
		return slog.LevelError
	default:
		slog.Warn("invalid config value, using default", "key", key, "value", v, "default", def.String())
		return def
	}
}
