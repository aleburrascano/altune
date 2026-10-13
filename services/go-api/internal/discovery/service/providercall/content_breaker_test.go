package providercall

import (
	"altune/go-api/internal/discovery/discoverytest"
	"altune/go-api/internal/discovery/domain"
	"context"
	"errors"
	"testing"
)

func TestRunAdmitted_SettlesWithMappedOutcome(t *testing.T) {
	cb := NewCircuitBreaker()
	c, ok := AdmitProviderCall(context.Background(), cb, domain.ProviderDeezer)
	if !ok {
		t.Fatal("fresh breaker refused a call")
	}
	_, err := RunAdmitted(context.Background(), c,
		func(error) error { return nil },
		func() (int, error) { return 1, discoverytest.UpstreamDown })
	if !errors.Is(err, discoverytest.UpstreamDown) {
		t.Fatalf("err = %v, want the call's own error returned unmapped", err)
	}
	for i := 0; i < 10; i++ {
		if !cb.AllowRequest(context.Background(), domain.ProviderDeezer) {
			t.Fatal("mapper returned nil, so failures must not be recorded")
		}
	}
}

func TestRunAdmitted_PanicRecordsOneFailureAndPropagates(t *testing.T) {
	cb := NewCircuitBreaker()
	c, _ := AdmitProviderCall(context.Background(), cb, domain.ProviderDeezer)
	func() {
		defer func() {
			if recover() == nil {
				t.Error("panic was swallowed")
			}
		}()
		_, _ = RunAdmitted(context.Background(), c, BreakerOutcome,
			func() (int, error) { panic("boom") })
	}()
	cb.mu.Lock()
	failures := cb.getOrCreate(domain.ProviderDeezer).failures
	cb.mu.Unlock()
	if failures != 1 {
		t.Errorf("failures = %d, want exactly 1", failures)
	}
}
