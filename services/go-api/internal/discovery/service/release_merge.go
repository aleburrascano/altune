package service

// Best-of release merge — the correctness core of the rebuilt discography path
// (docs/discovery-detail-pipeline.md §6). Unlike the old Merge/consensus, which
// pick ONE variant of a clustered release and discard the rest of its fields
// (faults F2/F3 in the doc — why a real album shows no year even though a
// provider returned its release date), this takes the BEST of every field across
// every provider that has the release, and unions their source ids. No variant is
// ever dropped. Pure: no I/O, exhaustively unit-tested.

import (
	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/shared/textnorm"
)

// ReleaseGroup is one provider's releases plus their provenance: IDVerified means
// the provider was queried by the artist's OWN id (contamination-proof), vs a
// by-name completeness fetch (which can surface a same-name artist's releases).
// The keep step weighs the two very differently.
type ReleaseGroup struct {
	Releases   []domain.SearchResult
	IDVerified bool
}

// MergedRelease is one release after best-of merge, plus the corroboration
// metadata the confidence-keep step (§6 build step 2) reads: which distinct
// providers carried it, whether any carried a strong identifier, and whether any
// contributing provider was queried by the artist's own id.
type MergedRelease struct {
	Result      domain.SearchResult
	Providers   map[domain.ProviderName]bool
	HasStrongID bool
	IDVerified  bool
}

// MergeReleases clusters release variants by canonical title and best-ofs each
// cluster into a single record. Output preserves first-seen cluster order for
// deterministic results.
func MergeReleases(groups []ReleaseGroup) []MergedRelease {
	byKey := make(map[string]*MergedRelease)
	var order []string
	for _, group := range groups {
		for _, variant := range group.Releases {
			key := textnorm.NormalizeForMatch(variant.Title)
			if key == "" {
				continue
			}
			m, ok := byKey[key]
			if !ok {
				m = &MergedRelease{Result: variant, Providers: map[domain.ProviderName]bool{}}
				byKey[key] = m
				order = append(order, key)
			} else {
				m.Result = bestOfRelease(m.Result, variant)
			}
			for _, s := range variant.Sources {
				m.Providers[s.Provider] = true
			}
			if hasStrongID(variant) {
				m.HasStrongID = true
			}
			if group.IDVerified {
				m.IDVerified = true
			}
		}
	}

	out := make([]MergedRelease, 0, len(order))
	for _, key := range order {
		out = append(out, *byKey[key])
	}
	return out
}

// bestOfRelease folds b into a, taking each field from whichever variant has it.
// a is the incumbent (keeps its Title as canonical); every other field upgrades
// when b carries a better value.
func bestOfRelease(a, b domain.SearchResult) domain.SearchResult {
	a.Subtitle = firstNonEmpty(a.Subtitle, b.Subtitle)
	a.ReleaseDate = bestReleaseDate(a.ReleaseDate, b.ReleaseDate)
	a.Year = firstNonZero(a.Year, b.Year)
	a.TrackCount = maxInt(a.TrackCount, b.TrackCount)
	a.Duration = firstNonZero(a.Duration, b.Duration)
	a.Album = firstNonEmpty(a.Album, b.Album)
	a.ISRC = firstNonEmpty(a.ISRC, b.ISRC)
	a.UPC = firstNonEmpty(a.UPC, b.UPC)
	a.MBID = firstNonEmpty(a.MBID, b.MBID)
	a.ImageURL, a.ArtworkSource = bestArtwork(a, b)
	a.Sources = unionSources(a.Sources, b.Sources)
	a.Extras = mergeReleaseExtras(a.Extras, b.Extras)
	return a
}

// bestArtwork keeps a's image when present, else adopts b's image AND its source
// tag together so ArtworkSource never describes the wrong URL.
func bestArtwork(a, b domain.SearchResult) (url, source string) {
	if a.ImageURL != "" {
		return a.ImageURL, a.ArtworkSource
	}
	return b.ImageURL, b.ArtworkSource
}

// bestReleaseDate prefers the more precise date (YYYY-MM-DD over a bare YYYY), so
// a provider that carries only a year never masks another's full date.
func bestReleaseDate(a, b string) string {
	if a == "" {
		return b
	}
	if b == "" {
		return a
	}
	if len(b) > len(a) {
		return b
	}
	return a
}

// mergeReleaseExtras unions two display-extras maps: b fills gaps, a's non-nil
// values win, except record_type which resolves to the more specific label.
func mergeReleaseExtras(a, b map[string]any) map[string]any {
	out := make(map[string]any, len(a)+len(b))
	for k, v := range b {
		out[k] = v
	}
	for k, v := range a {
		if v != nil {
			out[k] = v
		}
	}
	if rt := mergeRecordType(stringExtra(a, "record_type"), stringExtra(b, "record_type")); rt != "" {
		out["record_type"] = rt
	}
	return out
}

// mergeRecordType resolves two record-type signals to the more specific one: a
// concrete single/ep/compilation beats a generic "album" beats empty. (The full
// cross-provider normalizer is build step 3; this is the pairwise rule it uses.)
func mergeRecordType(a, b string) string {
	if recordTypeRank(b) > recordTypeRank(a) {
		return b
	}
	return a
}

func recordTypeRank(t string) int {
	switch t {
	case "single", "ep", "compilation":
		return 2
	case "album":
		return 1
	default:
		return 0
	}
}

// hasStrongID reports whether a variant carries a cross-provider identifier that
// makes its identity certain (a corroboration signal for the keep step). UPC
// prefers the typed field (what merge.go's UPC tier reads, and what applemusic.go
// instructs producers to set) with the Extras mirror as fallback, so a producer
// setting only one of the two still counts.
func hasStrongID(r domain.SearchResult) bool {
	return r.MBID != "" || r.ISRC != "" || r.UPC != "" || stringExtra(r.Extras, "upc") != ""
}

func unionSources(a, b []domain.SourceRef) []domain.SourceRef {
	seen := make(map[string]bool, len(a)+len(b))
	out := make([]domain.SourceRef, 0, len(a)+len(b))
	for _, s := range append(append([]domain.SourceRef{}, a...), b...) {
		key := s.Provider.String() + ":" + s.ExternalID
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, s)
	}
	return out
}

func stringExtra(extras map[string]any, key string) string {
	if extras == nil {
		return ""
	}
	if v, ok := extras[key].(string); ok {
		return v
	}
	return ""
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
