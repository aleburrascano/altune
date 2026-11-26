package memory

import "altune/go-api/internal/search/internal/domain"

func AlbumSearchResult(artist, deezerID string) domain.SearchResult {
	return domain.SearchResult{
		Kind: domain.ResultKindAlbum, Title: "Empty Clip", Subtitle: artist,
		Sources: []domain.ProviderLink{{Provider: domain.ProviderDeezer, ExternalID: deezerID}},
	}
}
