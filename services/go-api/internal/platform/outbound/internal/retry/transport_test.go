package retry_test

import (
	"altune/go-api/internal/platform/outbound/internal/retry"
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"math/rand/v2"
	"net"
	"net/http"
	"strings"
	"sync"
	"testing"
	"testing/synctest"
	"time"
)

type fakeRT struct {
	calls  int
	bodies []string
	steps  []fakeStep
}

type fakeStep struct {
	status int
	err    error
	header http.Header
	before func()
}

func (f *fakeRT) RoundTrip(req *http.Request) (*http.Response, error) {
	i := min(f.calls, len(f.steps)-1)
	f.calls++
	if req.Body != nil {
		raw, _ := io.ReadAll(req.Body)
		f.bodies = append(f.bodies, string(raw))
	}
	s := f.steps[i]
	if s.before != nil {
		s.before()
	}
	if s.err != nil {
		return nil, s.err
	}
	return &http.Response{StatusCode: s.status, Header: s.header, Body: io.NopCloser(strings.NewReader("body"))}, nil
}

func recordingSleep(rec *[]time.Duration) func(context.Context, time.Duration) error {
	return func(_ context.Context, d time.Duration) error {
		*rec = append(*rec, d)
		return nil
	}
}

func recordingDelays(base http.RoundTripper, rec *[]time.Duration) *retry.Transport {
	return retry.New(base, retry.Options{Sleep: recordingSleep(rec)})
}

func getReq(t *testing.T) *http.Request {
	t.Helper()
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, "https://unlisted.example.com/x", nil)
	if err != nil {
		t.Fatal(err)
	}
	return req
}

