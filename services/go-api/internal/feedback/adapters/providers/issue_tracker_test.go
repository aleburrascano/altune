package providers

import (
	"altune/go-api/internal/feedback/domain"
	"altune/go-api/internal/shared"
	"altune/go-api/internal/shared/httputil"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/google/uuid"
)

// newTestTracker builds a tracker via the production constructor, then points it
// at a test server by setting the unexported baseURL directly (white-box). It
// trims a trailing slash so it composes URLs the same way production does.
func newTestTracker(baseURL string) *GitHubIssueTracker {
	tracker := NewGitHubIssueTracker("o/r", "tok")
	tracker.baseURL = strings.TrimSuffix(baseURL, "/")
	return tracker
}

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
	return newTestTracker(server.URL), got
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
	if !strings.Contains(got.body.Body, "| Platform | `ios` `18.2` |") {
		t.Fatalf("body missing the diagnostics row: %q", got.body.Body)
	}
}

// TestCreate_ThreadsCorrelationIDFromContext reproduces #592: the request's
// correlation ID lives on the context but never reached the issue body. Now Create
// reads it from the context and renders it, so the issue links to the server logs.
func TestCreate_ThreadsCorrelationIDFromContext(t *testing.T) {
	tracker, got := newFakeGitHub(t, http.StatusCreated, `{"number":1,"html_url":"u"}`)
	report := testReport(t, domain.KindBug, "the player stops between tracks", domain.Diagnostics{})
	ctx := httputil.WithCorrelationID(context.Background(), "corr-req42")

	if _, err := tracker.Create(ctx, report); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if !strings.Contains(got.body.Body, "| Correlation ID | corr-req42 |") {
		t.Fatalf("issue body did not carry the correlation ID from context: %q", got.body.Body)
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
	if !strings.Contains(got.body.Body, "| Screen | `settings \\| fake \\| row` |") {
		t.Fatalf("body did not escape the pipes: %q", got.body.Body)
	}
}

// TestCreate_TitleCannotMentionAccounts reproduces #1107: the title reached
// GitHub as the raw first line of the message, and GitHub matches @mentions in
// titles, so any reporter could make the app's shared token notify an arbitrary
// account or team.
func TestCreate_TitleCannotMentionAccounts(t *testing.T) {
	tracker, got := newFakeGitHub(t, http.StatusCreated, `{"number":1,"html_url":"u"}`)
	report := testReport(t, domain.KindBug, "@octocat @acme/security-team \u202Eplayback stops\nmore", domain.Diagnostics{})
	if _, err := tracker.Create(context.Background(), report); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if want := "[bug] \uFF20octocat \uFF20acme/security-team playback stops"; got.body.Title != want {
		t.Fatalf("title = %q, want %q", got.body.Title, want)
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
	return newTestTracker(server.URL)
}

// TestCreate_ClassifiesFailuresIntoDistinctStatuses reproduces #588: before the
// fix every GitHub failure wrapped identically and collapsed to a generic 500.
// Now each surfaces its own HTTPStatus/ErrorCode via httputil's error interfaces.
func TestCreate_ClassifiesFailuresIntoDistinctStatuses(t *testing.T) {
	cases := []struct {
		name       string
		status     int
		headers    map[string]string
		wantStatus int
		wantCode   string
	}{
		{"unauthorized", http.StatusUnauthorized, nil, http.StatusBadGateway, codeUnauthorized},
		{"forbidden", http.StatusForbidden, nil, http.StatusBadGateway, codeUnauthorized},
		{"rate limited 429", http.StatusTooManyRequests, map[string]string{"Retry-After": "60"}, http.StatusServiceUnavailable, codeRateLimited},
		{"rate limited 403", http.StatusForbidden, map[string]string{"X-RateLimit-Remaining": "0", "Retry-After": "30"}, http.StatusServiceUnavailable, codeRateLimited},
		{"validation", http.StatusUnprocessableEntity, nil, http.StatusBadGateway, codeRejected},
		{"wrong repo", http.StatusNotFound, nil, http.StatusBadGateway, codeNotFound},
		{"issues disabled", http.StatusGone, nil, http.StatusBadGateway, codeNotFound},
		{"server error", http.StatusInternalServerError, nil, http.StatusBadGateway, codeUnavailable},
		{"bad gateway", http.StatusBadGateway, nil, http.StatusBadGateway, codeUnavailable},
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
		})
	}
}

func codeOf(c httputil.ErrorCoder) string {
	if c == nil {
		return ""
	}
	return c.ErrorCode()
}

