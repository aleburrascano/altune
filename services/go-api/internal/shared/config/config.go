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

	// TestAuthOptIn is the dedicated, explicit opt-in for the non-production
	// test-auth backdoor (the test verifier + POST /test/login). It defaults to
	// false so the path fails closed: ENV alone can never enable it. Because ENV
	// itself defaults to "development", gating on ENV alone would silently turn
	// the backdoor ON in any prod deploy that forgot to set ENV=production; this
	// flag makes enabling it a deliberate, separate act. See TestAuthEnabled.
	TestAuthOptIn bool `env:"TEST_AUTH_ENABLED" envDefault:"false"`

	ProviderReplayOptIn bool   `env:"PROVIDER_REPLAY_ENABLED" envDefault:"false"`
	ProviderReplayDir   string `env:"PROVIDER_REPLAY_DIR"`

	AcquisitionFixtureOptIn bool `env:"ACQUISITION_FIXTURE_ENABLED" envDefault:"false"`

	Host string `env:"HOST" envDefault:"0.0.0.0"`
	Port int    `env:"PORT" envDefault:"8000"`

	CORSOrigins []string `env:"CORS_ORIGINS" envSeparator:"," envDefault:"http://localhost:8081,http://localhost:19006"`

	DatabaseURL string `env:"DATABASE_URL"`

	// Ceiling on concurrent pgx connections, sized against this service's own
	// concurrency rather than the host's CPU count: the acquisition workers
	// (ACQUISITION_CONCURRENCY, default 5) hold connections for the length of a
	// job while the request path serves /v1 traffic underneath them, and pgx's
	// own default of max(4, NumCPU) gives a 2-vCPU container 4 — fewer than the
	// workers alone. 20 leaves the request path roughly three times the
	// worker's share and stays well inside a single Postgres role's connection
	// allowance with other deploys (and the CLI commands) sharing it.
	// Non-positive falls back to the pool default.
	DBPoolMaxConns int `env:"DB_POOL_MAX_CONNS" envDefault:"20"`

	SupabaseProjectURL string `env:"SUPABASE_PROJECT_URL"`
	SupabaseJWTAud     string `env:"SUPABASE_JWT_AUD" envDefault:"authenticated"`
	SupabaseJWTJWKSURL string `env:"SUPABASE_JWT_JWKS_URL"`

	RedisURL string `env:"REDIS_URL"`

	// Ceiling on concurrent go-redis connections. A cache call is short and a
	// request can make several of them while holding no database connection, so
	// this sits above DB_POOL_MAX_CONNS; go-redis's own default of
	// 10 x GOMAXPROCS makes the ceiling a property of the host instead, which
	// is what this pins down. Past it callers queue for a free connection and
	// only fail once go-redis's own PoolTimeout expires, which is what the
	// Timeouts counter in redis.ReadPoolStats reports. Non-positive falls back
	// to the client default.
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

	FFmpegLocation         string `env:"FFMPEG_LOCATION"`
	YtDLPCookieFile        string `env:"YTDLP_COOKIE_FILE"`
	YtDLPJSRuntime         string `env:"YTDLP_JS_RUNTIME"`
	AcquisitionConcurrency int    `env:"ACQUISITION_CONCURRENCY" envDefault:"5"`

	AcquisitionPrincipalQueueDepth int `env:"ACQUISITION_PRINCIPAL_QUEUE_DEPTH"`

	AcoustIDAPIKey    string   `env:"ACOUSTID_API_KEY"`
	StreamripBin      string   `env:"STREAMRIP_BIN"`
	StreamripServices []string `env:"STREAMRIP_SERVICES" envSeparator:","`
	YtMusicEnabled    bool     `env:"YTMUSIC_ENABLED" envDefault:"true"`
	YtDLPEnabled      bool     `env:"YTDLP_ENABLED" envDefault:"true"`

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

	// Kill switch for the feedback/GitHub integration, applied at startup
	// (restart to change). Default enabled; set to false to disable in-app
	// reports without discarding the stored GITHUB_ISSUE_REPO /
	// GITHUB_ISSUE_TOKEN credentials. Credential presence (HasIssueTracker)
	// remains an additional gate. While off, report submits get a 503 with code
	// "feedback.disabled".
	FeedbackEnabled bool `env:"FEEDBACK_ENABLED" envDefault:"true"`

	// Remote kill switch for the mobile app's audio prefetch pipeline. Default
	// enabled; set to false and every /v1/audio-urls response tells shipped
	// clients to stop prefetching and stream instead, without an app release.
	AudioPrefetchEnabled bool `env:"AUDIO_PREFETCH_ENABLED" envDefault:"true"`

	// Kill switch for the best-effort now-playing enrichment on queue resume
	// (GET /v1/playback/queue-state), applied at startup (restart to change).
	// Default enabled; set to false to shed the per-resume catalog lookup when
	// the catalog database is struggling. Resume still returns the queue, just
	// without current_track.
	NowPlayingEnrichmentEnabled bool `env:"PLAYBACK_NOW_PLAYING_ENRICHMENT_ENABLED" envDefault:"true"`

	OperatorUserID string `env:"OPERATOR_USER_ID"`

	// OperatorReadOnlyUserID is the optional second admin principal: it reaches
	// the admin GET surface and nothing else, so a service that only observes
	// (Overseer) holds a credential that cannot mutate production if it leaks.
	// Empty leaves the admin surface operator-only. It must not equal
	// OperatorUserID.
	OperatorReadOnlyUserID string `env:"OPERATOR_READONLY_USER_ID"`

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

	// Server-wide ceiling on concurrent /v1/events SSE streams across all
	// users; past it new streams get 429. Non-positive falls back to the
	// handler default.
	SSEMaxConns int `env:"SSE_MAX_CONNS" envDefault:"2048"`
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
	c.SupabaseJWTAud = strings.TrimSpace(c.SupabaseJWTAud)
	for i, origin := range c.CORSOrigins {
		c.CORSOrigins[i] = strings.TrimSpace(origin)
	}
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
	)
}
