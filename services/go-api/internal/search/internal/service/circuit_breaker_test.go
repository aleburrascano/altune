package service_test

import (
	"altune/go-api/internal/search/internal/domain"
	"altune/go-api/internal/search/internal/service"
	"altune/go-api/internal/shared/clock"
	"altune/go-api/internal/shared/clock/clocktest"
	"altune/go-api/internal/shared/logging"
	"context"
	"log/slog"
	"sync"
	"testing"
	"time"
)

const (
	guardBreakerTripsAfter = 5
	guardBreakerOpenFor    = 30 * time.Second
	probeLeaseFor          = 30 * time.Second
)

func tripProvider(ctx context.Context, cb *service.CircuitBreaker, provider domain.ProviderName) {
	for range guardBreakerTripsAfter {
		cb.RecordFailure(ctx, provider, service.Admission{})
	}
}

func TestCircuitBreaker_AProviderWithNoFailuresIsClosedAndOK(t *testing.T) {
	cb := service.NewCircuitBreaker(clock.Now)

	if _, ok := cb.AllowRequest(context.Background(), domain.ProviderDeezer); !ok {
		t.Error("expected closed circuit to allow requests")
	}
	if cb.GetStatus(domain.ProviderDeezer) != domain.ProviderStatusOK {
		t.Errorf("expected status OK, got %v", cb.GetStatus(domain.ProviderDeezer))
	}
}

func TestCircuitBreaker_OpensAtTheThresholdAndReportsCircuitOpen(t *testing.T) {
	cb := service.NewCircuitBreaker(clock.Now)
	for range guardBreakerTripsAfter - 1 {
		cb.RecordFailure(context.Background(), domain.ProviderDeezer, service.Admission{})
	}
	if _, ok := cb.AllowRequest(context.Background(), domain.ProviderDeezer); !ok {
		t.Fatal("expected the circuit to stay closed one failure below the threshold")
	}

	cb.RecordFailure(context.Background(), domain.ProviderDeezer, service.Admission{})

	if _, ok := cb.AllowRequest(context.Background(), domain.ProviderDeezer); ok {
		t.Error("expected open circuit to block requests at the threshold")
	}
	if cb.GetStatus(domain.ProviderDeezer) != domain.ProviderStatusCircuitOpen {
		t.Errorf("expected status CircuitOpen, got %v", cb.GetStatus(domain.ProviderDeezer))
	}
}

func TestCircuitBreaker_AHalfOpenProviderReportsOKWhileItsProbeRuns(t *testing.T) {
	cb := service.NewCircuitBreaker(clock.Now)
	tripProvider(context.Background(), cb, domain.ProviderDeezer)
	cb.AdvanceClock(guardBreakerOpenFor + time.Second)

	if _, ok := cb.AllowRequest(context.Background(), domain.ProviderDeezer); !ok {
		t.Fatal("expected the probe to be admitted after the open window")
	}

	if got := cb.GetStatus(domain.ProviderDeezer); got != domain.ProviderStatusOK {
		t.Errorf("half-open status = %v, want ok", got)
	}
	if snap := cb.Snapshot(domain.ProviderDeezer); snap.State != service.CircuitHalfOpen || !snap.Probing {
		t.Errorf("snapshot = %+v, want half-open with the probe in flight", snap)
	}
}

func TestCircuitBreaker_AProbeLeaseExpiresAfterItsConfiguredDuration(t *testing.T) {
	cb := service.NewCircuitBreaker(clock.Now)
	tripProvider(context.Background(), cb, domain.ProviderDeezer)
	cb.AdvanceClock(guardBreakerOpenFor + time.Second)
	if _, ok := cb.AllowRequest(context.Background(), domain.ProviderDeezer); !ok {
		t.Fatal("expected the half-open probe to be admitted")
	}

	cb.AdvanceClock(probeLeaseFor + time.Second)

	if _, ok := cb.AllowRequest(context.Background(), domain.ProviderDeezer); !ok {
		t.Error("expected an abandoned probe to lose its lease and admit a new probe")
	}
}

func TestCircuitBreaker_IndependentProviders(t *testing.T) {
	cb := service.NewCircuitBreaker(clock.Now)
	tripProvider(context.Background(), cb, domain.ProviderDeezer)

	if _, ok := cb.AllowRequest(context.Background(), domain.ProviderDeezer); ok {
		t.Error("expected Deezer circuit to be open")
	}
	if _, ok := cb.AllowRequest(context.Background(), domain.ProviderMusicBrainz); !ok {
		t.Error("expected MusicBrainz circuit to be independent and closed")
	}
	if _, ok := cb.AllowRequest(context.Background(), domain.ProviderSoundCloud); !ok {
		t.Error("expected SoundCloud circuit to be independent and closed")
	}
	if cb.GetStatus(domain.ProviderDeezer) != domain.ProviderStatusCircuitOpen {
		t.Errorf("expected Deezer status CircuitOpen, got %v", cb.GetStatus(domain.ProviderDeezer))
	}
	if cb.GetStatus(domain.ProviderMusicBrainz) != domain.ProviderStatusOK {
		t.Errorf("expected MusicBrainz status OK, got %v", cb.GetStatus(domain.ProviderMusicBrainz))
	}
}

