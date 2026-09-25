package config

import "strings"

func (c *Config) IsDevelopment() bool {
	return c.Env == "development"
}

var nonProdTestAuthEnvs = map[string]bool{
	"development": true,
	"test":        true,
}

func (c *Config) TestAuthEnabled() bool {
	if !c.TestAuthOptIn {
		return false
	}
	return nonProdTestAuthEnvs[strings.ToLower(strings.TrimSpace(c.Env))]
}

// ProviderReplayEnabled is true only with the explicit opt-in, a fixture dir,
// and a non-production ENV; anything else (including empty ENV) is production.
func (c *Config) ProviderReplayEnabled() bool {
	if !c.ProviderReplayOptIn || c.ProviderReplayDir == "" {
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

func (c *Config) HasSpotify() bool {
	return c.SpotifyEnabled
}

func (c *Config) HasSoundCloud() bool {
	return c.SoundCloudEnabled
}

func (c *Config) HasAppleMusic() bool {
	return c.AppleMusicEnabled
}

func (c *Config) HasAmazonMusic() bool {
	return c.AmazonMusicEnabled
}

func (c *Config) HasYouTubeMusic() bool {
	return c.YtMusicEnabled
}

func (c *Config) HasNowPlayingEnrichment() bool {
	return c.NowPlayingEnrichmentEnabled
}

func (c *Config) HasIssueTracker() bool {
	return c.GitHubIssueRepo != "" && c.GitHubIssueToken != ""
}
