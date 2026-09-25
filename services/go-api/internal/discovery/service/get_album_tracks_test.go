package service

import (
	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/discovery/ports"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
)

type fakeAlbumSearcher struct {
	results []domain.SearchResult
}

func (f *fakeAlbumSearcher) Name() domain.ProviderName { return domain.ProviderDeezer }
func (f *fakeAlbumSearcher) SupportedKinds() map[domain.ResultKind]bool {
	return map[domain.ResultKind]bool{domain.ResultKindAlbum: true}
}

func (f *fakeAlbumSearcher) Search(_ context.Context, _ string, _ map[domain.ResultKind]bool) ([]domain.SearchResult, error) {
	return f.results, nil
}

func albumSearchResult(artist, deezerID string) domain.SearchResult {
	return domain.SearchResult{
		Kind: domain.ResultKindAlbum, Title: "Empty Clip", Subtitle: artist,
		Sources: []domain.SourceRef{{Provider: domain.ProviderDeezer, ExternalID: deezerID}},
	}
}

func TestGetAlbumTracks_fallbackSkipsWrongArtist(t *testing.T) {
	var fetchedID string
	deezer := &fakeAlbumContentProvider{
		getAlbumTracksFn: func(_ context.Context, _ domain.ProviderName, id string) ([]domain.SearchResult, error) {
			fetchedID = id
			return []domain.SearchResult{{
				Kind: domain.ResultKindTrack, Title: "Like Lil Mexico",
				Sources: []domain.SourceRef{{Provider: domain.ProviderDeezer, ExternalID: "cht"}},
			}}, nil
		},
	}
	searcher := &fakeAlbumSearcher{results: []domain.SearchResult{
		albumSearchResult("Chase Fetti", "999"),
		albumSearchResult("Che", "111"),
	}}
	svc := NewGetAlbumTracksService(
		map[domain.ProviderName]ports.AlbumContentProvider{domain.ProviderDeezer: deezer},
		WithAlbumFallbackSearcher(searcher),
	)

	resp, err := svc.ExecuteRequest(context.Background(), AlbumTracksRequest{Provider: domain.ProviderSoundCloud, ExternalID: "sc-1", Title: "Empty Clip", Artist: "Che"})
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
	deezer := &fakeAlbumContentProvider{
		getAlbumTracksFn: func(_ context.Context, _ domain.ProviderName, _ string) ([]domain.SearchResult, error) {
			t.Fatal("must not fetch a wrong-artist album")
			return nil, nil
		},
	}
	searcher := &fakeAlbumSearcher{results: []domain.SearchResult{albumSearchResult("Chase Fetti", "999")}}
	svc := NewGetAlbumTracksService(
		map[domain.ProviderName]ports.AlbumContentProvider{domain.ProviderDeezer: deezer},
		WithAlbumFallbackSearcher(searcher),
	)

	resp, err := svc.ExecuteRequest(context.Background(), AlbumTracksRequest{Provider: domain.ProviderSoundCloud, ExternalID: "sc-1", Title: "Empty Clip", Artist: "Che"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(resp.Items) != 0 {
		t.Fatalf("items = %d, want 0 (no artist match → empty, not wrong)", len(resp.Items))
	}
}

func TestGetAlbumTracksService_ExecuteRequest(t *testing.T) {
	sampleTracks := []domain.SearchResult{
		{Kind: domain.ResultKindTrack, Title: "Track 1", Sources: []domain.SourceRef{{Provider: domain.ProviderDeezer, ExternalID: "t1"}}},
		{Kind: domain.ResultKindTrack, Title: "Track 2", Sources: []domain.SourceRef{{Provider: domain.ProviderDeezer, ExternalID: "t2"}}},
		{Kind: domain.ResultKindTrack, Title: "Track 3", Sources: []domain.SourceRef{{Provider: domain.ProviderDeezer, ExternalID: "t3"}}},
	}

	tests := []struct {
		name          string
		providerName  domain.ProviderName
		externalID    string
		limit         int
		providers     map[domain.ProviderName]ports.AlbumContentProvider
		wantStatus    domain.ProviderStatus
		wantItemCount int
	}{
		{
			name:         "valid provider returns tracks",
			providerName: domain.ProviderDeezer,
			externalID:   "album-123",
			limit:        0,
			providers: map[domain.ProviderName]ports.AlbumContentProvider{
				domain.ProviderDeezer: &fakeAlbumContentProvider{
					getAlbumTracksFn: func(_ context.Context, pn domain.ProviderName, extID string) ([]domain.SearchResult, error) {
						if pn != domain.ProviderDeezer {
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
			providerName:  domain.ProviderSoundCloud,
			externalID:    "album-123",
			limit:         0,
			providers:     map[domain.ProviderName]ports.AlbumContentProvider{},
			wantStatus:    domain.ProviderStatusError,
			wantItemCount: 0,
		},
		{
			name:         "provider error returns error status without propagating",
			providerName: domain.ProviderDeezer,
			externalID:   "album-err",
			limit:        0,
			providers: map[domain.ProviderName]ports.AlbumContentProvider{
				domain.ProviderDeezer: &fakeAlbumContentProvider{
					getAlbumTracksFn: func(_ context.Context, _ domain.ProviderName, _ string) ([]domain.SearchResult, error) {
						return nil, errors.New("upstream timeout")
					},
				},
			},
			wantStatus:    domain.ProviderStatusError,
			wantItemCount: 0,
		},
		{
			name:         "limit truncates results",
			providerName: domain.ProviderDeezer,
			externalID:   "album-123",
			limit:        2,
			providers: map[domain.ProviderName]ports.AlbumContentProvider{
				domain.ProviderDeezer: &fakeAlbumContentProvider{
					getAlbumTracksFn: func(_ context.Context, _ domain.ProviderName, _ string) ([]domain.SearchResult, error) {
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
			svc := NewGetAlbumTracksService(tt.providers)

			resp, err := svc.ExecuteRequest(context.Background(), AlbumTracksRequest{Provider: tt.providerName, ExternalID: tt.externalID, Limit: tt.limit})
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

// albumTracksFallbackRequest asks soundcloud for an album it can name, the
// shape that lets the deezer search fallback run.
func albumTracksFallbackRequest() AlbumTracksRequest {
	return AlbumTracksRequest{Provider: domain.ProviderSoundCloud, ExternalID: "sc-1", Title: "Empty Clip", Artist: "Che"}
}

// albumTracksWithFailingPrimary wires soundcloud as the requested provider,
// failing with primaryErr, next to the deezer provider the fallback fetches
// its candidate tracklists from.
func albumTracksWithFailingPrimary(primaryErr error, deezer ports.AlbumContentProvider, searcher ports.SearchProvider) *GetAlbumTracksService {
	return NewGetAlbumTracksService(
		map[domain.ProviderName]ports.AlbumContentProvider{
			domain.ProviderSoundCloud: &fakeAlbumContentProvider{
				getAlbumTracksFn: func(context.Context, domain.ProviderName, string) ([]domain.SearchResult, error) {
					return nil, primaryErr
				},
			},
			domain.ProviderDeezer: deezer,
		},
		WithAlbumFallbackSearcher(searcher),
	)
}

func unfetchableAlbumProvider(t *testing.T) *fakeAlbumContentProvider {
	t.Helper()
	return &fakeAlbumContentProvider{
		getAlbumTracksFn: func(context.Context, domain.ProviderName, string) ([]domain.SearchResult, error) {
			t.Error("no album may be fetched once the fallback search found nothing")
			return nil, nil
		},
	}
}

// fallbackServedRecord is the logged record of the fallback answering, or a
// failure when production never saw one.
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

// Issue #2238: the fallback answered ok/empty for a primary that was down, so
// a client saw a healthy empty album and never retried.
func TestGetAlbumTracks_fallbacklessAnswerCarriesPrimaryStatus(t *testing.T) {
	cases := []struct {
		name       string
		primaryErr error
		want       domain.ProviderStatus
	}{
		{name: "upstream 503", primaryErr: statusErr(503), want: domain.ProviderStatusError},
		{name: "upstream 429", primaryErr: statusErr(429), want: domain.ProviderStatusRateLimited},
		{name: "upstream timeout", primaryErr: fmt.Errorf("fetch: %w", context.DeadlineExceeded), want: domain.ProviderStatusTimeout},
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
			if resp.ProviderName != domain.ProviderSoundCloud {
				t.Errorf("provider = %v, want soundcloud (the provider that failed)", resp.ProviderName)
			}
		})
	}
}

// Every way the fallback can come up empty leaves the primary's failure
// standing: none of them is evidence the album is empty.
func TestGetAlbumTracks_everyEmptyFallbackCarriesPrimaryStatus(t *testing.T) {
	emptyTracklist := &fakeAlbumContentProvider{
		getAlbumTracksFn: func(context.Context, domain.ProviderName, string) ([]domain.SearchResult, error) {
			return nil, nil
		},
	}
	cases := []struct {
		name     string
		searcher ports.SearchProvider
		deezer   ports.AlbumContentProvider
	}{
		{name: "search errored", searcher: erroringAlbumSearcher{}, deezer: unfetchableAlbumProvider(t)},
		{name: "search matched nothing", searcher: &fakeAlbumSearcher{}, deezer: unfetchableAlbumProvider(t)},
		{name: "search matched another artist", searcher: &fakeAlbumSearcher{results: []domain.SearchResult{albumSearchResult("Chase Fetti", "999")}}, deezer: unfetchableAlbumProvider(t)},
		{name: "candidate has no tracklist", searcher: &fakeAlbumSearcher{results: []domain.SearchResult{albumSearchResult("Che", "111")}}, deezer: emptyTracklist},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svc := albumTracksWithFailingPrimary(statusErr(503), tc.deezer, tc.searcher)

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

// A served fallback names the provider it stood in for, in the response and in
// production logs, so neither a client nor an operator reads deezer's
// tracklist as the answer soundcloud gave.
func TestGetAlbumTracks_servedFallbackNamesTheProviderItStoodInFor(t *testing.T) {
	logs := captureProductionLogs(t)
	deezer := &fakeAlbumContentProvider{
		getAlbumTracksFn: func(context.Context, domain.ProviderName, string) ([]domain.SearchResult, error) {
			return []domain.SearchResult{{
				Kind: domain.ResultKindTrack, Title: "Like Lil Mexico",
				Sources: []domain.SourceRef{{Provider: domain.ProviderDeezer, ExternalID: "cht"}},
			}}, nil
		},
	}
	searcher := &fakeAlbumSearcher{results: []domain.SearchResult{albumSearchResult("Che", "111")}}
	svc := albumTracksWithFailingPrimary(statusErr(503), deezer, searcher)

	resp, err := svc.ExecuteRequest(context.Background(), albumTracksFallbackRequest())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Status != domain.ProviderStatusOK || len(resp.Items) != 1 {
		t.Fatalf("resp = %v/%d items, want OK and the fallback tracklist", resp.Status, len(resp.Items))
	}
	if resp.ProviderName != domain.ProviderDeezer || resp.FallbackFrom != domain.ProviderSoundCloud {
		t.Errorf("provider = %v, fallback_from = %v, want deezer standing in for soundcloud",
			resp.ProviderName, resp.FallbackFrom)
	}
	record := fallbackServedRecord(t, logs)
	if record["requested_provider"] != domain.ProviderSoundCloud.String() {
		t.Errorf("logged requested_provider = %v, want soundcloud", record["requested_provider"])
	}
}

// A provider that answers for itself is not a fallback, so nothing marks it as
// one.
func TestGetAlbumTracks_primaryAnswerIsNotMarkedAsAFallback(t *testing.T) {
	deezer := &fakeAlbumContentProvider{
		getAlbumTracksFn: func(context.Context, domain.ProviderName, string) ([]domain.SearchResult, error) {
			return []domain.SearchResult{{
				Kind: domain.ResultKindTrack, Title: "T",
				Sources: []domain.SourceRef{{Provider: domain.ProviderDeezer, ExternalID: "t"}},
			}}, nil
		},
	}
	svc := albumTracksSvc(deezer, &fakeAlbumSearcher{})

	resp, err := svc.ExecuteRequest(context.Background(), AlbumTracksRequest{Provider: domain.ProviderDeezer, ExternalID: "d-1", Title: "Empty Clip"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.FallbackFrom != domain.ProviderUnknown {
		t.Errorf("fallback_from = %v, want unknown (deezer answered for itself)", resp.FallbackFrom)
	}
}

type fakeTrackFeatured struct {
	byID map[string][]domain.FeaturedArtist
	err  error
}

func (f fakeTrackFeatured) LookupTrackFeatured(_ context.Context, id string) ([]domain.FeaturedArtist, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.byID[id], nil
}

func (fakeTrackFeatured) ResolveID(context.Context, domain.ResultKind, string, string) (string, error) {
	return "", nil
}

func deezerTrackFeat(id, title string) domain.SearchResult {
	return domain.NewProviderResult(domain.ResultKindTrack, title, "", "",
		domain.SourceRef{Provider: domain.ProviderDeezer, ExternalID: id}, nil)
}

func TestGetAlbumTracks_enrichFeatured(t *testing.T) {
	ctx := context.Background()

	t.Run("stamps featured onto deezer tracks", func(t *testing.T) {
		svc := NewGetAlbumTracksService(nil, WithTrackFeatured(fakeTrackFeatured{
			byID: map[string][]domain.FeaturedArtist{
				"1": {{Name: "Destroy Lonely", DeezerID: 99, Role: domain.RoleFeatured}},
			},
		}))
		results := []domain.SearchResult{deezerTrackFeat("1", "Singapore"), deezerTrackFeat("2", "Lose It")}
		svc.enrichFeatured(ctx, results)

		raw, ok := results[0].Extras["featured_artists"].([]map[string]any)
		if !ok || len(raw) != 1 || raw[0]["name"] != "Destroy Lonely" {
			t.Fatalf("track 0 featured = %v", results[0].Extras["featured_artists"])
		}
		if _, present := results[1].Extras["featured_artists"]; present {
			t.Errorf("track 1 should have no featured, got %v", results[1].Extras["featured_artists"])
		}
	})

	t.Run("lookup error degrades to no features", func(t *testing.T) {
		svc := NewGetAlbumTracksService(nil, WithTrackFeatured(fakeTrackFeatured{err: errors.New("rate limited")}))
		results := []domain.SearchResult{deezerTrackFeat("1", "X")}
		svc.enrichFeatured(ctx, results)
		if _, present := results[0].Extras["featured_artists"]; present {
			t.Errorf("expected no featured on error, got %v", results[0].Extras["featured_artists"])
		}
	})

	t.Run("no lookup configured is a no-op", func(t *testing.T) {
		svc := NewGetAlbumTracksService(nil)
		results := []domain.SearchResult{deezerTrackFeat("1", "X")}
		svc.enrichFeatured(ctx, results)
	})
}

type erroringAlbumSearcher struct{}

func (erroringAlbumSearcher) Name() domain.ProviderName { return domain.ProviderDeezer }
func (erroringAlbumSearcher) SupportedKinds() map[domain.ResultKind]bool {
	return map[domain.ResultKind]bool{domain.ResultKindAlbum: true}
}

func (erroringAlbumSearcher) Search(context.Context, string, map[domain.ResultKind]bool) ([]domain.SearchResult, error) {
	return nil, errors.New("search down")
}

func albumTracksSvc(deezer ports.AlbumContentProvider, searcher ports.SearchProvider) *GetAlbumTracksService {
	return NewGetAlbumTracksService(
		map[domain.ProviderName]ports.AlbumContentProvider{domain.ProviderDeezer: deezer},
		WithAlbumFallbackSearcher(searcher),
	)
}

func TestGetAlbumTracks_fallbackArtistGuardFoldsDiacritics(t *testing.T) {
	var fetchedID string
	deezer := &fakeAlbumContentProvider{
		getAlbumTracksFn: func(_ context.Context, _ domain.ProviderName, id string) ([]domain.SearchResult, error) {
			fetchedID = id
			return []domain.SearchResult{{
				Kind: domain.ResultKindTrack, Title: "T",
				Sources: []domain.SourceRef{{Provider: domain.ProviderDeezer, ExternalID: "t"}},
			}}, nil
		},
	}
	searcher := &fakeAlbumSearcher{results: []domain.SearchResult{albumSearchResult("Ché", "42")}}
	svc := albumTracksSvc(deezer, searcher)

	resp, err := svc.ExecuteRequest(context.Background(), AlbumTracksRequest{Provider: domain.ProviderSoundCloud, ExternalID: "sc-1", Title: "Empty Clip", Artist: "Che"})
	if err != nil {
		t.Fatal(err)
	}
	if fetchedID != "42" || len(resp.Items) != 1 {
		t.Fatalf("fetched %q items %d, want the accented-subtitle album accepted", fetchedID, len(resp.Items))
	}
}

func TestGetAlbumTracks_fallbackArtistGuardRejectsFeatTaggedSubtitle(t *testing.T) {
	deezer := &fakeAlbumContentProvider{
		getAlbumTracksFn: func(context.Context, domain.ProviderName, string) ([]domain.SearchResult, error) {
			t.Error("guard unexpectedly accepted the feat-tagged subtitle (known false negative fixed?)")
			return nil, nil
		},
	}
	searcher := &fakeAlbumSearcher{results: []domain.SearchResult{albumSearchResult("Che feat. Lil X", "42")}}
	svc := albumTracksSvc(deezer, searcher)

	resp, err := svc.ExecuteRequest(context.Background(), AlbumTracksRequest{Provider: domain.ProviderSoundCloud, ExternalID: "sc-1", Title: "Empty Clip", Artist: "Che"})
	if err != nil {
		t.Fatal(err)
	}
	if len(resp.Items) != 0 {
		t.Fatalf("items = %d, want 0 (feat-tagged subtitle is skipped by the exact-match guard)", len(resp.Items))
	}
}

func TestGetAlbumTracks_fallbackNoArtistTakesFirstCandidate(t *testing.T) {
	var fetchedID string
	deezer := &fakeAlbumContentProvider{
		getAlbumTracksFn: func(_ context.Context, _ domain.ProviderName, id string) ([]domain.SearchResult, error) {
			fetchedID = id
			return []domain.SearchResult{{
				Kind: domain.ResultKindTrack, Title: "T",
				Sources: []domain.SourceRef{{Provider: domain.ProviderDeezer, ExternalID: "t"}},
			}}, nil
		},
	}
	searcher := &fakeAlbumSearcher{results: []domain.SearchResult{
		{Kind: domain.ResultKindAlbum, Title: "Empty Clip", Subtitle: "Whoever"},
		albumSearchResult("Chase Fetti", "999"),
	}}
	svc := albumTracksSvc(deezer, searcher)

	resp, err := svc.ExecuteRequest(context.Background(), AlbumTracksRequest{Provider: domain.ProviderSoundCloud, ExternalID: "sc-1", Title: "Empty Clip"})
	if err != nil {
		t.Fatal(err)
	}
	if fetchedID != "999" || len(resp.Items) != 1 {
		t.Fatalf("fetched %q items %d, want the first SOURCED candidate fetched", fetchedID, len(resp.Items))
	}
}

func TestGetAlbumTracks_fallbackSearcherErrorKeepsTheRequestedProvidersFailure(t *testing.T) {
	deezer := &fakeAlbumContentProvider{
		getAlbumTracksFn: func(context.Context, domain.ProviderName, string) ([]domain.SearchResult, error) {
			t.Error("no album may be fetched when the fallback search errored")
			return nil, nil
		},
	}
	svc := albumTracksSvc(deezer, erroringAlbumSearcher{})

	resp, err := svc.ExecuteRequest(context.Background(), AlbumTracksRequest{Provider: domain.ProviderSoundCloud, ExternalID: "sc-1", Title: "Empty Clip", Artist: "Che"})
	if err != nil {
		t.Fatalf("fallback search failure must degrade, not propagate: %v", err)
	}
	if len(resp.Items) != 0 {
		t.Fatalf("items = %d, want 0", len(resp.Items))
	}
	if resp.ProviderName != domain.ProviderSoundCloud || !resp.Unserved {
		t.Errorf("resp = %v/unserved %v, want the requested provider's own failure kept",
			resp.ProviderName, resp.Unserved)
	}
}

func TestGetAlbumTracks_fallbackSkipsCandidateWithNoTracks(t *testing.T) {
	fetched := []string{}
	deezer := &fakeAlbumContentProvider{
		getAlbumTracksFn: func(_ context.Context, _ domain.ProviderName, id string) ([]domain.SearchResult, error) {
			fetched = append(fetched, id)
			if id == "111" {
				return nil, nil
			}
			return []domain.SearchResult{{
				Kind: domain.ResultKindTrack, Title: "T",
				Sources: []domain.SourceRef{{Provider: domain.ProviderDeezer, ExternalID: "t"}},
			}}, nil
		},
	}
	searcher := &fakeAlbumSearcher{results: []domain.SearchResult{
		albumSearchResult("Che", "111"),
		albumSearchResult("Che", "222"),
	}}
	svc := albumTracksSvc(deezer, searcher)

	resp, err := svc.ExecuteRequest(context.Background(), AlbumTracksRequest{Provider: domain.ProviderSoundCloud, ExternalID: "sc-1", Title: "Empty Clip", Artist: "Che"})
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
	deezer := &fakeAlbumContentProvider{
		getAlbumTracksFn: func(context.Context, domain.ProviderName, string) ([]domain.SearchResult, error) {
			return nil, nil
		},
	}
	svc := albumTracksSvc(deezer, &fakeAlbumSearcher{})

	resp, err := svc.ExecuteRequest(context.Background(), AlbumTracksRequest{Provider: domain.ProviderDeezer, ExternalID: "d-1"})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Status != domain.ProviderStatusOK || len(resp.Items) != 0 {
		t.Fatalf("resp = %v/%d items, want OK/0", resp.Status, len(resp.Items))
	}
}

type panickingTrackFeatured struct{ fakeTrackFeatured }

func (panickingTrackFeatured) LookupTrackFeatured(context.Context, string) ([]domain.FeaturedArtist, error) {
	panic("featured lookup exploded")
}

// Regression test for #568: a panic in a goroutine spawned around a provider
// or port call must be contained, not terminate the process.
func TestEnrichFeatured_PanickingLookupIsContained(t *testing.T) {
	svc := NewGetAlbumTracksService(nil, WithTrackFeatured(panickingTrackFeatured{}))
	results := []domain.SearchResult{deezerTrackFeat("1", "Singapore")}

	svc.enrichFeatured(context.Background(), results)

	if _, present := results[0].Extras["featured_artists"]; present {
		t.Errorf("featured should be unset after a panicking lookup, got %v", results[0].Extras["featured_artists"])
	}
}
