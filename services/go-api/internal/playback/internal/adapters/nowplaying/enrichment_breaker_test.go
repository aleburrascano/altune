package nowplaying_test

import (
	"altune/go-api/internal/playback"
	"altune/go-api/internal/shared/ids"
	"altune/go-api/internal/shared/logging"
	"context"
	"errors"
	"log/slog"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
)

const gaugeRaceAttempts = 5000

type breakerGauge struct {
	recordingMetrics
	gaugeMu  sync.Mutex
	degraded bool
}

func (g *breakerGauge) EnrichmentBreakerOpened() {
	g.recordingMetrics.EnrichmentBreakerOpened()
	g.write(true)
}

func (g *breakerGauge) EnrichmentBreakerClosed() {
	g.recordingMetrics.EnrichmentBreakerClosed()
	g.write(false)
}

func (g *breakerGauge) write(degraded bool) {
	g.gaugeMu.Lock()
	defer g.gaugeMu.Unlock()
	g.degraded = degraded
}

func (g *breakerGauge) readsDegraded() bool {
	g.gaugeMu.Lock()
	defer g.gaugeMu.Unlock()
	return g.degraded
}

var (
	succeedingTrack = uuid.NewString()
	failingTrack    = uuid.NewString()
	inspectingTrack = uuid.NewString()
)

type trackScript struct{}

func (trackScript) NowPlayingTrack(_ context.Context, _ ids.UserId, id ids.TrackId) (*playback.NowPlayingTrack, error) {
	switch id.String() {
	case succeedingTrack:
		return &playback.NowPlayingTrack{}, nil
	case failingTrack:
		return nil, playback.ErrTrackSourceUnavailable
	default:
		return nil, errors.New("row error: leaves the breaker as it is")
	}
}

type scriptedReader struct {
	reader nowPlayingLookup
	gauge  *breakerGauge
	user   ids.UserId
}

func newScriptedReader(clock func() time.Time) *scriptedReader {
	gauge := &breakerGauge{}
	return &scriptedReader{
		reader: playback.MustNewNowPlayingReader(trackScript{}, gauge, clock, defaultLookupTimeout),
		gauge:  gauge,
		user:   testUser(),
	}
}

func (s *scriptedReader) look(trackId string) error {
	_, err := s.reader.Lookup(context.Background(), s.user, trackId)
	return err
}

func (s *scriptedReader) failEnoughToTrip() {
	for range enrichmentFailureThreshold {
		_ = s.look(failingTrack)
	}
}

func (s *scriptedReader) degraded() bool {
	before := s.gauge.read(&s.gauge.breakerRejected)
	_ = s.look(inspectingTrack)
	return s.gauge.read(&s.gauge.breakerRejected) > before
}

func (s *scriptedReader) awaitDegraded(t *testing.T) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for !s.degraded() {
		if time.Now().After(deadline) {
			t.Error("breaker never tripped on a full run of dependency failures")
			return
		}
		runtime.Gosched()
	}
}

func (s *scriptedReader) assertGaugeMatchesState(t *testing.T, attempt int) {
	t.Helper()
	if gauge, state := s.gauge.readsDegraded(), s.degraded(); gauge != state {
		t.Fatalf("attempt %d: gauge reads degraded=%v while the breaker is degraded=%v", attempt, gauge, state)
	}
}

func TestBreaker_GaugeMatchesStateWhenASuccessRacesTheTrippingFailure(t *testing.T) {
	captureLogs(t)

	for attempt := 0; attempt < gaugeRaceAttempts; attempt++ {
		s := newScriptedReader(time.Now)

		var wg sync.WaitGroup
		wg.Add(2)
		go func() {
			defer wg.Done()
			_ = s.look(succeedingTrack)
		}()
		go func() {
			defer wg.Done()
			s.failEnoughToTrip()
		}()
		wg.Wait()

		s.assertGaugeMatchesState(t, attempt)
	}
}

func TestBreaker_GaugeMatchesStateWhenARecoverySuccessFollowsTheTrip(t *testing.T) {
	captureLogs(t)

	for attempt := 0; attempt < gaugeRaceAttempts; attempt++ {
		clock := newTestClock()
		s := newScriptedReader(clock.read)

		var wg sync.WaitGroup
		wg.Add(2)
		go func() {
			defer wg.Done()
			s.failEnoughToTrip()
		}()
		go func() {
			defer wg.Done()
			s.awaitDegraded(t)
			clock.advance(enrichmentOpenDuration + time.Second)
			closedBefore := s.gauge.read(&s.gauge.breakerClosed)
			if err := s.look(succeedingTrack); err != nil || s.gauge.read(&s.gauge.breakerClosed) != closedBefore+1 {
				t.Errorf("attempt %d: expected a half-open probe to close the breaker, got err=%v", attempt, err)
			}
			clock.advance(-(enrichmentOpenDuration + time.Second))
		}()
		wg.Wait()

		s.assertGaugeMatchesState(t, attempt)
	}
}

