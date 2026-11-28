package service_test

import (
	"altune/go-api/internal/search"
	"altune/go-api/internal/search/internal/adapters/memory"
	"altune/go-api/internal/search/internal/domain"
	"altune/go-api/internal/search/internal/ports"
	"altune/go-api/internal/search/internal/service"
	"altune/go-api/internal/search/searchtest"
	"altune/go-api/internal/shared/clock"
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
	results []search.SearchResult
}

func (f *fakeAlbumSearcher) Name() search.ProviderName { return search.ProviderDeezer }
func (f *fakeAlbumSearcher) SupportedKinds() map[search.ResultKind]bool {
	return map[search.ResultKind]bool{search.ResultKindAlbum: true}
}

func (f *fakeAlbumSearcher) Search(_ context.Context, _ string, _ map[search.ResultKind]bool) ([]search.SearchResult, error) {
	return f.results, nil
}

func TestGetAlbumTracks_fallbackSkipsWrongArtist(t *testing.T) {
	var fetchedID string
	deezer := &memory.AlbumContentProvider{
		GetAlbumTracksFn: func(_ context.Context, _ search.ProviderName, id string) ([]search.SearchResult, error) {
			fetchedID = id
			return []search.SearchResult{memory.NewResult(
				search.ResultKindTrack,
				"Like Lil Mexico",
				memory.WithSources([]search.ProviderLink{{Provider: search.ProviderDeezer, ExternalID: "cht"}}),
			)}, nil
		},
	}
	searcher := &fakeAlbumSearcher{results: []search.SearchResult{
		memory.AlbumSearchResult("Chase Fetti", "999"),
		memory.AlbumSearchResult("Che", "111"),
	}}
	svc := searchtest.NewAlbumTracks(t, map[search.ProviderName]ports.AlbumContentProvider{search.ProviderDeezer: deezer}, searchtest.AlbumFallbackSearcher(searcher))

	resp, err := svc.ExecuteRequest(context.Background(), service.AlbumTracksRequest{Provider: search.ProviderSoundCloud, ExternalID: "sc-1", Title: "Empty Clip", Artist: "Che"})
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
	deezer := &memory.AlbumContentProvider{
		GetAlbumTracksFn: func(_ context.Context, _ search.ProviderName, _ string) ([]search.SearchResult, error) {
			t.Fatal("must not fetch a wrong-artist album")
			return nil, nil
		},
	}
	searcher := &fakeAlbumSearcher{results: []search.SearchResult{memory.AlbumSearchResult("Chase Fetti", "999")}}
	svc := searchtest.NewAlbumTracks(t, map[search.ProviderName]ports.AlbumContentProvider{search.ProviderDeezer: deezer}, searchtest.AlbumFallbackSearcher(searcher))

	resp, err := svc.ExecuteRequest(context.Background(), service.AlbumTracksRequest{Provider: search.ProviderSoundCloud, ExternalID: "sc-1", Title: "Empty Clip", Artist: "Che"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(resp.Items) != 0 {
		t.Fatalf("items = %d, want 0 (no artist match → empty, not wrong)", len(resp.Items))
	}
}

func TestGetAlbumTracksService_ExecuteRequest(t *testing.T) {
	sampleTracks := []search.SearchResult{
		memory.NewResult(
			search.ResultKindTrack,
			"Track 1",
			memory.WithSources([]search.ProviderLink{{Provider: search.ProviderDeezer, ExternalID: "t1"}}),
		),
		memory.NewResult(
			search.ResultKindTrack,
			"Track 2",
			memory.WithSources([]search.ProviderLink{{Provider: search.ProviderDeezer, ExternalID: "t2"}}),
		),
		memory.NewResult(
			search.ResultKindTrack,
			"Track 3",
			memory.WithSources([]search.ProviderLink{{Provider: search.ProviderDeezer, ExternalID: "t3"}}),
		),
	}

	tests := []struct {
		name          string
		providerName  search.ProviderName
		externalID    string
		limit         int
		providers     map[search.ProviderName]ports.AlbumContentProvider
		wantStatus    domain.ProviderStatus
		wantItemCount int
	}{
		{
			name:         "valid provider returns tracks",
			providerName: search.ProviderDeezer,
			externalID:   "album-123",
			limit:        0,
			providers: map[search.ProviderName]ports.AlbumContentProvider{
				search.ProviderDeezer: &memory.AlbumContentProvider{
					GetAlbumTracksFn: func(_ context.Context, pn search.ProviderName, extID string) ([]search.SearchResult, error) {
						if pn != search.ProviderDeezer {
							t.Errorf("expected provider deezer, got %s", pn.String())
						}
						if extID != "album-123" {
							t.Errorf("expected externalID album-123, got %s", extID)
						}
						return sampleTracks, nil
					},
				},
			},
			wantStatus:    domain.ProviderStatusOK,
			wantItemCount: 3,
		},
		{
			name:          "unknown provider returns error status",
			providerName:  search.ProviderSoundCloud,
			externalID:    "album-123",
			limit:         0,
			providers:     map[search.ProviderName]ports.AlbumContentProvider{},
			wantStatus:    domain.ProviderStatusError,
			wantItemCount: 0,
		},
		{
			name:         "provider error returns error status without propagating",
			providerName: search.ProviderDeezer,
			externalID:   "album-err",
			limit:        0,
			providers: map[search.ProviderName]ports.AlbumContentProvider{
				search.ProviderDeezer: &memory.AlbumContentProvider{
					GetAlbumTracksFn: func(_ context.Context, _ search.ProviderName, _ string) ([]search.SearchResult, error) {
						return nil, errors.New("upstream timeout")
					},
				},
			},
			wantStatus:    domain.ProviderStatusError,
			wantItemCount: 0,
		},
		{
			name:         "limit truncates results",
			providerName: search.ProviderDeezer,
			externalID:   "album-123",
			limit:        2,
			providers: map[search.ProviderName]ports.AlbumContentProvider{
				search.ProviderDeezer: &memory.AlbumContentProvider{
					GetAlbumTracksFn: func(_ context.Context, _ search.ProviderName, _ string) ([]search.SearchResult, error) {
						return sampleTracks, nil
					},
				},
			},
			wantStatus:    domain.ProviderStatusOK,
			wantItemCount: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := searchtest.NewAlbumTracks(t, tt.providers)

			resp, err := svc.ExecuteRequest(context.Background(), service.AlbumTracksRequest{Provider: tt.providerName, ExternalID: tt.externalID, Limit: tt.limit})
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

func albumTracksFallbackRequest() service.AlbumTracksRequest {
	return service.AlbumTracksRequest{Provider: search.ProviderSoundCloud, ExternalID: "sc-1", Title: "Empty Clip", Artist: "Che"}
}

func albumTracksWithFailingPrimary(t testing.TB, primaryErr error, deezer ports.AlbumContentProvider, searcher search.SearchProvider) *service.GetAlbumTracksService {
	return searchtest.NewAlbumTracks(t, map[search.ProviderName]ports.AlbumContentProvider{
		search.ProviderSoundCloud: &memory.AlbumContentProvider{
			GetAlbumTracksFn: func(context.Context, search.ProviderName, string) ([]search.SearchResult, error) {
				return nil, primaryErr
			},
		},
		search.ProviderDeezer: deezer,
	}, searchtest.AlbumFallbackSearcher(searcher))
}

func unfetchableAlbumProvider(t *testing.T) *memory.AlbumContentProvider {
	t.Helper()
	return &memory.AlbumContentProvider{
		GetAlbumTracksFn: func(context.Context, search.ProviderName, string) ([]search.SearchResult, error) {
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
		want       domain.ProviderStatus
	}{
		{name: "upstream 503", primaryErr: memory.UpstreamStatusError(503), want: domain.ProviderStatusError},
		{name: "upstream 429", primaryErr: memory.UpstreamStatusError(429), want: domain.ProviderStatusRateLimited},
		{name: "upstream timeout", primaryErr: fmt.Errorf("fetch: %w", context.DeadlineExceeded), want: domain.ProviderStatusTimeout},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svc := albumTracksWithFailingPrimary(t, tc.primaryErr, unfetchableAlbumProvider(t), erroringAlbumSearcher{})

			resp, err := svc.ExecuteRequest(context.Background(), albumTracksFallbackRequest())
			if err != nil {
				t.Fatalf("a failed fallback must degrade, not propagate: %v", err)
			}
			if resp.Status != tc.want {
				t.Errorf("status = %v, want %v (the primary's failure, not a healthy empty album)", resp.Status, tc.want)
			}
			if resp.ProviderName != search.ProviderSoundCloud {
				t.Errorf("provider = %v, want soundcloud (the provider that failed)", resp.ProviderName)
			}
		})
	}
}

func TestGetAlbumTracks_everyEmptyFallbackCarriesPrimaryStatus(t *testing.T) {
	emptyTracklist := &memory.AlbumContentProvider{
		GetAlbumTracksFn: func(context.Context, search.ProviderName, string) ([]search.SearchResult, error) {
			return nil, nil
		},
	}
	cases := []struct {
		name     string
		searcher search.SearchProvider
		deezer   ports.AlbumContentProvider
	}{
		{name: "search errored", searcher: erroringAlbumSearcher{}, deezer: unfetchableAlbumProvider(t)},
		{name: "search matched nothing", searcher: &fakeAlbumSearcher{}, deezer: unfetchableAlbumProvider(t)},
		{name: "search matched another artist", searcher: &fakeAlbumSearcher{results: []search.SearchResult{memory.AlbumSearchResult("Chase Fetti", "999")}}, deezer: unfetchableAlbumProvider(t)},
		{name: "candidate has no tracklist", searcher: &fakeAlbumSearcher{results: []search.SearchResult{memory.AlbumSearchResult("Che", "111")}}, deezer: emptyTracklist},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svc := albumTracksWithFailingPrimary(t, memory.UpstreamStatusError(503), tc.deezer, tc.searcher)

			resp, err := svc.ExecuteRequest(context.Background(), albumTracksFallbackRequest())
			if err != nil {
				t.Fatalf("a failed fallback must degrade, not propagate: %v", err)
			}
			if resp.Status != domain.ProviderStatusError || len(resp.Items) != 0 {
				t.Errorf("resp = %v/%d items, want the primary's error status and no items", resp.Status, len(resp.Items))
			}
		})
	}
}

func TestGetAlbumTracks_servedFallbackNamesTheProviderItStoodInFor(t *testing.T) {
	logs := memory.CaptureProductionLogs(t)
	deezer := &memory.AlbumContentProvider{
		GetAlbumTracksFn: func(context.Context, search.ProviderName, string) ([]search.SearchResult, error) {
			return []search.SearchResult{memory.NewResult(
				search.ResultKindTrack,
				"Like Lil Mexico",
				memory.WithSources([]search.ProviderLink{{Provider: search.ProviderDeezer, ExternalID: "cht"}}),
			)}, nil
		},
	}
	searcher := &fakeAlbumSearcher{results: []search.SearchResult{memory.AlbumSearchResult("Che", "111")}}
	svc := albumTracksWithFailingPrimary(t, memory.UpstreamStatusError(503), deezer, searcher)

	resp, err := svc.ExecuteRequest(context.Background(), albumTracksFallbackRequest())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Status != domain.ProviderStatusOK || len(resp.Items) != 1 {
		t.Fatalf("resp = %v/%d items, want OK and the fallback tracklist", resp.Status, len(resp.Items))
	}
	if resp.ProviderName != search.ProviderDeezer || resp.FallbackFrom != search.ProviderSoundCloud {
		t.Errorf("provider = %v, fallback_from = %v, want deezer standing in for soundcloud",
			resp.ProviderName, resp.FallbackFrom)
	}
	record := fallbackServedRecord(t, logs)
	if record["requested_provider"] != search.ProviderSoundCloud.String() {
		t.Errorf("logged requested_provider = %v, want soundcloud", record["requested_provider"])
	}
}

func TestGetAlbumTracks_primaryAnswerIsNotMarkedAsAFallback(t *testing.T) {
	deezer := &memory.AlbumContentProvider{
		GetAlbumTracksFn: func(context.Context, search.ProviderName, string) ([]search.SearchResult, error) {
			return []search.SearchResult{memory.NewResult(
				search.ResultKindTrack,
				"T",
				memory.WithSources([]search.ProviderLink{{Provider: search.ProviderDeezer, ExternalID: "t"}}),
			)}, nil
		},
	}
	svc := albumTracksSvc(t, deezer, &fakeAlbumSearcher{})

	resp, err := svc.ExecuteRequest(context.Background(), service.AlbumTracksRequest{Provider: search.ProviderDeezer, ExternalID: "d-1", Title: "Empty Clip"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.FallbackFrom != search.ProviderUnknown {
		t.Errorf("fallback_from = %v, want unknown (deezer answered for itself)", resp.FallbackFrom)
	}
}

type fakeTrackFeatured struct {
	byID map[string][]search.FeaturedArtist
	err  error
}

func (f fakeTrackFeatured) LookupTrackFeatured(_ context.Context, id string) ([]search.FeaturedArtist, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.byID[id], nil
}

func (fakeTrackFeatured) ResolveID(context.Context, search.ResultKind, string, string) (string, error) {
	return "", nil
}

func deezerTrackFeat(id, title string) search.SearchResult {
	return memory.NewResult(
		search.ResultKindTrack,
		title,
		memory.WithSources([]search.ProviderLink{{Provider: search.ProviderDeezer, ExternalID: id}}),
		memory.WithExtras(map[string]any{}),
	)
}

func featuredOf(track search.SearchResult) []search.FeaturedArtist {
	raw, _ := track.Extras["featured_artists"].([]map[string]any)
	out := make([]search.FeaturedArtist, 0, len(raw))
	for _, m := range raw {
		name, _ := m["name"].(string)
		deezerID, _ := m["deezer_id"].(int64)
		out = append(out, search.FeaturedArtist{Name: name, DeezerID: deezerID})
	}
	return out
}

func deezerAlbum(t testing.TB, tracks []search.SearchResult, opts ...searchtest.AlbumTracksPart) *service.GetAlbumTracksService {
	deezer := &memory.AlbumContentProvider{Results: tracks}
	return searchtest.NewAlbumTracks(t, map[search.ProviderName]ports.AlbumContentProvider{search.ProviderDeezer: deezer}, opts...)
}

func albumTrackItems(t *testing.T, svc *service.GetAlbumTracksService) []search.SearchResult {
	t.Helper()
	resp, err := svc.ExecuteRequest(t.Context(), service.AlbumTracksRequest{Provider: search.ProviderDeezer, ExternalID: "d-album"})
	if err != nil {
		t.Fatalf("ExecuteRequest: %v", err)
	}
	if resp.Status != domain.ProviderStatusOK {
		t.Fatalf("status = %v, want ok", resp.Status)
	}
	return resp.Items
}

func TestGetAlbumTracks_DeezerTracksTakeTheirFeaturedArtists(t *testing.T) {
	t.Run("stamps featured onto deezer tracks", func(t *testing.T) {
		svc := deezerAlbum(t, []search.SearchResult{deezerTrackFeat("1", "Singapore"), deezerTrackFeat("2", "Lose It")},
			searchtest.AlbumFeatured(fakeTrackFeatured{
				byID: map[string][]search.FeaturedArtist{
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
		svc := deezerAlbum(t, []search.SearchResult{deezerTrackFeat("1", "X")}, searchtest.AlbumFeatured(fakeTrackFeatured{err: errors.New("rate limited")}))
		items := albumTrackItems(t, svc)
		if len(items) != 1 {
			t.Fatalf("items = %d, want the track served without features", len(items))
		}
		if _, present := items[0].Extras["featured_artists"]; present {
			t.Errorf("expected no featured on error, got %v", items[0].Extras["featured_artists"])
		}
	})

	t.Run("no lookup configured serves the tracks as they are", func(t *testing.T) {
		items := albumTrackItems(t, deezerAlbum(t, []search.SearchResult{deezerTrackFeat("1", "X")}))
		if len(items) != 1 {
			t.Fatalf("items = %d, want the track served", len(items))
		}
		if _, present := items[0].Extras["featured_artists"]; present {
			t.Errorf("no lookup configured, yet featured = %v", items[0].Extras["featured_artists"])
		}
	})
}

type albumTracksArgs struct {
	providers map[search.ProviderName]ports.AlbumContentProvider
	featured  service.FeaturedEnrichment
	fallback  service.AlbumFallback
	breaker   *service.CircuitBreaker
}

func completeAlbumTracksArgs() albumTracksArgs {
	return albumTracksArgs{
		providers: map[search.ProviderName]ports.AlbumContentProvider{},
		featured:  service.NoFeaturedEnrichment{},
		fallback:  service.NoAlbumFallback{},
		breaker:   search.NewCircuitBreaker(clock.Now),
	}
}

func (a albumTracksArgs) build() (*service.GetAlbumTracksService, error) {
	return service.NewGetAlbumTracksService(a.providers, a.featured, a.fallback, a.breaker)
}

func TestNewGetAlbumTracksService_RefusesEveryMissingCollaborator(t *testing.T) {
	if _, err := completeAlbumTracksArgs().build(); err != nil {
		t.Fatalf("complete collaborators refused: %v", err)
	}
	cases := map[string]struct {
		without func(*albumTracksArgs)
		named   string
	}{
		"providers": {func(a *albumTracksArgs) { a.providers = nil }, "providers"},
		"featured":  {func(a *albumTracksArgs) { a.featured = nil }, "featured enrichment"},
		"fallback":  {func(a *albumTracksArgs) { a.fallback = nil }, "album fallback"},
		"breaker":   {func(a *albumTracksArgs) { a.breaker = nil }, "circuit breaker"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			args := completeAlbumTracksArgs()
			tc.without(&args)

			svc, err := args.build()

			if !errors.Is(err, service.ErrArtistPageCollaboratorRequired) || svc != nil {
				t.Fatalf("svc = %v, err = %v, want artistpage.ErrCollaboratorRequired and no service", svc, err)
			}
			if !strings.Contains(err.Error(), tc.named) {
				t.Errorf("err = %q, want it to name %q", err, tc.named)
			}
		})
	}
}

func TestNewAlbumCollaborators_RefuseTheirOwnMissingParts(t *testing.T) {
	if _, err := service.NewDeezerFeaturedEnrichment(nil, search.NewCircuitBreaker(clock.Now)); !errors.Is(err, service.ErrArtistPageCollaboratorRequired) || !strings.Contains(err.Error(), "featured lookup") {
		t.Errorf("enrichment without a lookup: err = %v, want artistpage.ErrCollaboratorRequired naming the featured lookup", err)
	}
	if _, err := service.NewDeezerFeaturedEnrichment(fakeTrackFeatured{}, nil); !errors.Is(err, service.ErrArtistPageCollaboratorRequired) || !strings.Contains(err.Error(), "circuit breaker") {
		t.Errorf("enrichment without a breaker: err = %v, want artistpage.ErrCollaboratorRequired naming the circuit breaker", err)
	}
	if _, err := service.NewSearchAlbumFallback(nil, search.NewCircuitBreaker(clock.Now)); !errors.Is(err, service.ErrArtistPageCollaboratorRequired) || !strings.Contains(err.Error(), "search provider") {
		t.Errorf("fallback without a searcher: err = %v, want artistpage.ErrCollaboratorRequired naming the search provider", err)
	}
	if _, err := service.NewSearchAlbumFallback(&fakeAlbumSearcher{}, nil); !errors.Is(err, service.ErrArtistPageCollaboratorRequired) || !strings.Contains(err.Error(), "circuit breaker") {
		t.Errorf("fallback without a breaker: err = %v, want artistpage.ErrCollaboratorRequired naming the circuit breaker", err)
	}
}

func TestNoFeaturedEnrichment_ServesTheTracksAsTheyAreAndLeavesTheBreakerUntouched(t *testing.T) {
	breaker := search.NewCircuitBreaker(clock.Now)
	before := breaker.Snapshot(search.ProviderDeezer)
	deezer := &memory.AlbumContentProvider{Results: []search.SearchResult{deezerTrackFeat("1", "Singapore"), deezerTrackFeat("2", "Lose It")}}
	svc, err := service.NewGetAlbumTracksService(
		map[search.ProviderName]ports.AlbumContentProvider{search.ProviderDeezer: deezer},
		service.NoFeaturedEnrichment{}, service.NoAlbumFallback{}, breaker)
	if err != nil {
		t.Fatalf("NewGetAlbumTracksService: %v", err)
	}

	items := albumTrackItems(t, svc)

	if len(items) != 2 {
		t.Fatalf("items = %d, want both tracks served", len(items))
	}
	for i, item := range items {
		if _, present := item.Extras["featured_artists"]; present {
			t.Errorf("track %d featured = %v, want none", i, item.Extras["featured_artists"])
		}
	}
	if after := breaker.Snapshot(search.ProviderDeezer); after != before {
		t.Errorf("deezer circuit = %+v, want %+v: no lookup may be recorded", after, before)
	}
}

func TestNoAlbumFallback_ReturnsTheDegradedPrimaryAnswer(t *testing.T) {
	deezer := &memory.AlbumContentProvider{
		GetAlbumTracksFn: func(context.Context, search.ProviderName, string) ([]search.SearchResult, error) {
			t.Error("the Deezer album may not be fetched when no fallback is configured")
			return nil, nil
		},
	}
	svc, err := service.NewGetAlbumTracksService(
		map[search.ProviderName]ports.AlbumContentProvider{
			search.ProviderSoundCloud: &memory.AlbumContentProvider{
				GetAlbumTracksFn: func(context.Context, search.ProviderName, string) ([]search.SearchResult, error) {
					return nil, memory.ErrUpstreamDown
				},
			},
			search.ProviderDeezer: deezer,
		},
		service.NoFeaturedEnrichment{}, service.NoAlbumFallback{}, search.NewCircuitBreaker(clock.Now))
	if err != nil {
		t.Fatalf("NewGetAlbumTracksService: %v", err)
	}

	resp, err := svc.ExecuteRequest(context.Background(), albumTracksFallbackRequest())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if resp.Status == domain.ProviderStatusOK || len(resp.Items) != 0 || resp.ProviderName != search.ProviderSoundCloud {
		t.Errorf("status = %v, items = %d, provider = %v, want the degraded soundcloud answer with no tracks", resp.Status, len(resp.Items), resp.ProviderName)
	}
}

type erroringAlbumSearcher struct{}

func (erroringAlbumSearcher) Name() search.ProviderName { return search.ProviderDeezer }
func (erroringAlbumSearcher) SupportedKinds() map[search.ResultKind]bool {
	return map[search.ResultKind]bool{search.ResultKindAlbum: true}
}

func (erroringAlbumSearcher) Search(context.Context, string, map[search.ResultKind]bool) ([]search.SearchResult, error) {
	return nil, errors.New("search down")
}

func albumTracksSvc(t testing.TB, deezer ports.AlbumContentProvider, searcher search.SearchProvider) *service.GetAlbumTracksService {
	return searchtest.NewAlbumTracks(t, map[search.ProviderName]ports.AlbumContentProvider{search.ProviderDeezer: deezer}, searchtest.AlbumFallbackSearcher(searcher))
}

func TestGetAlbumTracks_fallbackArtistGuardFoldsDiacritics(t *testing.T) {
	var fetchedID string
	deezer := &memory.AlbumContentProvider{
		GetAlbumTracksFn: func(_ context.Context, _ search.ProviderName, id string) ([]search.SearchResult, error) {
			fetchedID = id
			return []search.SearchResult{memory.NewResult(
				search.ResultKindTrack,
				"T",
				memory.WithSources([]search.ProviderLink{{Provider: search.ProviderDeezer, ExternalID: "t"}}),
			)}, nil
		},
	}
	searcher := &fakeAlbumSearcher{results: []search.SearchResult{memory.AlbumSearchResult("Ché", "42")}}
	svc := albumTracksSvc(t, deezer, searcher)

	resp, err := svc.ExecuteRequest(context.Background(), service.AlbumTracksRequest{Provider: search.ProviderSoundCloud, ExternalID: "sc-1", Title: "Empty Clip", Artist: "Che"})
	if err != nil {
		t.Fatal(err)
	}
	if fetchedID != "42" || len(resp.Items) != 1 {
		t.Fatalf("fetched %q items %d, want the accented-subtitle album accepted", fetchedID, len(resp.Items))
	}
}

func TestGetAlbumTracks_fallbackArtistGuardRejectsFeatTaggedSubtitle(t *testing.T) {
	deezer := &memory.AlbumContentProvider{
		GetAlbumTracksFn: func(context.Context, search.ProviderName, string) ([]search.SearchResult, error) {
			t.Error("guard unexpectedly accepted the feat-tagged subtitle (known false negative fixed?)")
			return nil, nil
		},
	}
	searcher := &fakeAlbumSearcher{results: []search.SearchResult{memory.AlbumSearchResult("Che feat. Lil X", "42")}}
	svc := albumTracksSvc(t, deezer, searcher)

	resp, err := svc.ExecuteRequest(context.Background(), service.AlbumTracksRequest{Provider: search.ProviderSoundCloud, ExternalID: "sc-1", Title: "Empty Clip", Artist: "Che"})
	if err != nil {
		t.Fatal(err)
	}
	if len(resp.Items) != 0 {
		t.Fatalf("items = %d, want 0 (feat-tagged subtitle is skipped by the exact-match guard)", len(resp.Items))
	}
}

func TestGetAlbumTracks_fallbackNoArtistTakesFirstCandidate(t *testing.T) {
	var fetchedID string
	deezer := &memory.AlbumContentProvider{
		GetAlbumTracksFn: func(_ context.Context, _ search.ProviderName, id string) ([]search.SearchResult, error) {
			fetchedID = id
			return []search.SearchResult{memory.NewResult(
				search.ResultKindTrack,
				"T",
				memory.WithSources([]search.ProviderLink{{Provider: search.ProviderDeezer, ExternalID: "t"}}),
			)}, nil
		},
	}
	searcher := &fakeAlbumSearcher{results: []search.SearchResult{
		memory.NewResult(search.ResultKindAlbum, "Empty Clip", memory.WithSubtitle("Whoever")),
		memory.AlbumSearchResult("Chase Fetti", "999"),
	}}
	svc := albumTracksSvc(t, deezer, searcher)

	resp, err := svc.ExecuteRequest(context.Background(), service.AlbumTracksRequest{Provider: search.ProviderSoundCloud, ExternalID: "sc-1", Title: "Empty Clip"})
	if err != nil {
		t.Fatal(err)
	}
	if fetchedID != "999" || len(resp.Items) != 1 {
		t.Fatalf("fetched %q items %d, want the first SOURCED candidate fetched", fetchedID, len(resp.Items))
	}
}

func TestGetAlbumTracks_fallbackSearcherErrorKeepsTheRequestedProvidersFailure(t *testing.T) {
	deezer := &memory.AlbumContentProvider{
		GetAlbumTracksFn: func(context.Context, search.ProviderName, string) ([]search.SearchResult, error) {
			t.Error("no album may be fetched when the fallback search errored")
			return nil, nil
		},
	}
	svc := albumTracksSvc(t, deezer, erroringAlbumSearcher{})

	resp, err := svc.ExecuteRequest(context.Background(), service.AlbumTracksRequest{Provider: search.ProviderSoundCloud, ExternalID: "sc-1", Title: "Empty Clip", Artist: "Che"})
	if err != nil {
		t.Fatalf("fallback search failure must degrade, not propagate: %v", err)
	}
	if len(resp.Items) != 0 {
		t.Fatalf("items = %d, want 0", len(resp.Items))
	}
	if resp.ProviderName != search.ProviderSoundCloud || !resp.Unserved {
		t.Errorf("resp = %v/unserved %v, want the requested provider's own failure kept",
			resp.ProviderName, resp.Unserved)
	}
}

func TestGetAlbumTracks_fallbackSkipsCandidateWithNoTracks(t *testing.T) {
	fetched := []string{}
	deezer := &memory.AlbumContentProvider{
		GetAlbumTracksFn: func(_ context.Context, _ search.ProviderName, id string) ([]search.SearchResult, error) {
			fetched = append(fetched, id)
			if id == "111" {
				return nil, nil
			}
			return []search.SearchResult{memory.NewResult(
				search.ResultKindTrack,
				"T",
				memory.WithSources([]search.ProviderLink{{Provider: search.ProviderDeezer, ExternalID: "t"}}),
			)}, nil
		},
	}
	searcher := &fakeAlbumSearcher{results: []search.SearchResult{
		memory.AlbumSearchResult("Che", "111"),
		memory.AlbumSearchResult("Che", "222"),
	}}
	svc := albumTracksSvc(t, deezer, searcher)

	resp, err := svc.ExecuteRequest(context.Background(), service.AlbumTracksRequest{Provider: search.ProviderSoundCloud, ExternalID: "sc-1", Title: "Empty Clip", Artist: "Che"})
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
	deezer := &memory.AlbumContentProvider{
		GetAlbumTracksFn: func(context.Context, search.ProviderName, string) ([]search.SearchResult, error) {
			return nil, nil
		},
	}
	svc := albumTracksSvc(t, deezer, &fakeAlbumSearcher{})

	resp, err := svc.ExecuteRequest(context.Background(), service.AlbumTracksRequest{Provider: search.ProviderDeezer, ExternalID: "d-1"})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Status != domain.ProviderStatusOK || len(resp.Items) != 0 {
		t.Fatalf("resp = %v/%d items, want OK/0", resp.Status, len(resp.Items))
	}
}

type panickingTrackFeatured struct{ fakeTrackFeatured }

func (panickingTrackFeatured) LookupTrackFeatured(context.Context, string) ([]search.FeaturedArtist, error) {
	panic("featured lookup exploded")
}

func TestGetAlbumTracks_PanickingFeaturedLookupIsContained(t *testing.T) {
	items := albumTrackItems(t, deezerAlbum(t, []search.SearchResult{deezerTrackFeat("1", "Singapore")}, searchtest.AlbumFeatured(panickingTrackFeatured{})))

	if len(items) != 1 {
		t.Fatalf("items = %d, want the track served despite the panicking lookup", len(items))
	}
	if _, present := items[0].Extras["featured_artists"]; present {
		t.Errorf("featured should be unset after a panicking lookup, got %v", items[0].Extras["featured_artists"])
	}
}

func albumCandidate(title, artist, deezerID string) search.SearchResult {
	r := memory.AlbumSearchResult(artist, deezerID)
	r.Title = title
	return r
}

func singleTrackDeezer() *memory.AlbumContentProvider {
	return &memory.AlbumContentProvider{
		GetAlbumTracksFn: func(context.Context, search.ProviderName, string) ([]search.SearchResult, error) {
			return []search.SearchResult{memory.NewResult(
				search.ResultKindTrack,
				"Like Lil Mexico",
				memory.WithSources([]search.ProviderLink{{Provider: search.ProviderDeezer, ExternalID: "cht"}}),
			)}, nil
		},
	}
}

func TestGetAlbumTracks_fallbackRefusesSameArtistDifferentTitle(t *testing.T) {
	for _, artist := range []string{"Che", ""} {
		t.Run("artist="+artist, func(t *testing.T) {
			searcher := &fakeAlbumSearcher{results: []search.SearchResult{albumCandidate("Another Album", "Che", "222")}}
			svc := albumTracksWithFailingPrimary(t, memory.UpstreamStatusError(503), singleTrackDeezer(), searcher)
			req := albumTracksFallbackRequest()
			req.Artist = artist

			resp, err := svc.ExecuteRequest(context.Background(), req)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if resp.Status != domain.ProviderStatusError || len(resp.Items) != 0 {
				t.Errorf("resp = %v/%d items, want the primary's error status and no items", resp.Status, len(resp.Items))
			}
		})
	}
}

func TestGetAlbumTracks_fallbackAcceptsTitleEquivalentUnderNormaliser(t *testing.T) {
	for _, title := range []string{"EMPTY CLIP", "Empty Clip (Deluxe Edition)"} {
		t.Run(title, func(t *testing.T) {
			searcher := &fakeAlbumSearcher{results: []search.SearchResult{albumCandidate(title, "Che", "111")}}
			svc := albumTracksWithFailingPrimary(t, memory.UpstreamStatusError(503), singleTrackDeezer(), searcher)

			resp, err := svc.ExecuteRequest(context.Background(), albumTracksFallbackRequest())
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if resp.Status != domain.ProviderStatusOK || len(resp.Items) != 1 {
				t.Errorf("resp = %v/%d items, want OK and the fallback tracklist", resp.Status, len(resp.Items))
			}
		})
	}
}

type countingTrackFeatured struct {
	fakeTrackFeatured
	calls *atomic.Int32
}

func (c countingTrackFeatured) LookupTrackFeatured(ctx context.Context, id string) ([]search.FeaturedArtist, error) {
	c.calls.Add(1)
	return c.fakeTrackFeatured.LookupTrackFeatured(ctx, id)
}

func spotifyTrackFeat(title string, featured ...string) search.SearchResult {
	r := memory.NewResult(
		search.ResultKindTrack,
		title,
		memory.WithSubtitle("Drake"),
		memory.WithSources([]search.ProviderLink{{Provider: search.ProviderSpotify, ExternalID: "sp-" + title}}),
		memory.WithExtras(map[string]any{}),
	)
	var feats []map[string]any
	for _, name := range featured {
		feats = append(feats, search.FeaturedArtist{Name: name, Role: "featured"}.ToExtrasMap())
	}
	if len(feats) > 0 {
		r.Extras["featured_artists"] = feats
	}
	return r
}

func herLossService(t testing.TB, spotify *memory.AlbumContentProvider, lookups *atomic.Int32) *service.GetAlbumTracksService {
	deezer := &memory.AlbumContentProvider{Results: []search.SearchResult{deezerTrackFeat("1", "Rich Flex"), deezerTrackFeat("2", "Solo")}}
	return searchtest.NewAlbumTracks(t, map[search.ProviderName]ports.AlbumContentProvider{search.ProviderDeezer: deezer, search.ProviderSpotify: spotify}, searchtest.AlbumFeatured(countingTrackFeatured{
		fakeTrackFeatured: fakeTrackFeatured{byID: map[string][]search.FeaturedArtist{"1": {{Name: "21 Savage", DeezerID: 7, Role: "featured"}}}},
		calls:             lookups,
	}))
}

func TestGetAlbumTracks_featuredComeFromSpotifyWhenTheCardKnowsIt(t *testing.T) {
	var lookups atomic.Int32
	spotify := &memory.AlbumContentProvider{Results: []search.SearchResult{spotifyTrackFeat("Rich Flex", "21 Savage"), spotifyTrackFeat("Solo")}}
	svc := herLossService(t, spotify, &lookups)

	resp, err := svc.ExecuteRequest(t.Context(), service.AlbumTracksRequest{
		Provider: search.ProviderDeezer, ExternalID: "d-her-loss",
		Known: map[search.ProviderName]string{search.ProviderSpotify: "sp-her-loss"},
	})
	if err != nil {
		t.Fatal(err)
	}

	if feats := featuredOf(resp.Items[0]); len(feats) != 1 || feats[0].Name != "21 Savage" {
		t.Errorf("Rich Flex featured = %+v, want 21 Savage", feats)
	}
	if resp.Items[0].Sources[0].Provider != search.ProviderDeezer {
		t.Errorf("tracks came from %v, want the Deezer seed", resp.Items[0].Sources[0].Provider)
	}
	if n := lookups.Load(); n != 0 {
		t.Errorf("Deezer per-track lookups = %d, want 0", n)
	}
}

func TestGetAlbumTracks_featuredFallBackToDeezerWhenSpotifyFails(t *testing.T) {
	var lookups atomic.Int32
	svc := herLossService(t, &memory.AlbumContentProvider{Err: errors.New("spotify down")}, &lookups)

	resp, err := svc.ExecuteRequest(t.Context(), service.AlbumTracksRequest{
		Provider: search.ProviderDeezer, ExternalID: "d-her-loss",
		Known: map[search.ProviderName]string{search.ProviderSpotify: "sp-her-loss"},
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
	deezer := &memory.AlbumContentProvider{Results: []search.SearchResult{
		deezerTrackFeat("1", "Rich Flex"), deezerTrackFeat("2", "Solo"), deezerTrackFeat("3", "Spin Bout U (Bonus)"),
	}}
	spotify := &memory.AlbumContentProvider{Results: []search.SearchResult{spotifyTrackFeat("Rich Flex", "21 Savage"), spotifyTrackFeat("Solo")}}
	svc := searchtest.NewAlbumTracks(t, map[search.ProviderName]ports.AlbumContentProvider{search.ProviderDeezer: deezer, search.ProviderSpotify: spotify}, searchtest.AlbumFeatured(countingTrackFeatured{
		fakeTrackFeatured: fakeTrackFeatured{byID: map[string][]search.FeaturedArtist{"3": {{Name: "21 Savage", DeezerID: 7, Role: "featured"}}}},
		calls:             &lookups,
	}))

	resp, err := svc.ExecuteRequest(t.Context(), service.AlbumTracksRequest{
		Provider: search.ProviderDeezer, ExternalID: "d-her-loss",
		Known: map[search.ProviderName]string{search.ProviderSpotify: "sp-her-loss"},
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
	deezer := &memory.AlbumContentProvider{Results: []search.SearchResult{
		deezerTrackFeat("1", "About A Girl (Live)"), deezerTrackFeat("2", "Mama (2007 Remaster)"),
	}}
	spotify := &memory.AlbumContentProvider{Results: []search.SearchResult{spotifyTrackFeat("About A Girl - Live"), spotifyTrackFeat("Mama - 2007 Remaster")}}
	svc := searchtest.NewAlbumTracks(t, map[search.ProviderName]ports.AlbumContentProvider{search.ProviderDeezer: deezer, search.ProviderSpotify: spotify}, searchtest.AlbumFeatured(countingTrackFeatured{calls: &lookups}))

	if _, err := svc.ExecuteRequest(t.Context(), service.AlbumTracksRequest{
		Provider: search.ProviderDeezer, ExternalID: "d1",
		Known: map[search.ProviderName]string{search.ProviderSpotify: "sp1"},
	}); err != nil {
		t.Fatal(err)
	}

	if n := lookups.Load(); n != 0 {
		t.Errorf("Deezer per-track lookups = %d, want 0 (Spotify matched both tracks)", n)
	}
}

func TestGetAlbumTracks_exactTitleWinsOverTheSuffixlessMatch(t *testing.T) {
	deezer := &memory.AlbumContentProvider{Results: []search.SearchResult{
		deezerTrackFeat("1", "Lithium"), deezerTrackFeat("2", "Lithium - Live"),
	}}
	spotify := &memory.AlbumContentProvider{Results: []search.SearchResult{
		spotifyTrackFeat("Lithium"), spotifyTrackFeat("Lithium - Live", "Pat Smear"),
	}}
	svc := searchtest.NewAlbumTracks(t, map[search.ProviderName]ports.AlbumContentProvider{
		search.ProviderDeezer: deezer, search.ProviderSpotify: spotify,
	})

	resp, err := svc.ExecuteRequest(t.Context(), service.AlbumTracksRequest{
		Provider: search.ProviderDeezer, ExternalID: "d1",
		Known: map[search.ProviderName]string{search.ProviderSpotify: "sp1"},
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
