package requeststore

import "altune/go-api/internal/discovery/domain"

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

func projectDetailRows(items []domain.SearchResult) []DetailRow {
	out := make([]DetailRow, 0, len(items))
	for _, it := range items {
		out = append(out, DetailRow{
			Title:            it.Title,
			Year:             it.Year,
			ConsensusVerdict: extraStr(it, domain.ExtraConsensusStatus),
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
