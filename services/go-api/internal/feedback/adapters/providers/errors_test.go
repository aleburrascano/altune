package providers

import (
	"altune/go-api/internal/feedback/ports"
	"altune/go-api/internal/feedback/service"
	"altune/go-api/internal/shared"
	"altune/go-api/internal/shared/httputil"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/google/uuid"
)

const upstreamMarker = "UPSTREAM-MARKER-7f3a"

type silentMetrics struct{}

func (silentMetrics) TrackerCreateFailed(string) {}
func (silentMetrics) SubmissionRejected(string)  {}
func (silentMetrics) SubmissionCreated()         {}

func submitThrough(t *testing.T, tracker *GitHubIssueTracker) (status int, detail, code string) {
	t.Helper()
	svc := service.NewSubmitReportService(tracker, silentMetrics{})
	_, err := svc.Execute(context.Background(), shared.NewUserId(uuid.New()), service.SubmitReportInput{
		Kind:    "bug",
		Message: "three downloaded tracks went grey again",
	})
	if err == nil {
		t.Fatal("Execute succeeded against a failing tracker")
	}
	rec := httptest.NewRecorder()
	httputil.HandleServiceError(rec, httptest.NewRequest(http.MethodPost, "/feedback/reports", http.NoBody), err)
	var resp httputil.ErrorResponse
	if decodeErr := json.NewDecoder(rec.Body).Decode(&resp); decodeErr != nil {
		t.Fatalf("decode response: %v", decodeErr)
	}
	return rec.Code, resp.Detail, resp.Code
}

func captureLogLines(t *testing.T) *bytes.Buffer {
	t.Helper()
	buf := &bytes.Buffer{}
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(buf, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })
	return buf
}

func TestSubmitReport_ResponseDetailHidesUpstreamGitHubBody(t *testing.T) {
	cases := []struct {
		status   int
		wantCode string
	}{
		{http.StatusUnauthorized, codeUnauthorized},
		{http.StatusForbidden, codeUnauthorized},
		{http.StatusUnprocessableEntity, codeRejected},
	}
	for _, tc := range cases {
		t.Run(http.StatusText(tc.status), func(t *testing.T) {
			logs := captureLogLines(t)
			tracker, _ := newFakeGitHub(t, tc.status, `{"message":"`+upstreamMarker+`"}`)

			status, detail, code := submitThrough(t, tracker)

			if status != http.StatusBadGateway || code != tc.wantCode {
				t.Fatalf("got status %d code %q, want %d %q", status, code, http.StatusBadGateway, tc.wantCode)
			}
			statusText := "status " + strconv.Itoa(tc.status)
			if strings.Contains(detail, upstreamMarker) || strings.Contains(detail, statusText) {
				t.Fatalf("detail %q leaks the upstream response", detail)
			}
			if !createFailedLogCarries(logs, upstreamMarker, statusText) {
				t.Fatalf("feedback.create_failed log lost the upstream error:\n%s", logs)
			}
		})
	}
}

func TestSubmitReport_ResponseDetailHidesNetworkFailure(t *testing.T) {
	logs := captureLogLines(t)
	dead := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	deadURL := dead.URL
	dead.Close()
	host, _ := url.Parse(deadURL)

	status, detail, code := submitThrough(t, newTestTracker(deadURL))

	if status != http.StatusGatewayTimeout || code != codeUnreachable {
		t.Fatalf("got status %d code %q, want %d %q", status, code, http.StatusGatewayTimeout, codeUnreachable)
	}
	for _, leak := range []string{"/repos/o/r", "dial tcp", host.Host} {
		if strings.Contains(detail, leak) {
			t.Fatalf("detail %q leaks %q", detail, leak)
		}
	}
	if !createFailedLogCarries(logs, "dial tcp", "/repos/o/r") {
		t.Fatalf("feedback.create_failed log lost the network error:\n%s", logs)
	}
}

