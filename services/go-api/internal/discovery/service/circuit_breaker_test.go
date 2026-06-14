package service

import (
	"testing"
	"time"

	"altune/go-api/internal/discovery/domain"
)

func TestCircuitBreaker_Closed(t *testing.T) {
	// Arrange
	cb := NewCircuitBreaker()

	// Act + Assert: new circuit breaker allows requests for any provider
	if !cb.AllowRequest(domain.ProviderDeezer) {
		t.Error("expected closed circuit to allow requests")
	}
	if cb.GetStatus(domain.ProviderDeezer) != domain.ProviderStatusOK {
		t.Errorf("expected status OK, got %v", cb.GetStatus(domain.ProviderDeezer))
	}
}

func TestCircuitBreaker_StaysClosedBelowThreshold(t *testing.T) {
	cb := NewCircuitBreaker()

	// Record 4 failures (below threshold of 5)
	for i := 0; i < 4; i++ {
		cb.RecordFailure(domain.ProviderDeezer)
	}

	if !cb.AllowRequest(domain.ProviderDeezer) {
		t.Error("expected circuit to stay closed with fewer than 5 failures")
	}
}

func TestCircuitBreaker_OpensAfterThreshold(t *testing.T) {
	cb := NewCircuitBreaker()

	// Record exactly 5 failures
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

	// Open the circuit
	for i := 0; i < 5; i++ {
		cb.RecordFailure(domain.ProviderDeezer)
	}

	// Verify it's blocked
	if cb.AllowRequest(domain.ProviderDeezer) {
		t.Fatal("expected circuit to be open")
	}

	// Manipulate lastFailedAt to simulate timeout passing.
	// We access the internal state directly since this is a white-box unit test.
	cb.mu.Lock()
	entry := cb.circuits[domain.ProviderDeezer]
	entry.lastFailedAt = time.Now().Add(-31 * time.Second)
	cb.mu.Unlock()

	// After timeout, the circuit should allow exactly one probe request
	if !cb.AllowRequest(domain.ProviderDeezer) {
		t.Error("expected half-open circuit to allow probe request after timeout")
	}
}

func TestCircuitBreaker_ResetsOnSuccess(t *testing.T) {
	cb := NewCircuitBreaker()

	// Open the circuit
	for i := 0; i < 5; i++ {
		cb.RecordFailure(domain.ProviderDeezer)
	}

	// Transition to half-open
	cb.mu.Lock()
	entry := cb.circuits[domain.ProviderDeezer]
	entry.lastFailedAt = time.Now().Add(-31 * time.Second)
	cb.mu.Unlock()

	// Trigger half-open via AllowRequest
	cb.AllowRequest(domain.ProviderDeezer)

	// Record success: should transition from half-open to closed
	cb.RecordSuccess(domain.ProviderDeezer)

	// Verify it's back to closed: allows requests and status is OK
	if !cb.AllowRequest(domain.ProviderDeezer) {
		t.Error("expected circuit to be closed after success in half-open state")
	}
	if cb.GetStatus(domain.ProviderDeezer) != domain.ProviderStatusOK {
		t.Errorf("expected status OK after reset, got %v", cb.GetStatus(domain.ProviderDeezer))
	}

	// Verify failure counter was reset: 4 more failures should not open it
	for i := 0; i < 4; i++ {
		cb.RecordFailure(domain.ProviderDeezer)
	}
	if !cb.AllowRequest(domain.ProviderDeezer) {
		t.Error("expected failure counter to have been reset; 4 failures should not open circuit")
	}
}

func TestCircuitBreaker_IndependentProviders(t *testing.T) {
	cb := NewCircuitBreaker()

	// Open circuit for Deezer
	for i := 0; i < 5; i++ {
		cb.RecordFailure(domain.ProviderDeezer)
	}

	// Deezer should be blocked
	if cb.AllowRequest(domain.ProviderDeezer) {
		t.Error("expected Deezer circuit to be open")
	}

	// MusicBrainz should still work
	if !cb.AllowRequest(domain.ProviderMusicBrainz) {
		t.Error("expected MusicBrainz circuit to be independent and closed")
	}

	// SoundCloud should still work
	if !cb.AllowRequest(domain.ProviderSoundCloud) {
		t.Error("expected SoundCloud circuit to be independent and closed")
	}

	// Verify statuses are independent
	if cb.GetStatus(domain.ProviderDeezer) != domain.ProviderStatusCircuitOpen {
		t.Errorf("expected Deezer status CircuitOpen, got %v", cb.GetStatus(domain.ProviderDeezer))
	}
	if cb.GetStatus(domain.ProviderMusicBrainz) != domain.ProviderStatusOK {
		t.Errorf("expected MusicBrainz status OK, got %v", cb.GetStatus(domain.ProviderMusicBrainz))
	}
}

func TestCircuitBreaker_FailureAfterHalfOpenReopens(t *testing.T) {
	cb := NewCircuitBreaker()

	// Open the circuit
	for i := 0; i < 5; i++ {
		cb.RecordFailure(domain.ProviderDeezer)
	}

	// Transition to half-open
	cb.mu.Lock()
	entry := cb.circuits[domain.ProviderDeezer]
	entry.lastFailedAt = time.Now().Add(-31 * time.Second)
	cb.mu.Unlock()

	cb.AllowRequest(domain.ProviderDeezer) // triggers half-open

	// Failure in half-open should re-open the circuit
	cb.RecordFailure(domain.ProviderDeezer)

	if cb.AllowRequest(domain.ProviderDeezer) {
		t.Error("expected circuit to re-open after failure in half-open state")
	}
}