func captureBreakerLogs(t *testing.T) *logging.Capture {
	t.Helper()
	return logging.CaptureText(t, slog.LevelDebug)
}

func logLines(buf *logging.Capture) []string {
	var lines []string
	for _, l := range strings.Split(buf.String(), "\n") {
		if l != "" {
			lines = append(lines, l)
		}
	}
	return lines
}

func assertLogs(t *testing.T, buf *logging.Capture, want ...[2]string) {
	t.Helper()
	lines := logLines(buf)
	if len(lines) != len(want) {
		t.Fatalf("want %d log records, got %d: %q", len(want), len(lines), lines)
	}
	for i, w := range want {
		if !strings.Contains(lines[i], "level="+w[0]) || !strings.Contains(lines[i], `msg="`+w[1]+`"`) {
			t.Fatalf("log %d = %q, want level %s msg %q", i, lines[i], w[0], w[1])
		}
	}
	buf.Reset()
}

func TestEnrichmentBreaker_LogsTransitions(t *testing.T) {
	buf := captureBreakerLogs(t)
	clock := &testClock{now: time.Unix(1_700_000_000, 0)}
	s := newScriptedReader(clock.read)

	for range enrichmentFailureThreshold - 1 {
		_ = s.look(failingTrack)
	}
	if lines := logLines(buf); len(lines) != 0 {
		t.Fatalf("failures below the threshold must log nothing, got %q", lines)
	}

	_ = s.look(failingTrack)
	if !strings.Contains(buf.String(), "failures=5") {
		t.Fatalf("open record must carry failures=5, got %q", buf.String())
	}
	assertLogs(t, buf, [2]string{"WARN", "now-playing enrichment breaker opened (catalog failing)"})

	_ = s.look(failingTrack)
	if lines := logLines(buf); len(lines) != 0 {
		t.Fatalf("a call rejected while open must log nothing, got %q", lines)
	}

	clock.advance(enrichmentOpenDuration + time.Second)
	if err := s.look(succeedingTrack); err != nil {
		t.Fatalf("the probe after the open window must be admitted, got %v", err)
	}
	assertLogs(t, buf,
		[2]string{"WARN", "now-playing enrichment breaker half-open (probing catalog recovery)"},
		[2]string{"INFO", "now-playing enrichment breaker closed (catalog recovered)"})
}

func TestEnrichmentBreaker_AFailedProbeReopensAtOnce(t *testing.T) {
	clock := newTestClock()
	s := newScriptedReader(clock.read)
	s.failEnoughToTrip()

	clock.advance(enrichmentOpenDuration + time.Second)
	if err := s.look(failingTrack); !errors.Is(err, playback.ErrTrackSourceUnavailable) {
		t.Fatalf("the probe after the open window must reach the source, got %v", err)
	}

	if !s.degraded() {
		t.Error("a failed probe must reopen the breaker without waiting for another run of failures")
	}
	if opened := s.gauge.read(&s.gauge.breakerOpened); opened != 2 {
		t.Errorf("EnrichmentBreakerOpened = %d, want 2: the failed probe reopens it", opened)
	}
}

func TestEnrichmentBreaker_AFailureRecordedWhileOpenLogsNothing(t *testing.T) {
	buf := captureBreakerLogs(t)
	held, release := make(chan struct{}), make(chan struct{})
	var calls atomic.Int32
	reader := playback.MustNewNowPlayingReader(sourceFunc(func(context.Context) (*playback.NowPlayingTrack, error) {
		if calls.Add(1) == 1 {
			close(held)
			<-release
		}
		return nil, playback.ErrTrackSourceUnavailable
	}), noopEnrichmentMetrics{}, time.Now, defaultLookupTimeout)
	user := testUser()
	firstDone := make(chan struct{})
	go func() {
		defer close(firstDone)
		_, _ = lookup(context.Background(), reader, user)
	}()
	<-held

	for range enrichmentFailureThreshold {
		_, _ = lookup(context.Background(), reader, user)
	}
	assertLogs(t, buf, [2]string{"WARN", "now-playing enrichment breaker opened (catalog failing)"})

	close(release)
	<-firstDone
	if lines := logLines(buf); len(lines) != 0 {
		t.Fatalf("a failure recorded while open must log nothing, got %q", lines)
	}
}
