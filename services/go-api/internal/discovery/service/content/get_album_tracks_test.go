package content_test

import (
	"altune/go-api/internal/discovery"
	"altune/go-api/internal/discovery/discoverytest"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
)

type fakeAlbumSearcher struct {
	results []discovery.SearchResult
}

func (f *fakeAlbumSearcher) Name() discovery.ProviderName { return discovery.ProviderDeezer }
func (f *fakeAlbumSearcher) SupportedKinds() map[discovery.ResultKind]bool {
	return map[discovery.ResultKind]bool{discovery.ResultKindAlbum: true}
}

func (f *fakeAlbumSearcher) Search(_ context.Context, _ string, _ map[discovery.ResultKind]bool) ([]discovery.SearchResult, error) {
	return f.results, nil
}

func TestGetAlbumTracks_fallbackSkipsWrongArtist(t *testing.T) {
	var fetchedID string
	deezer := &discoverytest.AlbumContentProvider{
		GetAlbumTracksFn: func(_ context.Context, _ discovery.ProviderName, id string) ([]discovery.SearchResult, error) {
			fetchedID = id
			return []discovery.SearchResult{{
				Kind: discovery.ResultKindTrack, Title: "Like Lil Mexico",
				Sources: []discovery.SourceRef{{Provider: discovery.ProviderDeezer, ExternalID: "cht"}},
			}}, nil
		},
	}
	searcher := &fakeAlbumSearcher{results: []discovery.SearchResult{
		discoverytest.AlbumSearchResult("Chase Fetti", "999"),
		discoverytest.AlbumSearchResult("Che", "111"),
	}}
	svc := discovery.NewGetAlbumTracksService(
		map[discovery.ProviderName]discovery.AlbumContentProvider{discovery.ProviderDeezer: deezer},
		discovery.WithAlbumFallbackSearcher(searcher),
	)

	resp, err := svc.ExecuteRequest(context.Background(), discovery.AlbumTracksRequest{Provider: discovery.ProviderSoundCloud, ExternalID: "sc-1", Title: "Empty Clip", Artist: "Che"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if fetchedID != "111" {
		t.Fatalf("fetched deezer album %q, want 111 (Che, not Chase Fetti's 999)", fetchedID)
	}
	if len(resp.Items) != 1 || resp.Items[0].Title != "Like Lil Mexico" {
		t.Fatalf("items = %+v, want Che's tracklist", resp.Items)
	}
}

func TestGetAlbumTracks_fallbackNoArtistMatchReturnsEmpty(t *testing.T) {
	deezer := &discoverytest.AlbumContentProvider{
		GetAlbumTracksFn: func(_ context.Context, _ discovery.ProviderName, _ string) ([]discovery.SearchResult, error) {
			t.Fatal("must not fetch a wrong-artist album")
			return nil, nil
		},
	}
	searcher := &fakeAlbumSearcher{results: []discovery.SearchResult{discoverytest.AlbumSearchResult("Chase Fetti", "999")}}
	svc := discovery.NewGetAlbumTracksService(
		map[discovery.ProviderName]discovery.AlbumContentProvider{discovery.ProviderDeezer: deezer},
		discovery.WithAlbumFallbackSearcher(searcher),
	)

	resp, err := svc.ExecuteRequest(context.Background(), discovery.AlbumTracksRequest{Provider: discovery.ProviderSoundCloud, ExternalID: "sc-1", Title: "Empty Clip", Artist: "Che"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(resp.Items) != 0 {
		t.Fatalf("items = %d, want 0 (no artist match → empty, not wrong)", len(resp.Items))
	}
}

func TestGetAlbumTracksService_ExecuteRequest(t *testing.T) {
	sampleTracks := []discovery.SearchResult{
		{Kind: discovery.ResultKindTrack, Title: "Track 1", Sources: []discovery.SourceRef{{Provider: discovery.ProviderDeezer, ExternalID: "t1"}}},
		{Kind: discovery.ResultKindTrack, Title: "Track 2", Sources: []discovery.SourceRef{{Provider: discovery.ProviderDeezer, ExternalID: "t2"}}},
		{Kind: discovery.ResultKindTrack, Title: "Track 3", Sources: []discovery.SourceRef{{Provider: discovery.ProviderDeezer, ExternalID: "t3"}}},
	}

	tests := []struct {
		name          string
		providerName  discovery.ProviderName
		externalID    string
		limit         int
		providers     map[discovery.ProviderName]discovery.AlbumContentProvider
		wantStatus    discovery.ProviderStatus
		wantItemCount int
	}{
		{
			name:         "valid provider returns tracks",
			providerName: discovery.ProviderDeezer,
			externalID:   "album-123",
			limit:        0,
			providers: map[discovery.ProviderName]discovery.AlbumContentProvider{
				discovery.ProviderDeezer: &discoverytest.AlbumContentProvider{
					GetAlbumTracksFn: func(_ context.Context, pn discovery.ProviderName, extID string) ([]discovery.SearchResult, error) {
						if pn != discovery.ProviderDeezer {
							t.Errorf("expected provider deezer, got %s", pn.String())
						}
						if extID != "album-123" {
							t.Errorf("expected externalID album-123, got %s", extID)
						}
						return sampleTracks, nil
					},
				},
			},
			wantStatus:    discovery.ProviderStatusOK,
			wantItemCount: 3,
		},
		{
			name:          "unknown provider returns error status",
			providerName:  discovery.ProviderSoundCloud,
			externalID:    "album-123",
			limit:         0,
			providers:     map[discovery.ProviderName]discovery.AlbumContentProvider{},
			wantStatus:    discovery.ProviderStatusError,
			wantItemCount: 0,
		},
		{
			name:         "provider error returns error status without propagating",
			providerName: discovery.ProviderDeezer,
			externalID:   "album-err",
			limit:        0,
			providers: map[discovery.ProviderName]discovery.AlbumContentProvider{
				discovery.ProviderDeezer: &discoverytest.AlbumContentProvider{
					GetAlbumTracksFn: func(_ context.Context, _ discovery.ProviderName, _ string) ([]discovery.SearchResult, error) {
						return nil, errors.New("upstream timeout")
					},
				},
			},
			wantStatus:    discovery.ProviderStatusError,
			wantItemCount: 0,
		},
		{
			name:         "limit truncates results",
			providerName: discovery.ProviderDeezer,
			externalID:   "album-123",
			limit:        2,
			providers: map[discovery.ProviderName]discovery.AlbumContentProvider{
				discovery.ProviderDeezer: &discoverytest.AlbumContentProvider{
					GetAlbumTracksFn: func(_ context.Context, _ discovery.ProviderName, _ string) ([]discovery.SearchResult, error) {
						return sampleTracks, nil
					},
				},
			},
			wantStatus:    discovery.ProviderStatusOK,
			wantItemCount: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := discovery.NewGetAlbumTracksService(tt.providers)

			resp, err := svc.ExecuteRequest(context.Background(), discovery.AlbumTracksRequest{Provider: tt.providerName, ExternalID: tt.externalID, Limit: tt.limit})
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if resp.Status != tt.wantStatus {
				t.Errorf("expected status %s, got %s", tt.wantStatus.String(), resp.Status.String())
			}
			if len(resp.Items) != tt.wantItemCount {
				t.Errorf("expected %d items, got %d", tt.wantItemCount, len(resp.Items))
			}
			if resp.ProviderName != tt.providerName {
				t.Errorf("expected provider name %q, got %q", tt.providerName, resp.ProviderName)
			}
		})
	}
}

const fallbackServedEvent = "album_tracks.deezer_fallback_served"

func albumTracksFallbackRequest() discovery.AlbumTracksRequest {
	return discovery.AlbumTracksRequest{Provider: discovery.ProviderSoundCloud, ExternalID: "sc-1", Title: "Empty Clip", Artist: "Che"}
}

func albumTracksWithFailingPrimary(primaryErr error, deezer discovery.AlbumContentProvider, searcher discovery.SearchProvider) *discovery.GetAlbumTracksService {
	return discovery.NewGetAlbumTracksService(
		map[discovery.ProviderName]discovery.AlbumContentProvider{
			discovery.ProviderSoundCloud: &discoverytest.AlbumContentProvider{
				GetAlbumTracksFn: func(context.Context, discovery.ProviderName, string) ([]discovery.SearchResult, error) {
					return nil, primaryErr
				},
			},
			discovery.ProviderDeezer: deezer,
		},
		discovery.WithAlbumFallbackSearcher(searcher),
	)
}

func unfetchableAlbumProvider(t *testing.T) *discoverytest.AlbumContentProvider {
	t.Helper()
	return &discoverytest.AlbumContentProvider{
		GetAlbumTracksFn: func(context.Context, discovery.ProviderName, string) ([]discovery.SearchResult, error) {
			t.Error("no album may be fetched once the fallback search found nothing")
			return nil, nil
		},
	}
}

func fallbackServedRecord(t *testing.T, logs *bytes.Buffer) map[string]any {
	t.Helper()
	for _, line := range strings.Split(strings.TrimSpace(logs.String()), "\n") {
		var record map[string]any
		if err := json.Unmarshal([]byte(line), &record); err != nil {
			continue
		}
		if record["msg"] == fallbackServedEvent {
			return record
		}
	}
	t.Fatalf("no %q record logged, got %s", fallbackServedEvent, logs.String())
	return nil
}

func TestGetAlbumTracks_fallbacklessAnswerCarriesPrimaryStatus(t *testing.T) {
	cases := []struct {
		name       string
		primaryErr error
		want       discovery.ProviderStatus
	}{
		{name: "upstream 503", primaryErr: discoverytest.StatusErr(503), want: discovery.ProviderStatusError},
		{name: "upstream 429", primaryErr: discoverytest.StatusErr(429), want: discovery.ProviderStatusRateLimited},
		{name: "upstream timeout", primaryErr: fmt.Errorf("fetch: %w", context.DeadlineExceeded), want: discovery.ProviderStatusTimeout},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svc := albumTracksWithFailingPrimary(tc.primaryErr, unfetchableAlbumProvider(t), erroringAlbumSearcher{})

			resp, err := svc.ExecuteRequest(context.Background(), albumTracksFallbackRequest())
			if err != nil {
				t.Fatalf("a failed fallback must degrade, not propagate: %v", err)
			}
			if resp.Status != tc.want {
				t.Errorf("status = %v, want %v (the primary's failure, not a healthy empty album)", resp.Status, tc.want)
			}
			if resp.ProviderName != discovery.ProviderSoundCloud {
				t.Errorf("provider = %v, want soundcloud (the provider that failed)", resp.ProviderName)
			}
		})
	}
}

func TestGetAlbumTracks_everyEmptyFallbackCarriesPrimaryStatus(t *testing.T) {
	emptyTracklist := &discoverytest.AlbumContentProvider{
		GetAlbumTracksFn: func(context.Context, discovery.ProviderName, string) ([]discovery.SearchResult, error) {
			return nil, nil
		},
	}
	cases := []struct {
		name     string
		searcher discovery.SearchProvider
		deezer   discovery.AlbumContentProvider
	}{
		{name: "search errored", searcher: erroringAlbumSearcher{}, deezer: unfetchableAlbumProvider(t)},
		{name: "search matched nothing", searcher: &fakeAlbumSearcher{}, deezer: unfetchableAlbumProvider(t)},
		{name: "search matched another artist", searcher: &fakeAlbumSearcher{results: []discovery.SearchResult{discoverytest.AlbumSearchResult("Chase Fetti", "999")}}, deezer: unfetchableAlbumProvider(t)},
		{name: "candidate has no tracklist", searcher: &fakeAlbumSearcher{results: []discovery.SearchResult{discoverytest.AlbumSearchResult("Che", "111")}}, deezer: emptyTracklist},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svc := albumTracksWithFailingPrimary(discoverytest.StatusErr(503), tc.deezer, tc.searcher)

			resp, err := svc.ExecuteRequest(context.Background(), albumTracksFallbackRequest())
			if err != nil {
				t.Fatalf("a failed fallback must degrade, not propagate: %v", err)
			}
			if resp.Status != discovery.ProviderStatusError || len(resp.Items) != 0 {
				t.Errorf("resp = %v/%d items, want the primary's error status and no items", resp.Status, len(resp.Items))
			}
		})
	}
}

