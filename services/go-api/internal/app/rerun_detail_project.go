package app

import (
	"altune/go-api/internal/admin/requeststore"
	"altune/go-api/internal/discovery/domain"
)

func detailEntity(entity domain.SearchResult, byProvider map[domain.ProviderName]string) *requeststore.DetailEntity {
	sources := make(map[string]string, len(byProvider))
	for provider, id := range byProvider {
		sources[provider.String()] = id
	}
	return &requeststore.DetailEntity{
		Title:    entity.Title,
		Subtitle: entity.Subtitle,
		MBID:     entity.MBID,
		Sources:  sources,
	}
}

func seedIDsByProvider(sources []domain.SourceRef) map[domain.ProviderName]string {
	m := make(map[domain.ProviderName]string, len(sources))
	for _, s := range sources {
		if _, exists := m[s.Provider]; !exists {
			m[s.Provider] = s.ExternalID
		}
	}
	return m
}

func projectSeeds(seeds []rawSeed) []requeststore.DetailSeedGroup {
	out := make([]requeststore.DetailSeedGroup, 0, len(seeds))
	for _, s := range seeds {
		out = append(out, requeststore.DetailSeedGroup{
			Provider:   s.provider.String(),
			ExternalID: s.externalID,
			Status:     s.status.String(),
			Error:      s.err,
			Items:      projectDetailItems(s.items),
		})
	}
	return out
}

func projectDetailItems(items []domain.SearchResult) []requeststore.DetailItemRow {
	out := make([]requeststore.DetailItemRow, 0, len(items))
	for _, it := range items {
		out = append(out, requeststore.DetailItemRow{
			Title:      it.Title,
			Subtitle:   it.Subtitle,
			Year:       it.Year,
			TrackCount: it.TrackCount,
			RecordType: string(it.RecordType),
			ImageURL:   it.ImageURL,
			Sources:    seedProviderNames(it.Sources),
		})
	}
	return out
}

func seedProviderNames(sources []domain.SourceRef) []string {
	out := make([]string, 0, len(sources))
	for _, s := range sources {
		out = append(out, s.Provider.String())
	}
	return out
}
