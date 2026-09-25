package requeststore

import "altune/go-api/internal/discovery/domain"

type ProviderTrace struct {
	Provider    string      `json:"provider"`
	Status      string      `json:"status"`
	LatencyMs   int64       `json:"latency_ms"`
	ResultCount int         `json:"result_count"`
	Err         string      `json:"error,omitempty"`
	Results     []ResultRow `json:"results,omitempty"`
}

type ResultRow struct {
	Kind                  string   `json:"kind"`
	Title                 string   `json:"title"`
	Subtitle              string   `json:"subtitle,omitempty"`
	ImageURL              string   `json:"image_url,omitempty"`
	Sources               []string `json:"sources,omitempty"`
	ArtworkSource         string   `json:"artwork_source,omitempty"`
	ArtworkResolutionPath string   `json:"artwork_path,omitempty"`
	ResolutionTier        string   `json:"resolution_tier,omitempty"`
	Confidence            string   `json:"confidence,omitempty"`
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
			ArtworkResolutionPath: extraStr(r, domain.ExtraArtworkPath),
			ResolutionTier:        resolutionTierLabel(r.ResolutionTier),
			Confidence:            r.Confidence.String(),
		})
	}
	return out
}

func resolutionTierLabel(stamp domain.ResolutionTierStamp) string {
	if !stamp.Stamped {
		return ""
	}
	return stamp.Tier.String()
}

func sourceNames(sources []domain.SourceRef) []string {
	out := make([]string, 0, len(sources))
	for _, s := range sources {
		out = append(out, s.Provider.String())
	}
	return out
}
