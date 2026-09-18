// Package config loads Overseer's runtime configuration from the environment,
// mirroring go-api's env-driven approach (see services/go-api/internal/shared/
// config) but with an OVERSEER_ prefix so the two services can share a host.
package config

import (
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"time"
)

// Config is Overseer's fully-resolved configuration.
type Config struct {
	Env      string
	LogLevel string
	Host     string
	Port     int

	// OwnerUserID is the single Supabase user id (the JWT `sub` claim) allowed to
	// reach Overseer data. It IS the access control — one allowlisted id, no RBAC.
	// Required: without it every data request is rejected and the service will not
	// start (fail closed).
	OwnerUserID string

	// SupabaseURL is the Supabase project URL. It serves two purposes: the SPA's
	// supabase-js client logs in against it, and Overseer derives the JWKS endpoint
	// ({SupabaseURL}/auth/v1/.well-known/jwks.json) from it to verify JWT
	// signatures locally. Required.
	SupabaseURL string

	// SupabaseAnonKey is the Supabase publishable anon key. It is a public client
	// value (safe in the browser) the SPA needs for supabase-js login. Required so
	// the login screen can function.
	SupabaseAnonKey string

	// SupabaseJWTSecret is the optional legacy HS256 JWT secret. When set, Overseer
	// also accepts HS256-signed Supabase tokens verified with it. Modern Supabase
	// projects use asymmetric keys served via JWKS and need no secret here.
	SupabaseJWTSecret string

	// SupabaseJWKSURL optionally overrides the derived JWKS endpoint. Empty means
	// derive it from SupabaseURL.
	SupabaseJWKSURL string

	// BasePath is the URL prefix Overseer is mounted under (e.g. "/overseer"). It is
	// used only to build outbound paths so they land back inside the mount when a
	// reverse proxy strips the prefix. Optional, defaults to "" (rootless).
	BasePath string

	// TickInterval is how often each bucket's collect cycle runs.
	TickInterval time.Duration
}

// Load reads configuration from the environment, applies defaults and validates
// it. A returned error must abort startup.
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
		TickInterval:      5 * time.Second,
	}
	if err := c.applyPort(); err != nil {
		return nil, err
	}
	if err := c.applyTick(); err != nil {
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

func (c *Config) applyTick() error {
	raw := os.Getenv("OVERSEER_TICK_INTERVAL")
	if raw == "" {
		return nil
	}
	d, err := time.ParseDuration(raw)
	if err != nil || d <= 0 {
		return fmt.Errorf("OVERSEER_TICK_INTERVAL must be a positive duration, got %q", raw)
	}
	c.TickInterval = d
	return nil
}

// validate enforces the owner-only boundary at startup: the allowlisted owner id
// and the Supabase project settings the auth guard and the SPA login both need
// must all be present, or the service fails closed rather than shipping an open or
// unusable dashboard.
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

// JWKSURL returns the JWKS endpoint the JWT verifier fetches signing keys from:
// the explicit override when set, else the standard Supabase GoTrue path derived
// from the project URL.
func (c *Config) JWKSURL() string {
	if c.SupabaseJWKSURL != "" {
		return c.SupabaseJWKSURL
	}
	return c.authBaseURL() + "/.well-known/jwks.json"
}

// IssuerURL returns the iss claim the project's GoTrue stamps on its tokens,
// which the verifier binds every accepted token to. It ignores the JWKS override:
// that says where keys are fetched from, never who issued the token.
func (c *Config) IssuerURL() string {
	return c.authBaseURL()
}

func (c *Config) authBaseURL() string {
	return strings.TrimRight(c.SupabaseURL, "/") + "/auth/v1"
}

// IsDevelopment reports whether the service runs in the development environment,
// which selects human-readable logging.
func (c *Config) IsDevelopment() bool {
	return c.Env == "development"
}

// LogValue redacts the JWT secret so it never reaches a log sink; the anon key is
// public so it is safe to log, but reported only as presence for tidiness.
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
	)
}

// normalizeBasePath turns a raw OVERSEER_BASE_PATH value into a safe outbound
// prefix. An empty value stays ""; a non-empty value is coerced to exactly one
// leading slash and no trailing slash. Purely-slash inputs collapse to "". The
// value is server-configured only; no request input ever flows into it.
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
