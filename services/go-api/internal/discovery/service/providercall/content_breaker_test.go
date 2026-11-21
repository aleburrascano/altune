package providercall_test

import (
	"altune/go-api/internal/discovery/discoverytest"
	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/discovery/service/providercall"
	"context"
	"errors"
	"testing"
)

func TestRunAdmitted_SettlesWithMappedOutcome(t *testing.T) {
	cb := providercall.NewCircuitBreaker()
	c, ok := providercall.AdmitProviderCall(context.Background(), cb, domain.ProviderDeezer)
	if !ok {
		t.Fatal("fresh breaker refused a call")
	}
	_, err := providercall.RunAdmitted(context.Background(), c,
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
	cb := providercall.NewCircuitBreaker()
	c, _ := providercall.AdmitProviderCall(context.Background(), cb, domain.ProviderDeezer)
	func() {
		defer func() {
			if recover() == nil {
				t.Error("panic was swallowed")
			}
		}()
		_, _ = providercall.RunAdmitted(context.Background(), c, providercall.BreakerOutcome,
			func() (int, error) { panic("boom") })
	}()
	if failures := cb.Snapshot(domain.ProviderDeezer).Failures; failures != 1 {
		t.Errorf("failures = %d, want exactly 1", failures)
	}
}