func TestGetAlbumTracks_servedFallbackNamesTheProviderItStoodInFor(t *testing.T) {
	logs := discoverytest.CaptureProductionLogs(t)
	deezer := &discoverytest.AlbumContentProvider{
		GetAlbumTracksFn: func(context.Context, discovery.ProviderName, string) ([]discovery.SearchResult, error) {
			return []discovery.SearchResult{{
				Kind: discovery.ResultKindTrack, Title: "Like Lil Mexico",
				Sources: []discovery.SourceRef{{Provider: discovery.ProviderDeezer, ExternalID: "cht"}},
			}}, nil
		},
	}
	searcher := &fakeAlbumSearcher{results: []discovery.SearchResult{discoverytest.AlbumSearchResult("Che", "111")}}
	svc := albumTracksWithFailingPrimary(discoverytest.StatusErr(503), deezer, searcher)

	resp, err := svc.ExecuteRequest(context.Background(), albumTracksFallbackRequest())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Status != discovery.ProviderStatusOK || len(resp.Items) != 1 {
		t.Fatalf("resp = %v/%d items, want OK and the fallback tracklist", resp.Status, len(resp.Items))
	}
	if resp.ProviderName != discovery.ProviderDeezer || resp.FallbackFrom != discovery.ProviderSoundCloud {
		t.Errorf("provider = %v, fallback_from = %v, want deezer standing in for soundcloud",
			resp.ProviderName, resp.FallbackFrom)
	}
	record := fallbackServedRecord(t, logs)
	if record["requested_provider"] != discovery.ProviderSoundCloud.String() {
		t.Errorf("logged requested_provider = %v, want soundcloud", record["requested_provider"])
	}
}