func TestCircuitBreaker_ReleaseProbeAdmitsNextProbe(t *testing.T) {
	cb := service.NewCircuitBreaker(clock.Now)
	tripProvider(context.Background(), cb, domain.ProviderDeezer)
	cb.AdvanceClock(guardBreakerOpenFor + time.Second)
	adm, ok := cb.AllowRequest(context.Background(), domain.ProviderDeezer)
	if !ok {
		t.Fatal("expected the half-open probe to be admitted")
	}
	if _, ok := cb.AllowRequest(context.Background(), domain.ProviderDeezer); ok {
		t.Fatal("expected a second request to be rejected while the probe is in flight")
	}

	cb.ReleaseProbe(domain.ProviderDeezer, adm)

	if _, ok := cb.AllowRequest(context.Background(), domain.ProviderDeezer); !ok {
		t.Error("expected a released probe slot to admit the next probe")
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
	logging.UseHandler(t, h)
	return h
}

func TestCircuitBreaker_LogsCarryCallerContext(t *testing.T) {
	ctx := context.WithValue(context.Background(), breakerLogKey{}, "marker")
	p := domain.ProviderDeezer

	t.Run("opening logs once with provider and failures from ctx", func(t *testing.T) {
		h := captureBreakerLogs(t)
		cb := service.NewCircuitBreaker(clock.Now)
		tripProvider(ctx, cb, p)
		if got := h.messages(); len(got) != 1 || got[0] != "circuit breaker opened (provider failing)" {
			t.Fatalf("logs = %v, want exactly the opened message", got)
		}
		l := h.logs[0]
		if l.attrs["provider"] != p.String() || l.attrs["failures"] != int64(guardBreakerTripsAfter) {
			t.Errorf("attrs = %v", l.attrs)
		}
		if l.ctxV != "marker" {
			t.Errorf("record ctx value = %v, want the passed ctx", l.ctxV)
		}
	})

	t.Run("no failures logs nothing", func(t *testing.T) {
		h := captureBreakerLogs(t)
		cb := service.NewCircuitBreaker(clock.Now)
		cb.AllowRequest(ctx, p)
		if got := h.messages(); len(got) != 0 {
			t.Fatalf("logs = %v, want none", got)
		}
	})

	t.Run("success on closed breaker logs nothing", func(t *testing.T) {
		h := captureBreakerLogs(t)
		cb := service.NewCircuitBreaker(clock.Now)
		adm, _ := cb.AllowRequest(ctx, p)
		cb.RecordSuccess(ctx, p, adm)
		if got := h.messages(); len(got) != 0 {
			t.Fatalf("logs = %v, want none", got)
		}
	})

	t.Run("recovery logs half-open then closed", func(t *testing.T) {
		h := captureBreakerLogs(t)
		cb := service.NewCircuitBreaker(clock.Now)
		tripProvider(ctx, cb, p)
		cb.AdvanceClock(guardBreakerOpenFor + time.Second)
		adm, ok := cb.AllowRequest(ctx, p)
		if !ok {
			t.Fatal("probe refused after breakerOpenFor")
		}
		cb.RecordSuccess(ctx, p, adm)
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

	t.Run("an expired probe lease logs the re-probe", func(t *testing.T) {
		h := captureBreakerLogs(t)
		cb := service.NewCircuitBreaker(clock.Now)
		tripProvider(ctx, cb, p)
		cb.AdvanceClock(guardBreakerOpenFor + time.Second)
		cb.AllowRequest(ctx, p)
		cb.AdvanceClock(probeLeaseFor + time.Second)
		cb.AllowRequest(ctx, p)
		got := h.messages()
		if last := got[len(got)-1]; last != "circuit breaker probe lease expired (re-probing)" {
			t.Fatalf("logs = %v, want the lease expiry last", got)
		}
	})
}

func TestNewCircuitBreaker_PanicsOnANilClock(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("NewCircuitBreaker(nil) did not panic")
		}
	}()

	service.NewCircuitBreaker(nil)
}

func TestCircuitBreaker_ReopensForProbingOnTheInjectedClock(t *testing.T) {
	manual := clocktest.NewManual(time.Date(2026, time.March, 4, 10, 0, 0, 0, time.UTC))
	cb := service.NewCircuitBreaker(manual.Func())
	tripProvider(t.Context(), cb, domain.ProviderDeezer)
	if _, ok := cb.AllowRequest(t.Context(), domain.ProviderDeezer); ok {
		t.Fatal("an open circuit admitted a request")
	}

	manual.Advance(guardBreakerOpenFor + time.Second)

	if _, ok := cb.AllowRequest(t.Context(), domain.ProviderDeezer); !ok {
		t.Error("the circuit stayed closed to probes after the open window passed on the injected clock")
	}
}
