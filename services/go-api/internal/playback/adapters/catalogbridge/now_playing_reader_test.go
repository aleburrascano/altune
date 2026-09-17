package catalogbridge

import (
	"altune/go-api/internal/shared"
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	catalogDomain "altune/go-api/internal/catalog/domain"

	"github.com/google/uuid"
)

// captureLogs redirects the default slog logger to a buffer for the duration of
// the test, so a test can assert which structured log lines a code path emits.
func captureLogs(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(prev) })
	return &buf
}

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

// failingTrackReader returns a plain (non-timeout) dependency error, standing
// in for catalog rejecting the lookup rather than stalling.
type failingTrackReader struct{}

func (failingTrackReader) GetByID(_ context.Context, _ catalogDomain.TrackId, _ shared.UserId) (*catalogDomain.Track, error) {
	return nil, errors.New("catalog unavailable")
}

// recordingMetrics is a ports.EnrichmentMetrics double that counts each
// degradation signal, so a test can assert a counter fired on a failure path.
type recordingMetrics struct {
	enrichmentFailed         int
	nowPlayingLookupTimedOut int
}

func (m *recordingMetrics) EnrichmentFailed()         { m.enrichmentFailed++ }
func (m *recordingMetrics) NowPlayingLookupTimedOut() { m.nowPlayingLookupTimedOut++ }

// TestLookup_MalformedTrackId_EmitsDistinguishableSignal reproduces the defect:
// a malformed persisted track ID degrades to track-absent (nil, nil) exactly
// like an empty queue, but must now leave a distinguishable log signal carrying
// the owning user so an operator can tell a data defect from "nothing playing."
func TestLookup_MalformedTrackId_EmitsDistinguishableSignal(t *testing.T) {
	logs := captureLogs(t)
	reader := NewNowPlayingReader(&recoveringTrackReader{healthy: true})
	user := testUser()

	track, err := reader.Lookup(context.Background(), user, "not-a-uuid")
	if err != nil || track != nil {
		t.Fatalf("malformed id must degrade to track-absent, got track=%v err=%v", track, err)
	}

	out := logs.String()
	if !strings.Contains(out, "now_playing.malformed_track_id") {
		t.Fatalf("malformed track id emitted no distinguishable log signal; logs=%q", out)
	}
	if !strings.Contains(out, user.String()) {
		t.Fatalf("malformed-track-id log line omits user_id; logs=%q", out)
	}
}

// TestLookup_ValidButAbsentTrack_StaysSilent pins the contrast: the genuine
// "nothing playing" path (a well-formed ID the catalog does not know) must not
// emit the malformed-id signal, or the signal would be worthless.
func TestLookup_ValidButAbsentTrack_StaysSilent(t *testing.T) {
	logs := captureLogs(t)
	reader := NewNowPlayingReader(&recoveringTrackReader{healthy: true})

	if _, err := reader.Lookup(context.Background(), testUser(), uuid.New().String()); err != nil {
		t.Fatalf("valid but absent track must degrade cleanly: %v", err)
	}
	if strings.Contains(logs.String(), "malformed") {
		t.Fatalf("empty-queue path must not log a malformed-id signal; logs=%q", logs.String())
	}
}

// TestLookup_EnrichmentFailure_IncrementsMetric reproduces the missing health
// signal: a failed enrichment lookup degraded the resume but, before this
// change, incremented no counter — only a log line.
func TestLookup_EnrichmentFailure_IncrementsMetric(t *testing.T) {
	m := &recordingMetrics{}
	reader := NewNowPlayingReader(failingTrackReader{}, WithNowPlayingMetrics(m))

	if _, err := reader.Lookup(context.Background(), testUser(), uuid.New().String()); err == nil {
		t.Fatal("precondition: a failing catalog lookup must surface an error")
	}
	if m.enrichmentFailed != 1 {
		t.Fatalf("EnrichmentFailed counter = %d, want 1 after a failed lookup", m.enrichmentFailed)
	}
	if m.nowPlayingLookupTimedOut != 0 {
		t.Fatalf("NowPlayingLookupTimedOut = %d, want 0 for a non-timeout failure", m.nowPlayingLookupTimedOut)
	}
}

