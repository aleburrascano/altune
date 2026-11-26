package handler_test

import (
	"altune/go-api/internal/search"
	"altune/go-api/internal/search/searchtest"
	"altune/go-api/internal/shared/ids"
	"context"
	"net/http"
	"regexp"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
)

type goldenOwnershipReader struct {
	owned map[string]search.OwnedTrack
}

func (r goldenOwnershipReader) OwnedByTitleArtist(context.Context, ids.UserId) (map[string]search.OwnedTrack, error) {
	return r.owned, nil
}

type fillCall struct {
	trackID  string
	position int
}

type recordingTrackNumberFiller struct {
	mu    sync.Mutex
	calls []fillCall
	done  chan struct{}
	want  int
}

func (f *recordingTrackNumberFiller) FillTrackNumber(_ context.Context, _ ids.UserId, trackID string, position int) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, fillCall{trackID, position})
	if len(f.calls) == f.want {
		close(f.done)
	}
	return nil
}

func goldenOwned() goldenOwnershipReader {
	return goldenOwnershipReader{owned: map[string]search.OwnedTrack{
		search.OwnershipKey("Owned Song", "Artist"):  {TrackID: "track-1", AcquisitionStatus: "ready", ISRC: "USRC17607839", Album: "Deluxe"},
		search.OwnershipKey("Placed Song", "Artist"): {TrackID: "track-2", AcquisitionStatus: "pending"},
	}}
}

func goldenTrack(title, id string, extras map[string]any) search.SearchResult {
	return search.SearchResult{
		Kind: search.ResultKindTrack, Title: title, Subtitle: "Artist",
		Confidence: search.ConfidenceHigh,
		Sources:    []search.ProviderLink{{Provider: search.ProviderDeezer, ExternalID: id}},
		Extras:     extras,
	}
}

func goldenPlaced(title, id string, track int) search.SearchResult {
	r := goldenTrack(title, id, map[string]any{})
	r.TrackPosition = search.TrackPosition{Track: track}
	return r
}

func goldenAlbum(title, id string) search.SearchResult {
	return search.SearchResult{
		Kind: search.ResultKindAlbum, Title: title, Subtitle: "Artist",
		Confidence: search.ConfidenceHigh,
		Sources:    []search.ProviderLink{{Provider: search.ProviderDeezer, ExternalID: id}},
	}
}

func goldenTracks() []search.SearchResult {
	return []search.SearchResult{
		goldenTrack("Owned Song", "1", nil),
		goldenTrack("Stranger Song", "2", map[string]any{}),
		goldenPlaced("Placed Song", "3", 7),
		goldenAlbum("Owned Song", "4"),
	}
}

func goldenAlbumPageTracks() []search.SearchResult {
	tracks := goldenTracks()
	tracks[0] = goldenTrack("Owned Song", "1", map[string]any{})
	tracks[0].ISRC = "USRC17607839"
	tracks[0].TrackPosition = search.TrackPosition{Track: 1}
	return tracks
}

func goldenRouter(t *testing.T, svcs search.DiscoveryServices, filler search.TrackNumberFiller) chi.Router {
	h := wireGoldenOwnership(t, search.MustNewDiscoveryHandler(svcs, time.Now), goldenOwned(), filler)
	r := chi.NewRouter()
	r.Use(discAsTestUser)
	r.Mount("/discovery", h.Routes())
	return r
}

func wireGoldenOwnership(t *testing.T, h *search.DiscoveryHandler, reader search.OwnershipReader, filler search.TrackNumberFiller) *search.DiscoveryHandler {
	bg := searchtest.NewTracker(t)
	if err := h.AttachOwnership(searchtest.NewOwnership(t, bg, reader, filler)); err != nil {
		t.Fatal(err)
	}
	return h
}

var searchIDPattern = regexp.MustCompile(`"search_id":"[^"]*"`)

func assertGoldenBody(t *testing.T, got, want string) {
	t.Helper()
	got = searchIDPattern.ReplaceAllString(got, `"search_id":"<id>"`)
	if got != want+"\n" {
		t.Errorf("response JSON drifted.\n got: %s\nwant: %s", got, want)
	}
}

