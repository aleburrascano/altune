package config

import "testing"

// feedbackBaseEnv is the minimal valid environment plus feedback credentials,
// so the FEEDBACK_ENABLED flag is exercised independently of credential
// presence (the credentials stay set in every case).
func feedbackBaseEnv() map[string]string {
	return map[string]string{
		"SUPABASE_PROJECT_URL":  "https://example.supabase.co",
		"SUPABASE_JWT_JWKS_URL": "https://example.supabase.co/auth/v1/.well-known/jwks.json",
		"SUPABASE_ANON_KEY":     "anon-key",
		"OPERATOR_USER_ID":      validOperatorID,
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
