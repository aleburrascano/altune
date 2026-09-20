package service

import (
	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/discovery/ports"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

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
