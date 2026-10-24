package trackers

import (
	"altune/go-api/internal/discovery/ports"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

const testKey = "test-key-123"

type fakeIndexCache struct {
	index map[string]string
	sets  int
}

func (f *fakeIndexCache) Get(context.Context) (map[string]string, bool) {
	return f.index, f.index != nil
}

func (f *fakeIndexCache) Set(_ context.Context, index map[string]string, _ time.Duration) error {
	f.index = index
	f.sets++
	return nil
}

type sheetsServer struct {
	*httptest.Server
	hubCalls    atomic.Int32
	artistCalls atomic.Int32
	lastQuery   atomic.Value
	lastKey     atomic.Value
	artistCode  int
}

func newSheetsServer(t *testing.T, hub, artist []byte) *sheetsServer {
	t.Helper()
	s := &sheetsServer{artistCode: http.StatusOK}
	s.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.lastQuery.Store(r.URL.RawQuery)
		s.lastKey.Store(r.Header.Get("X-Goog-Api-Key"))
		if strings.HasSuffix(r.URL.Path, kenSheetID) {
			s.artistCalls.Add(1)
			w.WriteHeader(s.artistCode)
			_, _ = w.Write(artist)
			return
		}
		s.hubCalls.Add(1)
		_, _ = w.Write(hub)
	}))
	t.Cleanup(s.Close)
	return s
}

func (s *sheetsServer) client(cache ports.TrackerIndexCache) *Client {
	c := NewClient(s.Client(), testKey, "HUB1234567890123456789", cache)
	c.baseURL = s.URL + "/"
	return c
}

func TestClient_ArtistSongsReturnsTheTrackersFullLeaks(t *testing.T) {
	srv := newSheetsServer(t, readFixture(t, "hub_artists.json"), readFixture(t, "ken_carson_unreleased.json"))
	songs, err := srv.client(&fakeIndexCache{}).ArtistSongs(context.Background(), "Ken Carson")
	if err != nil {
		t.Fatalf("ArtistSongs: %v", err)
	}
	if len(songs) != 4 || songs[0].Title != "Dream [V2]" || songs[0].TrackerKey != "ken-carson:dream-v2" {
		t.Fatalf("songs = %+v", songs)
	}
	link := songs[0].AudioLinks[0]
	if link.Provider != "pixeldrain" || link.URL != "https://pixeldrain.com/u/ozSpSjn7" || songs[0].Quality != "High Quality" {
		t.Errorf("dream link/quality = %+v / %q", link, songs[0].Quality)
	}
}

func TestClient_SendsTheKeyInAHeaderNeverInTheURL(t *testing.T) {
	srv := newSheetsServer(t, readFixture(t, "hub_artists.json"), readFixture(t, "ken_carson_unreleased.json"))
	if _, err := srv.client(nil).ArtistSongs(context.Background(), "Ken Carson"); err != nil {
		t.Fatal(err)
	}
	query, _ := srv.lastQuery.Load().(string)
	if strings.Contains(query, testKey) || strings.Contains(query, "key=") {
		t.Errorf("api key leaked into the query: %q", query)
	}
	if got, _ := srv.lastKey.Load().(string); got != testKey {
		t.Errorf("X-Goog-Api-Key = %q, want the key", got)
	}
	for _, want := range []string{"ranges=Unreleased", "prettyPrint=false", "fields="} {
		if !strings.Contains(query, want) {
			t.Errorf("query %q lacks %q", query, want)
		}
	}
	if !strings.Contains(query, "textFormatRuns") || !strings.Contains(query, "hyperlink") {
		t.Errorf("fields mask %q must request both hyperlink and text-run links", query)
	}
}

func TestClient_HubIndexIsCachedAcrossCalls(t *testing.T) {
	srv := newSheetsServer(t, readFixture(t, "hub_artists.json"), readFixture(t, "ken_carson_unreleased.json"))
	cache := &fakeIndexCache{}
	c := srv.client(cache)
	for range 2 {
		if _, err := c.ArtistSongs(context.Background(), "Ken Carson"); err != nil {
			t.Fatal(err)
		}
	}
	if srv.hubCalls.Load() != 1 || cache.sets != 1 {
		t.Errorf("hub fetched %d times, cached %d times; want 1 and 1", srv.hubCalls.Load(), cache.sets)
	}
}

func TestClient_ArtistNotOnTheHubIsEmptyWithoutASheetFetch(t *testing.T) {
	srv := newSheetsServer(t, readFixture(t, "hub_artists.json"), nil)
	songs, err := srv.client(&fakeIndexCache{}).ArtistSongs(context.Background(), "Nobody Known")
	if err != nil || songs == nil || len(songs) != 0 || srv.artistCalls.Load() != 0 {
		t.Errorf("songs = %v, err = %v, sheet calls = %d", songs, err, srv.artistCalls.Load())
	}
}

func TestClient_ASheetsOutageIsAnError(t *testing.T) {
	srv := newSheetsServer(t, readFixture(t, "hub_artists.json"), nil)
	srv.artistCode = http.StatusTooManyRequests
	if _, err := srv.client(&fakeIndexCache{}).ArtistSongs(context.Background(), "Ken Carson"); err == nil {
		t.Error("a 429 from Sheets must surface as an error")
	}
}

func TestClient_ASheetWithoutAnUnreleasedTabIsEmptyNotAnError(t *testing.T) {
	srv := newSheetsServer(t, readFixture(t, "hub_artists.json"), nil)
	srv.artistCode = http.StatusBadRequest
	songs, err := srv.client(&fakeIndexCache{}).ArtistSongs(context.Background(), "Ken Carson")
	if err != nil || len(songs) != 0 {
		t.Errorf("songs = %v, err = %v, want none and no error", songs, err)
	}
}

func TestClient_ABrokenHubBodyIsAnError(t *testing.T) {
	srv := newSheetsServer(t, []byte("{"), nil)
	if _, err := srv.client(&fakeIndexCache{}).ArtistSongs(context.Background(), "Ken Carson"); err == nil {
		t.Error("a malformed hub body must surface as an error")
	}
}

func TestNewClient_UsesItsOwnTimeoutWithoutTouchingTheSharedClient(t *testing.T) {
	shared := &http.Client{Timeout: 10 * time.Second}
	c := NewClient(shared, testKey, "hub", nil)
	if c.http.Timeout != 30*time.Second || shared.Timeout != 10*time.Second {
		t.Errorf("timeouts = %v / %v, want 30s own and 10s shared", c.http.Timeout, shared.Timeout)
	}
}
