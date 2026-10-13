package discoverytest

import "altune/go-api/internal/discovery/domain"

func Res(kind domain.ResultKind, title, subtitle string, provider domain.ProviderName, extras map[string]any) domain.SearchResult {
	return domain.SearchResult{
		Kind:     kind,
		Title:    title,
		Subtitle: subtitle,
		Sources: []domain.SourceRef{
			{Provider: provider, ExternalID: title + ":" + provider.String(), URL: "https://x/" + title},
		},
		Popularity: PopFromExtras(extras),
		Extras:     extras,
	}
}

func Track(title, artist string, provider domain.ProviderName, extras map[string]any) domain.SearchResult {
	return Res(domain.ResultKindTrack, title, artist, provider, extras)
}

func PopFromExtras(extras map[string]any) float64 {
	switch n := extras["popularity"].(type) {
	case float64:
		return n
	case int64:
		return float64(n)
	case int:
		return float64(n)
	}
	return 0
}