const (
	ownershipSearchGolden        = `{"query":"song","query_norm":"song","search_id":"<id>","results":[{"kind":"track","title":"Owned Song","subtitle":"Artist","confidence":"high","result_signature":"track|owned song|artist","favorite_key":"artist|owned song","sources":[{"provider":"deezer","external_id":"1","url":""}],"extras":{"owned_acquisition_status":"ready","owned_track_id":"track-1"}},{"kind":"album","title":"Owned Song","subtitle":"Artist","confidence":"high","result_signature":"album|owned song|artist","favorite_key":"artist|owned song","sources":[{"provider":"deezer","external_id":"4","url":""}],"extras":{}},{"kind":"track","title":"Placed Song","subtitle":"Artist","confidence":"high","result_signature":"track|placed song|artist","favorite_key":"artist|placed song","sources":[{"provider":"deezer","external_id":"3","url":""}],"extras":{"owned_acquisition_status":"pending","owned_track_id":"track-2","track_number":7}},{"kind":"track","title":"Stranger Song","subtitle":"Artist","confidence":"high","result_signature":"track|stranger song|artist","favorite_key":"artist|stranger song","sources":[{"provider":"deezer","external_id":"2","url":""}],"extras":{}}],"top_result":{"kind":"track","title":"Owned Song","subtitle":"Artist","confidence":"high","result_signature":"track|owned song|artist","favorite_key":"artist|owned song","sources":[{"provider":"deezer","external_id":"1","url":""}],"extras":{"owned_acquisition_status":"ready","owned_track_id":"track-1"}},"sections":[{"kind":"track","items":[{"kind":"track","title":"Placed Song","subtitle":"Artist","confidence":"high","result_signature":"track|placed song|artist","favorite_key":"artist|placed song","sources":[{"provider":"deezer","external_id":"3","url":""}],"extras":{"owned_acquisition_status":"pending","owned_track_id":"track-2","track_number":7}},{"kind":"track","title":"Stranger Song","subtitle":"Artist","confidence":"high","result_signature":"track|stranger song|artist","favorite_key":"artist|stranger song","sources":[{"provider":"deezer","external_id":"2","url":""}],"extras":{}}],"has_more":false},{"kind":"album","items":[{"kind":"album","title":"Owned Song","subtitle":"Artist","confidence":"high","result_signature":"album|owned song|artist","favorite_key":"artist|owned song","sources":[{"provider":"deezer","external_id":"4","url":""}],"extras":{}}],"has_more":false}],"providers":[{"provider":"deezer","status":"ok","latency_ms":0,"result_count":4}],"partial":false,"cache":{"hit":false,"fetched_at":null},"total":4,"offset":0,"has_more":false}`
	ownershipAlbumTracksGolden   = `{"provider_name":"deezer","status":"ok","items":[{"kind":"track","title":"Owned Song","subtitle":"Artist","confidence":"high","result_signature":"track|owned song|artist","favorite_key":"artist|owned song","sources":[{"provider":"deezer","external_id":"1","url":""}],"extras":{"isrc":"USRC17607839","owned_acquisition_status":"ready","owned_track_id":"track-1","track_number":1}},{"kind":"track","title":"Stranger Song","subtitle":"Artist","confidence":"high","result_signature":"track|stranger song|artist","favorite_key":"artist|stranger song","sources":[{"provider":"deezer","external_id":"2","url":""}],"extras":{}},{"kind":"track","title":"Placed Song","subtitle":"Artist","confidence":"high","result_signature":"track|placed song|artist","favorite_key":"artist|placed song","sources":[{"provider":"deezer","external_id":"3","url":""}],"extras":{"owned_acquisition_status":"pending","owned_track_id":"track-2","track_number":7}},{"kind":"album","title":"Owned Song","subtitle":"Artist","confidence":"high","result_signature":"album|owned song|artist","favorite_key":"artist|owned song","sources":[{"provider":"deezer","external_id":"4","url":""}],"extras":{}}],"partial":false}`
	ownershipTopTracksGolden     = `{"provider_name":"deezer","status":"ok","items":[{"kind":"track","title":"Owned Song","subtitle":"Artist","confidence":"high","result_signature":"track|owned song|artist","favorite_key":"artist|owned song","sources":[{"provider":"deezer","external_id":"1","url":""}],"extras":{"owned_acquisition_status":"ready","owned_track_id":"track-1"}},{"kind":"track","title":"Stranger Song","subtitle":"Artist","confidence":"high","result_signature":"track|stranger song|artist","favorite_key":"artist|stranger song","sources":[{"provider":"deezer","external_id":"2","url":""}],"extras":{}},{"kind":"track","title":"Placed Song","subtitle":"Artist","confidence":"high","result_signature":"track|placed song|artist","favorite_key":"artist|placed song","sources":[{"provider":"deezer","external_id":"3","url":""}],"extras":{"owned_acquisition_status":"pending","owned_track_id":"track-2","track_number":7}},{"kind":"album","title":"Owned Song","subtitle":"Artist","confidence":"high","result_signature":"album|owned song|artist","favorite_key":"artist|owned song","sources":[{"provider":"deezer","external_id":"4","url":""}],"extras":{}}],"partial":false}`
	ownershipArtistAlbumsGolden  = `{"provider_name":"deezer","status":"ok","items":[{"kind":"track","title":"Owned Song","subtitle":"Artist","confidence":"high","result_signature":"track|owned song|artist","favorite_key":"artist|owned song","sources":[{"provider":"deezer","external_id":"1","url":""}],"extras":{"owned_acquisition_status":"ready","owned_track_id":"track-1"}},{"kind":"track","title":"Stranger Song","subtitle":"Artist","confidence":"high","result_signature":"track|stranger song|artist","favorite_key":"artist|stranger song","sources":[{"provider":"deezer","external_id":"2","url":""}],"extras":{}},{"kind":"track","title":"Placed Song","subtitle":"Artist","confidence":"high","result_signature":"track|placed song|artist","favorite_key":"artist|placed song","sources":[{"provider":"deezer","external_id":"3","url":""}],"extras":{"owned_acquisition_status":"pending","owned_track_id":"track-2","track_number":7}}],"partial":false}`
	ownershipArtistContentGolden = `{"top_tracks":{"provider_name":"deezer","status":"ok","items":[{"kind":"track","title":"Owned Song","subtitle":"Artist","confidence":"high","result_signature":"track|owned song|artist","favorite_key":"artist|owned song","sources":[{"provider":"deezer","external_id":"1","url":""}],"extras":{"owned_acquisition_status":"ready","owned_track_id":"track-1"}},{"kind":"track","title":"Stranger Song","subtitle":"Artist","confidence":"high","result_signature":"track|stranger song|artist","favorite_key":"artist|stranger song","sources":[{"provider":"deezer","external_id":"2","url":""}],"extras":{}},{"kind":"track","title":"Placed Song","subtitle":"Artist","confidence":"high","result_signature":"track|placed song|artist","favorite_key":"artist|placed song","sources":[{"provider":"deezer","external_id":"3","url":""}],"extras":{"owned_acquisition_status":"pending","owned_track_id":"track-2","track_number":7}},{"kind":"album","title":"Owned Song","subtitle":"Artist","confidence":"high","result_signature":"album|owned song|artist","favorite_key":"artist|owned song","sources":[{"provider":"deezer","external_id":"4","url":""}],"extras":{}}],"partial":false},"albums":{"provider_name":"deezer","status":"ok","items":[{"kind":"track","title":"Owned Song","subtitle":"Artist","confidence":"high","result_signature":"track|owned song|artist","favorite_key":"artist|owned song","sources":[{"provider":"deezer","external_id":"1","url":""}],"extras":{}},{"kind":"track","title":"Stranger Song","subtitle":"Artist","confidence":"high","result_signature":"track|stranger song|artist","favorite_key":"artist|stranger song","sources":[{"provider":"deezer","external_id":"2","url":""}],"extras":{}},{"kind":"track","title":"Placed Song","subtitle":"Artist","confidence":"high","result_signature":"track|placed song|artist","favorite_key":"artist|placed song","sources":[{"provider":"deezer","external_id":"3","url":""}],"extras":{"track_number":7}}],"partial":false}}`
	ownershipRelatedGolden       = `{"provider_name":"soundcloud","status":"ok","items":[{"kind":"track","title":"Owned Song","subtitle":"Artist","confidence":"high","result_signature":"track|owned song|artist","favorite_key":"artist|owned song","sources":[{"provider":"deezer","external_id":"1","url":""}],"extras":{"owned_acquisition_status":"ready","owned_track_id":"track-1"}},{"kind":"track","title":"Stranger Song","subtitle":"Artist","confidence":"high","result_signature":"track|stranger song|artist","favorite_key":"artist|stranger song","sources":[{"provider":"deezer","external_id":"2","url":""}],"extras":{}},{"kind":"track","title":"Placed Song","subtitle":"Artist","confidence":"high","result_signature":"track|placed song|artist","favorite_key":"artist|placed song","sources":[{"provider":"deezer","external_id":"3","url":""}],"extras":{"owned_acquisition_status":"pending","owned_track_id":"track-2","track_number":7}},{"kind":"album","title":"Owned Song","subtitle":"Artist","confidence":"high","result_signature":"album|owned song|artist","favorite_key":"artist|owned song","sources":[{"provider":"deezer","external_id":"4","url":""}],"extras":{}}],"partial":false}`
)