func statusOf(t *testing.T, rt http.RoundTripper, req *http.Request) int {
	t.Helper()
	resp, err := rt.RoundTrip(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	_ = resp.Body.Close()
	return resp.StatusCode
}

func TestTransport_RetriesOn503ThenSucceedsOnTheRealTimer(t *testing.T) {
	f := &fakeRT{steps: []fakeStep{{status: 503}, {status: 200}}}

	status := statusOf(t, retry.New(f, retry.Options{}), getReq(t))

	if status != 200 || f.calls != 2 {
		t.Errorf("status=%d calls=%d, want 200 after one retry", status, f.calls)
	}
}

func TestTransport_RetriesOn429And500(t *testing.T) {
	for _, first := range []int{429, 500} {
		f := &fakeRT{steps: []fakeStep{{status: first}, {status: 200}}}
		var delays []time.Duration

		status := statusOf(t, recordingDelays(f, &delays), getReq(t))

		if status != 200 || f.calls != 2 || len(delays) != 1 {
			t.Errorf("%d: status=%d calls=%d waits=%d, want 200 after one retry", first, status, f.calls, len(delays))
		}
	}
}

func TestTransport_NoRetryOnClientErrors(t *testing.T) {
	for _, clientError := range []int{404, 499} {
		f := &fakeRT{steps: []fakeStep{{status: clientError}}}

		status := statusOf(t, recordingDelays(f, &[]time.Duration{}), getReq(t))

		if status != clientError || f.calls != 1 {
			t.Errorf("status=%d calls=%d, want %d and 1 (no retry on a client error)", status, f.calls, clientError)
		}
	}
}

func TestTransport_ExhaustsOnPersistent503(t *testing.T) {
	f := &fakeRT{steps: []fakeStep{{status: 503}}}
	var delays []time.Duration

	status := statusOf(t, recordingDelays(f, &delays), getReq(t))

	if status != 503 {
		t.Errorf("status = %d, want the final 503 surfaced", status)
	}
	if f.calls != 3 || len(delays) != 2 {
		t.Errorf("calls=%d waits=%d, want 3 attempts with 2 waits", f.calls, len(delays))
	}
}

func TestTransport_GivesUpAfterThreeTransportErrors(t *testing.T) {
	refused := &net.OpError{Op: "dial", Err: errors.New("connection refused")}
	f := &fakeRT{steps: []fakeStep{{err: refused}}}
	var delays []time.Duration

	resp, err := recordingDelays(f, &delays).RoundTrip(getReq(t))
	if resp != nil {
		defer resp.Body.Close()
	}

	if !errors.Is(err, refused) {
		t.Fatalf("err = %v, want the last transport error", err)
	}
	if f.calls != 3 || len(delays) != 2 {
		t.Errorf("calls=%d waits=%d, want 3 attempts with 2 waits between them", f.calls, len(delays))
	}
}

func TestTransport_RecoversFromATransportError(t *testing.T) {
	f := &fakeRT{steps: []fakeStep{{err: errors.New("reset")}, {status: 200}}}

	status := statusOf(t, recordingDelays(f, &[]time.Duration{}), getReq(t))

	if status != 200 || f.calls != 2 {
		t.Errorf("status=%d calls=%d, want 200 after one retry", status, f.calls)
	}
}

func TestTransport_BackoffGrowsWithEachAttemptAndItsJitter(t *testing.T) {
	cases := map[float64][]time.Duration{
		0.25: {225 * time.Millisecond, 450 * time.Millisecond},
		0.5:  {250 * time.Millisecond, 500 * time.Millisecond},
		0.75: {275 * time.Millisecond, 550 * time.Millisecond},
	}
	for draw, want := range cases {
		f := &fakeRT{steps: []fakeStep{{status: 503}, {status: 503}, {status: 200}}}
		var delays []time.Duration
		rt := retry.New(f, retry.Options{Sleep: recordingSleep(&delays), Jitter: func() float64 { return draw }})

		statusOf(t, rt, getReq(t))

		if len(delays) != 2 || delays[0] != want[0] || delays[1] != want[1] {
			t.Errorf("draw %v: delays = %v, want %v", draw, delays, want)
		}
	}
}

func TestTransport_AnUpstreamThatNeverRecoversIsTriedThreeTimesWithEveryWaitPinned(t *testing.T) {
	f := &fakeRT{steps: []fakeStep{{status: 503}}}
	var delays []time.Duration
	rt := retry.New(f, retry.Options{Sleep: recordingSleep(&delays), Jitter: func() float64 { return 0.5 }})

	statusOf(t, rt, getReq(t))

	want := []time.Duration{250 * time.Millisecond, 500 * time.Millisecond}
	if f.calls != 3 || len(delays) != len(want) || delays[0] != want[0] || delays[1] != want[1] {
		t.Errorf("calls = %d, delays = %v, want 3 calls and exactly the waits %v; a change to the attempt limit must change this test", f.calls, delays, want)
	}
}

func TestTransport_DefaultJitterVariesTheBackoffWithinItsBound(t *testing.T) {
	const base = 250 * time.Millisecond
	spread := base / 5
	seen := map[time.Duration]struct{}{}
	for range 50 {
		var delays []time.Duration
		f := &fakeRT{steps: []fakeStep{{status: 503}, {status: 200}}}

		statusOf(t, recordingDelays(f, &delays), getReq(t))

		if delays[0] < base-spread || delays[0] > base+spread {
			t.Fatalf("backoff %v outside bound [%v,%v]", delays[0], base-spread, base+spread)
		}
		seen[delays[0]] = struct{}{}
	}
	if len(seen) < 2 {
		t.Fatalf("backoff never varied across 50 retries (no jitter); saw %v", seen)
	}
}

func TestTransport_SeededJitterStaysWithinItsBound(t *testing.T) {
	const base = 250 * time.Millisecond
	spread := base / 5
	rng := rand.New(rand.NewPCG(1, 2))
	for range 50 {
		var delays []time.Duration
		f := &fakeRT{steps: []fakeStep{{status: 503}, {status: 200}}}
		rt := retry.New(f, retry.Options{Sleep: recordingSleep(&delays), Jitter: rng.Float64})

		statusOf(t, rt, getReq(t))

		if delays[0] < base-spread || delays[0] > base+spread {
			t.Fatalf("backoff %v outside bound [%v,%v]", delays[0], base-spread, base+spread)
		}
	}
}

func TestTransport_NoRetryOnContextDeadlineOrCancel(t *testing.T) {
	for _, final := range []error{context.DeadlineExceeded, context.Canceled} {
		f := &fakeRT{steps: []fakeStep{{err: final}}}

		resp, err := recordingDelays(f, &[]time.Duration{}).RoundTrip(getReq(t))
		if resp != nil {
			defer resp.Body.Close()
		}

		if !errors.Is(err, final) {
			t.Fatalf("err = %v, want %v", err, final)
		}
		if f.calls != 1 {
			t.Errorf("%v: calls = %d, want 1 (budget gone, no further attempts)", final, f.calls)
		}
	}
}

func TestTransport_TheRealTimerStopsWaitingWhenTheRequestIsCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	f := &fakeRT{steps: []fakeStep{{status: 503, before: cancel}, {status: 200}}}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://unlisted.example.com/x", nil)
	if err != nil {
		t.Fatal(err)
	}

	resp, err := retry.New(f, retry.Options{}).RoundTrip(req)
	if resp != nil {
		defer resp.Body.Close()
	}

	if !errors.Is(err, context.Canceled) || f.calls != 1 {
		t.Fatalf("err=%v calls=%d, want context.Canceled after the first attempt", err, f.calls)
	}
}

