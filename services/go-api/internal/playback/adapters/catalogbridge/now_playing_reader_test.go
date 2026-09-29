package catalogbridge

import (
	catalogPorts "altune/go-api/internal/catalog/ports"
	"altune/go-api/internal/shared"
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"testing"
	"time"

	catalogDomain "altune/go-api/internal/catalog/domain"

	"github.com/google/uuid"
)

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

type failingTrackReader struct{}

func (failingTrackReader) GetByID(_ context.Context, _ catalogDomain.TrackId, _ shared.UserId) (*catalogDomain.Track, error) {
	return nil, fmt.Errorf("catalog unavailable: %w", catalogPorts.ErrDBTransient)
}

type recordingMetrics struct {
	enrichmentFailed         int
	nowPlayingLookupTimedOut int
	breakerRejected          int
	breakerOpened            int
	breakerClosed            int
}

func (m *recordingMetrics) EnrichmentFailed()          { m.enrichmentFailed++ }
func (m *recordingMetrics) NowPlayingLookupTimedOut()  { m.nowPlayingLookupTimedOut++ }
func (m *recordingMetrics) EnrichmentBreakerRejected() { m.breakerRejected++ }
func (m *recordingMetrics) EnrichmentBreakerOpened()   { m.breakerOpened++ }
func (m *recordingMetrics) EnrichmentBreakerClosed()   { m.breakerClosed++ }

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

