package config

import (
	"altune/go-api/internal/shared/redis"
	"errors"
	"fmt"
	"net"
	"net/url"
	"regexp"
	"strings"
	"unicode"

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
	if err := c.validateCORSOrigins(); err != nil {
		return err
	}
	if err := c.validateOverseerPrincipal(); err != nil {
		return err
	}
	if err := c.validateFeedback(); err != nil {
		return err
	}
	if err := c.validateAudioKeyPrefix(); err != nil {
		return err
	}
	if err := c.validateTransportSecurity(); err != nil {
		return err
	}
	return c.validateRedis()
}

func (c *Config) validateTransportSecurity() error {
	if !strings.EqualFold(strings.TrimSpace(c.Env), "production") {
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
		return errors.New("DATABASE_URL must require TLS when ENV=production (use sslmode=require, verify-ca or verify-full)")
	}
	for _, fb := range pgCfg.Fallbacks {
		if fb.TLSConfig == nil {
			return errors.New("DATABASE_URL must not fall back to plaintext when ENV=production (use sslmode=require, verify-ca or verify-full)")
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
	if strings.EqualFold(strings.TrimSpace(c.Env), "production") {
		return fmt.Errorf("AUDIO_KEY_PREFIX must not be set when ENV=production, got %q", c.AudioKeyPrefix)
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
	if c.MusicBrainzUserAgent == "" && strings.EqualFold(strings.TrimSpace(c.Env), "production") {
		return fmt.Errorf("MUSICBRAINZ_USER_AGENT must be set when ENV=production")
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
	if !isUnitFraction(c.ExplorationRate) {
		return fmt.Errorf("EXPLORATION_RATE must be between 0 and 1, got %v", c.ExplorationRate)
	}
	if !isUnitFraction(c.AcquisitionConfidenceFloor) {
		return fmt.Errorf("ACQUISITION_CONFIDENCE_FLOOR must be between 0 and 1, got %v", c.AcquisitionConfidenceFloor)
	}
	return nil
}

func (c *Config) validateFeedback() error {
	c.GiteaIssueToken = strings.TrimSpace(c.GiteaIssueToken)
	if err := validateSecureURL("GITEA_ISSUE_URL", c.GiteaIssueURL); err != nil {
		return err
	}
	switch {
	case c.GiteaIssueRepo == "" && c.GiteaIssueToken == "":
		return nil
	case c.GiteaIssueRepo == "":
		return errors.New("GITEA_ISSUE_TOKEN set but GITEA_ISSUE_REPO missing")
	case c.GiteaIssueToken == "":
		return errors.New("GITEA_ISSUE_REPO set but GITEA_ISSUE_TOKEN missing")
	}
	return errors.Join(validateOwnerRepo("GITEA_ISSUE_REPO", c.GiteaIssueRepo), validateToken("GITEA_ISSUE_TOKEN", c.GiteaIssueToken))
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
