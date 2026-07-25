package service

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
	a.Sources = unionSources(a.Sources, b.Sources)
	a.Extras = mergeReleaseExtras(a.Extras, b.Extras)
	return a
}

func bestArtwork(a, b domain.SearchResult) (url, source string) {
	if a.ImageURL != "" {
		return a.ImageURL, a.ArtworkSource
	}
	return b.ImageURL, b.ArtworkSource
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
	if rt := mergeRecordType(stringExtra(a, "record_type"), stringExtra(b, "record_type")); rt != "" {
		out["record_type"] = rt
	}
	return out
}

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
