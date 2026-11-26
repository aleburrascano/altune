package handler_test

import (
	"altune/go-api/internal/search"
	"altune/go-api/internal/search/searchtest"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
)

var oversizedParam = strings.Repeat("a", 5000)

const radioheadMBID = "a74b1b7f-71a5-4011-9441-d0b5e4122711"

func paramValidationRouter(t *testing.T) chi.Router {
	t.Helper()
	bg := searchtest.NewTracker(t)
	albumProviders := map[search.ProviderName]search.AlbumContentProvider{
		search.ProviderDeezer: &search.FakeAlbumContentProvider{Results: seedTracks(3)},
	}
	artistProviders := map[search.ProviderName]search.ArtistContentProvider{
		search.ProviderDeezer: &search.FakeArtistContentProvider{Albums: seedTracks(3)},
	}
	historyRepo := &fakeSearchHistoryRepo{}
	h := search.MustNewDiscoveryHandler(searchtest.CompleteServices(t, search.DiscoveryServices{
		Search:       searchtest.NewSearchServiceWith(t, bg, []search.SearchProvider{&fakeSearchProvider{name: search.ProviderDeezer}}, search.NewCircuitBreaker(), func(c *search.Collaborators) { c.History = historyRepo }),
		History:      searchtest.NewListHistory(t, historyRepo),
		ClearHistory: searchtest.NewClearHistory(t, historyRepo),
		Album:        searchtest.NewAlbumTracks(t, albumProviders),
		Artist:       searchtest.NewArtistContent(t, artistProviders),
		Enrich:       mustEnrichment(search.NewEnrichmentService(&fakeMetadataEnricher{}, search.NoArtwork{}, search.NoEnrichmentCache{}, search.NoMBIDIndex{})),
	}), time.Now)
	r := chi.NewRouter()
	r.Use(discAsTestUser)
	r.Mount("/discovery", h.Routes())
	return r
}

func TestContentRejections_ExternalIDAddressingAnotherPath(t *testing.T) {
	cases := []struct {
		cause string
		path  string
	}{
		{"album tracks addressed by the parent segment", "/discovery/albums/deezer/../tracks"},
		{"artist albums addressed by the parent segment", "/discovery/artists/deezer/../albums"},
		{"artist content addressed by the current segment", "/discovery/artists/deezer/./content"},
		{"related tracks addressed by the parent segment", "/discovery/tracks/deezer/../related"},
		{"top tracks addressed by an escaped parent segment", "/discovery/artists/deezer/%2e%2e/top-tracks"},
		{"an id carrying a path separator", "/discovery/artists/deezer/a%2Fb/albums"},
		{"an id past the length cap", "/discovery/artists/deezer/" + strings.Repeat("7", 300) + "/albums"},
	}
	for _, c := range cases {
		t.Run(c.cause, func(t *testing.T) {
			rec := discServe(t, paramValidationRouter(t), http.MethodGet, c.path, nil)

			discAssertStatus(t, rec, http.StatusBadRequest)
			if got := rejectionCode(t, rec); got != "discovery.invalid_param" {
				t.Errorf("code = %q, want %q", got, "discovery.invalid_param")
			}
		})
	}
}

