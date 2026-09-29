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

	TestAuthOptIn bool `env:"TEST_AUTH_ENABLED" envDefault:"false"`

	ProviderReplayOptIn bool   `env:"PROVIDER_REPLAY_ENABLED" envDefault:"false"`
	ProviderReplayDir   string `env:"PROVIDER_REPLAY_DIR"`

	AcquisitionFixtureOptIn bool `env:"ACQUISITION_FIXTURE_ENABLED" envDefault:"false"`

	Host string `env:"HOST" envDefault:"0.0.0.0"`
	Port int    `env:"PORT" envDefault:"8000"`

	CORSOrigins []string `env:"CORS_ORIGINS" envSeparator:"," envDefault:"http://localhost:8081,http://localhost:19006"`

	DatabaseURL string `env:"DATABASE_URL"`

	DBPoolMaxConns int `env:"DB_POOL_MAX_CONNS" envDefault:"20"`

	SupabaseProjectURL string `env:"SUPABASE_PROJECT_URL"`
	SupabaseJWTAud     string `env:"SUPABASE_JWT_AUD" envDefault:"authenticated"`
	SupabaseJWTJWKSURL string `env:"SUPABASE_JWT_JWKS_URL"`

	RedisURL string `env:"REDIS_URL"`

	RedisPoolSize int `env:"REDIS_POOL_SIZE" envDefault:"50"`

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

	AudioKeyPrefix string `env:"AUDIO_KEY_PREFIX"`

	FFmpegLocation         string `env:"FFMPEG_LOCATION"`
	YtDLPCookieFile        string `env:"YTDLP_COOKIE_FILE"`
	YtDLPJSRuntime         string `env:"YTDLP_JS_RUNTIME"`
	AcquisitionConcurrency int    `env:"ACQUISITION_CONCURRENCY" envDefault:"5"`

	AcquisitionDownloadConcurrency int `env:"ACQUISITION_DOWNLOAD_CONCURRENCY" envDefault:"6"`

	AcquisitionPrincipalQueueDepth int `env:"ACQUISITION_PRINCIPAL_QUEUE_DEPTH"`
	AcquisitionDrainBudgetSeconds  int `env:"ACQUISITION_DRAIN_BUDGET_SECONDS" envDefault:"60"`

	AcoustIDAPIKey    string   `env:"ACOUSTID_API_KEY"`
	StreamripBin      string   `env:"STREAMRIP_BIN"`
	StreamripServices []string `env:"STREAMRIP_SERVICES" envSeparator:","`
	YtMusicEnabled    bool     `env:"YTMUSIC_ENABLED" envDefault:"true"`
	YtDLPEnabled      bool     `env:"YTDLP_ENABLED" envDefault:"true"`

	SpotifyEnabled     bool `env:"SPOTIFY_ENABLED" envDefault:"true"`
	SoundCloudEnabled  bool `env:"SOUNDCLOUD_ENABLED" envDefault:"true"`
	AppleMusicEnabled  bool `env:"APPLEMUSIC_ENABLED" envDefault:"true"`
	AmazonMusicEnabled bool `env:"AMAZONMUSIC_ENABLED" envDefault:"true"`

	GitHubIssueRepo  string `env:"GITHUB_ISSUE_REPO"`
	GitHubIssueToken string `env:"GITHUB_ISSUE_TOKEN"`

	FeedbackEnabled bool `env:"FEEDBACK_ENABLED" envDefault:"true"`

	AudioPrefetchEnabled bool `env:"AUDIO_PREFETCH_ENABLED" envDefault:"true"`

	NowPlayingEnrichmentEnabled bool `env:"PLAYBACK_NOW_PLAYING_ENRICHMENT_ENABLED" envDefault:"true"`

	OverseerPrincipalID string `env:"OVERSEER_PRINCIPAL_ID"`

	EvalMeterEnabled           bool    `env:"EVAL_METER_ENABLED" envDefault:"false"`
	TailDemotionEnabled        bool    `env:"TAIL_DEMOTION_ENABLED" envDefault:"false"`
	CrossKindProminenceEnabled bool    `env:"CROSS_KIND_PROMINENCE_ENABLED" envDefault:"true"`
	BehavioralRankingEnabled   bool    `env:"BEHAVIORAL_RANKING_ENABLED" envDefault:"false"`
	BehavioralCorpusPath       string  `env:"BEHAVIORAL_CORPUS_PATH"`
	ExplorationEnabled         bool    `env:"EXPLORATION_ENABLED" envDefault:"false"`
	ExplorationRate            float64 `env:"EXPLORATION_RATE" envDefault:"0.03"`
	AlertZeroResultThreshold   int     `env:"ALERT_ZERO_RESULT_THRESHOLD" envDefault:"0"`
	IdentityVerifyOnPersist    bool    `env:"IDENTITY_VERIFY_ON_PERSIST" envDefault:"false"`

	SSEMaxConns int `env:"SSE_MAX_CONNS" envDefault:"2048"`

	AcquisitionPaused bool     `env:"ACQUISITION_PAUSED" envDefault:"false"`
	DisabledJobs      []string `env:"DISABLED_JOBS" envSeparator:","`
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

func (c *Config) normalize() {
	c.SupabaseJWTAud = strings.TrimSpace(c.SupabaseJWTAud)
	for i, origin := range c.CORSOrigins {
		c.CORSOrigins[i] = strings.TrimSpace(origin)
	}
	c.DisabledJobs = trimNonEmpty(c.DisabledJobs)
}

func trimNonEmpty(items []string) []string {
	out := make([]string, 0, len(items))
	for _, item := range items {
		trimmed := strings.TrimSpace(item)
		if trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}

func (c Config) LogValue() slog.Value {
	return slog.GroupValue(
		slog.String("env", c.Env),
		slog.String("host", c.Host),
		slog.Int("port", c.Port),
		slog.Bool("has_database", c.DatabaseURL != ""),
		slog.Int("db_pool_max_conns", c.DBPoolMaxConns),
		slog.Bool("has_redis", c.HasRedis()),
		slog.Int("redis_pool_size", c.RedisPoolSize),
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
		slog.Bool("has_now_playing_enrichment", c.HasNowPlayingEnrichment()),
		slog.String("audio_key_prefix", c.AudioKeyPrefix),
	)
}
