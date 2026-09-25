package providermetrics

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// stubTransport returns a canned response/error and captures the last request
// it saw, standing in for a real network transport.
type stubTransport struct {
	status  int
	err     error
	lastReq *http.Request
}

func (s *stubTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	s.lastReq = req
	if s.err != nil {
		return nil, s.err
	}
	return &http.Response{
		StatusCode: s.status,
		Body:       io.NopCloser(strings.NewReader("body")),
		Header:     make(http.Header),
		Request:    req,
	}, nil
}

func roundTrip(t *testing.T, base http.RoundTripper, rawURL string) (*http.Response, error) {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, rawURL, nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	return NewCountingTransport(base).RoundTrip(req)
}

func delta(before, after Outcomes) Outcomes {
	return Outcomes{
		OK:    after.OK - before.OK,
		Quota: after.Quota - before.Quota,
		Error: after.Error - before.Error,
	}
}

// TestCountsPerProviderAndOutcome drives one round trip per (host, status) case
// and asserts exactly the matching (provider, outcome) counter advanced by one.
func TestCountsPerProviderAndOutcome(t *testing.T) {
	cases := []struct {
		name         string
		url          string
		status       int
		err          error
		wantProvider string
		want         Outcomes
	}{
		{"deezer 2xx ok", "https://api.deezer.com/search?q=secret", 200, nil, providerDeezer, Outcomes{OK: 1}},
		{"spotify 3xx ok", "https://api.spotify.com/v1/x", 302, nil, providerSpotify, Outcomes{OK: 1}},
		{"soundcloud 429 quota", "https://api-v2.soundcloud.com/tracks", 429, nil, providerSoundCloud, Outcomes{Quota: 1}},
		{"applemusic 403 error", "https://api.music.apple.com/v1/x", 403, nil, providerAppleMusic, Outcomes{Error: 1}},
		{"applemusic 404 error", "https://api.music.apple.com/v1/x", 404, nil, providerAppleMusic, Outcomes{Error: 1}},
		{"applemusic 429 quota", "https://api.music.apple.com/v1/x", 429, nil, providerAppleMusic, Outcomes{Quota: 1}},
		{"itunes search is not applemusic", "https://itunes.apple.com/search?term=x", 200, nil, providerITunes, Outcomes{OK: 1}},
		{"amazon 5xx error", "https://music.amazon.com/x", 503, nil, providerAmazonMusic, Outcomes{Error: 1}},
		{"youtube transport error", "https://music.youtube.com/x", 0, errors.New("dial fail"), providerYouTube, Outcomes{Error: 1}},
		{"musicbrainz 2xx ok", "https://musicbrainz.org/ws/2/x", 200, nil, providerMusicBrainz, Outcomes{OK: 1}},
		{"lastfm 2xx ok", "https://ws.audioscrobbler.com/2.0/", 200, nil, providerLastFM, Outcomes{OK: 1}},
		{"lastfm image cdn host is lastfm", "https://lastfm.freetls.fastly.net/i/u/300x300/x.jpg", 200, nil, providerLastFM, Outcomes{OK: 1}},
		{"another tenant of the same cdn is other", "https://deezer.freetls.fastly.net/x.jpg", 200, nil, providerOther, Outcomes{OK: 1}},
		{"unknown host is other", "https://example.com/x", 200, nil, providerOther, Outcomes{OK: 1}},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			before := ReadSnapshot()
			resp, err := roundTrip(t, &stubTransport{status: c.status, err: c.err}, c.url)
			if resp != nil && resp.Body != nil {
				_ = resp.Body.Close()
			}
			if c.err != nil && !errors.Is(err, c.err) {
				t.Fatalf("err = %v, want %v", err, c.err)
			}
			got := delta(before[c.wantProvider], ReadSnapshot()[c.wantProvider])
			if got != c.want {
				t.Fatalf("provider %q delta = %+v, want %+v", c.wantProvider, got, c.want)
			}
		})
	}
}

