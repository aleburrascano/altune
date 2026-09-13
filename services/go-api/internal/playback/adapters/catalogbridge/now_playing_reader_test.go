package catalogbridge

import (
	"context"
	"errors"
	"testing"
	"time"

	catalogDomain "altune/go-api/internal/catalog/domain"
	"altune/go-api/internal/shared"

	"github.com/google/uuid"
)

type blockingTrackReader struct {
	entered chan struct{}
}

func (b *blockingTrackReader) GetByID(ctx context.Context, _ catalogDomain.TrackId, _ shared.UserId) (*catalogDomain.Track, error) {
	if b.entered != nil {
		close(b.entered)
	}
	<-ctx.Done()
	return nil, ctx.Err()
}

func testUser() shared.UserId {
	return shared.NewUserId(uuid.New())
}

func TestLookup_DerivesDeadlineWhenDependencyBlocks(t *testing.T) {
	prev := nowPlayingLookupTimeout
	nowPlayingLookupTimeout = 50 * time.Millisecond
	defer func() { nowPlayingLookupTimeout = prev }()

	reader := NewNowPlayingReader(&blockingTrackReader{entered: make(chan struct{})})

	done := make(chan error, 1)
	go func() {
		_, err := reader.Lookup(context.Background(), testUser(), uuid.New().String())
		done <- err
	}()

	select {
	case err := <-done:
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("err = %v, want context.DeadlineExceeded", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Lookup did not return; no per-call deadline was derived from the request context")
	}
}

// recoveringTrackReader blocks until the per-call deadline (simulating a
// catalog outage) until it is flipped healthy, after which it returns cleanly.
type recoveringTrackReader struct {
	healthy bool
}

func (r *recoveringTrackReader) GetByID(ctx context.Context, _ catalogDomain.TrackId, _ shared.UserId) (*catalogDomain.Track, error) {
	if r.healthy {
		return nil, nil
	}
	<-ctx.Done()
	return nil, ctx.Err()
}

// TestLookup_FastFailsAfterSustainedCatalogOutage reproduces the defect: before
// the breaker, every call during a catalog outage paid the full per-call
// timeout. Once a run of failures trips the breaker, further calls must
// short-circuit well under that timeout.
func TestLookup_FastFailsAfterSustainedCatalogOutage(t *testing.T) {
	prev := nowPlayingLookupTimeout
	nowPlayingLookupTimeout = 50 * time.Millisecond
	defer func() { nowPlayingLookupTimeout = prev }()

	reader := NewNowPlayingReader(&blockingTrackReader{})
	user := testUser()

	// Prime the breaker: each failing call pays the full per-call timeout.
	for i := 0; i < enrichmentFailureThreshold; i++ {
		if _, err := reader.Lookup(context.Background(), user, uuid.New().String()); err == nil {
			t.Fatalf("call %d: expected a catalog-outage error", i)
		}
	}

	// The breaker is now open: this call must fast-fail, not stall for 50ms.
	start := time.Now()
	_, err := reader.Lookup(context.Background(), user, uuid.New().String())
	elapsed := time.Since(start)

	if !errors.Is(err, errEnrichmentUnavailable) {
		t.Fatalf("err = %v, want errEnrichmentUnavailable (breaker open, fast-fail)", err)
	}
	if elapsed >= nowPlayingLookupTimeout {
		t.Fatalf("breaker-open call took %s; expected fast-fail well under the %s timeout", elapsed, nowPlayingLookupTimeout)
	}
}

// TestLookup_BreakerRecoversAfterCatalogHeals proves the fast-fail is temporary:
// after the open window elapses and the catalog recovers, a probe closes the
// breaker and normal lookups resume.
func TestLookup_BreakerRecoversAfterCatalogHeals(t *testing.T) {
	prev := nowPlayingLookupTimeout
	nowPlayingLookupTimeout = 50 * time.Millisecond
	defer func() { nowPlayingLookupTimeout = prev }()

	catalog := &recoveringTrackReader{healthy: false}
	reader := NewNowPlayingReader(catalog)
	clock := time.Now()
	reader.breaker.now = func() time.Time { return clock }
	user := testUser()

	for i := 0; i < enrichmentFailureThreshold; i++ {
		_, _ = reader.Lookup(context.Background(), user, uuid.New().String())
	}
	if _, err := reader.Lookup(context.Background(), user, uuid.New().String()); !errors.Is(err, errEnrichmentUnavailable) {
		t.Fatalf("precondition: breaker should be open, got err = %v", err)
	}

	// Catalog heals and the open window elapses; the next call is admitted.
	catalog.healthy = true
	clock = clock.Add(enrichmentOpenDuration + time.Second)

	if _, err := reader.Lookup(context.Background(), user, uuid.New().String()); err != nil {
		t.Fatalf("expected recovery once catalog healed, got err = %v", err)
	}
	// Breaker is closed again: a subsequent healthy call also succeeds.
	if _, err := reader.Lookup(context.Background(), user, uuid.New().String()); err != nil {
		t.Fatalf("expected breaker closed after recovery, got err = %v", err)
	}
}

// TestLookup_ClientCancellationDoesNotTripBreaker guards against a disconnecting
// client tripping the breaker against a healthy catalog.
func TestLookup_ClientCancellationDoesNotTripBreaker(t *testing.T) {
	reader := NewNowPlayingReader(&blockingTrackReader{})
	user := testUser()

	for i := 0; i < enrichmentFailureThreshold+2; i++ {
		ctx, cancel := context.WithCancel(context.Background())
		cancel() // client is already gone
		if _, err := reader.Lookup(ctx, user, uuid.New().String()); err == nil {
			t.Fatalf("call %d: expected a canceled-context error", i)
		}
	}

	if !reader.breaker.allow() {
		t.Fatal("client cancellations must not trip the enrichment breaker")
	}
}
