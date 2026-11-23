package providerhttp_test

import (
	"altune/go-api/internal/app/providerhttp"
	"altune/go-api/internal/discovery"
	"altune/go-api/internal/shared/egress"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

func TestLive_IsOneSharedTransport(t *testing.T) {
	first, second := providerhttp.Live(), providerhttp.Live()

	if first == nil {
		t.Fatal("Live() = nil, want the live transport")
	}
	if first != second {
		t.Error("Live() returned two transports; per-host rate limiters are no longer shared")
	}
}

func providerCalls() int64 {
	var total int64
	for _, o := range discovery.ReadProviderMetricsSnapshot() {
		total += o.OK + o.Quota + o.Error
	}
	return total
}

func TestLive_RefusesALoopbackUpstreamAndCountsTheCall(t *testing.T) {
	var connections atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		connections.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	before := providerCalls()

	resp, err := (&http.Client{Transport: providerhttp.Live()}).Get(upstream.URL)
	if err == nil {
		_ = resp.Body.Close()
		t.Fatal("a loopback upstream was fetched through the live provider transport")
	}
	if !errors.Is(err, egress.ErrNonPublicAddr) {
		t.Errorf("error = %v, want the non-public address refusal", err)
	}
	if connections.Load() != 0 {
		t.Errorf("upstream saw %d requests, want 0", connections.Load())
	}
	if counted := providerCalls() - before; counted != 1 {
		t.Errorf("provider counters moved by %d over one refused call, want 1", counted)
	}
}
