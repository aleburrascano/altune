package github

import (
	"altune/go-api/internal/feedback/domain"
	"altune/go-api/internal/shared"
	"altune/go-api/internal/shared/httputil"
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/google/uuid"
)

func testReport(t *testing.T, kind domain.Kind, message string, diag domain.Diagnostics) *domain.Report {
	t.Helper()
	report, err := domain.NewReport(shared.NewUserId(uuid.New()), kind, message, diag)
	if err != nil {
		t.Fatalf("NewReport: %v", err)
	}
	return report
}

// capturedRequest records what the fake GitHub server saw on the create call so
// tests can assert on the outbound request.
type capturedRequest struct {
	body    createIssueRequest
	path    string
	auth    string
	version string
}

// newFakeGitHub stands up an httptest server that always decodes the create
// request body (so no test silently skips it) and replies with the given status
// and body, then returns a tracker already pointed at it. The server is closed
// on test cleanup.
func newFakeGitHub(t *testing.T, status int, body string) (*GitHubIssueTracker, *capturedRequest) {
	t.Helper()
	got := &capturedRequest{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got.path = r.URL.Path
		got.auth = r.Header.Get("Authorization")
		got.version = r.Header.Get("X-GitHub-Api-Version")
		_ = json.NewDecoder(r.Body).Decode(&got.body)
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(server.Close)
	return NewGitHubIssueTracker("o/r", "tok").WithBaseURL(server.URL), got
}

func TestLabelFor_MapsKindsToGitHubVocabulary(t *testing.T) {
	cases := map[domain.Kind]string{
		domain.KindBug:       "bug",
		domain.KindIdea:      "enhancement",
		domain.KindConfusing: "ux",
	}
	for kind, want := range cases {
		if got := labelFor(kind); got != want {
			t.Fatalf("labelFor(%v) = %q, want %q", kind, got, want)
		}
	}
	if got := labelFor(domain.Kind(99)); got != "" {
		t.Fatalf("labelFor(undefined) = %q, want empty", got)
	}
}

func TestCreate_PostsTitleBodyAndLabels(t *testing.T) {
	tracker, got := newFakeGitHub(t, http.StatusCreated, `{"number":7,"html_url":"https://github.com/o/r/issues/7"}`)
	report := testReport(t, domain.KindIdea, "let me sort albums by year", domain.Diagnostics{
		AppVersion: "1.4.0", Platform: "ios", OSVersion: "18.2", Screen: "settings",
	})

	ref, err := tracker.Create(context.Background(), report)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if ref.Number != 7 || ref.URL != "https://github.com/o/r/issues/7" {
		t.Fatalf("ref = %+v, want issue 7", ref)
	}
	if got.path != "/repos/o/r/issues" {
		t.Fatalf("path = %q", got.path)
	}
	if got.auth != "Bearer tok" || got.version != apiVersion {
		t.Fatalf("auth = %q, version = %q", got.auth, got.version)
	}
	if got.body.Title != "[idea] let me sort albums by year" {
		t.Fatalf("title = %q", got.body.Title)
	}
	if strings.Join(got.body.Labels, ",") != "enhancement,from-app" {
		t.Fatalf("labels = %v", got.body.Labels)
	}
	if !strings.Contains(got.body.Body, "let me sort albums by year") {
		t.Fatalf("body missing the message: %q", got.body.Body)
	}
	if !strings.Contains(got.body.Body, "| Platform | ios 18.2 |") {
		t.Fatalf("body missing the diagnostics row: %q", got.body.Body)
	}
}

func TestCreate_AttributesTheReportToItsReporter(t *testing.T) {
	tracker, got := newFakeGitHub(t, http.StatusCreated, `{"number":1,"html_url":"u"}`)
	report := testReport(t, domain.KindBug, "the player stops between tracks", domain.Diagnostics{})
	if _, err := tracker.Create(context.Background(), report); err != nil {
		t.Fatalf("Create: %v", err)
	}

	if !strings.Contains(got.body.Body, report.Reporter.String()) {
		t.Fatalf("issue body did not name the reporter: %q", got.body.Body)
	}
	if strings.Contains(got.body.Title, report.Reporter.String()) {
		t.Fatalf("the reporter belongs in the body, not the title: %q", got.body.Title)
	}
}

func TestCreate_EscapesPipesInDiagnostics(t *testing.T) {
	tracker, got := newFakeGitHub(t, http.StatusCreated, `{"number":1,"html_url":"u"}`)
	report := testReport(t, domain.KindBug, "the player stops between tracks", domain.Diagnostics{
		Screen: "settings | fake | row",
	})
	if _, err := tracker.Create(context.Background(), report); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if !strings.Contains(got.body.Body, `settings \| fake \| row`) {
		t.Fatalf("body did not escape the pipes: %q", got.body.Body)
	}
}

func TestCreate_FailsOnNonCreatedStatus(t *testing.T) {
	tracker, _ := newFakeGitHub(t, http.StatusUnauthorized, `{"message":"Bad credentials"}`)
	report := testReport(t, domain.KindBug, "the player stops between tracks", domain.Diagnostics{})
	_, err := tracker.Create(context.Background(), report)
	if err == nil || !strings.Contains(err.Error(), "401") {
		t.Fatalf("err = %v, want a 401 failure", err)
	}
}

// newFakeGitHubWithHeaders is newFakeGitHub with response headers set before the
// status is written, so tests can drive GitHub's rate-limit and Retry-After signals.
func newFakeGitHubWithHeaders(t *testing.T, status int, headers map[string]string) *GitHubIssueTracker {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&createIssueRequest{})
		for k, v := range headers {
			w.Header().Set(k, v)
		}
		w.WriteHeader(status)
		_, _ = w.Write([]byte(`{"message":"boom"}`))
	}))
	t.Cleanup(server.Close)
	return NewGitHubIssueTracker("o/r", "tok").WithBaseURL(server.URL)
}

