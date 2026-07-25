package eval

import "altune/go-api/internal/discovery/domain"

func res(kind domain.ResultKind, title, subtitle string, provider domain.ProviderName, extras map[string]any) domain.SearchResult {
	return domain.SearchResult{
		Kind:     kind,
		Title:    title,
		Subtitle: subtitle,
		Sources: []domain.SourceRef{
			{Provider: provider, ExternalID: title + ":" + provider.String(), URL: "https://x/" + title},
		},
		Popularity: popFromExtras(extras),
		Extras:     extras,
	}
}

func popFromExtras(extras map[string]any) float64 {
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

func track(title, artist string, provider domain.ProviderName, extras map[string]any) domain.SearchResult {
	return res(domain.ResultKindTrack, title, artist, provider, extras)
}
