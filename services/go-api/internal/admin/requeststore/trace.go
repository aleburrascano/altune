package requeststore

import (
	"context"
	"time"

	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/shared/httputil"
)

// RecordSearch attaches the query, user, per-provider results, and final ranked
// list to the request record keyed by the context's correlation id — the same
// key the recording transport captured the raw provider exchanges under. Called
// at the discovery handler boundary, off the ranking path. No-op when the request
// carries no correlation id.
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

// ProjectStatuses projects per-provider search responses into the display rows the
// console reads. Exported so the re-run inspector can reuse the same shape.
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

// ProjectResults projects domain results into the display rows the console reads.
func ProjectResults(results []domain.SearchResult) []ResultRow {
	out := make([]ResultRow, 0, len(results))
	for _, r := range results {
		out = append(out, ResultRow{
			Kind:     r.Kind.String(),
			Title:    r.Title,
			Subtitle: r.Subtitle,
			ImageURL: r.ImageURL,
			Sources:  sourceNames(r.Sources),
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
