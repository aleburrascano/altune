package config

import (
	"fmt"
	"log/slog"
	"net/url"
	"strings"

	"github.com/caarlos0/env/v11"
	"github.com/google/uuid"
	"github.com/joho/godotenv"
)

type Config struct {
	Env      string `env:"ENV" envDefault:"development"`
	LogLevel string `env:"LOG_LEVEL" envDefault:"INFO"`

	Host string `env:"HOST" envDefault:"0.0.0.0"`
	Port int    `env:"PORT" envDefault:"8000"`

	CORSOrigins []string `env:"CORS_ORIGINS" envSeparator:"," envDefault:"http://localhost:8081,http://localhost:19006"`

	DatabaseURL string `env:"DATABASE_URL"`

	SupabaseProjectURL string `env:"SUPABASE_PROJECT_URL"`
	SupabaseJWTAud     string `env:"SUPABASE_JWT_AUD" envDefault:"authenticated"`
	SupabaseJWTJWKSURL string `env:"SUPABASE_JWT_JWKS_URL"`
	SupabaseAnonKey    string `env:"SUPABASE_ANON_KEY"`

	RedisURL string `env:"REDIS_URL"`

	MusicBrainzUserAgent string `env:"MUSICBRAINZ_USER_AGENT"`
	LastFMAPIKey         string `env:"LASTFM_API_KEY"`
	FanartTVAPIKey       string `env:"FANARTTV_API_KEY"`
	GeniusAccessToken    string `env:"GENIUS_ACCESS_TOKEN"`
	DiscogsToken         string `env:"DISCOGS_TOKEN"`

	OCIS3Endpoint  string `env:"OCI_S3_ENDPOINT"`
	OCIS3AccessKey string `env:"OCI_S3_ACCESS_KEY"`
	OCIS3SecretKey string `env:"OCI_S3_SECRET_KEY"`
	OCIS3Bucket    string `env:"OCI_S3_BUCKET"`
	OCIS3Region    string `env:"OCI_S3_REGION"`

	MusicDir string `env:"MUSIC_DIR"`

	FFmpegLocation         string   `env:"FFMPEG_LOCATION"`
	YtDLPCookieFile        string   `env:"YTDLP_COOKIE_FILE"`
	YtDLPJSRuntime         string   `env:"YTDLP_JS_RUNTIME"`
	AcquisitionConcurrency int      `env:"ACQUISITION_CONCURRENCY" envDefault:"5"`
	AcoustIDAPIKey         string   `env:"ACOUSTID_API_KEY"`
	StreamripBin           string   `env:"STREAMRIP_BIN"`
	StreamripServices      []string `env:"STREAMRIP_SERVICES" envSeparator:","`
	YtMusicEnabled         bool     `env:"YTMUSIC_ENABLED" envDefault:"true"`
	YtDLPEnabled           bool     `env:"YTDLP_ENABLED" envDefault:"true"`

	GitHubIssueRepo  string `env:"GITHUB_ISSUE_REPO"`
	GitHubIssueToken string `env:"GITHUB_ISSUE_TOKEN"`

	OperatorUserID             string  `env:"OPERATOR_USER_ID"`
	AlertNtfyURL               string  `env:"ALERT_NTFY_URL"`
	EvalMeterEnabled           bool    `env:"EVAL_METER_ENABLED" envDefault:"false"`
	TailDemotionEnabled        bool    `env:"TAIL_DEMOTION_ENABLED" envDefault:"false"`
	CrossKindProminenceEnabled bool    `env:"CROSS_KIND_PROMINENCE_ENABLED" envDefault:"true"`
	BehavioralRankingEnabled   bool    `env:"BEHAVIORAL_RANKING_ENABLED" envDefault:"false"`
	BehavioralCorpusPath       string  `env:"BEHAVIORAL_CORPUS_PATH"`
	ExplorationEnabled         bool    `env:"EXPLORATION_ENABLED" envDefault:"false"`
	ExplorationRate            float64 `env:"EXPLORATION_RATE" envDefault:"0.03"`
	AlertZeroResultThreshold   int     `env:"ALERT_ZERO_RESULT_THRESHOLD" envDefault:"0"`
	IdentityVerifyOnPersist    bool    `env:"IDENTITY_VERIFY_ON_PERSIST" envDefault:"false"`
}

func Load() (*Config, error) {
	_ = godotenv.Load(".env.development")

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
	if err := c.validateSupabase(); err != nil {
		return err
	}
	if c.MusicBrainzUserAgent != "" {
		if !strings.Contains(c.MusicBrainzUserAgent, "@") && !strings.Contains(strings.ToLower(c.MusicBrainzUserAgent), "http") {
			return fmt.Errorf("MUSICBRAINZ_USER_AGENT must contain a contact form URL or email")
		}
	}
	if c.AcquisitionConcurrency < 1 {
		return fmt.Errorf("ACQUISITION_CONCURRENCY must be >= 1, got %d", c.AcquisitionConcurrency)
	}
	if err := c.validateOperator(); err != nil {
		return err
	}
	return c.validateAlertPush()
}

func (c *Config) validateOperator() error {
	if c.OperatorUserID == "" {
		return fmt.Errorf("OPERATOR_USER_ID must be set (operator-only routes reject every user without it)")
	}
	if _, err := uuid.Parse(c.OperatorUserID); err != nil {
		return fmt.Errorf("OPERATOR_USER_ID must be a valid UUID, got %q", c.OperatorUserID)
	}
	return nil
}

func (c *Config) validateAlertPush() error {
	if c.AlertNtfyURL == "" {
		return nil
	}
	if u, err := url.Parse(c.AlertNtfyURL); err != nil || u.Scheme == "" || u.Host == "" {
		return fmt.Errorf("ALERT_NTFY_URL must be a valid URL, got %q", c.AlertNtfyURL)
	}
	return nil
}

func (c *Config) validateSupabase() error {
	if c.SupabaseJWTJWKSURL == "" {
		return fmt.Errorf("SUPABASE_JWT_JWKS_URL must be set (HS256 mode is not supported)")
	}
	if u, err := url.Parse(c.SupabaseJWTJWKSURL); err != nil || u.Scheme == "" || u.Host == "" {
		return fmt.Errorf("SUPABASE_JWT_JWKS_URL must be a valid URL, got %q", c.SupabaseJWTJWKSURL)
	}
	if c.SupabaseProjectURL == "" {
		return fmt.Errorf("SUPABASE_PROJECT_URL must be set (the JWT issuer is derived from it)")
	}
	if u, err := url.Parse(c.SupabaseProjectURL); err != nil || u.Scheme == "" || u.Host == "" {
		return fmt.Errorf("SUPABASE_PROJECT_URL must be a valid URL, got %q", c.SupabaseProjectURL)
	}
	if strings.TrimSpace(c.SupabaseAnonKey) == "" {
		return fmt.Errorf("SUPABASE_ANON_KEY must be set (the admin console needs it to construct its Supabase client)")
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

func (c *Config) HasAlertPush() bool {
	return c.AlertNtfyURL != ""
}

func (c *Config) HasIssueTracker() bool {
	return c.GitHubIssueRepo != "" && c.GitHubIssueToken != ""
}

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
		slog.Bool("has_issue_tracker", c.HasIssueTracker()),
	)
}
