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
	ambiguous := ambiguousArtistNames(perProvider)
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
				// Ambiguous-name guard: when MusicBrainz reports >1 distinct artist
				// for this name, a bare name match is NOT identity proof — refuse it
				// so distinct same-name artists (the "Che" problem) keep separate
				// sources. Identity tiers (ISRC/MBID/bridge) still merge freely.
				if c.Kind == domain.ResultKindArtist &&
					tier == domain.EntityResolutionNone &&
					ambiguous[textnorm.NormalizeForMatch(c.Title)] {
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
	if e.ISRC != "" && c.ISRC != "" && e.ISRC == c.ISRC {
		return domain.EntityResolutionISRC, true
	}
	// Album barcode. Positive-only: a mismatched UPC is NOT disproof (editions
	// of one album carry different barcodes), so unlike MBID it never blocks.
	// It only merges when the MBIDs do not conflict (both empty, one empty, or
	// equal): merging two different-MBID albums would keep only one MBID
	// (mergeInto's firstNonEmpty), seeding order-dependent fragmentation when a
	// later result carries the discarded MBID. Conflicting MBIDs fall through to
	// the MBID hard-stop below.
	if e.Kind == domain.ResultKindAlbum && e.UPC != "" && c.UPC != "" && e.UPC == c.UPC &&
		(e.MBID == "" || c.MBID == "" || e.MBID == c.MBID) {
		return domain.EntityResolutionUPC, true
	}
	if e.MBID != "" && c.MBID != "" {
		if e.MBID == c.MBID {
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

	// Artists resolve by canonical name alone — unless the name normalizes to ""
	// (symbol-only, e.g. "!!!"): shared emptiness proves nothing, so the name
	// tier refuses (identifier/bridge tiers above still merge freely).
	if e.Kind == domain.ResultKindArtist {
		name := textnorm.NormalizeForMatch(e.Title)
		if name == "" {
			return domain.EntityResolutionNone, false
		}
		return domain.EntityResolutionNone, name == textnorm.NormalizeForMatch(c.Title)
	}

	// Tracks/albums: same artist and same canonical title. A fully-bracketed
	// title ("(Intro)", "[untitled]") normalizes to "" — again shared emptiness,
	// not a shared title, so the text tier refuses.
	if textnorm.NormalizeForMatch(e.Subtitle) != textnorm.NormalizeForMatch(c.Subtitle) {
		return domain.EntityResolutionNone, false
	}
	title := textnorm.NormalizeForMatch(e.Title)
	if title != "" && title == textnorm.NormalizeForMatch(c.Title) {
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
	// Keep the strongest proof: a later, weaker merge (e.g. a name-tier merge
	// after an ISRC one) must not downgrade an identity-proven entity's stamped
	// tier — and, via the switch below, its confidence.
	if prev := domain.ResolutionTierFromExtras(extras); prev > tier {
		tier = prev
	}
	extras["resolution_tier"] = tier.String()

	imageURL := canonical.ImageURL
	if imageURL == "" {
		imageURL = other.ImageURL
	}

	conf := domain.ConfidenceLow
	switch tier {
	case domain.EntityResolutionISRC, domain.EntityResolutionUPC, domain.EntityResolutionMBID, domain.EntityResolutionBridge:
		conf = domain.ConfidenceHigh
	default:
		if len(sources) > 1 {
			conf = domain.ConfidenceMedium
		}
	}

	merged := domain.SearchResult{
		Kind:       canonical.Kind,
		Title:      canonical.Title,
		Subtitle:   canonical.Subtitle,
		ImageURL:   imageURL,
		Confidence: conf,
		Sources:    sources,
		Popularity: math.Max(canonical.Popularity, other.Popularity),
		Extras:     extras,
	}
	// Typed metadata: canonical wins when set, else the other side fills the gap
	// (the same present-beats-absent rule the Extras overlay applies).
	merged.ISRC = firstNonEmpty(canonical.ISRC, other.ISRC)
	merged.UPC = firstNonEmpty(canonical.UPC, other.UPC)
	merged.MBID = firstNonEmpty(canonical.MBID, other.MBID)
	merged.Xref = canonical.Xref
	if len(merged.Xref) == 0 {
		merged.Xref = other.Xref
	}
	merged.Year = firstNonZero(canonical.Year, other.Year)
	merged.ReleaseDate = firstNonEmpty(canonical.ReleaseDate, other.ReleaseDate)
	merged.TrackCount = firstNonZero(canonical.TrackCount, other.TrackCount)
	merged.ProviderRank = firstNonZero(canonical.ProviderRank, other.ProviderRank)
	merged.FanCount = firstNonZero(canonical.FanCount, other.FanCount)
	// Album/Duration/DeezerAlbumID were silently dropped on every merge until
	// 2026-07-23 (only their Extras mirrors survived), starving their typed
	// consumers (isLowConfidenceTail, FindRelatedService).
	merged.Album = firstNonEmpty(canonical.Album, other.Album)
	merged.Duration = firstNonZero(canonical.Duration, other.Duration)
	merged.DeezerAlbumID = firstNonEmpty(canonical.DeezerAlbumID, other.DeezerAlbumID)
	return merged
}

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

func firstNonZero[T int | int64](a, b T) T {
	if a != 0 {
		return a
	}
	return b
}

// providerID is one (provider, external id) identity claim.
type providerID struct {
	provider domain.ProviderName
	id       string
}

// bridgeMatch reports whether e and c share any cross-provider identity claim.
// A claim is either a native source id or a bridged id carried in Xref
// (MB → provider, populated pre-merge from the IdentityBridge). At least one
// bridged claim must participate — two native ids alone are same-provider dups,
// not a cross-provider bridge — so one side must carry an Xref for this to fire.
func bridgeMatch(e, c domain.SearchResult) bool {
	if len(e.Xref) == 0 && len(c.Xref) == 0 {
		return false
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
// any bridged ids carried in Xref.
func identityClaims(r domain.SearchResult) map[providerID]bool {
	claims := make(map[providerID]bool, len(r.Sources)+1)
	for _, s := range r.Sources {
		if s.ExternalID != "" {
			claims[providerID{s.Provider, s.ExternalID}] = true
		}
	}
	for name, id := range r.Xref {
		if id == "" {
			continue
		}
		if p, err := domain.ParseProviderName(name); err == nil {
			claims[providerID{p, id}] = true
		}
	}
	return claims
}

// ambiguousArtistNames returns the set of normalized artist names for which
// MusicBrainz surfaced 2+ distinct identities (MBIDs). A name in this set is one
// where a bare name match is not safe identity proof — multiple real artists
// share it (e.g. "Che"). Computed once per merge from the raw provider groups.
func ambiguousArtistNames(perProvider [][]domain.SearchResult) map[string]bool {
	var flat []domain.SearchResult
	for _, group := range perProvider {
		flat = append(flat, group...)
	}
	return ambiguousArtistNamesFlat(flat)
}

// ambiguousArtistNamesFlat is the flat-slice core of ambiguousArtistNames,
// shared with CollapseArtistDuplicates which operates on already-merged results.
func ambiguousArtistNamesFlat(results []domain.SearchResult) map[string]bool {
	mbidsByName := make(map[string]map[string]bool)
	for _, r := range results {
		if r.Kind != domain.ResultKindArtist || r.MBID == "" {
			continue
		}
		// Only MusicBrainz-sourced MBIDs count. A stale Last.fm mbid for the SAME
		// artist would otherwise register as a second "identity" and mark the name
		// ambiguous — refusing legitimate bare-name merges (duplicate artist
		// cards). The set's semantics are "names for which MUSICBRAINZ surfaced
		// ≥2 MBIDs".
		if !providersOf(r)[domain.ProviderMusicBrainz] {
			continue
		}
		name := textnorm.NormalizeForMatch(r.Title)
		if mbidsByName[name] == nil {
			mbidsByName[name] = make(map[string]bool)
		}
		mbidsByName[name][r.MBID] = true
	}
	ambiguous := make(map[string]bool)
	for name, mbids := range mbidsByName {
		if len(mbids) >= 2 {
			ambiguous[name] = true
		}
	}
	return ambiguous
}

func completenessOf(r domain.SearchResult) int {
	n := 0
	if r.ImageURL != "" {
		n++
	}
	if r.ISRC != "" {
		n++
	}
	if r.UPC != "" {
		n++
	}
	if r.Duration != 0 {
		n++
	}
	if r.Album != "" {
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
