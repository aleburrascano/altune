package egress_test

import (
	"altune/go-api/internal/shared/egress"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

func TestRefuseNonPublicDial_RefusesEveryNonPublicRange(t *testing.T) {
	nonPublic := []string{
		"10.0.0.1:80",
		"172.16.0.1:80",
		"192.168.1.1:80",
		"127.0.0.1:80",
		"169.254.169.254:80",
		"0.1.2.3:80",
		"100.64.0.1:80",
		"192.0.0.8:80",
		"198.18.0.1:80",
		"240.0.0.1:80",
		"255.255.255.255:80",
		"224.0.0.1:80",
		"[::1]:80",
		"[::]:80",
		"[fe80::1%eth0]:80",
		"[fc00::1]:80",
		"[::ffff:10.0.0.1]:80",
		"[::a00:1]:80",
		"[64:ff9b::a00:1]:80",
		"[64:ff9b:1::1]:80",
		"[2002::1]:80",
		"not-an-address",
		"example.com:443",
	}
	for _, addr := range nonPublic {
		if err := egress.RefuseNonPublicDial("tcp", addr, nil); !errors.Is(err, egress.ErrNonPublicAddr) {
			t.Errorf("%s: err = %v, want refusal", addr, err)
		}
	}
}

func TestRefuseNonPublicDial_AllowsPublicAddresses(t *testing.T) {
	for _, addr := range []string{"93.184.216.34:443", "[2606:2800:220:1:248:1893:25c8:1946]:443", "[::ffff:93.184.216.34]:443"} {
		if err := egress.RefuseNonPublicDial("tcp", addr, nil); err != nil {
			t.Errorf("%s: public address refused: %v", addr, err)
		}
	}
}

func TestNewLiveTransport_RefusesToDialALoopbackUpstreamAndRecordsEachRetry(t *testing.T) {
	var requests atomic.Int64
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()
	var retried []string
	rt := egress.NewLiveTransport(egress.WithRetryRecorder(func(host string) { retried = append(retried, host) }))
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, upstream.URL, nil)
	if err != nil {
		t.Fatal(err)
	}

	resp, err := rt.RoundTrip(req)
	if resp != nil {
		_ = resp.Body.Close()
	}

	if !errors.Is(err, egress.ErrNonPublicAddr) {
		t.Fatalf("err = %v, want the non-public address refusal", err)
	}
	if requests.Load() != 0 {
		t.Errorf("upstream saw %d requests, want 0", requests.Load())
	}
	if len(retried) != 2 || retried[0] != "127.0.0.1" {
		t.Errorf("retry recorder saw %v, want the two retries of the refused dial", retried)
	}
}

func TestNewLiveTransport_WorksWithoutARetryRecorder(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {}))
	defer upstream.Close()
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, upstream.URL, nil)
	if err != nil {
		t.Fatal(err)
	}

	resp, err := egress.NewLiveTransport().RoundTrip(req)
	if resp != nil {
		_ = resp.Body.Close()
	}

	if !errors.Is(err, egress.ErrNonPublicAddr) {
		t.Fatalf("err = %v, want the non-public address refusal", err)
	}
}
