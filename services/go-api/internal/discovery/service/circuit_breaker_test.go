package service

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"altune/go-api/internal/discovery/domain"
)

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
