package service

import (
	"altune/go-api/internal/feedback/domain"
	"altune/go-api/internal/feedback/ports"
	"altune/go-api/internal/shared"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
)

// capturingHandler records the attributes of each slog record so tests can
// assert on what the failure path logs.
type capturingHandler struct {
	records []map[string]string
}

func (h *capturingHandler) Enabled(context.Context, slog.Level) bool { return true }

func (h *capturingHandler) Handle(_ context.Context, r slog.Record) error {
	attrs := map[string]string{"msg": r.Message}
	r.Attrs(func(a slog.Attr) bool {
		attrs[a.Key] = a.Value.String()
		return true
	})
	h.records = append(h.records, attrs)
	return nil
}

func (h *capturingHandler) WithAttrs([]slog.Attr) slog.Handler { return h }
func (h *capturingHandler) WithGroup(string) slog.Handler      { return h }

// captureLogs swaps the default slog logger for a capturing one for the
// duration of the test and returns the handler holding the records.
func captureLogs(t *testing.T) *capturingHandler {
	t.Helper()
	h := &capturingHandler{}
	prev := slog.Default()
	slog.SetDefault(slog.New(h))
	t.Cleanup(func() { slog.SetDefault(prev) })
	return h
}

type recordingTracker struct {
	reports []*domain.Report
	err     error
}

func (t *recordingTracker) Create(_ context.Context, report *domain.Report) (ports.IssueRef, error) {
	if t.err != nil {
		return ports.IssueRef{}, t.err
	}
	t.reports = append(t.reports, report)
	return ports.IssueRef{Number: 42, URL: "https://github.com/o/r/issues/42"}, nil
}

// gatedTracker blocks each Create on release, so a test can hold the one caller
// that reaches the tracker while its concurrent duplicates queue behind the
// idempotency store, then count how many creates actually ran.
type gatedTracker struct {
	release chan struct{}
	mu      sync.Mutex
	creates int
}

func (g *gatedTracker) Create(_ context.Context, _ *domain.Report) (ports.IssueRef, error) {
	<-g.release
	g.mu.Lock()
	g.creates++
	n := g.creates
	g.mu.Unlock()
	return ports.IssueRef{Number: n, URL: "https://github.com/o/r/issues/1"}, nil
}

func (g *gatedTracker) count() int {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.creates
}

type recordingMetrics struct {
	trackerFailures int
	causes          []string
}

func (m *recordingMetrics) SubmissionRejected(string) {}
func (m *recordingMetrics) SubmissionCreated()        {}

func (m *recordingMetrics) TrackerCreateFailed(cause string) {
	m.trackerFailures++
	m.causes = append(m.causes, cause)
}

// codedErr is a tracker failure carrying a wire code, like the GitHub adapter's.
type codedErr struct{ code string }

func (e codedErr) Error() string     { return "github issues: " + e.code }
func (e codedErr) ErrorCode() string { return e.code }

func newUser() shared.UserId { return shared.NewUserId(uuid.New()) }

func validInput() SubmitReportInput {
	return SubmitReportInput{Kind: "bug", Message: "three downloaded tracks went grey again"}
}

func TestSubmitReport_CreatesIssueAndReturnsRef(t *testing.T) {
	tracker := &recordingTracker{}
	svc := NewSubmitReportService(tracker, &recordingMetrics{})

	ref, err := svc.Execute(context.Background(), newUser(), validInput())
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if ref.Number != 42 {
		t.Fatalf("issue number = %d, want 42", ref.Number)
	}
	if len(tracker.reports) != 1 {
		t.Fatalf("tracker saw %d reports, want 1", len(tracker.reports))
	}
}

func TestSubmitReport_RejectsUnknownKindBeforeCallingTracker(t *testing.T) {
	tracker := &recordingTracker{}
	svc := NewSubmitReportService(tracker, &recordingMetrics{})

	input := validInput()
	input.Kind = "rant"
	if _, err := svc.Execute(context.Background(), newUser(), input); err == nil {
		t.Fatal("expected unknown kind to be rejected")
	}
	if len(tracker.reports) != 0 {
		t.Fatalf("tracker saw %d reports, want none", len(tracker.reports))
	}
}

