package config

import (
	"os"
	"testing"
)

func TestLoad_MinimalValid(t *testing.T) {
	setEnv(t, map[string]string{
		"SUPABASE_JWT_JWKS_URL": "https://example.supabase.co/auth/v1/.well-known/jwks.json",
	})

	cfg, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Env != "development" {
		t.Errorf("expected default env=development, got %s", cfg.Env)
	}
	if cfg.Port != 8000 {
		t.Errorf("expected default port=8000, got %d", cfg.Port)
	}
	if cfg.LogLevel != "INFO" {
		t.Errorf("expected default log_level=INFO, got %s", cfg.LogLevel)
	}
}

func TestLoad_MissingJWKSURL(t *testing.T) {
	setEnv(t, map[string]string{})

	_, err := Load()
	if err == nil {
		t.Fatal("expected error for missing JWKS URL")
	}
}

func TestLoad_MusicBrainzUAWithoutContact(t *testing.T) {
	setEnv(t, map[string]string{
		"SUPABASE_JWT_JWKS_URL":  "https://example.supabase.co/auth/v1/.well-known/jwks.json",
		"MUSICBRAINZ_USER_AGENT": "altune/0.1",
	})

	_, err := Load()
	if err == nil {
		t.Fatal("expected error for MB user agent without contact info")
	}
}

func TestLoad_MusicBrainzUAWithEmail(t *testing.T) {
	setEnv(t, map[string]string{
		"SUPABASE_JWT_JWKS_URL":  "https://example.supabase.co/auth/v1/.well-known/jwks.json",
		"MUSICBRAINZ_USER_AGENT": "altune/0.1 ( mailto:dev@altune.test )",
	})

	cfg, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !cfg.HasMusicBrainz() {
		t.Error("expected HasMusicBrainz=true")
	}
}

func TestConfig_LogValue_RedactsSecrets(t *testing.T) {
	cfg := &Config{
		Env:            "production",
		Host:           "0.0.0.0",
		Port:           8000,
		DatabaseURL:    "postgresql://secret@host/db",
		OCIS3SecretKey: "super-secret",
		LastFMAPIKey:   "api-key-secret",
	}

	lv := cfg.LogValue()
	s := lv.String()

	if contains(s, "secret") {
		t.Errorf("LogValue should not contain secrets, got: %s", s)
	}
}

func TestConfig_HasOCIS3(t *testing.T) {
	cfg := &Config{
		OCIS3Endpoint:  "https://endpoint",
		OCIS3AccessKey: "key",
		OCIS3SecretKey: "secret",
		OCIS3Bucket:    "bucket",
	}
	if !cfg.HasOCIS3() {
		t.Error("expected HasOCIS3=true when all fields set")
	}

	cfg.OCIS3Bucket = ""
	if cfg.HasOCIS3() {
		t.Error("expected HasOCIS3=false when bucket is empty")
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && searchString(s, sub)
}

func searchString(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

func setEnv(t *testing.T, vars map[string]string) {
	t.Helper()

	// Clear all config-related env vars first
	envKeys := []string{
		"ENV", "LOG_LEVEL", "HOST", "PORT", "CORS_ORIGINS",
		"DATABASE_URL", "SUPABASE_PROJECT_URL", "SUPABASE_JWT_AUD",
		"SUPABASE_JWT_JWKS_URL", "REDIS_URL",
		"MUSICBRAINZ_USER_AGENT", "LASTFM_API_KEY", "FANARTTV_API_KEY",
		"GENIUS_ACCESS_TOKEN", "OCI_S3_ENDPOINT", "OCI_S3_ACCESS_KEY",
		"OCI_S3_SECRET_KEY", "OCI_S3_BUCKET", "OCI_S3_REGION",
		"MUSIC_DIR", "FFMPEG_LOCATION", "YTDLP_COOKIE_FILE",
	}
	for _, k := range envKeys {
		os.Unsetenv(k)
	}

	for k, v := range vars {
		t.Setenv(k, v)
	}
}