func createFailedLogCarries(logs *bytes.Buffer, fragments ...string) bool {
	for _, line := range strings.Split(logs.String(), "\n") {
		var rec map[string]any
		if json.Unmarshal([]byte(line), &rec) != nil || rec["msg"] != "feedback.create_failed" {
			continue
		}
		logged, _ := rec["error"].(string)
		return containsAll(logged, fragments)
	}
	return false
}

func containsAll(text string, fragments []string) bool {
	for _, fragment := range fragments {
		if !strings.Contains(text, fragment) {
			return false
		}
	}
	return true
}

// causeRecordingMetrics records the cause of every counted tracker failure.
type causeRecordingMetrics struct{ causes []string }

func (m *causeRecordingMetrics) SubmissionRejected(string) {}
func (m *causeRecordingMetrics) SubmissionCreated()        {}

func (m *causeRecordingMetrics) TrackerCreateFailed(cause string) {
	m.causes = append(m.causes, cause)
}

// TestSubmitReport_SplitsTrackerFailureMetricByCause reproduces #1111 end to end
// against a fake GitHub: a transient failure (5xx, unreachable host) and a
// permanent one (dead token, wrong repo) used to bump one undifferentiated
// counter, and a 404 wrong-repo classified identically to a 5xx outage. Each now
// lands under its own cause, and the permanent causes never collide with the
// transient ones.
func TestSubmitReport_SplitsTrackerFailureMetricByCause(t *testing.T) {
	unreachable := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	unreachableURL := unreachable.URL
	unreachable.Close()

	cases := []struct {
		name      string
		tracker   func(t *testing.T) *GitHubIssueTracker
		wantCause string
		permanent bool
	}{
		{"github 5xx", func(t *testing.T) *GitHubIssueTracker {
			tr, _ := newFakeGitHub(t, http.StatusServiceUnavailable, `{"message":"boom"}`)
			return tr
		}, codeUnavailable, false},
		{"network error", func(*testing.T) *GitHubIssueTracker { return newTestTracker(unreachableURL) }, codeUnreachable, false},
		{"dead token 401", func(t *testing.T) *GitHubIssueTracker {
			tr, _ := newFakeGitHub(t, http.StatusUnauthorized, `{"message":"Bad credentials"}`)
			return tr
		}, codeUnauthorized, true},
		{"wrong repo 404", func(t *testing.T) *GitHubIssueTracker {
			tr, _ := newFakeGitHub(t, http.StatusNotFound, `{"message":"Not Found"}`)
			return tr
		}, codeNotFound, true},
	}
	transient := map[string]bool{}
	permanent := map[string]bool{}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			metrics := &causeRecordingMetrics{}
			svc := service.NewSubmitReportService(tc.tracker(t), metrics)
			_, err := svc.Execute(context.Background(), shared.NewUserId(uuid.New()), service.SubmitReportInput{
				Kind:    "bug",
				Message: "three downloaded tracks went grey again",
			})
			if err == nil {
				t.Fatal("expected the tracker failure to surface")
			}
			if len(metrics.causes) != 1 || metrics.causes[0] != tc.wantCause {
				t.Fatalf("counted causes = %v, want [%s]", metrics.causes, tc.wantCause)
			}
			if tc.permanent {
				permanent[tc.wantCause] = true
			} else {
				transient[tc.wantCause] = true
			}
		})
	}
	for cause := range permanent {
		if transient[cause] {
			t.Fatalf("cause %q is shared by a permanent and a transient failure", cause)
		}
	}
}

func retryAfterFor(resp *http.Response) (int, string) {
	rec := httptest.NewRecorder()
	httputil.HandleServiceError(rec, httptest.NewRequest(http.MethodPost, "/", nil), statusError(resp, time.Now()))
	return rec.Code, rec.Header().Get("Retry-After")
}

func TestTrackerError_ForwardsBoundedRetryAfter(t *testing.T) {
	cases := []struct {
		name   string
		header string
		want   string
	}{
		{name: "github hint is forwarded", header: "30", want: "30"},
		{name: "an absurd hint is bounded", header: "86400", want: "3600"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resp := &http.Response{
				StatusCode: http.StatusTooManyRequests,
				Header:     http.Header{"Retry-After": {tc.header}},
				Body:       http.NoBody,
			}
			code, got := retryAfterFor(resp)
			if code != http.StatusServiceUnavailable || got != tc.want {
				t.Fatalf("status %d Retry-After %q, want 503 and %q", code, got, tc.want)
			}
		})
	}
}

