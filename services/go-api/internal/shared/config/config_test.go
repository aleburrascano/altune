package config

import (
	"os"
	"strings"
	"testing"
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

func TestLoad_NoOperatorVarsRequired(t *testing.T) {
	setEnv(t, validConfigEnv(nil))

	if _, err := Load(); err != nil {
		t.Fatalf("unexpected error with no OPERATOR_* set: %v", err)
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

// feedbackBaseEnv is the minimal valid environment plus feedback credentials,
// so the FEEDBACK_ENABLED flag is exercised independently of credential
// presence (the credentials stay set in every case).
func feedbackBaseEnv() map[string]string {
	return map[string]string{
		"SUPABASE_PROJECT_URL":  "https://example.supabase.co",
		"SUPABASE_JWT_JWKS_URL": "https://example.supabase.co/auth/v1/.well-known/jwks.json",
		"GITHUB_ISSUE_REPO":     "aleburrascano/altune",
		"GITHUB_ISSUE_TOKEN":    "ghp_secret",
	}
}

// TestLoad_FeedbackEnabledDefaultsOn guards that FEEDBACK_ENABLED defaults to
// enabled, so existing deployments with credentials keep wiring the feedback
// integration exactly as before unless an operator opts out.
func TestLoad_FeedbackEnabledDefaultsOn(t *testing.T) {
	setEnv(t, feedbackBaseEnv())

	cfg, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !cfg.FeedbackEnabled {
		t.Error("expected FEEDBACK_ENABLED to default to true (preserve current behavior)")
	}
	if !cfg.HasIssueTracker() {
		t.Error("precondition: credentials must remain configured")
	}
}

// TestLoad_FeedbackEnabledRespectsEnv guards that FEEDBACK_ENABLED=false turns
// the flag off while the stored credentials remain intact, so the feature can
// be disabled at runtime without discarding the repo/token values.
func TestLoad_FeedbackEnabledRespectsEnv(t *testing.T) {
	env := feedbackBaseEnv()
	env["FEEDBACK_ENABLED"] = "false"
	setEnv(t, env)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.FeedbackEnabled {
		t.Error("expected FEEDBACK_ENABLED=false to disable the feedback integration")
	}
	if !cfg.HasIssueTracker() {
		t.Error("expected credentials to stay configured when the flag is off")
	}
}

// TestHasIssueTracker_PassesMalformedRepo documents the gap that motivated the
// startup format check: HasIssueTracker only gates on presence, so a repo that
// is not owner/repo shape still reports true and would only fail at the first
// user submission. Load() is what must reject it (see below).
func TestHasIssueTracker_PassesMalformedRepo(t *testing.T) {
	cfg := &Config{GitHubIssueRepo: "altune-no-slash", GitHubIssueToken: "ghp_secret"}
	if !cfg.HasIssueTracker() {
		t.Fatal("HasIssueTracker gates on presence only, so a malformed repo still reports true")
	}
}

// TestLoad_GitHubIssueRepoMalformed guards that a repo which is not in
// owner/repo shape fails loud at startup, naming GITHUB_ISSUE_REPO, instead of
// starting up healthy and only erroring at first submission.
func TestLoad_GitHubIssueRepoMalformed(t *testing.T) {
	tests := []struct {
		name string
		repo string
	}{
		{name: "missing slash", repo: "altune-no-slash"},
		{name: "empty owner", repo: "/altune"},
		{name: "empty repo", repo: "aleburrascano/"},
		{name: "too many segments", repo: "aleburrascano/altune/extra"},
		{name: "embedded space", repo: "aleburrascano/al tune"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env := feedbackBaseEnv()
			env["GITHUB_ISSUE_REPO"] = tt.repo
			setEnv(t, env)

			_, err := Load()
			if err == nil {
				t.Fatal("expected error for malformed GITHUB_ISSUE_REPO")
			}
			if !searchString(err.Error(), "GITHUB_ISSUE_REPO") {
				t.Errorf("expected error to name GITHUB_ISSUE_REPO, got: %v", err)
			}
		})
	}
}

// TestLoad_GitHubIssueRepoValid guards that a well-formed owner/repo slug loads
// cleanly and wires the issue tracker on, so the format check never rejects a
// legitimate config.
func TestLoad_GitHubIssueRepoValid(t *testing.T) {
	setEnv(t, feedbackBaseEnv())

	cfg, err := Load()
	if err != nil {
		t.Fatalf("unexpected error for valid GITHUB_ISSUE_REPO: %v", err)
	}
	if !cfg.HasIssueTracker() {
		t.Error("expected HasIssueTracker=true for a valid owner/repo slug")
	}
}

// TestLoad_GitHubIssueRepoOptionalWhenUnset guards that the format check is
// skipped when no repo is configured, so deployments without the feedback
// integration still load.
func TestLoad_GitHubIssueRepoOptionalWhenUnset(t *testing.T) {
	env := feedbackBaseEnv()
	delete(env, "GITHUB_ISSUE_REPO")
	delete(env, "GITHUB_ISSUE_TOKEN")
	setEnv(t, env)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("unexpected error when GITHUB_ISSUE_REPO unset: %v", err)
	}
	if cfg.HasIssueTracker() {
		t.Error("expected HasIssueTracker=false when credentials unset")
	}
}

func TestLoad_FeedbackCredentialShape(t *testing.T) {
	tests := []struct {
		name    string
		repo    string
		token   string
		wantErr string
	}{
		{"token without repo", "", "ghp_x", "GITHUB_ISSUE_REPO"},
		{"repo without token", "a/b", "", "GITHUB_ISSUE_TOKEN"},
		{"dotdot repo", "owner/..", "ghp_x", "GITHUB_ISSUE_REPO"},
		{"query in repo", "owner/re?po", "ghp_x", "GITHUB_ISSUE_REPO"},
		{"fragment in repo", "a/b#f", "ghp_x", "GITHUB_ISSUE_REPO"},
		{"carriage return in repo", "a/b\r", "ghp_x", "GITHUB_ISSUE_REPO"},
		{"whitespace inside token", "a/b", "tok en", "GITHUB_ISSUE_TOKEN"},
		{"both empty", "", "", ""},
		{"trailing newline token is trimmed", "a/b", "tok\n", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env := feedbackBaseEnv()
			env["GITHUB_ISSUE_REPO"] = tt.repo
			env["GITHUB_ISSUE_TOKEN"] = tt.token
			setEnv(t, env)

			cfg, err := Load()

			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				if tt.token == "tok\n" && cfg.GitHubIssueToken != "tok" {
					t.Fatalf("token = %q, want trimmed", cfg.GitHubIssueToken)
				}
				return
			}
			if err == nil || !searchString(err.Error(), tt.wantErr) {
				t.Fatalf("err = %v, want it to name %s", err, tt.wantErr)
			}
		})
	}
}

