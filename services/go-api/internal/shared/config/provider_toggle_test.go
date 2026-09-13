package config

import (
	"os"
	"testing"
)

// TestLoad_YtProviderTogglesDefaultEnabled guards that the new per-source kill
// switches default to enabled, so existing deployments keep wiring ytmusic and
// yt-dlp exactly as before unless an operator opts out.
func TestLoad_YtProviderTogglesDefaultEnabled(t *testing.T) {
	setEnv(t, map[string]string{
		"SUPABASE_PROJECT_URL":  "https://example.supabase.co",
		"SUPABASE_JWT_JWKS_URL": "https://example.supabase.co/auth/v1/.well-known/jwks.json",
		"SUPABASE_ANON_KEY":     "anon-key",
		"OPERATOR_USER_ID":      validOperatorID,
	})

	cfg, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !cfg.YtMusicEnabled {
		t.Error("expected YTMUSIC_ENABLED to default to true (preserve current behavior)")
	}
	if !cfg.YtDLPEnabled {
		t.Error("expected YTDLP_ENABLED to default to true (preserve current behavior)")
	}
}

// TestLoad_YtProviderTogglesRespectEnv guards that setting the env flags to
// false pulls the corresponding source out of the startup wiring.
func TestLoad_YtProviderTogglesRespectEnv(t *testing.T) {
	setEnv(t, map[string]string{
		"SUPABASE_PROJECT_URL":  "https://example.supabase.co",
		"SUPABASE_JWT_JWKS_URL": "https://example.supabase.co/auth/v1/.well-known/jwks.json",
		"SUPABASE_ANON_KEY":     "anon-key",
		"OPERATOR_USER_ID":      validOperatorID,
		"YTMUSIC_ENABLED":       "false",
		"YTDLP_ENABLED":         "false",
	})

	cfg, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.YtMusicEnabled {
		t.Error("expected YTMUSIC_ENABLED=false to disable the ytmusic source")
	}
	if cfg.YtDLPEnabled {
		t.Error("expected YTDLP_ENABLED=false to disable the yt-dlp source")
	}
}

// TestLoad_ScrapedProviderKillSwitches guards the env contract of the
// reverse-engineered provider kill switches: enabled by default, and each
// independently disabled by setting its flag to false.
func TestLoad_ScrapedProviderKillSwitches(t *testing.T) {
	base := map[string]string{
		"SUPABASE_PROJECT_URL":  "https://example.supabase.co",
		"SUPABASE_JWT_JWKS_URL": "https://example.supabase.co/auth/v1/.well-known/jwks.json",
		"SUPABASE_ANON_KEY":     "anon-key",
		"OPERATOR_USER_ID":      validOperatorID,
	}
	switches := []struct {
		env string
		has func(*Config) bool
	}{
		{"SPOTIFY_ENABLED", (*Config).HasSpotify},
		{"SOUNDCLOUD_ENABLED", (*Config).HasSoundCloud},
		{"APPLEMUSIC_ENABLED", (*Config).HasAppleMusic},
		{"AMAZONMUSIC_ENABLED", (*Config).HasAmazonMusic},
		{"YTMUSIC_ENABLED", (*Config).HasYouTubeMusic},
	}

	for _, sw := range switches {
		t.Run(sw.env, func(t *testing.T) {
			for _, other := range switches {
				t.Setenv(other.env, "")
				os.Unsetenv(other.env)
			}
			setEnv(t, base)
			cfg, err := Load()
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !sw.has(cfg) {
				t.Fatalf("expected %s to default to enabled", sw.env)
			}

			t.Setenv(sw.env, "false")
			cfg, err = Load()
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if sw.has(cfg) {
				t.Errorf("expected %s=false to disable the provider", sw.env)
			}
		})
	}
}