func TestTrackerError_NoHintSendsNoRetryAfter(t *testing.T) {
	resp := &http.Response{StatusCode: http.StatusBadGateway, Header: http.Header{}, Body: http.NoBody}
	if _, got := retryAfterFor(resp); got != "" {
		t.Fatalf("Retry-After = %q, want none", got)
	}
}

func recordThenStallGitHub(t *testing.T) (*GitHubIssueTracker, *atomic.Int32) {
	t.Helper()
	var recorded atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		recorded.Add(1)
		<-r.Context().Done()
	}))
	t.Cleanup(server.Close)
	tracker := newTestTracker(server.URL)
	tracker.client.Timeout = 50 * time.Millisecond
	return tracker, &recorded
}

type keyedSubmitter struct {
	svc   *service.SubmitReportService
	user  shared.UserId
	input service.SubmitReportInput
}

func newKeyedSubmitter(tracker *GitHubIssueTracker, metrics ports.FeedbackMetrics) keyedSubmitter {
	clock := &steppedClock{t: time.Date(2026, 9, 24, 12, 0, 0, 0, time.UTC)}
	key := "retry-" + uuid.NewString()
	return keyedSubmitter{
		svc:  service.NewSubmitReportServiceWithLimits(tracker, metrics, refundLimits, clock.now),
		user: shared.NewUserId(uuid.New()),
		input: service.SubmitReportInput{
			Kind:           "bug",
			Message:        "three downloaded tracks went grey again",
			IdempotencyKey: &key,
		},
	}
}

func (k keyedSubmitter) submit() error {
	_, err := k.svc.Execute(context.Background(), k.user, k.input)
	return err
}

func vouchesUncreated(err error) bool {
	var uncreated ports.TrackerUncreated
	return errors.As(err, &uncreated) && uncreated.Uncreated()
}

func codeOfErr(err error) string {
	var coder httputil.ErrorCoder
	if !errors.As(err, &coder) {
		return ""
	}
	return coder.ErrorCode()
}

func TestSubmitReport_TimeoutAfterGitHubRecordedTheIssueKeepsQuotaAndKey(t *testing.T) {
	tracker, recorded := recordThenStallGitHub(t)
	metrics := &causeRecordingMetrics{}
	submitter := newKeyedSubmitter(tracker, metrics)

	first := submitter.submit()
	retry := submitter.submit()

	if first == nil || vouchesUncreated(first) {
		t.Fatalf("timed-out create = %v, want an error that does not vouch nothing was created", first)
	}
	if codeOfErr(first) != codeOutcomeUnknown || codeOfErr(retry) != codeOutcomeUnknown {
		t.Fatalf("codes = %q then %q, want %q both times", codeOfErr(first), codeOfErr(retry), codeOutcomeUnknown)
	}
	if got := recorded.Load(); got != 1 {
		t.Fatalf("GitHub recorded %d issues, want 1: the keyed retry must not create again", got)
	}
	if len(metrics.causes) != 1 || metrics.causes[0] != codeOutcomeUnknown {
		t.Fatalf("counted causes = %v, want [%s]", metrics.causes, codeOutcomeUnknown)
	}
	if got := admittedUntilRefused(t, submitter.svc); got != refundLimits.Global-1 {
		t.Fatalf("%d reports admitted after the timeout, want %d: its slot must stay spent", got, refundLimits.Global-1)
	}
}

