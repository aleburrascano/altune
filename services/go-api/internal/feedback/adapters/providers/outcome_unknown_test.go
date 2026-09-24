package providers

import (
	"altune/go-api/internal/feedback/ports"
	"altune/go-api/internal/feedback/service"
	"altune/go-api/internal/shared"
	"altune/go-api/internal/shared/httputil"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/google/uuid"
)

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

func TestSubmitReport_Truncated201AnswersOutcomeUnknownAndKeyedRetryDoesNotDuplicate(t *testing.T) {
	truncated := scriptedReply{status: http.StatusCreated, body: `{"number":7,`}
	tracker, hits := scriptedGitHub(t, created, truncated)
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
		t.Fatalf("GitHub saw %d creates, want 1: the keyed retry must not duplicate", got)
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
