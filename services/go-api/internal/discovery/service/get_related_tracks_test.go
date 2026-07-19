package service

import (
	"context"
	"errors"
	"testing"

	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/discovery/ports"
)

type fakeRelatedProvider struct {
	results []domain.SearchResult
	err     error
}

func (f *fakeRelatedProvider) GetRelatedTracks(_ context.Context, _ domain.ProviderName, _ string) ([]domain.SearchResult, error) {
	return f.results, f.err
}

func relatedTrack(title string) domain.SearchResult {
	return domain.SearchResult{Kind: domain.ResultKindTrack, Title: title}
}

func TestGetRelatedTracksService_Execute(t *testing.T) {
	tests := []struct {
		name       string
		provider   domain.ProviderName
		fake       *fakeRelatedProvider
		limit      int
		wantStatus domain.ProviderStatus
		wantCount  int
	}{
		{
			name:       "unknown provider",
			provider:   domain.ProviderDeezer,
			fake:       &fakeRelatedProvider{results: []domain.SearchResult{relatedTrack("a")}},
			limit:      20,
			wantStatus: domain.ProviderStatusError,
			wantCount:  0,
		},
		{
			name:       "provider returns error",
			provider:   domain.ProviderSoundCloud,
			fake:       &fakeRelatedProvider{err: errors.New("boom")},
			limit:      20,
			wantStatus: domain.ProviderStatusError,
			wantCount:  0,
		},
		{
			name:     "ok slices to limit",
			provider: domain.ProviderSoundCloud,
			fake: &fakeRelatedProvider{results: []domain.SearchResult{
				relatedTrack("a"), relatedTrack("b"), relatedTrack("c"),
			}},
			limit:      2,
			wantStatus: domain.ProviderStatusOK,
			wantCount:  2,
		},
		{
			name:     "ok no truncation when under limit",
			provider: domain.ProviderSoundCloud,
			fake: &fakeRelatedProvider{results: []domain.SearchResult{
				relatedTrack("a"),
			}},
			limit:      20,
			wantStatus: domain.ProviderStatusOK,
			wantCount:  1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// The fake is always registered under "soundcloud"; the "unknown
			// provider" case asks for "deezer" to exercise the miss path.
			svc := NewGetRelatedTracksService(map[string]ports.RelatedTracksProvider{
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
