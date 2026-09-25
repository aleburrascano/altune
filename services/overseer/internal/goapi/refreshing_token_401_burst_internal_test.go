package goapi

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

const burstWaitLimit = 5 * time.Second

func awaitClosed(r *http.Request, ch <-chan struct{}) bool {
	select {
	case <-ch:
		return true
	case <-r.Context().Done():
		return false
	case <-time.After(burstWaitLimit):
		return false
	}
}

type staleTokenBurstAPI struct {
	stale     string
	burst     int64
	calls     atomic.Int64
	arrivals  atomic.Int64
	allStale  chan struct{}
	freshSeen chan struct{}
	freshOnce sync.Once
}

func newStaleTokenBurstAPI(stale string, burst int64) *staleTokenBurstAPI {
	return &staleTokenBurstAPI{
		stale:     stale,
		burst:     burst,
		allStale:  make(chan struct{}),
		freshSeen: make(chan struct{}),
	}
}

func (a *staleTokenBurstAPI) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	a.calls.Add(1)
	if presentedToken(r) != a.stale {
		a.freshOnce.Do(func() { close(a.freshSeen) })
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"db":"ok","redis":"ok","auth":"ok"}`)
		return
	}
	arrival := a.arrivals.Add(1)
	if arrival == a.burst {
		close(a.allStale)
	}
	if !awaitClosed(r, a.allStale) {
		w.WriteHeader(http.StatusGatewayTimeout)
		return
	}
	if arrival != 1 && !awaitClosed(r, a.freshSeen) {
		w.WriteHeader(http.StatusGatewayTimeout)
		return
	}
	w.WriteHeader(http.StatusUnauthorized)
}

func TestConcurrent401sOnOneTokenShareOneRefreshExchange(t *testing.T) {
	const burst = 10
	stub := &rtsStub{clock: rtsClock(), lifetime: time.Hour}
	src, _ := rtsNewSource(t, stub)
	stale, err := src.Token(context.Background())
	if err != nil {
		t.Fatalf("prime token: %v", err)
	}
	api := newStaleTokenBurstAPI(stale, burst)
	apiSrv := httptest.NewServer(api)
	t.Cleanup(apiSrv.Close)
	client, err := New(apiSrv.URL, src)
	if err != nil {
		t.Fatalf("New client: %v", err)
	}

	errs := make([]error, burst)
	var wg sync.WaitGroup
	for i := range burst {
		wg.Go(func() {
			_, errs[i] = client.AdminHealth(context.Background())
		})
	}
	wg.Wait()

	for i, readErr := range errs {
		if readErr != nil {
			t.Fatalf("request %d: %v", i, readErr)
		}
	}
	if got := stub.calls.Load(); got != 2 {
		t.Fatalf("token exchanges = %d, want 2 (prime + one refresh for the whole 401 burst)", got)
	}
	if got := api.calls.Load(); got != 2*burst {
		t.Fatalf("go-api calls = %d, want %d (each request retried at most once)", got, 2*burst)
	}
}

func TestThrottled429TakesNoRetryAndClassifiesThrottled(t *testing.T) {
	stub := &rtsStub{clock: rtsClock(), lifetime: time.Hour}
	src, _ := rtsNewSource(t, stub)
	var apiCalls atomic.Int64
	apiSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		apiCalls.Add(1)
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	t.Cleanup(apiSrv.Close)
	client, err := New(apiSrv.URL, src)
	if err != nil {
		t.Fatalf("New client: %v", err)
	}

	_, err = client.AdminHealth(context.Background())

	if reason := Classify(err); reason != ReasonThrottled {
		t.Fatalf("Classify(%v) = %q, want %q", err, reason, ReasonThrottled)
	}
	if got := apiCalls.Load(); got != 1 {
		t.Fatalf("go-api calls = %d, want 1 (a 429 is never retried)", got)
	}
	if got := stub.calls.Load(); got != 1 {
		t.Fatalf("token exchanges = %d, want 1 (a 429 never invalidates the token)", got)
	}
}

func TestLate401OnSupersededTokenKeepsTheFreshToken(t *testing.T) {
	stub := &rtsStub{clock: rtsClock(), lifetime: time.Hour}
	src, _ := rtsNewSource(t, stub)
	stale, _ := src.Token(context.Background())
	invalidateOn401(src, http.StatusUnauthorized, stale)
	fresh, _ := src.Token(context.Background())

	invalidateOn401(src, http.StatusUnauthorized, stale)
	after, err := src.Token(context.Background())
	if err != nil {
		t.Fatalf("Token after late 401: %v", err)
	}

	if after != fresh {
		t.Fatal("a late 401 on the superseded token discarded the fresh one")
	}
	if got := stub.calls.Load(); got != 2 {
		t.Fatalf("token exchanges = %d, want 2 (prime + one refresh)", got)
	}
}

type sseConnector interface {
	connect(ctx context.Context) (*http.Response, error)
}

func TestStreamConnect401DiscardsThePresentedToken(t *testing.T) {
	always401 := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	})
	consumers := map[string]func(base string, tokens TokenSource) (sseConnector, error){
		"events": func(base string, tokens TokenSource) (sseConnector, error) { return NewConsumer(base, tokens) },
		"logs":   func(base string, tokens TokenSource) (sseConnector, error) { return NewLogsConsumer(base, tokens) },
	}
	for name, build := range consumers {
		t.Run(name, func(t *testing.T) {
			stub := &rtsStub{clock: rtsClock(), lifetime: time.Hour}
			src, _ := rtsNewSource(t, stub)
			rejected, _ := src.Token(context.Background())
			apiSrv := httptest.NewServer(always401)
			t.Cleanup(apiSrv.Close)
			consumer, err := build(apiSrv.URL, src)
			if err != nil {
				t.Fatalf("build consumer: %v", err)
			}

			resp, err := consumer.connect(context.Background())
			if resp != nil {
				_ = resp.Body.Close()
			}

			if Classify(err) != ReasonAuth {
				t.Fatalf("connect err = %v, want an auth rejection", err)
			}
			next, _ := src.Token(context.Background())
			if next == rejected || next == "" {
				t.Fatal("the reconnect would re-present the token go-api just rejected")
			}
		})
	}
}
