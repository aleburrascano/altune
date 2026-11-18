package providercall_test

import (
	"altune/go-api/internal/discovery"
	"context"
	"log/slog"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

const (
	breakerTripsAfter = 5
	breakerOpenFor    = 30 * time.Second
	probeLeaseFor     = 30 * time.Second
)

func TestCircuitBreaker_Closed(t *testing.T) {
	cb := discovery.NewCircuitBreaker()

	if !cb.AllowRequest(context.Background(), discovery.ProviderDeezer) {
		t.Error("expected closed circuit to allow requests")
	}
	if cb.GetStatus(discovery.ProviderDeezer) != discovery.ProviderStatusOK {
		t.Errorf("expected status OK, got %v", cb.GetStatus(discovery.ProviderDeezer))
	}
}

func TestCircuitBreaker_StaysClosedBelowThreshold(t *testing.T) {
	cb := discovery.NewCircuitBreaker()

	for i := 0; i < 4; i++ {
		cb.RecordFailure(context.Background(), discovery.ProviderDeezer)
	}

	if !cb.AllowRequest(context.Background(), discovery.ProviderDeezer) {
		t.Error("expected circuit to stay closed with fewer than 5 failures")
	}
}

func TestCircuitBreaker_OpensAfterThreshold(t *testing.T) {
	cb := discovery.NewCircuitBreaker()

	for i := 0; i < 5; i++ {
		cb.RecordFailure(context.Background(), discovery.ProviderDeezer)
	}

	if cb.AllowRequest(context.Background(), discovery.ProviderDeezer) {
		t.Error("expected open circuit to block requests after 5 failures")
	}
	if cb.GetStatus(discovery.ProviderDeezer) != discovery.ProviderStatusCircuitOpen {
		t.Errorf("expected status CircuitOpen, got %v", cb.GetStatus(discovery.ProviderDeezer))
	}
}

func TestCircuitBreaker_HalfOpen(t *testing.T) {
	cb := discovery.NewCircuitBreaker()

	for i := 0; i < 5; i++ {
		cb.RecordFailure(context.Background(), discovery.ProviderDeezer)
	}

	if cb.AllowRequest(context.Background(), discovery.ProviderDeezer) {
		t.Fatal("expected circuit to be open")
	}

	cb.AdvanceClock(breakerOpenFor + time.Second)

	if !cb.AllowRequest(context.Background(), discovery.ProviderDeezer) {
		t.Error("expected half-open circuit to allow probe request after timeout")
	}
}

func TestCircuitBreaker_ResetsOnSuccess(t *testing.T) {
	cb := discovery.NewCircuitBreaker()

	for i := 0; i < 5; i++ {
		cb.RecordFailure(context.Background(), discovery.ProviderDeezer)
	}

	cb.AdvanceClock(breakerOpenFor + time.Second)

	cb.AllowRequest(context.Background(), discovery.ProviderDeezer)

	cb.RecordSuccess(context.Background(), discovery.ProviderDeezer)

	if !cb.AllowRequest(context.Background(), discovery.ProviderDeezer) {
		t.Error("expected circuit to be closed after success in half-open state")
	}
	if cb.GetStatus(discovery.ProviderDeezer) != discovery.ProviderStatusOK {
		t.Errorf("expected status OK after reset, got %v", cb.GetStatus(discovery.ProviderDeezer))
	}

	for i := 0; i < 4; i++ {
		cb.RecordFailure(context.Background(), discovery.ProviderDeezer)
	}
	if !cb.AllowRequest(context.Background(), discovery.ProviderDeezer) {
		t.Error("expected failure counter to have been reset; 4 failures should not open circuit")
	}
}

func TestCircuitBreaker_IndependentProviders(t *testing.T) {
	cb := discovery.NewCircuitBreaker()

	for i := 0; i < 5; i++ {
		cb.RecordFailure(context.Background(), discovery.ProviderDeezer)
	}

	if cb.AllowRequest(context.Background(), discovery.ProviderDeezer) {
		t.Error("expected Deezer circuit to be open")
	}

	if !cb.AllowRequest(context.Background(), discovery.ProviderMusicBrainz) {
		t.Error("expected MusicBrainz circuit to be independent and closed")
	}

	if !cb.AllowRequest(context.Background(), discovery.ProviderSoundCloud) {
		t.Error("expected SoundCloud circuit to be independent and closed")
	}

	if cb.GetStatus(discovery.ProviderDeezer) != discovery.ProviderStatusCircuitOpen {
		t.Errorf("expected Deezer status CircuitOpen, got %v", cb.GetStatus(discovery.ProviderDeezer))
	}
	if cb.GetStatus(discovery.ProviderMusicBrainz) != discovery.ProviderStatusOK {
		t.Errorf("expected MusicBrainz status OK, got %v", cb.GetStatus(discovery.ProviderMusicBrainz))
	}
}

func TestCircuitBreaker_HalfOpenAdmitsExactlyOneConcurrentProbe(t *testing.T) {
	cb := discovery.NewCircuitBreaker()

	for i := 0; i < 5; i++ {
		cb.RecordFailure(context.Background(), discovery.ProviderDeezer)
	}
	cb.AdvanceClock(breakerOpenFor + time.Second)

	const n = 16
	var wg sync.WaitGroup
	var admitted int32
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if cb.AllowRequest(context.Background(), discovery.ProviderDeezer) {
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
	cb := discovery.NewCircuitBreaker()

	for i := 0; i < 5; i++ {
		cb.RecordFailure(context.Background(), discovery.ProviderDeezer)
	}

	cb.AdvanceClock(breakerOpenFor + time.Second)

	cb.AllowRequest(context.Background(), discovery.ProviderDeezer)

	cb.RecordFailure(context.Background(), discovery.ProviderDeezer)

	if cb.AllowRequest(context.Background(), discovery.ProviderDeezer) {
		t.Error("expected circuit to re-open after failure in half-open state")
	}
}

func TestCircuitBreaker_ReleaseProbeAdmitsNextProbe(t *testing.T) {
	cb := discovery.NewCircuitBreaker()
	tripToHalfOpenWindow(cb, discovery.ProviderDeezer)

	if !cb.AllowRequest(context.Background(), discovery.ProviderDeezer) {
		t.Fatal("expected the half-open probe to be admitted")
	}
	if cb.AllowRequest(context.Background(), discovery.ProviderDeezer) {
		t.Fatal("expected a second request to be rejected while the probe is in flight")
	}

	cb.ReleaseProbe(discovery.ProviderDeezer)

	if !cb.AllowRequest(context.Background(), discovery.ProviderDeezer) {
		t.Error("expected a released probe slot to admit the next probe")
	}
	if cb.AllowRequest(context.Background(), discovery.ProviderDeezer) {
		t.Error("expected the re-admitted probe to still be exclusive")
	}
}

func TestCircuitBreaker_ReleaseProbeIsNoOpOutsideHalfOpen(t *testing.T) {
	cb := discovery.NewCircuitBreaker()
	for i := 0; i < breakerTripsAfter; i++ {
		cb.RecordFailure(context.Background(), discovery.ProviderDeezer)
	}

	cb.ReleaseProbe(discovery.ProviderDeezer)
	if cb.AllowRequest(context.Background(), discovery.ProviderDeezer) {
		t.Error("ReleaseProbe must not let requests through an open circuit")
	}

	cb.ReleaseProbe(discovery.ProviderITunes)
	if !cb.AllowRequest(context.Background(), discovery.ProviderITunes) {
		t.Error("ReleaseProbe must not affect a closed circuit")
	}
}

func TestCircuitBreaker_AbandonedProbeLeaseExpires(t *testing.T) {
	cb := discovery.NewCircuitBreaker()
	for i := 0; i < breakerTripsAfter; i++ {
		cb.RecordFailure(context.Background(), discovery.ProviderDeezer)
	}
	cb.AdvanceClock(breakerOpenFor + time.Second)

	if !cb.AllowRequest(context.Background(), discovery.ProviderDeezer) {
		t.Fatal("expected the half-open probe to be admitted")
	}
	if cb.AllowRequest(context.Background(), discovery.ProviderDeezer) {
		t.Fatal("expected rejection while the probe lease is live")
	}

	cb.AdvanceClock(probeLeaseFor + time.Second)

	if !cb.AllowRequest(context.Background(), discovery.ProviderDeezer) {
		t.Fatal("expected an expired probe lease to admit a new probe")
	}
	if cb.AllowRequest(context.Background(), discovery.ProviderDeezer) {
		t.Error("expected the new probe to hold a fresh lease")
	}
}

func TestCircuitBreaker_FullStateWalkUnderConcurrency(t *testing.T) {
	cb := discovery.NewCircuitBreaker()
	p := discovery.ProviderDeezer

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

	hammer(breakerTripsAfter, func() { cb.RecordFailure(context.Background(), p) })
	if cb.AllowRequest(context.Background(), p) {
		t.Fatal("breaker must be open after the failure threshold")
	}
	if cb.GetStatus(p) != discovery.ProviderStatusCircuitOpen {
		t.Fatalf("status = %v, want circuit_open", cb.GetStatus(p))
	}

	cb.AdvanceClock(breakerOpenFor + time.Second)
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
	if cb.GetStatus(p) != discovery.ProviderStatusOK {
		t.Errorf("half-open status = %v, want ok", cb.GetStatus(p))
	}

	cb.RecordSuccess(context.Background(), p)
	hammer(16, func() {
		if !cb.AllowRequest(context.Background(), p) {
			t.Error("re-closed breaker must allow requests")
		}
	})

	hammer(breakerTripsAfter, func() { cb.RecordFailure(context.Background(), p) })
	if cb.AllowRequest(context.Background(), p) {
		t.Fatal("breaker must re-open after a fresh failure threshold")
	}

	cb.AdvanceClock(breakerOpenFor + time.Second)
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
	p := discovery.ProviderDeezer

	t.Run("opening logs once with provider and failures from ctx", func(t *testing.T) {
		h := captureBreakerLogs(t)
		cb := discovery.NewCircuitBreaker()
		for i := 0; i < breakerTripsAfter; i++ {
			cb.RecordFailure(ctx, p)
		}
		if got := h.messages(); len(got) != 1 || got[0] != "circuit breaker opened (provider failing)" {
			t.Fatalf("logs = %v, want exactly the opened message", got)
		}
		l := h.logs[0]
		if l.attrs["provider"] != p.String() || l.attrs["failures"] != int64(breakerTripsAfter) {
			t.Errorf("attrs = %v", l.attrs)
		}
		if l.ctxV != "marker" {
			t.Errorf("record ctx value = %v, want the passed ctx", l.ctxV)
		}
	})

	t.Run("no failures logs nothing", func(t *testing.T) {
		h := captureBreakerLogs(t)
		cb := discovery.NewCircuitBreaker()
		cb.AllowRequest(ctx, p)
		if got := h.messages(); len(got) != 0 {
			t.Fatalf("logs = %v, want none", got)
		}
	})

	t.Run("success on closed breaker logs nothing", func(t *testing.T) {
		h := captureBreakerLogs(t)
		cb := discovery.NewCircuitBreaker()
		cb.RecordSuccess(ctx, p)
		if got := h.messages(); len(got) != 0 {
			t.Fatalf("logs = %v, want none", got)
		}
	})

	t.Run("recovery logs half-open then closed", func(t *testing.T) {
		h := captureBreakerLogs(t)
		cb := discovery.NewCircuitBreaker()
		for i := 0; i < breakerTripsAfter; i++ {
			cb.RecordFailure(ctx, p)
		}
		cb.AdvanceClock(breakerOpenFor + time.Second)
		if !cb.AllowRequest(ctx, p) {
			t.Fatal("probe refused after breakerOpenFor")
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

func tripToHalfOpenWindow(cb *discovery.CircuitBreaker, provider discovery.ProviderName) {
	for i := 0; i < breakerTripsAfter; i++ {
		cb.RecordFailure(context.Background(), provider)
	}
	cb.AdvanceClock(breakerOpenFor + time.Second)
}
