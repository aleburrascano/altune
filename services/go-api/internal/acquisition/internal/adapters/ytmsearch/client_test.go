package ytmsearch

import (
	"altune/go-api/internal/shared/ytminnertube"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	acqdomain "altune/go-api/internal/acquisition/domain"

	"github.com/alicebob/miniredis/v2"
	goredis "github.com/redis/go-redis/v9"
)

type recordedRequest struct {
	path string
	body map[string]any
}

func fixtureServer(t *testing.T, requests *[]recordedRequest) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var body map[string]any
		_ = json.Unmarshal(raw, &body)
		*requests = append(*requests, recordedRequest{path: r.URL.Path, body: body})
		switch r.URL.Path {
		case "/youtubei/v1/next":
			_, _ = w.Write(fixture(t, "next_"+body["videoId"].(string)+".json"))
		case "/youtubei/v1/search":
			_, _ = w.Write(fixture(t, "search_lucki_2021_vibes.json"))
		case "/oembed":
			id := strings.TrimPrefix(r.URL.Query().Get("url"), "https://www.youtube.com/watch?v=")
			_, _ = w.Write(fixture(t, "oembed_"+id+".json"))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func clientFor(srv *httptest.Server, interval, timeout time.Duration) *Client {
	return New(Config{MusicURL: srv.URL, OEmbedURL: srv.URL + "/oembed", MinInterval: interval, Timeout: timeout})
}

func TestClientWatch_PostsTheWebRemixNextRequestAndParsesIt(t *testing.T) {
	var requests []recordedRequest
	client := clientFor(fixtureServer(t, &requests), 0, time.Second)

	got, err := client.Watch(context.Background(), "u4GAeAYGAqE")
	if err != nil {
		t.Fatalf("Watch: %v", err)
	}

	if got.Type != VideoTypeOMV {
		t.Errorf("Type = %q, want OMV", got.Type)
	}
	if len(requests) != 1 || requests[0].path != "/youtubei/v1/next" {
		t.Fatalf("requests = %+v, want one POST to /youtubei/v1/next", requests)
	}
	clientCtx := requests[0].body["context"].(map[string]any)["client"].(map[string]any)
	if clientCtx["clientName"] != "WEB_REMIX" {
		t.Errorf("clientName = %v, want WEB_REMIX", clientCtx["clientName"])
	}
	if requests[0].body["videoId"] != "u4GAeAYGAqE" {
		t.Errorf("videoId = %v", requests[0].body["videoId"])
	}
}

func TestClientSearchSongs_PostsTheQueryWithTheSongsFilter(t *testing.T) {
	var requests []recordedRequest
	client := clientFor(fixtureServer(t, &requests), 0, time.Second)

	songs, err := client.SearchSongs(context.Background(), "2021 Vibes LUCKI")
	if err != nil {
		t.Fatalf("SearchSongs: %v", err)
	}

	if len(songs) == 0 || songs[0].VideoID != "88oFlVnMEsc" {
		t.Fatalf("songs = %+v, want the clean master first", songs)
	}
	body := requests[0].body
	if body["query"] != "2021 Vibes LUCKI" || body["params"] != songsFilterParams {
		t.Errorf("body = %+v, want the query and the songs filter", body)
	}
}

func TestClientOEmbed_FetchesTheWatchURL(t *testing.T) {
	var requests []recordedRequest
	client := clientFor(fixtureServer(t, &requests), 0, time.Second)

	got, err := client.OEmbed(context.Background(), "XDC3uH1FSgw")
	if err != nil {
		t.Fatalf("OEmbed: %v", err)
	}

	if got.Author != "slayr - Topic" {
		t.Errorf("Author = %q", got.Author)
	}
}

func TestClient_NonOKStatusIsAnError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "nope", http.StatusTooManyRequests)
	}))
	defer srv.Close()
	client := clientFor(srv, 0, time.Second)

	if _, err := client.Watch(context.Background(), "XDC3uH1FSgw"); err == nil || !strings.Contains(err.Error(), "429") {
		t.Fatalf("err = %v, want a 429 error", err)
	}
	if _, err := client.OEmbed(context.Background(), "XDC3uH1FSgw"); err == nil {
		t.Fatal("want an oembed status error")
	}
}

