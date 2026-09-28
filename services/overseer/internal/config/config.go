package config

import (
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	Env      string
	LogLevel string
	Host     string
	Port     int

	OwnerUserID string

	SupabaseURL string

	SupabaseAnonKey string

	SupabaseJWTSecret string

	SupabaseJWKSURL string

	BasePath string

	TickInterval time.Duration

	BucketTimeout time.Duration

	CostSpendInterval time.Duration

	HistoryPath string
}

func Load() (*Config, error) {
	c := &Config{
		Env:               getenv("OVERSEER_ENV", "development"),
		LogLevel:          getenv("OVERSEER_LOG_LEVEL", "INFO"),
		Host:              getenv("OVERSEER_HOST", "0.0.0.0"),
		OwnerUserID:       strings.TrimSpace(os.Getenv("OVERSEER_OWNER_USER_ID")),
		SupabaseURL:       strings.TrimSpace(os.Getenv("OVERSEER_SUPABASE_URL")),
		SupabaseAnonKey:   strings.TrimSpace(os.Getenv("OVERSEER_SUPABASE_ANON_KEY")),
		SupabaseJWTSecret: strings.TrimSpace(os.Getenv("OVERSEER_SUPABASE_JWT_SECRET")),
		SupabaseJWKSURL:   strings.TrimSpace(os.Getenv("OVERSEER_SUPABASE_JWKS_URL")),
		BasePath:          normalizeBasePath(os.Getenv("OVERSEER_BASE_PATH")),
		HistoryPath:       getenv("OVERSEER_HISTORY_PATH", "/var/lib/overseer/history.db"),
	}
	if err := c.applyPort(); err != nil {
		return nil, err
	}
	if err := c.applyDurations(); err != nil {
		return nil, err
	}
	if err := c.validate(); err != nil {
		return nil, err
	}
	return c, nil
}

func (c *Config) applyPort() error {
	raw := getenv("OVERSEER_PORT", "8090")
	port, err := strconv.Atoi(raw)
	if err != nil || port < 1 || port > 65535 {
		return fmt.Errorf("OVERSEER_PORT must be a valid TCP port, got %q", raw)
	}
	c.Port = port
	return nil
}

func (c *Config) applyDurations() error {
	tick, err := positiveDuration("OVERSEER_TICK_INTERVAL", 5*time.Second)
	if err != nil {
		return err
	}
	bucketTimeout, err := positiveDuration("OVERSEER_BUCKET_TIMEOUT", 8*time.Second)
	if err != nil {
		return err
	}
	costSpend, err := positiveDuration("OVERSEER_COST_SPEND_INTERVAL", time.Hour)
	if err != nil {
		return err
	}
	c.TickInterval, c.BucketTimeout, c.CostSpendInterval = tick, bucketTimeout, costSpend
	return nil
}

func positiveDuration(key string, fallback time.Duration) (time.Duration, error) {
	raw := os.Getenv(key)
	if raw == "" {
		return fallback, nil
	}
	d, err := time.ParseDuration(raw)
	if err != nil || d <= 0 {
		return 0, fmt.Errorf("%s must be a positive duration, got %q", key, raw)
	}
	return d, nil
}

func (c *Config) validate() error {
	if c.OwnerUserID == "" {
		return fmt.Errorf("OVERSEER_OWNER_USER_ID must be set (the single allowlisted owner; owner-only rejects every request without it)")
	}
	if c.SupabaseURL == "" {
		return fmt.Errorf("OVERSEER_SUPABASE_URL must be set (used to verify Supabase JWTs and for SPA login)")
	}
	if c.SupabaseAnonKey == "" {
		return fmt.Errorf("OVERSEER_SUPABASE_ANON_KEY must be set (the public key the SPA login needs)")
	}
	return nil
}

func (c *Config) JWKSURL() string {
	if c.SupabaseJWKSURL != "" {
		return c.SupabaseJWKSURL
	}
	return c.authBaseURL() + "/.well-known/jwks.json"
}

func (c *Config) IssuerURL() string {
	return c.authBaseURL()
}

func (c *Config) authBaseURL() string {
	return strings.TrimRight(c.SupabaseURL, "/") + "/auth/v1"
}

func (c *Config) IsDevelopment() bool {
	return c.Env == "development"
}

func (c *Config) LogValue() slog.Value {
	return slog.GroupValue(
		slog.String("env", c.Env),
		slog.String("host", c.Host),
		slog.Int("port", c.Port),
		slog.String("base_path", c.BasePath),
		slog.String("owner_user_id", c.OwnerUserID),
		slog.String("supabase_url", c.SupabaseURL),
		slog.Bool("has_anon_key", c.SupabaseAnonKey != ""),
		slog.Bool("has_jwt_secret", c.SupabaseJWTSecret != ""),
		slog.Duration("tick_interval", c.TickInterval),
		slog.Duration("bucket_timeout", c.BucketTimeout),
		slog.Duration("cost_spend_interval", c.CostSpendInterval),
		slog.String("history_path", c.HistoryPath),
	)
}

func normalizeBasePath(raw string) string {
	p := strings.TrimSpace(raw)
	if p == "" {
		return ""
	}
	p = "/" + strings.TrimLeft(p, "/")
	p = strings.TrimRight(p, "/")
	return p
}

func getenv(key, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return fallback
}
