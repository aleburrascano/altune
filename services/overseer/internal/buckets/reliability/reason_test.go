package reliability

import (
	"altune/overseer/internal/goapi"
	"testing"
)

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