// TestLookup_Timeout_IncrementsBothMetrics proves a per-call timeout counts as
// both an enrichment failure and, more specifically, a lookup timeout.
func TestLookup_Timeout_IncrementsBothMetrics(t *testing.T) {
	prev := nowPlayingLookupTimeout
	nowPlayingLookupTimeout = 50 * time.Millisecond
	defer func() { nowPlayingLookupTimeout = prev }()

	m := &recordingMetrics{}
	reader := NewNowPlayingReader(&blockingTrackReader{}, WithNowPlayingMetrics(m))

	if _, err := reader.Lookup(context.Background(), testUser(), uuid.New().String()); err == nil {
		t.Fatal("precondition: a stalled catalog lookup must time out with an error")
	}
	if m.enrichmentFailed != 1 {
		t.Fatalf("EnrichmentFailed counter = %d, want 1 after a lookup timeout", m.enrichmentFailed)
	}
	if m.nowPlayingLookupTimedOut != 1 {
		t.Fatalf("NowPlayingLookupTimedOut counter = %d, want 1 after a lookup timeout", m.nowPlayingLookupTimedOut)
	}
}

// TestLookup_ClientCancellation_RecordsNoMetric guards against a disconnecting
// client inflating the catalog-health signal.
func TestLookup_ClientCancellation_RecordsNoMetric(t *testing.T) {
	m := &recordingMetrics{}
	reader := NewNowPlayingReader(&blockingTrackReader{}, WithNowPlayingMetrics(m))

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // client is already gone

	if _, err := reader.Lookup(ctx, testUser(), uuid.New().String()); err == nil {
		t.Fatal("precondition: a canceled context must surface an error")
	}
	if m.enrichmentFailed != 0 || m.nowPlayingLookupTimedOut != 0 {
		t.Fatalf("client cancel recorded metrics (enrichmentFailed=%d, lookupTimedOut=%d); want 0/0",
			m.enrichmentFailed, m.nowPlayingLookupTimedOut)
	}
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

// clientVanishingTrackReader stands in for the client disconnecting while the
// catalog call is in flight: the request's context is canceled underneath the
// call, which then reports that cancellation the way a context-aware repository
// does.
type clientVanishingTrackReader struct {
	disconnectClient context.CancelFunc
}

func (r *clientVanishingTrackReader) GetByID(ctx context.Context, _ catalogDomain.TrackId, _ shared.UserId) (*catalogDomain.Track, error) {
	r.disconnectClient()
	<-ctx.Done()
	return nil, ctx.Err()
}

// TestLookup_ProbeAbandonedByItsClientDoesNotWedgeBreaker reproduces the defect:
// the breaker admits exactly one recovery probe, and when that one request's
// client disconnected before the catalog answered, the probe reported no verdict
// and the slot was never freed — enrichment stayed dead process-wide for every
// user even once the catalog was healthy again.
func TestLookup_ProbeAbandonedByItsClientDoesNotWedgeBreaker(t *testing.T) {
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

	// The open window elapses, so this request is admitted as the single
	// recovery probe — and its client disconnects mid-call, so the probe learns
	// nothing about the catalog's health.
	clock = clock.Add(enrichmentOpenDuration + time.Second)
	clientCtx, disconnect := context.WithCancel(context.Background())
	defer disconnect()
	reader.tracks = &clientVanishingTrackReader{disconnectClient: disconnect}
	if _, err := reader.Lookup(clientCtx, user, uuid.New().String()); !errors.Is(err, context.Canceled) {
		t.Fatalf("precondition: the abandoned probe must fail with its client's cancellation, got err = %v", err)
	}

	// Catalog healthy, another open window elapsed: enrichment must resume.
	catalog.healthy = true
	reader.tracks = catalog
	clock = clock.Add(enrichmentOpenDuration + time.Second)

	if _, err := reader.Lookup(context.Background(), user, uuid.New().String()); err != nil {
		t.Fatalf("breaker wedged after a probe its client abandoned: %v", err)
	}
	if _, err := reader.Lookup(context.Background(), user, uuid.New().String()); err != nil {
		t.Fatalf("expected breaker closed after the recovery probe succeeded, got err = %v", err)
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

	if admitted, _ := reader.breaker.allow(); !admitted {
		t.Fatal("client cancellations must not trip the enrichment breaker")
	}
}