// TestTransparentDelegate proves the wrap alters neither the response nor the
// error, nor mutates the outgoing request.
func TestTransparentDelegate(t *testing.T) {
	t.Run("response and request pass through unchanged", func(t *testing.T) {
		stub := &stubTransport{status: 201}
		req, _ := http.NewRequest(http.MethodGet, "https://api.deezer.com/x?q=y", nil)
		resp, err := NewCountingTransport(stub).RoundTrip(req)
		if err != nil {
			t.Fatalf("err = %v, want nil", err)
		}
		defer func() { _ = resp.Body.Close() }()
		if resp.StatusCode != 201 {
			t.Errorf("status = %d, want 201", resp.StatusCode)
		}
		if stub.lastReq != req {
			t.Errorf("request was replaced; delegate must pass the same *http.Request")
		}
		if stub.lastReq.URL.RawQuery != "q=y" {
			t.Errorf("request query mutated: %q", stub.lastReq.URL.RawQuery)
		}
	})

	t.Run("error passes through verbatim", func(t *testing.T) {
		sentinel := errors.New("boom")
		resp, err := NewCountingTransport(&stubTransport{err: sentinel}).RoundTrip(
			httptest.NewRequest(http.MethodGet, "https://api.deezer.com/x", nil))
		if resp != nil {
			_ = resp.Body.Close()
		}
		if !errors.Is(err, sentinel) {
			t.Errorf("err = %v, want %v", err, sentinel)
		}
		if resp != nil {
			t.Errorf("resp = %v, want nil on transport error", resp)
		}
	})
}

// TestSnapshotHasNoPII proves the snapshot exposes only fixed provider labels —
// never a host, URL, or query — even after a request carrying query text.
func TestSnapshotHasNoPII(t *testing.T) {
	const secret = "topsecretquery"
	resp, _ := roundTrip(t, &stubTransport{status: 200},
		"https://api.deezer.com/search?q="+secret+"&user=alice@example.com")
	if resp != nil && resp.Body != nil {
		_ = resp.Body.Close()
	}

	allowed := map[string]bool{}
	for _, p := range providerNames {
		allowed[p] = true
	}
	for key := range ReadSnapshot() {
		if !allowed[key] {
			t.Errorf("snapshot key %q is not a fixed provider label", key)
		}
		if strings.Contains(key, secret) || strings.Contains(key, "alice") ||
			strings.Contains(key, "?") || strings.Contains(key, "/") {
			t.Errorf("snapshot key %q leaks URL/query text", key)
		}
	}
}

// TestKeySetIsBounded proves unknown hosts never grow new keys: the snapshot
// always has exactly the fixed provider set, regardless of hosts seen.
func TestKeySetIsBounded(t *testing.T) {
	for _, host := range []string{"https://a.example", "https://b.invalid", "https://c.test"} {
		resp, _ := roundTrip(t, &stubTransport{status: 200}, host+"/x")
		if resp != nil && resp.Body != nil {
			_ = resp.Body.Close()
		}
	}
	if got := len(ReadSnapshot()); got != len(providerNames) {
		t.Fatalf("snapshot has %d keys, want fixed %d", got, len(providerNames))
	}
}

// TestConcurrentRoundTripsCountExactly fires many parallel round trips through
// one shared CountingTransport and asserts the deltas sum to exactly the work
// done — under -race this pins the assembled-feature claim that the process-
// global counters are safe under concurrent provider traffic.
func TestConcurrentRoundTripsCountExactly(t *testing.T) {
	const goroutines, perGoroutine = 16, 64
	before := ReadSnapshot()[providerDeezer]

	var wg sync.WaitGroup
	wg.Add(goroutines)
	for g := 0; g < goroutines; g++ {
		go func() {
			defer wg.Done()
			// Each goroutine owns its transport so the only shared state under
			// test is the process-global counters the wrap increments.
			ct := NewCountingTransport(&stubTransport{status: 200})
			for i := 0; i < perGoroutine; i++ {
				req := httptest.NewRequest(http.MethodGet, "https://api.deezer.com/x", nil)
				resp, err := ct.RoundTrip(req)
				if err != nil {
					t.Errorf("RoundTrip: %v", err)
					return
				}
				_ = resp.Body.Close()
			}
		}()
	}
	wg.Wait()

	got := delta(before, ReadSnapshot()[providerDeezer])
	if want := (Outcomes{OK: goroutines * perGoroutine}); got != want {
		t.Fatalf("concurrent deezer delta = %+v, want %+v", got, want)
	}
}

func TestNilBaseDoesNotPanic(t *testing.T) {
	// A nil base must fall back to http.DefaultTransport rather than panic; the
	// request to an unroutable host errors, which is fine — we assert no panic.
	req := httptest.NewRequest(http.MethodGet, "https://127.0.0.1:0/x", nil)
	resp, err := (&CountingTransport{}).RoundTrip(req)
	if resp != nil {
		_ = resp.Body.Close()
	}
	if err == nil {
		t.Skip("unexpected success dialing unroutable host; nil-base path still exercised")
	}
}
