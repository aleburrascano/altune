package app

import (
	"altune/go-api/internal/admin/requeststore"
	"altune/go-api/internal/discovery/domain"
	"context"
	"errors"
	"regexp"
	"testing"

	discoveryPorts "altune/go-api/internal/discovery/ports"
	discoveryService "altune/go-api/internal/discovery/service"
)

// scriptedContentProvider returns fixed albums/top tracks, or err when set.
type scriptedContentProvider struct {
	albums, tracks []domain.SearchResult
	err            error
}

func (p scriptedContentProvider) GetArtistAlbums(context.Context, domain.ProviderName, string) ([]domain.SearchResult, error) {
	return p.albums, p.err
}

func (p scriptedContentProvider) GetArtistTopTracks(context.Context, domain.ProviderName, string) ([]domain.SearchResult, error) {
	return p.tracks, p.err
}

func sourcedResult(kind domain.ResultKind, title string, pn domain.ProviderName) domain.SearchResult {
	return domain.SearchResult{
		Kind:       kind,
		Title:      title,
		Year:       2020,
		Sources:    []domain.SourceRef{{Provider: pn, ExternalID: pn.String() + "-" + title}},
		RecordType: "album",
	}
}

var tookMsPattern = regexp.MustCompile(`"took_ms":\d+`)

// TestReRunDetail_wireShapePinned pins the exact admin JSON of /rerun-detail
// across every seed outcome the fan-out produces: an ok provider (deezer), a
// failing provider (soundcloud), an unregistered provider (itunes) and the
// MBID-keyed lastfm top-tracks probe. Provider names and statuses must stay
// byte-identical however they are represented internally.
func TestReRunDetail_wireShapePinned(t *testing.T) {
	artist := domain.SearchResult{
		Kind:       domain.ResultKindArtist,
		Title:      "Wire Artist",
		Subtitle:   "Artist",
		MBID:       "mbid-1",
		Confidence: domain.ConfidenceHigh,
		Popularity: 80,
		Sources: []domain.SourceRef{
			{Provider: domain.ProviderDeezer, ExternalID: "dz-1"},
			{Provider: domain.ProviderSoundCloud, ExternalID: "sc-1"},
			{Provider: domain.ProviderITunes, ExternalID: "it-1"},
			{Provider: domain.ProviderDeezer, ExternalID: "dz-dup"},
		},
	}
	searchSvc := discoveryService.NewService([]discoveryPorts.SearchProvider{
		outageProvider{name: domain.ProviderDeezer, results: []domain.SearchResult{artist}},
	}, discoveryService.NewCircuitBreaker())
	artistSvc := discoveryService.NewGetArtistContentService(map[domain.ProviderName]discoveryPorts.ArtistContentProvider{
		domain.ProviderDeezer: scriptedContentProvider{
			albums: []domain.SearchResult{sourcedResult(domain.ResultKindAlbum, "Alpha", domain.ProviderDeezer)},
			tracks: []domain.SearchResult{sourcedResult(domain.ResultKindTrack, "Song", domain.ProviderDeezer)},
		},
		domain.ProviderSoundCloud: scriptedContentProvider{err: errors.New("boom")},
		domain.ProviderLastFM: scriptedContentProvider{
			tracks: []domain.SearchResult{sourcedResult(domain.ResultKindTrack, "song", domain.ProviderLastFM)},
		},
	})

	body := serveDetail(t, func(ctx context.Context, query string) (requeststore.DetailReRunResult, error) {
		return reRunDetail(ctx, searchSvc, artistSvc, inspectorBudget, query)
	}, "Wire Artist")

	got := tookMsPattern.ReplaceAllString(body, `"took_ms":0`)
	const want = `{"query":"Wire Artist","resolved":{"title":"Wire Artist","subtitle":"Artist","mbid":"mbid-1","sources":{"deezer":"dz-1","itunes":"it-1","soundcloud":"sc-1"}},` +
		`"album_seeds":[{"provider":"deezer","external_id":"dz-1","status":"ok","items":[{"title":"Alpha","subtitle":"","year":2020,"track_count":0,"record_type":"album","image_url":"","sources":["deezer"]}]},` +
		`{"provider":"soundcloud","external_id":"sc-1","status":"error","items":[]},` +
		`{"provider":"itunes","external_id":"it-1","status":"error","items":[]}],` +
		`"track_seeds":[{"provider":"deezer","external_id":"dz-1","status":"ok","items":[{"title":"Song","subtitle":"","year":2020,"track_count":0,"record_type":"album","image_url":"","sources":["deezer"]}]},` +
		`{"provider":"soundcloud","external_id":"sc-1","status":"error","items":[]},` +
		`{"provider":"lastfm","external_id":"mbid-1","status":"ok","items":[{"title":"song","subtitle":"","year":2020,"track_count":0,"record_type":"album","image_url":"","sources":["lastfm"]}]}],` +
		`"albums":[{"title":"Alpha","subtitle":"","year":2020,"track_count":0,"record_type":"album","image_url":"","sources":["deezer"]}],` +
		`"top_tracks":[{"title":"Song","subtitle":"","year":2020,"track_count":0,"record_type":"album","image_url":"","sources":["deezer"]}],"took_ms":0}` + "\n"
	if got != want {
		t.Errorf("admin JSON drifted.\n got: %s\nwant: %s", got, want)
	}
}
