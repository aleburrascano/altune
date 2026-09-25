package config

import (
	"os"
	"strings"
	"testing"
)

const validOperatorID = "11111111-1111-1111-1111-111111111111"

// The admin principals' ids in the case tests that exercise hex-case folding:
// an all-digit UUID is its own upper case, so it cannot tell canonicalization
// from a plain string compare.
const (
	hexOperatorID = "aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee"
	hexReadOnlyID = "bbbbbbbb-cccc-4ddd-8eee-ffffffffffff"
)

func TestLoad_MinimalValid(t *testing.T) {
	setEnv(t, validConfigEnv(nil))

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
			setEnv(t, validConfigEnv(map[string]string{
				"SUPABASE_JWT_JWKS_URL": tt.jwksURL,
			}))

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
			setEnv(t, validConfigEnv(map[string]string{
				"SUPABASE_PROJECT_URL": tt.projectURL,
			}))

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

// TestLoad_SupabaseJWTAudEmptyUsesDefault pins what the env library does with
// an explicitly empty value: env/v11 treats set-but-empty as absent and applies
// envDefault. An upgrade that started honouring the empty string instead would
// hand the verifier an audience no token can match, so this is the boundary the
// blank check below does not cover.
func TestLoad_SupabaseJWTAudEmptyUsesDefault(t *testing.T) {
	env := validConfigEnv(nil)
	env["SUPABASE_JWT_AUD"] = "" // set here, not as an override: an empty override removes the key

	setEnv(t, env)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.SupabaseJWTAud != "authenticated" {
		t.Errorf("expected empty SUPABASE_JWT_AUD to fall back to %q, got %q", "authenticated", cfg.SupabaseJWTAud)
	}
}

// TestLoad_SupabaseJWTAudBlank covers the values env/v11 does not replace with
// the default: whitespace-only ones survive parsing, and an audience that
// matches no token's aud claim lets the process start and then rejects every
// real token with claim_invalid_aud (#2182).
func TestLoad_SupabaseJWTAudBlank(t *testing.T) {
	tests := []struct {
		name string
		aud  string
	}{
		{name: "spaces", aud: "   "},
		{name: "tab and newline", aud: "\t\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setEnv(t, validConfigEnv(map[string]string{
				"SUPABASE_JWT_AUD": tt.aud,
			}))

			_, err := Load()
			if err == nil {
				t.Fatal("expected error for blank SUPABASE_JWT_AUD")
			}
			if !searchString(err.Error(), "SUPABASE_JWT_AUD") {
				t.Errorf("expected error to name SUPABASE_JWT_AUD, got: %v", err)
			}
		})
	}
}

func TestLoad_SupabaseJWTAudTrimmed(t *testing.T) {
	setEnv(t, validConfigEnv(map[string]string{
		"SUPABASE_JWT_AUD": "  authenticated\t\n",
	}))

	cfg, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.SupabaseJWTAud != "authenticated" {
		t.Errorf("expected SUPABASE_JWT_AUD trimmed to %q, got %q", "authenticated", cfg.SupabaseJWTAud)
	}
}

func TestLoad_CORSOriginsTrimmed(t *testing.T) {
	setEnv(t, validConfigEnv(map[string]string{
		"CORS_ORIGINS": "http://a, http://b ",
	}))

	cfg, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []string{"http://a", "http://b"}
	if len(cfg.CORSOrigins) != len(want) {
		t.Fatalf("expected %d origins, got %d: %#v", len(want), len(cfg.CORSOrigins), cfg.CORSOrigins)
	}
	for i, w := range want {
		if cfg.CORSOrigins[i] != w {
			t.Errorf("origin %d: expected %q, got %q", i, w, cfg.CORSOrigins[i])
		}
	}
}

func TestLoad_CORSOriginsMalformed(t *testing.T) {
	tests := []struct {
		name    string
		origins string
	}{
		{name: "no scheme", origins: "localhost:8081"},
		{name: "bare host", origins: "altune.app"},
		{name: "trailing slash", origins: "https://altune.app/"},
		{name: "path", origins: "https://altune.app/app"},
		{name: "empty entry between valid ones", origins: "http://a,,http://b"},
		{name: "match-all wildcard", origins: "*"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setEnv(t, validConfigEnv(map[string]string{
				"CORS_ORIGINS": tt.origins,
			}))

			_, err := Load()
			if err == nil {
				t.Fatal("expected error for malformed CORS_ORIGINS")
			}
			if !searchString(err.Error(), "CORS_ORIGINS") {
				t.Errorf("expected error to name CORS_ORIGINS, got: %v", err)
			}
		})
	}
}