func TestClient_TimeoutBoundsAHungEndpoint(t *testing.T) {
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) { <-release }))
	defer srv.Close()
	defer close(release)
	client := clientFor(srv, 0, 30*time.Millisecond)

	start := time.Now()
	_, err := client.SearchSongs(context.Background(), "anything")

	if err == nil {
		t.Fatal("want a timeout error")
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("took %v, the timeout did not bound the call", elapsed)
	}
}

func TestClient_SpacesRequestsToOneEndpointByTheMinimumInterval(t *testing.T) {
	var requests []recordedRequest
	const interval = 80 * time.Millisecond
	client := clientFor(fixtureServer(t, &requests), interval, time.Second)

	start := time.Now()
	for _, id := range []string{"XDC3uH1FSgw", "u4GAeAYGAqE", "88oFlVnMEsc"} {
		if _, err := client.Watch(context.Background(), id); err != nil {
			t.Fatalf("Watch %s: %v", id, err)
		}
	}

	if elapsed := time.Since(start); elapsed < 2*interval {
		t.Fatalf("3 calls took %v, want at least %v", elapsed, 2*interval)
	}
}

func TestClient_EndpointsDoNotShareARateLimit(t *testing.T) {
	var requests []recordedRequest
	client := clientFor(fixtureServer(t, &requests), time.Hour, time.Second)

	start := time.Now()
	_, _ = client.Watch(context.Background(), "XDC3uH1FSgw")
	_, _ = client.SearchSongs(context.Background(), "q")
	_, _ = client.OEmbed(context.Background(), "XDC3uH1FSgw")

	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("first call per endpoint took %v, want no wait", elapsed)
	}
}

func TestClient_CancelledContextStopsTheWait(t *testing.T) {
	var requests []recordedRequest
	client := clientFor(fixtureServer(t, &requests), time.Hour, time.Second)
	_, _ = client.Watch(context.Background(), "XDC3uH1FSgw")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()

	if _, err := client.Watch(ctx, "u4GAeAYGAqE"); err == nil {
		t.Fatal("want the context error while waiting for the slot")
	}
}

func TestClient_AnOutageIsSourceUnavailable(t *testing.T) {
	statusServer := func(status int) *httptest.Server {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(status) }))
		t.Cleanup(srv.Close)
		return srv
	}
	refused := httptest.NewServer(http.NotFoundHandler())
	refused.Close()
	hung := make(chan struct{})
	hangingServer := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) { <-hung }))
	t.Cleanup(hangingServer.Close)
	t.Cleanup(func() { close(hung) })
	unparsable := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("not json")) }))
	t.Cleanup(unparsable.Close)

	cases := []struct {
		name    string
		srv     *httptest.Server
		timeout time.Duration
	}{
		{"429", statusServer(http.StatusTooManyRequests), time.Second},
		{"503", statusServer(http.StatusServiceUnavailable), time.Second},
		{"400", statusServer(http.StatusBadRequest), time.Second},
		{"refused connection", refused, time.Second},
		{"the client's own timeout", hangingServer, 30 * time.Millisecond},
		{"an unparsable body", unparsable, time.Second},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			client := clientFor(tc.srv, 0, tc.timeout)

			_, searchErr := client.SearchSongs(context.Background(), "anything")
			_, watchErr := client.Watch(context.Background(), "XDC3uH1FSgw")

			for name, err := range map[string]error{"SearchSongs": searchErr, "Watch": watchErr} {
				if !acqdomain.IsSourceUnavailable(err) {
					t.Errorf("%s err = %v, want source unavailable", name, err)
				}
				if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
					t.Errorf("%s err = %v, must not read as a cancellation", name, err)
				}
			}
		})
	}
}

