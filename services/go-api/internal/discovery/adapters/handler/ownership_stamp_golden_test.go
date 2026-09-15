package handler

import (
	"altune/go-api/internal/auth"
	"altune/go-api/internal/discovery/ports"
	"altune/go-api/internal/discovery/service"
	"altune/go-api/internal/shared"
	"context"
	"net/http"
	"regexp"
	"sort"
	"sync"
	"testing"
	"time"

	discdomain "altune/go-api/internal/discovery/domain"

	"github.com/go-chi/chi/v5"
)

// These goldens pin the wire bytes of every response ownership enrichment
// touches, captured before the enrichment moved out of the handler (#1075):
// which items get owned_track_id/owned_acquisition_status, and which album
// tracks get a track number filled.

type goldenOwnershipReader struct{ owned map[string]ports.OwnedTrack }

func (r goldenOwnershipReader) OwnedByTitleArtist(context.Context, shared.UserId) (map[string]ports.OwnedTrack, error) {
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

func (f *recordingTrackNumberFiller) FillTrackNumber(_ context.Context, _ shared.UserId, trackID string, position int) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, fillCall{trackID, position})
	if len(f.calls) == f.want {
		close(f.done)
	}
	return nil
}

func goldenOwned() goldenOwnershipReader {
	return goldenOwnershipReader{owned: map[string]ports.OwnedTrack{
		ports.OwnershipKey("Owned Song", "Artist"):  {TrackID: "track-1", AcquisitionStatus: "ready"},
		ports.OwnershipKey("Placed Song", "Artist"): {TrackID: "track-2", AcquisitionStatus: "pending"},
	}}
}

func goldenTrack(title, id string, extras map[string]any) discdomain.SearchResult {
	return discdomain.SearchResult{
		Kind: discdomain.ResultKindTrack, Title: title, Subtitle: "Artist",
		Confidence: discdomain.ConfidenceHigh,
		Sources:    []discdomain.SourceRef{{Provider: discdomain.ProviderDeezer, ExternalID: id}},
		Extras:     extras,
	}
}

func goldenAlbum(title, id string) discdomain.SearchResult {
	return discdomain.SearchResult{
		Kind: discdomain.ResultKindAlbum, Title: title, Subtitle: "Artist",
		Confidence: discdomain.ConfidenceHigh,
		Sources:    []discdomain.SourceRef{{Provider: discdomain.ProviderDeezer, ExternalID: id}},
	}
}

func goldenTracks() []discdomain.SearchResult {
	return []discdomain.SearchResult{
		goldenTrack("Owned Song", "1", nil),
		goldenTrack("Stranger Song", "2", map[string]any{}),
		goldenTrack("Placed Song", "3", map[string]any{"track_position": 7}),
		goldenAlbum("Owned Song", "4"),
	}
}

func goldenRouter(svcs DiscoveryServices, filler ports.TrackNumberFiller) chi.Router {
	h := wireGoldenOwnership(NewDiscoveryHandler(svcs), goldenOwned(), filler)
	r := chi.NewRouter()
	r.Use(auth.Middleware(discVerifyAsTestUser))
	r.Mount("/discovery", h.Routes())
	return r
}

// wireGoldenOwnership attaches ownership enrichment the way app wiring does.
func wireGoldenOwnership(h *DiscoveryHandler, reader ports.OwnershipReader, filler ports.TrackNumberFiller) *DiscoveryHandler {
	return h.WithOwnershipEnrichment(service.NewOwnershipEnrichmentService(reader, filler))
}

var searchIDPattern = regexp.MustCompile(`"search_id":"[^"]*"`)

func assertGoldenBody(t *testing.T, got, want string) {
	t.Helper()
	got = searchIDPattern.ReplaceAllString(got, `"search_id":"<id>"`)
	// WriteJSON's encoder terminates the body with a newline.
	if got != want+"\n" {
		t.Errorf("response JSON drifted.\n got: %s\nwant: %s", got, want)
	}
}

