package config

import (
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/url"
	"regexp"
	"strings"
	"unicode"

	"github.com/caarlos0/env/v11"
	"github.com/google/uuid"
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
	SupabaseAnonKey    string `env:"SUPABASE_ANON_KEY"`

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

	// Per-principal (userId) ceiling on outstanding acquisition jobs (in-flight
	// + pending) any one user may hold in the shared admission queue, so no
	// single user can fill the queue and starve others. Non-positive derives a
	// default of ACQUISITION_CONCURRENCY at wiring: one user may saturate the
	// workers but not the deeper (concurrency x factor) global queue.
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
	c.SupabaseAnonKey = strings.TrimSpace(c.SupabaseAnonKey)
	c.SupabaseJWTAud = strings.TrimSpace(c.SupabaseJWTAud)
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
	if !isUnitFraction(c.ExplorationRate) {
		return fmt.Errorf("EXPLORATION_RATE must be between 0 and 1, got %v", c.ExplorationRate)
	}
	if err := c.validateCORSOrigins(); err != nil {
		return err
	}
	if err := c.validateOperator(); err != nil {
		return err
	}
	if err := c.validateFeedback(); err != nil {
		return err
	}
	return c.validateAlertPush()
}

func (c *Config) validateFeedback() error {
	c.GitHubIssueToken = strings.TrimSpace(c.GitHubIssueToken)
	switch {
	case c.GitHubIssueRepo == "" && c.GitHubIssueToken == "":
		return nil
	case c.GitHubIssueRepo == "":
		return errors.New("GITHUB_ISSUE_TOKEN set but GITHUB_ISSUE_REPO missing")
	case c.GitHubIssueToken == "":
		return errors.New("GITHUB_ISSUE_REPO set but GITHUB_ISSUE_TOKEN missing")
	}
	return errors.Join(validateOwnerRepo("GITHUB_ISSUE_REPO", c.GitHubIssueRepo), validateToken("GITHUB_ISSUE_TOKEN", c.GitHubIssueToken))
}

var ownerRepoPattern = regexp.MustCompile(`^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$`)

func validateOwnerRepo(field, value string) error {
	owner, repo, _ := strings.Cut(value, "/")
	if !ownerRepoPattern.MatchString(value) || isDotSegment(owner) || isDotSegment(repo) {
		return fmt.Errorf("%s must be in owner/repo format, got %q", field, value)
	}
	return nil
}

func isDotSegment(s string) bool {
	return s == "." || s == ".."
}

func validateToken(field, value string) error {
	if strings.IndexFunc(value, isSpaceOrControl) >= 0 {
		return fmt.Errorf("%s must not contain whitespace or control characters", field)
	}
	return nil
}

func isSpaceOrControl(r rune) bool {
	return unicode.IsSpace(r) || unicode.IsControl(r)
}

// isUnitFraction reports whether v is a usable probability. It is phrased
// positively because the environment can supply NaN and ±Inf (strconv parses
// both), and NaN fails every comparison, so "v < 0 || v > 1" would pass it.
func isUnitFraction(v float64) bool {
	return v >= 0 && v <= 1
}

// validateCORSOrigins checks each allowed origin at startup because the CORS
// middleware matches the browser's Origin header by exact string: an entry with
// no scheme, a trailing slash, or a path is not a stricter policy but a dead
// one, and the preflight it rejects surfaces in a browser console, never in
// this service's logs. A subdomain wildcard ("https://*.altune.app", which the
// cors library expands) is an absolute URL and stays accepted; a bare "*" is
// not, and browsers reject it anyway alongside the credentials this service's
// CORS config allows.
func (c *Config) validateCORSOrigins() error {
	for _, origin := range c.CORSOrigins {
		if err := validateCORSOrigin(origin); err != nil {
			return err
		}
	}
	return nil
}

func validateCORSOrigin(origin string) error {
	if err := validateAbsoluteURL("CORS_ORIGINS", origin); err != nil {
		return err
	}
	u, err := url.Parse(origin)
	if err != nil {
		return fmt.Errorf("CORS_ORIGINS must be a valid URL, got %q", origin)
	}
	if !isBareOrigin(u) {
		return fmt.Errorf("CORS_ORIGINS entries must be scheme://host[:port] with nothing after the host, got %q", origin)
	}
	return nil
}

