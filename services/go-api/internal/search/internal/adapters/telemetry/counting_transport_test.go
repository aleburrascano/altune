package telemetry_test

import (
	"altune/go-api/internal/search"
	searchtelemetry "altune/go-api/internal/search/internal/adapters/telemetry"
	"altune/go-api/internal/search/internal/ports"
	"altune/go-api/internal/shared/telemetry"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

type stubTransport struct {
	status  int
	err     error
	lastReq *http.Request
}

var providerLabels = []string{
	"deezer", "spotify", "soundcloud", "applemusic", "itunes", "amazonmusic", "youtube", "musicbrainz",
	"lastfm", "discogs", "genius", "theaudiodb", "fanart", "sheets", "other",
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

func roundTrip(t *testing.T, reg *telemetry.Registry, base http.RoundTripper, rawURL string) (*http.Response, error) {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, rawURL, nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	return search.NewCountingTransport(base, reg).RoundTrip(req)
}

func delta(before, after searchtelemetry.Outcomes) searchtelemetry.Outcomes {
	return searchtelemetry.Outcomes{
		OK:    after.OK - before.OK,
		Quota: after.Quota - before.Quota,
		Error: after.Error - before.Error,
	}
}

func TestCountsPerProviderAndOutcome(t *testing.T) {
	reg := telemetry.NewRegistry()
	cases := []struct {
		name         string
		url          string
		status       int
		err          error
		wantProvider string
		want         searchtelemetry.Outcomes
	}{
		{"deezer 2xx ok", "https://api.deezer.com/search?q=secret", 200, nil, "deezer", searchtelemetry.Outcomes{OK: 1}},
		{"spotify 3xx ok", "https://api.spotify.com/v1/x", 302, nil, "spotify", searchtelemetry.Outcomes{OK: 1}},
		{"soundcloud 429 quota", "https://api-v2.soundcloud.com/tracks", 429, nil, "soundcloud", searchtelemetry.Outcomes{Quota: 1}},
		{"applemusic 403 error", "https://api.music.apple.com/v1/x", 403, nil, "applemusic", searchtelemetry.Outcomes{Error: 1}},
		{"applemusic 404 error", "https://api.music.apple.com/v1/x", 404, nil, "applemusic", searchtelemetry.Outcomes{Error: 1}},
		{"applemusic 429 quota", "https://api.music.apple.com/v1/x", 429, nil, "applemusic", searchtelemetry.Outcomes{Quota: 1}},
		{"itunes search is not applemusic", "https://itunes.apple.com/search?term=x", 200, nil, "itunes", searchtelemetry.Outcomes{OK: 1}},
		{"amazon 5xx error", "https://music.amazon.com/x", 503, nil, "amazonmusic", searchtelemetry.Outcomes{Error: 1}},
		{"youtube transport error", "https://music.youtube.com/x", 0, errors.New("dial fail"), "youtube", searchtelemetry.Outcomes{Error: 1}},
		{"musicbrainz 2xx ok", "https://musicbrainz.org/ws/2/x", 200, nil, "musicbrainz", searchtelemetry.Outcomes{OK: 1}},
		{"a request shed by the pacing queue never reached the provider", "https://musicbrainz.org/ws/2/x", 0, ports.ErrProviderRateLimitQueueTimeout, "musicbrainz", searchtelemetry.Outcomes{}},
		{"lastfm 2xx ok", "https://ws.audioscrobbler.com/2.0/", 200, nil, "lastfm", searchtelemetry.Outcomes{OK: 1}},
		{"lastfm image cdn host is lastfm", "https://lastfm.freetls.fastly.net/i/u/300x300/x.jpg", 200, nil, "lastfm", searchtelemetry.Outcomes{OK: 1}},
		{"another tenant of the same cdn is other", "https://deezer.freetls.fastly.net/x.jpg", 200, nil, "other", searchtelemetry.Outcomes{OK: 1}},
		{"unknown host is other", "https://example.com/x", 200, nil, "other", searchtelemetry.Outcomes{OK: 1}},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			before := search.ReadProviderMetricsSnapshot(reg)
			resp, err := roundTrip(t, reg, &stubTransport{status: c.status, err: c.err}, c.url)
			if resp != nil && resp.Body != nil {
				_ = resp.Body.Close()
			}
			if c.err != nil && !errors.Is(err, c.err) {
				t.Fatalf("err = %v, want %v", err, c.err)
			}
			got := delta(before[c.wantProvider], search.ReadProviderMetricsSnapshot(reg)[c.wantProvider])
			if got != c.want {
				t.Fatalf("provider %q delta = %+v, want %+v", c.wantProvider, got, c.want)
			}
		})
	}
}