func TestTransport_AFailedWaitEndsTheRoundTrip(t *testing.T) {
	waitFailed := errors.New("wait failed")
	f := &fakeRT{steps: []fakeStep{{status: 503}, {status: 200}}}
	rt := retry.New(f, retry.Options{Sleep: func(context.Context, time.Duration) error { return waitFailed }})

	resp, err := rt.RoundTrip(getReq(t))
	if resp != nil {
		defer resp.Body.Close()
	}

	if !errors.Is(err, waitFailed) || f.calls != 1 {
		t.Fatalf("err=%v calls=%d, want the wait error after one attempt", err, f.calls)
	}
}

func retryAfterDelays(t *testing.T, header http.Header) []time.Duration {
	t.Helper()
	f := &fakeRT{steps: []fakeStep{{status: 429, header: header}, {status: 200}}}
	var delays []time.Duration
	rt := retry.New(f, retry.Options{Sleep: recordingSleep(&delays), Jitter: func() float64 { return 0.5 }})
	statusOf(t, rt, getReq(t))
	return delays
}

func TestTransport_HonorsRetryAfterSeconds(t *testing.T) {
	delays := retryAfterDelays(t, http.Header{"Retry-After": []string{" 2 "}})

	if len(delays) != 1 || delays[0] != 2*time.Second {
		t.Errorf("delays = %v, want one 2s wait from Retry-After (not the fixed backoff)", delays)
	}
}

func TestTransport_HonorsRetryAfterHTTPDate(t *testing.T) {
	when := time.Now().Add(5 * time.Second).UTC().Format(http.TimeFormat)

	delays := retryAfterDelays(t, http.Header{"Retry-After": []string{when}})

	if len(delays) != 1 || delays[0] < 3*time.Second || delays[0] > 6*time.Second {
		t.Errorf("delays = %v, want one wait of roughly 5s from the HTTP-date Retry-After", delays)
	}
}

func TestTransport_CapsRetryAfter(t *testing.T) {
	far := time.Now().Add(time.Hour).UTC().Format(http.TimeFormat)
	for _, value := range []string{"31", "100000", "10000000000", "18446744074", far} {
		delays := retryAfterDelays(t, http.Header{"Retry-After": []string{value}})

		if len(delays) != 1 || delays[0] != 30*time.Second {
			t.Errorf("Retry-After %q: delays = %v, want a single wait capped at 30s", value, delays)
		}
	}
}

func TestTransport_KeepsARetryAfterAtTheCap(t *testing.T) {
	delays := retryAfterDelays(t, http.Header{"Retry-After": []string{"30"}})

	if len(delays) != 1 || delays[0] != 30*time.Second {
		t.Errorf("delays = %v, want a single 30s wait", delays)
	}
}

func TestTransport_FallsBackWithoutAUsableRetryAfter(t *testing.T) {
	past := time.Now().Add(-time.Hour).UTC().Format(http.TimeFormat)
	cases := map[string]http.Header{
		"absent":   nil,
		"blank":    {"Retry-After": []string{"  "}},
		"garbage":  {"Retry-After": []string{"soon"}},
		"negative": {"Retry-After": []string{"-5"}},
		"zero":     {"Retry-After": []string{"0"}},
		"past":     {"Retry-After": []string{past}},
	}
	for name, h := range cases {
		delays := retryAfterDelays(t, h)

		if len(delays) != 1 || delays[0] != 250*time.Millisecond {
			t.Errorf("%s: delays = %v, want the fixed backoff base 250ms", name, delays)
		}
	}
}

func TestTransport_RetriesNoBodyGet(t *testing.T) {
	f := &fakeRT{steps: []fakeStep{{status: 503}, {status: 200}}}
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, "https://unlisted.example.com/x", http.NoBody)
	if err != nil {
		t.Fatal(err)
	}

	statusOf(t, recordingDelays(f, &[]time.Duration{}), req)

	if f.calls != 2 {
		t.Errorf("calls = %d, want 2 (one retry)", f.calls)
	}
	if req.Body != http.NoBody {
		t.Errorf("retried body = %v, want http.NoBody", req.Body)
	}
}

