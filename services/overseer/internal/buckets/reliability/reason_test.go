package reliability

import (
	"altune/overseer/internal/goapi"
	"testing"
)

// TestTokenFailureReasonIsAuthNeverDown is the Done proof from the ticket: a
// bucket whose token source fails (the admin-health read cannot even acquire a
// token) shows reason "auth" in the snapshot, never "down" — a dead credential
// must never read the same as go-api actually being unreachable.
func TestTokenFailureReasonIsAuthNeverDown(t *testing.T) {
	reader := &fakeReader{}
	checker := &fakeChecker{}
	reader.set(goapi.OperatorHealth{}, &goapi.TokenError{Op: "acquire read-only token", Err: goapi.ErrNoToken})
	checker.set(goapi.Health{Status: "ok"}, nil)

	b := newBucket(reader, checker, defaultPollInterval)
	if err := collectStore(t, b); err == nil {
		t.Fatal("Collect with a failing token source returned nil error")
	}

	snap := b.Snapshot()
	if snap.Reason != "auth" {
		t.Errorf("Reason = %q, want auth", snap.Reason)
	}
	if snap.Reason == "down" {
		t.Fatal("a dead credential classified as down; must be auth")
	}
}

// TestReasonClearsOnRecovery proves Reason is not sticky past the failure it
// describes: a good read after a failing one clears it back to "".
func TestReasonClearsOnRecovery(t *testing.T) {
	reader := &fakeReader{}
	checker := &fakeChecker{}
	reader.set(goapi.OperatorHealth{}, &goapi.TokenError{Op: "acquire read-only token", Err: goapi.ErrNoToken})
	checker.set(goapi.Health{Status: "ok"}, nil)

	b := newBucket(reader, checker, defaultPollInterval)
	_ = collectStore(t, b)
	if b.Snapshot().Reason != "auth" {
		t.Fatalf("Reason after failure = %q, want auth", b.Snapshot().Reason)
	}

	reader.set(healthyHealth(), nil)
	if err := collectStore(t, b); err != nil {
		t.Fatalf("Collect after recovery = %v, want nil", err)
	}
	if got := b.Snapshot().Reason; got != "" {
		t.Errorf("Reason after recovery = %q, want empty", got)
	}
}
