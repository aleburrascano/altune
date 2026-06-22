package service

import (
	"math"

	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/shared/textnorm"
)

// Layer 2 — merge + entity resolution.
//
// "Same entity?" is decided by the only principled signals available: shared
// identifiers, then exact canonical-title equality. There is deliberately NO
// version-marker vocabulary and NO fuzzy threshold — those were query-fit
// heuristics (a hand-curated keyword list standing in for a tuned constant), so
// they are removed. The shared canonical normalization (textnorm) is the single
// structural decision: it defines what "same title" means, and it already
// preserves the distinctions that genuinely live in the title text. A trailing
// sequel number survives normalization ("Shotta Flow 2" ≠ "Shotta Flow", so
// Pattern B holds with no machinery), while a parenthetical "(2007 Remaster)"
// is canonical noise and folds away.
//
// Identifiers are the authority: a remaster, a sequel, and a remix each carry a
// different ISRC, so when a provider supplies one the decision is exact. The
// text fallback is irreducibly imperfect — that imperfection is the true cost of
// a missing identifier, not something a keyword list can honestly erase.

// Entity is a merged search result plus per-provider rank provenance: the best
// (lowest) position at which this entity surfaced in each provider's native
// ordering. Layer 3 consumes BestRank for the RRF within-tier tiebreak.
type Entity struct {
	Result   domain.SearchResult
	BestRank map[domain.ProviderName]int
}

// Merge collapses per-provider result groups into deduped entities by shared
// identifier or exact canonical title. Sources are unioned; the most complete
// variant becomes canonical. Native per-provider ordering is preserved as
// BestRank.
func Merge(perProvider [][]domain.SearchResult) []Entity {
	entities := make([]Entity, 0)
	for _, group := range perProvider {
		for rank, c := range group {
			cProviders := providersOf(c)

			merged := false
			for i := range entities {
				tier, ok := sameEntity(entities[i].Result, c)
				if !ok {
					continue
				}
				entities[i].Result = mergeInto(entities[i].Result, c, tier)
				for p := range cProviders {
					if prev, exists := entities[i].BestRank[p]; !exists || rank < prev {
						entities[i].BestRank[p] = rank
					}
				}
				merged = true
				break
			}
			if merged {
				continue
			}

			ranks := make(map[domain.ProviderName]int, len(cProviders))
			for p := range cProviders {
				ranks[p] = rank
			}
			entities = append(entities, Entity{Result: c, BestRank: ranks})
		}
	}
	return entities
}

// sameEntity decides identity by identifier, then exact canonical title (with
// artist) — and reports the strongest tier that proved it.
func sameEntity(e, c domain.SearchResult) (domain.EntityResolutionTier, bool) {
	if e.Kind != c.Kind {
		return domain.EntityResolutionNone, false
	}

	// Identifier authority.
	if ie, ic := stringExtra(e, "isrc"), stringExtra(c, "isrc"); ie != "" && ic != "" && ie == ic {
		return domain.EntityResolutionISRC, true
	}
	if me, mc := stringExtra(e, "mbid"), stringExtra(c, "mbid"); me != "" && mc != "" {
		if me == mc {
			return domain.EntityResolutionMBID, true
		}
		return domain.EntityResolutionNone, false
	}

	// Cross-provider identity bridge: a stated id (MB → Deezer/Spotify/Discogs,
	// stamped into extras pre-merge) that matches another result's native
	// provider id proves the same entity even when the titles differ. Additive —
	// it only ever merges; it never blocks a name match.
	if bridgeMatch(e, c) {
		return domain.EntityResolutionBridge, true
	}

	// Artists resolve by canonical name alone.
	if e.Kind == domain.ResultKindArtist {
		same := textnorm.NormalizeForMatch(e.Title) == textnorm.NormalizeForMatch(c.Title)
		return domain.EntityResolutionNone, same
	}

	// Tracks/albums: same artist and same canonical title.
	if textnorm.NormalizeForMatch(e.Subtitle) != textnorm.NormalizeForMatch(c.Subtitle) {
		return domain.EntityResolutionNone, false
	}
	if textnorm.NormalizeForMatch(e.Title) == textnorm.NormalizeForMatch(c.Title) {
		return domain.EntityResolutionNone, true
	}
	return domain.EntityResolutionNone, false
}

