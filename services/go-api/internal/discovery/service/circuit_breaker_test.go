package service

import (
	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/discovery/ports"
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// tripToHalfOpenWindow opens the breaker for provider and ages its last failure
// past openDuration, so the next AllowRequest admits the half-open probe.
func tripToHalfOpenWindow(cb *CircuitBreaker, provider domain.ProviderName) {
	for i := 0; i < failureThreshold; i++ {
		cb.RecordFailure(provider)
	}
	cb.mu.Lock()
	cb.getOrCreate(provider).lastFailedAt = time.Now().Add(-openDuration - time.Second)
	cb.mu.Unlock()
}

// A half-open probe whose caller's context is canceled mid-flight must not
// leave the provider blackholed: the breaker has to admit a new probe.
func TestFanOut_CanceledHalfOpenProbeDoesNotBlackholeProvider(t *testing.T) {
	cb := NewCircuitBreaker()
	tripToHalfOpenWindow(cb, domain.ProviderDeezer)

	p := &fakeProvider{
		name:    domain.ProviderDeezer,
		delay:   time.Minute,
		results: []domain.SearchResult{deezerTrack("Humble", "Kendrick Lamar", 80)},
	}
	svc := NewService([]ports.SearchProvider{p}, cb)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		svc.fanOut(ctx, "humble", nil)
	}()

	// Wait until the probe has been admitted and is in flight, then cancel it.
	deadline := time.Now().Add(2 * time.Second)
	for {
		cb.mu.Lock()
		probing := cb.getOrCreate(domain.ProviderDeezer).probing
		cb.mu.Unlock()
		if probing {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("half-open probe was never admitted")
		}
		time.Sleep(time.Millisecond)
	}
	cancel()
	<-done

	if !cb.AllowRequest(domain.ProviderDeezer) {
		t.Fatal("provider blackholed: AllowRequest = false after the half-open probe was canceled, want a new probe admitted")
	}
	// The client cancellation is not evidence of provider failure: the breaker
	// must not have re-opened on it.
	if got := cb.GetStatus(domain.ProviderDeezer); got != domain.ProviderStatusOK {
		t.Errorf("status after canceled probe = %v, want ok (half-open, not re-opened)", got)
	}
}

func TestCircuitBreaker_Closed(t *testing.T) {
	cb := NewCircuitBreaker()

	if !cb.AllowRequest(domain.ProviderDeezer) {
		t.Error("expected closed circuit to allow requests")
	}
	if cb.GetStatus(domain.ProviderDeezer) != domain.ProviderStatusOK {
		t.Errorf("expected status OK, got %v", cb.GetStatus(domain.ProviderDeezer))
	}
}

func TestCircuitBreaker_StaysClosedBelowThreshold(t *testing.T) {
	cb := NewCircuitBreaker()

	for i := 0; i < 4; i++ {
		cb.RecordFailure(domain.ProviderDeezer)
	}

	if !cb.AllowRequest(domain.ProviderDeezer) {
		t.Error("expected circuit to stay closed with fewer than 5 failures")
	}
}

func TestCircuitBreaker_OpensAfterThreshold(t *testing.T) {
	cb := NewCircuitBreaker()

	for i := 0; i < 5; i++ {
		cb.RecordFailure(domain.ProviderDeezer)
	}

	if cb.AllowRequest(domain.ProviderDeezer) {
		t.Error("expected open circuit to block requests after 5 failures")
	}
	if cb.GetStatus(domain.ProviderDeezer) != domain.ProviderStatusCircuitOpen {
		t.Errorf("expected status CircuitOpen, got %v", cb.GetStatus(domain.ProviderDeezer))
	}
}

func TestCircuitBreaker_HalfOpen(t *testing.T) {
	cb := NewCircuitBreaker()

	for i := 0; i < 5; i++ {
		cb.RecordFailure(domain.ProviderDeezer)
	}

	if cb.AllowRequest(domain.ProviderDeezer) {
		t.Fatal("expected circuit to be open")
	}

	cb.mu.Lock()
	entry := cb.circuits[domain.ProviderDeezer]
	entry.lastFailedAt = time.Now().Add(-31 * time.Second)
	cb.mu.Unlock()

	if !cb.AllowRequest(domain.ProviderDeezer) {
		t.Error("expected half-open circuit to allow probe request after timeout")
	}
}