func TestSubmitReport_RejectsShortMessageBeforeCallingTracker(t *testing.T) {
	tracker := &recordingTracker{}
	svc := NewSubmitReportService(tracker, &recordingMetrics{})

	input := validInput()
	input.Message = "broken"
	if _, err := svc.Execute(context.Background(), newUser(), input); err == nil {
		t.Fatal("expected a short message to be rejected")
	}
	if len(tracker.reports) != 0 {
		t.Fatalf("tracker saw %d reports, want none", len(tracker.reports))
	}
}

type fakeClock struct{ t time.Time }

func (c *fakeClock) now() time.Time          { return c.t }
func (c *fakeClock) advance(d time.Duration) { c.t = c.t.Add(d) }

var testLimits = SubmissionLimits{
	PerUser:       3,
	PerUserWindow: 10 * time.Minute,
	Global:        5,
	GlobalWindow:  time.Minute,
}

func throttledService(tracker ports.IssueTracker) (*SubmitReportService, *fakeClock) {
	clock := &fakeClock{t: time.Date(2026, 9, 13, 12, 0, 0, 0, time.UTC)}
	return NewSubmitReportServiceWithLimits(tracker, &recordingMetrics{}, testLimits, clock.now), clock
}

func assertThrottled(t *testing.T, err error, want error) {
	t.Helper()
	if !errors.Is(err, want) {
		t.Fatalf("err = %v, want %v", err, want)
	}
	var status interface{ HTTPStatus() int }
	if !errors.As(err, &status) || status.HTTPStatus() != 429 {
		t.Fatalf("throttle error must map to 429, got %v", err)
	}
}

func TestSubmitReport_ThrottlesAUserDumpingReports(t *testing.T) {
	tracker := &recordingTracker{}
	svc := NewSubmitReportService(tracker, &recordingMetrics{})
	user := newUser()

	var refused int
	for i := 0; i < 25; i++ {
		if _, err := svc.Execute(context.Background(), user, validInput()); err != nil {
			assertThrottled(t, err, ErrUserReportLimit)
			refused++
		}
	}
	if len(tracker.reports) != DefaultSubmissionLimits.PerUser {
		t.Fatalf("tracker saw %d reports, want the per-user cap of %d",
			len(tracker.reports), DefaultSubmissionLimits.PerUser)
	}
	if refused != 25-DefaultSubmissionLimits.PerUser {
		t.Fatalf("refused %d reports, want %d", refused, 25-DefaultSubmissionLimits.PerUser)
	}
}

func TestSubmitReport_UserLimitResetsAfterTheWindow(t *testing.T) {
	tracker := &recordingTracker{}
	svc, clock := throttledService(tracker)
	user := newUser()

	for i := 0; i < testLimits.PerUser; i++ {
		if _, err := svc.Execute(context.Background(), user, validInput()); err != nil {
			t.Fatalf("report %d was refused: %v", i, err)
		}
		clock.advance(time.Minute)
	}
	_, err := svc.Execute(context.Background(), user, validInput())
	assertThrottled(t, err, ErrUserReportLimit)

	clock.advance(testLimits.PerUserWindow - 2*time.Minute)
	if _, err := svc.Execute(context.Background(), user, validInput()); err != nil {
		t.Fatalf("report after the oldest one aged out was refused: %v", err)
	}
	if len(tracker.reports) != testLimits.PerUser+1 {
		t.Fatalf("tracker saw %d reports, want %d", len(tracker.reports), testLimits.PerUser+1)
	}
}

func TestSubmitReport_OneUsersLimitDoesNotBlockOthers(t *testing.T) {
	tracker := &recordingTracker{}
	svc, _ := throttledService(tracker)
	noisy := newUser()

	for i := 0; i < testLimits.PerUser+2; i++ {
		_, _ = svc.Execute(context.Background(), noisy, validInput())
	}
	if _, err := svc.Execute(context.Background(), newUser(), validInput()); err != nil {
		t.Fatalf("a quiet user was refused because of a noisy one: %v", err)
	}
}