func TestTransport_ResendsAReplayableBodyOnRetry(t *testing.T) {
	f := &fakeRT{steps: []fakeStep{{status: 503}, {status: 200}}}
	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, "https://unlisted.example.com/x", bytes.NewReader([]byte("payload")))
	if err != nil {
		t.Fatal(err)
	}

	statusOf(t, recordingDelays(f, &[]time.Duration{}), req)

	if len(f.bodies) != 2 || f.bodies[0] != "payload" || f.bodies[1] != "payload" {
		t.Errorf("bodies sent = %q, want the payload on both attempts", f.bodies)
	}
}

func TestTransport_RetryRefusesUnreplayableBody(t *testing.T) {
	f := &fakeRT{steps: []fakeStep{{status: 503}, {status: 200}}}
	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, "https://unlisted.example.com/x", io.NopCloser(strings.NewReader("payload")))
	if err != nil {
		t.Fatal(err)
	}

	resp, err := recordingDelays(f, &[]time.Duration{}).RoundTrip(req)
	if err == nil {
		resp.Body.Close()
		t.Fatal("expected an error for a body that cannot be replayed")
	}
	if f.calls != 1 {
		t.Errorf("calls = %d, want 1 (no retry)", f.calls)
	}
}

const musicBrainzPacingGap = time.Second

func musicBrainzReq(t *testing.T) *http.Request {
	t.Helper()
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, "https://musicbrainz.org/x", nil)
	if err != nil {
		t.Fatal(err)
	}
	return req
}

func TestTransport_PacesAProviderHostBeforeEveryAttempt(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		f := &fakeRT{steps: []fakeStep{{status: 503}, {status: 200}}}
		rt := retry.New(f, retry.Options{Sleep: recordingSleep(&[]time.Duration{})})
		start := time.Now()

		status := statusOf(t, rt, musicBrainzReq(t))

		if waited := time.Since(start); status != 200 || waited != musicBrainzPacingGap {
			t.Fatalf("status=%d after %v, want 200 with the retry held %v for musicbrainz.org's pace", status, waited, musicBrainzPacingGap)
		}
	})
}

func TestTransport_APacingFailureIsFinal(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		f := &fakeRT{steps: []fakeStep{{status: 200}, {status: 200}}}
		retried := 0
		rt := retry.New(f, retry.Options{Sleep: recordingSleep(&[]time.Duration{}), OnRetry: func(string) { retried++ }})
		statusOf(t, rt, musicBrainzReq(t))
		ctx, cancel := context.WithTimeout(context.Background(), musicBrainzPacingGap/2)
		defer cancel()

		resp, err := rt.RoundTrip(musicBrainzReq(t).WithContext(ctx))
		if resp != nil {
			defer resp.Body.Close()
		}

		if err == nil || f.calls != 1 || retried != 0 {
			t.Fatalf("err=%v calls=%d retries=%d, want the pacing refusal with no second request and no retry", err, f.calls, retried)
		}
	})
}

func TestTransport_RetriesA429OnAPacedProviderHost(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		f := &fakeRT{steps: []fakeStep{{status: 429}, {status: 200}}}
		rt := retry.New(f, retry.Options{Sleep: recordingSleep(&[]time.Duration{})})

		status := statusOf(t, rt, musicBrainzReq(t))

		if status != 200 || f.calls != 2 {
			t.Fatalf("status=%d calls=%d, want 200 after 2 calls", status, f.calls)
		}
	})
}

func TestTransport_LeavesAnUnlistedHostUnpaced(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		f := &fakeRT{steps: []fakeStep{{status: 503}, {status: 503}, {status: 200}}}
		rt := retry.New(f, retry.Options{Sleep: recordingSleep(&[]time.Duration{})})
		start := time.Now()

		statusOf(t, rt, getReq(t))

		if waited := time.Since(start); waited != 0 {
			t.Fatalf("an unlisted host waited %v across its attempts, want no pacing", waited)
		}
	})
}

type retryLogRecorder struct {
	mu    sync.Mutex
	warns []map[string]string
}

func (h *retryLogRecorder) Enabled(context.Context, slog.Level) bool { return true }

func (h *retryLogRecorder) Handle(_ context.Context, r slog.Record) error {
	if r.Level != slog.LevelWarn {
		return nil
	}
	attrs := map[string]string{"msg": r.Message}
	r.Attrs(func(a slog.Attr) bool {
		attrs[a.Key] = a.Value.String()
		return true
	})
	h.mu.Lock()
	defer h.mu.Unlock()
	h.warns = append(h.warns, attrs)
	return nil
}

