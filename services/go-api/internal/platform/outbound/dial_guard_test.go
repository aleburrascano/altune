package outbound_test

import (
	"altune/go-api/internal/platform/outbound"
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"syscall"
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
		if err := outbound.RefuseNonPublicDial("tcp", addr, nil); !errors.Is(err, outbound.ErrNonPublicAddr) {
			t.Errorf("%s: err = %v, want refusal", addr, err)
		}
	}
}

func TestRefuseNonPublicDial_AllowsPublicAddresses(t *testing.T) {
	for _, addr := range []string{"93.184.216.34:443", "[2606:2800:220:1:248:1893:25c8:1946]:443", "[::ffff:93.184.216.34]:443"} {
		if err := outbound.RefuseNonPublicDial("tcp", addr, nil); err != nil {
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
	rt := outbound.NewLiveTransport(func(host string) { retried = append(retried, host) })
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, upstream.URL, nil)
	if err != nil {
		t.Fatal(err)
	}

	resp, err := rt.RoundTrip(req)
	if resp != nil {
		_ = resp.Body.Close()
	}

	if !errors.Is(err, outbound.ErrNonPublicAddr) {
		t.Fatalf("err = %v, want the non-public address refusal", err)
	}
	if requests.Load() != 0 {
		t.Errorf("upstream saw %d requests, want 0", requests.Load())
	}
	if len(retried) != 2 || retried[0] != "127.0.0.1" {
		t.Errorf("retry recorder saw %v, want the two retries of the refused dial", retried)
	}
}

func TestNewLiveTransport_RefusesAMissingRetryRecorder(t *testing.T) {
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, "https://example.test/", nil)
	if err != nil {
		t.Fatal(err)
	}

	resp, err := outbound.NewLiveTransport(nil).RoundTrip(req)
	if resp != nil {
		_ = resp.Body.Close()
	}

	if !errors.Is(err, outbound.ErrRetryRecorderRequired) {
		t.Fatalf("err = %v, want the missing retry recorder refusal", err)
	}
}

func TestNewLiveTransport_WorksWithTheNoRetryRecorder(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {}))
	defer upstream.Close()
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, upstream.URL, nil)
	if err != nil {
		t.Fatal(err)
	}

	resp, err := outbound.NewLiveTransport(outbound.NoRetryRecorder).RoundTrip(req)
	if resp != nil {
		_ = resp.Body.Close()
	}

	if !errors.Is(err, outbound.ErrNonPublicAddr) {
		t.Fatalf("err = %v, want the non-public address refusal", err)
	}
}

func allowLoopbackThenGuard(ctx context.Context, network, address string) (net.Conn, error) {
	guarded := &net.Dialer{Control: func(network, address string, c syscall.RawConn) error {
		if strings.HasPrefix(address, "127.0.0.1:") {
			return nil
		}
		return outbound.RefuseNonPublicDial(network, address, c)
	}}
	return guarded.DialContext(ctx, network, address)
}

func TestRefuseNonPublicDial_RefusesARedirectToTheMetadataAddress(t *testing.T) {
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
	if !errors.Is(err, outbound.ErrNonPublicAddr) {
		t.Errorf("error = %v, want the non-public address refusal", err)
	}
}
