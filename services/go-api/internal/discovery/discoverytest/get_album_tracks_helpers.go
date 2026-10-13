package discoverytest

import "altune/go-api/internal/discovery/domain"

func AlbumSearchResult(artist, deezerID string) domain.SearchResult {
	return domain.SearchResult{
		Kind: domain.ResultKindAlbum, Title: "Empty Clip", Subtitle: artist,
		Sources: []domain.SourceRef{{Provider: domain.ProviderDeezer, ExternalID: deezerID}},
	}
}
