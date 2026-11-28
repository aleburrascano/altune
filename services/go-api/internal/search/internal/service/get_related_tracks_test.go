package service_test

import (
	"altune/go-api/internal/search/internal/adapters/memory"
	"altune/go-api/internal/search/internal/domain"
	"altune/go-api/internal/search/internal/ports"
	"altune/go-api/internal/search/internal/service"
	"altune/go-api/internal/shared/clock"
	"context"
	"errors"
	"strings"
	"testing"
)

func relatedTrack(title string) domain.SearchResult {
	return memory.NewResult(domain.ResultKindTrack, title)
}

func TestNewGetRelatedTracksService_RefusesAMissingCollaborator(t *testing.T) {
	cases := []struct {
		name      string
		providers map[string]ports.RelatedTracksProvider
		breaker   *service.CircuitBreaker
		missing   string
	}{
		{"no providers", nil, service.NewCircuitBreaker(clock.Now), "providers"},
		{"no breaker", map[string]ports.RelatedTracksProvider{}, nil, "breaker"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svc, err := service.NewGetRelatedTracksService(tc.providers, tc.breaker)
			if !errors.Is(err, domain.ErrRelatedCollaboratorRequired) || svc != nil || !strings.Contains(err.Error(), tc.missing) {
				t.Errorf("svc = %v, err = %v; want nil and related.ErrCollaboratorRequired naming %q", svc, err, tc.missing)
			}
		})
	}
}

func TestGetRelatedTracksService_Execute(t *testing.T) {
	tests := []struct {
		name       string
		provider   domain.ProviderName
		fake       *memory.FakeRelatedProvider
		limit      int
		wantStatus domain.ProviderStatus
		wantCount  int
	}{
		{
			name:       "unknown provider",
			provider:   domain.ProviderDeezer,
			fake:       &memory.FakeRelatedProvider{Results: []domain.SearchResult{relatedTrack("a")}},
			limit:      20,
			wantStatus: domain.ProviderStatusError,
			wantCount:  0,
		},
		{
			name:       "provider returns error",
			provider:   domain.ProviderSoundCloud,
			fake:       &memory.FakeRelatedProvider{Err: errors.New("boom")},
			limit:      20,
			wantStatus: domain.ProviderStatusError,
			wantCount:  0,
		},
		{
			name:     "ok slices to limit",
			provider: domain.ProviderSoundCloud,
			fake: &memory.FakeRelatedProvider{Results: []domain.SearchResult{
				relatedTrack("a"), relatedTrack("b"), relatedTrack("c"),
			}},
			limit:      2,
			wantStatus: domain.ProviderStatusOK,
			wantCount:  2,
		},
		{
			name:     "ok no truncation when under limit",
			provider: domain.ProviderSoundCloud,
			fake: &memory.FakeRelatedProvider{Results: []domain.SearchResult{
				relatedTrack("a"),
			}},
			limit:      20,
			wantStatus: domain.ProviderStatusOK,
			wantCount:  1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := memory.NewRelatedTracks(t, map[string]ports.RelatedTracksProvider{
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
