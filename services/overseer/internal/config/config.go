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

// minOwnerTokenLen is the smallest owner token accepted. Owner-only is the
// service's whole security boundary, so a weak, guessable token is refused at
// startup rather than shipped.
const minOwnerTokenLen = 32

// Config is Overseer's fully-resolved configuration.
type Config struct {
	Env      string
	LogLevel string
	Host     string
	Port     int

	// OwnerToken is the shared secret that authenticates the single owner. It is
	// required: without it every data request is rejected and the service will
	// not start.
	OwnerToken string

	// BasePath is the URL prefix Overseer is mounted under (e.g. "/overseer"). It
	// is used only to build outbound paths (the login redirect, the post-login
	// redirect, the form action and the cookie path) so they land back inside the
	// mount when a reverse proxy strips the prefix before proxying. It is optional
	// and defaults to "" (rootless), reproducing the historical root-absolute
	// behavior byte-for-byte. When set it is normalized to a single leading slash
	// and no trailing slash so base+"/login" never doubles a slash.
	BasePath string

	// TickInterval is how often each bucket's collect cycle runs.
	TickInterval time.Duration
}

// Load reads configuration from the environment, applies defaults and validates
// it. A returned error must abort startup.
func Load() (*Config, error) {
	c := &Config{
		Env:          getenv("OVERSEER_ENV", "development"),
		LogLevel:     getenv("OVERSEER_LOG_LEVEL", "INFO"),
		Host:         getenv("OVERSEER_HOST", "0.0.0.0"),
		OwnerToken:   strings.TrimSpace(os.Getenv("OVERSEER_OWNER_TOKEN")),
		BasePath:     normalizeBasePath(os.Getenv("OVERSEER_BASE_PATH")),
		TickInterval: 5 * time.Second,
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

func (c *Config) validate() error {
	if len(c.OwnerToken) < minOwnerTokenLen {
		return fmt.Errorf(
			"OVERSEER_OWNER_TOKEN must be set and at least %d chars (owner-only rejects every request without it)",
			minOwnerTokenLen,
		)
	}
	return nil
}

// IsDevelopment reports whether the service runs in the development environment,
// which selects human-readable logging.
func (c *Config) IsDevelopment() bool {
	return c.Env == "development"
}

// LogValue redacts the owner token so it never reaches a log sink.
func (c *Config) LogValue() slog.Value {
	return slog.GroupValue(
		slog.String("env", c.Env),
		slog.String("host", c.Host),
		slog.Int("port", c.Port),
		slog.String("base_path", c.BasePath),
		slog.Bool("has_owner_token", c.OwnerToken != ""),
		slog.Duration("tick_interval", c.TickInterval),
	)
}

// normalizeBasePath turns a raw OVERSEER_BASE_PATH value into a safe outbound
// prefix. It is forgiving rather than rejecting: an empty (or whitespace-only)
// value stays "" so the rootless default is byte-identical to before. A non-empty
// value is coerced to exactly one leading slash and no trailing slash, so
// base+"/login" never doubles a slash and a stray "//" leading form (which a
// browser would read as a protocol-relative URL) can never reach an outbound
// path. Purely-slash inputs ("/", "//") collapse to "". The value is
// server-configured only; no request input ever flows into it.
func normalizeBasePath(raw string) string {
	p := strings.TrimSpace(raw)
	if p == "" {
		return ""
	}
	// Collapse any run of leading slashes to one and strip every trailing slash.
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