func TestLoad_CORSOriginsAccepted(t *testing.T) {
	tests := []struct {
		name    string
		origins string
	}{
		{name: "http with port", origins: "http://localhost:8081"},
		{name: "https", origins: "https://altune-staging.duckdns.org"},
		{name: "subdomain wildcard", origins: "https://*.altune.app"},
		{name: "several", origins: "http://localhost:8081,http://localhost:19006"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setEnv(t, validConfigEnv(map[string]string{
				"CORS_ORIGINS": tt.origins,
			}))

			_, err := Load()
			if err != nil {
				t.Fatalf("unexpected error for CORS_ORIGINS=%q: %v", tt.origins, err)
			}
		})
	}
}

func TestLoad_ExplorationRateOutOfRange(t *testing.T) {
	tests := []struct {
		name string
		rate string
	}{
		{name: "percent mistaken for fraction", rate: "3"},
		{name: "just above one", rate: "1.5"},
		{name: "negative", rate: "-0.1"},
		{name: "not a number", rate: "NaN"},
		{name: "infinite", rate: "Inf"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setEnv(t, validConfigEnv(map[string]string{
				"EXPLORATION_RATE": tt.rate,
			}))

			_, err := Load()
			if err == nil {
				t.Fatalf("expected error for EXPLORATION_RATE=%s", tt.rate)
			}
			if !searchString(err.Error(), "EXPLORATION_RATE") {
				t.Errorf("expected error to name EXPLORATION_RATE, got: %v", err)
			}
		})
	}
}

func TestLoad_ExplorationRateInRange(t *testing.T) {
	tests := []struct {
		name string
		rate string
		want float64
	}{
		{name: "off", rate: "0", want: 0},
		{name: "three percent", rate: "0.03", want: 0.03},
		{name: "always explore", rate: "1", want: 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setEnv(t, validConfigEnv(map[string]string{
				"EXPLORATION_RATE": tt.rate,
			}))

			cfg, err := Load()
			if err != nil {
				t.Fatalf("unexpected error for EXPLORATION_RATE=%s: %v", tt.rate, err)
			}
			if cfg.ExplorationRate != tt.want {
				t.Errorf("expected ExplorationRate=%v, got %v", tt.want, cfg.ExplorationRate)
			}
		})
	}
}

func TestLoad_ExplorationRateDefault(t *testing.T) {
	setEnv(t, validConfigEnv(nil))

	cfg, err := Load()
	if err != nil {
		t.Fatalf("unexpected error with default EXPLORATION_RATE: %v", err)
	}
	if cfg.ExplorationRate != 0.03 {
		t.Errorf("expected default exploration_rate=0.03, got %v", cfg.ExplorationRate)
	}
}

func TestLoad_MusicBrainzUAWithoutContact(t *testing.T) {
	setEnv(t, validConfigEnv(map[string]string{
		"MUSICBRAINZ_USER_AGENT": "altune/0.1",
	}))

	_, err := Load()
	if err == nil {
		t.Fatal("expected error for MB user agent without contact info")
	}
}

func TestLoad_MusicBrainzUAWithEmail(t *testing.T) {
	setEnv(t, validConfigEnv(map[string]string{
		"MUSICBRAINZ_USER_AGENT": "altune/0.1 ( mailto:dev@altune.test )",
	}))

	cfg, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !cfg.HasMusicBrainz() {
		t.Error("expected HasMusicBrainz=true")
	}
}

func TestLoad_OperatorUserIDMissingOrMalformed(t *testing.T) {
	tests := []struct {
		name       string
		operatorID string
	}{
		{name: "missing", operatorID: ""},
		{name: "not a uuid", operatorID: "not-a-uuid"},
		{name: "truncated uuid", operatorID: "11111111-1111-1111-1111"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setEnv(t, validConfigEnv(map[string]string{
				"OPERATOR_USER_ID": tt.operatorID,
			}))

			_, err := Load()
			if err == nil {
				t.Fatal("expected error for missing/malformed OPERATOR_USER_ID")
			}
			if !searchString(err.Error(), "OPERATOR_USER_ID") {
				t.Errorf("expected error to name OPERATOR_USER_ID, got: %v", err)
			}
		})
	}
}

