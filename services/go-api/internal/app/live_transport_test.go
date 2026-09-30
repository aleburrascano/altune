package app

import (
	"context"
	"errors"
	"io"
	"math/rand/v2"
	"net/http"
	"strings"
	"testing"
	"time"

	"golang.org/x/time/rate"
)

type fakeRT struct {
	calls int
	steps []fakeStep
}

type fakeStep struct {
	status int
	err    error
	header http.Header
}

func (f *fakeRT) RoundTrip(_ *http.Request) (*http.Response, error) {
	i := f.calls
	f.calls++
	if i >= len(f.steps) {
		i = len(f.steps) - 1
	}
	s := f.steps[i]
	if s.err != nil {
		return nil, s.err
	}
	return &http.Response{StatusCode: s.status, Header: s.header, Body: io.NopCloser(strings.NewReader("body"))}, nil
}

func recordDelays(base http.RoundTripper, rec *[]time.Duration) *liveTransport {
	lt := newLiveOver(base)
	lt.sleep = func(_ context.Context, d time.Duration) error {
		*rec = append(*rec, d)
		return nil
	}
	return lt
}

func newLiveOver(base http.RoundTripper) *liveTransport {
	return &liveTransport{base: base, limiters: map[string]*rate.Limiter{}}
}

func getReq(t *testing.T) *http.Request {
	t.Helper()
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, "https://unlisted.example.com/x", nil)
	if err != nil {
		t.Fatal(err)
	}
	return req
}

func TestLiveTransport_RetriesOn503ThenSucceeds(t *testing.T) {
	f := &fakeRT{steps: []fakeStep{{status: 503}, {status: 200}}}
	resp, err := newLiveOver(f).RoundTrip(getReq(t))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
	if f.calls != 2 {
		t.Errorf("calls = %d, want 2 (one retry)", f.calls)
	}
}

func TestLiveTransport_RetriesOn429(t *testing.T) {
	f := &fakeRT{steps: []fakeStep{{status: 429}, {status: 200}}}
	resp, err := newLiveOver(f).RoundTrip(getReq(t))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 || f.calls != 2 {
		t.Errorf("status=%d calls=%d, want 200 and 2", resp.StatusCode, f.calls)
	}
}

func TestLiveTransport_NoRetryOn404(t *testing.T) {
	f := &fakeRT{steps: []fakeStep{{status: 404}}}
	resp, err := newLiveOver(f).RoundTrip(getReq(t))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 404 || f.calls != 1 {
		t.Errorf("status=%d calls=%d, want 404 and 1 (no retry on client error)", resp.StatusCode, f.calls)
	}
}

func TestLiveTransport_ExhaustsOnPersistent503(t *testing.T) {
	f := &fakeRT{steps: []fakeStep{{status: 503}}}
	resp, err := newLiveOver(f).RoundTrip(getReq(t))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 503 {
		t.Errorf("status = %d, want the final 503 surfaced", resp.StatusCode)
	}
	if f.calls != liveMaxAttempts {
		t.Errorf("calls = %d, want %d", f.calls, liveMaxAttempts)
	}
}

func TestLiveTransport_NoRetryOnContextDeadline(t *testing.T) {
	f := &fakeRT{steps: []fakeStep{{err: context.DeadlineExceeded}}}
	resp, err := newLiveOver(f).RoundTrip(getReq(t))
	if resp != nil {
		defer resp.Body.Close()
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v, want context.DeadlineExceeded", err)
	}
	if f.calls != 1 {
		t.Errorf("calls = %d, want 1 (budget gone — no further attempts)", f.calls)
	}
}

func TestLiveTransport_HonorsRetryAfterSeconds(t *testing.T) {
	h := http.Header{"Retry-After": []string{"2"}}
	f := &fakeRT{steps: []fakeStep{{status: 429, header: h}, {status: 200}}}
	var delays []time.Duration
	resp, err := recordDelays(f, &delays).RoundTrip(getReq(t))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
	if len(delays) != 1 {
		t.Fatalf("delays = %v, want exactly one wait", delays)
	}
	if delays[0] != 2*time.Second {
		t.Errorf("delay = %v, want 2s from Retry-After (not the fixed backoff)", delays[0])
	}
}

