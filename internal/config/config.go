// Package config loads server settings from BULLETIN_* environment variables.
package config

import (
	"fmt"
	"os"
	"strconv"
	"time"
)

// Config holds server settings.
type Config struct {
	DevMode              bool // BULLETIN_DEV_MODE: seed demo content on an empty DB
	ListenHost           string
	ListenPort           int
	HostKeyPath          string
	IdleTimeout          time.Duration
	MaxSessionsPerKey    int
	MaxSessionsPerSource int
	MaxConnections       int
	LogLevel             string
	LogFormat            string
	RateLimitPerSecond   float64
	RateLimitBurst       int
	RateLimitMaxEntries  int
	DBPath               string
	OwnerFingerprint     string
	ProxyKeysPath        string // trusted arcade proxy keys; empty = direct-only dev
}

// Load reads configuration from the environment with documented defaults.
func Load() (Config, error) {
	var err error
	cfg := Config{
		ListenHost:       envOr("BULLETIN_LISTEN_HOST", "0.0.0.0"),
		HostKeyPath:      envOr("BULLETIN_HOST_KEY_PATH", "var/ssh_host_key"),
		LogLevel:         envOr("BULLETIN_LOG_LEVEL", "info"),
		LogFormat:        envOr("BULLETIN_LOG_FORMAT", "text"),
		DBPath:           envOr("BULLETIN_DB_PATH", "var/bulletin.db"),
		OwnerFingerprint: os.Getenv("BULLETIN_OWNER_FINGERPRINT"),
		ProxyKeysPath:    os.Getenv("BULLETIN_PROXY_KEYS_PATH"),
	}
	if cfg.DevMode, err = envBoolOr("BULLETIN_DEV_MODE", false); err != nil {
		return Config{}, err
	}
	if cfg.ListenPort, err = envIntOr("BULLETIN_LISTEN_PORT", 22); err != nil {
		return Config{}, err
	}
	if cfg.IdleTimeout, err = envDurationOr("BULLETIN_IDLE_TIMEOUT", 30*time.Minute); err != nil {
		return Config{}, err
	}
	if cfg.MaxSessionsPerKey, err = envIntOr("BULLETIN_MAX_SESSIONS_PER_KEY", 3); err != nil {
		return Config{}, err
	}
	if cfg.MaxSessionsPerSource, err = envIntOr("BULLETIN_MAX_SESSIONS_PER_SOURCE", 6); err != nil {
		return Config{}, err
	}
	if cfg.MaxConnections, err = envIntOr("BULLETIN_MAX_CONNECTIONS", 100); err != nil {
		return Config{}, err
	}
	if cfg.RateLimitPerSecond, err = envFloatOr("BULLETIN_RATE_LIMIT_PER_SECOND", 2); err != nil {
		return Config{}, err
	}
	if cfg.RateLimitBurst, err = envIntOr("BULLETIN_RATE_LIMIT_BURST", 5); err != nil {
		return Config{}, err
	}
	if cfg.RateLimitMaxEntries, err = envIntOr("BULLETIN_RATE_LIMIT_MAX_IPS", 1000); err != nil {
		return Config{}, err
	}

	switch {
	case cfg.ListenPort < 1 || cfg.ListenPort > 65535:
		return Config{}, fmt.Errorf("BULLETIN_LISTEN_PORT must be 1-65535, got %d", cfg.ListenPort)
	case cfg.MaxSessionsPerKey < 1:
		return Config{}, fmt.Errorf("BULLETIN_MAX_SESSIONS_PER_KEY must be at least 1")
	case cfg.MaxSessionsPerSource < 1:
		return Config{}, fmt.Errorf("BULLETIN_MAX_SESSIONS_PER_SOURCE must be at least 1")
	case cfg.MaxConnections < 1:
		return Config{}, fmt.Errorf("BULLETIN_MAX_CONNECTIONS must be at least 1")
	case cfg.RateLimitPerSecond <= 0:
		return Config{}, fmt.Errorf("BULLETIN_RATE_LIMIT_PER_SECOND must be positive")
	case cfg.RateLimitBurst < 1:
		return Config{}, fmt.Errorf("BULLETIN_RATE_LIMIT_BURST must be at least 1")
	case cfg.IdleTimeout < 0:
		return Config{}, fmt.Errorf("BULLETIN_IDLE_TIMEOUT must be zero or positive")
	case cfg.DBPath == "":
		return Config{}, fmt.Errorf("BULLETIN_DB_PATH must not be empty")
	}
	return cfg, nil
}

// ListenAddr is host:port for the SSH listener.
func (c Config) ListenAddr() string { return fmt.Sprintf("%s:%d", c.ListenHost, c.ListenPort) }

// envBoolOr rejects anything strconv.ParseBool does not accept, rather than
// silently treating a typo as false.
func envBoolOr(key string, fallback bool) (bool, error) {
	v := os.Getenv(key)
	if v == "" {
		return fallback, nil
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		return false, fmt.Errorf("%s must be a boolean (true/false), got %q", key, v)
	}
	return b, nil
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func envIntOr(key string, fallback int) (int, error) {
	v := os.Getenv(key)
	if v == "" {
		return fallback, nil
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return 0, fmt.Errorf("%s: invalid integer %q", key, v)
	}
	return n, nil
}

func envFloatOr(key string, fallback float64) (float64, error) {
	v := os.Getenv(key)
	if v == "" {
		return fallback, nil
	}
	f, err := strconv.ParseFloat(v, 64)
	if err != nil {
		return 0, fmt.Errorf("%s: invalid float %q", key, v)
	}
	return f, nil
}

func envDurationOr(key string, fallback time.Duration) (time.Duration, error) {
	v := os.Getenv(key)
	if v == "" {
		return fallback, nil
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		return 0, fmt.Errorf("%s: invalid duration %q", key, v)
	}
	return d, nil
}