// TestLoad_OperatorReadOnlyUserIDRejected pins the misconfigurations that would
// hand the read-only admin principal write scope (#1810): an id that is not a
// UUID, and one that is the operator's own id in any hex case.
func TestLoad_OperatorReadOnlyUserIDRejected(t *testing.T) {
	tests := []struct {
		name       string
		readOnlyID string
	}{
		{name: "not a uuid", readOnlyID: "not-a-uuid"},
		{name: "equal to the operator", readOnlyID: hexOperatorID},
		{name: "equal to the operator in upper case", readOnlyID: strings.ToUpper(hexOperatorID)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setEnv(t, validConfigEnv(map[string]string{
				"OPERATOR_USER_ID":          hexOperatorID,
				"OPERATOR_READONLY_USER_ID": tt.readOnlyID,
			}))

			_, err := Load()
			if err == nil {
				t.Fatal("expected error for a read-only principal that carries write scope")
			}
			if !searchString(err.Error(), "OPERATOR_READONLY_USER_ID") {
				t.Errorf("expected error to name OPERATOR_READONLY_USER_ID, got: %v", err)
			}
		})
	}
}

// TestLoad_OperatorIDsAreCanonical pins that both admin principals are stored as
// canonical lower-case UUID text: the admin gate compares them to a JWT subject
// by string, so the hex case someone pasted must not decide who gets in.
func TestLoad_OperatorIDsAreCanonical(t *testing.T) {
	setEnv(t, validConfigEnv(map[string]string{
		"OPERATOR_USER_ID":          strings.ToUpper(hexOperatorID),
		"OPERATOR_READONLY_USER_ID": strings.ToUpper(hexReadOnlyID),
	}))

	cfg, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.OperatorUserID != hexOperatorID {
		t.Errorf("OperatorUserID = %q, want %q", cfg.OperatorUserID, hexOperatorID)
	}
	if cfg.OperatorReadOnlyUserID != hexReadOnlyID {
		t.Errorf("OperatorReadOnlyUserID = %q, want %q", cfg.OperatorReadOnlyUserID, hexReadOnlyID)
	}
}

func TestLoad_AcquisitionConcurrencyNotPositive(t *testing.T) {
	tests := []struct {
		name  string
		value string
	}{
		{name: "zero", value: "0"},
		{name: "negative", value: "-1"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setEnv(t, validConfigEnv(map[string]string{
				"ACQUISITION_CONCURRENCY": tt.value,
			}))

			_, err := Load()
			if err == nil {
				t.Fatal("expected error for non-positive ACQUISITION_CONCURRENCY")
			}
			if !searchString(err.Error(), "ACQUISITION_CONCURRENCY") {
				t.Errorf("expected error to name ACQUISITION_CONCURRENCY, got: %v", err)
			}
		})
	}
}

func TestLoad_AcquisitionConcurrencyDefaultValid(t *testing.T) {
	setEnv(t, validConfigEnv(nil))

	cfg, err := Load()
	if err != nil {
		t.Fatalf("unexpected error with default ACQUISITION_CONCURRENCY: %v", err)
	}
	if cfg.AcquisitionConcurrency != 5 {
		t.Errorf("expected default acquisition_concurrency=5, got %d", cfg.AcquisitionConcurrency)
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
		"SUPABASE_JWT_JWKS_URL", "REDIS_URL",
		"MUSICBRAINZ_USER_AGENT", "LASTFM_API_KEY", "FANARTTV_API_KEY",
		"GENIUS_ACCESS_TOKEN", "OCI_S3_ENDPOINT", "OCI_S3_ACCESS_KEY",
		"OCI_S3_SECRET_KEY", "OCI_S3_BUCKET", "OCI_S3_REGION",
		"MUSIC_DIR", "FFMPEG_LOCATION", "YTDLP_COOKIE_FILE",
		"OPERATOR_USER_ID", "OPERATOR_READONLY_USER_ID",
		"ACQUISITION_CONCURRENCY",
		"GITHUB_ISSUE_REPO", "GITHUB_ISSUE_TOKEN", "EXPLORATION_RATE",
		"DB_POOL_MAX_CONNS", "REDIS_POOL_SIZE",
	}
	for _, k := range envKeys {
		os.Unsetenv(k)
	}

	for k, v := range vars {
		t.Setenv(k, v)
	}
}

// validConfigEnv returns the canonical set of env vars that Load() currently
// requires, with per-test overrides layered on top. This is the single place a
// newly required env var needs adding. An override whose value is the empty
// string removes that key, letting a test exercise a missing required var.
func validConfigEnv(overrides map[string]string) map[string]string {
	env := map[string]string{
		"SUPABASE_PROJECT_URL":  "https://example.supabase.co",
		"SUPABASE_JWT_JWKS_URL": "https://example.supabase.co/auth/v1/.well-known/jwks.json",
		"OPERATOR_USER_ID":      validOperatorID,
	}
	for k, v := range overrides {
		if v == "" {
			delete(env, k)
			continue
		}
		env[k] = v
	}
	return env
}
