package service

import "altune/go-api/internal/shared/textnorm"

// Identity verification against the MusicBrainz anchor (doc §7). The contamination
// that survived every title/keep layer is a FRACTURED identity: MusicBrainz has a
// wrong url-relation (the rapper Che's MBID links to a *different* Che's Deezer
// id), so the fan-out returns two artists, each legitimately id-verified.
//
// The only signal that doesn't suffer same-name title collisions is the artist's
// OWN authoritative catalogue: the MBID *is* the identity, and MB's release-group
// set is what that artist actually released. A mis-bridged provider is a different
// artist, so its catalogue barely overlaps MB's (live-measured: the soul Che's
// Deezer shares 8% of the rapper's MB release-groups; the real rapper Deezer
// shares 60%). We verify each id-fanout provider group against the MB set and drop
// the ones that don't belong.

const (
	// mbAnchorMinReleaseGroups is how many release-groups MB must know before it is
	// a credible identity anchor. Below this we can't judge, so we verify nothing.
	mbAnchorMinReleaseGroups = 5
	// mbVerifyMinTitles is the fewest releases a provider must return before we
	// judge it — too few and one coincidence dominates, so we keep it.
	mbVerifyMinTitles = 4
	// A provider is kept if it shares at least mbVerifyMinOverlap releases with the
	// MB set OR mbVerifyMinRatio of its own catalogue is in it. The soul-Che margin
	// (8% / 3 shared) sits far below both; the real artist (60%+) far above.
	mbVerifyMinOverlap = 4
	mbVerifyMinRatio   = 0.25
)

// FilterGroupsByMBAnchor drops id-fanout provider groups whose catalogue does not
// meaningfully overlap the MBID's release-group titles — the mis-bridged
// same-name artists. mbTitles are the normalized MB release-group titles. When MB
// is not a credible anchor (too few titles), every group is kept.
func FilterGroupsByMBAnchor(mbTitles map[string]bool, groups []ReleaseGroup) []ReleaseGroup {
	if len(mbTitles) < mbAnchorMinReleaseGroups {
		return groups
	}
	out := make([]ReleaseGroup, 0, len(groups))
	for _, g := range groups {
		if groupMatchesAnchor(g, mbTitles) {
			out = append(out, g)
		}
	}
	return out
}

func groupMatchesAnchor(g ReleaseGroup, mbTitles map[string]bool) bool {
	titles := make(map[string]bool, len(g.Releases))
	for _, r := range g.Releases {
		if k := textnorm.NormalizeForMatch(r.Title); k != "" {
			titles[k] = true
		}
	}
	if len(titles) < mbVerifyMinTitles {
		return true // too few releases to judge — benefit of the doubt
	}
	overlap := 0
	for k := range titles {
		if mbTitles[k] {
			overlap++
		}
	}
	return overlap >= mbVerifyMinOverlap || float64(overlap)/float64(len(titles)) >= mbVerifyMinRatio
}

// normalizeTitleSet builds the normalized-title lookup used as the MB anchor.
func normalizeTitleSet(titles []string) map[string]bool {
	set := make(map[string]bool, len(titles))
	for _, t := range titles {
		if k := textnorm.NormalizeForMatch(t); k != "" {
			set[k] = true
		}
	}
	return set
}