func TestTransportError_VouchesUncreatedOnlyForProvableNonDelivery(t *testing.T) {
	cases := []struct {
		name          string
		cause         error
		wantUncreated bool
		wantCode      string
	}{
		{"dial refused", &net.OpError{Op: "dial", Net: "tcp", Err: syscall.ECONNREFUSED}, true, codeUnreachable},
		{"dns failure", &net.DNSError{Err: "no such host", Name: "api.github.invalid", IsNotFound: true}, true, codeUnreachable},
		{"reset after write", &net.OpError{Op: "read", Net: "tcp", Err: syscall.ECONNRESET}, false, codeOutcomeUnknown},
		{"eof after write", io.EOF, false, codeOutcomeUnknown},
		{"client timeout", context.DeadlineExceeded, false, codeOutcomeUnknown},
		{"cancelled mid-flight", context.Canceled, false, codeOutcomeUnknown},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			doErr := &url.Error{Op: "Post", URL: "https://api.github.com/repos/o/r/issues", Err: tc.cause}

			err := fmt.Errorf("create issue: %w", transportError(doErr))

			if vouchesUncreated(err) != tc.wantUncreated || codeOfErr(err) != tc.wantCode {
				t.Fatalf("uncreated=%v code=%q, want %v %q", vouchesUncreated(err), codeOfErr(err), tc.wantUncreated, tc.wantCode)
			}
		})
	}
}

// scriptedReply is one canned fake-GitHub answer.
type scriptedReply struct {
	status  int
	headers map[string]string
	body    string
}

var (
	outage  = scriptedReply{status: http.StatusServiceUnavailable, body: `{"message":"Service Unavailable"}`}
	created = scriptedReply{status: http.StatusCreated, body: `{"number":7,"html_url":"https://github.com/o/r/issues/7"}`}
)

// scriptedGitHub is a fake GitHub API that answers the n-th create with
// script[n], and with fallback once the script runs out. It counts every
// request that reaches it.
func scriptedGitHub(t *testing.T, fallback scriptedReply, script ...scriptedReply) (*GitHubIssueTracker, *atomic.Int32) {
	t.Helper()
	var hits atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		reply := fallback
		if n := int(hits.Add(1)); n <= len(script) {
			reply = script[n-1]
		}
		for k, v := range reply.headers {
			w.Header().Set(k, v)
		}
		w.WriteHeader(reply.status)
		_, _ = w.Write([]byte(reply.body))
	}))
	t.Cleanup(server.Close)
	return newTestTracker(server.URL), &hits
}

// refundLimits leaves the per-user cap out of the way so only the global cap,
// with a window longer than the throttle floor, decides what is admitted.
var refundLimits = service.SubmissionLimits{
	PerUser:       1000,
	PerUserWindow: time.Hour,
	Global:        4,
	GlobalWindow:  10 * time.Minute,
}

func refundService(tracker *GitHubIssueTracker) (*service.SubmitReportService, *steppedClock) {
	clock := &steppedClock{t: time.Date(2026, 9, 15, 12, 0, 0, 0, time.UTC)}
	return service.NewSubmitReportServiceWithLimits(tracker, noopMetrics{}, refundLimits, clock.now), clock
}

// admittedUntilRefused submits fresh users until one is refused locally and
// returns how many were admitted (succeeded or failed at GitHub).
func admittedUntilRefused(t *testing.T, svc *service.SubmitReportService) int {
	t.Helper()
	for i := 0; i < 100; i++ {
		if err := submitAsFreshUser(svc); err != nil && isLocalRefusal(err) {
			return i
		}
	}
	t.Fatal("admission never refused")
	return 0
}

func isLocalRefusal(err error) bool {
	var status interface{ HTTPStatus() int }
	return errors.As(err, &status) && status.HTTPStatus() == http.StatusTooManyRequests
}

// TestSubmitReport_GitHubOutageReleasesQuota reproduces #1115 against a fake
// GitHub: creates that failed with a 5xx used to keep their global slots, so
// once GitHub recovered the budget was already gone for issues never created.
// Now those slots are handed back and the full cap is available on recovery.
func TestSubmitReport_GitHubOutageReleasesQuota(t *testing.T) {
	tracker, hits := scriptedGitHub(t, created, outage, outage)
	svc, _ := refundService(tracker)

	for i := 0; i < 2; i++ {
		if err := submitAsFreshUser(svc); err == nil || isLocalRefusal(err) {
			t.Fatalf("outage create %d = %v, want a GitHub failure", i, err)
		}
	}
	if got := admittedUntilRefused(t, svc); got != refundLimits.Global {
		t.Fatalf("after the outage %d reports were admitted, want the full global cap %d", got, refundLimits.Global)
	}
	if got := hits.Load(); got != 2+int32(refundLimits.Global) {
		t.Fatalf("GitHub saw %d creates, want %d", got, 2+refundLimits.Global)
	}
}

