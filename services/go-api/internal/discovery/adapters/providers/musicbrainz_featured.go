package providers

import (
	"regexp"
	"strings"

	"altune/go-api/internal/discovery/domain"
)

var mbFeatJoinRe = regexp.MustCompile(`(?i)\b(?:feat|ft|featuring|with)\b`)

func extractMBFeatured(credits []mbArtistRef) []domain.FeaturedArtist {
	out := make([]domain.FeaturedArtist, 0, len(credits))
	featured := false
	for _, c := range credits {
		if featured {
			if fa, ok := mbCreditToFeatured(c); ok {
				out = append(out, fa)
			}
		}
		if mbFeatJoinRe.MatchString(c.JoinPhrase) {
			featured = true
		}
	}
	return out
}

func mbCreditToFeatured(c mbArtistRef) (domain.FeaturedArtist, bool) {
	name := strings.TrimSpace(c.Name)
	fa := domain.FeaturedArtist{Role: domain.RoleFeatured}
	if c.Artist != nil {
		fa.MBID = c.Artist.ID
		if name == "" {
			name = strings.TrimSpace(c.Artist.Name)
		}
	}
	fa.Name = name
	return fa, name != ""
}
