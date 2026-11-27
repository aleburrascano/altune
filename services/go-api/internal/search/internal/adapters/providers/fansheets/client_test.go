package fansheets_test

import (
	"altune/go-api/internal/search/internal/adapters/providers/fansheets"
	"altune/go-api/internal/search/internal/core/ports"
	"altune/go-api/internal/search/internal/domain"
	"altune/go-api/internal/shared/logging"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

const (
	testKey    = "test-key-123"
	tabsFields = "sheets.properties.title"
)

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

type redirectTransport struct{ target *url.URL }

func (rt redirectTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	out := r.Clone(r.Context())
	out.URL.Scheme, out.URL.Host, out.Host = rt.target.Scheme, rt.target.Host, rt.target.Host
	return http.DefaultTransport.RoundTrip(out)
}

type sheetsServer struct {
	*httptest.Server
	hubCalls     atomic.Int32
	artistCalls  atomic.Int32
	lastQuery    atomic.Value
	lastKey      atomic.Value
	mu           sync.Mutex
	gridRanges   []string
	artistCode   int
	tabs         string
	released     []byte
	releasedCode int
}

func tabsBody(titles ...string) string {
	sheets := make([]string, 0, len(titles))
	for _, title := range titles {
		sheets = append(sheets, `{"properties":{"title":"`+title+`"}}`)
	}
	return `{"sheets":[` + strings.Join(sheets, ",") + `]}`
}

func quotedTab(title string) string {
	return "'" + strings.ReplaceAll(title, "'", "''") + "'"
}

func newSheetsServer(t *testing.T, hub, artist []byte) *sheetsServer {
	t.Helper()
	s := &sheetsServer{artistCode: http.StatusOK, tabs: tabsBody("Released", "Unreleased")}
	s.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.lastKey.Store(r.Header.Get("X-Goog-Api-Key"))
		onArtistSheet := strings.HasSuffix(r.URL.Path, kenSheetID)
		if onArtistSheet && r.URL.Query().Get("fields") == tabsFields {
			_, _ = w.Write([]byte(s.tabs))
			return
		}
		s.lastQuery.Store(r.URL.RawQuery)
		if !onArtistSheet {
			s.hubCalls.Add(1)
			_, _ = w.Write(hub)
			return
		}
		sheetRange := r.URL.Query().Get("ranges")
		s.mu.Lock()
		s.gridRanges = append(s.gridRanges, sheetRange)
		s.mu.Unlock()
		if sheetRange == quotedTab("Released") && s.released != nil {
			w.WriteHeader(s.releasedCode)
			_, _ = w.Write(s.released)
			return
		}
		s.artistCalls.Add(1)
		w.WriteHeader(s.artistCode)
		_, _ = w.Write(artist)
	}))
	t.Cleanup(s.Close)
	return s
}

func (s *sheetsServer) ranges() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.gridRanges)
}

func (s *sheetsServer) httpClient() *http.Client {
	target, _ := url.Parse(s.URL)
	return &http.Client{Transport: redirectTransport{target: target}}
}

func (s *sheetsServer) client(t *testing.T, cache *fakeIndexCache) interface {
	ArtistSongs(context.Context, string) ([]domain.UnreleasedSong, error)
	Artists(context.Context) ([]string, error)
} {
	t.Helper()
	if cache == nil {
		return newTrackersClient(t, s.httpClient(), fansheets.NoIndexCache{})
	}
	return newTrackersClient(t, s.httpClient(), cache)
}

func newTrackersClient(t *testing.T, httpClient *http.Client, index ports.TrackerIndexCache) *fansheets.Client {
	t.Helper()
	client, err := fansheets.NewClient(httpClient, testKey, "HUB1234567890123456789", index)
	if err != nil {
		t.Fatalf("fansheets.NewClient: %v", err)
	}
	return client
}

func TestNewClient_RefusesAMissingIndexCache(t *testing.T) {
	client, err := fansheets.NewClient(http.DefaultClient, testKey, "hub", nil)

	if client != nil || !errors.Is(err, fansheets.ErrCollaboratorRequired) {
		t.Fatalf("got (%v, %v), want a refusal wrapping fansheets.ErrCollaboratorRequired", client, err)
	}
}

func captureLog(t *testing.T) *logging.Capture {
	t.Helper()
	return logging.CaptureText(t, slog.LevelInfo)
}

