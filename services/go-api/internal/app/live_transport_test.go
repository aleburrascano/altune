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
	if resp.StatusCode != 503 {
		t.Errorf("status = %d, want the final 503 surfaced", resp.StatusCode)
	}
	if f.calls != liveMaxAttempts {
		t.Errorf("calls = %d, want %d", f.calls, liveMaxAttempts)
	}
}

func TestLiveTransport_NoRetryOnContextDeadline(t *testing.T) {
	f := &fakeRT{steps: []fakeStep{{err: context.DeadlineExceeded}}}
	_, err := newLiveOver(f).RoundTrip(getReq(t))
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v, want context.DeadlineExceeded", err)
	}
	if f.calls != 1 {
		t.Errorf("calls = %d, want 1 (budget gone — no further attempts)", f.calls)
	}
}

// TestLiveTransport_HonorsRetryAfterSeconds is the repro: a 429 carrying a
// delta-seconds Retry-After must wait that value, not the fixed backoff.
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

// TestLiveTransport_HonorsRetryAfterHTTPDate covers the HTTP-date format.
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

// TestLiveTransport_CapsRetryAfter ensures a huge value is clamped.
func TestLiveTransport_CapsRetryAfter(t *testing.T) {
	h := http.Header{"Retry-After": []string{"100000"}}
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
}

// TestLiveTransport_FallsBackWithoutRetryAfter keeps the fixed backoff when the
// header is absent or unparseable.
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
			lt.randFloat = func() float64 { return 0.5 } // midpoint: no jitter offset
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

// TestLiveTransport_FixedBackoffHasJitter is the regression for the lockstep
// bug: repeated backoffs at the same attempt must not all be identical, yet
// every draw must stay within the +/-liveBackoffJitter bound and never exceed
// the documented max.
func TestLiveTransport_FixedBackoffHasJitter(t *testing.T) {
	lt := newLiveOver(&fakeRT{steps: []fakeStep{{status: 200}}})
	lt.randFloat = rand.New(rand.NewPCG(1, 2)).Float64 // seeded: deterministic yet varied

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

func TestLiveTransport_LimiterPerHost(t *testing.T) {
	lt := newLiveOver(&fakeRT{steps: []fakeStep{{status: 200}}})
	if lt.limiter("musicbrainz.org") == nil {
		t.Error("expected a limiter for a listed host")
	}
	if lt.limiter("unlisted.example.com") != nil {
		t.Error("expected no limiter for an unlisted host")
	}
	if lt.limiter("musicbrainz.org") != lt.limiter("musicbrainz.org") {
		t.Error("limiter not memoized for a listed host")
	}
}
