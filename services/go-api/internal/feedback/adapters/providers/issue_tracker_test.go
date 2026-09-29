package providers

import (
	"altune/go-api/internal/feedback/domain"
	"altune/go-api/internal/shared"
	"altune/go-api/internal/shared/httputil"
	"altune/go-api/internal/shared/logging"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/google/uuid"
)

func newTestTracker(baseURL string) *GiteaIssueTracker {
	return NewGiteaIssueTracker(baseURL, "o/r", "tok")
}

const repoLabelsJSON = `[{"id":19,"name":"bug"},{"id":23,"name":"enhancement"},{"id":26,"name":"from-app"}]`

func serveLabels(w http.ResponseWriter, r *http.Request) bool {
	if r.Method != http.MethodGet || r.URL.Path != "/api/v1/repos/o/r/labels" {
		return false
	}
	_, _ = w.Write([]byte(repoLabelsJSON))
	return true
}

func testReport(t *testing.T, kind domain.Kind, message string, diag domain.Diagnostics) *domain.Report {
	t.Helper()
	report, err := domain.NewReport(shared.NewUserId(uuid.New()), kind, message, diag)
	if err != nil {
		t.Fatalf("NewReport: %v", err)
	}
	return report
}

type capturedRequest struct {
	body createIssueRequest
	path string
	auth string
}

