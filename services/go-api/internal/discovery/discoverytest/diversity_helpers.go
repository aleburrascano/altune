package discoverytest

import "altune/go-api/internal/discovery/domain"

func TrackResult(provider domain.ProviderName, extID, title, subtitle string, extras map[string]any) domain.SearchResult {
	return domain.SearchResult{
		Kind:       domain.ResultKindTrack,
		Title:      title,
		Subtitle:   subtitle,
		Sources:    []domain.SourceRef{{Provider: provider, ExternalID: extID, URL: "https://example.com/" + extID}},
		Popularity: PopFromExtras(extras),
		Extras:     extras,
	}
}

func ArtistResult(provider domain.ProviderName, extID, name string, extras map[string]any) domain.SearchResult {
	return domain.SearchResult{
		Kind:       domain.ResultKindArtist,
		Title:      name,
		Sources:    []domain.SourceRef{{Provider: provider, ExternalID: extID, URL: "https://example.com/" + extID}},
		Popularity: PopFromExtras(extras),
		Extras:     extras,
	}
}

func AlbumResult(provider domain.ProviderName, extID, title, subtitle string, extras map[string]any) domain.SearchResult {
	return domain.SearchResult{
		Kind:       domain.ResultKindAlbum,
		Title:      title,
		Subtitle:   subtitle,
		Sources:    []domain.SourceRef{{Provider: provider, ExternalID: extID, URL: "https://example.com/" + extID}},
		Popularity: PopFromExtras(extras),
		Extras:     extras,
	}
}
