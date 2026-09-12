package app

import (
	"altune/go-api/internal/discovery/domain"
	"fmt"
	"strings"
)

// parseSearchKinds turns the raw kind tokens from an admin search-debug request
// into a result-kind set, rejecting any unparseable token with a typed "invalid
// kinds" error and defaulting to the full set when none are given. It is the
// shared kinds gate for the search-debug seam: both reRun (rerun.go) and
// inspectSearch (search_inspector.go) call it, so it lives here rather than
// beside either caller.
func parseSearchKinds(kinds []string) (map[domain.ResultKind]bool, error) {
	out := map[domain.ResultKind]bool{}
	var invalid []string
	for _, k := range kinds {
		rk, err := domain.ParseResultKind(k)
		if err != nil {
			invalid = append(invalid, k)
			continue
		}
		out[rk] = true
	}
	if len(invalid) > 0 {
		return nil, fmt.Errorf("invalid kinds: %s", strings.Join(invalid, ", "))
	}
	if len(out) == 0 {
		out[domain.ResultKindTrack] = true
		out[domain.ResultKindAlbum] = true
		out[domain.ResultKindArtist] = true
	}
	return out, nil
}
