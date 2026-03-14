package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

type Config struct {
	AppURL   string `yaml:"app_url"`
	Env      string `yaml:"env"`
	Debug    bool   `yaml:"debug"`
	LogLevel string `yaml:"log_level"`

	SentryDSN string `yaml:"sentry_dsn"`

	DB        DBConfig        `yaml:"db"`
	Server    ServerConfig    `yaml:"server"`
	R2        R2Config        `yaml:"r2"`
	Session   SessionConfig   `yaml:"session"`
	Telemetry TelemetryConfig `yaml:"telemetry"`
	Discovery DiscoveryConfig `yaml:"discovery"`
}

type DBConfig struct {
	Path string `yaml:"path"`
}

type ServerConfig struct {
	Addr           string   `yaml:"addr"`
	AdminAllowCIDR []string `yaml:"admin_allow_cidr"`
	TrustProxy     bool     `yaml:"trust_proxy"`
}

type R2Config struct {
	AccessKeyID     string `yaml:"access_key_id"`
	SecretAccessKey string `yaml:"secret_access_key"`
	Bucket          string `yaml:"bucket"`
	Endpoint        string `yaml:"endpoint"`
	Enabled         bool   `yaml:"enabled"`
	CDNBucket       string `yaml:"cdn_bucket"`
	CDNPublicURL    string `yaml:"cdn_public_url"`
}

type SessionConfig struct {
	LifetimeMinutes int `yaml:"lifetime_minutes"`
}

type TelemetryConfig struct {
	DedupeWindowSeconds int `yaml:"dedupe_window_seconds"`
}

type DiscoveryConfig struct {
	SeedsFile    string `yaml:"seeds_file"`
	Concurrency  int    `yaml:"concurrency"`
	APITimeoutS  int    `yaml:"api_timeout"`
	MaxRetries   int    `yaml:"max_retries"`
	RetryDelayMs int    `yaml:"retry_delay_ms"`
	SVNTimeoutS  int    `yaml:"svn_timeout"`
}

func defaults() *Config {
	return &Config{
		Env:      "local",
		LogLevel: "info",
		DB:       DBConfig{Path: "./storage/wpcomposer.db"},
		Server: ServerConfig{
			Addr:           ":8080",
			AdminAllowCIDR: []string{"100.64.0.0/10", "fd7a:115c:a1e0::/48"}, // Tailscale default ranges
		},
		Session: SessionConfig{LifetimeMinutes: 7200},
		Telemetry: TelemetryConfig{
			DedupeWindowSeconds: 3600,
		},
		Discovery: DiscoveryConfig{
			SeedsFile:    "./seeds.yaml",
			Concurrency:  10,
			APITimeoutS:  30,
			MaxRetries:   3,
			RetryDelayMs: 1000,
			SVNTimeoutS:  600,
		},
	}
}

func Load(path string) (*Config, error) {
	cfg := defaults()

	if path != "" {
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("reading config file: %w", err)
		}
		if err := yaml.Unmarshal(data, cfg); err != nil {
			return nil, fmt.Errorf("parsing config file: %w", err)
		}
	}

	applyEnv(cfg)
	return cfg, nil
}

func applyEnv(cfg *Config) {
	if v := os.Getenv("APP_URL"); v != "" {
		cfg.AppURL = v
	}
	if v := os.Getenv("APP_ENV"); v != "" {
		cfg.Env = v
	}
	if v := os.Getenv("APP_DEBUG"); v != "" {
		cfg.Debug = strings.EqualFold(v, "true") || v == "1"
	}
	if v := os.Getenv("LOG_LEVEL"); v != "" {
		cfg.LogLevel = v
	}
	if v := os.Getenv("SENTRY_DSN"); v != "" {
		cfg.SentryDSN = v
	}
	if v := os.Getenv("DB_PATH"); v != "" {
		cfg.DB.Path = v
	}
	if v := os.Getenv("LISTEN_ADDR"); v != "" {
		cfg.Server.Addr = v
	}
	if v := os.Getenv("R2_ACCESS_KEY_ID"); v != "" {
		cfg.R2.AccessKeyID = v
	}
	if v := os.Getenv("R2_SECRET_ACCESS_KEY"); v != "" {
		cfg.R2.SecretAccessKey = v
	}
	if v := os.Getenv("R2_BUCKET"); v != "" {
		cfg.R2.Bucket = v
	}
	if v := os.Getenv("R2_ENDPOINT"); v != "" {
		cfg.R2.Endpoint = v
	}
	if v := os.Getenv("WP_COMPOSER_DEPLOY_R2"); v != "" {
		cfg.R2.Enabled = strings.EqualFold(v, "true") || v == "1"
	}
	if v := os.Getenv("R2_CDN_BUCKET"); v != "" {
		cfg.R2.CDNBucket = v
	}
	if v := os.Getenv("R2_CDN_PUBLIC_URL"); v != "" {
		cfg.R2.CDNPublicURL = v
	}
	if v := os.Getenv("SESSION_LIFETIME_MINUTES"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			cfg.Session.LifetimeMinutes = n
		}
	}
	if v := os.Getenv("TELEMETRY_DEDUPE_WINDOW_SECONDS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			cfg.Telemetry.DedupeWindowSeconds = n
		}
	}
	if v := os.Getenv("TRUST_PROXY"); v != "" {
		cfg.Server.TrustProxy = strings.EqualFold(v, "true") || v == "1"
	}
	if v, ok := os.LookupEnv("ADMIN_ALLOW_CIDR"); ok {
		if v == "" {
			cfg.Server.AdminAllowCIDR = nil // explicitly disable restriction
		} else {
			cfg.Server.AdminAllowCIDR = strings.Split(v, ",")
		}
	}
	if v := os.Getenv("SEEDS_FILE"); v != "" {
		cfg.Discovery.SeedsFile = v
	}
	if v := os.Getenv("DISCOVERY_CONCURRENCY"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			cfg.Discovery.Concurrency = n
		}
	}
}