func TestOwnershipGolden_Search(t *testing.T) {
	bg := searchtest.NewTracker(t)
	provider := &fakeSearchProvider{name: search.ProviderDeezer, results: goldenTracks()}
	router := goldenRouter(t, searchtest.CompleteServices(t, search.DiscoveryServices{
		Search: searchtest.NewSearchService(t, bg, []search.SearchProvider{provider}, search.NewCircuitBreaker()),
	}), &recordingTrackNumberFiller{done: make(chan struct{})})

	rec := discServe(t, router, http.MethodGet, "/discovery/search?q=song&save_history=false", nil)
	discAssertStatus(t, rec, http.StatusOK)
	assertGoldenBody(t, rec.Body.String(), ownershipSearchGolden)
}

func TestOwnershipGolden_AlbumTracksStampsAndFills(t *testing.T) {
	albumSvc := searchtest.NewAlbumTracks(t, map[search.ProviderName]search.AlbumContentProvider{
		search.ProviderDeezer: &search.FakeAlbumContentProvider{Results: goldenAlbumPageTracks()},
	})
	filler := &recordingTrackNumberFiller{done: make(chan struct{}), want: 1}
	router := goldenRouter(t, searchtest.CompleteServices(t, search.DiscoveryServices{Album: albumSvc}), filler)

	rec := discServe(t, router, http.MethodGet, "/discovery/albums/deezer/9/tracks?title=Deluxe", nil)
	discAssertStatus(t, rec, http.StatusOK)
	assertGoldenBody(t, rec.Body.String(), ownershipAlbumTracksGolden)

	select {
	case <-filler.done:
	case <-time.After(2 * time.Second):
		t.Fatal("track number fill never ran")
	}
	time.Sleep(50 * time.Millisecond)
	filler.mu.Lock()
	defer filler.mu.Unlock()
	sort.Slice(filler.calls, func(i, j int) bool { return filler.calls[i].trackID < filler.calls[j].trackID })
	if len(filler.calls) != 1 || filler.calls[0] != (fillCall{"track-1", 1}) {
		t.Errorf("fill calls = %+v, want only track-1 at position 1", filler.calls)
	}
}

