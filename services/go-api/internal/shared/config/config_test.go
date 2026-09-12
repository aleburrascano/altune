package config

import (
	"os"
	"testing"
)

func TestLoad_MinimalValid(t *testing.T) {
	setEnv(t, map[string]string{
		"SUPABASE_PROJECT_URL":  "https://example.supabase.co",
		"SUPABASE_JWT_JWKS_URL": "https://example.supabase.co/auth/v1/.well-known/jwks.json",
		"SUPABASE_ANON_KEY":     "anon-key",
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

func TestLoad_SupabaseJWKSURLMalformed(t *testing.T) {
	tests := []struct {
		name    string
		jwksURL string
	}{
		{name: "no scheme", jwksURL: "example.supabase.co/jwks"},
		{name: "no host", jwksURL: "https://"},
		{name: "bare path", jwksURL: "/auth/v1/.well-known/jwks.json"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setEnv(t, map[string]string{
				"SUPABASE_PROJECT_URL":  "https://example.supabase.co",
				"SUPABASE_JWT_JWKS_URL": tt.jwksURL,
				"SUPABASE_ANON_KEY":     "anon-key",
			})

			_, err := Load()
			if err == nil {
				t.Fatal("expected error for malformed SUPABASE_JWT_JWKS_URL")
			}
			if !searchString(err.Error(), "SUPABASE_JWT_JWKS_URL") {
				t.Errorf("expected error to name SUPABASE_JWT_JWKS_URL, got: %v", err)
			}
		})
	}
}

func TestLoad_SupabaseProjectURLMissingOrMalformed(t *testing.T) {
	tests := []struct {
		name       string
		projectURL string
	}{
		{name: "missing", projectURL: ""},
		{name: "no scheme", projectURL: "example.supabase.co"},
		{name: "no host", projectURL: "https://"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env := map[string]string{
				"SUPABASE_JWT_JWKS_URL": "https://example.supabase.co/auth/v1/.well-known/jwks.json",
			}
			if tt.projectURL != "" {
				env["SUPABASE_PROJECT_URL"] = tt.projectURL
			}
			setEnv(t, env)

			_, err := Load()
			if err == nil {
				t.Fatal("expected error for missing/malformed SUPABASE_PROJECT_URL")
			}
			if !searchString(err.Error(), "SUPABASE_PROJECT_URL") {
				t.Errorf("expected error to name SUPABASE_PROJECT_URL, got: %v", err)
			}
		})
	}
}

func TestLoad_MissingAnonKey(t *testing.T) {
	tests := []struct {
		name    string
		anonKey string
	}{
		{name: "missing", anonKey: ""},
		{name: "blank", anonKey: "   "},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env := map[string]string{
				"SUPABASE_PROJECT_URL":  "https://example.supabase.co",
				"SUPABASE_JWT_JWKS_URL": "https://example.supabase.co/auth/v1/.well-known/jwks.json",
			}
			if tt.anonKey != "" {
				env["SUPABASE_ANON_KEY"] = tt.anonKey
			}
			setEnv(t, env)

			_, err := Load()
			if err == nil {
				t.Fatal("expected error for missing/blank SUPABASE_ANON_KEY")
			}
			if !searchString(err.Error(), "SUPABASE_ANON_KEY") {
				t.Errorf("expected error to name SUPABASE_ANON_KEY, got: %v", err)
			}
		})
	}
}

func TestLoad_MusicBrainzUAWithoutContact(t *testing.T) {
	setEnv(t, map[string]string{
		"SUPABASE_PROJECT_URL":   "https://example.supabase.co",
		"SUPABASE_JWT_JWKS_URL":  "https://example.supabase.co/auth/v1/.well-known/jwks.json",
		"SUPABASE_ANON_KEY":      "anon-key",
		"MUSICBRAINZ_USER_AGENT": "altune/0.1",
	})

	_, err := Load()
	if err == nil {
		t.Fatal("expected error for MB user agent without contact info")
	}
}

func TestLoad_MusicBrainzUAWithEmail(t *testing.T) {
	setEnv(t, map[string]string{
		"SUPABASE_PROJECT_URL":   "https://example.supabase.co",
		"SUPABASE_JWT_JWKS_URL":  "https://example.supabase.co/auth/v1/.well-known/jwks.json",
		"SUPABASE_ANON_KEY":      "anon-key",
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

	envKeys := []string{
		"ENV", "LOG_LEVEL", "HOST", "PORT", "CORS_ORIGINS",
		"DATABASE_URL", "SUPABASE_PROJECT_URL", "SUPABASE_JWT_AUD",
		"SUPABASE_JWT_JWKS_URL", "SUPABASE_ANON_KEY", "REDIS_URL",
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