func TestGetAlbumTracks_primaryAnswerIsNotMarkedAsAFallback(t *testing.T) {
	deezer := &discoverytest.AlbumContentProvider{
		GetAlbumTracksFn: func(context.Context, discovery.ProviderName, string) ([]discovery.SearchResult, error) {
			return []discovery.SearchResult{{
				Kind: discovery.ResultKindTrack, Title: "T",
				Sources: []discovery.SourceRef{{Provider: discovery.ProviderDeezer, ExternalID: "t"}},
			}}, nil
		},
	}
	svc := albumTracksSvc(deezer, &fakeAlbumSearcher{})

	resp, err := svc.ExecuteRequest(context.Background(), discovery.AlbumTracksRequest{Provider: discovery.ProviderDeezer, ExternalID: "d-1", Title: "Empty Clip"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.FallbackFrom != discovery.ProviderUnknown {
		t.Errorf("fallback_from = %v, want unknown (deezer answered for itself)", resp.FallbackFrom)
	}
}

type fakeTrackFeatured struct {
	byID map[string][]discovery.FeaturedArtist
	err  error
}

func (f fakeTrackFeatured) LookupTrackFeatured(_ context.Context, id string) ([]discovery.FeaturedArtist, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.byID[id], nil
}

func (fakeTrackFeatured) ResolveID(context.Context, discovery.ResultKind, string, string) (string, error) {
	return "", nil
}

func deezerTrackFeat(id, title string) discovery.SearchResult {
	return discovery.SearchResult{
		Kind:    discovery.ResultKindTrack,
		Title:   title,
		Sources: []discovery.SourceRef{{Provider: discovery.ProviderDeezer, ExternalID: id}},
		Extras:  map[string]any{},
	}
}

func featuredOf(track discovery.SearchResult) []discovery.FeaturedArtist {
	raw, _ := track.Extras["featured_artists"].([]map[string]any)
	out := make([]discovery.FeaturedArtist, 0, len(raw))
	for _, m := range raw {
		name, _ := m["name"].(string)
		deezerID, _ := m["deezer_id"].(int64)
		out = append(out, discovery.FeaturedArtist{Name: name, DeezerID: deezerID})
	}
	return out
}

func deezerAlbum(tracks []discovery.SearchResult, opts ...discovery.AlbumTracksOption) *discovery.GetAlbumTracksService {
	deezer := &discoverytest.AlbumContentProvider{Results: tracks}
	return discovery.NewGetAlbumTracksService(map[discovery.ProviderName]discovery.AlbumContentProvider{discovery.ProviderDeezer: deezer}, opts...)
}

func albumTrackItems(t *testing.T, svc *discovery.GetAlbumTracksService) []discovery.SearchResult {
	t.Helper()
	resp, err := svc.ExecuteRequest(t.Context(), discovery.AlbumTracksRequest{Provider: discovery.ProviderDeezer, ExternalID: "d-album"})
	if err != nil {
		t.Fatalf("ExecuteRequest: %v", err)
	}
	if resp.Status != discovery.ProviderStatusOK {
		t.Fatalf("status = %v, want ok", resp.Status)
	}
	return resp.Items
}

func TestGetAlbumTracks_DeezerTracksTakeTheirFeaturedArtists(t *testing.T) {
	t.Run("stamps featured onto deezer tracks", func(t *testing.T) {
		svc := deezerAlbum([]discovery.SearchResult{deezerTrackFeat("1", "Singapore"), deezerTrackFeat("2", "Lose It")},
			discovery.WithTrackFeatured(fakeTrackFeatured{
				byID: map[string][]discovery.FeaturedArtist{
					"1": {{Name: "Destroy Lonely", DeezerID: 99, Role: "featured"}},
				},
			}))
		items := albumTrackItems(t, svc)

		if feats := featuredOf(items[0]); len(feats) != 1 || feats[0].Name != "Destroy Lonely" || feats[0].DeezerID != 99 {
			t.Fatalf("track 0 featured = %v", items[0].Extras["featured_artists"])
		}
		if _, present := items[1].Extras["featured_artists"]; present {
			t.Errorf("track 1 should have no featured, got %v", items[1].Extras["featured_artists"])
		}
	})

	t.Run("lookup error degrades to no features", func(t *testing.T) {
		svc := deezerAlbum([]discovery.SearchResult{deezerTrackFeat("1", "X")}, discovery.WithTrackFeatured(fakeTrackFeatured{err: errors.New("rate limited")}))
		items := albumTrackItems(t, svc)
		if len(items) != 1 {
			t.Fatalf("items = %d, want the track served without features", len(items))
		}
		if _, present := items[0].Extras["featured_artists"]; present {
			t.Errorf("expected no featured on error, got %v", items[0].Extras["featured_artists"])
		}
	})

	t.Run("no lookup configured serves the tracks as they are", func(t *testing.T) {
		items := albumTrackItems(t, deezerAlbum([]discovery.SearchResult{deezerTrackFeat("1", "X")}))
		if len(items) != 1 {
			t.Fatalf("items = %d, want the track served", len(items))
		}
		if _, present := items[0].Extras["featured_artists"]; present {
			t.Errorf("no lookup configured, yet featured = %v", items[0].Extras["featured_artists"])
		}
	})
}

type erroringAlbumSearcher struct{}

func (erroringAlbumSearcher) Name() discovery.ProviderName { return discovery.ProviderDeezer }
func (erroringAlbumSearcher) SupportedKinds() map[discovery.ResultKind]bool {
	return map[discovery.ResultKind]bool{discovery.ResultKindAlbum: true}
}

func (erroringAlbumSearcher) Search(context.Context, string, map[discovery.ResultKind]bool) ([]discovery.SearchResult, error) {
	return nil, errors.New("search down")
}

func albumTracksSvc(deezer discovery.AlbumContentProvider, searcher discovery.SearchProvider) *discovery.GetAlbumTracksService {
	return discovery.NewGetAlbumTracksService(
		map[discovery.ProviderName]discovery.AlbumContentProvider{discovery.ProviderDeezer: deezer},
		discovery.WithAlbumFallbackSearcher(searcher),
	)
}

func TestGetAlbumTracks_fallbackArtistGuardFoldsDiacritics(t *testing.T) {
	var fetchedID string
	deezer := &discoverytest.AlbumContentProvider{
		GetAlbumTracksFn: func(_ context.Context, _ discovery.ProviderName, id string) ([]discovery.SearchResult, error) {
			fetchedID = id
			return []discovery.SearchResult{{
				Kind: discovery.ResultKindTrack, Title: "T",
				Sources: []discovery.SourceRef{{Provider: discovery.ProviderDeezer, ExternalID: "t"}},
			}}, nil
		},
	}
	searcher := &fakeAlbumSearcher{results: []discovery.SearchResult{discoverytest.AlbumSearchResult("Ché", "42")}}
	svc := albumTracksSvc(deezer, searcher)

	resp, err := svc.ExecuteRequest(context.Background(), discovery.AlbumTracksRequest{Provider: discovery.ProviderSoundCloud, ExternalID: "sc-1", Title: "Empty Clip", Artist: "Che"})
	if err != nil {
		t.Fatal(err)
	}
	if fetchedID != "42" || len(resp.Items) != 1 {
		t.Fatalf("fetched %q items %d, want the accented-subtitle album accepted", fetchedID, len(resp.Items))
	}
}

func TestGetAlbumTracks_fallbackArtistGuardRejectsFeatTaggedSubtitle(t *testing.T) {
	deezer := &discoverytest.AlbumContentProvider{
		GetAlbumTracksFn: func(context.Context, discovery.ProviderName, string) ([]discovery.SearchResult, error) {
			t.Error("guard unexpectedly accepted the feat-tagged subtitle (known false negative fixed?)")
			return nil, nil
		},
	}
	searcher := &fakeAlbumSearcher{results: []discovery.SearchResult{discoverytest.AlbumSearchResult("Che feat. Lil X", "42")}}
	svc := albumTracksSvc(deezer, searcher)

	resp, err := svc.ExecuteRequest(context.Background(), discovery.AlbumTracksRequest{Provider: discovery.ProviderSoundCloud, ExternalID: "sc-1", Title: "Empty Clip", Artist: "Che"})
	if err != nil {
		t.Fatal(err)
	}
	if len(resp.Items) != 0 {
		t.Fatalf("items = %d, want 0 (feat-tagged subtitle is skipped by the exact-match guard)", len(resp.Items))
	}
}

func TestGetAlbumTracks_fallbackNoArtistTakesFirstCandidate(t *testing.T) {
	var fetchedID string
	deezer := &discoverytest.AlbumContentProvider{
		GetAlbumTracksFn: func(_ context.Context, _ discovery.ProviderName, id string) ([]discovery.SearchResult, error) {
			fetchedID = id
			return []discovery.SearchResult{{
				Kind: discovery.ResultKindTrack, Title: "T",
				Sources: []discovery.SourceRef{{Provider: discovery.ProviderDeezer, ExternalID: "t"}},
			}}, nil
		},
	}
	searcher := &fakeAlbumSearcher{results: []discovery.SearchResult{
		{Kind: discovery.ResultKindAlbum, Title: "Empty Clip", Subtitle: "Whoever"},
		discoverytest.AlbumSearchResult("Chase Fetti", "999"),
	}}
	svc := albumTracksSvc(deezer, searcher)

	resp, err := svc.ExecuteRequest(context.Background(), discovery.AlbumTracksRequest{Provider: discovery.ProviderSoundCloud, ExternalID: "sc-1", Title: "Empty Clip"})
	if err != nil {
		t.Fatal(err)
	}
	if fetchedID != "999" || len(resp.Items) != 1 {
		t.Fatalf("fetched %q items %d, want the first SOURCED candidate fetched", fetchedID, len(resp.Items))
	}
}

func TestGetAlbumTracks_fallbackSearcherErrorKeepsTheRequestedProvidersFailure(t *testing.T) {
	deezer := &discoverytest.AlbumContentProvider{
		GetAlbumTracksFn: func(context.Context, discovery.ProviderName, string) ([]discovery.SearchResult, error) {
			t.Error("no album may be fetched when the fallback search errored")
			return nil, nil
		},
	}
	svc := albumTracksSvc(deezer, erroringAlbumSearcher{})

	resp, err := svc.ExecuteRequest(context.Background(), discovery.AlbumTracksRequest{Provider: discovery.ProviderSoundCloud, ExternalID: "sc-1", Title: "Empty Clip", Artist: "Che"})
	if err != nil {
		t.Fatalf("fallback search failure must degrade, not propagate: %v", err)
	}
	if len(resp.Items) != 0 {
		t.Fatalf("items = %d, want 0", len(resp.Items))
	}
	if resp.ProviderName != discovery.ProviderSoundCloud || !resp.Unserved {
		t.Errorf("resp = %v/unserved %v, want the requested provider's own failure kept",
			resp.ProviderName, resp.Unserved)
	}
}

func TestGetAlbumTracks_fallbackSkipsCandidateWithNoTracks(t *testing.T) {
	fetched := []string{}
	deezer := &discoverytest.AlbumContentProvider{
		GetAlbumTracksFn: func(_ context.Context, _ discovery.ProviderName, id string) ([]discovery.SearchResult, error) {
			fetched = append(fetched, id)
			if id == "111" {
				return nil, nil
			}
			return []discovery.SearchResult{{
				Kind: discovery.ResultKindTrack, Title: "T",
				Sources: []discovery.SourceRef{{Provider: discovery.ProviderDeezer, ExternalID: "t"}},
			}}, nil
		},
	}
	searcher := &fakeAlbumSearcher{results: []discovery.SearchResult{
		discoverytest.AlbumSearchResult("Che", "111"),
		discoverytest.AlbumSearchResult("Che", "222"),
	}}
	svc := albumTracksSvc(deezer, searcher)

	resp, err := svc.ExecuteRequest(context.Background(), discovery.AlbumTracksRequest{Provider: discovery.ProviderSoundCloud, ExternalID: "sc-1", Title: "Empty Clip", Artist: "Che"})
	if err != nil {
		t.Fatal(err)
	}
	if len(fetched) != 2 || fetched[1] != "222" {
		t.Fatalf("fetched = %v, want the empty candidate skipped and the next tried", fetched)
	}
	if len(resp.Items) != 1 {
		t.Fatalf("items = %d, want the second candidate's tracklist", len(resp.Items))
	}
}

func TestGetAlbumTracks_primaryEmptyWithNoTitleKeepsEmptyOK(t *testing.T) {
	deezer := &discoverytest.AlbumContentProvider{
		GetAlbumTracksFn: func(context.Context, discovery.ProviderName, string) ([]discovery.SearchResult, error) {
			return nil, nil
		},
	}
	svc := albumTracksSvc(deezer, &fakeAlbumSearcher{})

	resp, err := svc.ExecuteRequest(context.Background(), discovery.AlbumTracksRequest{Provider: discovery.ProviderDeezer, ExternalID: "d-1"})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Status != discovery.ProviderStatusOK || len(resp.Items) != 0 {
		t.Fatalf("resp = %v/%d items, want OK/0", resp.Status, len(resp.Items))
	}
}

type panickingTrackFeatured struct{ fakeTrackFeatured }

func (panickingTrackFeatured) LookupTrackFeatured(context.Context, string) ([]discovery.FeaturedArtist, error) {
	panic("featured lookup exploded")
}

func TestGetAlbumTracks_PanickingFeaturedLookupIsContained(t *testing.T) {
	items := albumTrackItems(t, deezerAlbum([]discovery.SearchResult{deezerTrackFeat("1", "Singapore")}, discovery.WithTrackFeatured(panickingTrackFeatured{})))

	if len(items) != 1 {
		t.Fatalf("items = %d, want the track served despite the panicking lookup", len(items))
	}
	if _, present := items[0].Extras["featured_artists"]; present {
		t.Errorf("featured should be unset after a panicking lookup, got %v", items[0].Extras["featured_artists"])
	}
}

func albumCandidate(title, artist, deezerID string) discovery.SearchResult {
	r := discoverytest.AlbumSearchResult(artist, deezerID)
	r.Title = title
	return r
}

func singleTrackDeezer() *discoverytest.AlbumContentProvider {
	return &discoverytest.AlbumContentProvider{
		GetAlbumTracksFn: func(context.Context, discovery.ProviderName, string) ([]discovery.SearchResult, error) {
			return []discovery.SearchResult{{
				Kind: discovery.ResultKindTrack, Title: "Like Lil Mexico",
				Sources: []discovery.SourceRef{{Provider: discovery.ProviderDeezer, ExternalID: "cht"}},
			}}, nil
		},
	}
}

func TestGetAlbumTracks_fallbackRefusesSameArtistDifferentTitle(t *testing.T) {
	for _, artist := range []string{"Che", ""} {
		t.Run("artist="+artist, func(t *testing.T) {
			searcher := &fakeAlbumSearcher{results: []discovery.SearchResult{albumCandidate("Another Album", "Che", "222")}}
			svc := albumTracksWithFailingPrimary(discoverytest.StatusErr(503), singleTrackDeezer(), searcher)
			req := albumTracksFallbackRequest()
			req.Artist = artist

			resp, err := svc.ExecuteRequest(context.Background(), req)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if resp.Status != discovery.ProviderStatusError || len(resp.Items) != 0 {
				t.Errorf("resp = %v/%d items, want the primary's error status and no items", resp.Status, len(resp.Items))
			}
		})
	}
}

func TestGetAlbumTracks_fallbackAcceptsTitleEquivalentUnderNormaliser(t *testing.T) {
	for _, title := range []string{"EMPTY CLIP", "Empty Clip (Deluxe Edition)"} {
		t.Run(title, func(t *testing.T) {
			searcher := &fakeAlbumSearcher{results: []discovery.SearchResult{albumCandidate(title, "Che", "111")}}
			svc := albumTracksWithFailingPrimary(discoverytest.StatusErr(503), singleTrackDeezer(), searcher)

			resp, err := svc.ExecuteRequest(context.Background(), albumTracksFallbackRequest())
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if resp.Status != discovery.ProviderStatusOK || len(resp.Items) != 1 {
				t.Errorf("resp = %v/%d items, want OK and the fallback tracklist", resp.Status, len(resp.Items))
			}
		})
	}
}

type countingTrackFeatured struct {
	fakeTrackFeatured
	calls *atomic.Int32
}

func (c countingTrackFeatured) LookupTrackFeatured(ctx context.Context, id string) ([]discovery.FeaturedArtist, error) {
	c.calls.Add(1)
	return c.fakeTrackFeatured.LookupTrackFeatured(ctx, id)
}

func spotifyTrackFeat(title string, featured ...string) discovery.SearchResult {
	r := discovery.SearchResult{
		Kind:     discovery.ResultKindTrack,
		Title:    title,
		Subtitle: "Drake",
		Sources:  []discovery.SourceRef{{Provider: discovery.ProviderSpotify, ExternalID: "sp-" + title}},
		Extras:   map[string]any{},
	}
	var feats []map[string]any
	for _, name := range featured {
		feats = append(feats, discovery.FeaturedArtist{Name: name, Role: "featured"}.ToExtrasMap())
	}
	if len(feats) > 0 {
		r.Extras["featured_artists"] = feats
	}
	return r
}

func herLossService(spotify *discoverytest.AlbumContentProvider, lookups *atomic.Int32) *discovery.GetAlbumTracksService {
	deezer := &discoverytest.AlbumContentProvider{Results: []discovery.SearchResult{deezerTrackFeat("1", "Rich Flex"), deezerTrackFeat("2", "Solo")}}
	return discovery.NewGetAlbumTracksService(
		map[discovery.ProviderName]discovery.AlbumContentProvider{discovery.ProviderDeezer: deezer, discovery.ProviderSpotify: spotify},
		discovery.WithTrackFeatured(countingTrackFeatured{
			fakeTrackFeatured: fakeTrackFeatured{byID: map[string][]discovery.FeaturedArtist{"1": {{Name: "21 Savage", DeezerID: 7, Role: "featured"}}}},
			calls:             lookups,
		}),
	)
}

func TestGetAlbumTracks_featuredComeFromSpotifyWhenTheCardKnowsIt(t *testing.T) {
	var lookups atomic.Int32
	spotify := &discoverytest.AlbumContentProvider{Results: []discovery.SearchResult{spotifyTrackFeat("Rich Flex", "21 Savage"), spotifyTrackFeat("Solo")}}
	svc := herLossService(spotify, &lookups)

	resp, err := svc.ExecuteRequest(t.Context(), discovery.AlbumTracksRequest{
		Provider: discovery.ProviderDeezer, ExternalID: "d-her-loss",
		Known: map[discovery.ProviderName]string{discovery.ProviderSpotify: "sp-her-loss"},
	})
	if err != nil {
		t.Fatal(err)
	}

	if feats := featuredOf(resp.Items[0]); len(feats) != 1 || feats[0].Name != "21 Savage" {
		t.Errorf("Rich Flex featured = %+v, want 21 Savage", feats)
	}
	if resp.Items[0].Sources[0].Provider != discovery.ProviderDeezer {
		t.Errorf("tracks came from %v, want the Deezer seed", resp.Items[0].Sources[0].Provider)
	}
	if n := lookups.Load(); n != 0 {
		t.Errorf("Deezer per-track lookups = %d, want 0", n)
	}
}

func TestGetAlbumTracks_featuredFallBackToDeezerWhenSpotifyFails(t *testing.T) {
	var lookups atomic.Int32
	svc := herLossService(&discoverytest.AlbumContentProvider{Err: errors.New("spotify down")}, &lookups)

	resp, err := svc.ExecuteRequest(t.Context(), discovery.AlbumTracksRequest{
		Provider: discovery.ProviderDeezer, ExternalID: "d-her-loss",
		Known: map[discovery.ProviderName]string{discovery.ProviderSpotify: "sp-her-loss"},
	})
	if err != nil {
		t.Fatal(err)
	}

	if feats := featuredOf(resp.Items[0]); len(feats) != 1 || feats[0].DeezerID != 7 {
		t.Errorf("Rich Flex featured = %+v, want 21 Savage from Deezer", feats)
	}
	if n := lookups.Load(); n != 2 {
		t.Errorf("Deezer per-track lookups = %d, want 2", n)
	}
}

func TestGetAlbumTracks_deezerLooksUpOnlyTheTracksSpotifyCouldNotMatch(t *testing.T) {
	var lookups atomic.Int32
	deezer := &discoverytest.AlbumContentProvider{Results: []discovery.SearchResult{
		deezerTrackFeat("1", "Rich Flex"), deezerTrackFeat("2", "Solo"), deezerTrackFeat("3", "Spin Bout U (Bonus)"),
	}}
	spotify := &discoverytest.AlbumContentProvider{Results: []discovery.SearchResult{spotifyTrackFeat("Rich Flex", "21 Savage"), spotifyTrackFeat("Solo")}}
	svc := discovery.NewGetAlbumTracksService(
		map[discovery.ProviderName]discovery.AlbumContentProvider{discovery.ProviderDeezer: deezer, discovery.ProviderSpotify: spotify},
		discovery.WithTrackFeatured(countingTrackFeatured{
			fakeTrackFeatured: fakeTrackFeatured{byID: map[string][]discovery.FeaturedArtist{"3": {{Name: "21 Savage", DeezerID: 7, Role: "featured"}}}},
			calls:             &lookups,
		}),
	)

	resp, err := svc.ExecuteRequest(t.Context(), discovery.AlbumTracksRequest{
		Provider: discovery.ProviderDeezer, ExternalID: "d-her-loss",
		Known: map[discovery.ProviderName]string{discovery.ProviderSpotify: "sp-her-loss"},
	})
	if err != nil {
		t.Fatal(err)
	}

	if n := lookups.Load(); n != 1 {
		t.Errorf("Deezer per-track lookups = %d, want 1 (only the bonus track Spotify lacks)", n)
	}
	if feats := featuredOf(resp.Items[2]); len(feats) != 1 {
		t.Errorf("bonus track featured = %+v, want 21 Savage from Deezer", feats)
	}
}

func TestGetAlbumTracks_spotifyMatchesTracksAcrossVersionSuffixStyles(t *testing.T) {
	var lookups atomic.Int32
	deezer := &discoverytest.AlbumContentProvider{Results: []discovery.SearchResult{
		deezerTrackFeat("1", "About A Girl (Live)"), deezerTrackFeat("2", "Mama (2007 Remaster)"),
	}}
	spotify := &discoverytest.AlbumContentProvider{Results: []discovery.SearchResult{spotifyTrackFeat("About A Girl - Live"), spotifyTrackFeat("Mama - 2007 Remaster")}}
	svc := discovery.NewGetAlbumTracksService(
		map[discovery.ProviderName]discovery.AlbumContentProvider{discovery.ProviderDeezer: deezer, discovery.ProviderSpotify: spotify},
		discovery.WithTrackFeatured(countingTrackFeatured{calls: &lookups}),
	)

	if _, err := svc.ExecuteRequest(t.Context(), discovery.AlbumTracksRequest{
		Provider: discovery.ProviderDeezer, ExternalID: "d1",
		Known: map[discovery.ProviderName]string{discovery.ProviderSpotify: "sp1"},
	}); err != nil {
		t.Fatal(err)
	}

	if n := lookups.Load(); n != 0 {
		t.Errorf("Deezer per-track lookups = %d, want 0 (Spotify matched both tracks)", n)
	}
}

func TestGetAlbumTracks_exactTitleWinsOverTheSuffixlessMatch(t *testing.T) {
	deezer := &discoverytest.AlbumContentProvider{Results: []discovery.SearchResult{
		deezerTrackFeat("1", "Lithium"), deezerTrackFeat("2", "Lithium - Live"),
	}}
	spotify := &discoverytest.AlbumContentProvider{Results: []discovery.SearchResult{
		spotifyTrackFeat("Lithium"), spotifyTrackFeat("Lithium - Live", "Pat Smear"),
	}}
	svc := discovery.NewGetAlbumTracksService(map[discovery.ProviderName]discovery.AlbumContentProvider{
		discovery.ProviderDeezer: deezer, discovery.ProviderSpotify: spotify,
	})

	resp, err := svc.ExecuteRequest(t.Context(), discovery.AlbumTracksRequest{
		Provider: discovery.ProviderDeezer, ExternalID: "d1",
		Known: map[discovery.ProviderName]string{discovery.ProviderSpotify: "sp1"},
	})
	if err != nil {
		t.Fatal(err)
	}

	if feats := featuredOf(resp.Items[0]); len(feats) != 0 {
		t.Errorf("studio Lithium featured = %+v, want none", feats)
	}
	if feats := featuredOf(resp.Items[1]); len(feats) != 1 {
		t.Errorf("live Lithium featured = %+v, want Pat Smear", feats)
	}
}
