package eval

import "altune/go-api/internal/discovery/domain"

// stringExtra is a local copy of the merge.go read accessor. It is trivial and
// pure; copying it keeps this offline harness from depending on — and forcing
// the export of — the core service package's internal helpers ("a little
// copying is better than a little dependency").
func stringExtra(r domain.SearchResult, key string) string {
	if r.Extras == nil {
		return ""
	}
	if v, ok := r.Extras[key].(string); ok {
		return v
	}
	return ""
}
