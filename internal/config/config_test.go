package config_test

import (
	"log/slog"
	"testing"
	"time"

	"github.com/graffhyrum/warpgate/internal/config"
)

func TestLoad_Defaults(t *testing.T) {
	t.Parallel()
	cfg := config.Load()

	if cfg.Port != 8080 {
		t.Errorf("expected default port 8080, got %d", cfg.Port)
	}
	if cfg.LogLevel != slog.LevelInfo {
		t.Errorf("expected default log level Info, got %v", cfg.LogLevel)
	}
	if cfg.ProviderTimeout != 5*time.Second {
		t.Errorf("expected default timeout 5s, got %v", cfg.ProviderTimeout)
	}
}

func TestLoad_FromEnv(t *testing.T) {
	t.Setenv("PORT", "9090")
	t.Setenv("LOG_LEVEL", "debug")
	t.Setenv("PROVIDER_TIMEOUT", "10s")

	cfg := config.Load()

	if cfg.Port != 9090 {
		t.Errorf("expected port 9090, got %d", cfg.Port)
	}
	if cfg.LogLevel != slog.LevelDebug {
		t.Errorf("expected log level Debug, got %v", cfg.LogLevel)
	}
	if cfg.ProviderTimeout != 10*time.Second {
		t.Errorf("expected timeout 10s, got %v", cfg.ProviderTimeout)
	}
}

func TestLoad_InvalidValues_FallToDefaults(t *testing.T) {
	t.Setenv("PORT", "not-a-number")
	t.Setenv("LOG_LEVEL", "bogus")
	t.Setenv("PROVIDER_TIMEOUT", "nope")

	cfg := config.Load()

	if cfg.Port != 8080 {
		t.Errorf("expected fallback port 8080, got %d", cfg.Port)
	}
	if cfg.LogLevel != slog.LevelInfo {
		t.Errorf("expected fallback log level Info, got %v", cfg.LogLevel)
	}
	if cfg.ProviderTimeout != 5*time.Second {
		t.Errorf("expected fallback timeout 5s, got %v", cfg.ProviderTimeout)
	}
}

func TestLoad_TimeoutClamped(t *testing.T) {
	t.Setenv("PROVIDER_TIMEOUT", "0s")
	cfg := config.Load()
	if cfg.ProviderTimeout != 100*time.Millisecond {
		t.Errorf("expected clamped to 100ms, got %v", cfg.ProviderTimeout)
	}

	t.Setenv("PROVIDER_TIMEOUT", "60s")
	cfg = config.Load()
	if cfg.ProviderTimeout != 30*time.Second {
		t.Errorf("expected clamped to 30s, got %v", cfg.ProviderTimeout)
	}
}