func TestClient_ACancelledCallerIsNotAnOutage(t *testing.T) {
	hung := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) { <-hung }))
	t.Cleanup(srv.Close)
	t.Cleanup(func() { close(hung) })
	client := clientFor(srv, 0, time.Minute)
	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(30*time.Millisecond, cancel)

	_, searchErr := client.SearchSongs(ctx, "anything")
	_, watchErr := client.Watch(ctx, "XDC3uH1FSgw")

	for name, err := range map[string]error{"SearchSongs": searchErr, "Watch": watchErr} {
		if !errors.Is(err, context.Canceled) || acqdomain.IsSourceUnavailable(err) {
			t.Errorf("%s err = %v, want the caller's cancellation, not an outage", name, err)
		}
	}
}

func TestClient_AnOutageNamesYtmsearchAsTheUnavailableSource(t *testing.T) {
	for _, status := range []int{http.StatusInternalServerError, http.StatusBadGateway, http.StatusNotFound} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(status) }))
		t.Cleanup(srv.Close)
		client := clientFor(srv, 0, time.Second)

		_, searchErr := client.SearchSongs(context.Background(), "anything")
		_, watchErr := client.Watch(context.Background(), "XDC3uH1FSgw")

		for name, err := range map[string]error{"SearchSongs": searchErr, "Watch": watchErr} {
			var unavailable *acqdomain.SourceUnavailableError
			if !errors.As(err, &unavailable) || unavailable.Source != acqdomain.SourceName("ytmsearch") {
				t.Errorf("status %d: %s err = %v, want SourceUnavailableError{Source: ytmsearch}", status, name, err)
			}
		}
	}
}

func TestClient_ACallerDeadlineIsNotAnOutage(t *testing.T) {
	hung := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) { <-hung }))
	t.Cleanup(srv.Close)
	t.Cleanup(func() { close(hung) })
	client := clientFor(srv, 0, time.Minute)

	for name, call := range map[string]func(context.Context) error{
		"SearchSongs": func(ctx context.Context) error { _, err := client.SearchSongs(ctx, "anything"); return err },
		"Watch":       func(ctx context.Context) error { _, err := client.Watch(ctx, "XDC3uH1FSgw"); return err },
	} {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
		err := call(ctx)
		cancel()
		if !errors.Is(err, context.DeadlineExceeded) || acqdomain.IsSourceUnavailable(err) {
			t.Errorf("%s err = %v, want the caller's deadline, not an outage", name, err)
		}
	}
}

func TestClient_ACallerCancelledWhileWaitingForItsTurnIsNotAnOutage(t *testing.T) {
	var requests []recordedRequest
	client := clientFor(fixtureServer(t, &requests), time.Hour, time.Second)
	_, _ = client.Watch(context.Background(), "XDC3uH1FSgw")
	_, _ = client.SearchSongs(context.Background(), "q")
	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(30*time.Millisecond, cancel)

	_, watchErr := client.Watch(ctx, "u4GAeAYGAqE")
	_, searchErr := client.SearchSongs(ctx, "q")

	for name, err := range map[string]error{"SearchSongs": searchErr, "Watch": watchErr} {
		if !errors.Is(err, context.Canceled) || acqdomain.IsSourceUnavailable(err) {
			t.Errorf("%s err = %v, want the caller's cancellation, not an outage", name, err)
		}
	}
}

func TestClient_AnAlreadyCancelledCallerIsNotAnOutageEvenWhenTheServerIsDown(t *testing.T) {
	refused := httptest.NewServer(http.NotFoundHandler())
	refused.Close()
	client := clientFor(refused, 0, time.Second)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, searchErr := client.SearchSongs(ctx, "anything")
	_, watchErr := client.Watch(ctx, "XDC3uH1FSgw")

	for name, err := range map[string]error{"SearchSongs": searchErr, "Watch": watchErr} {
		if !errors.Is(err, context.Canceled) || acqdomain.IsSourceUnavailable(err) {
			t.Errorf("%s err = %v, want the caller's cancellation, not an outage", name, err)
		}
	}
}