func TestTransparentDelegate(t *testing.T) {
	reg := telemetry.NewRegistry()
	t.Run("response and request pass through unchanged", func(t *testing.T) {
		stub := &stubTransport{status: http.StatusCreated}
		req, _ := http.NewRequest(http.MethodGet, "https://api.deezer.com/x?q=y", nil)
		resp, err := search.NewCountingTransport(stub, reg).RoundTrip(req)
		if err != nil {
			t.Fatalf("err = %v, want nil", err)
		}
		defer func() { _ = resp.Body.Close() }()
		if resp.StatusCode != http.StatusCreated {
			t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusCreated)
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
		resp, err := search.NewCountingTransport(&stubTransport{err: sentinel}, reg).RoundTrip(
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

func TestSnapshotHasNoPII(t *testing.T) {
	reg := telemetry.NewRegistry()
	const secret = "topsecretquery"
	resp, _ := roundTrip(t, reg, &stubTransport{status: 200},
		"https://api.deezer.com/search?q="+secret+"&user=alice@example.com")
	if resp != nil && resp.Body != nil {
		_ = resp.Body.Close()
	}

	allowed := map[string]bool{}
	for _, p := range providerLabels {
		allowed[p] = true
	}
	for key := range search.ReadProviderMetricsSnapshot(reg) {
		if !allowed[key] {
			t.Errorf("snapshot key %q is not a fixed provider label", key)
		}
		if strings.Contains(key, secret) || strings.Contains(key, "alice") ||
			strings.Contains(key, "?") || strings.Contains(key, "/") {
			t.Errorf("snapshot key %q leaks URL/query text", key)
		}
	}
}

func TestKeySetIsBounded(t *testing.T) {
	reg := telemetry.NewRegistry()
	for _, host := range []string{"https://a.example", "https://b.invalid", "https://c.test"} {
		resp, _ := roundTrip(t, reg, &stubTransport{status: 200}, host+"/x")
		if resp != nil && resp.Body != nil {
			_ = resp.Body.Close()
		}
	}
	if got := len(search.ReadProviderMetricsSnapshot(reg)); got != len(providerLabels) {
		t.Fatalf("snapshot has %d keys, want fixed %d", got, len(providerLabels))
	}
}

func TestConcurrentRoundTripsCountExactly(t *testing.T) {
	reg := telemetry.NewRegistry()
	const goroutines, perGoroutine = 16, 64
	before := search.ReadProviderMetricsSnapshot(reg)["deezer"]

	var wg sync.WaitGroup
	wg.Add(goroutines)
	for g := 0; g < goroutines; g++ {
		go func() {
			defer wg.Done()
			ct := search.NewCountingTransport(&stubTransport{status: 200}, reg)
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

	got := delta(before, search.ReadProviderMetricsSnapshot(reg)["deezer"])
	if want := (searchtelemetry.Outcomes{OK: goroutines * perGoroutine}); got != want {
		t.Fatalf("concurrent deezer delta = %+v, want %+v", got, want)
	}
}

func TestNilBaseDoesNotPanic(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "https://127.0.0.1:0/x", nil)
	resp, err := search.NewCountingTransport(nil, telemetry.NewRegistry()).RoundTrip(req)
	if resp != nil {
		_ = resp.Body.Close()
	}
	if err == nil {
		t.Skip("unexpected success dialing unroutable host; nil-base path still exercised")
	}
}

func TestMetadataProvidersGetTheirOwnBucket(t *testing.T) {
	cases := []struct {
		url      string
		provider string
	}{
		{"https://api.discogs.com/database/search", "discogs"},
		{"https://i.discogs.com/x.jpg", "discogs"},
		{"https://api.genius.com/search", "genius"},
		{"https://www.theaudiodb.com/api/v1/json/2/search.php", "theaudiodb"},
		{"https://theaudiodb.com/api/v1/json/2/search.php", "theaudiodb"},
		{"https://webservice.fanart.tv/v3/music/x", "fanart"},
		{"https://sheets.googleapis.com/v4/spreadsheets/x", "sheets"},
	}
	for _, c := range cases {
		t.Run(c.url, func(t *testing.T) {
			reg := telemetry.NewRegistry()
			before := search.ReadProviderMetricsSnapshot(reg)
			if _, ok := before[c.provider]; !ok {
				t.Fatalf("snapshot has no %q key", c.provider)
			}
			resp, _ := roundTrip(t, reg, &stubTransport{status: 200}, c.url)
			if resp != nil && resp.Body != nil {
				_ = resp.Body.Close()
			}
			after := search.ReadProviderMetricsSnapshot(reg)
			if got := delta(before[c.provider], after[c.provider]); got != (searchtelemetry.Outcomes{OK: 1}) {
				t.Errorf("provider %q delta = %+v, want {OK:1}", c.provider, got)
			}
			if got := delta(before["other"], after["other"]); got != (searchtelemetry.Outcomes{}) {
				t.Errorf("other delta = %+v, want unchanged", got)
			}
		})
	}
}

func TestRetryRecorder_CountsPerProviderHost(t *testing.T) {
	reg := telemetry.NewRegistry()
	before := search.ReadProviderMetricsSnapshot(reg)

	record := search.ProviderRetryRecorder(reg)
	record("api.deezer.com")
	record("api.deezer.com")
	record("musicbrainz.org")

	after := search.ReadProviderMetricsSnapshot(reg)
	if got := after["deezer"].Retries - before["deezer"].Retries; got != 2 {
		t.Errorf("deezer retries moved by %d, want 2", got)
	}
	if got := after["musicbrainz"].Retries - before["musicbrainz"].Retries; got != 1 {
		t.Errorf("musicbrainz retries moved by %d, want 1", got)
	}
	if got := after["spotify"].Retries - before["spotify"].Retries; got != 0 {
		t.Errorf("spotify retries moved by %d, want 0", got)
	}
}
