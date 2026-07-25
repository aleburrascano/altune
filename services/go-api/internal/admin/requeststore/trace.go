package requeststore

import (
	"context"
	"time"

	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/shared/httputil"
)

func (s *Store) RecordSearch(
	ctx context.Context,
	query string,
	kinds []string,
	user string,
	statuses []domain.ProviderSearchResponse,
	final []domain.SearchResult,
) {
	corrID := httputil.GetCorrelationID(ctx)
	if corrID == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	rec := s.getOrCreateLocked(corrID, time.Now().UTC())
	rec.Query = query
	rec.Kinds = kinds
	rec.User = user
	rec.Providers = ProjectStatuses(statuses)
	rec.Final = ProjectResults(final)
}

func (s *Store) RecordContentFetch(
	ctx context.Context,
	kind, provider, artist, status string,
	items []domain.SearchResult,
) {
	corrID := httputil.GetCorrelationID(ctx)
	if corrID == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	rec := s.getOrCreateLocked(corrID, time.Now().UTC())
	rec.Detail = &DetailTrace{
		Kind:     kind,
		Provider: provider,
		Artist:   artist,
		Status:   status,
		Items:    projectDetailRows(items),
	}
}

func projectDetailRows(items []domain.SearchResult) []DetailRow {
	out := make([]DetailRow, 0, len(items))
	for _, it := range items {
		out = append(out, DetailRow{
			Title:            it.Title,
			Year:             it.Year,
			ConsensusVerdict: extraStr(it, "consensus_status"),
		})
	}
	return out
}

func extraStr(r domain.SearchResult, key string) string {
	if v, ok := r.Extras[key].(string); ok {
		return v
	}
	return ""
}

func ProjectStatuses(statuses []domain.ProviderSearchResponse) []ProviderTrace {
	out := make([]ProviderTrace, 0, len(statuses))
	for _, st := range statuses {
		out = append(out, ProviderTrace{
			Provider:    st.Provider.String(),
			Status:      st.Status.String(),
			LatencyMs:   st.LatencyMs,
			ResultCount: st.ResultCount,
			Results:     ProjectResults(st.Results),
		})
	}
	return out
}

func ProjectResults(results []domain.SearchResult) []ResultRow {
	out := make([]ResultRow, 0, len(results))
	for _, r := range results {
		out = append(out, ResultRow{
			Kind:                  r.Kind.String(),
			Title:                 r.Title,
			Subtitle:              r.Subtitle,
			ImageURL:              r.ImageURL,
			Sources:               sourceNames(r.Sources),
			ArtworkSource:         r.ArtworkSource,
			ArtworkResolutionPath: extraStr(r, "artwork_path"),
			ResolutionTier:        extraStr(r, "resolution_tier"),
			Confidence:            r.Confidence.String(),
		})
	}
	return out
}

func sourceNames(sources []domain.SourceRef) []string {
	out := make([]string, 0, len(sources))
	for _, s := range sources {
		out = append(out, s.Provider.String())
	}
	return out
}