// TestSubmitReport_UnreachableGitHubReleasesQuota covers a transport failure:
// the request never got an answer, so no issue exists and the slot comes back.
func TestSubmitReport_UnreachableGitHubReleasesQuota(t *testing.T) {
	tracker, _ := scriptedGitHub(t, created)
	liveURL := tracker.baseURL
	dead := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	dead.Close()
	svc, _ := refundService(tracker)

	tracker.baseURL = dead.URL
	if err := submitAsFreshUser(svc); err == nil || isLocalRefusal(err) {
		t.Fatalf("create against a dead host = %v, want a transport failure", err)
	}
	tracker.baseURL = liveURL
	if got := admittedUntilRefused(t, svc); got != refundLimits.Global {
		t.Fatalf("after an unreachable GitHub %d reports were admitted, want %d", got, refundLimits.Global)
	}
}

// TestSubmitReport_SustainedOutageStillBoundsGitHubCalls is the abuse case: a
// GitHub that fails every create must not let refunds turn admission into an
// open tap. Only half the cap is refundable per window, so a flood reaches
// GitHub at most 1.5x the cap and is then refused locally.
func TestSubmitReport_SustainedOutageStillBoundsGitHubCalls(t *testing.T) {
	tracker, hits := scriptedGitHub(t, outage)
	svc, _ := refundService(tracker)

	want := refundLimits.Global + refundLimits.Global/2
	if got := admittedUntilRefused(t, svc); got != want {
		t.Fatalf("a failing flood was admitted %d times, want %d", got, want)
	}
	for i := 0; i < 50; i++ {
		_ = submitAsFreshUser(svc)
	}
	if got := hits.Load(); got != int32(want) {
		t.Fatalf("GitHub saw %d creates during the outage, want at most %d", got, want)
	}
}

// TestSubmitReport_FailuresThatMayHaveCreatedOrWereCountedKeepQuota pins the
// failures that must still spend their slot: a 201 whose body was lost (GitHub
// created that issue) and a rate-limit answer (GitHub counted the request, and
// the #1116 pause must keep holding).
func TestSubmitReport_FailuresThatMayHaveCreatedOrWereCountedKeepQuota(t *testing.T) {
	cases := []struct {
		name  string
		first scriptedReply
		pause time.Duration
	}{
		{"201 with an undecodable body", scriptedReply{status: http.StatusCreated, body: `{not json`}, 0},
		{"429 throttle", scriptedReply{
			status:  http.StatusTooManyRequests,
			headers: map[string]string{"Retry-After": "90"},
			body:    `{"message":"API rate limit exceeded"}`,
		}, 90 * time.Second},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tracker, hits := scriptedGitHub(t, created, tc.first)
			svc, clock := refundService(tracker)

			if err := submitAsFreshUser(svc); err == nil {
				t.Fatal("the first create should surface as a failure")
			}
			if tc.pause > 0 {
				assertRefusedLocally(t, submitAsFreshUser(svc))
				clock.t = clock.t.Add(tc.pause)
			}
			if got := admittedUntilRefused(t, svc); got != refundLimits.Global-1 {
				t.Fatalf("%d reports admitted after the failure, want %d (its slot stays spent)", got, refundLimits.Global-1)
			}
			if got := hits.Load(); got != int32(refundLimits.Global) {
				t.Fatalf("GitHub saw %d creates, want %d", got, refundLimits.Global)
			}
		})
	}
}

