package config

import (
	"altune/go-api/internal/shared/redis"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
)

func (c *Config) validate() error {
	if err := c.validateSupabase(); err != nil {
		return err
	}
	if err := c.validateTuning(); err != nil {
		return err
	}
	if err := c.validateSilentDefaults(); err != nil {
		return err
	}
	if err := c.validateCORSOrigins(); err != nil {
		return err
	}
	if err := c.validateOverseerPrincipal(); err != nil {
		return err
	}
	if err := c.validateAudioKeyPrefix(); err != nil {
		return err
	}
	if err := c.validateTransportSecurity(); err != nil {
		return err
	}
	if err := c.validateAlerting(); err != nil {
		return err
	}
	if err := c.validateBehavioralCorpusPath(); err != nil {
		return err
	}
	if err := c.validateDiscoverySearch(); err != nil {
		return err
	}
	return c.validateRedis()
}

var DiscoverySearchProviderNames = []string{
	"deezer", "applemusic", "musicbrainz", "lastfm", "soundcloud", "youtube", "amazonmusic", "spotify",
}

func (c *Config) validateDiscoverySearch() error {
	if c.DiscoverySearchDeadline < 0 {
		return fmt.Errorf("DISCOVERY_SEARCH_DEADLINE must not be negative, got %s", c.DiscoverySearchDeadline)
	}
	for key, ttl := range map[string]time.Duration{
		"DISCOVERY_PROVIDER_CACHE_TTL":    c.DiscoveryProviderCacheTTL,
		"DISCOVERY_MUSICBRAINZ_CACHE_TTL": c.DiscoveryMusicBrainzCacheTTL,
	} {
		if ttl <= 0 {
			return fmt.Errorf("%s must be positive, got %s", key, ttl)
		}
	}
	return c.validateDiscoverySearchProviders()
}

func (c *Config) validateDiscoverySearchProviders() error {
	for _, name := range c.DiscoverySearchProviders {
		if !slices.Contains(DiscoverySearchProviderNames, name) {
			return fmt.Errorf("DISCOVERY_SEARCH_PROVIDERS: unknown provider %q (known: %s)",
				name, strings.Join(DiscoverySearchProviderNames, ", "))
		}
	}
	return nil
}

func (c *Config) validateBehavioralCorpusPath() error {
	if c.BehavioralCorpusPath == "" {
		return nil
	}
	dir := filepath.Dir(c.BehavioralCorpusPath)
	info, err := os.Stat(dir)
	if err != nil {
		return fmt.Errorf("BEHAVIORAL_CORPUS_PATH parent directory is unusable: %w", err)
	}
	if !info.IsDir() {
		return fmt.Errorf("BEHAVIORAL_CORPUS_PATH parent %q is not a directory", dir)
	}
	return probeWritableDir(dir)
}

func probeWritableDir(dir string) error {
	f, err := os.CreateTemp(dir, ".altune-write-probe-*")
	if err != nil {
		return fmt.Errorf("BEHAVIORAL_CORPUS_PATH parent %q is not writable (read-only mount or missing permission): %w", dir, err)
	}
	name := f.Name()
	closeErr := f.Close()
	removeErr := os.Remove(name)
	return errors.Join(closeErr, removeErr)
}

func (c *Config) validateAlerting() error {
	if c.AlertZeroResultThreshold < 0 {
		return fmt.Errorf("ALERT_ZERO_RESULT_THRESHOLD must be >= 0, got %d", c.AlertZeroResultThreshold)
	}
	if c.AlertWebhookURL == "" {
		return nil
	}
	return validateSecureURL("ALERT_WEBHOOK_URL", c.AlertWebhookURL)
}

func (c *Config) validateTransportSecurity() error {
	if c.isKnownNonProductionEnv() {
		return nil
	}
	if err := validateDatabaseTLS(c.DatabaseURL); err != nil {
		return err
	}
	return validateObjectStorageEndpoint(c.OCIS3Endpoint)
}

func validateDatabaseTLS(databaseURL string) error {
	if databaseURL == "" {
		return nil
	}
	pgCfg, err := pgconn.ParseConfig(databaseURL)
	if err != nil {
		return fmt.Errorf("DATABASE_URL is malformed: %w", err)
	}
	if isLoopbackHost(pgCfg.Host) {
		return nil
	}
	if pgCfg.TLSConfig == nil {
		return errors.New("DATABASE_URL must require TLS unless ENV is development, test or staging (use sslmode=require, verify-ca or verify-full)")
	}
	for _, fb := range pgCfg.Fallbacks {
		if fb.TLSConfig == nil {
			return errors.New("DATABASE_URL must not fall back to plaintext unless ENV is development, test or staging (use sslmode=require, verify-ca or verify-full)")
		}
	}
	return nil
}