func TestClient_AConnectionDroppedMidResponseIsSourceUnavailable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Length", "100000")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"contents":`))
		w.(http.Flusher).Flush()
		conn, _, err := w.(http.Hijacker).Hijack()
		if err == nil {
			_ = conn.Close()
		}
	}))
	t.Cleanup(srv.Close)
	client := clientFor(srv, 0, time.Second)

	_, searchErr := client.SearchSongs(context.Background(), "anything")
	_, watchErr := client.Watch(context.Background(), "XDC3uH1FSgw")

	for name, err := range map[string]error{"SearchSongs": searchErr, "Watch": watchErr} {
		if !acqdomain.IsSourceUnavailable(err) || errors.Is(err, context.Canceled) {
			t.Errorf("%s err = %v, want source unavailable", name, err)
		}
	}
}

func TestClient_ItsOwnTimeoutSaysItTimedOut(t *testing.T) {
	hung := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) { <-hung }))
	t.Cleanup(srv.Close)
	t.Cleanup(func() { close(hung) })
	client := clientFor(srv, 0, 30*time.Millisecond)

	_, searchErr := client.SearchSongs(context.Background(), "anything")
	_, watchErr := client.Watch(context.Background(), "XDC3uH1FSgw")

	for name, err := range map[string]error{"SearchSongs": searchErr, "Watch": watchErr} {
		if err == nil || !strings.Contains(err.Error(), "timed out after 30ms") {
			t.Errorf("%s err = %v, want it to say it timed out after 30ms", name, err)
		}
	}
}

func TestClient_WaitingForItsTurnDoesNotUseUpTheRequestTimeout(t *testing.T) {
	var requests []recordedRequest
	const interval, timeout = 150 * time.Millisecond, 50 * time.Millisecond
	client := clientFor(fixtureServer(t, &requests), interval, timeout)
	if _, err := client.SearchSongs(context.Background(), "first"); err != nil {
		t.Fatalf("first search: %v", err)
	}

	_, err := client.SearchSongs(context.Background(), "waits longer than the timeout for its turn")
	if err != nil {
		t.Fatalf("second search err = %v, want the turn wait outside the %v request timeout", err, timeout)
	}
}

func TestClient_WaitsForASharedBudgetSlotBeyondTheTimeout(t *testing.T) {
	const interval, timeout = 300 * time.Millisecond, 50 * time.Millisecond
	const budgetKey = "ytminnertube:budget"
	redisServer := miniredis.RunT(t)
	redisClient := goredis.NewClient(&goredis.Options{Addr: redisServer.Addr()})
	t.Cleanup(func() { _ = redisClient.Close() })
	budget, err := ytminnertube.NewBudget(redisClient, ytminnertube.Settings{MinInterval: interval})
	if err != nil {
		t.Fatal(err)
	}
	if err := budget.Wait(context.Background()); err != nil {
		t.Fatal(err)
	}
	slotBefore := budgetSlot(t, redisServer, budgetKey)

	var requests []recordedRequest
	srv := fixtureServer(t, &requests)
	client := New(Config{
		HTTP:        &http.Client{Transport: budget.Transport(nil)},
		MusicURL:    srv.URL,
		OEmbedURL:   srv.URL + "/oembed",
		MinInterval: time.Millisecond,
		Timeout:     timeout,
	})

	if _, err := client.SearchSongs(context.Background(), "waits for the shared budget"); err != nil {
		if errors.Is(err, ytminnertube.ErrBudgetRefused) {
			t.Fatalf("slot %v out under a %v timeout was refused: %v", interval, timeout, err)
		}
		t.Fatalf("search err = %v, want the slot wait outside the %v request timeout", err, timeout)
	}
	if taken := budgetSlot(t, redisServer, budgetKey) - slotBefore; taken != interval.Milliseconds() {
		t.Fatalf("the search advanced the shared slot by %dms, want exactly one interval of %v", taken, interval)
	}
}

func budgetSlot(t *testing.T, server *miniredis.Miniredis, key string) int64 {
	t.Helper()
	raw, err := server.Get(key)
	if err != nil {
		t.Fatal(err)
	}
	slot, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		t.Fatal(err)
	}
	return slot
}
