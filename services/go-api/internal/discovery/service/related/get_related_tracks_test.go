package related_test

import (
	"altune/go-api/internal/discovery"
	"altune/go-api/internal/discovery/discoverytest"
	"context"
	"errors"
	"testing"
)

func relatedTrack(title string) discovery.SearchResult {
	return discovery.SearchResult{Kind: discovery.ResultKindTrack, Title: title}
}

func TestGetRelatedTracksService_Execute(t *testing.T) {
	tests := []struct {
		name       string
		provider   discovery.ProviderName
		fake       *discoverytest.FakeRelatedProvider
		limit      int
		wantStatus discovery.ProviderStatus
		wantCount  int
	}{
		{
			name:       "unknown provider",
			provider:   discovery.ProviderDeezer,
			fake:       &discoverytest.FakeRelatedProvider{Results: []discovery.SearchResult{relatedTrack("a")}},
			limit:      20,
			wantStatus: discovery.ProviderStatusError,
			wantCount:  0,
		},
		{
			name:       "provider returns error",
			provider:   discovery.ProviderSoundCloud,
			fake:       &discoverytest.FakeRelatedProvider{Err: errors.New("boom")},
			limit:      20,
			wantStatus: discovery.ProviderStatusError,
			wantCount:  0,
		},
		{
			name:     "ok slices to limit",
			provider: discovery.ProviderSoundCloud,
			fake: &discoverytest.FakeRelatedProvider{Results: []discovery.SearchResult{
				relatedTrack("a"), relatedTrack("b"), relatedTrack("c"),
			}},
			limit:      2,
			wantStatus: discovery.ProviderStatusOK,
			wantCount:  2,
		},
		{
			name:     "ok no truncation when under limit",
			provider: discovery.ProviderSoundCloud,
			fake: &discoverytest.FakeRelatedProvider{Results: []discovery.SearchResult{
				relatedTrack("a"),
			}},
			limit:      20,
			wantStatus: discovery.ProviderStatusOK,
			wantCount:  1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := discovery.NewGetRelatedTracksService(map[string]discovery.RelatedTracksProvider{
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
