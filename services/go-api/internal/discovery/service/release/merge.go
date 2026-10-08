package release

import (
	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/shared/textnorm"
)

type ReleaseGroup struct {
	Releases   []domain.SearchResult
	IDVerified bool
}

type MergedRelease struct {
	Result      domain.SearchResult
	Providers   map[domain.ProviderName]bool
	HasStrongID bool
	IDVerified  bool
}

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
	a.Sources = UnionSources(a.Sources, b.Sources)
	a.RecordType = mergeReleaseRecordType(a.RecordType, b.RecordType)
	if !a.ResolutionTier.Stamped {
		a.ResolutionTier = b.ResolutionTier
	}
	a.Extras = mergeReleaseExtras(a.Extras, b.Extras)
	return a
}

func bestArtwork(a, b domain.SearchResult) (url string, source domain.ProviderKey) {
	if b.ArtworkConfidenceRank() > a.ArtworkConfidenceRank() {
		return b.ImageURL, b.ArtworkSource
	}
	return domain.FirstNonEmptyArtwork(a, b)
}

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
	return out
}

func mergeReleaseRecordType(a, b domain.RecordType) domain.RecordType {
	if rt := mergeRecordType(a, b); rt != "" {
		return rt
	}
	return b
}

func mergeRecordType(a, b domain.RecordType) domain.RecordType {
	if domain.ParseRecordType(string(b)).Rank() > domain.ParseRecordType(string(a)).Rank() {
		return b
	}
	return a
}

func hasStrongID(r domain.SearchResult) bool {
	return r.MBID != "" || r.ISRC != "" || r.UPC != "" || domain.StringExtra(r.Extras, domain.ExtraUPC) != ""
}

func UnionSources(a, b []domain.SourceRef) []domain.SourceRef {
	seen := make(map[string]bool, len(a)+len(b))
	out := make([]domain.SourceRef, 0, len(a)+len(b))
	for _, s := range append(append([]domain.SourceRef{}, a...), b...) {
		key := SourceKey(s.Provider, s.ExternalID)
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, s)
	}
	return out
}

func SourceKey(provider domain.ProviderName, id string) string {
	return provider.String() + ":" + id
}

func firstNonEmpty[T ~string](a, b T) T {
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

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
