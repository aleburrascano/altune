package config

import "strings"

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
