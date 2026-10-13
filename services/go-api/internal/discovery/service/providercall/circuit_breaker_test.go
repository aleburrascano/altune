package providercall

import (
	"altune/go-api/internal/discovery/domain"
	"context"
	"log/slog"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type breakerClock struct {
	mu  sync.Mutex
	cur time.Time
}

func newBreakerClock() *breakerClock {
	return &breakerClock{cur: time.Unix(1_700_000_000, 0)}
}

func (c *breakerClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.cur
}

func (c *breakerClock) advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.cur = c.cur.Add(d)
}

func TestCircuitBreaker_Closed(t *testing.T) {
	cb := NewCircuitBreaker()

	if !cb.AllowRequest(context.Background(), domain.ProviderDeezer) {
		t.Error("expected closed circuit to allow requests")
	}
	if cb.GetStatus(domain.ProviderDeezer) != domain.ProviderStatusOK {
		t.Errorf("expected status OK, got %v", cb.GetStatus(domain.ProviderDeezer))
	}
}

func TestCircuitBreaker_StaysClosedBelowThreshold(t *testing.T) {
	cb := NewCircuitBreaker()

	for i := 0; i < 4; i++ {
		cb.RecordFailure(context.Background(), domain.ProviderDeezer)
	}

	if !cb.AllowRequest(context.Background(), domain.ProviderDeezer) {
		t.Error("expected circuit to stay closed with fewer than 5 failures")
	}
}

func TestCircuitBreaker_OpensAfterThreshold(t *testing.T) {
	cb := NewCircuitBreaker()

	for i := 0; i < 5; i++ {
		cb.RecordFailure(context.Background(), domain.ProviderDeezer)
	}

	if cb.AllowRequest(context.Background(), domain.ProviderDeezer) {
		t.Error("expected open circuit to block requests after 5 failures")
	}
	if cb.GetStatus(domain.ProviderDeezer) != domain.ProviderStatusCircuitOpen {
		t.Errorf("expected status CircuitOpen, got %v", cb.GetStatus(domain.ProviderDeezer))
	}
}

func TestCircuitBreaker_HalfOpen(t *testing.T) {
	clock := newBreakerClock()
	cb := newCircuitBreaker(clock.Now)

	for i := 0; i < 5; i++ {
		cb.RecordFailure(context.Background(), domain.ProviderDeezer)
	}

	if cb.AllowRequest(context.Background(), domain.ProviderDeezer) {
		t.Fatal("expected circuit to be open")
	}

	clock.advance(OpenDuration + time.Second)

	if !cb.AllowRequest(context.Background(), domain.ProviderDeezer) {
		t.Error("expected half-open circuit to allow probe request after timeout")
	}
}

func TestCircuitBreaker_ResetsOnSuccess(t *testing.T) {
	clock := newBreakerClock()
	cb := newCircuitBreaker(clock.Now)

	for i := 0; i < 5; i++ {
		cb.RecordFailure(context.Background(), domain.ProviderDeezer)
	}

	clock.advance(OpenDuration + time.Second)

	cb.AllowRequest(context.Background(), domain.ProviderDeezer)

	cb.RecordSuccess(context.Background(), domain.ProviderDeezer)

	if !cb.AllowRequest(context.Background(), domain.ProviderDeezer) {
		t.Error("expected circuit to be closed after success in half-open state")
	}
	if cb.GetStatus(domain.ProviderDeezer) != domain.ProviderStatusOK {
		t.Errorf("expected status OK after reset, got %v", cb.GetStatus(domain.ProviderDeezer))
	}

	for i := 0; i < 4; i++ {
		cb.RecordFailure(context.Background(), domain.ProviderDeezer)
	}
	if !cb.AllowRequest(context.Background(), domain.ProviderDeezer) {
		t.Error("expected failure counter to have been reset; 4 failures should not open circuit")
	}
}

