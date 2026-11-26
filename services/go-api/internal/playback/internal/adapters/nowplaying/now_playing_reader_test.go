package nowplaying_test

import (
	"altune/go-api/internal/playback"
	"altune/go-api/internal/shared/ids"
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
)

const (
	enrichmentFailureThreshold = 5
	enrichmentOpenDuration     = 30 * time.Second
	enrichmentProbeLease       = 30 * time.Second
	testLookupTimeout          = 50 * time.Millisecond
)

func captureLogs(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(prev) })
	return &buf
}

func testUser() ids.UserId {
	return ids.NewUserId(uuid.New())
}

type sourceFunc func(ctx context.Context) (*playback.NowPlayingTrack, error)

func (f sourceFunc) NowPlayingTrack(ctx context.Context, _ ids.UserId, _ ids.TrackId) (*playback.NowPlayingTrack, error) {
	return f(ctx)
}

func blockingSource(entered chan struct{}) sourceFunc {
	return func(ctx context.Context) (*playback.NowPlayingTrack, error) {
		if entered != nil {
			close(entered)
		}
		<-ctx.Done()
		return nil, ctx.Err()
	}
}

func failingSource() sourceFunc {
	return func(context.Context) (*playback.NowPlayingTrack, error) {
		return nil, fmt.Errorf("catalog unavailable: %w", playback.ErrTrackSourceUnavailable)
	}
}

func absentSource() sourceFunc {
	return func(context.Context) (*playback.NowPlayingTrack, error) {
		return nil, playback.ErrNowPlayingTrackNotFound
	}
}

func healthySource() sourceFunc {
	return func(context.Context) (*playback.NowPlayingTrack, error) {
		return &playback.NowPlayingTrack{}, nil
	}
}

func permanentRowErrorSource() sourceFunc {
	return func(context.Context) (*playback.NowPlayingTrack, error) {
		return nil, errors.New("acquisition status no longer parses")
	}
}

type switchableSource struct {
	mu      sync.Mutex
	current playback.TrackSource
}

func (s *switchableSource) use(next playback.TrackSource) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.current = next
}

func (s *switchableSource) NowPlayingTrack(ctx context.Context, userId ids.UserId, id ids.TrackId) (*playback.NowPlayingTrack, error) {
	s.mu.Lock()
	current := s.current
	s.mu.Unlock()
	return current.NowPlayingTrack(ctx, userId, id)
}

type testClock struct {
	mu  sync.Mutex
	now time.Time
}

func newTestClock() *testClock { return &testClock{now: time.Now()} }

func (c *testClock) read() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *testClock) advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

type recordingMetrics struct {
	mu                       sync.Mutex
	enrichmentFailed         int
	nowPlayingLookupTimedOut int
	breakerRejected          int
	breakerOpened            int
	breakerClosed            int
}

func (m *recordingMetrics) count(field *int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	*field++
}

func (m *recordingMetrics) read(field *int) int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return *field
}

func (m *recordingMetrics) EnrichmentFailed()          { m.count(&m.enrichmentFailed) }
func (m *recordingMetrics) NowPlayingLookupTimedOut()  { m.count(&m.nowPlayingLookupTimedOut) }
func (m *recordingMetrics) EnrichmentBreakerRejected() { m.count(&m.breakerRejected) }
func (m *recordingMetrics) EnrichmentBreakerOpened()   { m.count(&m.breakerOpened) }
func (m *recordingMetrics) EnrichmentBreakerClosed()   { m.count(&m.breakerClosed) }

type nowPlayingLookup interface {
	Lookup(ctx context.Context, userId ids.UserId, trackId string) (*playback.NowPlayingTrack, error)
}

func lookup(ctx context.Context, reader nowPlayingLookup, user ids.UserId) (*playback.NowPlayingTrack, error) {
	return reader.Lookup(ctx, user, uuid.New().String())
}

func rejected(reader nowPlayingLookup, m *recordingMetrics, user ids.UserId) bool {
	before := m.read(&m.breakerRejected)
	_, _ = lookup(context.Background(), reader, user)
	return m.read(&m.breakerRejected) > before
}