func (h *retryLogRecorder) WithAttrs([]slog.Attr) slog.Handler { return h }

func (h *retryLogRecorder) WithGroup(string) slog.Handler { return h }

func captureRetryLogs(t *testing.T) *retryLogRecorder {
	t.Helper()
	rec := &retryLogRecorder{}
	prev := slog.Default()
	slog.SetDefault(slog.New(rec))
	t.Cleanup(func() { slog.SetDefault(prev) })
	return rec
}

const retrySecretQuery = "q=private+search+text"

func secretReq(t *testing.T) *http.Request {
	t.Helper()
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet,
		"https://api.deezer.com/search/private-path?"+retrySecretQuery, nil)
	if err != nil {
		t.Fatal(err)
	}
	return req
}

func roundTripRecordingRetries(t *testing.T, steps []fakeStep) []string {
	t.Helper()
	var retried []string
	rt := retry.New(&fakeRT{steps: steps}, retry.Options{
		Sleep:   recordingSleep(&[]time.Duration{}),
		OnRetry: func(host string) { retried = append(retried, host) },
	})
	statusOf(t, rt, secretReq(t))
	return retried
}

func TestTransport_LogsAndCountsEachRetriedAttempt(t *testing.T) {
	logs := captureRetryLogs(t)

	retried := roundTripRecordingRetries(t, []fakeStep{{status: 503}, {status: 429}, {status: 200}})

	if len(logs.warns) != 2 {
		t.Fatalf("got %d Warn records, want 2 (one per retried attempt): %v", len(logs.warns), logs.warns)
	}
	for i, want := range []struct{ attempt, status string }{{"2", "503"}, {"3", "429"}} {
		w := logs.warns[i]
		if w["msg"] != "provider.retry" || w["host"] != "api.deezer.com" || w["attempt"] != want.attempt || w["status"] != want.status || w["delay"] == "" {
			t.Errorf("warn %d = %v, want provider.retry host=api.deezer.com attempt=%s status=%s with a delay", i, w, want.attempt, want.status)
		}
	}
	if len(retried) != 2 || retried[0] != "api.deezer.com" || retried[1] != "api.deezer.com" {
		t.Errorf("retry recorder saw %v, want two retries on api.deezer.com", retried)
	}
}

type timeoutError struct{}

func (timeoutError) Error() string   { return "i/o timeout" }
func (timeoutError) Timeout() bool   { return true }
func (timeoutError) Temporary() bool { return true }

func TestTransport_RetryAfterTransportErrorNamesTheErrorClass(t *testing.T) {
	cases := map[string]error{
		"dial":      &net.OpError{Op: "dial", Net: "tcp", Err: errors.New("connection refused")},
		"timeout":   &net.OpError{Op: "read", Net: "tcp", Err: timeoutError{}},
		"transport": errors.New("connection reset"),
	}
	for class, transportErr := range cases {
		logs := captureRetryLogs(t)

		roundTripRecordingRetries(t, []fakeStep{{err: transportErr}, {status: 200}})

		if len(logs.warns) != 1 || logs.warns[0]["error_class"] != class {
			t.Errorf("warns = %v, want one with error_class=%s", logs.warns, class)
		}
		if _, ok := logs.warns[0]["status"]; ok {
			t.Errorf("warn %v names a status for a transport error", logs.warns[0])
		}
	}
}

func TestTransport_RetryLogCarriesNoPathOrQuery(t *testing.T) {
	logs := captureRetryLogs(t)

	roundTripRecordingRetries(t, []fakeStep{{status: 503}, {status: 200}})

	if len(logs.warns) == 0 {
		t.Fatal("no retry Warn written")
	}
	for _, w := range logs.warns {
		for k, v := range w {
			if strings.Contains(v, "private") || strings.Contains(v, "/search") {
				t.Errorf("retry log attr %s=%q leaks the URL path or query", k, v)
			}
		}
	}
}

func TestTransport_FirstAttemptSuccessLogsNothing(t *testing.T) {
	logs := captureRetryLogs(t)

	retried := roundTripRecordingRetries(t, []fakeStep{{status: 200}})

	if len(logs.warns) != 0 || len(retried) != 0 {
		t.Fatalf("first-attempt success wrote %d Warn records and %d retries, want none", len(logs.warns), len(retried))
	}
}
