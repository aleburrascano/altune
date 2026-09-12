package requeststore

import (
	"altune/go-api/internal/shared/httputil"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
)

const lastfmKey = "0123456789abcdef0123456789abcdef"

func TestRedactSecrets_masksValuesKeepsShape(t *testing.T) {
	in := "https://ws.audioscrobbler.com/2.0/?method=track.search&api_key=" + lastfmKey + "&format=json"
	got := RedactSecrets(in)
	if strings.Contains(got, lastfmKey) {
		t.Fatalf("raw key leaked: %q", got)
	}
	if !strings.Contains(got, "api_key=REDACTED") {
		t.Errorf("api_key value not redacted: %q", got)
	}
	if !strings.Contains(got, "method=track.search") || !strings.Contains(got, "format=json") {
		t.Errorf("non-secret params or path lost: %q", got)
	}
}

func TestTransport_RedactsSecretInCapturedURL(t *testing.T) {
	s := New()
	base := respWith("ok")
	t.Cleanup(func() { _ = base.Body.Close() })
	rt := NewCorrelatedTransport(fakeRT{resp: base}, s)

	r, err := http.NewRequest("GET", "https://ws.audioscrobbler.com/2.0/?method=x&api_key="+lastfmKey, nil)
	if err != nil {
		t.Fatal(err)
	}
	r = r.WithContext(httputil.WithCorrelationID(r.Context(), "c1"))

	resp, err := rt.RoundTrip(r)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = io.ReadAll(resp.Body)
	_ = resp.Body.Close()

	rec, _ := s.Get("c1")
	if strings.Contains(rec.Exchanges[0].URL, lastfmKey) {
		t.Fatalf("captured Exchange.URL leaked the api key: %q", rec.Exchanges[0].URL)
	}
}

func TestRerunRecorder_SanitizesErrorURL(t *testing.T) {
	errMsg := `Get "https://ws.audioscrobbler.com/2.0/?api_key=` + lastfmKey + `&method=x": dial tcp: i/o timeout`
	rr := NewRerunRecorder(fakeRT{err: errors.New(errMsg)}, 1024)

	req, err := http.NewRequest("GET", "https://ws.audioscrobbler.com/2.0/?api_key="+lastfmKey, nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, rtErr := rr.RoundTrip(req)
	if resp != nil {
		_ = resp.Body.Close()
	}
	if rtErr == nil {
		t.Fatal("expected the transport error to propagate")
	}

	ex := rr.Exchanges()[0]
	if strings.Contains(ex.Err, lastfmKey) || strings.Contains(ex.URL, lastfmKey) {
		t.Fatalf("rerun exchange leaked the api key: url=%q err=%q", ex.URL, ex.Err)
	}
}
