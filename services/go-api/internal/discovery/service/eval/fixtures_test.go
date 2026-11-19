package eval_test

import "altune/go-api/internal/discovery"

func res(kind discovery.ResultKind, title, subtitle string, provider discovery.ProviderName, extras map[string]any) discovery.SearchResult {
	return discovery.SearchResult{
		Kind:     kind,
		Title:    title,
		Subtitle: subtitle,
		Sources: []discovery.SourceRef{
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

func track(title, artist string, provider discovery.ProviderName, extras map[string]any) discovery.SearchResult {
	return res(discovery.ResultKindTrack, title, artist, provider, extras)
}