func TestSubmitReport_GlobalLimitBoundsABurstAcrossUsers(t *testing.T) {
	tracker := &recordingTracker{}
	svc, clock := throttledService(tracker)

	for i := 0; i < testLimits.Global; i++ {
		if _, err := svc.Execute(context.Background(), newUser(), validInput()); err != nil {
			t.Fatalf("report %d was refused: %v", i, err)
		}
	}
	_, err := svc.Execute(context.Background(), newUser(), validInput())
	assertThrottled(t, err, ErrGlobalReportLimit)
	if len(tracker.reports) != testLimits.Global {
		t.Fatalf("tracker saw %d reports, want the global cap of %d", len(tracker.reports), testLimits.Global)
	}

	clock.advance(testLimits.GlobalWindow)
	if _, err := svc.Execute(context.Background(), newUser(), validInput()); err != nil {
		t.Fatalf("report after the global window was refused: %v", err)
	}
}

func TestSubmitReport_RejectedInputDoesNotSpendTheQuota(t *testing.T) {
	tracker := &recordingTracker{}
	svc, _ := throttledService(tracker)
	user := newUser()

	bad := validInput()
	bad.Message = "broken"
	for i := 0; i < testLimits.PerUser*2; i++ {
		_, _ = svc.Execute(context.Background(), user, bad)
	}
	if _, err := svc.Execute(context.Background(), user, validInput()); err != nil {
		t.Fatalf("valid report refused after invalid ones: %v", err)
	}
}

func TestSubmitReport_WrapsTrackerFailure(t *testing.T) {
	tracker := &recordingTracker{err: errors.New("github is down")}
	svc := NewSubmitReportService(tracker, &recordingMetrics{})

	if _, err := svc.Execute(context.Background(), newUser(), validInput()); err == nil {
		t.Fatal("expected the tracker failure to surface")
	}
}

func TestSubmitReport_CountsTrackerFailure(t *testing.T) {
	tracker := &recordingTracker{err: errors.New("bad credentials")}
	metrics := &recordingMetrics{}
	svc := NewSubmitReportService(tracker, metrics)

	if _, err := svc.Execute(context.Background(), newUser(), validInput()); err == nil {
		t.Fatal("expected the tracker failure to surface")
	}
	if metrics.trackerFailures != 1 {
		t.Fatalf("tracker failures = %d, want 1", metrics.trackerFailures)
	}
}

func TestSubmitReport_LogsReportContextOnTrackerFailure(t *testing.T) {
	logs := captureLogs(t)
	tracker := &recordingTracker{err: errors.New("github is down")}
	svc := NewSubmitReportService(tracker, &recordingMetrics{})
	user := newUser()

	if _, err := svc.Execute(context.Background(), user, validInput()); err == nil {
		t.Fatal("expected the tracker failure to surface")
	}

	var rec map[string]string
	for _, r := range logs.records {
		if r["msg"] == "feedback.create_failed" {
			rec = r
			break
		}
	}
	if rec == nil {
		t.Fatal("failure path logged no feedback.create_failed record")
	}
	if rec["kind"] != "bug" {
		t.Errorf("logged kind = %q, want %q", rec["kind"], "bug")
	}
	if rec["user_id"] != user.String() {
		t.Errorf("logged user_id = %q, want %q", rec["user_id"], user.String())
	}
	if rec["cause"] != ports.TrackerFailureUnclassified {
		t.Errorf("logged cause = %q, want %q", rec["cause"], ports.TrackerFailureUnclassified)
	}
}