// throttlingGitHub is a fake GitHub API whose first create call answers with the
// given (rate-limit/abuse) response and every later call succeeds with a 201.
// It counts every request that actually reaches it.
func throttlingGitHub(t *testing.T, status int, headers map[string]string, body string) (*GitHubIssueTracker, *atomic.Int32) {
	t.Helper()
	var hits atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		if hits.Add(1) == 1 {
			for k, v := range headers {
				w.Header().Set(k, v)
			}
			w.WriteHeader(status)
			_, _ = w.Write([]byte(body))
			return
		}
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"number":7,"html_url":"https://github.com/o/r/issues/7"}`))
	}))
	t.Cleanup(server.Close)
	return newTestTracker(server.URL), &hits
}

type steppedClock struct{ t time.Time }

func (c *steppedClock) now() time.Time { return c.t }

// generousLimits keeps the local sliding-window caps out of the way so only the
// reaction to GitHub's throttling signal can refuse a submission.
var generousLimits = service.SubmissionLimits{
	PerUser:       1000,
	PerUserWindow: time.Minute,
	Global:        1000,
	GlobalWindow:  time.Minute,
}

type noopMetrics struct{}

func (noopMetrics) TrackerCreateFailed(string) {}
func (noopMetrics) SubmissionRejected(string)  {}
func (noopMetrics) SubmissionCreated()         {}

func submitAsFreshUser(svc *service.SubmitReportService) error {
	_, err := svc.Execute(context.Background(), shared.NewUserId(uuid.New()), service.SubmitReportInput{
		Kind:    "bug",
		Message: "three downloaded tracks went grey again",
	})
	return err
}

func assertRefusedLocally(t *testing.T, err error) {
	t.Helper()
	var status interface{ HTTPStatus() int }
	if !errors.As(err, &status) || status.HTTPStatus() != http.StatusTooManyRequests {
		t.Fatalf("submission during GitHub's lockout = %v, want a local 429 refusal", err)
	}
}

// TestAdmission_PausesOnGitHubThrottleSignal reproduces #1116: GitHub answering a
// create with a rate-limit/abuse response used to leave the admission layer
// admitting at its fixed local rate, so every following report hit GitHub again
// during the lockout. Now the first throttle response pauses admissions for as
// long as GitHub asked, refusing locally (429) without calling GitHub, and
// admissions resume once that wait has elapsed.
func TestAdmission_PausesOnGitHubThrottleSignal(t *testing.T) {
	cases := []struct {
		name    string
		status  int
		headers func() map[string]string
		body    string
		// stillPaused is a point inside the requested wait; resumed is past it.
		stillPaused time.Duration
		resumed     time.Duration
	}{
		{
			name:        "429 with Retry-After",
			status:      http.StatusTooManyRequests,
			headers:     func() map[string]string { return map[string]string{"Retry-After": "120"} },
			body:        `{"message":"API rate limit exceeded"}`,
			stillPaused: 119 * time.Second,
			resumed:     120 * time.Second,
		},
		{
			name:        "403 secondary limit with Retry-After",
			status:      http.StatusForbidden,
			headers:     func() map[string]string { return map[string]string{"Retry-After": "300"} },
			body:        `{"message":"You have exceeded a secondary rate limit."}`,
			stillPaused: 299 * time.Second,
			resumed:     300 * time.Second,
		},
		{
			name:   "403 primary limit exhausted until X-RateLimit-Reset",
			status: http.StatusForbidden,
			headers: func() map[string]string {
				return map[string]string{
					"X-RateLimit-Remaining": "0",
					"X-RateLimit-Reset":     strconv.FormatInt(time.Now().Add(10*time.Minute).Unix(), 10),
				}
			},
			body:        `{"message":"API rate limit exceeded"}`,
			stillPaused: 9 * time.Minute,
			resumed:     10*time.Minute + time.Second,
		},
		{
			name:        "403 secondary limit without headers waits at least a minute",
			status:      http.StatusForbidden,
			headers:     func() map[string]string { return nil },
			body:        `{"message":"You have exceeded a secondary rate limit. Please wait a few minutes before you try again."}`,
			stillPaused: 59 * time.Second,
			resumed:     time.Minute,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tracker, hits := throttlingGitHub(t, tc.status, tc.headers(), tc.body)
			start := time.Date(2026, 9, 15, 12, 0, 0, 0, time.UTC)
			clock := &steppedClock{t: start}
			svc := service.NewSubmitReportServiceWithLimits(tracker, noopMetrics{}, generousLimits, clock.now)

			if err := submitAsFreshUser(svc); err == nil {
				t.Fatal("the throttled create should surface as a failure")
			}
			for i := 0; i < 5; i++ {
				assertRefusedLocally(t, submitAsFreshUser(svc))
			}
			clock.t = start.Add(tc.stillPaused)
			assertRefusedLocally(t, submitAsFreshUser(svc))
			if got := hits.Load(); got != 1 {
				t.Fatalf("GitHub was called %d times during its lockout, want 1", got)
			}

			clock.t = start.Add(tc.resumed)
			if err := submitAsFreshUser(svc); err != nil {
				t.Fatalf("submission after GitHub's wait elapsed was refused: %v", err)
			}
			if got := hits.Load(); got != 2 {
				t.Fatalf("GitHub was called %d times, want 2 once the wait elapsed", got)
			}
		})
	}
}

func TestRequestedBackoff_ReadsGitHubHeaders(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	cases := []struct {
		name    string
		headers map[string]string
		want    time.Duration
	}{
		{"retry-after seconds", map[string]string{"Retry-After": "42"}, 42 * time.Second},
		{"retry-after wins over reset", map[string]string{"Retry-After": "5", "X-RateLimit-Remaining": "0", "X-RateLimit-Reset": "1800000600"}, 5 * time.Second},
		{"reset when exhausted", map[string]string{"X-RateLimit-Remaining": "0", "X-RateLimit-Reset": "1800000600"}, 10 * time.Minute},
		{"reset ignored while quota remains", map[string]string{"X-RateLimit-Remaining": "12", "X-RateLimit-Reset": "1800000600"}, 0},
		{"reset in the past", map[string]string{"X-RateLimit-Remaining": "0", "X-RateLimit-Reset": "1799999000"}, 0},
		{"absurd retry-after does not overflow", map[string]string{"Retry-After": "92233720369"}, 24 * time.Hour},
		{"far-future reset is clamped", map[string]string{"X-RateLimit-Remaining": "0", "X-RateLimit-Reset": "9223372036854775807"}, 24 * time.Hour},
		{"far-past reset does not wrap", map[string]string{"X-RateLimit-Remaining": "0", "X-RateLimit-Reset": "-9223372036854775808"}, 0},
		{"negative retry-after falls through", map[string]string{"Retry-After": "-5"}, 0},
		{"garbage values", map[string]string{"Retry-After": "soon", "X-RateLimit-Remaining": "0", "X-RateLimit-Reset": "later"}, 0},
		{"no headers", nil, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := http.Header{}
			for k, v := range tc.headers {
				h.Set(k, v)
			}
			if got := requestedBackoff(h, now); got != tc.want {
				t.Fatalf("requestedBackoff = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestAdmission_NonThrottleFailureDoesNotPause pins that only a rate-limit
// signal pauses admissions: a plain GitHub outage or a dead token keeps admitting.
func TestAdmission_NonThrottleFailureDoesNotPause(t *testing.T) {
	cases := []struct {
		name   string
		status int
		body   string
	}{
		{"bad gateway", http.StatusBadGateway, `{"message":"boom"}`},
		{"forbidden without rate-limit signal", http.StatusForbidden, `{"message":"Resource not accessible by personal access token"}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tracker, hits := throttlingGitHub(t, tc.status, nil, tc.body)
			clock := &steppedClock{t: time.Date(2026, 9, 15, 12, 0, 0, 0, time.UTC)}
			svc := service.NewSubmitReportServiceWithLimits(tracker, noopMetrics{}, generousLimits, clock.now)

			_ = submitAsFreshUser(svc)
			if err := submitAsFreshUser(svc); err != nil {
				t.Fatalf("a non-throttle failure paused admissions: %v", err)
			}
			if got := hits.Load(); got != 2 {
				t.Fatalf("GitHub was called %d times, want 2", got)
			}
		})
	}
}