func TestLiveTransport_HonorsRetryAfterHTTPDate(t *testing.T) {
	when := time.Now().Add(5 * time.Second).UTC().Format(http.TimeFormat)
	h := http.Header{"Retry-After": []string{when}}
	f := &fakeRT{steps: []fakeStep{{status: 503, header: h}, {status: 200}}}
	var delays []time.Duration
	resp, err := recordDelays(f, &delays).RoundTrip(getReq(t))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer resp.Body.Close()
	if len(delays) != 1 {
		t.Fatalf("delays = %v, want exactly one wait", delays)
	}
	if delays[0] < 3*time.Second || delays[0] > 6*time.Second {
		t.Errorf("delay = %v, want roughly 5s from the HTTP-date Retry-After", delays[0])
	}
}

func TestLiveTransport_CapsRetryAfter(t *testing.T) {
	seconds := []string{"100000", "10000000000", "18446744074"}
	for _, secs := range seconds {
		t.Run(secs, func(t *testing.T) {
			h := http.Header{"Retry-After": []string{secs}}
			f := &fakeRT{steps: []fakeStep{{status: 429, header: h}, {status: 200}}}
			var delays []time.Duration
			resp, err := recordDelays(f, &delays).RoundTrip(getReq(t))
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			defer resp.Body.Close()
			if len(delays) != 1 || delays[0] != liveMaxRetryAfter {
				t.Errorf("delays = %v, want a single wait capped at %v", delays, liveMaxRetryAfter)
			}
		})
	}
}

func TestLiveTransport_FallsBackWithoutRetryAfter(t *testing.T) {
	cases := map[string]http.Header{
		"absent":   nil,
		"garbage":  {"Retry-After": []string{"soon"}},
		"negative": {"Retry-After": []string{"-5"}},
	}
	for name, h := range cases {
		t.Run(name, func(t *testing.T) {
			f := &fakeRT{steps: []fakeStep{{status: 429, header: h}, {status: 200}}}
			var delays []time.Duration
			lt := recordDelays(f, &delays)
			lt.randFloat = func() float64 { return 0.5 }
			resp, err := lt.RoundTrip(getReq(t))
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			defer resp.Body.Close()
			base := time.Duration(1) * liveBackoffBase
			if len(delays) != 1 || delays[0] != base {
				t.Errorf("delays = %v, want the fixed backoff base %v", delays, base)
			}
		})
	}
}

func TestLiveTransport_FixedBackoffHasJitter(t *testing.T) {
	lt := newLiveOver(&fakeRT{steps: []fakeStep{{status: 200}}})
	lt.randFloat = rand.New(rand.NewPCG(1, 2)).Float64

	const attempt = 1
	base := time.Duration(attempt) * liveBackoffBase
	spread := time.Duration(liveBackoffJitter * float64(base))
	lo, hi := base-spread, base+spread

	seen := make(map[time.Duration]struct{})
	for i := 0; i < 50; i++ {
		d := lt.fixedBackoff(attempt)
		if d < lo || d > hi {
			t.Fatalf("backoff %v outside bound [%v,%v]", d, lo, hi)
		}
		if d > liveMaxBackoff {
			t.Fatalf("backoff %v exceeds documented max %v", d, liveMaxBackoff)
		}
		seen[d] = struct{}{}
	}
	if len(seen) < 2 {
		t.Fatalf("backoff never varied across 50 calls (no jitter); saw %v", seen)
	}
}

func TestLiveTransport_LimitsMissingProviderHosts(t *testing.T) {
	lt := newLiveOver(&fakeRT{steps: []fakeStep{{status: 200}}})
	hosts := []string{
		"api.deezer.com",
		"api-v2.soundcloud.com",
		"na.web.skill.music.a2z.com",
		"api-partner.spotify.com",
	}
	for _, h := range hosts {
		if lt.limiter(h) == nil {
			t.Errorf("expected a rate limiter for provider host %q", h)
		}
	}
}

func TestBaseTransport_CapsConnsPerHost(t *testing.T) {
	tr, ok := baseTransport().(*http.Transport)
	if !ok {
		t.Fatalf("baseTransport = %T, want *http.Transport", baseTransport())
	}
	if tr.MaxConnsPerHost != liveMaxConnsPerHost {
		t.Errorf("MaxConnsPerHost = %d, want %d", tr.MaxConnsPerHost, liveMaxConnsPerHost)
	}
}

