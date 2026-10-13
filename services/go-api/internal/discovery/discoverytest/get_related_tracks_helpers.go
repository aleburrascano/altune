package discoverytest

import (
	"altune/go-api/internal/discovery/domain"
	"context"
)

type FakeRelatedProvider struct {
	Results []domain.SearchResult
	Err     error
}

func (f *FakeRelatedProvider) GetRelatedTracks(_ context.Context, _ domain.ProviderName, _ string) ([]domain.SearchResult, error) {
	return f.Results, f.Err
}
