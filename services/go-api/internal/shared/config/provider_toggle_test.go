package config

import "testing"

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
