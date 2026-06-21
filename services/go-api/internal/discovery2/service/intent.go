package service

import (
	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/shared/textnorm"
)

// Layer 0 — query understanding.
//
// Intent is the authoritative structured reading of the query that downstream
// layers trust. It is a CONTRACT, not a score nudge: Layer 3 reads it to select
// relevance tiers. Empty Artist/Title mean "unspecified"; Kind ==
// ResultKindUnknown means "no kind preference".
type Intent struct {
	Artist string // normalized
	Title  string // normalized — the title the user is looking for
	Kind   domain.ResultKind
}

// BuildIntent assembles the Layer-0 intent from the normalized query and an
// optional structured artist/title split (produced by the vocabulary-backed
// detector at orchestration time).
//
// When the query splits cleanly into artist + title, the intended kind is a
// track — people search "<artist> <title>" looking for the song. This single
// categorical inference is what lets Layer 3 seat the exact track at T1 and the
// same-named album at T2 directly below it (Pattern A), and it is safe when the
// guess is wrong: if no track matches, the album is still the top non-T1 tier.
func BuildIntent(queryNorm, artist, title string) Intent {
	a := textnorm.NormalizeForMatch(artist)
	t := textnorm.NormalizeForMatch(title)

	kind := domain.ResultKindUnknown
	if a != "" && t != "" {
		kind = domain.ResultKindTrack
	}
	if t == "" {
		t = textnorm.NormalizeForMatch(queryNorm)
	}
	return Intent{Artist: a, Title: t, Kind: kind}
}
