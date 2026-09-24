package config

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"regexp"
	"strings"
	"unicode"

	"github.com/google/uuid"
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
	if err := c.validateOperator(); err != nil {
		return err
	}
	if err := c.validateFeedback(); err != nil {
		return err
	}
	return c.validateAlertPush()
}

func (c *Config) validateTuning() error {
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
	return nil
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
	u, err := parseAbsoluteURL("ALERT_NTFY_URL", c.AlertNtfyURL)
	if err != nil {
		return err
	}
	// The ntfy topic in the path is a de-facto secret: never send it in plaintext.
	if u.Scheme != "https" {
		return fmt.Errorf("ALERT_NTFY_URL must use https, got scheme %q", u.Scheme)
	}
	return nil
}

// parseAbsoluteURL enforces the shared "must be an absolute URL" rule
// (parseable, with both a scheme and a host) used across config fields, and
// returns the parsed URL so callers never re-parse the same string.
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

// validateSecureURL is parseAbsoluteURL plus a transport requirement: https,
// or plain http only to a loopback host (local Supabase). The JWKS response is
// the trust root for every bearer-token signature check and the issuer is
// derived from the project URL, so plaintext to a remote host would let a
// network-positioned attacker substitute the key set.
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