func TestCircuitBreaker_ResetsOnSuccess(t *testing.T) {
	cb := NewCircuitBreaker()

	for i := 0; i < 5; i++ {
		cb.RecordFailure(domain.ProviderDeezer)
	}

	cb.mu.Lock()
	entry := cb.circuits[domain.ProviderDeezer]
	entry.lastFailedAt = time.Now().Add(-31 * time.Second)
	cb.mu.Unlock()

	cb.AllowRequest(domain.ProviderDeezer)

	cb.RecordSuccess(domain.ProviderDeezer)

	if !cb.AllowRequest(domain.ProviderDeezer) {
		t.Error("expected circuit to be closed after success in half-open state")
	}
	if cb.GetStatus(domain.ProviderDeezer) != domain.ProviderStatusOK {
		t.Errorf("expected status OK after reset, got %v", cb.GetStatus(domain.ProviderDeezer))
	}

	for i := 0; i < 4; i++ {
		cb.RecordFailure(domain.ProviderDeezer)
	}
	if !cb.AllowRequest(domain.ProviderDeezer) {
		t.Error("expected failure counter to have been reset; 4 failures should not open circuit")
	}
}

func TestCircuitBreaker_IndependentProviders(t *testing.T) {
	cb := NewCircuitBreaker()

	for i := 0; i < 5; i++ {
		cb.RecordFailure(domain.ProviderDeezer)
	}

	if cb.AllowRequest(domain.ProviderDeezer) {
		t.Error("expected Deezer circuit to be open")
	}

	if !cb.AllowRequest(domain.ProviderMusicBrainz) {
		t.Error("expected MusicBrainz circuit to be independent and closed")
	}

	if !cb.AllowRequest(domain.ProviderSoundCloud) {
		t.Error("expected SoundCloud circuit to be independent and closed")
	}

	if cb.GetStatus(domain.ProviderDeezer) != domain.ProviderStatusCircuitOpen {
		t.Errorf("expected Deezer status CircuitOpen, got %v", cb.GetStatus(domain.ProviderDeezer))
	}
	if cb.GetStatus(domain.ProviderMusicBrainz) != domain.ProviderStatusOK {
		t.Errorf("expected MusicBrainz status OK, got %v", cb.GetStatus(domain.ProviderMusicBrainz))
	}
}

func TestCircuitBreaker_HalfOpenAdmitsExactlyOneConcurrentProbe(t *testing.T) {
	cb := NewCircuitBreaker()

	for i := 0; i < 5; i++ {
		cb.RecordFailure(domain.ProviderDeezer)
	}
	cb.mu.Lock()
	cb.circuits[domain.ProviderDeezer].lastFailedAt = time.Now().Add(-31 * time.Second)
	cb.mu.Unlock()

	const n = 16
	var wg sync.WaitGroup
	var admitted int32
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if cb.AllowRequest(domain.ProviderDeezer) {
				atomic.AddInt32(&admitted, 1)
			}
		}()
	}
	wg.Wait()

	if admitted != 1 {
		t.Errorf("half-open admitted %d concurrent probes, want exactly 1", admitted)
	}
}

func TestCircuitBreaker_FailureAfterHalfOpenReopens(t *testing.T) {
	cb := NewCircuitBreaker()

	for i := 0; i < 5; i++ {
		cb.RecordFailure(domain.ProviderDeezer)
	}

	cb.mu.Lock()
	entry := cb.circuits[domain.ProviderDeezer]
	entry.lastFailedAt = time.Now().Add(-31 * time.Second)
	cb.mu.Unlock()

	cb.AllowRequest(domain.ProviderDeezer)

	cb.RecordFailure(domain.ProviderDeezer)

	if cb.AllowRequest(domain.ProviderDeezer) {
		t.Error("expected circuit to re-open after failure in half-open state")
	}
}

func TestCircuitBreaker_ReleaseProbeAdmitsNextProbe(t *testing.T) {
	cb := NewCircuitBreaker()
	tripToHalfOpenWindow(cb, domain.ProviderDeezer)

	if !cb.AllowRequest(domain.ProviderDeezer) {
		t.Fatal("expected the half-open probe to be admitted")
	}
	if cb.AllowRequest(domain.ProviderDeezer) {
		t.Fatal("expected a second request to be rejected while the probe is in flight")
	}

	cb.ReleaseProbe(domain.ProviderDeezer)

	if !cb.AllowRequest(domain.ProviderDeezer) {
		t.Error("expected a released probe slot to admit the next probe")
	}
	if cb.AllowRequest(domain.ProviderDeezer) {
		t.Error("expected the re-admitted probe to still be exclusive")
	}
}