// TestSubmitReport_CountsTrackerFailureUnderItsCause pins that a coded tracker
// failure is counted and logged under its code, and an uncoded one under the
// fixed unclassified cause, so the metric can be split by cause.
func TestSubmitReport_CountsTrackerFailureUnderItsCause(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want string
	}{
		{"coded", codedErr{code: "tracker_unauthorized"}, "tracker_unauthorized"},
		{"wrapped coded", fmt.Errorf("outer: %w", codedErr{code: "tracker_unreachable"}), "tracker_unreachable"},
		{"uncoded", errors.New("decode issue: unexpected EOF"), ports.TrackerFailureUnclassified},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			logs := captureLogs(t)
			metrics := &recordingMetrics{}
			svc := NewSubmitReportService(&recordingTracker{err: tc.err}, metrics)

			if _, err := svc.Execute(context.Background(), newUser(), validInput()); err == nil {
				t.Fatal("expected the tracker failure to surface")
			}
			if len(metrics.causes) != 1 || metrics.causes[0] != tc.want {
				t.Fatalf("counted causes = %v, want [%s]", metrics.causes, tc.want)
			}
			for _, r := range logs.records {
				if r["msg"] == "feedback.create_failed" && r["cause"] != tc.want {
					t.Fatalf("logged cause = %q, want %q", r["cause"], tc.want)
				}
			}
		})
	}
}

func keyPtr(s string) *string { return &s }

// TestSubmitReport_KeylessCallsEachCreateAnIssue pins the pre-existing (and
// still-correct) behaviour: with no idempotency key, a retry or double-tap
// creates a second issue. This is the defect scenario absent a key.
func TestSubmitReport_KeylessCallsEachCreateAnIssue(t *testing.T) {
	tracker := &recordingTracker{}
	svc := NewSubmitReportService(tracker, &recordingMetrics{})
	user := newUser()

	for i := 0; i < 2; i++ {
		if _, err := svc.Execute(context.Background(), user, validInput()); err != nil {
			t.Fatalf("Execute %d: %v", i, err)
		}
	}
	if len(tracker.reports) != 2 {
		t.Fatalf("keyless calls created %d issues, want 2", len(tracker.reports))
	}
}

// TestSubmitReport_DuplicateKeyReplaysFirstIssue is the regression guard: a
// retried submission carrying the same idempotency key must not create a second
// issue and must return the first result.
func TestSubmitReport_DuplicateKeyReplaysFirstIssue(t *testing.T) {
	tracker := &recordingTracker{}
	svc := NewSubmitReportService(tracker, &recordingMetrics{})
	user := newUser()

	input := validInput()
	input.IdempotencyKey = keyPtr("retry-0001")

	first, err := svc.Execute(context.Background(), user, input)
	if err != nil {
		t.Fatalf("first Execute: %v", err)
	}
	second, err := svc.Execute(context.Background(), user, input)
	if err != nil {
		t.Fatalf("second Execute: %v", err)
	}

	if len(tracker.reports) != 1 {
		t.Fatalf("duplicate key created %d issues, want 1", len(tracker.reports))
	}
	if first != second {
		t.Fatalf("replay returned %+v, want the first result %+v", second, first)
	}
}

// TestSubmitReport_KeyIsScopedPerUser ensures one user's key cannot mask
// another user's distinct submission.
func TestSubmitReport_KeyIsScopedPerUser(t *testing.T) {
	tracker := &recordingTracker{}
	svc := NewSubmitReportService(tracker, &recordingMetrics{})

	input := validInput()
	input.IdempotencyKey = keyPtr("shared-key")

	if _, err := svc.Execute(context.Background(), newUser(), input); err != nil {
		t.Fatalf("first user Execute: %v", err)
	}
	if _, err := svc.Execute(context.Background(), newUser(), input); err != nil {
		t.Fatalf("second user Execute: %v", err)
	}
	if len(tracker.reports) != 2 {
		t.Fatalf("same key across users created %d issues, want 2", len(tracker.reports))
	}
}

// TestSubmitReport_DifferentKeysCreateSeparateIssues confirms distinct keys are
// not collapsed.
func TestSubmitReport_DifferentKeysCreateSeparateIssues(t *testing.T) {
	tracker := &recordingTracker{}
	svc := NewSubmitReportService(tracker, &recordingMetrics{})
	user := newUser()

	for _, k := range []string{"a", "b"} {
		input := validInput()
		input.IdempotencyKey = keyPtr(k)
		if _, err := svc.Execute(context.Background(), user, input); err != nil {
			t.Fatalf("Execute key %q: %v", k, err)
		}
	}
	if len(tracker.reports) != 2 {
		t.Fatalf("distinct keys created %d issues, want 2", len(tracker.reports))
	}
}

