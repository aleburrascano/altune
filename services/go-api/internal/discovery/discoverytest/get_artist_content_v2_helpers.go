package discoverytest

import "altune/go-api/internal/discovery/domain"

func TrackFrom(provider domain.ProviderName, id, title, artist string) domain.SearchResult {
	return domain.SearchResult{
		Kind:     domain.ResultKindTrack,
		Title:    title,
		Subtitle: artist,
		Sources:  []domain.SourceRef{{Provider: provider, ExternalID: id}},
	}
}

func V2Album(provider domain.ProviderName, id, title string, opts ...func(*domain.SearchResult)) domain.SearchResult {
	r := domain.SearchResult{
		Kind:     domain.ResultKindAlbum,
		Title:    title,
		Subtitle: "Che",
		Sources:  []domain.SourceRef{{Provider: provider, ExternalID: id}},
		Extras:   map[string]any{},
	}
	for _, o := range opts {
		o(&r)
	}
	return r
}
