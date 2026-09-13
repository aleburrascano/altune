package httptrace

import (
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
)

const fakeKey = "0123456789abcdef0123456789abcdef"

type errRT struct{ err error }

func (e errRT) RoundTrip(*http.Request) (*http.Response, error) { return nil, e.err }

type okRT struct{}

func (okRT) RoundTrip(req *http.Request) (*http.Response, error) {
	return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader("ok")), Request: req}, nil
}

// Regression for #642: the recorder feeds fixture files written to disk, so a
// query-param secret must never survive into Exchange.URL or Exchange.Err.
func TestRecorder_RedactsSecretInURLAndErr(t *testing.T) {
	errMsg := `Get "https://ws.audioscrobbler.com/2.0/?api_key=` + fakeKey + `&method=x": dial tcp: i/o timeout`
	rec := NewRecorder(errRT{err: errors.New(errMsg)})

	req, err := http.NewRequest(http.MethodGet, "https://ws.audioscrobbler.com/2.0/?method=track.search&api_key="+fakeKey, nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, rtErr := rec.RoundTrip(req)
	if resp != nil {
		_ = resp.Body.Close()
	}
	if rtErr == nil {
		t.Fatal("expected the transport error to propagate")
	}

	ex := rec.Exchanges()[0]
	if strings.Contains(ex.URL, fakeKey) {
		t.Errorf("Exchange.URL leaked the api key: %q", ex.URL)
	}
	if strings.Contains(ex.Err, fakeKey) {
		t.Errorf("Exchange.Err leaked the api key: %q", ex.Err)
	}
	if !strings.Contains(ex.URL, "api_key=REDACTED") || !strings.Contains(ex.URL, "method=track.search") {
		t.Errorf("URL shape not kept: %q", ex.URL)
	}
}

func TestRecorder_RedactsSecretInURLOnSuccess(t *testing.T) {
	rec := NewRecorder(okRT{})
	resp := mustGet(t, rec, "http://fixture.local/?key="+fakeKey)
	defer resp.Body.Close()

	ex := rec.Exchanges()[0]
	if strings.Contains(ex.URL, fakeKey) {
		t.Errorf("Exchange.URL leaked the key: %q", ex.URL)
	}
}

// A fixture recorded with a redacted URL must still replay for a live request
// that carries the real key, or redaction would break record/replay.
func TestReplayer_MatchesRedactedFixtureForKeyedRequest(t *testing.T) {
	const url = "https://ws.audioscrobbler.com/2.0/?method=x&api_key=" + fakeKey
	rec := NewRecorder(okRT{})
	recorded := mustGet(t, rec, url)
	defer recorded.Body.Close()

	rep := NewReplayer(rec.Exchanges())
	replayed := mustGet(t, rep, url)
	defer replayed.Body.Close()
	if got := bodyString(t, replayed); got != "ok" {
		t.Errorf("replay body = %q, want ok", got)
	}
}