func TestClient_ArtistSongsReturnsTheTrackersFullLeaks(t *testing.T) {
	srv := newSheetsServer(t, readFixture(t, "hub_artists.json"), readFixture(t, "ken_carson_unreleased.json"))
	songs, err := srv.client(t, &fakeIndexCache{}).ArtistSongs(context.Background(), "Ken Carson")
	if err != nil {
		t.Fatalf("ArtistSongs: %v", err)
	}
	if len(songs) != 4 || songs[0].Title != "Dream" || songs[0].TrackerKey != "ken-carson:dream" {
		t.Fatalf("songs = %+v", songs)
	}
	link := songs[0].AudioLinks[0]
	if link.Provider != "pixeldrain" || link.URL != "https://pixeldrain.com/u/ozSpSjn7" || songs[0].Quality != "High Quality" {
		t.Errorf("dream link/quality = %+v / %q", link, songs[0].Quality)
	}
}

func TestClient_SendsTheKeyInAHeaderNeverInTheURL(t *testing.T) {
	srv := newSheetsServer(t, readFixture(t, "hub_artists.json"), readFixture(t, "ken_carson_unreleased.json"))
	srv.tabs = tabsBody("Unreleased")
	if _, err := srv.client(t, nil).ArtistSongs(context.Background(), "Ken Carson"); err != nil {
		t.Fatal(err)
	}
	query, _ := srv.lastQuery.Load().(string)
	if strings.Contains(query, testKey) || strings.Contains(query, "key=") {
		t.Errorf("api key leaked into the query: %q", query)
	}
	if got, _ := srv.lastKey.Load().(string); got != testKey {
		t.Errorf("X-Goog-Api-Key = %q, want the key", got)
	}
	for _, want := range []string{"ranges=%27Unreleased%27", "prettyPrint=false", "fields="} {
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
	c := srv.client(t, cache)
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
	songs, err := srv.client(t, &fakeIndexCache{}).ArtistSongs(context.Background(), "Nobody Known")
	if err != nil || songs == nil || len(songs) != 0 || srv.artistCalls.Load() != 0 {
		t.Errorf("songs = %v, err = %v, sheet calls = %d", songs, err, srv.artistCalls.Load())
	}
}

func TestClient_ASheetsOutageIsAnError(t *testing.T) {
	srv := newSheetsServer(t, readFixture(t, "hub_artists.json"), nil)
	srv.artistCode = http.StatusTooManyRequests
	if _, err := srv.client(t, &fakeIndexCache{}).ArtistSongs(context.Background(), "Ken Carson"); err == nil {
		t.Error("a 429 from Sheets must surface as an error")
	}
}

func TestClient_ReadsTheUnreleasedTabWhateverItIsCalled(t *testing.T) {
	for _, title := range []string{"Unreleased Discography", "💿Unreleased", "Tom's unreleased"} {
		srv := newSheetsServer(t, readFixture(t, "hub_artists.json"), readFixture(t, "ken_carson_unreleased.json"))
		srv.tabs = tabsBody("Released Discography", title, "Unsurfaced")
		songs, err := srv.client(t, nil).ArtistSongs(context.Background(), "Ken Carson")
		if err != nil || len(songs) != 4 {
			t.Errorf("tab %q: songs = %d, err = %v, want the 4 full leaks", title, len(songs), err)
		}
		if query, _ := srv.lastQuery.Load().(string); !strings.Contains(query, "ranges="+url.QueryEscape(quotedTab(title))) {
			t.Errorf("tab %q: grid query %q does not read that tab", title, query)
		}
	}
}

func TestClient_PrefersTheTabNamedJustUnreleased(t *testing.T) {
	srv := newSheetsServer(t, readFixture(t, "hub_artists.json"), readFixture(t, "ken_carson_unreleased.json"))
	srv.tabs = tabsBody("Unreleased (Alt)", "💿 Unreleased", "Released")
	if _, err := srv.client(t, nil).ArtistSongs(context.Background(), "Ken Carson"); err != nil {
		t.Fatal(err)
	}
	if got := srv.ranges(); len(got) == 0 || got[0] != quotedTab("💿 Unreleased") {
		t.Errorf("grid reads = %q, want the tab named just Unreleased read first", got)
	}
}

func TestClient_ASheetWithoutAnUnreleasedTabIsEmptyWithoutAGridFetch(t *testing.T) {
	srv := newSheetsServer(t, readFixture(t, "hub_artists.json"), nil)
	srv.tabs = tabsBody("Fit Pics", "Remakes")
	songs, err := srv.client(t, &fakeIndexCache{}).ArtistSongs(context.Background(), "Ken Carson")
	if err != nil || len(songs) != 0 || srv.artistCalls.Load() != 0 {
		t.Errorf("songs = %v, err = %v, grid calls = %d, want none, no error, no grid fetch", songs, err, srv.artistCalls.Load())
	}
}

func TestClient_ABadRequestForTheTabIsAnError(t *testing.T) {
	srv := newSheetsServer(t, readFixture(t, "hub_artists.json"), nil)
	srv.artistCode = http.StatusBadRequest
	if _, err := srv.client(t, &fakeIndexCache{}).ArtistSongs(context.Background(), "Ken Carson"); err == nil {
		t.Error("a 400 on a tab that exists must surface, not read as an empty sheet")
	}
}

func TestClient_ABrokenHubBodyIsAnError(t *testing.T) {
	srv := newSheetsServer(t, []byte("{"), nil)
	if _, err := srv.client(t, &fakeIndexCache{}).ArtistSongs(context.Background(), "Ken Carson"); err == nil {
		t.Error("a malformed hub body must surface as an error")
	}
	if _, err := srv.client(t, nil).Artists(context.Background()); err == nil {
		t.Error("a malformed hub body must fail Artists too")
	}
}

func TestNewClient_UsesItsOwnTimeoutWithoutTouchingTheSharedClient(t *testing.T) {
	srv := newSheetsServer(t, readFixture(t, "hub_artists.json"), readFixture(t, "ken_carson_unreleased.json"))
	shared := srv.httpClient()
	shared.Timeout = time.Nanosecond

	songs, err := newTrackersClient(t, shared, fansheets.NoIndexCache{}).ArtistSongs(context.Background(), "Ken Carson")
	if err != nil || len(songs) != 4 {
		t.Errorf("ArtistSongs = %d songs, err %v; want the client's own timeout, not the shared 1ns", len(songs), err)
	}
	if shared.Timeout != time.Nanosecond {
		t.Errorf("shared timeout = %v, want it untouched", shared.Timeout)
	}
}

func offStreamingSheet(t *testing.T) []byte {
	t.Helper()
	var rows [][]cell
	for i := range 6 {
		rows = append(rows, releasedRow(fmt.Sprintf("Hit %d", i), "Yes", fmt.Sprintf("https://youtu.be/hit%08d", i)))
	}
	rows = append(rows, releasedRow("Do You Want", "No", "https://www.youtube.com/watch?v=JNprs9jO0eU"))
	return gridBody(t, []string{"Era", "Name", "Type", "Streaming", "Link(s)"}, rows...)
}

func TestClient_AddsTheReleasedTabsOffStreamingSongs(t *testing.T) {
	srv := newSheetsServer(t, readFixture(t, "hub_artists.json"), readFixture(t, "ken_carson_unreleased.json"))
	srv.released, srv.releasedCode = offStreamingSheet(t), http.StatusOK
	songs, err := srv.client(t, nil).ArtistSongs(context.Background(), "Ken Carson")
	if err != nil || len(songs) != 5 {
		t.Fatalf("ArtistSongs = %d songs, err %v; want 4 unreleased and 1 off streaming", len(songs), err)
	}
	last := songs[4]
	if last.Title != "Do You Want" || !last.OffStreaming || last.AudioLinks[0].Provider != "youtube" || songs[0].OffStreaming {
		t.Errorf("songs = %+v, want only Do You Want flagged, with its YouTube link", songs)
	}
}

func TestClient_KeepsTheUnreleasedSongsWhenTheReleasedTabFails(t *testing.T) {
	srv := newSheetsServer(t, readFixture(t, "hub_artists.json"), readFixture(t, "ken_carson_unreleased.json"))
	srv.released, srv.releasedCode = []byte(`{}`), http.StatusInternalServerError
	songs, err := srv.client(t, nil).ArtistSongs(context.Background(), "Ken Carson")
	if !errors.Is(err, ports.ErrTrackerPartial) || len(songs) != 4 {
		t.Errorf("ArtistSongs = %d songs, err %v; want the 4 unreleased songs and a partial error", len(songs), err)
	}
}

func TestClient_ReadsOnlyTheUnreleasedTabWhenThereIsNoReleasedTab(t *testing.T) {
	srv := newSheetsServer(t, readFixture(t, "hub_artists.json"), readFixture(t, "ken_carson_unreleased.json"))
	srv.tabs = tabsBody("Unreleased", "Released [old]")
	songs, err := srv.client(t, nil).ArtistSongs(context.Background(), "Ken Carson")
	if err != nil || len(songs) != 4 || srv.artistCalls.Load() != 1 {
		t.Errorf("ArtistSongs = %d songs, err %v, %d grid reads; want 4 songs from one read", len(songs), err, srv.artistCalls.Load())
	}
}
