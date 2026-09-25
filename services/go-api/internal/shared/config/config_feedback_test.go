package config

import "testing"

// feedbackBaseEnv is the minimal valid environment plus feedback credentials,
// so the FEEDBACK_ENABLED flag is exercised independently of credential
// presence (the credentials stay set in every case).
func feedbackBaseEnv() map[string]string {
	return map[string]string{
		"SUPABASE_PROJECT_URL":  "https://example.supabase.co",
		"SUPABASE_JWT_JWKS_URL": "https://example.supabase.co/auth/v1/.well-known/jwks.json",
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
