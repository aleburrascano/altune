package app

import (
	"altune/go-api/internal/shared/config"
	"altune/go-api/internal/shared/httputil"
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
)

// TestWireFeedback_KillSwitch reproduces the gap where the feedback/GitHub
// integration could only be disabled by blanking the stored credentials: the
// dedicated FEEDBACK_ENABLED flag must gate the wiring independently, so the
// feature can be turned off (and back on) by config without discarding
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

// TestMountFeedback_DisabledAnswers503WithCode proves the kill switch at the
// route level: with FEEDBACK_ENABLED off (credentials still present) the real
// submit handler is not mounted, and a report POST gets a coded 503 the client
// can recognize instead of chi's bare plain-text 404.
func TestMountFeedback_DisabledAnswers503WithCode(t *testing.T) {
	tests := []struct {
		name       string
		enabled    bool
		repo       string
		wantStatus int
		wantCode   string
	}{
		// The real handler is mounted: with no auth middleware in front of it,
		// RequireUserID answers 401, proving the request reached it.
		{"flag on with creds mounts the submit handler", true, "aleburrascano/altune", http.StatusUnauthorized, ""},
		{"flag off with creds present answers disabled", false, "aleburrascano/altune", http.StatusServiceUnavailable, "feedback.disabled"},
		{"flag on without creds answers disabled", true, "", http.StatusServiceUnavailable, "feedback.disabled"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := &App{cfg: &config.Config{
				FeedbackEnabled:  tt.enabled,
				GitHubIssueRepo:  tt.repo,
				GitHubIssueToken: "ghp_secret",
			}}
			r := chi.NewRouter()
			mountFeedback(r, a.wireFeedback())

			rec := postReport(r)

			if rec.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d: %s", rec.Code, tt.wantStatus, rec.Body.String())
			}
			if tt.wantCode == "" {
				return
			}
			var body httputil.ErrorResponse
			if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
				t.Fatalf("decode %q: %v", rec.Body.String(), err)
			}
			if body.Code != tt.wantCode || body.Detail == "" {
				t.Fatalf("body = %+v, want code %q with a detail", body, tt.wantCode)
			}
		})
	}
}

func postReport(r http.Handler) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/feedback/reports",
		strings.NewReader(`{"kind":"bug","message":"three tracks went grey"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}

func TestWireFeedback_WarnsWhenEnabledButUnconfigured(t *testing.T) {
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })
	a := &App{cfg: &config.Config{FeedbackEnabled: true}}

	if a.wireFeedback() != nil {
		t.Fatal("expected no handler without credentials")
	}
	out := buf.String()
	if !strings.Contains(out, "level=WARN") || !strings.Contains(out, "GITHUB_ISSUE_TOKEN") {
		t.Fatalf("log = %q, want a WARN naming the variables", out)
	}
}