// TestCreate_ClassifiesNetworkFailureAsUnreachable covers the transient transport
// case: an unanswered request surfaces as a gateway timeout, not a generic 500.
func TestCreate_ClassifiesNetworkFailureAsUnreachable(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {}))
	baseURL := server.URL
	server.Close() // nothing is listening now, so the dial fails
	tracker := newTestTracker(baseURL)
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

// logCapture records slog records so tests can assert on what a failure path
// logged, mirroring the service package's capturing handler.
type logCapture struct {
	records []map[string]string
}

func (h *logCapture) Enabled(context.Context, slog.Level) bool { return true }

func (h *logCapture) Handle(_ context.Context, r slog.Record) error {
	attrs := map[string]string{"msg": r.Message}
	r.Attrs(func(a slog.Attr) bool {
		attrs[a.Key] = a.Value.String()
		return true
	})
	h.records = append(h.records, attrs)
	return nil
}

func (h *logCapture) WithAttrs([]slog.Attr) slog.Handler { return h }
func (h *logCapture) WithGroup(string) slog.Handler      { return h }

func captureLogs(t *testing.T) *logCapture {
	t.Helper()
	h := &logCapture{}
	prev := slog.Default()
	slog.SetDefault(slog.New(h))
	t.Cleanup(func() { slog.SetDefault(prev) })
	return h
}

func (h *logCapture) find(msg string) map[string]string {
	for _, rec := range h.records {
		if rec["msg"] == msg {
			return rec
		}
	}
	return nil
}

// TestCreate_LogsConfirmed201WithUndecodableBodyDistinctly reproduces #589: a 201
// means GitHub created the issue, so a body we cannot decode is a lost
// confirmation, not a non-created issue. Before the fix this was indistinguishable
// from a true failure in the logs, so a retry risked a real duplicate. Now the
// confirmed-but-undecoded case is logged distinctly with the status and raw body,
// while the caller still sees an error.
func TestCreate_LogsConfirmed201WithUndecodableBodyDistinctly(t *testing.T) {
	logs := captureLogs(t)
	malformed := `{"number":7,` // truncated JSON: a 201 body we cannot decode
	tracker, _ := newFakeGitHub(t, http.StatusCreated, malformed)
	report := testReport(t, domain.KindBug, "the player stops between tracks", domain.Diagnostics{})

	if _, err := tracker.Create(context.Background(), report); err == nil {
		t.Fatal("an undecodable 201 body must still surface as an error to the caller")
	}
	rec := logs.find("github.issue_confirmed_but_undecoded")
	if rec == nil {
		t.Fatalf("confirmed 201 with an undecodable body was not logged distinctly; records=%v", logs.records)
	}
	if rec["status"] != "201" {
		t.Fatalf("distinct log must carry the confirmed status, got %q", rec["status"])
	}
	if !strings.Contains(rec["raw_body"], malformed) {
		t.Fatalf("distinct log must carry the raw body, got %q", rec["raw_body"])
	}
}

// TestCreate_LogsConfirmed201MissingIssueNumberDistinctly is the regression twin:
// a well-formed 201 body that carries no issue number is still a confirmed create
// we could not read, so it is logged distinctly too.
func TestCreate_LogsConfirmed201MissingIssueNumberDistinctly(t *testing.T) {
	logs := captureLogs(t)
	tracker, _ := newFakeGitHub(t, http.StatusCreated, `{}`)
	report := testReport(t, domain.KindBug, "the player stops between tracks", domain.Diagnostics{})

	if _, err := tracker.Create(context.Background(), report); err == nil {
		t.Fatal("a 201 with no issue number must still surface as an error")
	}
	if logs.find("github.issue_confirmed_but_undecoded") == nil {
		t.Fatalf("confirmed 201 without a number was not logged distinctly; records=%v", logs.records)
	}
}

// TestCreate_DoesNotLogDistinctlyOnNonCreatedStatus guards the boundary: a true
// creation failure (non-201) must NOT be logged as a confirmed-but-undecoded
// case, or the distinction the fix draws would be meaningless.
func TestCreate_DoesNotLogDistinctlyOnNonCreatedStatus(t *testing.T) {
	logs := captureLogs(t)
	tracker, _ := newFakeGitHub(t, http.StatusUnprocessableEntity, `not json at all`)
	report := testReport(t, domain.KindBug, "the player stops between tracks", domain.Diagnostics{})

	if _, err := tracker.Create(context.Background(), report); err == nil {
		t.Fatal("expected a non-201 failure")
	}
	if rec := logs.find("github.issue_confirmed_but_undecoded"); rec != nil {
		t.Fatalf("a non-created status must not be logged as confirmed-but-undecoded; got %v", rec)
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

	tracker := newTestTracker(server.URL)
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
