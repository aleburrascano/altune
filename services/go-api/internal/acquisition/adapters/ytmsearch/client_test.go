package ytmsearch

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
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
