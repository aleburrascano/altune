package domain

import (
	"altune/go-api/internal/shared/songfacts"
	"altune/go-api/internal/shared/textnorm"
	"altune/go-api/internal/shared/versiontag"
	"regexp"
	"strconv"
	"strings"
)

type FeaturedArtist struct {
	Name     string
	MBID     string
	DeezerID int64
	Role     string
}

const RoleFeatured = "featured"

func (f FeaturedArtist) IdentityKey() string {
	if f.MBID != "" {
		return "mb:" + f.MBID
	}
	if f.DeezerID != 0 {
		return "dz:" + strconv.FormatInt(f.DeezerID, 10)
	}
	return "name:" + NormalizeFeaturedName(f.Name)
}

func (f FeaturedArtist) ToExtrasMap() map[string]any {
	m := map[string]any{"name": f.Name, "role": f.Role}
	if f.MBID != "" {
		m["mbid"] = f.MBID
	}
	if f.DeezerID != 0 {
		m["deezer_id"] = f.DeezerID
	}
	return m
}

func FeaturedArtistsToExtras(fs []FeaturedArtist) []map[string]any {
	if len(fs) == 0 {
		return nil
	}
	out := make([]map[string]any, 0, len(fs))
	for _, f := range fs {
		out = append(out, f.ToExtrasMap())
	}
	return out
}

func FeaturedArtistsFromExtras(extras map[string]any) []FeaturedArtist {
	switch raw := extras[ExtraFeaturedArtists].(type) {
	case []map[string]any:
		out := make([]FeaturedArtist, 0, len(raw))
		for _, m := range raw {
			out = append(out, FeaturedArtistFromMap(m))
		}
		return out
	default:
		return nil
	}
}

func FeaturedArtistFromMap(m map[string]any) FeaturedArtist {
	f := FeaturedArtist{
		Name: asString(m["name"]),
		MBID: asString(m["mbid"]),
		Role: asString(m["role"]),
	}
	if f.Role == "" {
		f.Role = RoleFeatured
	}
	switch v := m["deezer_id"].(type) {
	case int64:
		f.DeezerID = v
	case int:
		f.DeezerID = int64(v)
	case float64:
		f.DeezerID = int64(v)
	}
	return f
}

func NormalizeFeaturedName(s string) string {
	return textnorm.FoldName(s)
}

func asString(v any) string {
	if s, ok := v.(string); ok {
		return strings.TrimSpace(s)
	}
	return ""
}

var (
	bracketedRe    = regexp.MustCompile(`[(\[]([^()\[\]]*)[)\]]`)
	dashTailRe     = regexp.MustCompile(`\s[-–—]\s+(.*)$`)
	dashCutRe      = regexp.MustCompile(`\s[-–—]\s.*$`)
	withCreditRe   = regexp.MustCompile(`(?i)^\s*with\s+(.+)$`)
	featuredCredit = versiontag.FeaturedCredit
)

func FeaturedFromText(title, subtitle string) []FeaturedArtist {
	for _, text := range []string{title, subtitle} {
		if out := creditsIn(text); len(out) > 0 {
			return out
		}
	}
	return nil
}

func creditsIn(text string) []FeaturedArtist {
	var segments []string
	for _, m := range bracketedRe.FindAllStringSubmatch(text, -1) {
		segments = append(segments, m[1])
	}
	if m := dashTailRe.FindStringSubmatch(text); m != nil {
		segments = append(segments, m[1])
	}
	for _, seg := range segments {
		if w := withCreditRe.FindStringSubmatch(seg); w != nil {
			return featuredNames(w[1])
		}
	}
	for _, seg := range append(segments, dashCutRe.ReplaceAllString(text, "")) {
		if m := featuredCredit.FindStringSubmatch(seg); m != nil {
			return featuredNames(dashCutRe.ReplaceAllString(m[1], ""))
		}
	}
	return nil
}

func featuredNames(credit string) []FeaturedArtist {
	var out []FeaturedArtist
	for _, name := range songfacts.SplitCredit(credit) {
		if name = strings.TrimSpace(name); name != "" {
			out = append(out, FeaturedArtist{Name: name, Role: RoleFeatured})
		}
	}
	return out
}
