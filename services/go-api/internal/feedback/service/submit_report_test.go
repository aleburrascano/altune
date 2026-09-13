package service

import (
	"altune/go-api/internal/feedback/domain"
	"altune/go-api/internal/feedback/ports"
	"altune/go-api/internal/shared"
	"context"
	"errors"
	"log/slog"
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

type recordingMetrics struct {
	trackerFailures int
}

func (m *recordingMetrics) TrackerCreateFailed() { m.trackerFailures++ }

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
