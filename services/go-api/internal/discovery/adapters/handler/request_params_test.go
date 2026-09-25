package handler

import (
	"altune/go-api/internal/discovery/ports"
	"net/http"
	"net/url"
	"strings"
	"testing"

	discdomain "altune/go-api/internal/discovery/domain"

	"github.com/go-chi/chi/v5"
)

// Every identifier and name below is interpolated into a provider URL, so the
// handler is the last place that can refuse one addressing a path the caller
// was never given, or one long enough to spend MusicBrainz's shared 1 req/s
// budget for every other caller.

// oversizedParam is past every free-text cap, at the size the enrichment
// routes were driven with before they had one.
var oversizedParam = strings.Repeat("a", 5000)

const radioheadMBID = "a74b1b7f-71a5-4011-9441-d0b5e4122711"

// paramValidationRouter serves the content and enrichment routes over
// providers that answer, so a rejected request is rejected by validation and
// not by a missing service.
func paramValidationRouter(t *testing.T) chi.Router {
	t.Helper()
	albumProviders := map[discdomain.ProviderName]ports.AlbumContentProvider{
		discdomain.ProviderDeezer: &fakeAlbumContentProvider{results: seedTracks(3)},
	}
	artistProviders := map[discdomain.ProviderName]ports.ArtistContentProvider{
		discdomain.ProviderDeezer: &fakeArtistContentProvider{albums: seedTracks(3)},
	}
	return buildDiscoveryRouter(
		&fakeSearchProvider{name: discdomain.ProviderDeezer}, &fakeSearchHistoryRepo{},
		albumProviders, artistProviders)
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
		{"a last.fm ref addressed by the parent segment", "/discovery/artists/lastfm/../albums"},
		{"a last.fm ref past the length cap", "/discovery/artists/lastfm/" + strings.Repeat("N", 201) + "/albums"},
	}
	for _, c := range cases {
		t.Run(c.cause, func(t *testing.T) {
			rec := discServe(t, paramValidationRouter(t), http.MethodGet, c.path, nil)

			discAssertStatus(t, rec, http.StatusBadRequest)
			if got := rejectionCode(t, rec); got != requestCodeInvalidParam {
				t.Errorf("code = %q, want %q", got, requestCodeInvalidParam)
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
		{"an oversized last.fm title", "/discovery/enrichment/lastfm?kind=artist&title=" + oversizedParam},
		{"an oversized deezer title", "/discovery/enrichment/deezer?kind=album&title=" + oversizedParam},
		{"an oversized lyrics title", "/discovery/lyrics?title=" + oversizedParam},
		{"an oversized album-tracks title", "/discovery/albums/deezer/123/tracks?title=" + oversizedParam},
		{"an oversized artist name", "/discovery/artists/deezer/123/albums?name=" + oversizedParam},
		{"a title one rune past the cap", "/discovery/enrichment?kind=artist&title=" + strings.Repeat("a", 201)},
	}
	for _, c := range cases {
		t.Run(c.cause, func(t *testing.T) {
			rec := discServe(t, paramValidationRouter(t), http.MethodGet, c.path, nil)

			discAssertStatus(t, rec, http.StatusBadRequest)
			if got := rejectionCode(t, rec); got != requestCodeInvalidParam {
				t.Errorf("code = %q, want %q", got, requestCodeInvalidParam)
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

// realProviderIDs carries one id per provider in the shape that provider
// actually issues. An id a user can reach today must keep serving, so this is
// the other arm of the rejections above.
var realProviderIDs = []struct {
	name       string
	provider   discdomain.ProviderName
	externalID string
}{
	{"deezer artist id", discdomain.ProviderDeezer, "27"},
	{"itunes artist id", discdomain.ProviderITunes, "159260351"},
	{"apple music artist id", discdomain.ProviderAppleMusic, "1258279972"},
	{"soundcloud user id", discdomain.ProviderSoundCloud, "3207"},
	{"spotify base62 id", discdomain.ProviderSpotify, "4Z8W4fKeB5YxbusRsdQVPb"},
	{"spotify uri", discdomain.ProviderSpotify, "spotify:artist:4Z8W4fKeB5YxbusRsdQVPb"},
	{"musicbrainz uuid", discdomain.ProviderMusicBrainz, radioheadMBID},
	{"youtube channel id", discdomain.ProviderYouTube, "UCmMUZbaYdNH0bEd1PAlAqsA"},
	{"youtube browse id", discdomain.ProviderYouTube, "MPREb_4pL8gzRtw1p"},
	{"amazon music asin", discdomain.ProviderAmazonMusic, "B00W0FHN1U"},
	{"theaudiodb artist id", discdomain.ProviderTheAudioDB, "111239"},
	{"discogs artist id", discdomain.ProviderDiscogs, "3840"},
	{"last.fm name ref", discdomain.ProviderLastFM, "Sigur Rós"},
	{"last.fm url segment ref", discdomain.ProviderLastFM, "Sigur+R%C3%B3s"},
}

func allProvidersArtistRouter(t *testing.T) chi.Router {
	t.Helper()
	artistProviders := make(map[discdomain.ProviderName]ports.ArtistContentProvider, len(realProviderIDs))
	for _, c := range realProviderIDs {
		artistProviders[c.provider] = &fakeArtistContentProvider{albums: seedTracks(1)}
	}
	return buildDiscoveryRouter(
		&fakeSearchProvider{name: discdomain.ProviderDeezer}, &fakeSearchHistoryRepo{}, nil, artistProviders)
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
