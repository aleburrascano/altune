package domain

import (
	"altune/go-api/internal/shared/music"
	"regexp"
	"sort"
	"strings"
)

var (
	bracketRe = regexp.MustCompile(`[\(\[\{]([^\)\]\}]*)[\)\]\}]`)
	creditRe  = regexp.MustCompile(`(?i)^\s*(?:feat|ft|featuring|with)\b`)
)

type ReleaseGroup struct {
	Releases   []SearchResult
	IDVerified bool
}

type MergedRelease struct {
	Result      SearchResult
	Providers   map[ProviderName]bool
	HasStrongID bool
	IDVerified  bool
}

func MergeReleases(groups []ReleaseGroup) []MergedRelease {
	byKey := make(map[string][]*MergedRelease)
	coverOf := make(map[*MergedRelease]SearchResult)
	var order []*MergedRelease
	for _, group := range groups {
		for _, variant := range group.Releases {
			key := releaseKey(variant.Title)
			if key == "" {
				continue
			}
			m := compatibleRelease(byKey[key], variant)
			if m == nil {
				m = &MergedRelease{Result: variant, Providers: map[ProviderName]bool{}}
				byKey[key] = append(byKey[key], m)
				order = append(order, m)
				coverOf[m] = variant
			} else {
				coverOf[m] = absorbVariant(m, coverOf[m], variant)
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
	for _, m := range order {
		out = append(out, *m)
	}
	return out
}

func absorbVariant(m *MergedRelease, cover, variant SearchResult) SearchResult {
	m.Result = bestOfRelease(m.Result, variant)
	cover = coverSupplier(cover, variant)
	m.Result.ImageURL, m.Result.ArtworkSource = cover.ImageURL, cover.ArtworkSource
	return cover
}

func compatibleRelease(candidates []*MergedRelease, variant SearchResult) *MergedRelease {
	for _, c := range candidates {
		if !conflictingIdentity(c.Result, variant) {
			return c
		}
	}
	return nil
}

func conflictingIdentity(a, b SearchResult) bool {
	return differ(a.RecordingMBID, b.RecordingMBID) || differ(a.ReleaseMBID, b.ReleaseMBID) ||
		differ(a.ReleaseGroupMBID, b.ReleaseGroupMBID) || differ(a.ArtistMBID, b.ArtistMBID) || differ(a.UPC, b.UPC) ||
		claimsAnotherID(a, b) || albumVersusSingle(a.RecordType, b.RecordType)
}

func albumVersusSingle(a, b RecordType) bool {
	pa, pb := ParseRecordType(string(a)), ParseRecordType(string(b))
	return pa == RecordTypeAlbum && pb == RecordTypeSingle ||
		pa == RecordTypeSingle && pb == RecordTypeAlbum
}

func claimsAnotherID(e, c SearchResult) bool {
	for _, cs := range c.Sources {
		sameProvider, sameID := false, false
		for _, es := range e.Sources {
			if es.Provider != cs.Provider {
				continue
			}
			sameProvider = true
			if es.ExternalID == cs.ExternalID {
				sameID = true
			}
		}
		if sameProvider && !sameID {
			return true
		}
	}
	return false
}

func differ(a, b string) bool {
	return a != "" && b != "" && a != b
}

func releaseKey(title string) string {
	base := music.NormalizeForMatch(title)
	if base == "" {
		return ""
	}
	var qualifiers []string
	for _, m := range bracketRe.FindAllStringSubmatch(title, -1) {
		if creditRe.MatchString(m[1]) {
			continue
		}
		if q := music.NormalizeForMatch(m[1]); q != "" {
			qualifiers = append(qualifiers, q)
		}
	}
	sort.Strings(qualifiers)
	return strings.Join(append([]string{base}, qualifiers...), music.KeySeparator)
}

func bestOfRelease(a, b SearchResult) SearchResult {
	a.Subtitle = FirstNonEmpty(a.Subtitle, b.Subtitle)
	a.ReleaseDate = bestReleaseDate(a.ReleaseDate, b.ReleaseDate)
	a.Year = yearOfDate(a.ReleaseDate, FirstNonZero(a.Year, b.Year))
	a.TrackCount = maxInt(a.TrackCount, b.TrackCount)
	a.Duration = FirstNonZero(a.Duration, b.Duration)
	a.Album = FirstNonEmpty(a.Album, b.Album)
	a.ISRC = FirstNonEmpty(a.ISRC, b.ISRC)
	a.UPC = FirstNonEmpty(a.UPC, b.UPC)
	a.RecordingMBID = FirstNonEmpty(a.RecordingMBID, b.RecordingMBID)
	a.ReleaseMBID = FirstNonEmpty(a.ReleaseMBID, b.ReleaseMBID)
	a.ReleaseGroupMBID = FirstNonEmpty(a.ReleaseGroupMBID, b.ReleaseGroupMBID)
	a.ArtistMBID = FirstNonEmpty(a.ArtistMBID, b.ArtistMBID)
	if a.TrackPosition == (TrackPosition{}) {
		a.TrackPosition = b.TrackPosition
	}
	if len(a.Contributors) == 0 {
		a.Contributors = b.Contributors
	}
	if a.Explicit == nil {
		a.Explicit = b.Explicit
	}
	a.Sources = UnionSources(a.Sources, b.Sources)
	a.RecordType = mergeReleaseRecordType(a.RecordType, b.RecordType)
	if !a.ResolutionTier.Stamped {
		a.ResolutionTier = b.ResolutionTier
	}
	a.Extras = mergeReleaseExtras(a.Extras, b.Extras)
	return a
}

func yearOfDate(releaseDate string, fallback int) int {
	dated := SearchResult{ReleaseDate: releaseDate}
	NormalizeReleaseYear(&dated)
	return FirstNonZero(dated.Year, fallback)
}

func coverSupplier(current, variant SearchResult) SearchResult {
	if variant.ArtworkConfidenceRank() > current.ArtworkConfidenceRank() || current.ImageURL == "" {
		return variant
	}
	return current
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

func mergeReleaseRecordType(a, b RecordType) RecordType {
	if rt := mergeRecordType(a, b); rt != "" {
		return rt
	}
	return b
}

func mergeRecordType(a, b RecordType) RecordType {
	if ParseRecordType(string(b)).Rank() > ParseRecordType(string(a)).Rank() {
		return b
	}
	return a
}

func hasStrongID(r SearchResult) bool {
	return r.HasEntityMBID() || r.ISRC != "" || r.UPC != ""
}

func UnionSources(a, b []ProviderLink) []ProviderLink {
	seen := make(map[string]bool, len(a)+len(b))
	out := make([]ProviderLink, 0, len(a)+len(b))
	for _, s := range append(append([]ProviderLink{}, a...), b...) {
		key := SourceKey(s.Provider, s.ExternalID)
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, s)
	}
	return out
}

func SourceKey(provider ProviderName, id string) string {
	return provider.String() + ":" + id
}

func FirstNonEmpty[T ~string](a, b T) T {
	if a != "" {
		return a
	}
	return b
}

func FirstNonZero[T int | int64](a, b T) T {
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

func NormalizeRecordType(m MergedRelease) RecordType {
	if m.Result.TrackCount == 1 {
		return RecordTypeSingle
	}
	switch ParseRecordType(string(m.Result.RecordType)) {
	case RecordTypeSingle:
		return RecordTypeSingle
	case RecordTypeEP:
		return RecordTypeEP
	case RecordTypeUnknown, RecordTypeAlbum, RecordTypeCompilation:
		return RecordTypeAlbum
	}
	return RecordTypeAlbum
}

func KeepRelease(m MergedRelease) bool {
	return m.IDVerified
}

func FilterKept(releases []MergedRelease) []MergedRelease {
	out := make([]MergedRelease, 0, len(releases))
	for _, m := range releases {
		if KeepRelease(m) {
			out = append(out, m)
		}
	}
	return out
}
