package config

import (
	"fmt"
	"log/slog"
	"strings"

	"github.com/caarlos0/env/v11"
	"github.com/joho/godotenv"
)

type Config struct {
	Env      string `env:"ENV" envDefault:"development"`
	LogLevel string `env:"LOG_LEVEL" envDefault:"INFO"`

	Host string `env:"HOST" envDefault:"0.0.0.0"`
	Port int    `env:"PORT" envDefault:"8000"`

	CORSOrigins []string `env:"CORS_ORIGINS" envSeparator:"," envDefault:"http://localhost:8081,http://localhost:19006"`

	DatabaseURL string `env:"DATABASE_URL"`

	// Supabase Auth — JWKS only (HS256 not implemented, matching Python behavior).
	SupabaseProjectURL string `env:"SUPABASE_PROJECT_URL"`
	SupabaseJWTAud     string `env:"SUPABASE_JWT_AUD" envDefault:"authenticated"`
	SupabaseJWTJWKSURL string `env:"SUPABASE_JWT_JWKS_URL"`
	// Public client (publishable/anon) key — safe to expose to browsers. Lets the
	// Mission Control page sign the operator in with email + password directly.
	SupabaseAnonKey string `env:"SUPABASE_ANON_KEY"`

	// Redis
	RedisURL string `env:"REDIS_URL"`

	// Discovery providers
	MusicBrainzUserAgent string `env:"MUSICBRAINZ_USER_AGENT"`
	LastFMAPIKey         string `env:"LASTFM_API_KEY"`
	FanartTVAPIKey       string `env:"FANARTTV_API_KEY"`
	GeniusAccessToken    string `env:"GENIUS_ACCESS_TOKEN"`
	DiscogsToken         string `env:"DISCOGS_TOKEN"`

	// Audio storage — OCI Object Storage (S3-compatible)
	OCIS3Endpoint  string `env:"OCI_S3_ENDPOINT"`
	OCIS3AccessKey string `env:"OCI_S3_ACCESS_KEY"`
	OCIS3SecretKey string `env:"OCI_S3_SECRET_KEY"`
	OCIS3Bucket    string `env:"OCI_S3_BUCKET"`
	OCIS3Region    string `env:"OCI_S3_REGION"`

	// Audio storage — local filesystem fallback
	MusicDir string `env:"MUSIC_DIR"`

	// Audio acquisition tools
	FFmpegLocation         string `env:"FFMPEG_LOCATION"`
	YtDLPCookieFile        string `env:"YTDLP_COOKIE_FILE"`
	YtDLPJSRuntime         string `env:"YTDLP_JS_RUNTIME"`
	AcquisitionConcurrency int    `env:"ACQUISITION_CONCURRENCY" envDefault:"5"`

	// Mission Control — operator console. The console is denied to everyone
	// (fail-closed) unless OPERATOR_USER_ID is set to the operator's account id.
	OperatorUserID string `env:"OPERATOR_USER_ID"`
	// Alert push channel (ntfy topic URL). Empty → alerts are logged only, not
	// pushed. Use a non-guessable random topic.
	AlertNtfyURL string `env:"ALERT_NTFY_URL"`
	// Discovery-eval meter. OFF by default: the live smoke run hits real provider
	// APIs and shares per-provider quota with user traffic, so it must be opted
	// into deliberately.
	EvalMeterEnabled bool `env:"EVAL_METER_ENABLED" envDefault:"false"`
	// Tail-noise demotion experiment. OFF by default: demotes single-source
	// UGC/scrobble results with no identity below corroborated results. Flipped on
	// for eval A/B before any production rollout. See
	// docs/brainstorms/2026-06-27-discovery-tail-noise-demotion.md.
	TailDemotionEnabled bool `env:"TAIL_DEMOTION_ENABLED" envDefault:"false"`
	// Behavioral ranking. OFF by default: feeds the EventConsumer-derived
	// satisfaction signal (play-to-completion +, skip-after-click −) into ranking
	// as a within-tie input. A new ranking consumer, so it ships dark until eval
	// A/B proves it on the hard corpus — same discipline as tail demotion.
	BehavioralRankingEnabled bool `env:"BEHAVIORAL_RANKING_ENABLED" envDefault:"false"`
	// Self-growing eval corpus. Empty → the nightly behavioral-label corpus job is
	// disabled. When set to a writable path, the job materializes search→engagement
	// labels (positive) + wrong_album (hard negative) into the eval corpus format.
	BehavioralCorpusPath string `env:"BEHAVIORAL_CORPUS_PATH"`
	// Exploration randomization. OFF by default: a small fraction of searches
	// serve a randomized result order (logged as exploration) so offline
	// counterfactual eval has unbiased propensity data. The one user-facing
	// behavior change — shipped dark behind this flag so it needs no live sign-off.
	ExplorationEnabled bool    `env:"EXPLORATION_ENABLED" envDefault:"false"`
	ExplorationRate    float64 `env:"EXPLORATION_RATE" envDefault:"0.03"`
}

func Load() (*Config, error) {
	_ = godotenv.Load()

	cfg := &Config{}
	if err := env.Parse(cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}
	if err := cfg.validate(); err != nil {
		return nil, fmt.Errorf("validate config: %w", err)
	}
	return cfg, nil
}

func (c *Config) validate() error {
	if c.SupabaseJWTJWKSURL == "" {
		return fmt.Errorf("SUPABASE_JWT_JWKS_URL must be set (HS256 mode is not supported)")
	}
	if c.MusicBrainzUserAgent != "" {
		if !strings.Contains(c.MusicBrainzUserAgent, "@") && !strings.Contains(strings.ToLower(c.MusicBrainzUserAgent), "http") {
			return fmt.Errorf("MUSICBRAINZ_USER_AGENT must contain a contact form URL or email")
		}
	}
	return nil
}

func (c *Config) IsDevelopment() bool {
	return c.Env == "development"
}

func (c *Config) HasOCIS3() bool {
	return c.OCIS3Endpoint != "" && c.OCIS3AccessKey != "" && c.OCIS3SecretKey != "" && c.OCIS3Bucket != ""
}

func (c *Config) HasRedis() bool {
	return c.RedisURL != ""
}

func (c *Config) HasLastFM() bool {
	return c.LastFMAPIKey != ""
}

func (c *Config) HasFanartTV() bool {
	return c.FanartTVAPIKey != ""
}

func (c *Config) HasGenius() bool {
	return c.GeniusAccessToken != ""
}

func (c *Config) HasMusicBrainz() bool {
	return c.MusicBrainzUserAgent != ""
}

func (c *Config) HasDiscogs() bool {
	return c.DiscogsToken != ""
}

func (c *Config) HasOperatorConsole() bool {
	return c.OperatorUserID != ""
}

func (c *Config) HasAlertPush() bool {
	return c.AlertNtfyURL != ""
}

// LogValue implements slog.LogValuer to redact secrets.
func (c Config) LogValue() slog.Value {
	return slog.GroupValue(
		slog.String("env", c.Env),
		slog.String("host", c.Host),
		slog.Int("port", c.Port),
		slog.Bool("has_database", c.DatabaseURL != ""),
		slog.Bool("has_redis", c.HasRedis()),
		slog.Bool("has_oci_s3", c.HasOCIS3()),
		slog.Bool("has_lastfm", c.HasLastFM()),
		slog.Bool("has_musicbrainz", c.HasMusicBrainz()),
		slog.Bool("has_fanarttv", c.HasFanartTV()),
		slog.Bool("has_genius", c.HasGenius()),
		slog.Bool("has_discogs", c.HasDiscogs()),
	)
}