func TestLoad_FeedbackDisabledBothEmptyStarts(t *testing.T) {
	env := feedbackBaseEnv()
	env["GITHUB_ISSUE_REPO"] = ""
	env["GITHUB_ISSUE_TOKEN"] = ""
	env["FEEDBACK_ENABLED"] = "false"
	setEnv(t, env)

	if _, err := Load(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestConfig_HasRedis(t *testing.T) {
	tests := []struct {
		name     string
		redisURL string
		want     bool
	}{
		{name: "set", redisURL: "redis://localhost:6379", want: true},
		{name: "empty", redisURL: "", want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &Config{RedisURL: tt.redisURL}
			if got := cfg.HasRedis(); got != tt.want {
				t.Errorf("HasRedis() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestConfig_HasLastFM(t *testing.T) {
	tests := []struct {
		name string
		key  string
		want bool
	}{
		{name: "set", key: "abc123", want: true},
		{name: "empty", key: "", want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &Config{LastFMAPIKey: tt.key}
			if got := cfg.HasLastFM(); got != tt.want {
				t.Errorf("HasLastFM() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestConfig_HasFanartTV(t *testing.T) {
	tests := []struct {
		name string
		key  string
		want bool
	}{
		{name: "set", key: "fanart-key", want: true},
		{name: "empty", key: "", want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &Config{FanartTVAPIKey: tt.key}
			if got := cfg.HasFanartTV(); got != tt.want {
				t.Errorf("HasFanartTV() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestConfig_HasGenius(t *testing.T) {
	tests := []struct {
		name  string
		token string
		want  bool
	}{
		{name: "set", token: "genius-token", want: true},
		{name: "empty", token: "", want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &Config{GeniusAccessToken: tt.token}
			if got := cfg.HasGenius(); got != tt.want {
				t.Errorf("HasGenius() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestConfig_HasMusicBrainz(t *testing.T) {
	tests := []struct {
		name string
		ua   string
		want bool
	}{
		{name: "set", ua: "altune/0.1 ( mailto:dev@test.com )", want: true},
		{name: "empty", ua: "", want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &Config{MusicBrainzUserAgent: tt.ua}
			if got := cfg.HasMusicBrainz(); got != tt.want {
				t.Errorf("HasMusicBrainz() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestConfig_IsDevelopment(t *testing.T) {
	tests := []struct {
		name string
		env  string
		want bool
	}{
		{name: "development", env: "development", want: true},
		{name: "production", env: "production", want: false},
		{name: "staging", env: "staging", want: false},
		{name: "empty", env: "", want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &Config{Env: tt.env}
			if got := cfg.IsDevelopment(); got != tt.want {
				t.Errorf("IsDevelopment() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestConfig_HasOCIS3_AllCombinations(t *testing.T) {
	tests := []struct {
		name     string
		endpoint string
		access   string
		secret   string
		bucket   string
		want     bool
	}{
		{name: "all set", endpoint: "e", access: "a", secret: "s", bucket: "b", want: true},
		{name: "missing endpoint", endpoint: "", access: "a", secret: "s", bucket: "b", want: false},
		{name: "missing access key", endpoint: "e", access: "", secret: "s", bucket: "b", want: false},
		{name: "missing secret key", endpoint: "e", access: "a", secret: "", bucket: "b", want: false},
		{name: "missing bucket", endpoint: "e", access: "a", secret: "s", bucket: "", want: false},
		{name: "all empty", endpoint: "", access: "", secret: "", bucket: "", want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &Config{
				OCIS3Endpoint:  tt.endpoint,
				OCIS3AccessKey: tt.access,
				OCIS3SecretKey: tt.secret,
				OCIS3Bucket:    tt.bucket,
			}
			if got := cfg.HasOCIS3(); got != tt.want {
				t.Errorf("HasOCIS3() = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestLoad_PoolSizes guards the connection-pool knobs (#1610): both default to
// a ceiling derived from this service's own concurrency rather than from the
// host's CPU count, and an operator can tune either from the environment.
func TestLoad_PoolSizes(t *testing.T) {
	cases := []struct {
		name          string
		env           map[string]string
		wantDBMax     int
		wantRedisSize int
	}{
		{name: "defaults", env: nil, wantDBMax: 20, wantRedisSize: 50},
		{
			name:          "tuned",
			env:           map[string]string{"DB_POOL_MAX_CONNS": "40", "REDIS_POOL_SIZE": "80"},
			wantDBMax:     40,
			wantRedisSize: 80,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			env := feedbackBaseEnv()
			for k, v := range tc.env {
				env[k] = v
			}
			setEnv(t, env)

			cfg, err := Load()
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if cfg.DBPoolMaxConns != tc.wantDBMax {
				t.Errorf("DBPoolMaxConns = %d, want %d", cfg.DBPoolMaxConns, tc.wantDBMax)
			}
			if cfg.RedisPoolSize != tc.wantRedisSize {
				t.Errorf("RedisPoolSize = %d, want %d", cfg.RedisPoolSize, tc.wantRedisSize)
			}
		})
	}
}

func TestLoad_RedisURL(t *testing.T) {
	cases := []struct {
		name    string
		url     string
		wantErr bool
	}{
		{name: "unset", url: ""},
		{name: "valid", url: "redis://localhost:6379/0"},
		{name: "padded", url: " redis://localhost:6379", wantErr: true},
		{name: "wrong scheme", url: "http://localhost:6379", wantErr: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			env := feedbackBaseEnv()
			env["REDIS_URL"] = tc.url
			setEnv(t, env)

			_, err := Load()
			if !tc.wantErr {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), "REDIS_URL") {
				t.Fatalf("err = %v, want one naming REDIS_URL", err)
			}
		})
	}
}

func TestLoad_RedisURLErrorRedactsCredentials(t *testing.T) {
	env := feedbackBaseEnv()
	env["REDIS_URL"] = "redis://user:s3cret@host:99999999/x"
	setEnv(t, env)

	_, err := Load()
	if err == nil {
		t.Fatal("want error")
	}
	if strings.Contains(err.Error(), "s3cret") {
		t.Fatalf("error leaks credentials: %v", err)
	}
}

// TestLoad_SSEMaxConns guards the SSE_MAX_CONNS knob (#1022): it defaults to a
// bounded global ceiling and an operator can tune it from the environment.
func TestLoad_SSEMaxConns(t *testing.T) {
	cases := []struct {
		name string
		env  string
		want int
	}{
		{name: "default", env: "", want: 2048},
		{name: "tuned", env: "500", want: 500},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("SSE_MAX_CONNS", "")
			if err := os.Unsetenv("SSE_MAX_CONNS"); err != nil {
				t.Fatal(err)
			}
			env := feedbackBaseEnv()
			if tc.env != "" {
				env["SSE_MAX_CONNS"] = tc.env
			}
			setEnv(t, env)

			cfg, err := Load()
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if cfg.SSEMaxConns != tc.want {
				t.Fatalf("SSEMaxConns = %d, want %d", cfg.SSEMaxConns, tc.want)
			}
		})
	}
}

// The JWKS endpoint is the trust root for every bearer-token signature check,
// so a plaintext URL would let a network-positioned attacker substitute the key
// set and mint tokens that verify (#1030).
func TestLoad_SupabaseURLsRejectPlaintext(t *testing.T) {
	fields := []string{"SUPABASE_JWT_JWKS_URL", "SUPABASE_PROJECT_URL"}
	urls := []struct {
		name string
		url  string
	}{
		{name: "http remote host", url: "http://example.supabase.co/auth/v1/.well-known/jwks.json"},
		{name: "http upper-case scheme", url: "HTTP://example.supabase.co/auth/v1/.well-known/jwks.json"},
		{name: "http userinfo spoofing loopback", url: "http://127.0.0.1@example.supabase.co/jwks"},
		{name: "http loopback-looking subdomain", url: "http://localhost.example.com/jwks"},
		{name: "ftp", url: "ftp://example.supabase.co/jwks"},
		{name: "ws", url: "ws://example.supabase.co/jwks"},
	}
	for _, field := range fields {
		for _, tt := range urls {
			t.Run(field+"/"+tt.name, func(t *testing.T) {
				setEnv(t, validConfigEnv(map[string]string{field: tt.url}))

				_, err := Load()
				if err == nil {
					t.Fatalf("expected %s=%q to be rejected as non-https", field, tt.url)
				}
				if !strings.Contains(err.Error(), field) || !strings.Contains(err.Error(), "https") {
					t.Errorf("error should name %s and the https requirement, got: %v", field, err)
				}
			})
		}
	}
}

// Loopback traffic never crosses a network an attacker can sit on, so local
// Supabase (supabase start serves http://127.0.0.1:54321) stays usable.
func TestLoad_SupabaseURLsAllowLoopbackHTTP(t *testing.T) {
	for _, base := range []string{
		"http://127.0.0.1:54321",
		"http://localhost:54321",
		"http://LOCALHOST:54321",
		"http://[::1]:54321",
	} {
		t.Run(base, func(t *testing.T) {
			setEnv(t, validConfigEnv(map[string]string{
				"SUPABASE_PROJECT_URL":  base,
				"SUPABASE_JWT_JWKS_URL": base + "/auth/v1/.well-known/jwks.json",
			}))

			if _, err := Load(); err != nil {
				t.Fatalf("loopback http should be accepted, got: %v", err)
			}
		})
	}
}

// TestAuthEnabled must require BOTH an explicit opt-in (TEST_AUTH_ENABLED=true)
// AND a non-prod ENV, and fail closed on any absent/ambiguous input.
func TestTestAuthEnabled_RequiresOptInAndNonProdEnv(t *testing.T) {
	tests := []struct {
		name  string
		env   string
		optIn bool
		want  bool
	}{
		// Only opt-in + non-prod env enables the backdoor.
		{"optin + development enables", "development", true, true},
		{"optin + test enables", "test", true, true},
		{"optin + case-insensitive env", "Development", true, true},
		{"optin + trims padding", "  test  ", true, true},

		// Opt-in alone in a prod-like or unknown env stays DISABLED.
		{"optin + production disabled", "production", true, false},
		{"optin + prod disabled", "prod", true, false},
		{"optin + unknown env disabled", "staging", true, false},
		{"optin + misspelled env disabled", "developmnt", true, false},
		{"optin + substring not matched", "development-prod", true, false},
		{"optin + empty env disabled (fail closed)", "", true, false},

		// Non-prod env WITHOUT the explicit opt-in stays DISABLED (fail closed):
		// this is the #1384 case — ENV defaults to development, so ENV alone must
		// never be sufficient.
		{"no optin + development disabled", "development", false, false},
		{"no optin + test disabled", "test", false, false},

		// Unset ENV and no opt-in — the exact prod-misconfig shape — DISABLED.
		{"unset env + no optin disabled (fail closed)", "", false, false},
		{"production + no optin disabled", "production", false, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &Config{Env: tt.env, TestAuthOptIn: tt.optIn}
			if got := cfg.TestAuthEnabled(); got != tt.want {
				t.Errorf("TestAuthEnabled() with Env=%q optIn=%v = %v, want %v",
					tt.env, tt.optIn, got, tt.want)
			}
		})
	}
}

// The zero-value Config (ENV unset, opt-in unset) — the shape a prod deploy that
// forgets to set anything lands in — must resolve to DISABLED.
func TestTestAuthEnabled_ZeroValueFailsClosed(t *testing.T) {
	if (&Config{}).TestAuthEnabled() {
		t.Fatal("zero-value Config enabled test auth; must fail closed to DISABLED")
	}
}

const hexOverseerID = "cccccccc-dddd-4eee-8fff-000000000000"

func loadWithOverseerPrincipal(t *testing.T, raw string) (*Config, error) {
	t.Helper()
	setEnv(t, validConfigEnv(nil))
	t.Setenv("OVERSEER_PRINCIPAL_ID", raw)
	return Load()
}

func TestLoad_OverseerPrincipalIsCanonical(t *testing.T) {
	cfg, err := loadWithOverseerPrincipal(t, "  "+strings.ToUpper(hexOverseerID)+" ")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.OverseerPrincipalID != hexOverseerID {
		t.Errorf("OverseerPrincipalID = %q, want %q", cfg.OverseerPrincipalID, hexOverseerID)
	}
}

func TestLoad_OverseerPrincipalIsOptional(t *testing.T) {
	for _, raw := range []string{"", "   "} {
		cfg, err := loadWithOverseerPrincipal(t, raw)
		if err != nil {
			t.Fatalf("OVERSEER_PRINCIPAL_ID=%q: unexpected error: %v", raw, err)
		}
		if cfg.OverseerPrincipalID != "" {
			t.Errorf("OVERSEER_PRINCIPAL_ID=%q: got %q, want empty", raw, cfg.OverseerPrincipalID)
		}
	}
}

func TestLoad_OverseerPrincipalMustBeAUUID(t *testing.T) {
	_, err := loadWithOverseerPrincipal(t, "overseer")
	if err == nil || !strings.Contains(err.Error(), "OVERSEER_PRINCIPAL_ID") {
		t.Fatalf("err = %v, want an error naming OVERSEER_PRINCIPAL_ID", err)
	}
}

// TestLoad_YtProviderTogglesDefaultEnabled guards that the new per-source kill
// switches default to enabled, so existing deployments keep wiring ytmusic and
// yt-dlp exactly as before unless an operator opts out.
func TestLoad_YtProviderTogglesDefaultEnabled(t *testing.T) {
	setEnv(t, validConfigEnv(nil))

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
	setEnv(t, validConfigEnv(map[string]string{
		"YTMUSIC_ENABLED": "false",
		"YTDLP_ENABLED":   "false",
	}))

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
	base := validConfigEnv(nil)
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

// TestLoad_AudioPrefetchKillSwitch guards that AUDIO_PREFETCH_ENABLED defaults
// to true (clients keep prefetching) and that false turns it off remotely.
func TestLoad_AudioPrefetchKillSwitch(t *testing.T) {
	t.Setenv("AUDIO_PREFETCH_ENABLED", "")
	os.Unsetenv("AUDIO_PREFETCH_ENABLED")
	setEnv(t, validConfigEnv(nil))
	cfg, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !cfg.AudioPrefetchEnabled {
		t.Error("expected AUDIO_PREFETCH_ENABLED to default to true (preserve current behavior)")
	}

	t.Setenv("AUDIO_PREFETCH_ENABLED", "false")
	cfg, err = Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.AudioPrefetchEnabled {
		t.Error("expected AUDIO_PREFETCH_ENABLED=false to disable client prefetching")
	}
}

// TestLoad_NowPlayingEnrichmentKillSwitch guards the env contract of
// PLAYBACK_NOW_PLAYING_ENRICHMENT_ENABLED (#1125): enabled by default so resume
// keeps enriching the current track, and false sheds the lookup.
func TestLoad_NowPlayingEnrichmentKillSwitch(t *testing.T) {
	t.Setenv("PLAYBACK_NOW_PLAYING_ENRICHMENT_ENABLED", "")
	os.Unsetenv("PLAYBACK_NOW_PLAYING_ENRICHMENT_ENABLED")
	setEnv(t, validConfigEnv(nil))
	cfg, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !cfg.HasNowPlayingEnrichment() {
		t.Error("expected PLAYBACK_NOW_PLAYING_ENRICHMENT_ENABLED to default to true (preserve current behavior)")
	}

	t.Setenv("PLAYBACK_NOW_PLAYING_ENRICHMENT_ENABLED", "false")
	cfg, err = Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.HasNowPlayingEnrichment() {
		t.Error("expected PLAYBACK_NOW_PLAYING_ENRICHMENT_ENABLED=false to disable now-playing enrichment")
	}
}

// TestLoad_AcquisitionPaused guards the ACQUISITION_PAUSED startup setting
// (#2800): it defaults to false and an operator can pause acquisition from
// the environment, without an /admin POST.
func TestLoad_AcquisitionPaused(t *testing.T) {
	cases := []struct {
		name string
		env  string
		want bool
	}{
		{name: "default", env: "", want: false},
		{name: "paused", env: "true", want: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			setEnv(t, validConfigEnv(nil))
			if tc.env != "" {
				t.Setenv("ACQUISITION_PAUSED", tc.env)
			}

			cfg, err := Load()
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if cfg.AcquisitionPaused != tc.want {
				t.Fatalf("AcquisitionPaused = %v, want %v", cfg.AcquisitionPaused, tc.want)
			}
		})
	}
}

// TestLoad_DisabledJobs guards the DISABLED_JOBS startup setting (#2800): it
// splits on commas, trims whitespace around each name, and an unset or empty
// value leaves no jobs disabled.
func TestLoad_DisabledJobs(t *testing.T) {
	cases := []struct {
		name string
		env  string
		set  bool
		want []string
	}{
		{name: "unset", set: false, want: nil},
		{name: "empty", env: "", set: true, want: nil},
		{name: "trims whitespace", env: "eval meter, stream recovery", set: true, want: []string{"eval meter", "stream recovery"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			setEnv(t, validConfigEnv(nil))
			if tc.set {
				t.Setenv("DISABLED_JOBS", tc.env)
			}

			cfg, err := Load()
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(cfg.DisabledJobs) != len(tc.want) {
				t.Fatalf("DisabledJobs = %v, want %v", cfg.DisabledJobs, tc.want)
			}
			for i, name := range tc.want {
				if cfg.DisabledJobs[i] != name {
					t.Fatalf("DisabledJobs = %v, want %v", cfg.DisabledJobs, tc.want)
				}
			}
		})
	}
}
