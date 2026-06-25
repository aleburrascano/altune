package app

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"golang.org/x/time/rate"
)

// fakeRT returns a programmed sequence of (status, err) per attempt and counts
// calls. The last entry repeats if more attempts occur.
type fakeRT struct {
	calls int
	steps []fakeStep
}

type fakeStep struct {
	status int
	err    error
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
	return &http.Response{StatusCode: s.status, Body: io.NopCloser(strings.NewReader("body"))}, nil
}

func newLiveOver(base http.RoundTripper) *liveTransport {
	return &liveTransport{base: base, limiters: map[string]*rate.Limiter{}}
}

func getReq(t *testing.T) *http.Request {
	t.Helper()
	// unlisted host → no rate-limit delay, so retry tests only pay backoff.
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
	// After exhausting retries the real upstream response is returned (not a
	// synthetic error) — the adapter sees the 503 and handles it. What matters is
	// that we tried exactly liveMaxAttempts times.
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

func TestLiveTransport_LimiterPerHost(t *testing.T) {
	lt := newLiveOver(&fakeRT{steps: []fakeStep{{status: 200}}})
	if lt.limiter("musicbrainz.org") == nil {
		t.Error("expected a limiter for a listed host")
	}
	if lt.limiter("unlisted.example.com") != nil {
		t.Error("expected no limiter for an unlisted host")
	}
	// memoized: same instance back.
	if lt.limiter("musicbrainz.org") != lt.limiter("musicbrainz.org") {
		t.Error("limiter not memoized for a listed host")
	}
}