func validateObjectStorageEndpoint(endpoint string) error {
	if !strings.HasPrefix(strings.ToLower(endpoint), "http://") {
		return nil
	}
	return validateSecureURL("OCI_S3_ENDPOINT", endpoint)
}

var audioKeyPrefixPattern = regexp.MustCompile(`^[a-z0-9-]+/$`)

func (c *Config) validateAudioKeyPrefix() error {
	if c.AudioKeyPrefix == "" {
		return nil
	}
	if !c.isKnownNonProductionEnv() {
		return fmt.Errorf("AUDIO_KEY_PREFIX may be set only when ENV is development, test or staging, got ENV=%q and prefix %q", c.Env, c.AudioKeyPrefix)
	}
	if !audioKeyPrefixPattern.MatchString(c.AudioKeyPrefix) {
		return fmt.Errorf("AUDIO_KEY_PREFIX must match %s, got %q", audioKeyPrefixPattern.String(), c.AudioKeyPrefix)
	}
	return nil
}

func (c *Config) validateRedis() error {
	if c.RedisURL == "" {
		return nil
	}
	if err := redis.ValidateURL(c.RedisURL); err != nil {
		return fmt.Errorf("REDIS_URL is malformed: %w", err)
	}
	return nil
}

func (c *Config) validateTuning() error {
	if c.MusicBrainzUserAgent == "" && !c.isKnownNonProductionEnv() {
		return fmt.Errorf("MUSICBRAINZ_USER_AGENT must be set unless ENV is development, test or staging, got ENV=%q", c.Env)
	}
	if c.MusicBrainzUserAgent != "" {
		if !strings.Contains(c.MusicBrainzUserAgent, "@") && !strings.Contains(strings.ToLower(c.MusicBrainzUserAgent), "http") {
			return fmt.Errorf("MUSICBRAINZ_USER_AGENT must contain a contact form URL or email")
		}
	}
	if c.AcquisitionConcurrency < 1 {
		return fmt.Errorf("ACQUISITION_CONCURRENCY must be >= 1, got %d", c.AcquisitionConcurrency)
	}
	if c.AcquisitionDownloadConcurrency < 1 {
		return fmt.Errorf("ACQUISITION_DOWNLOAD_CONCURRENCY must be >= 1, got %d", c.AcquisitionDownloadConcurrency)
	}
	if c.AcquisitionDrainBudgetSeconds < 1 {
		return fmt.Errorf("ACQUISITION_DRAIN_BUDGET_SECONDS must be >= 1, got %d", c.AcquisitionDrainBudgetSeconds)
	}
	if c.PlaybackQueueRateEvery <= 0 {
		return fmt.Errorf("PLAYBACK_QUEUE_RATE_EVERY must be > 0, got %s", c.PlaybackQueueRateEvery)
	}
	if c.PlaybackQueueRateBurst < 1 {
		return fmt.Errorf("PLAYBACK_QUEUE_RATE_BURST must be >= 1, got %d", c.PlaybackQueueRateBurst)
	}
	if c.PlaybackErasureScanTimeout <= 0 {
		return fmt.Errorf("PLAYBACK_ERASURE_SCAN_TIMEOUT must be > 0, got %s", c.PlaybackErasureScanTimeout)
	}
	if err := c.validateAudioSources(); err != nil {
		return err
	}
	if err := c.validateCookieFile(); err != nil {
		return err
	}
	if err := c.validateYtDLPRuntime(); err != nil {
		return err
	}
	if !isUnitFraction(c.ExplorationRate) {
		return fmt.Errorf("EXPLORATION_RATE must be between 0 and 1, got %v", c.ExplorationRate)
	}
	if !isUnitFraction(c.AcquisitionConfidenceFloor) {
		return fmt.Errorf("ACQUISITION_CONFIDENCE_FLOOR must be between 0 and 1, got %v", c.AcquisitionConfidenceFloor)
	}
	return nil
}