// TestSubmitReport_FailedSubmissionUnderKeyStaysRetryable proves a failed first
// attempt does not poison the key: a later call under it can still succeed.
func TestSubmitReport_FailedSubmissionUnderKeyStaysRetryable(t *testing.T) {
	tracker := &recordingTracker{err: errors.New("github is down")}
	svc := NewSubmitReportService(tracker, &recordingMetrics{})
	user := newUser()

	input := validInput()
	input.IdempotencyKey = keyPtr("retry-after-failure")

	if _, err := svc.Execute(context.Background(), user, input); err == nil {
		t.Fatal("expected the first attempt to fail")
	}
	tracker.err = nil
	if _, err := svc.Execute(context.Background(), user, input); err != nil {
		t.Fatalf("retry after a failed attempt was refused: %v", err)
	}
	if len(tracker.reports) != 1 {
		t.Fatalf("tracker saw %d reports, want 1 after the retry succeeded", len(tracker.reports))
	}
}

// TestSubmitReport_ConcurrentDuplicatesCreateOneIssue is the double-tap case:
// several requests sharing a key that arrive together must collapse onto a
// single created issue.
func TestSubmitReport_ConcurrentDuplicatesCreateOneIssue(t *testing.T) {
	tracker := &gatedTracker{release: make(chan struct{})}
	svc := NewSubmitReportService(tracker, &recordingMetrics{})
	user := newUser()

	input := validInput()
	input.IdempotencyKey = keyPtr("double-tap")

	const callers = 8
	var wg sync.WaitGroup
	refs := make([]ports.IssueRef, callers)
	for i := 0; i < callers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			ref, err := svc.Execute(context.Background(), user, input)
			if err != nil {
				t.Errorf("caller %d: %v", i, err)
			}
			refs[i] = ref
		}(i)
	}
	// The lone caller that reaches the tracker is parked on release; every other
	// caller is waiting on it. Releasing now lets exactly one create proceed.
	close(tracker.release)
	wg.Wait()

	if got := tracker.count(); got != 1 {
		t.Fatalf("concurrent duplicates created %d issues, want 1", got)
	}
	for i, ref := range refs {
		if ref != refs[0] {
			t.Fatalf("caller %d saw %+v, want the shared result %+v", i, ref, refs[0])
		}
	}
}

func TestSubmitReport_RejectsOversizedIdempotencyKey(t *testing.T) {
	tracker := &recordingTracker{}
	svc := NewSubmitReportService(tracker, &recordingMetrics{})

	input := validInput()
	oversized := make([]byte, maxIdempotencyKeyLength+1)
	for i := range oversized {
		oversized[i] = 'k'
	}
	input.IdempotencyKey = keyPtr(string(oversized))

	if _, err := svc.Execute(context.Background(), newUser(), input); err == nil {
		t.Fatal("expected an oversized idempotency key to be rejected")
	}
	if len(tracker.reports) != 0 {
		t.Fatalf("tracker saw %d reports, want none", len(tracker.reports))
	}
}

func TestSubmitReport_DoesNotCountSuccessOrRejectedInput(t *testing.T) {
	tracker := &recordingTracker{}
	metrics := &recordingMetrics{}
	svc := NewSubmitReportService(tracker, metrics)

	if _, err := svc.Execute(context.Background(), newUser(), validInput()); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	input := validInput()
	input.Kind = "rant"
	_, _ = svc.Execute(context.Background(), newUser(), input)
	if metrics.trackerFailures != 0 {
		t.Fatalf("tracker failures = %d, want 0", metrics.trackerFailures)
	}
}

