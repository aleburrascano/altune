package requeststore

import (
	"time"

	"altune/go-api/internal/discovery/domain"
)

type Exchange struct {
	Provider  string    `json:"provider,omitempty"`
	Method    string    `json:"method"`
	URL       string    `json:"url"`
	Status    int       `json:"status"`
	LatencyMs int64     `json:"latency_ms"`
	RespBody  string    `json:"response_body"`
	Truncated bool      `json:"truncated,omitempty"`
	Err       string    `json:"error,omitempty"`
	At        time.Time `json:"at"`
}

type RequestRecord struct {
	CorrID    string     `json:"corr_id"`
	StartedAt time.Time  `json:"started_at"`
	Exchanges []Exchange `json:"exchanges"`

	Query     string          `json:"query,omitempty"`
	Kinds     []string        `json:"kinds,omitempty"`
	User      string          `json:"user,omitempty"`
	Providers []ProviderTrace `json:"providers,omitempty"`
	Final     []ResultRow     `json:"final,omitempty"`

	Detail *DetailTrace `json:"detail,omitempty"`

	bytes int
}

type DetailTrace struct {
	Kind     string      `json:"kind"`
	Provider string      `json:"provider"`
	Artist   string      `json:"artist,omitempty"`
	Status   string      `json:"status"`
	Items    []DetailRow `json:"items,omitempty"`
}

type DetailRow struct {
	Title            string `json:"title"`
	Year             int    `json:"year,omitempty"`
	ConsensusVerdict string `json:"status,omitempty"`
}

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
