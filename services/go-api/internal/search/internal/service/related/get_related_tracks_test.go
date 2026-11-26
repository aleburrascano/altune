package related_test

import (
	"altune/go-api/internal/search"
	"altune/go-api/internal/search/internal/searchtest"
	"context"
	"errors"
	"strings"
	"testing"
)

func relatedTrack(title string) search.SearchResult {
	return search.SearchResult{Kind: search.ResultKindTrack, Title: title}
}

func TestNewGetRelatedTracksService_RefusesAMissingCollaborator(t *testing.T) {
	cases := []struct {
		name      string
		providers map[string]search.RelatedTracksProvider
		breaker   *search.CircuitBreaker
		missing   string
	}{
		{"no providers", nil, search.NewCircuitBreaker(), "providers"},
		{"no breaker", map[string]search.RelatedTracksProvider{}, nil, "breaker"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svc, err := search.NewGetRelatedTracksService(tc.providers, tc.breaker)
			if !errors.Is(err, search.ErrRelatedCollaboratorRequired) || svc != nil || !strings.Contains(err.Error(), tc.missing) {
				t.Errorf("svc = %v, err = %v; want nil and ErrRelatedCollaboratorRequired naming %q", svc, err, tc.missing)
			}
		})
	}
}

func TestGetRelatedTracksService_Execute(t *testing.T) {
	tests := []struct {
		name       string
		provider   search.ProviderName
		fake       *search.FakeRelatedProvider
		limit      int
		wantStatus search.ProviderStatus
		wantCount  int
	}{
		{
			name:       "unknown provider",
			provider:   search.ProviderDeezer,
			fake:       &search.FakeRelatedProvider{Results: []search.SearchResult{relatedTrack("a")}},
			limit:      20,
			wantStatus: search.ProviderStatusError,
			wantCount:  0,
		},
		{
			name:       "provider returns error",
			provider:   search.ProviderSoundCloud,
			fake:       &search.FakeRelatedProvider{Err: errors.New("boom")},
			limit:      20,
			wantStatus: search.ProviderStatusError,
			wantCount:  0,
		},
		{
			name:     "ok slices to limit",
			provider: search.ProviderSoundCloud,
			fake: &search.FakeRelatedProvider{Results: []search.SearchResult{
				relatedTrack("a"), relatedTrack("b"), relatedTrack("c"),
			}},
			limit:      2,
			wantStatus: search.ProviderStatusOK,
			wantCount:  2,
		},
		{
			name:     "ok no truncation when under limit",
			provider: search.ProviderSoundCloud,
			fake: &search.FakeRelatedProvider{Results: []search.SearchResult{
				relatedTrack("a"),
			}},
			limit:      20,
			wantStatus: search.ProviderStatusOK,
			wantCount:  1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := searchtest.NewRelatedTracks(t, map[string]search.RelatedTracksProvider{
				"soundcloud": tt.fake,
			})

			resp, err := svc.Execute(context.Background(), tt.provider, "123", tt.limit)
			if err != nil {
				t.Fatalf("Execute error: %v", err)
			}
			if resp.Status != tt.wantStatus {
				t.Errorf("status = %v, want %v", resp.Status, tt.wantStatus)
			}
			if len(resp.Items) != tt.wantCount {
				t.Errorf("item count = %d, want %d", len(resp.Items), tt.wantCount)
			}
			if resp.Items == nil {
				t.Error("Items must never be nil (JSON null vs [])")
			}
		})
	}
}
