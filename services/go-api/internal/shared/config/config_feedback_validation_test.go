package config

import "testing"

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