func TestLookup_ClientCancellation_RecordsNoMetric(t *testing.T) {
	m := &recordingMetrics{}
	reader := NewNowPlayingReader(&blockingTrackReader{}, WithNowPlayingMetrics(m))

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

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

func TestLookup_FastFailsAfterSustainedCatalogOutage(t *testing.T) {
	prev := nowPlayingLookupTimeout
	nowPlayingLookupTimeout = 50 * time.Millisecond
	defer func() { nowPlayingLookupTimeout = prev }()

	reader := NewNowPlayingReader(&blockingTrackReader{})
	user := testUser()

	for i := 0; i < enrichmentFailureThreshold; i++ {
		if _, err := reader.Lookup(context.Background(), user, uuid.New().String()); err == nil {
			t.Fatalf("call %d: expected a catalog-outage error", i)
		}
	}

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

	catalog.healthy = true
	clock = clock.Add(enrichmentOpenDuration + time.Second)

	if _, err := reader.Lookup(context.Background(), user, uuid.New().String()); err != nil {
		t.Fatalf("expected recovery once catalog healed, got err = %v", err)
	}
	if _, err := reader.Lookup(context.Background(), user, uuid.New().String()); err != nil {
		t.Fatalf("expected breaker closed after recovery, got err = %v", err)
	}
}

func tripBreakerOpen(t *testing.T, reader *NowPlayingReader, user shared.UserId) (advanceClock func(time.Duration)) {
	t.Helper()
	clock := time.Now()
	reader.breaker.now = func() time.Time { return clock }
	for i := 0; i < enrichmentFailureThreshold; i++ {
		if _, err := reader.Lookup(context.Background(), user, uuid.New().String()); err == nil {
			t.Fatalf("call %d: expected a catalog-outage error", i)
		}
	}
	return func(d time.Duration) { clock = clock.Add(d) }
}

func TestLookup_BreakerRejection_CountsApartFromFailures(t *testing.T) {
	m := &recordingMetrics{}
	reader := NewNowPlayingReader(failingTrackReader{}, WithNowPlayingMetrics(m))
	user := testUser()
	tripBreakerOpen(t, reader, user)

	for i := 0; i < 2; i++ {
		if _, err := reader.Lookup(context.Background(), user, uuid.New().String()); !errors.Is(err, errEnrichmentUnavailable) {
			t.Fatalf("rejected call %d: err = %v, want errEnrichmentUnavailable", i, err)
		}
	}

	if m.breakerRejected != 2 {
		t.Errorf("EnrichmentBreakerRejected = %d, want 2 after two fast-failed calls", m.breakerRejected)
	}
	if m.enrichmentFailed != enrichmentFailureThreshold {
		t.Errorf("EnrichmentFailed = %d, want %d: a fast-fail is not a dependency failure",
			m.enrichmentFailed, enrichmentFailureThreshold)
	}
}

func TestLookup_BreakerStateChangesAreReported(t *testing.T) {
	m := &recordingMetrics{}
	reader := NewNowPlayingReader(failingTrackReader{}, WithNowPlayingMetrics(m))
	user := testUser()
	advanceClock := tripBreakerOpen(t, reader, user)

	if m.breakerOpened != 1 || m.breakerClosed != 0 {
		t.Fatalf("after the outage: opened = %d, closed = %d; want 1/0", m.breakerOpened, m.breakerClosed)
	}

	reader.tracks = &recoveringTrackReader{healthy: true}
	advanceClock(enrichmentOpenDuration + time.Second)

	if _, err := reader.Lookup(context.Background(), user, uuid.New().String()); err != nil {
		t.Fatalf("expected the recovery probe to succeed, got err = %v", err)
	}
	if m.breakerClosed != 1 {
		t.Errorf("EnrichmentBreakerClosed = %d, want 1 once the catalog recovered", m.breakerClosed)
	}
	if m.breakerOpened != 1 {
		t.Errorf("EnrichmentBreakerOpened = %d, want 1: recovery must not re-open the breaker", m.breakerOpened)
	}
}

type clientVanishingTrackReader struct {
	disconnectClient context.CancelFunc
}

func (r *clientVanishingTrackReader) GetByID(ctx context.Context, _ catalogDomain.TrackId, _ shared.UserId) (*catalogDomain.Track, error) {
	r.disconnectClient()
	<-ctx.Done()
	return nil, ctx.Err()
}

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

	clock = clock.Add(enrichmentOpenDuration + time.Second)
	clientCtx, disconnect := context.WithCancel(context.Background())
	defer disconnect()
	reader.tracks = &clientVanishingTrackReader{disconnectClient: disconnect}
	if _, err := reader.Lookup(clientCtx, user, uuid.New().String()); !errors.Is(err, context.Canceled) {
		t.Fatalf("precondition: the abandoned probe must fail with its client's cancellation, got err = %v", err)
	}

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

func TestLookup_ClientCancellationDoesNotTripBreaker(t *testing.T) {
	reader := NewNowPlayingReader(&blockingTrackReader{})
	user := testUser()

	for i := 0; i < enrichmentFailureThreshold+2; i++ {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		if _, err := reader.Lookup(ctx, user, uuid.New().String()); err == nil {
			t.Fatalf("call %d: expected a canceled-context error", i)
		}
	}

	if admitted, _ := reader.breaker.allow(); !admitted {
		t.Fatal("client cancellations must not trip the enrichment breaker")
	}
}

type permanentRowErrorReader struct{}

func (permanentRowErrorReader) GetByID(_ context.Context, _ catalogDomain.TrackId, _ shared.UserId) (*catalogDomain.Track, error) {
	return nil, errors.New("acquisition status no longer parses")
}

func TestLookup_PermanentRowErrorsLeaveBreakerClosed(t *testing.T) {
	m := &recordingMetrics{}
	reader := NewNowPlayingReader(permanentRowErrorReader{}, WithNowPlayingMetrics(m))
	user := testUser()

	for i := 0; i < enrichmentFailureThreshold; i++ {
		if _, err := reader.Lookup(context.Background(), user, uuid.New().String()); err == nil {
			t.Fatalf("call %d: expected the row error to surface", i)
		}
	}

	if admitted, _ := reader.breaker.allow(); !admitted {
		t.Fatal("permanent per-row errors must not open the breaker for every user")
	}
	if m.enrichmentFailed != enrichmentFailureThreshold {
		t.Errorf("EnrichmentFailed = %d, want %d: row errors still count as failed lookups",
			m.enrichmentFailed, enrichmentFailureThreshold)
	}
}

type staleSuccessReader struct {
	firstCallAdmitted chan struct{}
	releaseFirstCall  chan struct{}
	calls             int
}

func (r *staleSuccessReader) GetByID(_ context.Context, _ catalogDomain.TrackId, _ shared.UserId) (*catalogDomain.Track, error) {
	r.calls++
	if r.calls == 1 {
		close(r.firstCallAdmitted)
		<-r.releaseFirstCall
		return nil, nil
	}
	return nil, catalogPorts.ErrDBTransient
}

func TestLookup_StaleSuccessDoesNotCloseAnOpenBreaker(t *testing.T) {
	catalog := &staleSuccessReader{firstCallAdmitted: make(chan struct{}), releaseFirstCall: make(chan struct{})}
	reader := NewNowPlayingReader(catalog)
	user := testUser()

	firstDone := make(chan error)
	go func() {
		_, err := reader.Lookup(context.Background(), user, uuid.New().String())
		firstDone <- err
	}()
	<-catalog.firstCallAdmitted

	for i := 0; i < enrichmentFailureThreshold; i++ {
		_, _ = reader.Lookup(context.Background(), user, uuid.New().String())
	}
	close(catalog.releaseFirstCall)
	if err := <-firstDone; err != nil {
		t.Fatalf("first call: %v", err)
	}

	if _, err := reader.Lookup(context.Background(), user, uuid.New().String()); !errors.Is(err, errEnrichmentUnavailable) {
		t.Fatalf("a stale success closed the open breaker without a probe, got err = %v", err)
	}
}