const (
	ownershipSearchGolden        = `{"query":"song","query_norm":"song","search_id":"<id>","results":[{"kind":"track","title":"Owned Song","subtitle":"Artist","confidence":"high","result_signature":"track|owned song|artist","favorite_key":"artist|owned song","sources":[{"provider":"deezer","external_id":"1","url":""}],"extras":{"owned_acquisition_status":"ready","owned_track_id":"track-1"}},{"kind":"album","title":"Owned Song","subtitle":"Artist","confidence":"high","result_signature":"album|owned song|artist","favorite_key":"artist|owned song","sources":[{"provider":"deezer","external_id":"4","url":""}],"extras":{}},{"kind":"track","title":"Placed Song","subtitle":"Artist","confidence":"high","result_signature":"track|placed song|artist","favorite_key":"artist|placed song","sources":[{"provider":"deezer","external_id":"3","url":""}],"extras":{"owned_acquisition_status":"pending","owned_track_id":"track-2","track_position":7}},{"kind":"track","title":"Stranger Song","subtitle":"Artist","confidence":"high","result_signature":"track|stranger song|artist","favorite_key":"artist|stranger song","sources":[{"provider":"deezer","external_id":"2","url":""}],"extras":{}}],"top_result":{"kind":"track","title":"Owned Song","subtitle":"Artist","confidence":"high","result_signature":"track|owned song|artist","favorite_key":"artist|owned song","sources":[{"provider":"deezer","external_id":"1","url":""}],"extras":{"owned_acquisition_status":"ready","owned_track_id":"track-1"}},"sections":[{"kind":"track","items":[{"kind":"track","title":"Placed Song","subtitle":"Artist","confidence":"high","result_signature":"track|placed song|artist","favorite_key":"artist|placed song","sources":[{"provider":"deezer","external_id":"3","url":""}],"extras":{"owned_acquisition_status":"pending","owned_track_id":"track-2","track_position":7}},{"kind":"track","title":"Stranger Song","subtitle":"Artist","confidence":"high","result_signature":"track|stranger song|artist","favorite_key":"artist|stranger song","sources":[{"provider":"deezer","external_id":"2","url":""}],"extras":{}}],"has_more":false},{"kind":"album","items":[{"kind":"album","title":"Owned Song","subtitle":"Artist","confidence":"high","result_signature":"album|owned song|artist","favorite_key":"artist|owned song","sources":[{"provider":"deezer","external_id":"4","url":""}],"extras":{}}],"has_more":false}],"providers":[{"provider":"deezer","status":"ok","latency_ms":0,"result_count":4}],"partial":false,"cache":{"hit":false,"fetched_at":null},"total":4,"offset":0,"has_more":false}`
	ownershipAlbumTracksGolden   = `{"provider_name":"deezer","status":"ok","items":[{"kind":"track","title":"Owned Song","subtitle":"Artist","confidence":"high","result_signature":"track|owned song|artist","favorite_key":"artist|owned song","sources":[{"provider":"deezer","external_id":"1","url":""}],"extras":{"owned_acquisition_status":"ready","owned_track_id":"track-1"}},{"kind":"track","title":"Stranger Song","subtitle":"Artist","confidence":"high","result_signature":"track|stranger song|artist","favorite_key":"artist|stranger song","sources":[{"provider":"deezer","external_id":"2","url":""}],"extras":{}},{"kind":"track","title":"Placed Song","subtitle":"Artist","confidence":"high","result_signature":"track|placed song|artist","favorite_key":"artist|placed song","sources":[{"provider":"deezer","external_id":"3","url":""}],"extras":{"owned_acquisition_status":"pending","owned_track_id":"track-2","track_position":7}},{"kind":"album","title":"Owned Song","subtitle":"Artist","confidence":"high","result_signature":"album|owned song|artist","favorite_key":"artist|owned song","sources":[{"provider":"deezer","external_id":"4","url":""}],"extras":{}}],"partial":false}`
	ownershipTopTracksGolden     = `{"provider_name":"deezer","status":"ok","items":[{"kind":"track","title":"Owned Song","subtitle":"Artist","confidence":"high","result_signature":"track|owned song|artist","favorite_key":"artist|owned song","sources":[{"provider":"deezer","external_id":"1","url":""}],"extras":{"owned_acquisition_status":"ready","owned_track_id":"track-1"}},{"kind":"track","title":"Stranger Song","subtitle":"Artist","confidence":"high","result_signature":"track|stranger song|artist","favorite_key":"artist|stranger song","sources":[{"provider":"deezer","external_id":"2","url":""}],"extras":{}},{"kind":"track","title":"Placed Song","subtitle":"Artist","confidence":"high","result_signature":"track|placed song|artist","favorite_key":"artist|placed song","sources":[{"provider":"deezer","external_id":"3","url":""}],"extras":{"owned_acquisition_status":"pending","owned_track_id":"track-2","track_position":7}},{"kind":"album","title":"Owned Song","subtitle":"Artist","confidence":"high","result_signature":"album|owned song|artist","favorite_key":"artist|owned song","sources":[{"provider":"deezer","external_id":"4","url":""}],"extras":{}}],"partial":false}`
	ownershipArtistAlbumsGolden  = `{"provider_name":"deezer","status":"ok","items":[{"kind":"track","title":"Owned Song","subtitle":"Artist","confidence":"high","result_signature":"track|owned song|artist","favorite_key":"artist|owned song","sources":[{"provider":"deezer","external_id":"1","url":""}],"extras":{"owned_acquisition_status":"ready","owned_track_id":"track-1"}},{"kind":"track","title":"Stranger Song","subtitle":"Artist","confidence":"high","result_signature":"track|stranger song|artist","favorite_key":"artist|stranger song","sources":[{"provider":"deezer","external_id":"2","url":""}],"extras":{}},{"kind":"track","title":"Placed Song","subtitle":"Artist","confidence":"high","result_signature":"track|placed song|artist","favorite_key":"artist|placed song","sources":[{"provider":"deezer","external_id":"3","url":""}],"extras":{"owned_acquisition_status":"pending","owned_track_id":"track-2","track_position":7}}],"partial":false}`
	ownershipArtistContentGolden = `{"top_tracks":{"provider_name":"deezer","status":"ok","items":[{"kind":"track","title":"Owned Song","subtitle":"Artist","confidence":"high","result_signature":"track|owned song|artist","favorite_key":"artist|owned song","sources":[{"provider":"deezer","external_id":"1","url":""}],"extras":{"owned_acquisition_status":"ready","owned_track_id":"track-1"}},{"kind":"track","title":"Stranger Song","subtitle":"Artist","confidence":"high","result_signature":"track|stranger song|artist","favorite_key":"artist|stranger song","sources":[{"provider":"deezer","external_id":"2","url":""}],"extras":{}},{"kind":"track","title":"Placed Song","subtitle":"Artist","confidence":"high","result_signature":"track|placed song|artist","favorite_key":"artist|placed song","sources":[{"provider":"deezer","external_id":"3","url":""}],"extras":{"owned_acquisition_status":"pending","owned_track_id":"track-2","track_position":7}},{"kind":"album","title":"Owned Song","subtitle":"Artist","confidence":"high","result_signature":"album|owned song|artist","favorite_key":"artist|owned song","sources":[{"provider":"deezer","external_id":"4","url":""}],"extras":{}}],"partial":false},"albums":{"provider_name":"deezer","status":"ok","items":[{"kind":"track","title":"Owned Song","subtitle":"Artist","confidence":"high","result_signature":"track|owned song|artist","favorite_key":"artist|owned song","sources":[{"provider":"deezer","external_id":"1","url":""}],"extras":{}},{"kind":"track","title":"Stranger Song","subtitle":"Artist","confidence":"high","result_signature":"track|stranger song|artist","favorite_key":"artist|stranger song","sources":[{"provider":"deezer","external_id":"2","url":""}],"extras":{}},{"kind":"track","title":"Placed Song","subtitle":"Artist","confidence":"high","result_signature":"track|placed song|artist","favorite_key":"artist|placed song","sources":[{"provider":"deezer","external_id":"3","url":""}],"extras":{"track_position":7}}],"partial":false}}`
	ownershipRelatedGolden       = `{"provider_name":"soundcloud","status":"ok","items":[{"kind":"track","title":"Owned Song","subtitle":"Artist","confidence":"high","result_signature":"track|owned song|artist","favorite_key":"artist|owned song","sources":[{"provider":"deezer","external_id":"1","url":""}],"extras":{"owned_acquisition_status":"ready","owned_track_id":"track-1"}},{"kind":"track","title":"Stranger Song","subtitle":"Artist","confidence":"high","result_signature":"track|stranger song|artist","favorite_key":"artist|stranger song","sources":[{"provider":"deezer","external_id":"2","url":""}],"extras":{}},{"kind":"track","title":"Placed Song","subtitle":"Artist","confidence":"high","result_signature":"track|placed song|artist","favorite_key":"artist|placed song","sources":[{"provider":"deezer","external_id":"3","url":""}],"extras":{"owned_acquisition_status":"pending","owned_track_id":"track-2","track_position":7}},{"kind":"album","title":"Owned Song","subtitle":"Artist","confidence":"high","result_signature":"album|owned song|artist","favorite_key":"artist|owned song","sources":[{"provider":"deezer","external_id":"4","url":""}],"extras":{}}],"partial":false}`
)