func TestLookup_MalformedTrackId_EmitsDistinguishableSignal(t *testing.T) {
	logs := captureLogs(t)
	called := false
	reader := playback.MustNewNowPlayingReader(sourceFunc(func(context.Context) (*playback.NowPlayingTrack, error) {
		called = true
		return &playback.NowPlayingTrack{}, nil
	}), noopEnrichmentMetrics{}, time.Now, defaultLookupTimeout)
	user := testUser()

	track, err := reader.Lookup(context.Background(), user, "not-a-uuid")
	if !errors.Is(err, playback.ErrNowPlayingTrackNotFound) || track != nil {
		t.Fatalf("malformed id must degrade to track-absent, got track=%v err=%v", track, err)
	}
	if called {
		t.Error("a malformed id must not reach the track source")
	}

	out := logs.String()
	if !strings.Contains(out, "now_playing.malformed_track_id") {
		t.Fatalf("malformed track id emitted no distinguishable log signal; logs=%q", out)
	}
	if !strings.Contains(out, user.String()) {
		t.Fatalf("malformed-track-id log line omits user_id; logs=%q", out)
	}
}

const defaultLookupTimeout = 3 * time.Second

type noopEnrichmentMetrics struct{}

func (noopEnrichmentMetrics) EnrichmentFailed()          {}
func (noopEnrichmentMetrics) NowPlayingLookupTimedOut()  {}
func (noopEnrichmentMetrics) EnrichmentBreakerRejected() {}
func (noopEnrichmentMetrics) EnrichmentBreakerOpened()   {}
func (noopEnrichmentMetrics) EnrichmentBreakerClosed()   {}

func TestNewNowPlayingReader_RequiresItsMetricsClockAndTimeout(t *testing.T) {
	cases := map[string]struct {
		build func() error
		want  error
	}{
		"track source": {func() error {
			_, err := playback.NewNowPlayingReader(nil, noopEnrichmentMetrics{}, time.Now, defaultLookupTimeout)
			return err
		}, playback.ErrNowPlayingCollaboratorRequired},
		"metrics": {func() error {
			_, err := playback.NewNowPlayingReader(absentSource(), nil, time.Now, defaultLookupTimeout)
			return err
		}, playback.ErrNowPlayingCollaboratorRequired},
		"clock": {func() error {
			_, err := playback.NewNowPlayingReader(absentSource(), noopEnrichmentMetrics{}, nil, defaultLookupTimeout)
			return err
		}, playback.ErrNowPlayingClockRequired},
		"timeout": {func() error {
			_, err := playback.NewNowPlayingReader(absentSource(), noopEnrichmentMetrics{}, time.Now, 0)
			return err
		}, playback.ErrNowPlayingInvalidTimeout},
	}
	for name, tt := range cases {
		t.Run(name, func(t *testing.T) {
			err := tt.build()

			if !errors.Is(err, tt.want) {
				t.Fatalf("a missing %s = %v, want %v", name, err, tt.want)
			}
			if !strings.Contains(err.Error(), "NewNowPlayingReader") {
				t.Fatalf("error %q does not name the constructor", err)
			}
		})
	}
}

func TestMustNewNowPlayingReader_PanicsOnANilClock(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("MustNewNowPlayingReader accepted a nil clock")
		}
	}()

	playback.MustNewNowPlayingReader(absentSource(), noopEnrichmentMetrics{}, nil, defaultLookupTimeout)
}

func TestLookup_ValidButAbsentTrack_StaysSilent(t *testing.T) {
	logs := captureLogs(t)
	reader := playback.MustNewNowPlayingReader(absentSource(), noopEnrichmentMetrics{}, time.Now, defaultLookupTimeout)

	track, err := lookup(context.Background(), reader, testUser())
	if !errors.Is(err, playback.ErrNowPlayingTrackNotFound) || track != nil {
		t.Fatalf("valid but absent track must be reported as not found, got track=%v err=%v", track, err)
	}
	if strings.Contains(logs.String(), "malformed") {
		t.Fatalf("empty-queue path must not log a malformed-id signal; logs=%q", logs.String())
	}
}

func TestLookup_AbsentTrackIsNotAnEnrichmentFailureAndNeverTripsTheBreaker(t *testing.T) {
	m := &recordingMetrics{}
	reader := playback.MustNewNowPlayingReader(absentSource(), m, time.Now, defaultLookupTimeout)

	for range 20 {
		if _, err := lookup(context.Background(), reader, testUser()); !errors.Is(err, playback.ErrNowPlayingTrackNotFound) {
			t.Fatalf("Lookup error = %v, want ErrNowPlayingTrackNotFound every time", err)
		}
	}
	if m.enrichmentFailed != 0 || m.breakerOpened != 0 || m.breakerRejected != 0 {
		t.Errorf("failed=%d opened=%d rejected=%d, want none: an absent track is not an outage", m.enrichmentFailed, m.breakerOpened, m.breakerRejected)
	}
}

