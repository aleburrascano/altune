package eval

import "altune/go-api/internal/discovery/domain"

// Test fixtures local to the eval harness package. Copies of the core service
// package's merge_test.go builders: white-box test helpers can't cross a package
// boundary, and copying a few fixture constructors is cheaper than exporting test
// scaffolding from core. Keep in sync if the SearchResult shape changes.

// res builds a SearchResult with one source for the given provider.
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

// popFromExtras lifts a fixture's legacy "popularity" key into the typed
// Popularity field, mirroring how providers populate it at ACL translation.
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