func TestOwnershipGolden_Search(t *testing.T) {
	provider := &fakeSearchProvider{name: discdomain.ProviderDeezer, results: goldenTracks()}
	router := goldenRouter(DiscoveryServices{
		Search: service.NewService([]ports.SearchProvider{provider}, service.NewCircuitBreaker()),
	}, &recordingTrackNumberFiller{done: make(chan struct{})})

	rec := discServe(t, router, http.MethodGet, "/discovery/search?q=song&save_history=false", nil)
	discAssertStatus(t, rec, http.StatusOK)
	assertGoldenBody(t, rec.Body.String(), ownershipSearchGolden)
}

func TestOwnershipGolden_AlbumTracksStampsAndFills(t *testing.T) {
	albumSvc := service.NewGetAlbumTracksService(map[discdomain.ProviderName]ports.AlbumContentProvider{
		discdomain.ProviderDeezer: &fakeAlbumContentProvider{results: goldenTracks()},
	})
	filler := &recordingTrackNumberFiller{done: make(chan struct{}), want: 1}
	router := goldenRouter(DiscoveryServices{Album: albumSvc}, filler)

	rec := discServe(t, router, http.MethodGet, "/discovery/albums/deezer/9/tracks", nil)
	discAssertStatus(t, rec, http.StatusOK)
	assertGoldenBody(t, rec.Body.String(), ownershipAlbumTracksGolden)

	select {
	case <-filler.done:
	case <-time.After(2 * time.Second):
		t.Fatal("track number fill never ran")
	}
	// Let any unexpected extra fill land before asserting the exact set.
	time.Sleep(50 * time.Millisecond)
	filler.mu.Lock()
	defer filler.mu.Unlock()
	sort.Slice(filler.calls, func(i, j int) bool { return filler.calls[i].trackID < filler.calls[j].trackID })
	if len(filler.calls) != 1 || filler.calls[0] != (fillCall{"track-1", 1}) {
		t.Errorf("fill calls = %+v, want only track-1 at position 1", filler.calls)
	}
}

func goldenArtistSvc() *service.GetArtistContentService {
	return service.NewGetArtistContentService(map[discdomain.ProviderName]ports.ArtistContentProvider{
		discdomain.ProviderDeezer: &fakeArtistContentProvider{topTracks: goldenTracks(), albums: goldenTracks()},
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
			router := goldenRouter(DiscoveryServices{Artist: goldenArtistSvc()}, filler)

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
	relatedSvc := service.NewGetRelatedTracksService(map[string]ports.RelatedTracksProvider{
		"soundcloud": &fakeRelatedTracksProvider{results: goldenTracks()},
	})
	router := goldenRouter(DiscoveryServices{Related: relatedSvc}, &recordingTrackNumberFiller{done: make(chan struct{})})

	rec := discServe(t, router, http.MethodGet, "/discovery/tracks/soundcloud/9/related", nil)
	discAssertStatus(t, rec, http.StatusOK)
	assertGoldenBody(t, rec.Body.String(), ownershipRelatedGolden)
}