// TestCreate_ClassifiesFailuresIntoDistinctStatuses reproduces #588: before the
// fix every GitHub failure wrapped identically and collapsed to a generic 500.
// Now each surfaces its own HTTPStatus/ErrorCode via httputil's error interfaces.
func TestCreate_ClassifiesFailuresIntoDistinctStatuses(t *testing.T) {
	cases := []struct {
		name           string
		status         int
		headers        map[string]string
		wantStatus     int
		wantCode       string
		wantRetryAfter string
	}{
		{"unauthorized", http.StatusUnauthorized, nil, http.StatusBadGateway, codeUnauthorized, ""},
		{"forbidden", http.StatusForbidden, nil, http.StatusBadGateway, codeUnauthorized, ""},
		{"rate limited 429", http.StatusTooManyRequests, map[string]string{"Retry-After": "60"}, http.StatusServiceUnavailable, codeRateLimited, "60"},
		{"rate limited 403", http.StatusForbidden, map[string]string{"X-RateLimit-Remaining": "0", "Retry-After": "30"}, http.StatusServiceUnavailable, codeRateLimited, "30"},
		{"validation", http.StatusUnprocessableEntity, nil, http.StatusBadGateway, codeRejected, ""},
		{"server error", http.StatusInternalServerError, nil, http.StatusBadGateway, codeUnavailable, ""},
		{"bad gateway", http.StatusBadGateway, nil, http.StatusBadGateway, codeUnavailable, ""},
	}
	report := testReport(t, domain.KindBug, "the player stops between tracks", domain.Diagnostics{})
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tracker := newFakeGitHubWithHeaders(t, tc.status, tc.headers)
			_, err := tracker.Create(context.Background(), report)
			if err == nil {
				t.Fatal("expected a failure")
			}
			var se httputil.StatusError
			if !errors.As(err, &se) {
				t.Fatalf("err %v does not implement StatusError; it would fall through to 500", err)
			}
			if se.HTTPStatus() != tc.wantStatus {
				t.Fatalf("HTTPStatus() = %d, want %d", se.HTTPStatus(), tc.wantStatus)
			}
			var coder httputil.ErrorCoder
			if !errors.As(err, &coder) || coder.ErrorCode() != tc.wantCode {
				t.Fatalf("ErrorCode() = %q, want %q", codeOf(coder), tc.wantCode)
			}
			var te *trackerError
			if !errors.As(err, &te) || te.RetryAfter() != tc.wantRetryAfter {
				t.Fatalf("RetryAfter() = %q, want %q", retryAfterOf(err), tc.wantRetryAfter)
			}
		})
	}
}

