package providertransport_test

import (
	"altune/go-api/internal/app/providertransport"
	"altune/go-api/internal/platform/outbound/outboundtest"
	"altune/go-api/internal/search"
	"altune/go-api/internal/shared/telemetry"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

func TestNew_BuildsTheLiveTransportCountingIntoTheGivenRegistry(t *testing.T) {
	transport, err := providertransport.New(telemetry.NewRegistry())
	if err != nil {
		t.Fatal(err)
	}

	if transport == nil {
		t.Fatal("New() = nil, want the live transport")
	}
}

func providerCalls(reg *telemetry.Registry) int64 {
	var total int64
	for _, o := range search.ReadProviderMetricsSnapshot(reg) {
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

	reg := telemetry.NewRegistry()
	before := providerCalls(reg)

	transport, err := providertransport.New(reg)
	if err != nil {
		t.Fatal(err)
	}

	resp, err := (&http.Client{Transport: transport}).Get(upstream.URL)
	if err == nil {
		_ = resp.Body.Close()
		t.Fatal("a loopback upstream was fetched through the live provider transport")
	}
	if !errors.Is(err, outboundtest.ErrNonPublicAddr) {
		t.Errorf("error = %v, want the non-public address refusal", err)
	}
	if connections.Load() != 0 {
		t.Errorf("upstream saw %d requests, want 0", connections.Load())
	}
	if counted := providerCalls(reg) - before; counted != 1 {
		t.Errorf("provider counters moved by %d over one refused call, want 1", counted)
	}
}