func goldenArtistSvc(t testing.TB) *search.GetArtistContentService {
	return searchtest.NewArtistContent(t, map[search.ProviderName]search.ArtistContentProvider{
		search.ProviderDeezer: &search.FakeArtistContentProvider{TopTracks: goldenTracks(), Albums: goldenTracks()},
	})
}

func TestOwnershipGolden_ArtistEndpoints(t *testing.T) {
	cases := []struct {
		name, path, want string
	}{
		{"top tracks", "/discovery/artists/deezer/9/top-tracks?limit=10", ownershipTopTracksGolden},
		{"albums", "/discovery/artists/deezer/9/albums", ownershipArtistAlbumsGolden},
		{"content", "/discovery/artists/deezer/9/content?tracks_limit=10", ownershipArtistContentGolden},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			filler := &recordingTrackNumberFiller{done: make(chan struct{})}
			router := goldenRouter(t, searchtest.CompleteServices(t, search.DiscoveryServices{Artist: goldenArtistSvc(t)}), filler)

			rec := discServe(t, router, http.MethodGet, tc.path, nil)
			discAssertStatus(t, rec, http.StatusOK)
			assertGoldenBody(t, rec.Body.String(), tc.want)

			time.Sleep(50 * time.Millisecond)
			filler.mu.Lock()
			defer filler.mu.Unlock()
			if len(filler.calls) != 0 {
				t.Errorf("fill calls = %+v, want none outside album tracks", filler.calls)
			}
		})
	}
}

func TestOwnershipGolden_Related(t *testing.T) {
	relatedSvc := searchtest.NewRelatedTracks(t, map[string]search.RelatedTracksProvider{
		"soundcloud": &fakeRelatedTracksProvider{results: goldenTracks()},
	})
	router := goldenRouter(t, searchtest.CompleteServices(t, search.DiscoveryServices{Related: relatedSvc}), &recordingTrackNumberFiller{done: make(chan struct{})})

	rec := discServe(t, router, http.MethodGet, "/discovery/tracks/soundcloud/9/related", nil)
	discAssertStatus(t, rec, http.StatusOK)
	assertGoldenBody(t, rec.Body.String(), ownershipRelatedGolden)
}