func (c *Config) validateAudioSources() error {
	if c.YtMusicEnabled || c.YtDLPEnabled || len(c.StreamripServices) > 0 {
		return nil
	}
	if c.AcquisitionFixtureEnabled() || c.AcquisitionPaused {
		return nil
	}
	return errors.New("no audio source enabled: enable at least one of YTMUSIC_ENABLED, YTDLP_ENABLED or STREAMRIP_SERVICES (or set ACQUISITION_PAUSED=true)")
}

func (c *Config) validateCookieFile() error {
	if c.YtDLPCookieFile == "" {
		return nil
	}
	f, err := os.Open(c.YtDLPCookieFile)
	if err != nil {
		return fmt.Errorf("YTDLP_COOKIE_FILE is not readable: %w", err)
	}
	return f.Close()
}

var allowedJSRuntimes = map[string]bool{"node": true, "deno": true}

var remoteComponentsPattern = regexp.MustCompile(`^[A-Za-z0-9_.:@/-]+(,[A-Za-z0-9_.:@/-]+)*$`)

func (c *Config) validateYtDLPRuntime() error {
	if c.YtDLPJSRuntime != "" && !allowedJSRuntimes[c.YtDLPJSRuntime] {
		return fmt.Errorf("YTDLP_JS_RUNTIME must be one of node, deno (or empty), got %q", c.YtDLPJSRuntime)
	}
	if c.YtDLPRemoteComponents != "" && !remoteComponentsPattern.MatchString(c.YtDLPRemoteComponents) {
		return fmt.Errorf("YTDLP_REMOTE_COMPONENTS must be a comma-separated list of component names (or empty), got %q", c.YtDLPRemoteComponents)
	}
	return nil
}

var knownLogLevels = map[string]bool{"DEBUG": true, "INFO": true, "WARN": true, "WARNING": true, "ERROR": true}

func (c *Config) validateSilentDefaults() error {
	for _, p := range []struct {
		name  string
		value int
	}{
		{"DB_POOL_MAX_CONNS", c.DBPoolMaxConns},
		{"SSE_MAX_CONNS", c.SSEMaxConns},
		{"REDIS_POOL_SIZE", c.RedisPoolSize},
	} {
		if p.value < 1 {
			return fmt.Errorf("%s must be >= 1, got %d", p.name, p.value)
		}
	}
	if !knownLogLevels[strings.ToUpper(c.LogLevel)] {
		return fmt.Errorf("LOG_LEVEL must be one of DEBUG, INFO, WARN, WARNING, ERROR, got %q", c.LogLevel)
	}
	if c.ProviderReplayOptIn && c.ProviderReplayDir == "" {
		return errors.New("PROVIDER_REPLAY_DIR must be set when PROVIDER_REPLAY_ENABLED=true")
	}
	return nil
}

func isUnitFraction(v float64) bool {
	return v >= 0 && v <= 1
}

func (c *Config) validateCORSOrigins() error {
	for _, origin := range c.CORSOrigins {
		if err := validateCORSOrigin(origin); err != nil {
			return err
		}
	}
	return nil
}

func validateCORSOrigin(origin string) error {
	u, err := parseAbsoluteURL("CORS_ORIGINS", origin)
	if err != nil {
		return err
	}
	if !isBareOrigin(u) {
		return fmt.Errorf("CORS_ORIGINS entries must be scheme://host[:port] with nothing after the host, got %q", origin)
	}
	return nil
}

func isBareOrigin(u *url.URL) bool {
	return u.Path == "" && u.RawQuery == "" && u.Fragment == "" && u.User == nil
}

func (c *Config) validateOverseerPrincipal() error {
	id, err := canonicalUserID("OVERSEER_PRINCIPAL_ID", c.OverseerPrincipalID)
	if err != nil {
		return err
	}
	c.OverseerPrincipalID = id
	return nil
}

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

func parseAbsoluteURL(field, value string) (*url.URL, error) {
	u, err := url.Parse(value)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return nil, fmt.Errorf("%s must be a valid URL, got %q", field, value)
	}
	return u, nil
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
	if c.SupabaseJWTAud == "" {
		return fmt.Errorf("SUPABASE_JWT_AUD must not be blank (every token would be rejected as claim_invalid_aud)")
	}
	return nil
}

func validateSecureURL(field, value string) error {
	u, err := parseAbsoluteURL(field, value)
	if err != nil {
		return err
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