func isBareOrigin(u *url.URL) bool {
	return u.Path == "" && u.RawQuery == "" && u.Fragment == "" && u.User == nil
}

func (c *Config) validateOperator() error {
	id, err := canonicalUserID("OPERATOR_USER_ID", c.OperatorUserID)
	if err != nil {
		return err
	}
	if id == "" {
		return fmt.Errorf("OPERATOR_USER_ID must be set (operator-only routes reject every user without it)")
	}
	c.OperatorUserID = id
	return c.validateOperatorReadOnly()
}

// validateOperatorReadOnly checks the optional read-only admin principal. It
// must differ from the operator: a read-only id that equals the operator id is
// admitted by the operator arm of the admin gate and so carries the write scope
// it exists to drop. Both sides are canonical UUID text by here, so the same id
// in different hex case cannot slip past that comparison.
func (c *Config) validateOperatorReadOnly() error {
	id, err := canonicalUserID("OPERATOR_READONLY_USER_ID", c.OperatorReadOnlyUserID)
	if err != nil {
		return err
	}
	if id != "" && id == c.OperatorUserID {
		return fmt.Errorf("OPERATOR_READONLY_USER_ID must differ from OPERATOR_USER_ID (an equal id would hold write scope)")
	}
	c.OperatorReadOnlyUserID = id
	return nil
}

// canonicalUserID parses a configured Supabase user id into canonical lower-case
// UUID text, so the admin gate's string comparison against the JWT subject
// cannot be defeated by the hex case an operator happened to paste. An empty
// value stays empty; the caller decides whether that is allowed.
func canonicalUserID(field, raw string) (string, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "", nil
	}
	id, err := uuid.Parse(trimmed)
	if err != nil {
		return "", fmt.Errorf("%s must be a valid UUID, got %q", field, trimmed)
	}
	return id.String(), nil
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
	// Whitespace-only survives env parsing (only a fully empty value takes the
	// envDefault), and the verifier matches the aud claim by exact string, so a
	// blank audience starts the process and then rejects every real token.
	if c.SupabaseJWTAud == "" {
		return fmt.Errorf("SUPABASE_JWT_AUD must not be blank (every token would be rejected as claim_invalid_aud)")
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
	u, err := url.Parse(value)
	if err != nil {
		return fmt.Errorf("%s must be a valid URL", field)
	}
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

// nonProdTestAuthEnvs is the ALLOWLIST of ENV values that enable the
// non-production test-auth path (the test verifier + POST /test/login). It is
// an allowlist by design so the guard fails closed: any value not listed here
// — "production", an unrecognized or misspelled string, or empty — leaves test
// auth OFF, so an ambiguous or misconfigured environment is treated as
// production. Never add a prod-like value here.
var nonProdTestAuthEnvs = map[string]bool{
	"development": true,
	"test":        true,
}

// TestAuthEnabled reports whether the non-production test-auth path may be
// wired. It is the single structural guard the wiring consults: when it returns
// false the app constructs no test verifier and mounts no /test/login route, so
// a production build has neither. Enabling it requires BOTH a deliberate opt-in
// (TEST_AUTH_ENABLED=true) AND a non-prod ENV on the explicit allowlist (see
// nonProdTestAuthEnvs), trimmed and lower-cased so stray padding or casing
// cannot flip a prod environment into a non-prod one. It fails closed: an
// absent opt-in, or an unset/unknown/production ENV, leaves the backdoor OFF.
func (c *Config) TestAuthEnabled() bool {
	if !c.TestAuthOptIn {
		return false
	}
	return nonProdTestAuthEnvs[strings.ToLower(strings.TrimSpace(c.Env))]
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

// HasNowPlayingEnrichment reports whether queue resume enriches the current
// track from the catalog.
func (c *Config) HasNowPlayingEnrichment() bool {
	return c.NowPlayingEnrichmentEnabled
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
