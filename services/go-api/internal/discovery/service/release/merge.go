package release

import (
	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/shared/textnorm"
	"regexp"
	"sort"
	"strings"
)

var (
	bracketRe = regexp.MustCompile(`[\(\[\{]([^\)\]\}]*)[\)\]\}]`)
	creditRe  = regexp.MustCompile(`(?i)^\s*(?:feat|ft|featuring|with)\b`)
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
	byKey := make(map[string][]*MergedRelease)
	var order []*MergedRelease
	for _, group := range groups {
		for _, variant := range group.Releases {
			key := releaseKey(variant.Title)
			if key == "" {
				continue
			}
			m := compatibleRelease(byKey[key], variant)
			if m == nil {
				m = &MergedRelease{Result: variant, Providers: map[domain.ProviderName]bool{}}
				byKey[key] = append(byKey[key], m)
				order = append(order, m)
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
	for _, m := range order {
		out = append(out, *m)
	}
	return out
}

func compatibleRelease(candidates []*MergedRelease, variant domain.SearchResult) *MergedRelease {
	for _, c := range candidates {
		if !conflictingIdentity(c.Result, variant) {
			return c
		}
	}
	return nil
}

func conflictingIdentity(a, b domain.SearchResult) bool {
	return differ(a.MBID, b.MBID) || differ(a.UPC, b.UPC) ||
		ClaimsAnotherID(a, b) || albumVersusSingle(a.RecordType, b.RecordType)
}

func albumVersusSingle(a, b domain.RecordType) bool {
	pa, pb := domain.ParseRecordType(string(a)), domain.ParseRecordType(string(b))
	return pa == domain.RecordTypeAlbum && pb == domain.RecordTypeSingle ||
		pa == domain.RecordTypeSingle && pb == domain.RecordTypeAlbum
}

func ClaimsAnotherID(e, c domain.SearchResult) bool {
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
	base := textnorm.NormalizeForMatch(title)
	if base == "" {
		return ""
	}
	var qualifiers []string
	for _, m := range bracketRe.FindAllStringSubmatch(title, -1) {
		if creditRe.MatchString(m[1]) {
			continue
		}
		if q := textnorm.NormalizeForMatch(m[1]); q != "" {
			qualifiers = append(qualifiers, q)
		}
	}
	sort.Strings(qualifiers)
	return strings.Join(append([]string{base}, qualifiers...), textnorm.KeySeparator)
}

func bestOfRelease(a, b domain.SearchResult) domain.SearchResult {
	a.Subtitle = FirstNonEmpty(a.Subtitle, b.Subtitle)
	a.ReleaseDate = bestReleaseDate(a.ReleaseDate, b.ReleaseDate)
	a.Year = yearOfDate(a.ReleaseDate, FirstNonZero(a.Year, b.Year))
	a.TrackCount = maxInt(a.TrackCount, b.TrackCount)
	a.Duration = FirstNonZero(a.Duration, b.Duration)
	a.Album = FirstNonEmpty(a.Album, b.Album)
	a.ISRC = FirstNonEmpty(a.ISRC, b.ISRC)
	a.UPC = FirstNonEmpty(a.UPC, b.UPC)
	a.MBID = FirstNonEmpty(a.MBID, b.MBID)
	a.RecordingMBID = FirstNonEmpty(a.RecordingMBID, b.RecordingMBID)
	a.ReleaseMBID = FirstNonEmpty(a.ReleaseMBID, b.ReleaseMBID)
	a.ReleaseGroupMBID = FirstNonEmpty(a.ReleaseGroupMBID, b.ReleaseGroupMBID)
	a.ArtistMBID = FirstNonEmpty(a.ArtistMBID, b.ArtistMBID)
	if a.TrackPosition == (domain.TrackPosition{}) {
		a.TrackPosition = b.TrackPosition
	}
	if len(a.Contributors) == 0 {
		a.Contributors = b.Contributors
	}
	if a.Explicit == nil {
		a.Explicit = b.Explicit
	}
	a.ImageURL, a.ArtworkSource = bestArtwork(a, b)
	a.Sources = UnionSources(a.Sources, b.Sources)
	a.RecordType = mergeReleaseRecordType(a.RecordType, b.RecordType)
	if !a.ResolutionTier.Stamped {
		a.ResolutionTier = b.ResolutionTier
	}
	a.Extras = mergeReleaseExtras(a.Extras, b.Extras)
	return a
}

func yearOfDate(releaseDate string, fallback int) int {
	dated := domain.SearchResult{ReleaseDate: releaseDate}
	domain.NormalizeReleaseYear(&dated)
	return FirstNonZero(dated.Year, fallback)
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
	return r.MBID != "" || r.ISRC != "" || r.UPC != ""
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

func NormalizeRecordType(m MergedRelease) domain.RecordType {
	if m.Result.TrackCount == 1 {
		return domain.RecordTypeSingle
	}
	switch domain.ParseRecordType(string(m.Result.RecordType)) {
	case domain.RecordTypeSingle:
		return domain.RecordTypeSingle
	case domain.RecordTypeEP:
		return domain.RecordTypeEP
	default:
		return domain.RecordTypeAlbum
	}
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
