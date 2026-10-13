package discoverytest

import "altune/go-api/internal/discovery/domain"

func WithPop(r domain.SearchResult, pop float64) domain.SearchResult {
	r.Popularity = pop
	return r
}

func DeezerTrack(title, artist string, pop float64) domain.SearchResult {
	return WithPop(Track(title, artist, domain.ProviderDeezer, nil), pop)
}

func DeezerAlbum(title, artist string, pop float64) domain.SearchResult {
	return WithPop(Res(domain.ResultKindAlbum, title, artist, domain.ProviderDeezer, nil), pop)
}

func Titles(results []domain.SearchResult) []string {
	out := make([]string, len(results))
	for i, r := range results {
		out[i] = r.Title
	}
	return out
}

func WithISRC(r domain.SearchResult, isrc string) domain.SearchResult {
	r.ISRC = isrc
	return r
}