func TestEnrichmentRejections_MalformedMBIDAndOversizedNames(t *testing.T) {
	cases := []struct {
		cause string
		path  string
	}{
		{"an mbid addressing the parent segment", "/discovery/enrichment?kind=artist&title=x&mbid=.."},
		{"an mbid that is not a UUID", "/discovery/enrichment?kind=artist&title=x&mbid=abc"},
		{"an mbid in a non-canonical UUID form", "/discovery/enrichment?kind=artist&title=x&mbid=urn:uuid:" + radioheadMBID},
		{"an album-tracks mbid addressing the parent segment", "/discovery/albums/deezer/123/tracks?mbid=.."},
		{"an oversized enrichment title", "/discovery/enrichment?kind=artist&title=" + oversizedParam},
		{"an oversized enrichment subtitle", "/discovery/enrichment?kind=album&title=x&subtitle=" + oversizedParam},
		{"an oversized deezer title", "/discovery/enrichment/deezer?kind=album&title=" + oversizedParam},
		{"an oversized lyrics title", "/discovery/lyrics?title=" + oversizedParam},
		{"an oversized album-tracks title", "/discovery/albums/deezer/123/tracks?title=" + oversizedParam},
		{"a title one rune past the cap", "/discovery/enrichment?kind=artist&title=" + strings.Repeat("a", 201)},
	}
	for _, c := range cases {
		t.Run(c.cause, func(t *testing.T) {
			rec := discServe(t, paramValidationRouter(t), http.MethodGet, c.path, nil)

			discAssertStatus(t, rec, http.StatusBadRequest)
			if got := rejectionCode(t, rec); got != "discovery.invalid_param" {
				t.Errorf("code = %q, want %q", got, "discovery.invalid_param")
			}
		})
	}
}

func TestEnrichment_CanonicalMBIDAndBoundedNamesStillServe(t *testing.T) {
	cases := []struct {
		name string
		path string
	}{
		{"a canonical mbid", "/discovery/enrichment?kind=artist&title=Radiohead&mbid=" + radioheadMBID},
		{"a title with an accent and punctuation", "/discovery/lyrics?title=N.Y.+State+of+Mind&subtitle=Sigur+R%C3%B3s"},
		{"a title exactly at the cap", "/discovery/enrichment?kind=artist&title=" + strings.Repeat("a", 200)},
		{"an album-tracks title and artist", "/discovery/albums/deezer/123/tracks?title=Illmatic&artist=Nas"},
		{"an artist name on a content route", "/discovery/artists/deezer/123/albums?name=Nas"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rec := discServe(t, paramValidationRouter(t), http.MethodGet, c.path, nil)

			discAssertStatus(t, rec, http.StatusOK)
		})
	}
}

var realProviderIDs = []struct {
	name       string
	provider   search.ProviderName
	externalID string
}{
	{"deezer artist id", search.ProviderDeezer, "27"},
	{"itunes artist id", search.ProviderITunes, "159260351"},
	{"apple music artist id", search.ProviderAppleMusic, "1258279972"},
	{"soundcloud user id", search.ProviderSoundCloud, "3207"},
	{"spotify base62 id", search.ProviderSpotify, "4Z8W4fKeB5YxbusRsdQVPb"},
	{"spotify uri", search.ProviderSpotify, "spotify:artist:4Z8W4fKeB5YxbusRsdQVPb"},
	{"musicbrainz uuid", search.ProviderMusicBrainz, radioheadMBID},
	{"youtube channel id", search.ProviderYouTube, "UCmMUZbaYdNH0bEd1PAlAqsA"},
	{"youtube browse id", search.ProviderYouTube, "MPREb_4pL8gzRtw1p"},
	{"theaudiodb artist id", search.ProviderTheAudioDB, "111239"},
	{"discogs artist id", search.ProviderDiscogs, "3840"},
}

func allProvidersArtistRouter(t *testing.T) chi.Router {
	t.Helper()
	artistProviders := make(map[search.ProviderName]search.ArtistContentProvider, len(realProviderIDs))
	for _, c := range realProviderIDs {
		artistProviders[c.provider] = &search.FakeArtistContentProvider{Albums: seedTracks(1)}
	}
	return buildDiscoveryRouter(t,
		&fakeSearchProvider{name: search.ProviderDeezer}, &fakeSearchHistoryRepo{}, nil, artistProviders)
}

func TestContentIDs_RealProviderIDsStillServe(t *testing.T) {
	for _, c := range realProviderIDs {
		t.Run(c.name, func(t *testing.T) {
			path := "/discovery/artists/" + c.provider.String() + "/" + url.PathEscape(c.externalID) + "/albums"

			rec := discServe(t, allProvidersArtistRouter(t), http.MethodGet, path, nil)

			discAssertStatus(t, rec, http.StatusOK)
		})
	}
}