// rejectionLog runs one rejected submission with logs captured and returns the
// feedback.rejected record, failing the test if none was emitted.
func rejectionLog(t *testing.T, user shared.UserId, input SubmitReportInput) map[string]string {
	t.Helper()
	logs := captureLogs(t)
	svc := NewSubmitReportService(&recordingTracker{}, &recordingMetrics{})
	if _, err := svc.Execute(context.Background(), user, input); err == nil {
		t.Fatal("expected the submission to be rejected")
	}
	for _, r := range logs.records {
		if r["msg"] == "feedback.rejected" {
			return r
		}
	}
	t.Fatal("rejection logged no feedback.rejected record")
	return nil
}

// assertRejectionAttrs checks the wanted attributes and that no attribute
// echoes the submitted text: a report can hold pasted secrets and redaction
// only runs on an accepted report, so a rejection log must never carry input.
func assertRejectionAttrs(t *testing.T, rec map[string]string, want map[string]string, content ...string) {
	t.Helper()
	for k, v := range want {
		if rec[k] != v {
			t.Errorf("logged %s = %q, want %q", k, rec[k], v)
		}
	}
	for k, v := range rec {
		for _, c := range content {
			if k != "msg" && strings.Contains(v, c) {
				t.Errorf("rejection log attr %s = %q echoes user content %q", k, v, c)
			}
		}
	}
}

func TestSubmitReport_LogsUnknownKindRejection(t *testing.T) {
	user := newUser()
	input := validInput()
	input.Kind = "rant-ghp_secretish"

	rec := rejectionLog(t, user, input)

	assertRejectionAttrs(t, rec, map[string]string{
		"reason":     "unknown_kind",
		"error_code": "feedback.validation_error",
		"user_id":    user.String(),
		"kind_runes": "18",
	}, "ghp_secretish", input.Message)
	if v, ok := rec["kind"]; ok {
		t.Errorf("an unparseable kind must not be logged, got kind = %q", v)
	}
}

func TestSubmitReport_LogsInvalidMessageRejection(t *testing.T) {
	user := newUser()
	input := validInput()
	input.Kind = "idea"
	input.Message = "pw hunter"

	rec := rejectionLog(t, user, input)

	assertRejectionAttrs(t, rec, map[string]string{
		"reason":        "invalid_report",
		"error_code":    "feedback.validation_error",
		"user_id":       user.String(),
		"kind":          "idea",
		"message_runes": "9",
	}, "hunter")
}

func TestSubmitReport_LogsInvalidIdempotencyKeyRejection(t *testing.T) {
	user := newUser()
	input := validInput()
	input.IdempotencyKey = keyPtr("")

	rec := rejectionLog(t, user, input)

	assertRejectionAttrs(t, rec, map[string]string{
		"reason":                "invalid_idempotency_key",
		"user_id":               user.String(),
		"kind":                  "bug",
		"idempotency_key_bytes": "0",
		"error_code":            "feedback.validation_error",
	}, input.Message)
}

// uncreatedErr is a tracker failure that vouches no issue was created, like the
// GitHub adapter's classified outage, refusal and transport errors.
type uncreatedErr struct{ code string }

func (e uncreatedErr) Error() string     { return "github issues: " + e.code }
func (e uncreatedErr) ErrorCode() string { return e.code }
func (e uncreatedErr) Uncreated() bool   { return true }

// uncreatedThrottleErr is a rate-limited create: nothing was created, but
// GitHub counted the request against the token.
type uncreatedThrottleErr struct{ uncreatedErr }

func (uncreatedThrottleErr) Throttled() (time.Duration, bool) { return 0, true }

type countingTracker struct {
	n   int
	err error
}

func (c *countingTracker) Create(context.Context, *domain.Report) (ports.IssueRef, error) {
	c.n++
	return ports.IssueRef{}, c.err
}

