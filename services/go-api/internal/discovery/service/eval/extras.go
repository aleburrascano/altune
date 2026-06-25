package eval

import "altune/go-api/internal/discovery/domain"

// stringExtra and popularityOf are local copies of the merge.go read accessors.
// They are trivial, pure getters over a SearchResult; copying them keeps this
// offline harness from depending on — and forcing the export of — the core
// service package's internal helpers ("a little copying is better than a little
// dependency"). If the underlying Extras/Popularity shape changes, update both.
func stringExtra(r domain.SearchResult, key string) string {
	if r.Extras == nil {
		return ""
	}
	if v, ok := r.Extras[key].(string); ok {
		return v
	}
	return ""
}

func popularityOf(r domain.SearchResult) float64 {
	return r.Popularity
}
