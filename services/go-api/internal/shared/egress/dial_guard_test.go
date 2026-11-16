package egress

import (
	"errors"
	"net/http"
	"testing"
	"time"
)

func TestBaseTransportKeepsItsConnectionCapAndRefusesNonPublicAddresses(t *testing.T) {
	tr, ok := baseTransport().(*http.Transport)
	if !ok {
		t.Fatal("baseTransport is not an *http.Transport")
	}
	if tr.MaxConnsPerHost != liveMaxConnsPerHost {
		t.Errorf("MaxConnsPerHost = %d, want %d", tr.MaxConnsPerHost, liveMaxConnsPerHost)
	}
	for _, addr := range []string{"10.0.0.1:80", "169.254.169.254:80", "[::1]:80", "100.64.0.1:80"} {
		if err := RefuseNonPublicDial("tcp", addr, nil); !errors.Is(err, ErrNonPublicAddr) {
			t.Errorf("%s: err = %v, want refusal", addr, err)
		}
	}
	if err := RefuseNonPublicDial("tcp", "93.184.216.34:443", nil); err != nil {
		t.Errorf("public address refused: %v", err)
	}
}

func TestProviderDialerGivesUpConnectingAfter30sAndKeepsConnectionsAlive(t *testing.T) {
	d := providerDialer()
	if d.Timeout != 30*time.Second || d.KeepAlive != 30*time.Second {
		t.Errorf("Timeout=%v KeepAlive=%v, want 30s each", d.Timeout, d.KeepAlive)
	}
	if d.Control == nil {
		t.Error("dialer has no Control: it would dial non-public addresses")
	}
}