// mergeInto folds other into canonical: the more complete result wins title/
// subtitle/image, sources are unioned, popularity is the max, and the merge's
// resolution tier and display confidence are recorded.
func mergeInto(canonical, other domain.SearchResult, tier domain.EntityResolutionTier) domain.SearchResult {
	if completenessOf(other) > completenessOf(canonical) {
		canonical, other = other, canonical
	}

	seen := make(map[string]bool, len(canonical.Sources)+len(other.Sources))
	sources := make([]domain.SourceRef, 0, len(canonical.Sources)+len(other.Sources))
	for _, s := range append(append([]domain.SourceRef{}, canonical.Sources...), other.Sources...) {
		key := s.Provider.String() + ":" + s.ExternalID
		if seen[key] {
			continue
		}
		seen[key] = true
		sources = append(sources, s)
	}

	extras := make(map[string]any, len(canonical.Extras)+len(other.Extras))
	for k, v := range other.Extras {
		extras[k] = v
	}
	for k, v := range canonical.Extras {
		if v != nil || extras[k] == nil {
			extras[k] = v
		}
	}
	extras["resolution_tier"] = tier.String()

	imageURL := canonical.ImageURL
	if imageURL == "" {
		imageURL = other.ImageURL
	}

	conf := domain.ConfidenceLow
	switch tier {
	case domain.EntityResolutionISRC, domain.EntityResolutionMBID, domain.EntityResolutionBridge:
		conf = domain.ConfidenceHigh
	default:
		if len(sources) > 1 {
			conf = domain.ConfidenceMedium
		}
	}

	return domain.SearchResult{
		Kind:       canonical.Kind,
		Title:      canonical.Title,
		Subtitle:   canonical.Subtitle,
		ImageURL:   imageURL,
		Confidence: conf,
		Sources:    sources,
		Popularity: math.Max(canonical.Popularity, other.Popularity),
		Extras:     extras,
	}
}

// providerID is one (provider, external id) identity claim.
type providerID struct {
	provider domain.ProviderName
	id       string
}

// bridgeMatch reports whether e and c share any cross-provider identity claim.
// A claim is either a native source id or a bridged id stamped into extras["xref"]
// (MB → provider, populated pre-merge from the IdentityBridge). At least one
// bridged claim must participate — two native ids alone are same-provider dups,
// not a cross-provider bridge — so e must carry an xref for this to fire.
func bridgeMatch(e, c domain.SearchResult) bool {
	if _, ok := e.Extras["xref"].(map[string]string); !ok {
		if _, ok := c.Extras["xref"].(map[string]string); !ok {
			return false
		}
	}
	ec := identityClaims(e)
	if len(ec) == 0 {
		return false
	}
	for claim := range identityClaims(c) {
		if ec[claim] {
			return true
		}
	}
	return false
}

// identityClaims gathers a result's (provider, id) claims: native source ids plus
// any bridged ids stamped into extras["xref"].
func identityClaims(r domain.SearchResult) map[providerID]bool {
	claims := make(map[providerID]bool, len(r.Sources)+1)
	for _, s := range r.Sources {
		if s.ExternalID != "" {
			claims[providerID{s.Provider, s.ExternalID}] = true
		}
	}
	if xref, ok := r.Extras["xref"].(map[string]string); ok {
		for name, id := range xref {
			if id == "" {
				continue
			}
			if p, err := domain.ParseProviderName(name); err == nil {
				claims[providerID{p, id}] = true
			}
		}
	}
	return claims
}

func stringExtra(r domain.SearchResult, key string) string {
	if r.Extras == nil {
		return ""
	}
	if v, ok := r.Extras[key].(string); ok {
		return v
	}
	return ""
}

// popularityOf is the single read accessor for the typed Popularity signal —
// merge, rank, diversity, and vocab all read through it.
func popularityOf(r domain.SearchResult) float64 {
	return r.Popularity
}

func completenessOf(r domain.SearchResult) int {
	n := 0
	if r.ImageURL != "" {
		n++
	}
	if stringExtra(r, "isrc") != "" {
		n++
	}
	if r.Extras != nil {
		if _, ok := r.Extras["duration"]; ok {
			n++
		}
	}
	if stringExtra(r, "album") != "" {
		n++
	}
	return n
}

func providersOf(r domain.SearchResult) map[domain.ProviderName]bool {
	m := make(map[domain.ProviderName]bool, len(r.Sources))
	for _, s := range r.Sources {
		m[s.Provider] = true
	}
	return m
}
