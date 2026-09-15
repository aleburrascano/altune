package config

import (
	"fmt"
	"log/slog"
	"net"
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

	// Kill switches for the reverse-engineered provider adapters (scraped
	// tokens / private endpoints). Default enabled; set to false to pull a
	// provider out of every discovery wiring site at startup without a code
	// change. YTMUSIC_ENABLED above also gates the ytmusic discovery adapter.
	SpotifyEnabled     bool `env:"SPOTIFY_ENABLED" envDefault:"true"`
	SoundCloudEnabled  bool `env:"SOUNDCLOUD_ENABLED" envDefault:"true"`
	AppleMusicEnabled  bool `env:"APPLEMUSIC_ENABLED" envDefault:"true"`
	AmazonMusicEnabled bool `env:"AMAZONMUSIC_ENABLED" envDefault:"true"`

	GitHubIssueRepo  string `env:"GITHUB_ISSUE_REPO"`
	GitHubIssueToken string `env:"GITHUB_ISSUE_TOKEN"`

	// Runtime kill switch for the feedback/GitHub integration. Default enabled;
	// set to false to disable in-app reports without discarding the stored
	// GITHUB_ISSUE_REPO / GITHUB_ISSUE_TOKEN credentials. Credential presence
	// (HasIssueTracker) remains an additional gate.
	FeedbackEnabled bool `env:"FEEDBACK_ENABLED" envDefault:"true"`

	// Remote kill switch for the mobile app's audio prefetch pipeline. Default
	// enabled; set to false and every /v1/audio-urls response tells shipped
	// clients to stop prefetching and stream instead, without an app release.
	AudioPrefetchEnabled bool `env:"AUDIO_PREFETCH_ENABLED" envDefault:"true"`

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
	cfg.normalize()
	if err := cfg.validate(); err != nil {
		return nil, fmt.Errorf("validate config: %w", err)
	}
	return cfg, nil
}

// normalize canonicalizes whitespace-sensitive fields once, at load time, so
// stray padding from the environment never silently breaks matching later.
func (c *Config) normalize() {
	c.SupabaseAnonKey = strings.TrimSpace(c.SupabaseAnonKey)
	for i, origin := range c.CORSOrigins {
		c.CORSOrigins[i] = strings.TrimSpace(origin)
	}
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
	if err := c.validateFeedback(); err != nil {
		return err
	}
	return c.validateAlertPush()
}

// validateFeedback checks the feedback integration's config shape at startup so
// a typo'd repo fails loud here instead of as an opaque 500 at first user
// submission. The token is opaque and stays presence-only; only the repo has a
// checkable format.
func (c *Config) validateFeedback() error {
	if c.GitHubIssueRepo == "" {
		return nil
	}
	return validateOwnerRepo("GITHUB_ISSUE_REPO", c.GitHubIssueRepo)
}

// validateOwnerRepo enforces GitHub's "owner/repo" slug shape: exactly two
// non-empty, whitespace-free segments joined by a single slash, matching how
// the GitHub tracker adapter interpolates the value into its API path.
func validateOwnerRepo(field, value string) error {
	owner, repo, ok := strings.Cut(value, "/")
	if !ok || owner == "" || repo == "" || strings.Contains(repo, "/") || strings.ContainsAny(value, " \t\n") {
		return fmt.Errorf("%s must be in owner/repo format, got %q", field, value)
	}
	return nil
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
	if err := validateAbsoluteURL("ALERT_NTFY_URL", c.AlertNtfyURL); err != nil {
		return err
	}
	// The ntfy topic in the path is a de-facto secret: never send it in plaintext.
	u, err := url.Parse(c.AlertNtfyURL)
	if err != nil {
		return fmt.Errorf("ALERT_NTFY_URL is not a valid URL")
	}
	if u.Scheme != "https" {
		return fmt.Errorf("ALERT_NTFY_URL must use https, got scheme %q", u.Scheme)
	}
	return nil
}

// validateAbsoluteURL enforces the shared "must be an absolute URL" rule
// (parseable, with both a scheme and a host) used across config fields.
func validateAbsoluteURL(field, value string) error {
	if u, err := url.Parse(value); err != nil || u.Scheme == "" || u.Host == "" {
		return fmt.Errorf("%s must be a valid URL, got %q", field, value)
	}
	return nil
}

func (c *Config) validateSupabase() error {
	if c.SupabaseJWTJWKSURL == "" {
		return fmt.Errorf("SUPABASE_JWT_JWKS_URL must be set (HS256 mode is not supported)")
	}
	if err := validateSecureURL("SUPABASE_JWT_JWKS_URL", c.SupabaseJWTJWKSURL); err != nil {
		return err
	}
	if c.SupabaseProjectURL == "" {
		return fmt.Errorf("SUPABASE_PROJECT_URL must be set (the JWT issuer is derived from it)")
	}
	if err := validateSecureURL("SUPABASE_PROJECT_URL", c.SupabaseProjectURL); err != nil {
		return err
	}
	if strings.TrimSpace(c.SupabaseAnonKey) == "" {
		return fmt.Errorf("SUPABASE_ANON_KEY must be set (the admin console needs it to construct its Supabase client)")
	}
	return nil
}

// validateSecureURL is validateAbsoluteURL plus a transport requirement: https,
// or plain http only to a loopback host (local Supabase). The JWKS response is
// the trust root for every bearer-token signature check and the issuer is
// derived from the project URL, so plaintext to a remote host would let a
// network-positioned attacker substitute the key set.
func validateSecureURL(field, value string) error {
	if err := validateAbsoluteURL(field, value); err != nil {
		return err
	}
	u, _ := url.Parse(value)
	if u.Scheme == "https" || (u.Scheme == "http" && isLoopbackHost(u.Hostname())) {
		return nil
	}
	return fmt.Errorf("%s must use https (plain http is allowed only for loopback hosts), got scheme %q host %q",
		field, u.Scheme, u.Hostname())
}

func isLoopbackHost(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
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

// HasSpotify reports whether the scraped-token Spotify adapters are enabled.
func (c *Config) HasSpotify() bool {
	return c.SpotifyEnabled
}

// HasSoundCloud reports whether the scraped-client-id SoundCloud adapters are enabled.
func (c *Config) HasSoundCloud() bool {
	return c.SoundCloudEnabled
}

// HasAppleMusic reports whether the scraped-token Apple Music adapter is enabled.
func (c *Config) HasAppleMusic() bool {
	return c.AppleMusicEnabled
}

// HasAmazonMusic reports whether the mimicked-session Amazon Music adapter is enabled.
func (c *Config) HasAmazonMusic() bool {
	return c.AmazonMusicEnabled
}

// HasYouTubeMusic reports whether the YTMusic adapters (discovery and
// acquisition) are enabled.
func (c *Config) HasYouTubeMusic() bool {
	return c.YtMusicEnabled
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
		slog.Bool("feedback_enabled", c.FeedbackEnabled),
		slog.Bool("has_spotify", c.HasSpotify()),
		slog.Bool("has_soundcloud", c.HasSoundCloud()),
		slog.Bool("has_applemusic", c.HasAppleMusic()),
		slog.Bool("has_amazonmusic", c.HasAmazonMusic()),
		slog.Bool("has_ytmusic", c.HasYouTubeMusic()),
	)
}
