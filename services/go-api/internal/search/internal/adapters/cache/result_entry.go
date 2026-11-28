package cache

import "altune/go-api/internal/search/internal/domain"

type collapsedArtistEntry struct {
	Title    string                `json:"title"`
	Subtitle string                `json:"subtitle"`
	ImageURL string                `json:"image_url,omitempty"`
	Sources  []domain.ProviderLink `json:"sources"`
	Extras   map[string]any        `json:"extras"`
}

func entriesOf(results []domain.SearchResult) []domain.SearchResult {
	if results == nil {
		return nil
	}
	entries := make([]domain.SearchResult, len(results))
	for i, r := range results {
		entries[i] = entryOf(r)
	}
	return entries
}

func entryOf(r domain.SearchResult) domain.SearchResult {
	collapsed, ok := r.Extras[domain.ExtraCollapsedArtists].([]domain.CollapsedArtistSummary)
	if !ok {
		return r
	}
	stored := make([]collapsedArtistEntry, len(collapsed))
	for i, c := range collapsed {
		stored[i] = collapsedArtistEntry{Title: c.Title, Subtitle: c.Subtitle, ImageURL: c.ImageURL, Sources: c.Sources, Extras: c.Extras}
	}
	return r.WithExtra(domain.ExtraCollapsedArtists, stored)
}