func TestLookup_ReturnsTheSourcesTrack(t *testing.T) {
	want := &playback.NowPlayingTrack{Id: uuid.NewString(), Title: "Track Y", Artist: "Artist", AcquisitionStatus: "ready"}
	reader := playback.MustNewNowPlayingReader(sourceFunc(func(context.Context) (*playback.NowPlayingTrack, error) { return want, nil }), noopEnrichmentMetrics{}, time.Now, defaultLookupTimeout)

	got, err := lookup(context.Background(), reader, testUser())
	if err != nil || got != want {
		t.Fatalf("Lookup = (%+v, %v), want the source's track", got, err)
	}
}

func TestLookup_PassesTheParsedIdAndUserToTheSource(t *testing.T) {
	user := testUser()
	id := uuid.New()
	var gotUser ids.UserId
	var gotId ids.TrackId
	reader := playback.MustNewNowPlayingReader(trackSourceFunc(func(userId ids.UserId, trackId ids.TrackId) {
		gotUser, gotId = userId, trackId
	}), noopEnrichmentMetrics{}, time.Now, defaultLookupTimeout)

	if _, err := reader.Lookup(context.Background(), user, strings.ToUpper(id.String())); err != nil {
		t.Fatalf("Lookup: %v", err)
	}
	if gotUser != user || gotId.String() != id.String() {
		t.Errorf("source asked for (%v, %v), want (%v, %v)", gotUser, gotId, user, id)
	}
}

type trackSourceFunc func(ids.UserId, ids.TrackId)

func (f trackSourceFunc) NowPlayingTrack(_ context.Context, userId ids.UserId, id ids.TrackId) (*playback.NowPlayingTrack, error) {
	f(userId, id)
	return &playback.NowPlayingTrack{}, nil
}

func TestLookup_EnrichmentFailure_IncrementsMetric(t *testing.T) {
	m := &recordingMetrics{}
	reader := playback.MustNewNowPlayingReader(failingSource(), m, time.Now, defaultLookupTimeout)

	if _, err := lookup(context.Background(), reader, testUser()); !errors.Is(err, playback.ErrTrackSourceUnavailable) {
		t.Fatalf("precondition: a failing catalog lookup must surface its error, got %v", err)
	}
	if m.enrichmentFailed != 1 {
		t.Fatalf("EnrichmentFailed counter = %d, want 1 after a failed lookup", m.enrichmentFailed)
	}
	if m.nowPlayingLookupTimedOut != 0 {
		t.Fatalf("NowPlayingLookupTimedOut = %d, want 0 for a non-timeout failure", m.nowPlayingLookupTimedOut)
	}
}