func TestCircuitBreaker_ReleaseProbeIsNoOpOutsideHalfOpen(t *testing.T) {
	cb := NewCircuitBreaker()
	for i := 0; i < failureThreshold; i++ {
		cb.RecordFailure(domain.ProviderDeezer)
	}

	cb.ReleaseProbe(domain.ProviderDeezer)
	if cb.AllowRequest(domain.ProviderDeezer) {
		t.Error("ReleaseProbe must not let requests through an open circuit")
	}

	cb.ReleaseProbe(domain.ProviderITunes)
	if !cb.AllowRequest(domain.ProviderITunes) {
		t.Error("ReleaseProbe must not affect a closed circuit")
	}
}

// Backstop: a probe never resolved by any Record*/ReleaseProbe call (e.g. a
// provider that ignores its context and hangs) expires after probeLease, so
// the breaker cannot stay half-open-and-probing forever.
func TestCircuitBreaker_AbandonedProbeLeaseExpires(t *testing.T) {
	cb := NewCircuitBreaker()
	tripToHalfOpenWindow(cb, domain.ProviderDeezer)

	if !cb.AllowRequest(domain.ProviderDeezer) {
		t.Fatal("expected the half-open probe to be admitted")
	}
	if cb.AllowRequest(domain.ProviderDeezer) {
		t.Fatal("expected rejection while the probe lease is live")
	}

	cb.mu.Lock()
	cb.getOrCreate(domain.ProviderDeezer).probeStartedAt = time.Now().Add(-probeLease - time.Second)
	cb.mu.Unlock()

	if !cb.AllowRequest(domain.ProviderDeezer) {
		t.Fatal("expected an expired probe lease to admit a new probe")
	}
	if cb.AllowRequest(domain.ProviderDeezer) {
		t.Error("expected the new probe to hold a fresh lease")
	}
}

func TestCircuitBreaker_FullStateWalkUnderConcurrency(t *testing.T) {
	cb := NewCircuitBreaker()
	p := domain.ProviderDeezer

	hammer := func(n int, f func()) {
		var wg sync.WaitGroup
		for i := 0; i < n; i++ {
			wg.Add(1)
			go func() { defer wg.Done(); f() }()
		}
		wg.Wait()
	}

	hammer(16, func() {
		if !cb.AllowRequest(p) {
			t.Error("closed breaker must allow requests")
		}
	})

	hammer(failureThreshold, func() { cb.RecordFailure(p) })
	if cb.AllowRequest(p) {
		t.Fatal("breaker must be open after the failure threshold")
	}
	if cb.GetStatus(p) != domain.ProviderStatusCircuitOpen {
		t.Fatalf("status = %v, want circuit_open", cb.GetStatus(p))
	}

	cb.mu.Lock()
	cb.circuits[p].lastFailedAt = time.Now().Add(-openDuration - time.Second)
	cb.mu.Unlock()
	var admitted int32
	var mu sync.Mutex
	hammer(16, func() {
		if cb.AllowRequest(p) {
			mu.Lock()
			admitted++
			mu.Unlock()
		}
	})
	if admitted != 1 {
		t.Fatalf("half-open admitted %d probes, want exactly 1", admitted)
	}
	if cb.GetStatus(p) != domain.ProviderStatusOK {
		t.Errorf("half-open status = %v, want ok", cb.GetStatus(p))
	}

	cb.RecordSuccess(p)
	hammer(16, func() {
		if !cb.AllowRequest(p) {
			t.Error("re-closed breaker must allow requests")
		}
	})

	hammer(failureThreshold, func() { cb.RecordFailure(p) })
	if cb.AllowRequest(p) {
		t.Fatal("breaker must re-open after a fresh failure threshold")
	}

	cb.mu.Lock()
	cb.circuits[p].lastFailedAt = time.Now().Add(-openDuration - time.Second)
	cb.mu.Unlock()
	if !cb.AllowRequest(p) {
		t.Fatal("aged-open breaker must admit the probe")
	}
	cb.RecordFailure(p)
	if cb.AllowRequest(p) {
		t.Error("a failed half-open probe must re-open the breaker immediately")
	}
}
