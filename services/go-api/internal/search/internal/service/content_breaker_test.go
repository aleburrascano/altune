package service_test

import (
	"altune/go-api/internal/search/internal/adapters/memory"
	"altune/go-api/internal/search/internal/domain"
	"altune/go-api/internal/search/internal/service"
	"altune/go-api/internal/shared/clock"
	"context"
	"errors"
	"testing"
)

func TestRunAdmitted_SettlesWithMappedOutcome(t *testing.T) {
	cb := service.NewCircuitBreaker(clock.Now)
	c, ok := service.AdmitProviderCall(context.Background(), cb, domain.ProviderDeezer)
	if !ok {
		t.Fatal("fresh breaker refused a call")
	}
	_, err := service.RunAdmitted(context.Background(), c,
		func(error) error { return nil },
		func() (int, error) { return 1, memory.ErrUpstreamDown })
	if !errors.Is(err, memory.ErrUpstreamDown) {
		t.Fatalf("err = %v, want the call's own error returned unmapped", err)
	}
	for i := 0; i < 10; i++ {
		if _, ok := cb.AllowRequest(context.Background(), domain.ProviderDeezer); !ok {
			t.Fatal("mapper returned nil, so failures must not be recorded")
		}
	}
}

func TestRunAdmitted_PanicRecordsOneFailureAndPropagates(t *testing.T) {
	cb := service.NewCircuitBreaker(clock.Now)
	c, _ := service.AdmitProviderCall(context.Background(), cb, domain.ProviderDeezer)
	func() {
		defer func() {
			if recover() == nil {
				t.Error("panic was swallowed")
			}
		}()
		_, _ = service.RunAdmitted(context.Background(), c, service.BreakerOutcome,
			func() (int, error) { panic("boom") })
	}()
	if failures := cb.Snapshot(domain.ProviderDeezer).Failures; failures != 1 {
		t.Errorf("failures = %d, want exactly 1", failures)
	}
}