func TestLookup_Timeout_IncrementsBothMetrics(t *testing.T) {
	m := &recordingMetrics{}
	reader := playback.MustNewNowPlayingReader(blockingSource(nil), m, time.Now, testLookupTimeout)

	if _, err := lookup(context.Background(), reader, testUser()); err == nil {
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
	reader := playback.MustNewNowPlayingReader(blockingSource(nil), m, time.Now, testLookupTimeout)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := lookup(ctx, reader, testUser()); err == nil {
		t.Fatal("precondition: a canceled context must surface an error")
	}
	if m.enrichmentFailed != 0 || m.nowPlayingLookupTimedOut != 0 {
		t.Fatalf("client cancel recorded metrics (enrichmentFailed=%d, lookupTimedOut=%d); want 0/0",
			m.enrichmentFailed, m.nowPlayingLookupTimedOut)
	}
}

func TestLookup_DerivesDeadlineWhenDependencyBlocks(t *testing.T) {
	reader := playback.MustNewNowPlayingReader(blockingSource(make(chan struct{})), noopEnrichmentMetrics{}, time.Now, testLookupTimeout)

	done := make(chan error, 1)
	go func() {
		_, err := lookup(context.Background(), reader, testUser())
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

func TestLookup_DefaultDeadlineIsThreeSeconds(t *testing.T) {
	if testing.Short() {
		t.Skip("waits out the 3s production now-playing lookup deadline")
	}
	reader := playback.MustNewNowPlayingReader(blockingSource(nil), noopEnrichmentMetrics{}, time.Now, defaultLookupTimeout)

	start := time.Now()
	_, err := lookup(context.Background(), reader, testUser())
	elapsed := time.Since(start)

	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v, want context.DeadlineExceeded", err)
	}
	if elapsed < 3*time.Second || elapsed > 5*time.Second {
		t.Errorf("a stalled lookup gave up after %s, want the 3s production deadline", elapsed)
	}
}

func TestLookup_FastFailsAfterSustainedCatalogOutage(t *testing.T) {
	m := &recordingMetrics{}
	reader := playback.MustNewNowPlayingReader(blockingSource(nil), m, time.Now, testLookupTimeout)
	user := testUser()

	for i := 0; i < enrichmentFailureThreshold; i++ {
		if _, err := lookup(context.Background(), reader, user); err == nil {
			t.Fatalf("call %d: expected a catalog-outage error", i)
		}
	}

	start := time.Now()
	_, err := lookup(context.Background(), reader, user)
	elapsed := time.Since(start)

	if err == nil || m.read(&m.breakerRejected) != 1 {
		t.Fatalf("err = %v with %d rejections, want the open breaker to fast-fail the call", err, m.read(&m.breakerRejected))
	}
	if elapsed >= testLookupTimeout {
		t.Fatalf("breaker-open call took %s; expected fast-fail well under the %s timeout", elapsed, testLookupTimeout)
	}
}

func TestLookup_BreakerRecoversAfterCatalogHeals(t *testing.T) {
	m := &recordingMetrics{}
	clock := newTestClock()
	catalog := &switchableSource{current: blockingSource(nil)}
	reader := playback.MustNewNowPlayingReader(catalog, m, clock.read, testLookupTimeout)
	user := testUser()

	for i := 0; i < enrichmentFailureThreshold; i++ {
		_, _ = lookup(context.Background(), reader, user)
	}
	if !rejected(reader, m, user) {
		t.Fatal("precondition: breaker should be open")
	}

	catalog.use(healthySource())
	clock.advance(enrichmentOpenDuration + time.Second)

	if _, err := lookup(context.Background(), reader, user); err != nil {
		t.Fatalf("expected recovery once catalog healed, got err = %v", err)
	}
	if _, err := lookup(context.Background(), reader, user); err != nil {
		t.Fatalf("expected breaker closed after recovery, got err = %v", err)
	}
}

func TestLookup_AnAbsentTrackAsTheRecoveryProbeClosesTheBreaker(t *testing.T) {
	m := &recordingMetrics{}
	clock := newTestClock()
	catalog := &switchableSource{current: blockingSource(nil)}
	reader := playback.MustNewNowPlayingReader(catalog, m, clock.read, testLookupTimeout)
	user := testUser()

	for i := 0; i < enrichmentFailureThreshold; i++ {
		_, _ = lookup(context.Background(), reader, user)
	}
	if !rejected(reader, m, user) {
		t.Fatal("precondition: breaker should be open")
	}

	catalog.use(absentSource())
	clock.advance(enrichmentOpenDuration + time.Second)

	if _, err := lookup(context.Background(), reader, user); !errors.Is(err, playback.ErrNowPlayingTrackNotFound) {
		t.Fatalf("probe err = %v, want ErrNowPlayingTrackNotFound", err)
	}
	if m.breakerClosed != 1 {
		t.Fatalf("breakerClosed = %d, want 1: an absent track answers the probe, so the catalog is up", m.breakerClosed)
	}
	if rejected(reader, m, user) {
		t.Fatal("the next lookup was rejected: the breaker did not close")
	}
}

func TestLookup_AnAbsentTrackResetsTheConsecutiveFailureCount(t *testing.T) {
	m := &recordingMetrics{}
	catalog := &switchableSource{current: failingSource()}
	reader := playback.MustNewNowPlayingReader(catalog, m, time.Now, defaultLookupTimeout)
	user := testUser()

	for i := 0; i < enrichmentFailureThreshold-1; i++ {
		_, _ = lookup(context.Background(), reader, user)
	}
	catalog.use(absentSource())
	_, _ = lookup(context.Background(), reader, user)
	catalog.use(failingSource())
	_, _ = lookup(context.Background(), reader, user)

	if m.breakerOpened != 0 {
		t.Fatalf("breakerOpened = %d, want 0: the absent track in between ended the failure streak", m.breakerOpened)
	}
}

func TestLookup_BreakerStaysOpenForItsWholeWindow(t *testing.T) {
	m := &recordingMetrics{}
	clock := newTestClock()
	reader := playback.MustNewNowPlayingReader(failingSource(), m, clock.read, defaultLookupTimeout)
	user := testUser()
	tripBreakerOpen(t, reader, user)

	clock.advance(enrichmentOpenDuration)
	if !rejected(reader, m, user) {
		t.Fatalf("breaker admitted a call %s after it opened, want it open for longer than that", enrichmentOpenDuration)
	}
}

func tripBreakerOpen(t *testing.T, reader nowPlayingLookup, user ids.UserId) {
	t.Helper()
	for i := 0; i < enrichmentFailureThreshold; i++ {
		if _, err := lookup(context.Background(), reader, user); err == nil {
			t.Fatalf("call %d: expected a catalog-outage error", i)
		}
	}
}

func TestLookup_BreakerRejection_CountsApartFromFailures(t *testing.T) {
	m := &recordingMetrics{}
	reader := playback.MustNewNowPlayingReader(failingSource(), m, time.Now, defaultLookupTimeout)
	user := testUser()
	tripBreakerOpen(t, reader, user)

	for i := 0; i < 2; i++ {
		if _, err := lookup(context.Background(), reader, user); err == nil || errors.Is(err, playback.ErrTrackSourceUnavailable) {
			t.Fatalf("rejected call %d: err = %v, want the breaker's own fast-fail", i, err)
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
	clock := newTestClock()
	catalog := &switchableSource{current: failingSource()}
	reader := playback.MustNewNowPlayingReader(catalog, m, clock.read, defaultLookupTimeout)
	user := testUser()
	tripBreakerOpen(t, reader, user)

	if m.breakerOpened != 1 || m.breakerClosed != 0 {
		t.Fatalf("after the outage: opened = %d, closed = %d; want 1/0", m.breakerOpened, m.breakerClosed)
	}

	catalog.use(healthySource())
	clock.advance(enrichmentOpenDuration + time.Second)

	if _, err := lookup(context.Background(), reader, user); err != nil {
		t.Fatalf("expected the recovery probe to succeed, got err = %v", err)
	}
	if m.breakerClosed != 1 {
		t.Errorf("EnrichmentBreakerClosed = %d, want 1 once the catalog recovered", m.breakerClosed)
	}
	if m.breakerOpened != 1 {
		t.Errorf("EnrichmentBreakerOpened = %d, want 1: recovery must not re-open the breaker", m.breakerOpened)
	}
}

func clientVanishingSource(disconnectClient context.CancelFunc) sourceFunc {
	return func(ctx context.Context) (*playback.NowPlayingTrack, error) {
		disconnectClient()
		<-ctx.Done()
		return nil, ctx.Err()
	}
}

func TestLookup_ProbeAbandonedByItsClientDoesNotWedgeBreaker(t *testing.T) {
	m := &recordingMetrics{}
	clock := newTestClock()
	catalog := &switchableSource{current: blockingSource(nil)}
	reader := playback.MustNewNowPlayingReader(catalog, m, clock.read, testLookupTimeout)
	user := testUser()

	for i := 0; i < enrichmentFailureThreshold; i++ {
		_, _ = lookup(context.Background(), reader, user)
	}
	if !rejected(reader, m, user) {
		t.Fatal("precondition: breaker should be open")
	}

	clock.advance(enrichmentOpenDuration + time.Second)
	clientCtx, disconnect := context.WithCancel(context.Background())
	defer disconnect()
	catalog.use(clientVanishingSource(disconnect))
	if _, err := lookup(clientCtx, reader, user); !errors.Is(err, context.Canceled) {
		t.Fatalf("precondition: the abandoned probe must fail with its client's cancellation, got err = %v", err)
	}

	catalog.use(healthySource())
	clock.advance(enrichmentOpenDuration + time.Second)

	if _, err := lookup(context.Background(), reader, user); err != nil {
		t.Fatalf("breaker wedged after a probe its client abandoned: %v", err)
	}
	if _, err := lookup(context.Background(), reader, user); err != nil {
		t.Fatalf("expected breaker closed after the recovery probe succeeded, got err = %v", err)
	}
}

func TestLookup_AProbeThatNeverSettlesLosesItsLeaseAfterThirtySeconds(t *testing.T) {
	m := &recordingMetrics{}
	clock := newTestClock()
	catalog := &switchableSource{current: failingSource()}
	reader := playback.MustNewNowPlayingReader(catalog, m, clock.read, time.Minute)
	user := testUser()
	for i := 0; i < enrichmentFailureThreshold; i++ {
		_, _ = lookup(context.Background(), reader, user)
	}
	clock.advance(enrichmentOpenDuration + time.Second)

	started, release := make(chan struct{}), make(chan struct{})
	catalog.use(sourceFunc(func(context.Context) (*playback.NowPlayingTrack, error) {
		close(started)
		<-release
		return &playback.NowPlayingTrack{}, nil
	}))
	probeDone := make(chan struct{})
	go func() {
		defer close(probeDone)
		_, _ = lookup(context.Background(), reader, user)
	}()
	<-started
	defer func() {
		close(release)
		<-probeDone
	}()

	catalog.use(healthySource())
	clock.advance(enrichmentProbeLease)
	if !rejected(reader, m, user) {
		t.Fatal("a second probe was admitted at exactly the lease, want it refused while the first probe is in flight")
	}

	clock.advance(time.Nanosecond)
	if _, err := lookup(context.Background(), reader, user); err != nil {
		t.Fatalf("breaker still refusing after the probe lease expired: %v", err)
	}
}

func TestLookup_ClientCancellationDoesNotTripBreaker(t *testing.T) {
	m := &recordingMetrics{}
	reader := playback.MustNewNowPlayingReader(blockingSource(nil), m, time.Now, testLookupTimeout)
	user := testUser()

	for i := 0; i < enrichmentFailureThreshold+2; i++ {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		if _, err := lookup(ctx, reader, user); err == nil {
			t.Fatalf("call %d: expected a canceled-context error", i)
		}
	}

	if rejected(reader, m, user) {
		t.Fatal("client cancellations must not trip the enrichment breaker")
	}
}

func TestLookup_PermanentRowErrorsLeaveBreakerClosed(t *testing.T) {
	m := &recordingMetrics{}
	reader := playback.MustNewNowPlayingReader(permanentRowErrorSource(), m, time.Now, defaultLookupTimeout)
	user := testUser()

	for i := 0; i < enrichmentFailureThreshold; i++ {
		if _, err := lookup(context.Background(), reader, user); err == nil {
			t.Fatalf("call %d: expected the row error to surface", i)
		}
	}

	if m.enrichmentFailed != enrichmentFailureThreshold {
		t.Errorf("EnrichmentFailed = %d, want %d: row errors still count as failed lookups",
			m.enrichmentFailed, enrichmentFailureThreshold)
	}
	if rejected(reader, m, user) {
		t.Fatal("permanent per-row errors must not open the breaker for every user")
	}
}

type staleSuccessSource struct {
	firstCallAdmitted chan struct{}
	releaseFirstCall  chan struct{}
	mu                sync.Mutex
	calls             int
}

func (s *staleSuccessSource) NowPlayingTrack(context.Context, ids.UserId, ids.TrackId) (*playback.NowPlayingTrack, error) {
	s.mu.Lock()
	s.calls++
	first := s.calls == 1
	s.mu.Unlock()
	if first {
		close(s.firstCallAdmitted)
		<-s.releaseFirstCall
		return &playback.NowPlayingTrack{}, nil
	}
	return nil, playback.ErrTrackSourceUnavailable
}

func TestLookup_StaleSuccessDoesNotCloseAnOpenBreaker(t *testing.T) {
	m := &recordingMetrics{}
	catalog := &staleSuccessSource{firstCallAdmitted: make(chan struct{}), releaseFirstCall: make(chan struct{})}
	reader := playback.MustNewNowPlayingReader(catalog, m, time.Now, testLookupTimeout)
	user := testUser()

	firstDone := make(chan error)
	go func() {
		_, err := lookup(context.Background(), reader, user)
		firstDone <- err
	}()
	<-catalog.firstCallAdmitted

	for i := 0; i < enrichmentFailureThreshold; i++ {
		_, _ = lookup(context.Background(), reader, user)
	}
	close(catalog.releaseFirstCall)
	if err := <-firstDone; err != nil {
		t.Fatalf("first call: %v", err)
	}

	if !rejected(reader, m, user) {
		t.Fatal("a stale success closed the open breaker without a probe")
	}
}