func TestCircuitBreaker_IndependentProviders(t *testing.T) {
	cb := NewCircuitBreaker()

	for i := 0; i < 5; i++ {
		cb.RecordFailure(context.Background(), domain.ProviderDeezer)
	}

	if cb.AllowRequest(context.Background(), domain.ProviderDeezer) {
		t.Error("expected Deezer circuit to be open")
	}

	if !cb.AllowRequest(context.Background(), domain.ProviderMusicBrainz) {
		t.Error("expected MusicBrainz circuit to be independent and closed")
	}

	if !cb.AllowRequest(context.Background(), domain.ProviderSoundCloud) {
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
	clock := newBreakerClock()
	cb := newCircuitBreaker(clock.Now)

	for i := 0; i < 5; i++ {
		cb.RecordFailure(context.Background(), domain.ProviderDeezer)
	}
	clock.advance(OpenDuration + time.Second)

	const n = 16
	var wg sync.WaitGroup
	var admitted int32
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if cb.AllowRequest(context.Background(), domain.ProviderDeezer) {
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
	clock := newBreakerClock()
	cb := newCircuitBreaker(clock.Now)

	for i := 0; i < 5; i++ {
		cb.RecordFailure(context.Background(), domain.ProviderDeezer)
	}

	clock.advance(OpenDuration + time.Second)

	cb.AllowRequest(context.Background(), domain.ProviderDeezer)

	cb.RecordFailure(context.Background(), domain.ProviderDeezer)

	if cb.AllowRequest(context.Background(), domain.ProviderDeezer) {
		t.Error("expected circuit to re-open after failure in half-open state")
	}
}

func TestCircuitBreaker_ReleaseProbeAdmitsNextProbe(t *testing.T) {
	cb := NewCircuitBreaker()
	tripToHalfOpenWindow(cb, domain.ProviderDeezer)

	if !cb.AllowRequest(context.Background(), domain.ProviderDeezer) {
		t.Fatal("expected the half-open probe to be admitted")
	}
	if cb.AllowRequest(context.Background(), domain.ProviderDeezer) {
		t.Fatal("expected a second request to be rejected while the probe is in flight")
	}

	cb.ReleaseProbe(domain.ProviderDeezer)

	if !cb.AllowRequest(context.Background(), domain.ProviderDeezer) {
		t.Error("expected a released probe slot to admit the next probe")
	}
	if cb.AllowRequest(context.Background(), domain.ProviderDeezer) {
		t.Error("expected the re-admitted probe to still be exclusive")
	}
}

func TestCircuitBreaker_ReleaseProbeIsNoOpOutsideHalfOpen(t *testing.T) {
	cb := NewCircuitBreaker()
	for i := 0; i < FailureThreshold; i++ {
		cb.RecordFailure(context.Background(), domain.ProviderDeezer)
	}

	cb.ReleaseProbe(domain.ProviderDeezer)
	if cb.AllowRequest(context.Background(), domain.ProviderDeezer) {
		t.Error("ReleaseProbe must not let requests through an open circuit")
	}

	cb.ReleaseProbe(domain.ProviderITunes)
	if !cb.AllowRequest(context.Background(), domain.ProviderITunes) {
		t.Error("ReleaseProbe must not affect a closed circuit")
	}
}

func TestCircuitBreaker_AbandonedProbeLeaseExpires(t *testing.T) {
	clock := newBreakerClock()
	cb := newCircuitBreaker(clock.Now)
	for i := 0; i < FailureThreshold; i++ {
		cb.RecordFailure(context.Background(), domain.ProviderDeezer)
	}
	clock.advance(OpenDuration + time.Second)

	if !cb.AllowRequest(context.Background(), domain.ProviderDeezer) {
		t.Fatal("expected the half-open probe to be admitted")
	}
	if cb.AllowRequest(context.Background(), domain.ProviderDeezer) {
		t.Fatal("expected rejection while the probe lease is live")
	}

	clock.advance(probeLease + time.Second)

	if !cb.AllowRequest(context.Background(), domain.ProviderDeezer) {
		t.Fatal("expected an expired probe lease to admit a new probe")
	}
	if cb.AllowRequest(context.Background(), domain.ProviderDeezer) {
		t.Error("expected the new probe to hold a fresh lease")
	}
}

func TestCircuitBreaker_FullStateWalkUnderConcurrency(t *testing.T) {
	clock := newBreakerClock()
	cb := newCircuitBreaker(clock.Now)
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
		if !cb.AllowRequest(context.Background(), p) {
			t.Error("closed breaker must allow requests")
		}
	})

	hammer(FailureThreshold, func() { cb.RecordFailure(context.Background(), p) })
	if cb.AllowRequest(context.Background(), p) {
		t.Fatal("breaker must be open after the failure threshold")
	}
	if cb.GetStatus(p) != domain.ProviderStatusCircuitOpen {
		t.Fatalf("status = %v, want circuit_open", cb.GetStatus(p))
	}

	clock.advance(OpenDuration + time.Second)
	var admitted int32
	var mu sync.Mutex
	hammer(16, func() {
		if cb.AllowRequest(context.Background(), p) {
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

	cb.RecordSuccess(context.Background(), p)
	hammer(16, func() {
		if !cb.AllowRequest(context.Background(), p) {
			t.Error("re-closed breaker must allow requests")
		}
	})

	hammer(FailureThreshold, func() { cb.RecordFailure(context.Background(), p) })
	if cb.AllowRequest(context.Background(), p) {
		t.Fatal("breaker must re-open after a fresh failure threshold")
	}

	clock.advance(OpenDuration + time.Second)
	if !cb.AllowRequest(context.Background(), p) {
		t.Fatal("aged-open breaker must admit the probe")
	}
	cb.RecordFailure(context.Background(), p)
	if cb.AllowRequest(context.Background(), p) {
		t.Error("a failed half-open probe must re-open the breaker immediately")
	}
}

type breakerLogKey struct{}

type capturedLog struct {
	msg   string
	attrs map[string]any
	ctxV  any
}

type captureHandler struct {
	mu   sync.Mutex
	logs []capturedLog
}

func (h *captureHandler) Enabled(context.Context, slog.Level) bool { return true }
func (h *captureHandler) WithAttrs([]slog.Attr) slog.Handler       { return h }
func (h *captureHandler) WithGroup(string) slog.Handler            { return h }

func (h *captureHandler) Handle(ctx context.Context, r slog.Record) error {
	l := capturedLog{msg: r.Message, attrs: map[string]any{}, ctxV: ctx.Value(breakerLogKey{})}
	r.Attrs(func(a slog.Attr) bool {
		l.attrs[a.Key] = a.Value.Any()
		return true
	})
	h.mu.Lock()
	defer h.mu.Unlock()
	h.logs = append(h.logs, l)
	return nil
}

func (h *captureHandler) messages() []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	var out []string
	for _, l := range h.logs {
		out = append(out, l.msg)
	}
	return out
}

func captureBreakerLogs(t *testing.T) *captureHandler {
	t.Helper()
	h := &captureHandler{}
	prev := slog.Default()
	slog.SetDefault(slog.New(h))
	t.Cleanup(func() { slog.SetDefault(prev) })
	return h
}

func TestCircuitBreaker_LogsCarryCallerContext(t *testing.T) {
	ctx := context.WithValue(context.Background(), breakerLogKey{}, "marker")
	p := domain.ProviderDeezer

	t.Run("opening logs once with provider and failures from ctx", func(t *testing.T) {
		h := captureBreakerLogs(t)
		cb := NewCircuitBreaker()
		for i := 0; i < FailureThreshold; i++ {
			cb.RecordFailure(ctx, p)
		}
		if got := h.messages(); len(got) != 1 || got[0] != "circuit breaker opened (provider failing)" {
			t.Fatalf("logs = %v, want exactly the opened message", got)
		}
		l := h.logs[0]
		if l.attrs["provider"] != p.String() || l.attrs["failures"] != int64(FailureThreshold) {
			t.Errorf("attrs = %v", l.attrs)
		}
		if l.ctxV != "marker" {
			t.Errorf("record ctx value = %v, want the passed ctx", l.ctxV)
		}
	})

	t.Run("no failures logs nothing", func(t *testing.T) {
		h := captureBreakerLogs(t)
		cb := NewCircuitBreaker()
		cb.AllowRequest(ctx, p)
		if got := h.messages(); len(got) != 0 {
			t.Fatalf("logs = %v, want none", got)
		}
	})

	t.Run("success on closed breaker logs nothing", func(t *testing.T) {
		h := captureBreakerLogs(t)
		cb := NewCircuitBreaker()
		cb.RecordSuccess(ctx, p)
		if got := h.messages(); len(got) != 0 {
			t.Fatalf("logs = %v, want none", got)
		}
	})

	t.Run("recovery logs half-open then closed", func(t *testing.T) {
		h := captureBreakerLogs(t)
		clock := newBreakerClock()
		cb := newCircuitBreaker(clock.Now)
		for i := 0; i < FailureThreshold; i++ {
			cb.RecordFailure(ctx, p)
		}
		clock.advance(OpenDuration + time.Second)
		if !cb.AllowRequest(ctx, p) {
			t.Fatal("probe refused after OpenDuration")
		}
		cb.RecordSuccess(ctx, p)
		want := []string{
			"circuit breaker opened (provider failing)",
			"circuit breaker half-open (probing recovery)",
			"circuit breaker closed (provider recovered)",
		}
		got := h.messages()
		if len(got) != len(want) {
			t.Fatalf("logs = %v, want %v", got, want)
		}
		for i := range want {
			if got[i] != want[i] {
				t.Fatalf("logs = %v, want %v", got, want)
			}
		}
		for _, l := range h.logs {
			if l.ctxV != "marker" {
				t.Errorf("%q lost the ctx value", l.msg)
			}
		}
	})
}

func tripToHalfOpenWindow(cb *CircuitBreaker, provider domain.ProviderName) {
	for i := 0; i < FailureThreshold; i++ {
		cb.RecordFailure(context.Background(), provider)
	}
	cb.mu.Lock()
	real := cb.now
	cb.now = func() time.Time { return real().Add(OpenDuration + time.Second) }
	cb.mu.Unlock()
}
