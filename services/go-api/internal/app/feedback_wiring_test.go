package app

import (
	"altune/go-api/internal/shared/config"
	"testing"
)

// TestWireFeedback_KillSwitch reproduces the gap where the feedback/GitHub
// integration could only be disabled by blanking the stored credentials: the
// dedicated FEEDBACK_ENABLED flag must gate the wiring independently, so the
// feature can be turned off (and back on) at runtime without discarding
// GITHUB_ISSUE_REPO / GITHUB_ISSUE_TOKEN.
func TestWireFeedback_KillSwitch(t *testing.T) {
	tests := []struct {
		name        string
		enabled     bool
		wantHandler bool
	}{
		{"flag on with creds wires the handler", true, true},
		{"flag off with creds present disables the handler", false, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := &App{cfg: &config.Config{
				FeedbackEnabled:  tt.enabled,
				GitHubIssueRepo:  "aleburrascano/altune",
				GitHubIssueToken: "ghp_secret",
			}}

			handler := a.wireFeedback()

			if tt.wantHandler && handler == nil {
				t.Fatal("expected feedback handler to be wired when FEEDBACK_ENABLED is on with creds present")
			}
			if !tt.wantHandler && handler != nil {
				t.Fatal("expected feedback handler to be nil when FEEDBACK_ENABLED is off, even with creds present")
			}
		})
	}
}
