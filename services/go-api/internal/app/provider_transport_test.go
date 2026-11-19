package app

import (
	"altune/go-api/internal/discovery"
	"altune/go-api/internal/shared/config"
	"altune/go-api/internal/shared/egress"
	"context"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"syscall"
	"testing"
)

func TestNilTransportResolvesToOneSharedLiveTransport(t *testing.T) {
	first := newClientFactory(nil).roundTripper()
	second := newClientFactory(nil).roundTripper()

	if first == nil {
		t.Fatal("a factory over a nil transport must carry the live transport, not nil")
	}
	if first != second {
		t.Error("nil-transport factories hold different transports; per-host rate limiters are no longer shared")
	}
}

func TestTheDefaultTransportCountsEveryProviderCall(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	before := discovery.ReadProviderMetricsSnapshot()
	counting := countedProviderTransport(&http.Transport{DialContext: allowLoopbackThenGuard})
	resp, err := newClientFactory(counting).discovery().Get(upstream.URL)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	_ = resp.Body.Close()

	counted := totalProviderCounts(discovery.ReadProviderMetricsSnapshot()) - totalProviderCounts(before)
	if counted != 1 {
		t.Errorf("provider counters moved by %d over one call on the default transport, want 1", counted)
	}
}

func TestDiscoveryClientRefusesALoopbackUpstream(t *testing.T) {
	connections := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		connections++
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	resp, err := newClientFactory(nil).discovery().Get(upstream.URL)
	if err == nil {
		_ = resp.Body.Close()
		t.Fatal("a loopback upstream was fetched through the discovery client")
	}
	if !errors.Is(err, egress.ErrNonPublicAddr) {
		t.Errorf("error = %v, want the non-public address refusal", err)
	}
	if connections != 0 {
		t.Errorf("upstream saw %d requests, want 0", connections)
	}
}

func allowLoopbackThenGuard(ctx context.Context, network, address string) (net.Conn, error) {
	guarded := &net.Dialer{Control: func(network, address string, c syscall.RawConn) error {
		if strings.HasPrefix(address, "127.0.0.1:") {
			return nil
		}
		return egress.RefuseNonPublicDial(network, address, c)
	}}
	return guarded.DialContext(ctx, network, address)
}

func TestDiscoveryClientRefusesARedirectToTheMetadataAddress(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "http://169.254.169.254/latest/meta-data/", http.StatusFound)
	}))
	defer upstream.Close()
	client := &http.Client{Transport: &http.Transport{DialContext: allowLoopbackThenGuard}}

	resp, err := client.Get(upstream.URL)
	if err == nil {
		_ = resp.Body.Close()
		t.Fatal("the redirect to the metadata address was followed")
	}
	if !errors.Is(err, egress.ErrNonPublicAddr) {
		t.Errorf("error = %v, want the non-public address refusal", err)
	}
}

type warnRecorder struct {
	mu      sync.Mutex
	records []slog.Record
}

func (h *warnRecorder) Enabled(context.Context, slog.Level) bool { return true }

func (h *warnRecorder) Handle(_ context.Context, r slog.Record) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.records = append(h.records, r.Clone())
	return nil
}

func (h *warnRecorder) WithAttrs([]slog.Attr) slog.Handler { return h }

func (h *warnRecorder) WithGroup(string) slog.Handler { return h }

func (h *warnRecorder) warnsNaming(flag string) []map[string]string {
	h.mu.Lock()
	defer h.mu.Unlock()
	var out []map[string]string
	for _, r := range h.records {
		attrs := map[string]string{}
		r.Attrs(func(a slog.Attr) bool {
			attrs[a.Key] = a.Value.String()
			return true
		})
		if r.Level == slog.LevelWarn && attrs["flag"] == flag {
			out = append(out, attrs)
		}
	}
	return out
}

func recordWarnings(t *testing.T) *warnRecorder {
	t.Helper()
	rec := &warnRecorder{}
	prev := slog.Default()
	slog.SetDefault(slog.New(rec))
	t.Cleanup(func() { slog.SetDefault(prev) })
	return rec
}

func TestWarnIgnoredStartupOptIns_NamesEachIgnoredHook(t *testing.T) {
	cases := []struct {
		name string
		cfg  config.Config
		flag string
		want int
	}{
		{"test auth in production", config.Config{Env: "production", TestAuthOptIn: true}, "TEST_AUTH_ENABLED", 1},
		{"test auth off", config.Config{Env: "production"}, "TEST_AUTH_ENABLED", 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := recordWarnings(t)

			warnIgnoredStartupOptIns(t.Context(), &tc.cfg)

			if got := len(rec.warnsNaming(tc.flag)); got != tc.want {
				t.Fatalf("got %d %s warnings, want %d", got, tc.flag, tc.want)
			}
		})
	}
}
