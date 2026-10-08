package catalogbridge

import (
	"bytes"
	"log/slog"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

const gaugeRaceAttempts = 5000

type breakerGauge struct {
	mu       sync.Mutex
	degraded bool
}

func (g *breakerGauge) EnrichmentFailed()          {}
func (g *breakerGauge) NowPlayingLookupTimedOut()  {}
func (g *breakerGauge) EnrichmentBreakerRejected() {}
func (g *breakerGauge) EnrichmentBreakerOpened()   { g.write(true) }
func (g *breakerGauge) EnrichmentBreakerClosed()   { g.write(false) }

func (g *breakerGauge) write(degraded bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.degraded = degraded
}

func (g *breakerGauge) readsDegraded() bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.degraded
}

func failEnoughToTrip(b *enrichmentBreaker) {
	for i := 0; i < enrichmentFailureThreshold; i++ {
		b.recordFailure()
	}
}

func breakerIsDegraded(b *enrichmentBreaker) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.state != breakerClosed
}

func awaitDegraded(t *testing.T, b *enrichmentBreaker) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for !breakerIsDegraded(b) {
		if time.Now().After(deadline) {
			t.Error("breaker never tripped on a full run of dependency failures")
			return
		}
		runtime.Gosched()
	}
}

func assertGaugeMatchesState(t *testing.T, attempt int, gauge *breakerGauge, b *enrichmentBreaker) {
	t.Helper()
	if gauge.readsDegraded() != breakerIsDegraded(b) {
		t.Fatalf("attempt %d: gauge reads degraded=%v while the breaker is degraded=%v",
			attempt, gauge.readsDegraded(), breakerIsDegraded(b))
	}
}

func TestBreaker_GaugeMatchesStateWhenASuccessRacesTheTrippingFailure(t *testing.T) {
	captureLogs(t)

	for attempt := 0; attempt < gaugeRaceAttempts; attempt++ {
		gauge := &breakerGauge{}
		breaker := newEnrichmentBreaker(gauge)
		failEnoughToTrip(breaker)

		var wg sync.WaitGroup
		wg.Add(2)
		go func() {
			defer wg.Done()
			breaker.recordSuccess(admission{})
		}()
		go func() {
			defer wg.Done()
			failEnoughToTrip(breaker)
		}()
		wg.Wait()

		assertGaugeMatchesState(t, attempt, gauge, breaker)
	}
}

func TestBreaker_GaugeMatchesStateWhenARecoverySuccessFollowsTheTrip(t *testing.T) {
	captureLogs(t)

	for attempt := 0; attempt < gaugeRaceAttempts; attempt++ {
		gauge := &breakerGauge{}
		breaker := newEnrichmentBreaker(gauge)
		var skew atomic.Int64
		breaker.now = func() time.Time { return time.Now().Add(time.Duration(skew.Load())) }

		var wg sync.WaitGroup
		wg.Add(2)
		go func() {
			defer wg.Done()
			failEnoughToTrip(breaker)
		}()
		go func() {
			defer wg.Done()
			awaitDegraded(t, breaker)
			skew.Store(int64(enrichmentOpenDuration + time.Second))
			admitted, probe := breaker.allow()
			if !admitted || !probe.probe {
				t.Errorf("attempt %d: expected a half-open probe admission, got admitted=%v probe=%v", attempt, admitted, probe.probe)
				return
			}
			breaker.recordSuccess(probe)
			breaker.release(probe)
		}()
		wg.Wait()

		assertGaugeMatchesState(t, attempt, gauge, breaker)
	}
}

func captureBreakerLogs(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(prev) })
	return &buf
}

func logLines(buf *bytes.Buffer) []string {
	var lines []string
	for _, l := range strings.Split(buf.String(), "\n") {
		if l != "" {
			lines = append(lines, l)
		}
	}
	return lines
}

func assertSingleLog(t *testing.T, buf *bytes.Buffer, level, msg string) {
	t.Helper()
	lines := logLines(buf)
	if len(lines) != 1 {
		t.Fatalf("want exactly one log record, got %d: %q", len(lines), lines)
	}
	if !strings.Contains(lines[0], "level="+level) || !strings.Contains(lines[0], `msg="`+msg+`"`) {
		t.Fatalf("log = %q, want level %s msg %q", lines[0], level, msg)
	}
	buf.Reset()
}

func TestEnrichmentBreaker_LogsTransitions(t *testing.T) {
	buf := captureBreakerLogs(t)
	now := time.Unix(1_700_000_000, 0)
	b := newEnrichmentBreaker(&breakerGauge{})
	b.now = func() time.Time { return now }

	for range enrichmentFailureThreshold - 1 {
		b.recordFailure()
	}
	if lines := logLines(buf); len(lines) != 0 {
		t.Fatalf("failures below the threshold must log nothing, got %q", lines)
	}

	b.recordFailure()
	if !strings.Contains(buf.String(), "failures=5") {
		t.Fatalf("open record must carry failures=5, got %q", buf.String())
	}
	assertSingleLog(t, buf, "WARN", "now-playing enrichment breaker opened (catalog failing)")

	b.recordFailure()
	if lines := logLines(buf); len(lines) != 0 {
		t.Fatalf("a failure while open must log nothing, got %q", lines)
	}

	now = now.Add(enrichmentOpenDuration + time.Second)
	ok, adm := b.allow()
	if !ok || !adm.probe {
		t.Fatalf("allow after the open window must admit a probe, got ok=%v probe=%v", ok, adm.probe)
	}
	assertSingleLog(t, buf, "WARN", "now-playing enrichment breaker half-open (probing catalog recovery)")

	b.recordSuccess(adm)
	assertSingleLog(t, buf, "INFO", "now-playing enrichment breaker closed (catalog recovered)")
}
