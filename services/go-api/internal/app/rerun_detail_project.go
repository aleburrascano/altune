package app

import (
	"altune/go-api/internal/admin/requeststore"
	"altune/go-api/internal/discovery/domain"
)

func detailEntity(entity domain.SearchResult, byProvider map[string]string) *requeststore.DetailEntity {
	return &requeststore.DetailEntity{
		Title:    entity.Title,
		Subtitle: entity.Subtitle,
		MBID:     entity.MBID,
		Sources:  byProvider,
	}
}

func seedIDsByProvider(sources []domain.SourceRef) map[string]string {
	m := make(map[string]string, len(sources))
	for _, s := range sources {
		name := s.Provider.String()
		if _, exists := m[name]; !exists {
			m[name] = s.ExternalID
		}
	}
	return m
}

func projectSeeds(seeds []rawSeed) []requeststore.DetailSeedGroup {
	out := make([]requeststore.DetailSeedGroup, 0, len(seeds))
	for _, s := range seeds {
		out = append(out, requeststore.DetailSeedGroup{
			Provider:   s.provider,
			ExternalID: s.externalID,
			Status:     s.status,
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
			RecordType: detailExtraString(it, "record_type"),
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

func detailExtraString(r domain.SearchResult, key string) string {
	if v, ok := r.Extras[key].(string); ok {
		return v
	}
	return ""
}