func TestLiveTransport_LimiterPerHost(t *testing.T) {
	lt := newLiveOver(&fakeRT{steps: []fakeStep{{status: 200}}})
	if lt.limiter("musicbrainz.org") == nil {
		t.Error("expected a limiter for a listed host")
	}
	if lt.limiter("unlisted.example.com") != nil {
		t.Error("expected no limiter for an unlisted host")
	}
	first := lt.limiter("musicbrainz.org")
	second := lt.limiter("musicbrainz.org")
	if first != second {
		t.Error("limiter not memoized for a listed host")
	}
}

func TestLiveTransport_RetriesNoBodyGet(t *testing.T) {
	f := &fakeRT{steps: []fakeStep{{status: 503}, {status: 200}}}
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, "https://unlisted.example.com/x", http.NoBody)
	if err != nil {
		t.Fatal(err)
	}
	var delays []time.Duration
	resp, err := recordDelays(f, &delays).RoundTrip(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer resp.Body.Close()
	if f.calls != 2 {
		t.Errorf("calls = %d, want 2 (one retry)", f.calls)
	}
	if req.Body != http.NoBody {
		t.Errorf("retried body = %v, want http.NoBody", req.Body)
	}
}

func TestLiveTransport_RetryRefusesUnreplayableBody(t *testing.T) {
	f := &fakeRT{steps: []fakeStep{{status: 503}, {status: 200}}}
	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, "https://unlisted.example.com/x", io.NopCloser(strings.NewReader("payload")))
	if err != nil {
		t.Fatal(err)
	}
	var delays []time.Duration
	resp, err := recordDelays(f, &delays).RoundTrip(req)
	if err == nil {
		resp.Body.Close()
		t.Fatal("expected an error for a body that cannot be replayed")
	}
	if f.calls != 1 {
		t.Errorf("calls = %d, want 1 (no retry)", f.calls)
	}
}

func TestLiveTransport_MusicBrainzAllowsNoBurst(t *testing.T) {
	lt := newLiveOver(&fakeRT{steps: []fakeStep{{status: 200}}})
	if got := lt.limiter("musicbrainz.org").Burst(); got != 1 {
		t.Errorf("musicbrainz burst = %d, want 1", got)
	}
	if got := lt.limiter("api.deezer.com").Burst(); got != defaultProviderBurst {
		t.Errorf("deezer burst = %d, want %d", got, defaultProviderBurst)
	}
}

var wiredProviderHosts = []string{
	"musicbrainz.org",
	"itunes.apple.com",
	"ws.audioscrobbler.com",
	"music.youtube.com",
	"api.discogs.com",
	"api.deezer.com",
	"api-v2.soundcloud.com",
	"na.web.skill.music.a2z.com",
	"api-partner.spotify.com",
	"api.genius.com",
	"webservice.fanart.tv",
	"coverartarchive.org",
	"api.music.apple.com",
	"open.spotify.com",
	"auth.deezer.com",
	"pipe.deezer.com",
	"theaudiodb.com",
}

func TestLiveTransport_EveryWiredProviderHostHasLimiter(t *testing.T) {
	lt := newLiveOver(&fakeRT{steps: []fakeStep{{status: 200}}})
	for _, h := range wiredProviderHosts {
		if lt.limiter(h) == nil {
			t.Errorf("wired provider host %q has no rate limit", h)
		}
	}
}

func TestLiveTransport_PacedHostRetries429ThenSucceeds(t *testing.T) {
	for _, h := range wiredProviderHosts[9:] {
		t.Run(h, func(t *testing.T) {
			rt := &fakeRT{steps: []fakeStep{{status: 429}, {status: 200}}}
			lt := recordDelays(rt, &[]time.Duration{})
			req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, "https://"+h+"/x", nil)
			if err != nil {
				t.Fatal(err)
			}
			resp, err := lt.RoundTrip(req)
			if err != nil {
				t.Fatal(err)
			}
			_ = resp.Body.Close()
			if resp.StatusCode != 200 || rt.calls != 2 {
				t.Fatalf("status=%d calls=%d, want 200 after 2 calls", resp.StatusCode, rt.calls)
			}
			if lt.limiter(h) == nil {
				t.Fatalf("host %q is unpaced", h)
			}
		})
	}
}