func codeOf(c httputil.ErrorCoder) string {
	if c == nil {
		return ""
	}
	return c.ErrorCode()
}

func retryAfterOf(err error) string {
	var te *trackerError
	if errors.As(err, &te) {
		return te.RetryAfter()
	}
	return ""
}

// TestCreate_ClassifiesNetworkFailureAsUnreachable covers the transient transport
// case: an unanswered request surfaces as a gateway timeout, not a generic 500.
func TestCreate_ClassifiesNetworkFailureAsUnreachable(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {}))
	baseURL := server.URL
	server.Close() // nothing is listening now, so the dial fails
	tracker := NewGitHubIssueTracker("o/r", "tok").WithBaseURL(baseURL)
	report := testReport(t, domain.KindBug, "the player stops between tracks", domain.Diagnostics{})

	_, err := tracker.Create(context.Background(), report)
	var se httputil.StatusError
	if !errors.As(err, &se) || se.HTTPStatus() != http.StatusGatewayTimeout {
		t.Fatalf("network failure should surface as 504, got %v", err)
	}
	var coder httputil.ErrorCoder
	if !errors.As(err, &coder) || coder.ErrorCode() != codeUnreachable {
		t.Fatalf("ErrorCode() = %q, want %q", codeOf(coder), codeUnreachable)
	}
}

func TestCreate_FailsWhenResponseCarriesNoIssueNumber(t *testing.T) {
	tracker, _ := newFakeGitHub(t, http.StatusCreated, `{}`)
	report := testReport(t, domain.KindBug, "the player stops between tracks", domain.Diagnostics{})
	if _, err := tracker.Create(context.Background(), report); err == nil {
		t.Fatal("expected a missing issue number to fail")
	}
}

func TestCreate_RejectsAnOversizedCreatedBody(t *testing.T) {
	padding := strings.Repeat("x", maxIssueBody+1)
	tracker, _ := newFakeGitHub(t, http.StatusCreated, `{"number":7,"html_url":"u","body":"`+padding+`"}`)
	report := testReport(t, domain.KindBug, "the player stops between tracks", domain.Diagnostics{})
	if _, err := tracker.Create(context.Background(), report); err == nil {
		t.Fatal("expected a created body past the read cap to fail, not be decoded unbounded")
	}
}

func TestCreate_DrainsErrorBodySoTheConnectionIsReused(t *testing.T) {
	var conns atomic.Int32
	errorBody := strings.Repeat("e", 4*maxErrorBody)
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte(errorBody))
	}))
	server.Config.ConnState = func(_ net.Conn, state http.ConnState) {
		if state == http.StateNew {
			conns.Add(1)
		}
	}
	server.Start()
	defer server.Close()

	tracker := NewGitHubIssueTracker("o/r", "tok").WithBaseURL(server.URL)
	report := testReport(t, domain.KindBug, "the player stops between tracks", domain.Diagnostics{})
	for range 3 {
		if _, err := tracker.Create(context.Background(), report); err == nil {
			t.Fatal("expected a 502 failure")
		}
	}
	if got := conns.Load(); got != 1 {
		t.Fatalf("opened %d connections for 3 failed requests, want 1 reused", got)
	}
}
