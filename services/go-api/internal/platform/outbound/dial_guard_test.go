package outbound_test

import (
	"altune/go-api/internal/platform/outbound"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
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
		"[fe80::1%25eth0]:80",
		"[fc00::1]:80",
		"[::ffff:10.0.0.1]:80",
		"[::a00:1]:80",
		"[64:ff9b::a00:1]:80",
		"[64:ff9b:1::1]:80",
		"[2002::1]:80",
	}
	rt := liveTransport(t)
	for _, addr := range nonPublic {
		t.Run(addr, func(t *testing.T) {
			t.Parallel()
			if err := roundTrip(t, rt, addr, 5*time.Second); !errors.Is(err, outbound.ErrNonPublicAddr) {
				t.Errorf("err = %v, want refusal", err)
			}
		})
	}
}

func TestRefuseNonPublicDial_AllowsAddressesTheGuardTreatsAsPublic(t *testing.T) {
	rt := liveTransport(t)
	for _, addr := range []string{"192.0.2.1:9", "198.51.100.1:9", "203.0.113.1:9", "[2001:db8::1]:9", "[::ffff:192.0.2.1]:9"} {
		if err := roundTrip(t, rt, addr, 100*time.Millisecond); errors.Is(err, outbound.ErrNonPublicAddr) {
			t.Errorf("%s: public address refused: %v", addr, err)
		}
	}
}

func liveTransport(t *testing.T) http.RoundTripper {
	t.Helper()
	rt, err := outbound.NewLiveTransport(func(string) {})
	if err != nil {
		t.Fatal(err)
	}
	return rt
}

func roundTrip(t *testing.T, rt http.RoundTripper, hostPort string, within time.Duration) error {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), within)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+hostPort+"/", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := rt.RoundTrip(req)
	if resp != nil {
		_ = resp.Body.Close()
	}
	return err
}

func TestNewLiveTransport_RefusesToDialALoopbackUpstreamAndRecordsEachRetry(t *testing.T) {
	var requests atomic.Int64
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()
	var retried []string
	rt, err := outbound.NewLiveTransport(func(host string) { retried = append(retried, host) })
	if err != nil {
		t.Fatal(err)
	}
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
	rt, err := outbound.NewLiveTransport(nil)

	if err == nil || !strings.Contains(err.Error(), "live transport needs a retry recorder") {
		t.Fatalf("err = %v, want the missing retry recorder refusal", err)
	}
	if rt != nil {
		t.Errorf("transport = %v, want none", rt)
	}
}

func TestNewLiveTransport_WorksWithAnIgnoringRetryRecorder(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {}))
	defer upstream.Close()
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, upstream.URL, nil)
	if err != nil {
		t.Fatal(err)
	}

	rt, err := outbound.NewLiveTransport(func(string) {})
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
}

type loopbackThenGuard struct {
	guarded http.RoundTripper
}

func (l loopbackThenGuard) RoundTrip(req *http.Request) (*http.Response, error) {
	if strings.HasPrefix(req.URL.Host, "127.0.0.1:") {
		return http.DefaultTransport.RoundTrip(req)
	}
	return l.guarded.RoundTrip(req)
}

func TestRefuseNonPublicDial_RefusesARedirectToTheMetadataAddress(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "http://169.254.169.254/latest/meta-data/", http.StatusFound)
	}))
	defer upstream.Close()
	client := &http.Client{Transport: loopbackThenGuard{guarded: liveTransport(t)}}

	resp, err := client.Get(upstream.URL)
	if err == nil {
		_ = resp.Body.Close()
		t.Fatal("the redirect to the metadata address was followed")
	}
	if !errors.Is(err, outbound.ErrNonPublicAddr) {
		t.Errorf("error = %v, want the non-public address refusal", err)
	}
}
