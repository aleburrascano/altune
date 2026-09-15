package providers

import (
	"altune/go-api/internal/feedback/service"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

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