// TestSubmitReport_FailedCreateReleasesTheQuotaSlot reproduces #1115: a failed
// tracker create used to spend its user and global slots for good, so an outage
// ate the budget of issues that never existed. Now the slots are handed back:
// the user still gets the full per-user cap, and everyone the full global cap,
// once the tracker recovers.
func TestSubmitReport_FailedCreateReleasesTheQuotaSlot(t *testing.T) {
	tracker := &recordingTracker{err: uncreatedErr{code: "tracker_unavailable"}}
	svc, _ := throttledService(tracker)
	user := newUser()

	if _, err := svc.Execute(context.Background(), user, validInput()); err == nil {
		t.Fatal("expected the tracker failure to surface")
	}
	if _, err := svc.Execute(context.Background(), newUser(), validInput()); err == nil {
		t.Fatal("expected the tracker failure to surface")
	}

	tracker.err = nil
	for i := 0; i < testLimits.PerUser; i++ {
		if _, err := svc.Execute(context.Background(), user, validInput()); err != nil {
			t.Fatalf("report %d after a failed create was refused, its user slot was not released: %v", i, err)
		}
	}
	for i := testLimits.PerUser; i < testLimits.Global; i++ {
		if _, err := svc.Execute(context.Background(), newUser(), validInput()); err != nil {
			t.Fatalf("report %d after failed creates was refused, their global slots were not released: %v", i, err)
		}
	}
	if len(tracker.reports) != testLimits.Global {
		t.Fatalf("tracker created %d issues after the outage, want the full global cap %d", len(tracker.reports), testLimits.Global)
	}
}

// TestSubmitReport_RepeatedFailuresCannotHammerTheTracker is the abuse case: a
// caller (or an outage) that keeps making creates fail must not turn refunds
// into unlimited tracker calls. Refunds stop at half of each cap, after which
// failures spend quota as before.
func TestSubmitReport_RepeatedFailuresCannotHammerTheTracker(t *testing.T) {
	calls := &countingTracker{err: uncreatedErr{code: "tracker_unavailable"}}
	svc, _ := throttledService(calls)
	for i := 0; i < 100; i++ {
		_, _ = svc.Execute(context.Background(), newUser(), validInput())
	}
	if want := testLimits.Global + testLimits.Global/2; calls.n != want {
		t.Fatalf("a failing flood reached the tracker %d times in one window, want %d", calls.n, want)
	}
	_, err := svc.Execute(context.Background(), newUser(), validInput())
	assertThrottled(t, err, ErrGlobalReportLimit)

	calls = &countingTracker{err: uncreatedErr{code: "tracker_rejected"}}
	svc, _ = throttledService(calls)
	user := newUser()
	for i := 0; i < 100; i++ {
		_, _ = svc.Execute(context.Background(), user, validInput())
	}
	if want := testLimits.PerUser + testLimits.PerUser/2; calls.n != want {
		t.Fatalf("one user's failing creates reached the tracker %d times, want %d", calls.n, want)
	}
}

// TestSubmitReport_OnlyUncreatedNonThrottleFailuresRefund pins what never hands
// a slot back: a failure the tracker does not vouch for (an issue may exist,
// e.g. a 201 whose body was lost) and a throttle, which GitHub counted and whose
// pause must still hold.
func TestSubmitReport_OnlyUncreatedNonThrottleFailuresRefund(t *testing.T) {
	cases := []struct {
		name     string
		err      error
		throttle bool
	}{
		{"unvouched failure", errors.New("github issues: decode issue: unexpected EOF"), false},
		{"throttle", fmt.Errorf("github issues: %w", uncreatedThrottleErr{uncreatedErr{code: "tracker_rate_limited"}}), true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tracker := &recordingTracker{err: tc.err}
			svc, clock := throttledService(tracker)
			user := newUser()

			_, _ = svc.Execute(context.Background(), user, validInput())
			if tc.throttle {
				_, err := svc.Execute(context.Background(), user, validInput())
				assertThrottled(t, err, ErrTrackerPaused)
			}
			clock.advance(throttlePauseFloor)

			tracker.err = nil
			for i := 1; i < testLimits.PerUser; i++ {
				if _, err := svc.Execute(context.Background(), user, validInput()); err != nil {
					t.Fatalf("report %d was refused: %v", i, err)
				}
			}
			_, err := svc.Execute(context.Background(), user, validInput())
			assertThrottled(t, err, ErrUserReportLimit)
		})
	}
}