func newFakeGitea(t *testing.T, status int, body string) (*GiteaIssueTracker, *capturedRequest) {
	t.Helper()
	got := &capturedRequest{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if serveLabels(w, r) {
			return
		}
		got.path = r.URL.Path
		got.auth = r.Header.Get("Authorization")
		_ = json.NewDecoder(r.Body).Decode(&got.body)
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(server.Close)
	return newTestTracker(server.URL), got
}

func TestLabelFor_MapsKindsToLabelVocabulary(t *testing.T) {
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
	tracker, got := newFakeGitea(t, http.StatusCreated, `{"number":7,"html_url":"https://git.example/o/r/issues/7"}`)
	report := testReport(t, domain.KindIdea, "let me sort albums by year", domain.Diagnostics{
		AppVersion: "1.4.0", Platform: "ios", OSVersion: "18.2", Screen: "settings",
	})

	ref, err := tracker.Create(context.Background(), report)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if ref.Number != 7 || ref.URL != "https://git.example/o/r/issues/7" {
		t.Fatalf("ref = %+v, want issue 7", ref)
	}
	if got.path != "/api/v1/repos/o/r/issues" {
		t.Fatalf("path = %q", got.path)
	}
	if got.auth != "token tok" {
		t.Fatalf("auth = %q", got.auth)
	}
	if got.body.Title != "[idea] let me sort albums by year" {
		t.Fatalf("title = %q", got.body.Title)
	}
	if !slices.Equal(got.body.Labels, []int64{23, 26}) {
		t.Fatalf("labels = %v", got.body.Labels)
	}
	if !strings.Contains(got.body.Body, "let me sort albums by year") {
		t.Fatalf("body missing the message: %q", got.body.Body)
	}
	if !strings.Contains(got.body.Body, "| Platform | `ios` `18.2` |") {
		t.Fatalf("body missing the diagnostics row: %q", got.body.Body)
	}
}

func TestCreate_ThreadsCorrelationIDFromContext(t *testing.T) {
	tracker, got := newFakeGitea(t, http.StatusCreated, `{"number":1,"html_url":"u"}`)
	report := testReport(t, domain.KindBug, "the player stops between tracks", domain.Diagnostics{})
	ctx := logging.WithCorrelationID(context.Background(), "corr-req42")

	if _, err := tracker.Create(ctx, report); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if !strings.Contains(got.body.Body, "| Correlation ID | corr-req42 |") {
		t.Fatalf("issue body did not carry the correlation ID from context: %q", got.body.Body)
	}
}

func TestCreate_AttributesTheReportToItsReporter(t *testing.T) {
	tracker, got := newFakeGitea(t, http.StatusCreated, `{"number":1,"html_url":"u"}`)
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
	tracker, got := newFakeGitea(t, http.StatusCreated, `{"number":1,"html_url":"u"}`)
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

func TestCreate_TitleCannotMentionAccounts(t *testing.T) {
	tracker, got := newFakeGitea(t, http.StatusCreated, `{"number":1,"html_url":"u"}`)
	report := testReport(t, domain.KindBug, "@octocat @acme/security-team \u202Eplayback stops\nmore", domain.Diagnostics{})
	if _, err := tracker.Create(context.Background(), report); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if want := "[bug] \uFF20octocat \uFF20acme/security-team playback stops"; got.body.Title != want {
		t.Fatalf("title = %q, want %q", got.body.Title, want)
	}
}

func TestCreate_FailsOnNonCreatedStatus(t *testing.T) {
	tracker, _ := newFakeGitea(t, http.StatusUnauthorized, `{"message":"Bad credentials"}`)
	report := testReport(t, domain.KindBug, "the player stops between tracks", domain.Diagnostics{})
	_, err := tracker.Create(context.Background(), report)
	if err == nil || !strings.Contains(err.Error(), "401") {
		t.Fatalf("err = %v, want a 401 failure", err)
	}
}

func newFakeGiteaWithHeaders(t *testing.T, status int, headers map[string]string) *GiteaIssueTracker {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if serveLabels(w, r) {
			return
		}
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
			tracker := newFakeGiteaWithHeaders(t, tc.status, tc.headers)
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

func TestCreate_ClassifiesNetworkFailureAsUnreachable(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {}))
	baseURL := server.URL
	server.Close()
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

func TestCreate_LogsConfirmed201WithUndecodableBodyDistinctly(t *testing.T) {
	logs := captureLogs(t)
	malformed := `{"number":7,`
	tracker, _ := newFakeGitea(t, http.StatusCreated, malformed)
	report := testReport(t, domain.KindBug, "the player stops between tracks", domain.Diagnostics{})

	if _, err := tracker.Create(context.Background(), report); err == nil {
		t.Fatal("an undecodable 201 body must still surface as an error to the caller")
	}
	rec := logs.find("gitea.issue_confirmed_but_undecoded")
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

func TestCreate_LogsConfirmed201MissingIssueNumberDistinctly(t *testing.T) {
	logs := captureLogs(t)
	tracker, _ := newFakeGitea(t, http.StatusCreated, `{}`)
	report := testReport(t, domain.KindBug, "the player stops between tracks", domain.Diagnostics{})

	if _, err := tracker.Create(context.Background(), report); err == nil {
		t.Fatal("a 201 with no issue number must still surface as an error")
	}
	if logs.find("gitea.issue_confirmed_but_undecoded") == nil {
		t.Fatalf("confirmed 201 without a number was not logged distinctly; records=%v", logs.records)
	}
}

func TestCreate_DoesNotLogDistinctlyOnNonCreatedStatus(t *testing.T) {
	logs := captureLogs(t)
	tracker, _ := newFakeGitea(t, http.StatusUnprocessableEntity, `not json at all`)
	report := testReport(t, domain.KindBug, "the player stops between tracks", domain.Diagnostics{})

	if _, err := tracker.Create(context.Background(), report); err == nil {
		t.Fatal("expected a non-201 failure")
	}
	if rec := logs.find("gitea.issue_confirmed_but_undecoded"); rec != nil {
		t.Fatalf("a non-created status must not be logged as confirmed-but-undecoded; got %v", rec)
	}
}

func TestCreate_FailsWhenResponseCarriesNoIssueNumber(t *testing.T) {
	tracker, _ := newFakeGitea(t, http.StatusCreated, `{}`)
	report := testReport(t, domain.KindBug, "the player stops between tracks", domain.Diagnostics{})
	if _, err := tracker.Create(context.Background(), report); err == nil {
		t.Fatal("expected a missing issue number to fail")
	}
}

func TestCreate_RejectsAnOversizedCreatedBody(t *testing.T) {
	padding := strings.Repeat("x", maxIssueBody+1)
	tracker, _ := newFakeGitea(t, http.StatusCreated, `{"number":7,"html_url":"u","body":"`+padding+`"}`)
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

func TestSubmitReport_Truncated201AnswersOutcomeUnknownAndKeyedRetryDoesNotDuplicate(t *testing.T) {
	truncated := scriptedReply{status: http.StatusCreated, body: `{"number":7,`}
	tracker, hits := scriptedGitea(t, created, truncated)
	submitter := newKeyedSubmitter(tracker, noopMetrics{})

	first := submitter.submit()
	retry := submitter.submit()

	rec := httptest.NewRecorder()
	httputil.HandleServiceError(rec, httptest.NewRequest(http.MethodPost, "/feedback/reports", http.NoBody), first)
	if rec.Code != http.StatusBadGateway || codeOfErr(first) != codeOutcomeUnknown {
		t.Fatalf("truncated 201 answered %d %q, want %d %q", rec.Code, codeOfErr(first), http.StatusBadGateway, codeOutcomeUnknown)
	}
	if codeOfErr(retry) != codeOutcomeUnknown {
		t.Fatalf("keyed retry = %v, want the replayed %q", retry, codeOutcomeUnknown)
	}
	if got := hits.Load(); got != 1 {
		t.Fatalf("Gitea saw %d creates, want 1: the keyed retry must not duplicate", got)
	}
}

func TestGiteaIssueTracker_PostsBugWithBugAndFromAppLabelIDs(t *testing.T) {
	tracker, got := newFakeGitea(t, http.StatusCreated, `{"number":3,"html_url":"https://git.example/o/r/issues/3"}`)
	report := testReport(t, domain.KindBug, "the player stops between tracks", domain.Diagnostics{})

	ref, err := tracker.Create(context.Background(), report)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if ref.Number != 3 || ref.URL != "https://git.example/o/r/issues/3" {
		t.Fatalf("ref = %+v, want issue 3", ref)
	}
	if got.path != "/api/v1/repos/o/r/issues" || got.auth != "token tok" {
		t.Fatalf("path = %q, auth = %q", got.path, got.auth)
	}
	if !slices.Equal(got.body.Labels, []int64{19, 26}) {
		t.Fatalf("labels = %v, want [19 26]", got.body.Labels)
	}
}

type labelsThenIssues struct {
	labelStatuses []int
	labelHits     atomic.Int32
	posted        []createIssueRequest
}

func newFlakyLabelsGitea(t *testing.T, labelStatuses ...int) (*GiteaIssueTracker, *labelsThenIssues) {
	t.Helper()
	state := &labelsThenIssues{labelStatuses: labelStatuses}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			n := int(state.labelHits.Add(1))
			if n <= len(state.labelStatuses) && state.labelStatuses[n-1] != http.StatusOK {
				w.WriteHeader(state.labelStatuses[n-1])
				return
			}
			_, _ = w.Write([]byte(repoLabelsJSON))
			return
		}
		var body createIssueRequest
		_ = json.NewDecoder(r.Body).Decode(&body)
		state.posted = append(state.posted, body)
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"number":5,"html_url":"u"}`))
	}))
	t.Cleanup(server.Close)
	return newTestTracker(server.URL), state
}

func TestGiteaIssueTracker_LabelLookupFailureFilesWithoutLabelsAndRetriesNextReport(t *testing.T) {
	tracker, state := newFlakyLabelsGitea(t, http.StatusInternalServerError)
	report := testReport(t, domain.KindBug, "the player stops between tracks", domain.Diagnostics{})

	if _, err := tracker.Create(context.Background(), report); err != nil {
		t.Fatalf("a failed label lookup must not lose the report: %v", err)
	}
	if _, err := tracker.Create(context.Background(), report); err != nil {
		t.Fatalf("second Create: %v", err)
	}
	if len(state.posted) != 2 {
		t.Fatalf("posted %d issues, want 2", len(state.posted))
	}
	if len(state.posted[0].Labels) != 0 {
		t.Fatalf("first issue labels = %v, want none", state.posted[0].Labels)
	}
	if !slices.Equal(state.posted[1].Labels, []int64{19, 26}) {
		t.Fatalf("second issue labels = %v, want the retried lookup's [19 26]", state.posted[1].Labels)
	}
	if got := state.labelHits.Load(); got != 2 {
		t.Fatalf("label lookups = %d, want 2 (failure retried once)", got)
	}
}

func TestGiteaIssueTracker_CachesLabelsAfterFirstSuccess(t *testing.T) {
	tracker, state := newFlakyLabelsGitea(t)
	report := testReport(t, domain.KindBug, "the player stops between tracks", domain.Diagnostics{})
	for range 3 {
		if _, err := tracker.Create(context.Background(), report); err != nil {
			t.Fatalf("Create: %v", err)
		}
	}
	if got := state.labelHits.Load(); got != 1 {
		t.Fatalf("label lookups = %d, want 1 cached", got)
	}
}

func TestGiteaIssueTracker_MissingUxLabelFilesWithFromAppOnly(t *testing.T) {
	tracker, got := newFakeGitea(t, http.StatusCreated, `{"number":4,"html_url":"u"}`)
	report := testReport(t, domain.KindConfusing, "I could not find the queue", domain.Diagnostics{})

	if _, err := tracker.Create(context.Background(), report); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if !slices.Equal(got.body.Labels, []int64{26}) {
		t.Fatalf("labels = %v, want only from-app [26]", got.body.Labels)
	}
}
